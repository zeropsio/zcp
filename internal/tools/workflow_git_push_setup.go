package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// gitPushSessionAuthAttempts/Delay bound the step-4c session-auth
// verification loop: the platform env write propagates to fresh SSH
// sessions within the ~5-10s zembed window, so 8 × 3s comfortably covers
// it while failing fast enough on a genuinely broken write. Initialized
// package vars (not zero-value state) — tests narrow them to keep the
// failure path fast.
//
//nolint:gochecknoglobals // tuning knobs, initialized; test-narrowed
var (
	gitPushSessionAuthAttempts = 8
	gitPushSessionAuthDelay    = 3 * time.Second
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
	u, err := url.ParseRequestURI(remote)
	if err != nil {
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("remoteUrl %q is not a valid git remote: %v", remote, err),
			"Pass a fully-qualified URL (https://github.com/owner/repo) or scp-form SSH remote (git@github.com:owner/repo)",
		)
	}
	// Reject a credential embedded in the URL (https://user:token@host/...).
	// Auth is via the PAT in gitToken (stored as the GIT_TOKEN service
	// secret, consumed live by the credential helper), never the
	// remote URL — accepting it lands the secret verbatim in meta.RemoteURL and
	// the container's .git/config, and then leaks it on every drift echo. The
	// error names the credential-free shape so the agent re-passes a clean URL.
	if u.User != nil {
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("remoteUrl %q embeds a credential (user:token@) — ZCP authenticates with the PAT passed as gitToken, not via the URL.", topology.RedactRepoURLCredentials(remote)),
			fmt.Sprintf("Pass the credential-free URL: %s, and supply the PAT as gitToken.", topology.RedactRepoURLCredentials(remote)),
		)
	}
	return nil
}

// withSSHStderr appends the git stderr an SSHExecError carries to a failure
// prefix. SSHExecError.Error() renders only "ssh <host>: exit status N" — the
// distinguishing git output (Authentication failed vs Repository not found)
// lives in its Output field and was discarded (B6/F36). Non-SSH errors (the
// local probe folds stderr into Error() already) fall back to err verbatim.
func withSSHStderr(prefix string, err error) string {
	msg := prefix + ": " + err.Error()
	var sshErr *platform.SSHExecError
	if errors.As(err, &sshErr) {
		if out := strings.TrimSpace(sshErr.Output); out != "" {
			const maxStderr = 600
			if len(out) > maxStderr {
				out = out[:maxStderr] + "…"
			}
			msg += " — git stderr: " + out
		}
	}
	return msg
}

// shallowCloneGuard runs the F1b shallow-clone fix on a first-configuration
// push source BEFORE origin is rewritten. A recipe-bootstrapped service can
// carry a shallow/incomplete clone whose missing delta-base objects make a
// push to a new remote fail deterministically (p2 #1); the command auto
// `git fetch --unshallow`s from the CURRENT (recipe) origin. Returns a blocker
// result when the clone is shallow AND the fetch cannot recover it — the caller
// must return it before the origin sync so the original remote stays intact for
// manual recovery. Returns nil otherwise (not shallow, auto-unshallowed, or a
// non-fatal probe transport error — origin sync + push surface those).
func shallowCloneGuard(ctx context.Context, sshDeployer ops.SSHDeployer, pushHost, gitToken string) *mcp.CallToolResult {
	shallowOut, shallowErr := sshDeployer.ExecSSH(ctx, pushHost, ops.BuildGitShallowFixCommand("/var/www", gitToken))
	if shallowErr != nil {
		return nil
	}
	rest, isFail := strings.CutPrefix(strings.TrimSpace(string(shallowOut)), "ZCP_UNSHALLOW_FAIL")
	if !isFail {
		return nil
	}
	origURL := strings.TrimSpace(rest)
	return convertError(platform.NewPlatformError(
		platform.ErrPrerequisiteMissing,
		fmt.Sprintf("git-push-setup: %q has a shallow/incomplete git clone (recipe-bootstrapped) that could not be auto-completed from its origin %q — pushing it to a new remote will fail on missing objects.", pushHost, topology.RedactRepoURLCredentials(origURL)),
		fmt.Sprintf("Recover on the container, then re-call git-push-setup with the SAME inputs (NO remote ref, secret, origin, or meta state was modified; origin still points at the original remote — the local .git may have been self-heal-repaired: init/identity/HEAD only): (a) complete the history if the original remote is reachable — ssh %s \"cd /var/www && git fetch --unshallow\"; or (b) flatten to a self-contained snapshot — ssh %s \"cd /var/www && git checkout --orphan _zcp_flat && git add -A && git commit -m 'flatten for git-push' && git branch -M _zcp_flat main\".", pushHost, pushHost),
	), WithRecoveryStatus())
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
//     state. On probe success: writes GIT_TOKEN as a service-scope secret on the
//     push source, syncs origin in /var/www/.git/config (a fresh SSH session
//     reads the live secret — no restart), then stamps meta.GitPushState=configured +
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
	httpClient ops.HTTPDoer,
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
		// State-aware (F5): reflect the REAL recorded state instead of
		// hardcoding unconfigured — an agent re-entering on an
		// already-configured pair used to get the full PAT-collection
		// walkthrough with no hint the work was done.
		gitPushState := meta.GitPushState
		if gitPushState == "" {
			gitPushState = topology.GitPushUnconfigured
		}
		snap := workflow.ServiceSnapshot{
			Hostname:        input.Service,
			Mode:            meta.Mode,
			StageHostname:   meta.StageHostname,
			Bootstrapped:    true,
			CloseDeployMode: topology.CloseModeAuto,
			GitPushState:    gitPushState,
		}
		guidance, err := workflow.SynthesizeStrategySetup(rt, []workflow.ServiceSnapshot{snap})
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrNotImplemented,
				fmt.Sprintf("git-push-setup synthesis failed: %v", err),
				"Build-time defect — report it. Run `make lint-local` to verify the atom corpus."), WithRecoveryStatus()), nil, nil
		}
		// inputsRequired is env-aware: container mode collects gitToken so
		// the handler can probe + write it; local mode trusts the user's
		// local git credential helper and rejects gitToken explicitly on
		// the confirm path (see confirmGitPushSetupLocal).
		inputs := []map[string]any{
			{
				"name":        "remoteUrl",
				"label":       "Git remote URL",
				"description": gitPushRemoteURLDescription(rt),
				"required":    true,
			},
		}
		if rt.InContainer {
			inputs = append(inputs, map[string]any{
				"name":        "gitToken",
				"label":       "GIT_TOKEN (fine-grained PAT)",
				"description": "Personal access token scoped to the single target repo. For GitHub git-push only: " + ghPATScopeRecommendation("", false) + " For the recommended GitHub Actions track, the FULL set is REQUIRED: " + ghPATScopeRecommendation("", true) + " For GitLab: write_repository; add api for webhook. The handler probes this token against the remote BEFORE writing it as a service-scope secret on the push source — value is never echoed back.",
				"secret":      true,
				"required":    true,
			})
		}
		inputs = append(inputs,
			map[string]any{
				"name":        "integration",
				"label":       "CI integration",
				"description": "Which CI shape consumes the remote push. Actions = GitHub Actions workflow runs zcli push (recommended for GitHub — zero manual dashboard steps); webhook = Zerops dashboard OAuth pulls the repo (requires manual dashboard step); none = independent CI/CD you already own.",
				"options":     []string{"actions", "webhook", "none"},
				"required":    true,
			})
		body := map[string]any{
			"status":                 "walkthrough",
			"service":                input.Service,
			"gitPushState":           gitPushState,
			"guidance":               guidance,
			"inputsRequired":         inputs,
			"recommendedIntegration": "actions",
			"prompt":                 gitPushWalkthroughPrompt(rt, input.Service),
			"nextStep":               gitPushWalkthroughNextStep(rt, input.Service),
			"steps":                  gitPushWalkthroughSteps(rt, input.Service),
		}
		if meta.GitPushState == topology.GitPushConfigured && meta.RemoteURL != "" {
			body["alreadyConfigured"] = map[string]any{
				"remoteUrl": topology.RedactRepoURLCredentials(meta.RemoteURL),
				"note":      "git-push is ALREADY configured for this pair — re-running the walkthrough is only needed to point at a DIFFERENT remote. A confirm re-call with the same remoteUrl is a no-op (no probe, no restart).",
			}
		}
		return jsonResult(attachWorkSessionState(body, stateDir)), nil, nil
	}

	// Confirm mode: probe-first verifier.
	//
	// 1. Validate URL format (existing).
	// 2. Container env additionally requires gitToken + HTTPS-only URL.
	// 3. Run auth probe — if it fails, return error with NO state mutation.
	// 4. Side effects (in order): SSH sync origin in /var/www/.git/config,
	//    write sensitive GIT_TOKEN, stamp meta (no restart — a fresh SSH
	//    session reads the live secret within seconds).
	//
	// Local env (rt.InContainer == false) is handled by Phase 2 of the
	// systemic fix plan — until then, fall through to URL-format-only
	// confirm for backward compat (local path uses user's credential helper
	// directly; ZCP holds no credential on the user's local machine).
	if err := validateRemoteURL(input.RemoteURL); err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}

	if rt.InContainer {
		return confirmGitPushSetupContainer(ctx, client, httpClient, sshDeployer, projectID, stateDir, input, meta)
	}
	return confirmGitPushSetupLocal(ctx, stateDir, input, meta)
}

// localGitProbeReader is the local-mode auth-probe hook. Production wires
// it to ops.RunGitAuthProbeLocal; tests swap a stub. Variable indirection
// only — initial assignment is the real implementation, so per CLAUDE.md
// the global-state-ban exempts this initialized form.
//
//nolint:gochecknoglobals // test hook for local-mode probe
var localGitProbeReader = ops.RunGitAuthProbeLocal

// localGitOriginSyncer is the local-mode origin-sync hook. Same indirection
// pattern as localGitProbeReader.
//
//nolint:gochecknoglobals // test hook for local-mode origin sync
var localGitOriginSyncer = ops.RunGitOriginSyncLocal

// setLocalGitProbeReader swaps localGitProbeReader for the duration of one
// test; returns a cleanup func to defer-restore. Test-only helper.
func setLocalGitProbeReader(f func(ctx context.Context, workingDir, remoteURL string) error) func() {
	prev := localGitProbeReader
	localGitProbeReader = f
	return func() { localGitProbeReader = prev }
}

// setLocalGitOriginSyncer swaps localGitOriginSyncer for the duration of
// one test; returns a cleanup func to defer-restore. Test-only helper.
func setLocalGitOriginSyncer(f func(ctx context.Context, workingDir, remoteURL string) error) func() {
	prev := localGitOriginSyncer
	localGitOriginSyncer = f
	return func() { localGitOriginSyncer = prev }
}

// confirmGitPushSetupLocal implements the local-mode probe-first verifier.
// Reached only when rt.InContainer is false. Symmetric to the container
// path but uses the user's local git config + credential helper instead
// of the container-side credential helper — ZCP never sees credentials in local mode.
// Probe-first: no project state is mutated until the probe proves the
// supplied remoteUrl is reachable + authenticates with local creds.
func confirmGitPushSetupLocal(
	ctx context.Context,
	stateDir string,
	input WorkflowInput,
	meta *workflow.ServiceMeta,
) (*mcp.CallToolResult, any, error) {
	// gitToken not collected locally — local git holds the user's creds
	// directly (SSH agent, OS credential manager, cached PAT). Reject
	// explicitly so an agent re-using container-mode atom guidance gets
	// an early signal rather than a misleading "auth failed" later.
	if input.GitToken != "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Local git-push-setup does not collect gitToken — your local git already holds credentials (SSH agent, OS credential manager, cached PAT).",
			"Re-call without gitToken: zerops_workflow action=\"git-push-setup\" service=<host> remoteUrl=<url>",
		), WithRecoveryStatus()), nil, nil
	}

	workingDir, wdErr := os.Getwd()
	if wdErr != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Local git-push-setup: resolve workingDir: %v", wdErr),
			"This usually means the agent's process has no current directory. Run from a project workspace.",
		), WithRecoveryStatus()), nil, nil
	}

	// 1. Probe — write-auth check (push --dry-run; RunGitAuthProbeLocal) using
	//    local credentials. Non-mutating: sends no pack, creates no ref.
	if probeErr := localGitProbeReader(ctx, workingDir, input.RemoteURL); probeErr != nil {
		// localGitProbeReader folds git stderr into err.Error(); classify it
		// (the credential contract is appended by convertError).
		classification := ops.ClassifyDeployFailure(ops.FailureInput{
			Phase:        ops.PhaseTransport,
			Strategy:     "git-push",
			TransportErr: probeErr,
		})
		return convertError(platform.NewPlatformError(
			platform.ErrGitTokenInvalid,
			withSSHStderr(fmt.Sprintf("git-push-setup probe against %s failed", input.RemoteURL), probeErr),
			"Read failureClassification for the precise cause (test locally with `git ls-remote "+input.RemoteURL+" HEAD`), fix the named input, then re-call. NO project state was modified.",
		), WithFailureClassification(classification), WithRecoveryStatus()), nil, nil
	}

	// 2. Sync origin in workingDir's .git/config.
	if syncErr := localGitOriginSyncer(ctx, workingDir, input.RemoteURL); syncErr != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("git-push-setup probe passed but origin sync in %s failed: %v", workingDir, syncErr),
			"Ensure the working directory is a git repo (`git init` if not) and the user has write permission to .git/config. NO meta state was modified.",
		), WithRecoveryStatus()), nil, nil
	}

	// 3. Stamp configured — decide-outside / commit-inside: all network side
	// effects (probe, origin-sync) happened OUTSIDE the lock above; here we only
	// commit the {GitPushState,RemoteURL} delta onto the fresh meta under the
	// .services.lock, so a concurrent close-mode / build-integration on the same
	// pair isn't lost-updated (XCUT-1).
	if err := workflow.UpdateServiceMeta(stateDir, input.Service, func(m *workflow.ServiceMeta) error {
		m.GitPushState = topology.GitPushConfigured
		m.RemoteURL = input.RemoteURL
		return nil
	}); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Write service meta %q: %v", input.Service, err),
			"Origin synced, probe passed, but local meta write failed. Re-run git-push-setup to re-stamp; probe is idempotent.",
		), WithRecoveryStatus()), nil, nil
	}
	meta.GitPushState = topology.GitPushConfigured // mirror onto local copy for the response below
	meta.RemoteURL = input.RemoteURL

	localDelivery := deliveryDecisionForMeta(meta)
	return jsonResult(attachWorkSessionState(map[string]any{
		"status":                    "configured",
		"service":                   input.Service,
		"gitPushState":              meta.GitPushState,
		"remoteUrl":                 meta.RemoteURL,
		"recommendedIntegration":    string(localDelivery.Recommended),
		"recommendedIntegrationWhy": localDelivery.Why,
		"nextStep":                  fmt.Sprintf("git-push wiring verified (local mode): your git credential passed the push auth probe (`git push --dry-run`; on a repo with no commit yet it falls back to read reachability and the first push proves write) and origin is synced in workingDir. A non-fast-forward (the remote branch has commits yours doesn't) still surfaces at the first real push, not here. Wire CI: zerops_workflow action=\"build-integration\" service=%q integration=\"actions|webhook|none\". Then push via: zerops_deploy targetService=%q strategy=\"git-push\".", input.Service, input.Service),
	}, stateDir)), nil, nil
}

// confirmGitPushSetupContainer implements the container-mode probe-first
// verifier. Reached only when rt.InContainer is true. Probe-first principle:
// no project state is mutated until the auth probe proves the supplied
// (gitToken, remoteUrl) pair authenticates against the remote.
func confirmGitPushSetupContainer(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	projectID, stateDir string,
	input WorkflowInput,
	meta *workflow.ServiceMeta,
) (*mcp.CallToolResult, any, error) {
	// HTTPS-only: SCP-form SSH remotes (git@github.com:owner/repo.git)
	// don't authenticate via PAT-over-HTTPS. Reject early with a clear
	// remediation pointing at HTTPS form. (SSH deploy-key flow is a
	// separate phase, not yet implemented.)
	if scpStyleRemote.MatchString(input.RemoteURL) {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Container git-push-setup uses HTTPS + PAT auth (session-env credential helper); SCP-form SSH remote %q is not supported.", input.RemoteURL),
			"Pass an HTTPS URL: https://github.com/<owner>/<repo>.git. SSH deploy-key flow is not yet implemented.",
		), WithRecoveryStatus()), nil, nil
	}
	u, parseErr := url.Parse(input.RemoteURL)
	if parseErr != nil || u.Scheme != "https" {
		//nolint:nilerr // parseErr surfaced via error-code wrap below; caller wants structured error not raw url.Parse error
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			// http:// is rejected, not just non-URLs: the PAT would
			// travel in cleartext over an http remote (tell==check — the
			// message already says HTTPS).
			fmt.Sprintf("Container git-push-setup requires an HTTPS remote URL; got %q", topology.RedactRepoURLCredentials(input.RemoteURL)),
			"Pass an HTTPS URL: https://github.com/<owner>/<repo>.git.",
		), WithRecoveryStatus()), nil, nil
	}

	// O3 check-before-mutate (B6c/GPS-5): a pair already configured with this
	// same canonical remote AND no fresh token is a redundant re-call —
	// short-circuit BEFORE the probe + env write. A NON-EMPTY gitToken on the
	// same remote is ROTATION INTENT (spec-git-delivery-target §4): the user
	// holds a new/rescoped PAT and the canonical action must accept it — the
	// old token-blind short-circuit forced agents into the raw zerops_env
	// bypass (which echoed the secret; prod.txt T3).
	rotation := meta.GitPushState == topology.GitPushConfigured &&
		topology.CanonicalRepoURL(meta.RemoteURL) == topology.CanonicalRepoURL(input.RemoteURL)
	if rotation && input.GitToken == "" {
		return gitPushConfiguredRecall(ctx, sshDeployer, input, meta), nil, nil
	}

	// Token required in container mode (first configuration).
	if input.GitToken == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Container git-push-setup requires gitToken (fine-grained PAT) — the handler verifies the token against the remote before writing project state.",
			fmt.Sprintf("Re-call: zerops_workflow action=\"git-push-setup\" service=%q remoteUrl=%q gitToken=<PAT>. For git-push only use %s For the recommended GitHub Actions track use %s", input.Service, input.RemoteURL, ghPATScopeRecommendation("", false), ghPATScopeRecommendation("", true)),
		), WithRecoveryStatus()), nil, nil
	}

	pushHost := meta.Hostname

	// 1+2. Presence check (captured BEFORE any self-heal) + the conditional
	// local self-heal itself. Extracted to gitPushSetupPreProbeSelfHeal to
	// keep this probe-orchestration function under the maintainability
	// ceiling; see that function's doc comment for the full rationale
	// (Codex diff-review finding 1 — presence must be read before the
	// self-heal can create .git and mask a pending reconstruction).
	needsReconstruct, healBlocker := gitPushSetupPreProbeSelfHeal(ctx, sshDeployer, pushHost, meta)
	if healBlocker != nil {
		return healBlocker, nil, nil
	}

	// 3. Probe — WRITE-auth check via the inline credential helper with the
	//    CANDIDATE token (probe-first). NO mutation of remote/project state
	//    (push --dry-run sends no pack, creates no ref; a prior local repo
	//    self-heal in step 2, if it ran, already happened regardless of
	//    this outcome). A garbage / read-only token fails HERE, before any
	//    secret is written, so an existing working GIT_TOKEN is never
	//    clobbered by an unproven replacement (the destruction guard is the
	//    probe-first structure itself, now that the proof matches the claim).
	probeCmd := ops.BuildGitWritePushProbeCommand("/var/www", input.RemoteURL, input.GitToken)
	if _, probeErr := sshDeployer.ExecSSH(ctx, pushHost, probeCmd); probeErr != nil {
		// Surface the git stderr the SSHExecError carries (Authentication
		// failed vs Repository not found vs network) + classify it, so the
		// agent fixes the ONE right input instead of guessing across three
		// (B6/F36). The credential contract is appended by convertError.
		classification := ops.ClassifyDeployFailure(ops.FailureInput{
			Phase:        ops.PhaseTransport,
			Strategy:     "git-push",
			TransportErr: probeErr,
		})
		return convertError(platform.NewPlatformError(
			platform.ErrGitTokenInvalid,
			withSSHStderr(fmt.Sprintf("git-push-setup probe against %s failed", input.RemoteURL), probeErr),
			"Read failureClassification for the precise cause, then fix the named input and re-call. NO remote ref, secret, origin, or meta state was modified (the push source's local .git may have been self-heal-repaired — init/identity/HEAD only — regardless of this failure).",
		), WithFailureClassification(classification), WithRecoveryStatus()), nil, nil
	}

	// 3b. Human attribution (F3): derive identity from the GitHub PAT
	// (github.com remotes only) and seed it into the already-present repo,
	// or hand it to reconstruction (step 6d) so a rebuilt repo lands
	// human-attributed from its first init. Best-effort end to end — every
	// failure mode falls back to the robot identity and surfaces a
	// non-blocking warning; a genuinely custom pre-existing identity is
	// preserved and reported, never silently left unexplained.
	identity, emailSeeded, nameSeeded, emailPreserved, namePreserved, identityWarning := gitPushSetupDeriveAndSeedIdentity(
		ctx, httpClient, sshDeployer, pushHost, input.RemoteURL, input.GitToken, needsReconstruct,
	)

	// 4. SSH sync origin + url-scoped credential helper in /var/www/.git
	//    (single assertion owner; also one-way-cleans any stray legacy
	//    .netrc residue). Skipped when reconstruction will wire origin +
	//    helper itself (step 6d).
	if !needsReconstruct {
		// Shallow-clone guard (F1b) — only on FIRST configuration: a configured
		// pair already synced origin to the user's repo, so there's no recipe
		// remote to preserve and a token rotation must never be blocked here.
		if meta.GitPushState != topology.GitPushConfigured {
			if blocker := shallowCloneGuard(ctx, sshDeployer, pushHost, input.GitToken); blocker != nil {
				return blocker, nil, nil
			}
		}

		originCmd := ops.BuildGitOriginSyncCommand("/var/www", input.RemoteURL)
		if _, originErr := sshDeployer.ExecSSH(ctx, pushHost, originCmd); originErr != nil {
			// Same stderr swallow lived here (B6-N1) — surface it too, else
			// shipping the probe fix just re-creates the bug one branch lower.
			return convertError(platform.NewPlatformError(
				platform.ErrSSHDeployFailed,
				withSSHStderr(fmt.Sprintf("git-push-setup probe passed but origin sync on %s failed", pushHost), originErr),
				"The container's /var/www/.git/config could not be updated. Confirm the container has /var/www/.git initialized (bootstrap runs InitServiceGit) and SSH is healthy, then re-call. NO project env or meta state was modified.",
			), WithRecoveryStatus()), nil, nil
		}
	}

	// 5. Resolve the push-source service — needed for BOTH the
	//    service-scoped token write and the session-auth check below.
	svc, lookupErr := ops.LookupService(ctx, client, projectID, pushHost)
	if lookupErr != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("git-push-setup: locate push-source service %q: %v", pushHost, lookupErr),
			"Origin synced, but the push-source service could not be resolved — no env was written. Verify the hostname via zerops_discover, then re-call git-push-setup with the same inputs (probe is idempotent).",
		), WithRecoveryStatus()), nil, nil
	}

	// 6. Write GIT_TOKEN as a SERVICE-scope secret on the push source —
	//    value never echoes back in response or audit log. Service scope
	//    (F5): one token per push-source/repo pair (a second pair's setup
	//    no longer clobbers the first project-wide), and the platform's
	//    service userData lands as Type=SECRET, which actually masks on
	//    read — the project-level sensitive flag did NOT persist, so the
	//    old singleton was effectively unmasked in discover reads.
	if _, envErr := ops.EnvSetSecretService(ctx, client, svc.ID, ops.GitTokenEnvKey, input.GitToken); envErr != nil {
		return convertError(envErr, WithRecoveryStatus()), nil, nil
	}

	// 6b. Lazy one-way migration off the legacy PROJECT-scope singleton:
	//     when the project env still carries GIT_TOKEN, delete it — the
	//     service-scope secret above is the sole owner now. Best-effort:
	//     a delete failure leaves a redundant (and unused) project key.
	_ = ops.EnvDeleteProjectKeyIfPresent(ctx, client, projectID, ops.GitTokenEnvKey)

	// 6c. Session-auth verification — the XCUT-2 successor. The old path
	// restarted the container and polled the restart to terminal SUCCESS,
	// because $GIT_TOKEN was believed live only in post-restart shells.
	// Live-verified reality (spec-git-delivery-target §4): FRESH SSH
	// sessions see a platform env write within seconds, no restart — so
	// the confirmed-live check becomes an end-to-end session probe (env
	// store → fresh-session env → credential helper → remote) retried
	// across the ~5-10s zembed propagation window. The invariant survives
	// with a new check target: `configured` is stamped ONLY after a fresh
	// session authenticated with the just-written secret.
	if sessionErr := gitPushSessionAuthVerify(ctx, sshDeployer, pushHost, input.RemoteURL); sessionErr != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrAPITimeout,
			withSSHStderr(fmt.Sprintf("git-push-setup: token written as a service-scope secret on %q but a fresh SSH session could not authenticate with it yet", pushHost), sessionErr),
			"The secret is stored and origin is synced, but the end-to-end session check (fresh SSH session → credential helper → remote) did not pass within the propagation window. Re-call git-push-setup with the same inputs — the probe is idempotent and stamps configured once a fresh session authenticates.",
		), WithRecoveryStatus()), nil, nil
	}

	// 6d. Reconstruction (configured pair whose /var/www/.git vanished —
	// see step 1): rebuild from the recorded remote using the SESSION env
	// token the loop above just proved live. Non-destructive: mixed reset
	// aligns HEAD/index to the remote tree, the working tree stays
	// untouched; divergence surfaces in the response, never destroyed.
	reconstructed := false
	reconstructDivergence := ""
	if needsReconstruct {
		divergence, reconErr := gitPushReconstruct(ctx, sshDeployer, pushHost, input.RemoteURL, identity)
		if reconErr != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrSSHDeployFailed,
				withSSHStderr(fmt.Sprintf("git-push-setup: token verified but /var/www/.git reconstruction on %q failed", pushHost), reconErr),
				"The secret is stored; only the repo rebuild failed. Re-call git-push-setup with the same inputs to retry the reconstruction.",
			), WithRecoveryStatus()), nil, nil
		}
		reconstructed = true
		reconstructDivergence = divergence
	}

	// 7. Stamp configured — decide-outside / commit-inside: all side effects
	// (SSH, env write) happened OUTSIDE the lock above; here we only commit
	// the {GitPushState,RemoteURL} delta onto the fresh meta under the
	// .services.lock (XCUT-1). Reached only after the session-auth probe
	// confirmed the secret live end-to-end (XCUT-2 successor, step 6c).
	if err := workflow.UpdateServiceMeta(stateDir, input.Service, func(m *workflow.ServiceMeta) error {
		m.GitPushState = topology.GitPushConfigured
		m.RemoteURL = input.RemoteURL
		return nil
	}); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Write service meta %q: %v", input.Service, err),
			"All platform-side side effects (env write, origin sync) succeeded but local meta write failed. Re-run git-push-setup to re-stamp; the probe is idempotent (token already verified).",
		), WithRecoveryStatus()), nil, nil
	}
	meta.GitPushState = topology.GitPushConfigured // mirror onto local copy for the response below
	meta.RemoteURL = input.RemoteURL

	return jsonResult(attachWorkSessionState(
		gitPushContainerConfiguredResponse(input, meta, rotation, reconstructed, reconstructDivergence,
			identity, emailSeeded, nameSeeded, emailPreserved, namePreserved, identityWarning),
		stateDir)), nil, nil
}

// gitIdentitySeedKeyOutcome classifies what BuildGitIdentitySeedCommand
// actually did for ONE key (email or name), parsed strictly from its
// dispatch line — never inferred from absence (Codex diff-review finding
// 2: the earlier Contains-based parse treated a MISSING seeded-token —
// which a write failure produces — as if it meant "preserved").
type gitIdentitySeedKeyOutcome int

const (
	seedKeyUnrecognized gitIdentitySeedKeyOutcome = iota
	seedKeySeeded
	seedKeyPreserved
	seedKeyWriteFailed
)

func (o gitIdentitySeedKeyOutcome) describe() string {
	switch o {
	case seedKeySeeded:
		return "seeded"
	case seedKeyPreserved:
		return "preserved"
	case seedKeyWriteFailed:
		return "write failed"
	case seedKeyUnrecognized:
		return "unrecognized response"
	default:
		return "unrecognized response"
	}
}

// classifyGitIdentitySeedLine matches line EXACTLY (after trimming
// surrounding whitespace) against the three tokens a given key can emit —
// no substring search, so a token embedded inside unrelated output (or a
// duplicated/garbled line) is correctly classified as unrecognized rather
// than accidentally matching.
func classifyGitIdentitySeedLine(line, seededTok, preservedTok, writeFailedTok string) gitIdentitySeedKeyOutcome {
	switch strings.TrimSpace(line) {
	case seededTok:
		return seedKeySeeded
	case preservedTok:
		return seedKeyPreserved
	case writeFailedTok:
		return seedKeyWriteFailed
	default:
		return seedKeyUnrecognized
	}
}

// gitPushSetupDeriveAndSeedIdentity implements F3 human attribution:
// derives a git identity from the GitHub PAT (github.com remotes only —
// ops.IsGitHubRemote is the single owner, a strict fail-CLOSED host check
// deliberately distinct from ops.ParseGitHost's fail-open credential-
// scoping default; other hosts skip derivation and keep the robot
// fallback), then — unless reconstruction is about to run,
// which fills identity itself as part of its own init (there is no .git
// yet to seed into here) — seeds it into the already-present repo via the
// single-owner seed-if-absent-or-exactly-robot command. Every failure mode
// (non-github host, nil httpClient, GitHub API error, SSH failure, a
// per-key write failure, malformed dispatch output) is non-blocking: the
// caller always gets back a valid identity (the derived one, or
// ops.DeployGitIdentity as fallback) plus a human-readable warning to
// surface, never a hard error. A genuinely custom pre-existing identity is
// preserved, never overwritten — the caller surfaces that as a distinct,
// non-silent note.
//
// email/name seeded/preserved are reported INDEPENDENTLY per key — a
// mixed result (e.g. email got seeded because it was absent, while name
// stayed a genuine custom value) is never collapsed into a single
// misleading flag (Codex diff-review finding 2). Any anomaly (a
// write-failure token, an unrecognized/missing/duplicated line) discards
// ALL seeded/preserved claims for this call and reports a warning
// instead — a partially-uncertain outcome is never dressed up as a clean
// preserve.
//
// Extracted from confirmGitPushSetupContainer to keep that
// probe-orchestration function under the maintainability ceiling.
func gitPushSetupDeriveAndSeedIdentity(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	pushHost, remoteURL, token string,
	needsReconstruct bool,
) (identity ops.GitIdentity, emailSeeded, nameSeeded, emailPreserved, namePreserved bool, warning string) {
	identity = ops.DeployGitIdentity
	if !ops.IsGitHubRemote(remoteURL) {
		return identity, false, false, false, false, ""
	}

	derived, deriveErr := ops.DeriveGitHubIdentity(ctx, httpClient, token)
	if deriveErr != nil {
		return identity, false, false, false, false, fmt.Sprintf("Could not derive your GitHub identity for commit attribution (%v) — repo-local identity falls back to the ZCP default; re-run git-push-setup later to retry.", deriveErr)
	}
	identity = derived

	if needsReconstruct {
		return identity, false, false, false, false, ""
	}

	seedOut, seedErr := sshDeployer.ExecSSH(ctx, pushHost, ops.BuildGitIdentitySeedCommand("/var/www", identity))
	if seedErr != nil {
		return identity, false, false, false, false, fmt.Sprintf("Derived your GitHub identity (%s <%s>) but could not seed it into the container's git config (%v) — repo-local identity is unchanged; re-run git-push-setup to retry.", identity.Name, identity.Email, seedErr)
	}

	// Positional parse — the command's stdout is ALWAYS exactly two lines
	// (email token, then name token; BuildGitIdentitySeedCommand's doc
	// comment carries the full guarantee). Anything else is an anomaly.
	lines := strings.Split(strings.TrimRight(string(seedOut), "\n"), "\n")
	if len(lines) != 2 {
		return identity, false, false, false, false, fmt.Sprintf("Derived your GitHub identity (%s <%s>) but the seed command produced unexpected output — repo-local identity state is uncertain; verify manually or re-run git-push-setup.", identity.Name, identity.Email)
	}
	emailOutcome := classifyGitIdentitySeedLine(lines[0], ops.GitIdentitySeedEmailSeeded, ops.GitIdentitySeedEmailPreserved, ops.GitIdentitySeedEmailWriteFailed)
	nameOutcome := classifyGitIdentitySeedLine(lines[1], ops.GitIdentitySeedNameSeeded, ops.GitIdentitySeedNamePreserved, ops.GitIdentitySeedNameWriteFailed)

	if emailOutcome == seedKeyUnrecognized || nameOutcome == seedKeyUnrecognized ||
		emailOutcome == seedKeyWriteFailed || nameOutcome == seedKeyWriteFailed {
		return identity, false, false, false, false, fmt.Sprintf(
			"Derived your GitHub identity (%s <%s>) but seeding it reported an unexpected result (email: %s, name: %s) — repo-local identity may be partially updated; verify manually or re-run git-push-setup.",
			identity.Name, identity.Email, emailOutcome.describe(), nameOutcome.describe(),
		)
	}

	return identity, emailOutcome == seedKeySeeded, nameOutcome == seedKeySeeded,
		emailOutcome == seedKeyPreserved, nameOutcome == seedKeyPreserved, ""
}

// gitPushSetupPreProbeSelfHeal runs the presence check (for configured
// pairs) BEFORE any self-heal, then — unless reconstruction is about to
// run — self-heals the local repo (init-if-missing, identity filled if
// absent, HEAD guaranteed) so the write-auth probe that follows is the
// real `push --dry-run` proof. Extracted from confirmGitPushSetupContainer
// to keep that probe-orchestration function under the maintainability
// ceiling.
//
// Ordering is load-bearing (Codex diff-review finding 1): presence MUST be
// read before the self-heal runs. The self-heal's init guard would
// otherwise satisfy `test -d .git` on its own, and BuildGitReconstructCommand's
// own `test ! -d .git` guard would then no-op post-heal — stranding a
// configured pair's vanished repo on the ZCP marker commit instead of the
// recorded remote's real history, while setup still reports success.
// Skipping the self-heal when reconstruction is pending trades a weaker
// (read-only ls-remote) probe proof for that one case; reconstruction
// (step 6d, run later by the caller) fully re-establishes the repo
// regardless of which probe branch ran.
//
// Returns needsReconstruct (the caller uses it to skip origin sync too,
// and to drive step 6d) and a non-nil blocker only when the self-heal SSH
// call itself fails transport-wise.
func gitPushSetupPreProbeSelfHeal(ctx context.Context, sshDeployer ops.SSHDeployer, pushHost string, meta *workflow.ServiceMeta) (needsReconstruct bool, blocker *mcp.CallToolResult) {
	if meta.GitPushState == topology.GitPushConfigured {
		if presentOut, presentErr := sshDeployer.ExecSSH(ctx, pushHost, "test -d /var/www/.git && echo present || echo absent"); presentErr == nil {
			needsReconstruct = strings.Contains(string(presentOut), "absent")
		}
	}

	if !needsReconstruct {
		// This step mutates ONLY the local repo (init-if-missing, identity
		// filled if absent, an empty HEAD-guarantee marker commit if HEAD
		// was unborn) — never PROJECT state (no remote ref, no secret, no
		// origin sync, no meta write). That local repair is unconditional
		// and best-effort; it happens even when the probe that follows
		// subsequently fails.
		if _, ensureErr := sshDeployer.ExecSSH(ctx, pushHost, ops.GitEnsureRepoHeadCommand("/var/www")); ensureErr != nil {
			return needsReconstruct, convertError(platform.NewPlatformError(
				platform.ErrSSHDeployFailed,
				withSSHStderr(fmt.Sprintf("git-push-setup: could not ensure a commit-ready repo on %q before probing", pushHost), ensureErr),
				"Verify /var/www is writable and SSH is healthy on the push source, then re-call. NO remote ref, secret, origin, or meta state was modified.",
			), WithRecoveryStatus())
		}
	}

	return needsReconstruct, nil
}

// gitPushContainerConfiguredResponse builds the container-mode "configured"
// response body, including the delivery recommendation (single owner) and the
// rotation / reconstruction / identity-attribution (F3) annotations. Extracted
// from confirmGitPushSetupContainer to keep that probe-orchestration function
// under the maintainability ceiling.
func gitPushContainerConfiguredResponse(
	input WorkflowInput, meta *workflow.ServiceMeta, rotation, reconstructed bool, reconstructDivergence string,
	identity ops.GitIdentity, emailSeeded, nameSeeded, emailPreserved, namePreserved bool, identityWarning string,
) map[string]any {
	delivery := deliveryDecisionForMeta(meta)
	resp := map[string]any{
		"status":                    "configured",
		"service":                   input.Service,
		"gitPushState":              meta.GitPushState,
		"remoteUrl":                 meta.RemoteURL,
		"recommendedIntegration":    string(delivery.Recommended),
		"recommendedIntegrationWhy": delivery.Why,
		"nextStep":                  fmt.Sprintf("git-push wiring verified: the token passed the push auth probe (`git push --dry-run`; on a repo with no commit yet it falls back to read reachability and the first push proves write), origin + credential helper synced on /var/www/.git, and a FRESH session authenticated with the stored secret (rotation needs no restart — fresh sessions read the live value). A non-fast-forward (the remote branch has commits yours doesn't) still surfaces at the first real push, not here. Wire CI (integration=\"actions\" recommended for GitHub; \"webhook\" for GitLab; \"none\" for external CI/CD): zerops_workflow action=\"build-integration\" service=%q integration=\"actions|webhook|none\". Then push via: zerops_deploy targetService=%q strategy=\"git-push\".", input.Service, input.Service),
	}
	if rotation {
		resp["rotated"] = true
		resp["note"] = "Credential ROTATED for the existing remote: the new token was probe-verified, replaced the service-scope secret, and a fresh session authenticated with it. No restart was needed."
	}
	if reconstructed {
		resp["reconstructed"] = true
		if reconstructDivergence != "" {
			resp["divergence"] = "After reconstruction the working tree differs from the remote HEAD:\n" + reconstructDivergence + "\nReview: build artifacts are expected noise; real edits need commit + zerops_deploy strategy=\"git-push\"."
		}
	}
	// F3 human attribution — never silent, and never collapsed: email and
	// name are reported per-key (Codex diff-review finding 2), so a mixed
	// outcome (e.g. email seeded because it was absent, name left as a
	// genuine custom value) surfaces BOTH an attribution and a preserved
	// note, each naming exactly which key it covers, instead of folding
	// into one misleading flag.
	if emailSeeded || nameSeeded || (reconstructed && identity != ops.DeployGitIdentity) {
		resp["identityAttributed"] = fmt.Sprintf("%s <%s>%s", identity.Name, identity.Email,
			gitPushIdentityAttributedSuffix(emailSeeded, nameSeeded, reconstructed))
	}
	if emailPreserved || namePreserved {
		resp["identityPreservedNote"] = gitPushIdentityPreservedNote(identity, meta.Hostname, emailPreserved, namePreserved)
	}
	if identityWarning != "" {
		resp["identityWarning"] = identityWarning
	}
	return resp
}

// gitPushIdentityAttributedSuffix names which key(s) actually received the
// derived identity when the seed result was MIXED (one key seeded, the
// other left as a genuine custom value) — a mixed result must be reported
// per-key, never collapsed into an unqualified "identity attributed"
// claim (Codex diff-review finding 2).
func gitPushIdentityAttributedSuffix(emailSeeded, nameSeeded, reconstructed bool) string {
	if reconstructed || (emailSeeded && nameSeeded) || (!emailSeeded && !nameSeeded) {
		return ""
	}
	if emailSeeded {
		return " (email only — user.name was left as your existing custom value)"
	}
	return " (name only — user.email was left as your existing custom value)"
}

// gitPushIdentityPreservedNote builds the "identity preserved" response
// note, naming exactly which key(s) were preserved (never collapsing a
// mixed result — Codex diff-review finding 2) and giving a copy-pasteable
// recovery command. Each interpolated identity value is shell-quoted
// individually via ops.ShellQuote, and the assembled remote script is then
// quoted as ONE argument for the emitted `ssh host '<script>'` line
// (Codex diff-review finding 3): the earlier hand-built `'%s'`
// interpolation would break — or splice extra shell syntax — on a derived
// name/email containing an apostrophe or shell metacharacters, since
// GitHub account names/emails are attacker-adjacent input (the PAT's
// owner controls their own GitHub profile, not ZCP).
func gitPushIdentityPreservedNote(identity ops.GitIdentity, hostname string, emailPreserved, namePreserved bool) string {
	which := "identity"
	switch {
	case emailPreserved && namePreserved:
		which = "identity (both name and email)"
	case emailPreserved:
		which = "email"
	case namePreserved:
		which = "name"
	}
	remoteScript := fmt.Sprintf("cd /var/www && git config user.name %s && git config user.email %s",
		ops.ShellQuote(identity.Name), ops.ShellQuote(identity.Email))
	return fmt.Sprintf(
		"Repo-local git %s differs from your GitHub account (%s <%s>) — preserved, not overwritten. To attribute future commits to your GitHub account instead, set it yourself: ssh %s %s.",
		which, identity.Name, identity.Email, hostname, ops.ShellQuote(remoteScript),
	)
}

// gitPushPorcelainSummary best-effort reads `git status --porcelain` on
// the push source after a reconstruction, truncated for the response.
// Empty string = clean tree (or unreadable — the gate re-checks later).
func gitPushPorcelainSummary(ctx context.Context, sshDeployer ops.SSHDeployer, pushHost string) string {
	out, err := sshDeployer.ExecSSH(ctx, pushHost, "cd /var/www && git status --porcelain 2>/dev/null | head -20")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitPushSessionAuthVerify is the step-4c loop (the XCUT-2 successor):
// retries the session-env auth probe across the ~5-10s zembed propagation
// window so `configured` is stamped only after a FRESH session
// authenticated with the just-written secret end-to-end.
func gitPushSessionAuthVerify(ctx context.Context, sshDeployer ops.SSHDeployer, pushHost, remoteURL string) error {
	sessionCmd := ops.BuildGitSessionAuthProbeCommand(remoteURL)
	var sessionErr error
	for attempt := 0; attempt < gitPushSessionAuthAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				sessionErr = ctx.Err()
			case <-time.After(gitPushSessionAuthDelay):
			}
			if ctx.Err() != nil {
				break
			}
		}
		if _, sessionErr = sshDeployer.ExecSSH(ctx, pushHost, sessionCmd); sessionErr == nil {
			return nil
		}
	}
	return sessionErr
}

// gitPushReconstruct runs the non-destructive repo rebuild (step 6d /
// configured-recall) and returns the post-rebuild porcelain divergence
// summary ("" = clean). identity is the identity to fill (set-if-absent)
// during the reconstruction's init — a GitHub-derived identity when the
// caller has one (F3), or ops.DeployGitIdentity as the robot fallback.
func gitPushReconstruct(ctx context.Context, sshDeployer ops.SSHDeployer, pushHost, remoteURL string, identity ops.GitIdentity) (string, error) {
	reconCmd := ops.BuildGitReconstructCommand("/var/www", remoteURL, identity)
	if _, reconErr := sshDeployer.ExecSSH(ctx, pushHost, reconCmd); reconErr != nil {
		return "", reconErr
	}
	return gitPushPorcelainSummary(ctx, sshDeployer, pushHost), nil
}

// gitPushConfiguredRecall handles the token-less confirm re-call on an
// already-configured pair (O3 check-before-mutate, B6c/GPS-5).
// Check-before-claim: "already-configured" promises working wiring, so
// the branch first verifies /var/www/.git exists. A container whose repo
// vanished (artifact deploy without -g; container replacement — the
// gate's git-state-missing state) still has its service secret, so the
// token-less re-call becomes the RECONSTRUCTION path: rebuild the repo
// from the recorded remote (non-destructive — mixed reset never touches
// the working tree). No gitToken is available on this path (that's what
// makes it "token-less"), so reconstruction always falls back to the
// robot identity — deriving from GitHub needs a PAT (F3 item 4).
func gitPushConfiguredRecall(ctx context.Context, sshDeployer ops.SSHDeployer, input WorkflowInput, meta *workflow.ServiceMeta) *mcp.CallToolResult {
	presentOut, presentErr := sshDeployer.ExecSSH(ctx, meta.Hostname, "test -d /var/www/.git && echo present || echo absent")
	if presentErr == nil && strings.Contains(string(presentOut), "absent") {
		divergence, reconErr := gitPushReconstruct(ctx, sshDeployer, meta.Hostname, meta.RemoteURL, ops.DeployGitIdentity)
		if reconErr != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrSSHDeployFailed,
				withSSHStderr(fmt.Sprintf("git-push-setup: /var/www/.git missing on %q and reconstruction from %s failed", meta.Hostname, topology.RedactRepoURLCredentials(meta.RemoteURL)), reconErr),
				"The repo could not be rebuilt from the recorded remote (auth or network). Verify the service env still carries GIT_TOKEN (re-call with gitToken to rotate it), then re-call.",
			), WithRecoveryStatus())
		}
		resp := map[string]any{
			"status":        "configured",
			"reconstructed": true,
			"service":       input.Service,
			"pushSource":    meta.Hostname,
			"remoteUrl":     topology.RedactRepoURLCredentials(meta.RemoteURL),
			"gitPushState":  meta.GitPushState,
			"note":          "/var/www/.git was missing (a deploy replaced the container with a git-less artifact) and has been RECONSTRUCTED from the recorded remote: init + fetch + mixed reset — the working tree was not modified.",
		}
		if divergence != "" {
			resp["divergence"] = "After reconstruction the working tree differs from the remote HEAD:\n" + divergence + "\nReview: build artifacts are expected noise; real edits need commit + zerops_deploy strategy=\"git-push\"."
		}
		return jsonResult(resp)
	}
	resp := map[string]any{
		"status":       "already-configured",
		"service":      input.Service,
		"pushSource":   meta.Hostname,
		"remoteUrl":    topology.RedactRepoURLCredentials(meta.RemoteURL),
		"gitPushState": meta.GitPushState,
		"note":         "git-push is already configured for this remote — no probe or env write performed. Pass a different remoteUrl to change the remote, or pass gitToken to ROTATE the credential for this remote (probe-first: the new token is verified before it replaces the stored secret).",
	}
	if note := gitPushIdentityMigrationNote(ctx, sshDeployer, meta); note != "" {
		resp["identityMigrationNote"] = note
	}
	return jsonResult(resp)
}

// gitPushIdentityMigrationNote reads the push source's current git
// identity (read-only, no mutation) and, when it EXACTLY equals the robot
// identity, returns a note prompting a one-time re-run with gitToken to
// migrate attribution (F3 item 4). A token-less recall cannot derive
// anything itself — it can only detect the still-robot state and point at
// the fix; it never fabricates an identity. Gated to github.com remotes
// (ops.IsGitHubRemote — the same strict fail-closed check the derivation
// gate uses, not ops.ParseGitHost's fail-open credential-scoping default):
// F3 only derives identity from GitHub, so prompting the same re-run for a
// GitLab/other/malformed remote would be a false promise. Read failure
// (SSH down, malformed output) is silent — this is advisory, never a
// blocker.
func gitPushIdentityMigrationNote(ctx context.Context, sshDeployer ops.SSHDeployer, meta *workflow.ServiceMeta) string {
	if !ops.IsGitHubRemote(meta.RemoteURL) {
		return ""
	}
	out, err := sshDeployer.ExecSSH(ctx, meta.Hostname, ops.BuildGitIdentityReadCommand("/var/www"))
	if err != nil {
		return ""
	}
	// TrimSuffix removes exactly the ONE trailing newline the read
	// command's second printf always emits — TrimRight would strip ALL
	// trailing newlines and, when BOTH values happen to be empty (a
	// totally unconfigured identity), collapse the guaranteed-2-lines
	// output to fewer elements.
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) < 2 {
		return ""
	}
	email, name := strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
	if email != ops.DeployGitIdentity.Email || name != ops.DeployGitIdentity.Name {
		return ""
	}
	return "Repo-local git identity is still the ZCP default (Zerops Agent <agent@zerops.io>) — to attribute future commits to your GitHub account, re-run git-push-setup once with gitToken=<PAT> (no token means ZCP cannot derive your identity; it never fabricates one)."
}

// gitPushRemoteURLDescription returns env-aware help text for the
// remoteUrl input. Container mode rejects scp-form SSH (PAT auth only
// works for HTTPS); local mode allows any URL git itself accepts.
func gitPushRemoteURLDescription(rt runtime.Info) string {
	if rt.InContainer {
		return "HTTPS URL of the target repository (https://github.com/<owner>/<repo>). Container mode authenticates via PAT (session-env credential helper), which requires HTTPS. SSH form (git@github.com:owner/repo) is rejected — use the HTTPS clone URL."
	}
	return "HTTPS or SSH URL of the target repository (https://github.com/<owner>/<repo> or git@github.com:<owner>/<repo>). Local mode uses your local git credential helper — whatever URL form works with `git ls-remote` on your machine works here."
}

// gitPushWalkthroughPrompt returns env-aware user-facing text for the
// walkthrough response. Container mode lists three inputs (URL + token +
// integration); local mode lists two (URL + integration).
func gitPushWalkthroughPrompt(rt runtime.Info, service string) string {
	if rt.InContainer {
		return "Three inputs needed to wire git-push for " + service + ": (1) HTTPS remote repo URL, (2) fine-grained PAT, (3) CI integration. The setup call probes the token against the remote BEFORE writing project state — failed probe leaves project state untouched. Actions is the default for GitHub repos."
	}
	return "Two inputs needed to wire git-push for " + service + ": (1) remote repo URL, (2) CI integration. Local mode uses your existing git credentials (SSH agent, OS credential manager) — no token collection. The setup call probes the remote before stamping configured. Actions is the default for GitHub repos."
}

// gitPushWalkthroughNextStep returns env-aware next-step guidance.
// Container mode tells the agent that GIT_TOKEN is collected on the
// confirm call (handler writes it); local mode skips the token step.
func gitPushWalkthroughNextStep(rt runtime.Info, service string) string {
	if rt.InContainer {
		return fmt.Sprintf("After collecting inputs: 1) confirm capability with all three values: zerops_workflow action=\"git-push-setup\" service=%q remoteUrl=<url> gitToken=<PAT>. Handler probes auth, writes GIT_TOKEN as a service-scope secret on the push source, stamps configured (no restart — a fresh session reads the live secret). 2) wire CI: zerops_workflow action=\"build-integration\" service=%q integration=\"actions|webhook|none\".", service, service)
	}
	return fmt.Sprintf("After collecting inputs: 1) confirm capability: zerops_workflow action=\"git-push-setup\" service=%q remoteUrl=<url>. Handler probes the remote using your local git credentials, syncs origin, stamps configured. 2) wire CI: zerops_workflow action=\"build-integration\" service=%q integration=\"actions|webhook|none\".", service, service)
}

// gitPushWalkthroughStep is one entry in the walkthrough's structured
// steps[] response field — gives the agent an enumerable, machine-
// readable view of the call sequence alongside the prose `prompt` +
// `nextStep` strings. Plan §P7 F2.
type gitPushWalkthroughStep struct {
	N     int    `json:"n"`     // 1-indexed step number
	Title string `json:"title"` // one-line label
	Call  string `json:"call"`  // exact MCP call template the agent makes
}

// gitPushWalkthroughSteps returns the env-aware structured step
// sequence. Container mode has 3 steps (collect inputs → probe-confirm
// with token → wire CI); local mode has 2 (collect inputs → probe-
// confirm → wire CI, no token step). Mirrors the prose in
// gitPushWalkthroughNextStep but in agent-machine-readable form so
// downstream tools can render a progress checklist without re-parsing
// human language.
func gitPushWalkthroughSteps(rt runtime.Info, service string) []gitPushWalkthroughStep {
	if rt.InContainer {
		return []gitPushWalkthroughStep{
			{
				N:     1,
				Title: "Collect inputs from user",
				Call:  "<no MCP call> — gather remoteUrl + gitToken (fine-grained PAT) + integration choice from the user",
			},
			{
				N:     2,
				Title: "Probe-confirm + write capability",
				Call:  fmt.Sprintf("zerops_workflow action=\"git-push-setup\" service=%q remoteUrl=<url> gitToken=<PAT>", service),
			},
			{
				N:     3,
				Title: "Wire CI integration",
				Call:  fmt.Sprintf("zerops_workflow action=\"build-integration\" service=%q integration=\"actions|webhook|none\"", service),
			},
		}
	}
	return []gitPushWalkthroughStep{
		{
			N:     1,
			Title: "Collect inputs from user",
			Call:  "<no MCP call> — gather remoteUrl + integration choice from the user (local git credentials handle auth)",
		},
		{
			N:     2,
			Title: "Probe-confirm + wire CI",
			Call:  fmt.Sprintf("zerops_workflow action=\"git-push-setup\" service=%q remoteUrl=<url>, then zerops_workflow action=\"build-integration\" service=%q integration=\"actions|webhook|none\"", service, service),
		},
	}
}

// deliveryDecisionForMeta builds the topology delivery-recommendation inputs
// from a ServiceMeta and returns the decision. Single adapter so every
// git-push-setup / build-integration site recommends from the SAME owner
// (topology.RecommendDelivery) keyed on the same meta the launch earn-probe
// reads — the host-only `gitlab→webhook else actions` heuristic this replaced
// drifted from the full git-push × build-integration × stage matrix.
func deliveryDecisionForMeta(meta *workflow.ServiceMeta) topology.DeliveryDecision {
	return topology.RecommendDelivery(topology.DeliveryInputs{
		GitPushState:     meta.GitPushState,
		BuildIntegration: meta.BuildIntegration,
		Verified:         meta.BuildIntegrationVerifiedAt != "",
		HasStage:         meta.StageHostname != "",
		RemoteURL:        meta.RemoteURL,
	})
}
