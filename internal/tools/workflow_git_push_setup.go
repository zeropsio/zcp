package tools

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// scpStyleRemote matches git's scp-form SSH remote syntax (e.g.
// `git@github.com:owner/repo.git`). url.ParseRequestURI rejects this form
// because there's no `://` scheme; git accepts it natively, so the
// remoteUrl validator (below) needs both branches.
//
//nolint:gochecknoglobals // immutable regex
var scpStyleRemote = regexp.MustCompile(`^[A-Za-z0-9_.-]+@[A-Za-z0-9_.-]+:[^/].*$`)

// validateRemoteURL accepts a remoteUrl in either the URI form
// (scheme://host/path — https / git / ssh) or git's scp-form
// (user@host:path) used by SSH remotes. Returns nil on success or a
// platform error with remediation pointing at the two accepted shapes.
//
// Phase 7 fix for the Phase 5 P0 surfaced by Codex Phase 6 review: the
// initial url.ParseRequestURI-only validator rejected valid scp-form
// SSH URLs (e.g. git@github.com:owner/repo.git).
func validateRemoteURL(remote string) error {
	if scpStyleRemote.MatchString(remote) {
		return nil
	}
	if _, err := url.ParseRequestURI(remote); err != nil {
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("remoteUrl %q is not a valid git remote: %v", remote, err),
			"Pass a fully-qualified URL (https://github.com/owner/repo.git) or scp-form SSH remote (git@github.com:owner/repo.git)",
		)
	}
	return nil
}

// handleGitPushSetup walks the agent through configuring git-push capability
// for a service: GIT_TOKEN on the container (or origin URL on local) plus
// the remote repository URL. Introduced by deploy-strategy decomposition
// Phase 5.
//
// Two modes:
//
//   - Walkthrough (input.RemoteURL empty): synthesize the env-aware setup
//     atom from the corpus — agent reads, executes the steps, then re-calls
//     with input.RemoteURL set. No meta mutation.
//   - Confirm (input.RemoteURL set): writes meta.GitPushState=configured
//     plus meta.RemoteURL on a successful pre-flight, returns confirmation.
//     The pre-flight is light at this layer — full GIT_TOKEN / .netrc
//     verification still runs at zerops_deploy time (deploy-decomp P4).
//
// service param is required and resolves via FindServiceMeta (pair-keyed).
// Stage-hostname targets are rejected with the same source-of-push remediation
// as the deploy handlers.
func handleGitPushSetup(input WorkflowInput, stateDir string, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	if input.Service == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"service is required for git-push-setup",
			"Pass service=<hostname> identifying the runtime to configure"), WithRecoveryStatus()), nil, nil
	}

	meta, err := workflow.FindServiceMeta(stateDir, input.Service)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Read service meta %q: %v", input.Service, err),
			""), WithRecoveryStatus()), nil, nil
	}
	if meta == nil || !meta.IsComplete() {
		// Mirrors workflow_close_mode.go — point at bootstrap+adopt, not
		// generic status. Code is ErrAdoptRequired (not ErrServiceNotFound —
		// the service IS found in Zerops, it just lacks ZCP bootstrap
		// metadata). Pinned by TestErrAdoptRequiredCarriesAdoptRecovery.
		return convertError(platform.NewPlatformError(
			platform.ErrAdoptRequired,
			fmt.Sprintf("Service %q is not bootstrapped", input.Service),
			"Run bootstrap first: zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\""),
			WithRecovery(&RecoveryHint{
				Tool:   "zerops_workflow",
				Action: "start",
				Args:   map[string]string{"workflow": "bootstrap", "route": "adopt"},
			})), nil, nil
	}
	switch meta.PushSourceCheckFor(input.Service) {
	case topology.PushSourceOK:
		// proceed
	case topology.PushSourceIsStageHalf:
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("git-push-setup target %q is the stage half of a pair (build target, never push source); set up from the dev half %q instead", input.Service, meta.Hostname),
			fmt.Sprintf("Retry with: zerops_workflow action=\"git-push-setup\" service=%q", meta.Hostname),
		), WithRecoveryStatus()), nil, nil
	case topology.PushSourceModeUnsupported:
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("git-push-setup target %q is in mode %q which does not support push-git (only Standard/Simple/LocalStage/LocalOnly do)", input.Service, meta.Mode),
			"Mode expansion (ModeDev → ModeStandard adds a stage half) is a bootstrap-with-isExisting flow, not a workflow action. Re-run bootstrap with route=adopt and a plan target that carries isExisting=true + bootstrapMode=\"standard\" + an explicit stageHostname. See develop-mode-expansion atom for the plan shape.",
		), WithRecoveryStatus()), nil, nil
	case topology.PushSourceUnknownHost:
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("git-push-setup target %q is not part of meta scope keyed at %q", input.Service, meta.Hostname),
			"The meta lookup matched a different service. Verify the hostname or re-run bootstrap on the right pair.",
		), WithRecoveryStatus()), nil, nil
	default:
		// Defensive: future PushSourceResult variants must be classified
		// explicitly. Falling through silently as if OK would let a new
		// rejection case slip past validation.
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("internal classifier returned an unexpected PushSourceResult for service %q — please file a bug", input.Service),
			"Run zerops_workflow action=\"status\" to recover and report the issue.",
		), WithRecoveryStatus()), nil, nil
	}

	// Walkthrough mode: synthesize the env-aware setup atom + emit the
	// structured input-collection block. inputsRequired carries the parsable
	// shape AskUserQuestion-capable harnesses render as a typed picker
	// instead of having the LLM improvise free-text questions for repo URL +
	// PAT + integration choice (which sent every observed real-session agent
	// down the "ask three things in a numbered list" path and let webhook
	// land as the "obvious" default).
	if input.RemoteURL == "" {
		snap := workflow.ServiceSnapshot{
			Hostname:        input.Service,
			Mode:            meta.Mode,
			StageHostname:   meta.StageHostname,
			Bootstrapped:    true,
			CloseDeployMode: topology.CloseModeGitPush,
			GitPushState:    topology.GitPushUnconfigured,
		}
		guidance, err := workflow.SynthesizeStrategySetup(rt, []workflow.ServiceSnapshot{snap})
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrNotImplemented,
				fmt.Sprintf("git-push-setup synthesis failed: %v", err),
				"Build-time defect — report it. Run `make lint-local` to verify the atom corpus."), WithRecoveryStatus()), nil, nil
		}
		return jsonResult(attachWorkSessionState(map[string]any{
			"status":   "walkthrough",
			"service":  input.Service,
			"guidance": guidance,
			"inputsRequired": []map[string]any{
				{
					"name":        "remoteUrl",
					"label":       "Git remote URL",
					"description": "HTTPS or scp-form SSH URL of the target repository. Example: https://github.com/<owner>/<repo>.git or git@github.com:<owner>/<repo>.git",
					"required":    true,
				},
				{
					"name":        "gitToken",
					"label":       "GIT_TOKEN (fine-grained PAT)",
					"description": "Personal access token scoped to the single target repo. For GitHub: Contents:Read+Write; add Secrets+Workflows if you plan integration=actions (recommended). For GitLab: write_repository; add api for webhook. Stored via zerops_env action=\"set\" project=true variables=[\"GIT_TOKEN=...\"].",
					"secret":      true,
					"required":    true,
				},
				{
					"name":        "integration",
					"label":       "CI integration",
					"description": "Which CI shape consumes the remote push. Actions = GitHub Actions workflow runs zcli push (recommended for GitHub — zero manual dashboard steps); webhook = Zerops dashboard OAuth pulls the repo (requires manual dashboard step); none = independent CI/CD you already own.",
					"options":     []string{"actions", "webhook", "none"},
					"required":    true,
				},
			},
			"recommendedIntegration": "actions",
			"prompt":                 "Three inputs needed to wire git-push for " + input.Service + ": (1) remote repo URL, (2) fine-grained PAT, (3) CI integration. Actions is the default for GitHub repos with a permissive PAT (zero manual Zerops dashboard step); webhook is the fallback for GitLab / policy-constrained repos; none means external CI/CD you already own.",
			"nextStep":               fmt.Sprintf("After collecting inputs: 1) set GIT_TOKEN via zerops_env action=\"set\" project=true. 2) confirm capability: zerops_workflow action=\"git-push-setup\" service=%q remoteUrl=<url>. 3) wire CI: zerops_workflow action=\"build-integration\" service=%q integration=\"actions|webhook|none\".", input.Service, input.Service),
		}, stateDir)), nil, nil
	}

	// Confirm mode: validate remoteUrl format then write meta state. Full
	// GIT_TOKEN / .netrc verification happens later at deploy time
	// (deploy_git_push.go pre-flight); the URL-format check here closes
	// the gap surfaced in the Phase 5 Codex POST-WORK review (a malformed
	// URL persisted in meta would survive to deploy preflight silently).
	if err := validateRemoteURL(input.RemoteURL); err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	meta.GitPushState = topology.GitPushConfigured
	meta.RemoteURL = input.RemoteURL
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Write service meta %q: %v", input.Service, err),
			""), WithRecoveryStatus()), nil, nil
	}

	return jsonResult(attachWorkSessionState(map[string]any{
		"status":                 "configured",
		"service":                input.Service,
		"gitPushState":           meta.GitPushState,
		"remoteUrl":              meta.RemoteURL,
		"recommendedIntegration": recommendIntegrationForRemoteURL(meta.RemoteURL),
		"nextStep":               fmt.Sprintf("git-push capability is ready. Wire CI (recommended for GitHub: integration=\"actions\" — zero manual dashboard step; for GitLab or policy-constrained repos: integration=\"webhook\"; for external CI/CD: integration=\"none\"): zerops_workflow action=\"build-integration\" service=%q integration=\"actions|webhook|none\". Then push via: zerops_deploy targetService=%q strategy=\"git-push\".", input.Service, input.Service),
	}, stateDir)), nil, nil
}

// recommendIntegrationForRemoteURL picks the default CI integration based
// on the remote URL host. GitHub URLs default to "actions" (the agent can
// land workflow + secrets via `gh` without leaving the terminal); GitLab
// and other hosts default to "webhook" (the dashboard OAuth is the
// canonical path there because no equivalent CLI-driven secret wiring
// exists). Empty URL falls back to "actions" — most users on the
// happy path are on GitHub, and the agent can still override.
func recommendIntegrationForRemoteURL(remote string) string {
	low := strings.ToLower(remote)
	if strings.Contains(low, "gitlab") {
		return "webhook"
	}
	return "actions"
}
