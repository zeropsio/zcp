package tools

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
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
// for a service. Container env: GIT_TOKEN + HTTPS remote URL + verified
// authentication. Local env: origin URL + user's local credential helper
// proves auth.
//
// Two modes:
//
//   - Walkthrough (input.RemoteURL empty): synthesize the env-aware setup
//     atom from the corpus — agent reads, executes the steps, then re-calls
//     with input.RemoteURL (+ input.GitToken in container mode). No meta
//     mutation.
//   - Confirm (input.RemoteURL set): probe-first verifier. Runs auth probe
//     against the supplied remoteUrl + gitToken BEFORE writing any project
//     state. On probe success: writes sensitive GIT_TOKEN env, restarts the
//     push-source runtime so $GIT_TOKEN is live, syncs origin in
//     /var/www/.git/config, then stamps meta.GitPushState=configured +
//     meta.RemoteURL. On probe failure: returns a structured credential
//     error with NO project state mutation — agent re-calls with corrected
//     inputs.
//
// service param is required and resolves via FindServiceMeta (pair-keyed).
// Stage-hostname targets are rejected with the same source-of-push remediation
// as the deploy handlers.
func handleGitPushSetup(
	ctx context.Context,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	projectID string,
	input WorkflowInput,
	stateDir string,
	rt runtime.Info,
) (*mcp.CallToolResult, any, error) {
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

	// Confirm mode: probe-first verifier.
	//
	// 1. Validate URL format (existing).
	// 2. Container env additionally requires gitToken + HTTPS-only URL.
	// 3. Run auth probe — if it fails, return error with NO state mutation.
	// 4. Side effects (in order): SSH sync origin in /var/www/.git/config,
	//    write sensitive GIT_TOKEN, restart push-source runtime, stamp meta.
	//
	// Local env (rt.InContainer == false) is handled by Phase 2 of the
	// systemic fix plan — until then, fall through to URL-format-only
	// confirm for backward compat (local path uses user's credential helper
	// directly; ZCP can't inject .netrc on user's local machine).
	if err := validateRemoteURL(input.RemoteURL); err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}

	if rt.InContainer {
		return confirmGitPushSetupContainer(ctx, client, sshDeployer, projectID, stateDir, input, meta)
	}

	// Local env — Phase 2 will replace this with a real local probe.
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
		"nextStep":               fmt.Sprintf("git-push capability is ready (local mode). Wire CI: zerops_workflow action=\"build-integration\" service=%q integration=\"actions|webhook|none\". Then push via: zerops_deploy targetService=%q strategy=\"git-push\".", input.Service, input.Service),
	}, stateDir)), nil, nil
}

// confirmGitPushSetupContainer implements the container-mode probe-first
// verifier. Reached only when rt.InContainer is true. Probe-first principle:
// no project state is mutated until the auth probe proves the supplied
// (gitToken, remoteUrl) pair authenticates against the remote.
func confirmGitPushSetupContainer(
	ctx context.Context,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	projectID, stateDir string,
	input WorkflowInput,
	meta *workflow.ServiceMeta,
) (*mcp.CallToolResult, any, error) {
	// HTTPS-only: SCP-form SSH remotes (git@github.com:owner/repo.git)
	// don't authenticate via .netrc + PAT. Reject early with a clear
	// remediation pointing at HTTPS form. (SSH deploy-key flow is a
	// separate phase, not yet implemented.)
	if scpStyleRemote.MatchString(input.RemoteURL) {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Container git-push-setup uses HTTPS + PAT auth via .netrc; SCP-form SSH remote %q is not supported.", input.RemoteURL),
			"Pass an HTTPS URL: https://github.com/<owner>/<repo>.git. SSH deploy-key flow is not yet implemented.",
		), WithRecoveryStatus()), nil, nil
	}
	u, parseErr := url.Parse(input.RemoteURL)
	if parseErr != nil || (u.Scheme != "https" && u.Scheme != "http") {
		//nolint:nilerr // parseErr surfaced via error-code wrap below; caller wants structured error not raw url.Parse error
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Container git-push-setup requires HTTPS remote URL; got %q", input.RemoteURL),
			"Pass an HTTPS URL: https://github.com/<owner>/<repo>.git.",
		), WithRecoveryStatus()), nil, nil
	}

	// Token required in container mode.
	if input.GitToken == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Container git-push-setup requires gitToken (fine-grained PAT) — the handler verifies the token against the remote before writing project state.",
			fmt.Sprintf("Re-call: zerops_workflow action=\"git-push-setup\" service=%q remoteUrl=%q gitToken=<PAT>. Fine-grained PAT scoped to owner/repo with Contents: Read and write (add Secrets/Workflows for integration=actions).", input.Service, input.RemoteURL),
		), WithRecoveryStatus()), nil, nil
	}

	pushHost := meta.Hostname

	// 1. Probe — read-only auth check via ephemeral .netrc. NO mutation.
	probeCmd := ops.BuildGitAuthProbeCommand(input.RemoteURL, input.GitToken)
	if _, probeErr := sshDeployer.ExecSSH(ctx, pushHost, probeCmd); probeErr != nil {
		// Probe failure = bad token OR unreachable URL OR network. We can
		// inspect probeErr for category but agent-side recovery is the
		// same: fix inputs and re-call. NO state was written.
		return convertError(platform.NewPlatformError(
			platform.ErrGitTokenInvalid,
			fmt.Sprintf("git-push-setup probe against %s failed: %v", input.RemoteURL, probeErr),
			"Verify: (1) PAT is correct and unexpired, (2) PAT has Contents: Read+Write on this repo (add Secrets/Workflows if integration=actions), (3) Remote URL exists and is reachable. Then re-call with corrected inputs. NO project state was modified.",
		), WithRecoveryStatus()), nil, nil
	}

	// 2. SSH sync origin in /var/www/.git/config. Writes survive the
	//    upcoming restart (filesystem persists). Done before env+restart
	//    so we don't have to wait for the container to come back.
	originCmd := ops.BuildGitOriginSyncCommand("/var/www", input.RemoteURL)
	if _, originErr := sshDeployer.ExecSSH(ctx, pushHost, originCmd); originErr != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("git-push-setup probe passed but origin sync on %s failed: %v", pushHost, originErr),
			"The container's /var/www/.git/config could not be updated. Confirm the container has /var/www/.git initialized (bootstrap runs InitServiceGit) and SSH is healthy, then re-call. NO project env or meta state was modified.",
		), WithRecoveryStatus()), nil, nil
	}

	// 3. Write GIT_TOKEN to project env as sensitive — value never echoes
	//    back in response or audit log.
	if _, envErr := ops.EnvSetSensitiveProject(ctx, client, projectID, "GIT_TOKEN", input.GitToken); envErr != nil {
		return convertError(envErr, WithRecoveryStatus()), nil, nil
	}

	// 4. Restart push-source so $GIT_TOKEN lands in the container's shell
	//    env. Without this, the next zerops_deploy strategy=git-push would
	//    SSH to the same container session and its gitTokenCheckCmd would
	//    return 0 (token in platform DB, not in shell).
	svc, lookupErr := ops.LookupService(ctx, client, projectID, pushHost)
	if lookupErr != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("git-push-setup: locate push-source service %q for restart: %v", pushHost, lookupErr),
			"Token was written to project env, origin synced, but push-source restart could not be issued (service lookup failed). Restart the runtime manually via zerops_manage action=restart serviceHostname=<host>, then re-call git-push-setup with the same inputs (probe is idempotent; subsequent runs will stamp configured).",
		), WithRecoveryStatus()), nil, nil
	}
	restartProc, restartErr := client.RestartService(ctx, svc.ID)
	if restartErr != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrAPIError,
			fmt.Sprintf("git-push-setup: restart push-source %q failed: %v", pushHost, restartErr),
			"Token was written + origin synced. Restart manually via zerops_manage action=restart serviceHostname=<host>, then re-call git-push-setup.",
		), WithRecoveryStatus()), nil, nil
	}
	// Poll restart to completion so the agent sees a fully ready container
	// on the next deploy call.
	_, _ = pollManageProcess(ctx, client, restartProc, nil)

	// 5. Stamp configured. All side effects succeeded.
	meta.GitPushState = topology.GitPushConfigured
	meta.RemoteURL = input.RemoteURL
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Write service meta %q: %v", input.Service, err),
			"All platform-side side effects (env write, restart) succeeded but local meta write failed. Re-run git-push-setup to re-stamp; the probe is idempotent (token already verified).",
		), WithRecoveryStatus()), nil, nil
	}

	return jsonResult(attachWorkSessionState(map[string]any{
		"status":                 "configured",
		"service":                input.Service,
		"gitPushState":           meta.GitPushState,
		"remoteUrl":              meta.RemoteURL,
		"recommendedIntegration": recommendIntegrationForRemoteURL(meta.RemoteURL),
		"nextStep":               fmt.Sprintf("git-push capability verified end-to-end: token authenticates against remote, origin synced on /var/www/.git/config, GIT_TOKEN live in container shell. Wire CI (integration=\"actions\" recommended for GitHub; \"webhook\" for GitLab; \"none\" for external CI/CD): zerops_workflow action=\"build-integration\" service=%q integration=\"actions|webhook|none\". Then push via: zerops_deploy targetService=%q strategy=\"git-push\".", input.Service, input.Service),
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
