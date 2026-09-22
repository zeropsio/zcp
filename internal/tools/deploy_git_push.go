package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// gitPushErrorDetail extracts the most actionable text from a failed git
// push. The real reason (non-fast-forward, auth rejected, repo missing)
// lives in the command's combined output — which the SSH deployer captures
// in SSHExecError.Output — NOT in err.Error() ("ssh <host>: exit status 1").
// Reads the wrapped SSHExecError.Output first (authoritative), falls back to
// the returned output bytes, then to the raw error.
func gitPushErrorDetail(err error, output []byte) string {
	var sshErr *platform.SSHExecError
	if errors.As(err, &sshErr) {
		if d := truncateStderr(sshErr.Output); d != "" {
			return d
		}
	}
	if d := truncateStderr(string(output)); d != "" {
		return d
	}
	return err.Error()
}

// sshGitRunner adapts an ops.SSHDeployer into an ops.GitRunner: each call
// runs one `git <args...>` step over SSH at workingDir, authenticating the
// `fetch` step via the session-env credential helper (ops.
// BuildGitDivergenceStepCommand decides which). Used to probe remote
// divergence (ops.ProbeGitDivergence) after a GIT_PUSH_NON_FAST_FORWARD
// rejection — never to push or mutate anything.
func sshGitRunner(ctx context.Context, sshDeployer ops.SSHDeployer, hostname, workingDir string) ops.GitRunner {
	return func(args ...string) (string, error) {
		cmd := ops.BuildGitDivergenceStepCommand(workingDir, args)
		out, err := sshDeployer.ExecSSH(ctx, hostname, cmd)
		if err != nil {
			return string(out), fmt.Errorf("%s", gitPushErrorDetail(err, out))
		}
		return string(out), nil
	}
}

// fetchZeropsYamlOverSSH reads zerops.yaml (or zerops.yml fallback) from
// the target container via SSH `cat`. Returns ("", nil) when the file is
// absent so callers can treat "no yaml" the same way the filesystem path
// does (skip validation). A read error is returned for transport failures
// so the caller can log; validation itself falls back to the server which
// will error just the same if the YAML were malformed.
func fetchZeropsYamlOverSSH(ctx context.Context, sshDeployer ops.SSHDeployer, hostname, workingDir string) (string, error) {
	if sshDeployer == nil {
		return "", nil
	}
	// Try zerops.yaml then zerops.yml; 2>/dev/null + trailing echo lets us
	// distinguish "file missing" (nothing in stdout) from "read failed"
	// (SSH error) without special-casing exit codes.
	qwd := ops.ShellQuote(workingDir)
	cmd := fmt.Sprintf(
		`cat %s/zerops.yaml 2>/dev/null || cat %s/zerops.yml 2>/dev/null || true`,
		qwd, qwd,
	)
	out, err := sshDeployer.ExecSSH(ctx, hostname, cmd)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// gitPushPrerequisites is a structured response when GIT_TOKEN is missing.
// Guides the agent through the decision question and setup steps.
type gitPushPrerequisites struct {
	Status       string `json:"status"`
	Message      string `json:"message"`
	Instructions string `json:"instructions"`
}

// gitPushMetaPreflight runs the source-of-push + GitPushState validation
// shared by handleGitPush (container) and handleLocalGitPush (local).
// Returns a CallToolResult ready to return verbatim from the caller, or
// nil when checks pass / no meta exists (legacy services without metas
// pass through). recordAttempt is invoked exactly once on the failure
// branch so each caller stays the canonical site of attempt persistence.
//
// FindServiceMeta honors the pair-keyed invariant: a stage-hostname
// targetService resolves to the dev-keyed meta — and PushSourceCheckFor
// classifies why a target may be invalid (stage half / mode unsupported /
// unknown host) so the rejection message is reason-specific rather than
// the generic "build target half" wording that misled users on standalone
// ModeDev services where target == meta.Hostname.
//
// Introduced by deploy-decomp P4 (handler validation phase).
func gitPushMetaPreflight(
	stateDir, targetService string,
	recordAttempt func(string, topology.FailureClass),
) *mcp.CallToolResult {
	meta, _ := workflow.FindServiceMeta(stateDir, targetService)
	if meta == nil {
		return nil
	}
	switch meta.PushSourceCheckFor(targetService) {
	case topology.PushSourceOK:
		// proceed to GitPushState check below
	case topology.PushSourceIsStageHalf:
		recordAttempt("targetService is the stage half of a pair", topology.FailureClassConfig)
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("git-push target %q is the stage half of a pair (build target, never push source); push from the dev half %q instead", targetService, meta.Hostname),
			fmt.Sprintf("Retry with: zerops_deploy targetService=%q strategy=\"git-push\"", meta.Hostname),
		), WithRecoveryStatus())
	case topology.PushSourceModeUnsupported:
		recordAttempt(fmt.Sprintf("targetService mode %q does not support push-git", meta.Mode), topology.FailureClassConfig)
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("git-push target %q is in mode %q which does not support push-git (only Standard/Simple/LocalStage/LocalOnly do)", targetService, meta.Mode),
			"Mode expansion (ModeDev → ModeStandard adds a stage half) is a bootstrap-with-isExisting flow, not a workflow action. Re-run bootstrap with route=adopt and a plan target that carries isExisting=true + bootstrapMode=\"standard\" + an explicit stageHostname. See develop-mode-expansion atom for the plan shape.",
		), WithRecoveryStatus())
	case topology.PushSourceUnknownHost:
		recordAttempt("targetService not part of meta scope", topology.FailureClassConfig)
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("git-push target %q is not part of meta scope keyed at %q", targetService, meta.Hostname),
			"The meta lookup matched a different service. Verify the hostname or re-run bootstrap on the right pair.",
		), WithRecoveryStatus())
	default:
		// Defensive: future PushSourceResult variants must be classified
		// explicitly. Falling through silently as if OK would let a new
		// rejection case slip past validation.
		recordAttempt("internal classifier returned an unexpected PushSourceResult", topology.FailureClassConfig)
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("internal classifier returned an unexpected PushSourceResult for target %q — please file a bug", targetService),
			"Run zerops_workflow action=\"status\" to recover and report the issue.",
		), WithRecoveryStatus())
	}
	if meta.GitPushState != topology.GitPushConfigured {
		recordAttempt(fmt.Sprintf("git-push not configured (state=%s)", meta.GitPushState), topology.FailureClassConfig)
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("git-push not configured for service %q (current state: %s)", targetService, meta.GitPushState),
			fmt.Sprintf("Run zerops_workflow action=\"git-push-setup\" service=%q first to set up the GIT_TOKEN service secret, credential helper, and remote URL.", targetService),
		), WithRecoveryStatus())
	}
	return nil
}

const gitTokenCheckCmd = `test -n "$GIT_TOKEN" && echo 1 || echo 0`

// defaultTrackedRef is the GF-7 "main" fallback (docs/spec-workflows.md
// §12.6) — the single literal every reader / writer of ServiceMeta.
// TrackedRef falls back to when nothing else resolves one. One constant so
// the fallback stays byte-identical everywhere it's used.
const defaultTrackedRef = "main"

// statusNothingToPush is the GitPushResult.Status when `git push` finds the
// remote already at HEAD ("Everything up-to-date"). Single owner so the
// container + local git-push paths and the build-watch skip agree.
const statusNothingToPush = "NOTHING_TO_PUSH"

// degradeGitPushStateToBroken flips meta.GitPushState from configured to
// broken in response to a credential failure observed during git push.
// Best-effort: meta read/write errors are silently ignored — the deploy
// has already failed and we're augmenting the recovery story, not
// gating on disk I/O. Only flips state if currently `configured` (does
// not touch unconfigured / unknown / broken states).
func degradeGitPushStateToBroken(stateDir, targetService string) {
	meta, _ := workflow.FindServiceMeta(stateDir, targetService)
	if meta == nil || meta.GitPushState != topology.GitPushConfigured {
		return
	}
	// Locked RMW: flip only GitPushState on the fresh meta so a concurrent
	// orthogonal-field update on the same pair isn't lost (XCUT-1). Best-effort.
	_ = workflow.UpdateServiceMeta(stateDir, targetService, func(m *workflow.ServiceMeta) error {
		m.GitPushState = topology.GitPushBroken
		return nil
	})
}

// resolveEffectiveRemote picks the URL the deploy handler should push to:
// the explicit input arg wins (lets the agent override on a one-off push),
// falling back to the remote stamped in meta during
// action="git-push-setup" so atoms can honestly say "remoteUrl is optional
// after setup". Without this fallback, BuildGitPushCommand would receive
// an empty remote and the push would either default to working-tree
// origin (wrong) or fail.
//
// Shared by handleGitPush (container) and handleLocalGitPush (local) so
// both halves of the deploy stack honor the same atom claim.
func resolveEffectiveRemote(stateDir, targetService, inputRemote string) string {
	if inputRemote != "" {
		return inputRemote
	}
	meta, _ := workflow.FindServiceMeta(stateDir, targetService)
	if meta == nil {
		return ""
	}
	return meta.RemoteURL
}

// gitPushBuildIntegrationConfigured reports whether targetService has a
// ZCP-managed CI integration wired (webhook or actions) — the only shape
// that gives a successful git-push an async resolver (the build watch, or
// its manual record-deploy fallback). Unset/none means nothing will ever
// rebuild the target from this push.
func gitPushBuildIntegrationConfigured(stateDir, targetService string) bool {
	meta, _ := workflow.FindServiceMeta(stateDir, targetService)
	if meta == nil {
		return false
	}
	return meta.BuildIntegration == topology.BuildIntegrationWebhook || meta.BuildIntegration == topology.BuildIntegrationActions
}

// trackedRefOrDefault is the single owner of the GF-7 "main" fallback
// (docs/spec-workflows.md §12.6): every reader of ServiceMeta.TrackedRef
// — the git-push default branch, the GitHub Actions template, the launch
// gate's remote-HEAD compare — falls back to "main" identically when meta
// carries none (pre-existing metas written before GF-7, or a recall that
// never ran detection). Never re-detects; git-push-setup is the sole
// writer of TrackedRef.
func trackedRefOrDefault(meta *workflow.ServiceMeta) string {
	if meta != nil && meta.TrackedRef != "" {
		return meta.TrackedRef
	}
	return defaultTrackedRef
}

// resolveTrackedBranch resolves the branch a container-mode git-push
// transmits to: an explicit inputBranch always wins (back-compat with the
// pre-GF-7 `branch` input), else the target's recorded tracked ref (GF-7),
// else the Mate's own Gitea branch, else "main".
//
// The Gitea step sits between TrackedRef and the "main" fallback because on
// the account's own Gitea `main` is protected on every repository and a
// direct push is refused by a pre-receive hook (docs/vocabulary.md,
// "protected branches"): there the branch to push is the Mate's own,
// recorded on the pair when the broker gave it the repository (A5, guide
// 1.5). Falling through to `main` there would make every delivery fail at
// the remote, and the failure reads like a credential fault — the one
// diagnosis that leads an agent to rotate a perfectly good token.
func resolveTrackedBranch(stateDir, targetService, inputBranch string) string {
	meta, _ := workflow.FindServiceMeta(stateDir, targetService)
	return notTheProtectedBase(meta, resolveAskedBranch(meta, inputBranch))
}

// resolveAskedBranch is what was asked for, before the protected base is ruled
// out: the caller's branch, the recorded tracked ref (GF-7), the Mate's own
// branch, then `main`.
func resolveAskedBranch(meta *workflow.ServiceMeta, inputBranch string) string {
	switch {
	case inputBranch != "":
		return inputBranch
	case meta != nil && meta.TrackedRef != "":
		return meta.TrackedRef
	case meta != nil && meta.Gitea != nil && meta.Gitea.Branch != "":
		return meta.Gitea.Branch
	}
	return defaultTrackedRef
}

// notTheProtectedBase keeps a wired pair off the branch it may never push to.
//
// Every repository on the account's Gitea protects its default branch, and a
// Mate lands on it through a pull request — but a pair wired before it had a
// branch kept `main` as its tracked ref, and nothing since replaced it. Every
// git-push deploy of such a pair therefore aimed at `main`, which Gitea
// rejects as non-fast-forward the moment anybody merges, leaving the agent to
// find its own way around (the owner, 2026-09-18: "it keeps running into this
// as well"). Asked for the base or defaulted to it, a wired pair pushes to its
// own branch; a pair with no Gitea has no other branch and keeps what it was
// given.
func notTheProtectedBase(meta *workflow.ServiceMeta, branch string) string {
	if meta == nil || meta.Gitea == nil || meta.Gitea.Branch == "" {
		return branch
	}
	base := meta.Gitea.DefaultBranch
	if base == "" {
		base = defaultTrackedRef
	}
	if branch != base {
		return branch
	}
	return meta.Gitea.Branch
}

// gitPushEnvRefPreflight validates the run.envVariables refs of the named
// setup block in yamlContent against live platform state. Returns a
// blocking error response + the failure detail when a ${peer_var} ref is
// invalid, else (nil, ""). Shared by the container (handleGitPush) and
// local (handleLocalGitPush) git-push paths — DEPLOY-3: the container path
// ran this check but the local path did not, so a bad peer-var ref got
// actionable feedback over SSH but a delayed, opaque remote build failure
// locally. Parse failures fall through (the yaml schema is validated
// separately; a parse miss here is not a deploy-blocker).
func gitPushEnvRefPreflight(ctx context.Context, client platform.Client, projectID, hostname, setupName, yamlContent string) (*mcp.CallToolResult, string) {
	doc, parseErr := ops.ParseZeropsYmlContent([]byte(yamlContent), "zerops.yaml")
	if parseErr != nil {
		return nil, ""
	}
	entry := doc.FindEntry(setupName)
	if entry == nil || len(entry.Run.EnvVariables) == 0 {
		return nil, ""
	}
	for _, c := range preflightEnvRefs(ctx, client, projectID, hostname, entry) {
		if c.Status == statusFail {
			return convertError(platform.NewPlatformError(
				platform.ErrPreflightFailed,
				"env-var references invalid: "+c.Detail,
				"Fix env-var references in zerops.yaml run.envVariables; ${peer_var} refs must name an existing peer service + env var.",
			), WithRecoveryStatus()), c.Detail
		}
	}
	return nil, ""
}

// committedCodeCheckCmd returns "1" when workingDir contains a git repo
// with at least one commit reachable from HEAD, "0" otherwise. This is the
// real precondition for git-push: the push has to transmit a commit, not a
// platform-level "service deployed" timestamp. Test stub dispatchers
// discriminate this command by its `rev-parse HEAD` shape (and the token
// preflight by `test -n "$GIT_TOKEN"`) — keep those shapes distinct from
// the push command, which carries neither.
func committedCodeCheckCmd(workingDir string) string {
	qwd := ops.ShellQuote(workingDir)
	return fmt.Sprintf(
		`test -d %s/.git && git -C %s rev-parse HEAD >/dev/null 2>&1 && echo 1 || echo 0`,
		qwd, qwd,
	)
}

// gitStatusPorcelainCmd lists uncommitted/untracked changes in workingDir
// (capped so the warning stays readable). committedCodeCheckCmd proves a
// commit EXISTS; this proves whether the tree is CLEAN. git-push transmits
// the committed HEAD only, so any dirty change is silently left behind unless
// surfaced — this feeds dirtyTreeWarning.
func gitStatusPorcelainCmd(workingDir string) string {
	qwd := ops.ShellQuote(workingDir)
	return fmt.Sprintf(`git -C %s status --porcelain 2>/dev/null | head -20`, qwd)
}

// dirtyTreeWarning builds the "uncommitted changes were NOT pushed" warning
// from `git status --porcelain` output. Single owner for both the container
// (handleGitPush) and local (handleLocalGitPush) git-push paths so the
// contract — git-push transmits the committed HEAD only; it never stages or
// commits for you — is stated once and cannot drift. Returns "" for a clean
// tree. commitHint is the path-specific way to commit (SSH vs local).
func dirtyTreeWarning(porcelain, commitHint string) string {
	porcelain = strings.TrimSpace(porcelain)
	if porcelain == "" {
		return ""
	}
	return fmt.Sprintf(
		"Uncommitted or untracked changes were NOT pushed — git-push transmits the committed HEAD only; it does not stage or commit for you. Changes:\n%s\nCommit them first (%s), then re-push.",
		porcelain, commitHint,
	)
}

// gitPushSetupPointerInstructions redirects to the probe-first
// git-push-setup verifier. The full setup flow is synthesized there
// from the atom corpus. After git-push-setup completes, the agent can
// independently wire a build integration via action=build-integration
// (orthogonal dimension).
const gitPushSetupPointerInstructions = `Configure git-push capability via the deploy-config actions:

  zerops_workflow action="git-push-setup" service="%s" remoteUrl="<url>" gitToken="<PAT>"
  # then optionally:
  zerops_workflow action="build-integration" service="%s" integration="webhook|actions"

git-push-setup probes the (remoteUrl, gitToken) pair against the remote
BEFORE writing project state — failed probe leaves state untouched, agent
re-calls with corrected inputs. Local mode (no container) skips gitToken
and uses local git credentials. build-integration wires the ZCP-managed
CI integration shape (workflow YAML / dashboard URL); external CI/CD you
already own continues unchanged. After setup completes, retry the push.`

// handleGitPush executes the git-push strategy: push committed code to an
// external git remote. No Zerops build is triggered directly from our side,
// but the remote's receipt of the push triggers one — so zerops.yaml still
// needs to be valid. Pre-push validation fetches the file from the container
// via SSH cat and calls the Zerops validator; any failure aborts the push.
//
//nolint:maintidx // long but linear: pre-flight chain (meta gate → committed-code → token diagnose → yaml validate → env-ref preflight → push → token-rotation degradation). Extracting any single branch would split the single-place state-mutation policy.
func handleGitPush(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	projectID string,
	sshDeployer ops.SSHDeployer,
	logFetcher platform.LogFetcher,
	onProgress ops.ProgressCallback,
	input DeploySSHInput,
	stateDir string,
	rt runtime.Info,
) (*mcp.CallToolResult, any, error) {
	attempt := workflow.DeployAttempt{
		AttemptedAt: time.Now().UTC().Format(time.RFC3339),
		Setup:       input.Setup,
		Strategy:    deployStrategyGitPush,
	}
	// recordAttempt accepts the error string and a FailureClass so each
	// failure point classifies the recovery shape: pre-flight checks tied
	// to git/network are FailureClassNetwork; missing GIT_TOKEN /
	// committed-code are FailureClassConfig; YAML validation is
	// FailureClassConfig; the actual push failure is FailureClassNetwork
	// (transport-layer failure to reach the remote).
	recordAttempt := func(err string, class topology.FailureClass) {
		attempt.Error = err
		attempt.FailureClass = class
		_ = workflow.RecordDeployAttempt(stateDir, input.TargetService, attempt)
	}

	if sshDeployer == nil {
		recordAttempt("SSH deployer not configured", topology.FailureClassConfig)
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"SSH deployer not configured",
			"git-push requires a running Zerops container with SSH access",
		)), nil, nil
	}
	if input.TargetService == "" {
		recordAttempt("targetService missing", topology.FailureClassConfig)
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"targetService is required for git-push",
			"Provide the hostname of the service to push from",
		)), nil, nil
	}

	// Meta-based source-of-push + setup-state pre-flight (deploy-decomp P4).
	if blocked := gitPushMetaPreflight(stateDir, input.TargetService, recordAttempt); blocked != nil {
		return blocked, nil, nil
	}

	hostname := input.TargetService
	workingDir := input.WorkingDir
	if workingDir == "" {
		workingDir = "/var/www"
	}
	branch := resolveTrackedBranch(stateDir, input.TargetService, input.Branch)

	effectiveRemote := resolveEffectiveRemote(stateDir, input.TargetService, input.RemoteURL)

	// Pre-flight: the container must have a git repo with at least one
	// commit at workingDir. A git push with nothing to transmit is either
	// a user bug or a silent fallback we refuse to ship (an earlier
	// design auto-committed everything when no commits existed; that
	// masked "agent forgot to commit" failures). See plan phase A.2 —
	// this replaces the old meta.IsDeployed() gate which false-positived
	// on adopted services the platform had deployed before ZCP ever
	// touched the meta.
	committedOut, err := sshDeployer.ExecSSH(ctx, hostname, committedCodeCheckCmd(workingDir))
	if err != nil {
		recordAttempt(fmt.Sprintf("committed-code check failed: %v", err), topology.FailureClassNetwork)
		return convertError(platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("cannot check committed code on %s: %s", hostname, err),
			"Verify the container is running and SSH is accessible",
		)), nil, nil
	}
	if strings.TrimSpace(string(committedOut)) != "1" {
		recordAttempt("no committed code at workingDir", topology.FailureClassConfig)
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"git-push requires committed code at "+workingDir+" on "+hostname,
			"Commit your changes on the container first: ssh "+hostname+` "cd `+workingDir+` && git add -A && git commit -m 'your message'". Then retry.`,
		)), nil, nil
	}

	// Dirty-tree probe (F1a): committedCodeCheckCmd proved a commit EXISTS, not
	// that the tree is clean. git-push transmits the committed HEAD only, so an
	// uncommitted/untracked change (e.g. a freshly written .github/workflows/
	// file) is silently left behind under a calm "Everything up-to-date".
	// Probe once, upfront; the warning is carried on EVERY outcome (PUSHED and
	// NOTHING_TO_PUSH) so the agent is told before the terminal message, not
	// after. Best-effort: a probe transport error is non-blocking. Parity with
	// the local path via the shared dirtyTreeWarning owner.
	var dirtyWarn string
	if porcelainOut, statusErr := sshDeployer.ExecSSH(ctx, hostname, gitStatusPorcelainCmd(workingDir)); statusErr == nil {
		dirtyWarn = dirtyTreeWarning(
			string(porcelainOut),
			fmt.Sprintf(`ssh %s "cd %s && git add -A && git commit -m '<msg>'"`, hostname, workingDir),
		)
	}

	// Pre-flight: check GIT_TOKEN exists on the container.
	tokenOut, err := sshDeployer.ExecSSH(ctx, hostname, gitTokenCheckCmd)
	if err != nil {
		recordAttempt(fmt.Sprintf("GIT_TOKEN check failed: %v", err), topology.FailureClassNetwork)
		return convertError(platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("cannot check GIT_TOKEN on %s: %s", hostname, err),
			"Verify the container is running and SSH is accessible",
		)), nil, nil
	}
	if strings.TrimSpace(string(tokenOut)) == "0" {
		recordAttempt("GIT_TOKEN missing in container session", topology.FailureClassCredential)
		// Lean diagnose: fresh SSH sessions read the LIVE platform env
		// (no restart coupling — spec-git-delivery-target §4), so a
		// missing $GIT_TOKEN in this session means the service env
		// genuinely lacks the secret (deleted / never written for this
		// service), NOT propagation lag. Either way the canonical fix is
		// the same single owner: re-run git-push-setup with the token —
		// it probes first, re-writes the service secret, and re-verifies
		// a fresh session end-to-end.
		meta, _ := workflow.FindServiceMeta(stateDir, hostname)
		if meta != nil && meta.GitPushState == topology.GitPushConfigured {
			return jsonResult(&gitPushPrerequisites{
				Status:       platform.ErrGitTokenMissing,
				Message:      fmt.Sprintf("meta records git-push as configured for %s, but the service env carries no GIT_TOKEN secret (fresh sessions read the live env — this is a missing secret, not propagation lag).", hostname),
				Instructions: fmt.Sprintf("Re-run zerops_workflow action=\"git-push-setup\" service=%q remoteUrl=<recorded remote> gitToken=<PAT> — it probe-verifies the token, re-writes the service-scope secret, and re-checks a fresh session before re-stamping.", hostname),
			}), nil, nil
		}
		// Route through convertError so appendCredentialContract (the single
		// owner for credential-class errors) fires — the agent must surface
		// the missing token to the user and NEVER fabricate a PAT (B11).
		return convertError(platform.NewPlatformError(
			platform.ErrGitTokenMissing,
			"GIT_TOKEN is not set. The project env var is required for pushing to a git remote.",
			fmt.Sprintf(gitPushSetupPointerInstructions, hostname, hostname),
		), WithRecoveryStatus()), nil, nil
	}

	// Pre-push zerops.yaml validation: the remote's receipt of this push
	// triggers a Zerops build pipeline — we validate the YAML the pipeline
	// is about to consume by running the same platform validator now. YAML
	// lives in the container at workingDir; fetch via SSH cat and pass to
	// the content-based entry point. Any failure aborts the push.
	yamlContent, yamlErr := fetchZeropsYamlOverSSH(ctx, sshDeployer, hostname, workingDir)
	if yamlErr == nil && yamlContent != "" {
		setupName := input.Setup
		if resolvedSetup, recordErr := recordDeploySetupMetaFromContent(stateDir, hostname, input.Setup, yamlContent); recordErr != nil {
			recordAttempt(fmt.Sprintf("deployed setup metadata record failed: %v", recordErr), topology.FailureClassConfig)
			return convertError(platform.NewPlatformError(
				platform.ErrPreflightFailed,
				fmt.Sprintf("failed to record deployed setup metadata for %s: %v", hostname, recordErr),
				"Retry after the local .zcp/state directory is writable; verify needs this metadata to distinguish HTTP services from workers.",
			), WithRecoveryStatus()), nil, nil
		} else if resolvedSetup != "" {
			setupName = resolvedSetup
		}
		if setupName == "" {
			setupName = hostname
		}
		if target := resolveTargetForValidation(ctx, client, projectID, hostname); target != nil {
			if vErr := ops.ValidatePreDeployContent(ctx, client, target, setupName, yamlContent); vErr != nil {
				recordAttempt(fmt.Sprintf("zerops.yaml validation failed: %v", vErr), topology.FailureClassConfig)
				return convertError(vErr), nil, nil
			}
			// Env-var pre-flight (deploy-decomp P4 R5 / DEPLOY-3): the build
			// pipeline that runs on the remote's receipt of this push consumes
			// run.envVariables refs at build time; missing peer-service refs
			// cause silent build failures. Shared with the local git-push path
			// (handleLocalGitPush) so both surface the same actionable feedback.
			if resp, detail := gitPushEnvRefPreflight(ctx, client, projectID, hostname, setupName, yamlContent); resp != nil {
				recordAttempt(fmt.Sprintf("env-var pre-flight failed: %s", detail), topology.FailureClassConfig)
				return resp, nil, nil
			}
		}
	}

	// A push onto a wired pair's own Gitea branch can open or touch a pull
	// request right after it (giteaPullRequestAfterPush below) — and Gitea
	// computes that request's mergeability itself, independent of whether
	// THIS push succeeds. So a landing of this Mate's own earlier pull
	// request (a squash shares no history with the branch it came from) has
	// to be absorbed BEFORE this push, or the request looks broken to
	// whoever looks at it before the next stage delivery ever runs
	// (gitea_delivery.go's deliverGiteaPair, which does the same absorb as
	// part of its own commit+push). A REAL conflict here — not the false
	// squash-vs-history one — stops the push outright: pushing on top of a
	// checkout the sync left mid-way is never right.
	var giteaLearnedNote string
	if giteaRemoteOfThisMate(effectiveRemote) {
		meta, _ := workflow.FindServiceMeta(stateDir, hostname)
		absorb := giteaAbsorbBeforePush(ctx, httpClient, sshDeployer, stateDir, hostname, workingDir, meta)
		giteaLearnedNote = absorb.LearnedNote
		if absorb.Conflict != "" {
			var detail, sequence string
			switch {
			case absorb.IsAbsorbConflict:
				detail = fmt.Sprintf("a real conflict inside absorbing this Mate's own earlier pull request — %s changes the same lines (%s)", giteaBaseOf(meta), absorb.Conflict)
				sequence = giteaManualAbsorbSequence(hostname, absorb.LandedCommit, giteaBaseOf(meta))
			case absorb.Unprovable:
				detail = fmt.Sprintf("%s has moved on and %s changes the same lines (%s) — this may be this Mate's own squashed pull request, which this container's git could not prove safe to fold in automatically", giteaBaseOf(meta), branch, absorb.Conflict)
				sequence = giteaManualAbsorbSequence(hostname, absorb.LandedCommit, giteaBaseOf(meta))
			default:
				detail = fmt.Sprintf("%s has moved on and %s changes the same lines (%s)", giteaBaseOf(meta), branch, absorb.Conflict)
				sequence = fmt.Sprintf("In %s's checkout run `git fetch origin && git merge origin/%s`, resolve it", hostname, giteaBaseOf(meta))
			}
			recordAttempt(fmt.Sprintf("gitea landing absorb conflict: %s", detail), topology.FailureClassConfig)
			return convertError(platform.NewPlatformError(
				platform.ErrSSHDeployFailed,
				fmt.Sprintf("git-push from %s has not reached %s: %s.", hostname, meta.Gitea.FullName, detail),
				sequence+", then push again.",
			), WithRecoveryStatus()), nil, nil
		}
	}

	// pushedAt anchors the build-watch discovery: integration builds
	// created at-or-after this instant belong to THIS push.
	pushedAt := time.Now().UTC()

	cmd := ops.BuildGitPushCommand(workingDir, effectiveRemote, branch)

	output, err := sshDeployer.ExecSSH(ctx, hostname, cmd)
	if err != nil {
		// Run the classifier so credential-class git failures land as
		// FailureClassCredential instead of generic Network — the recovery
		// (rotate GIT_TOKEN vs check connectivity) differs (E2).
		classification := classifyTransportError(err, "git-push")
		category := topology.FailureClassNetwork
		if classification != nil {
			category = classification.Category
		}
		// Token-rotation degradation: when a previously probe-verified
		// configured state hits a credential failure during push, the
		// most likely cause is upstream PAT rotation/revocation. Degrade
		// meta.GitPushState to broken so the next launch source-control
		// gate refuses cleanly + chains the agent into git-push-setup
		// for a fresh probe — instead of letting the stale "configured"
		// flag surface a launch as ready when its push wouldn't work.
		if category == topology.FailureClassCredential {
			degradeGitPushStateToBroken(stateDir, input.TargetService)
		}
		// Surface the real git stderr — "ssh <host>: exit status 1" alone
		// hides the actionable reason (non-fast-forward, auth rejected, repo
		// missing), which sits in the command output. Parity with the local
		// git-push path (handleLocalGitPush, which already truncates stderr).
		detail := gitPushErrorDetail(err, output)

		// GF-11: a non-fast-forward rejection is a structured, non-
		// destructive decision (rebase / merge / replace-remote) that
		// belongs to the user — never the generic SSH_DEPLOY_FAILED, and
		// zcp never force-pushes or merges on its own to resolve it.
		if ops.IsNonFastForwardRejection(detail) {
			rejection := classifyGitPushNonFastForward(sshGitRunner(ctx, sshDeployer, hostname, workingDir), effectiveRemote, branch)
			recordAttempt(fmt.Sprintf("git-push rejected non-fast-forward: %s", detail), topology.FailureClassConfig)
			return convertError(
				newGitPushNonFastForwardError(hostname, detail),
				WithFailureClassification(classification),
				WithGitPushRejection(rejection),
				WithRecoveryStatus(),
			), nil, nil
		}

		recordAttempt(fmt.Sprintf("git-push failed: %s", detail), category)
		return convertError(platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("git-push from %s failed: %s", hostname, detail),
			"See the git error above and `failureClassification` for the specific fix. Common cases: non-fast-forward pushes are classified as GIT_PUSH_NON_FAST_FORWARD, not this generic error; protected branch (the ref was refused by a pre-receive hook, NOT a credential fault) → push a topic branch and open a pull request, and do not rotate the token; auth rejected → re-run zerops_workflow action=\"git-push-setup\" with a fresh PAT; GIT_TOKEN missing → restart the runtime via zerops_manage action=\"restart\" then retry.",
		), WithFailureClassification(classification)), nil, nil
	}

	result := &ops.GitPushResult{
		Status:    "PUSHED",
		RemoteURL: effectiveRemote,
		Branch:    branch,
		Message:   fmt.Sprintf("Code pushed from %s to %s (branch: %s)", hostname, effectiveRemote, branch),
	}

	// Check for "Everything up-to-date" in output.
	if strings.Contains(string(output), "Everything up-to-date") {
		result.Status = statusNothingToPush
		result.Message = fmt.Sprintf("Nothing to push from %s — remote is up to date", hostname)
	}

	// Opened as soon as the push lands: the push is what put the Mate's branch
	// on the account's Gitea, and `main` there takes no direct push from
	// anyone. Idempotent: a second push finds the open one.
	pullRequest := giteaPullRequestAfterPush(ctx, httpClient, stateDir, hostname, effectiveRemote)
	giteaRemote := giteaRemoteOfThisMate(effectiveRemote)

	// C2 closure (audit-prerelease-internal-testing-2026-04-29): the
	// pre-fix path stamped attempt.SucceededAt = time.Now() right here,
	// which RecordDeployAttempt then propagated to FirstDeployedAt via
	// stampFirstDeployedAt (work_session.go:220). For
	// BuildIntegration ∈ {webhook, actions} the build was still async at
	// this point — meta.IsDeployed() flipped true while the actual deploy
	// hadn't landed yet. Agents observed Deployed=true post-push, ran
	// zerops_verify against stale state, and retried. So we record the
	// in-flight push attempt (no SucceededAt) and require explicit
	// record-deploy after the agent observes Status=ACTIVE on
	// zerops_events. The result.NextActions text below names that bridge.
	//
	// That placeholder is only honest when SOMETHING can later resolve it
	// (the build watch below, or its manual record-deploy fallback). Three
	// outcomes have no such resolver — nothing was transmitted
	// (statusNothingToPush), the destination is this Mate's own Gitea
	// branch (nothing ever builds from it — gitea_delivery.go), or the
	// target has no ZCP-managed BuildIntegration wired at all — and
	// recording it there left a permanent, unexplained "failed deploy"
	// (Success:false, no Reason) on the push source forever. A git-push
	// that no build follows is delivery, not a deploy of that service, so
	// those three cases record nothing (GF-13, docs/spec-workflows.md §12.6).
	trackable := result.Status != statusNothingToPush && !giteaRemote && gitPushBuildIntegrationConfigured(stateDir, input.TargetService)
	if trackable {
		_ = workflow.RecordDeployAttempt(stateDir, input.TargetService, attempt)
	}

	switch {
	case result.Status == statusNothingToPush && !giteaRemote:
		// Nothing was transmitted, so no integration build will fire — skip the
		// build watch (it would emit a false "Push landed…" nextActions). A
		// dirty tree is the usual cause: the dirtyWarn below already names the
		// uncommitted changes and the commit contract.
		if dirtyWarn != "" {
			result.NextActions = `Commit the changes listed in warnings (git-push pushes the committed HEAD only), then re-run zerops_deploy strategy="git-push".`
		} else {
			result.NextActions = "Working tree is clean and the remote already has this HEAD — nothing to deploy."
		}
	case giteaRemote:
		// The group's workflow runs on main, which the person's merge moves:
		// nothing builds from a Mate's branch (gitea_delivery.go).
		result.NextActions = giteaPushNextActions(pullRequest)
	default:
		// L1 build watch (spec-git-delivery-target §6.1): the push IS the
		// deploy, so follow the integration-triggered build to terminal the
		// way ZCP's own deploys are followed — discovery (new appVersion on
		// the build target at-or-after the push) → poll to terminal → build
		// logs + classification on FAILED → auto-record on ACTIVE. The manual
		// zerops_events + record-deploy bridge survives as the RECOVERY path
		// (watch timeout, compaction), never the happy path.
		finishGitPushWithBuildWatch(ctx, client, projectID, logFetcher, onProgress, input, stateDir, pushedAt, result)
	}

	// Container-side trackTriggerMissingWarning parity (deploy-decomp P4
	// R6). Surfaces the soft warning when the push succeeded but no
	// ZCP-managed BuildIntegration is configured — same shape as the
	// local-git path at deploy_local_git.go:212. UTILITY framing: the
	// user may still have independent CI/CD that ZCP doesn't track.
	// The dirty-tree warning (F1a) leads: it names work that did NOT ship.
	var warnings []string
	if dirtyWarn != "" {
		warnings = append(warnings, dirtyWarn)
	}
	// News about a PREVIOUSLY recorded pull request this push's own absorb
	// step learned ("pull request #N is merged…") — independent of this
	// push's own outcome, and the last place anything would ever say it
	// once the number is gone from meta (giteaAbsorbBeforePush).
	if giteaLearnedNote != "" {
		warnings = append(warnings, giteaLearnedNote)
	}
	if warn := trackTriggerMissingWarning(stateDir, hostname); warn != "" && !giteaRemote {
		warnings = append(warnings, warn)
	}

	return jsonResult(deployGitPushResponse{
		GitPushResult:    result,
		PullRequest:      pullRequest,
		Warnings:         warnings,
		WorkSessionState: sessionAnnotations(stateDir),
		Envelope:         freshEnvelope(ctx, stateDir, client, projectID, rt),
	}), nil, nil
}

// deployGitPushResponse wraps the push-git result with the structured
// WorkSessionState lifecycle signal (F5 closure). Same shape as the
// local/batch wrappers; the three exist because their underlying result
// types differ and Go can't embed an interface-typed field the way we'd
// want.
type deployGitPushResponse struct {
	*ops.GitPushResult
	// PullRequest is the request this push's branch lands through on the
	// account's own Gitea — absent everywhere else.
	PullRequest      *giteaPullRequestRef `json:"pullRequest,omitempty"`
	Warnings         []string             `json:"warnings,omitempty"`
	WorkSessionState *WorkSessionState    `json:"workSessionState,omitempty"`
	// Envelope is the post-mutation lifecycle state (docs/spec-mate.md §1.3).
	// Absent when its computation failed — the rest of the response is
	// unaffected.
	Envelope *workflow.StateEnvelope `json:"envelope,omitempty"`
}

// finishGitPushWithBuildWatch is the §6.1 build watch: after a successful
// push it resolves the build target, watches the integration-triggered
// build to terminal, and mutates the GitPushResult in place —
// DELIVERED + auto-record on ACTIVE; build logs + classification on
// FAILED; honest push-landed-build-not-observed otherwise. The manual
// zerops_events + record-deploy bridge survives only as the recovery
// path (watch timeout, compaction).
func finishGitPushWithBuildWatch(
	ctx context.Context,
	client platform.Client,
	projectID string,
	logFetcher platform.LogFetcher,
	onProgress ops.ProgressCallback,
	input DeploySSHInput,
	stateDir string,
	pushedAt time.Time,
	result *ops.GitPushResult,
) {
	manualBridge := fmt.Sprintf(
		"Watch the build via zerops_events serviceHostname=%q until Status=ACTIVE, then ack with zerops_workflow action=\"record-deploy\" targetService=%q.",
		input.TargetService, input.TargetService,
	)

	meta, _ := workflow.FindServiceMeta(stateDir, input.TargetService)
	if meta == nil {
		result.NextActions = manualBridge
		return
	}
	buildHost, _ := anticipatedBuildTarget(meta)
	if buildHost == "" {
		buildHost = meta.Hostname
	}
	result.BuildTarget = buildHost

	// No integration declared → nothing will fire. Surface the standing
	// three-way choice (spec-git-delivery-target §2, L1-incomplete) —
	// never a silent PUSHED.
	if meta.BuildIntegration == "" || meta.BuildIntegration == topology.BuildIntegrationNone {
		result.NextActions = fmt.Sprintf(
			"Push landed, but NO build integration is wired — nothing rebuilds %q from the repo. Choose: (1) wire it (recommended): zerops_workflow action=\"build-integration\" service=%q integration=\"actions|webhook\"; (2) one-time sync deploy: zerops_deploy targetService=%q breakGlass=true; (3) leave the service on its current version. %s",
			buildHost, meta.Hostname, meta.Hostname, manualBridge,
		)
		return
	}

	if client == nil || projectID == "" {
		result.NextActions = manualBridge
		return
	}
	svc, lookupErr := ops.LookupService(ctx, client, projectID, buildHost)
	if lookupErr != nil || svc == nil {
		result.NextActions = manualBridge
		return
	}

	watch, watchErr := ops.WatchIntegrationBuild(ctx, client, projectID, svc.ID, pushedAt, onProgress)
	if watchErr != nil {
		// Read failure — the push itself landed; degrade to the bridge.
		result.Warnings = append(result.Warnings, fmt.Sprintf("build watch read failed: %v", watchErr))
		result.NextActions = manualBridge
		return
	}

	switch {
	case !watch.Observed:
		result.BuildStatus = "NOT_OBSERVED"
		result.NextActions = fmt.Sprintf(
			"Push landed but no integration build appeared on %q within the discovery window. The %q integration may be slow (Actions runner spin-up) or broken — check the repo's CI runs / the service's dashboard pipeline, then %s",
			buildHost, meta.BuildIntegration, manualBridge,
		)
	case watch.TimedOut:
		result.BuildObserved = true
		result.BuildStatus = watch.Event.Status
		result.NextActions = fmt.Sprintf(
			"Integration build observed on %q (status %s) but did not finish within the watch window. %s",
			buildHost, watch.Event.Status, manualBridge,
		)
	case watch.Event.Status == "ACTIVE":
		result.Status = "DELIVERED"
		result.BuildObserved = true
		result.BuildStatus = watch.Event.Status
		result.AutoRecorded = true
		result.VerifyTarget = buildHost
		result.Message = fmt.Sprintf(
			"Code pushed from %s and the integration build landed on %s (ACTIVE). Deploy auto-recorded — no record-deploy ack needed.",
			input.TargetService, buildHost,
		)
		result.NextActions = fmt.Sprintf("Run zerops_verify serviceHostname=%q for runtime state.", buildHost)
		// Auto-record: the platform observation IS the record-deploy
		// bridge (derivation over agent memory). Recorded on the BUILD
		// TARGET hostname — the same target the manual bridge names.
		_ = workflow.RecordDeployAttempt(stateDir, buildHost, workflow.DeployAttempt{
			AttemptedAt: pushedAt.Format(time.RFC3339),
			SucceededAt: time.Now().UTC().Format(time.RFC3339),
			Strategy:    deployStrategyGitPush,
			Setup:       input.Setup,
		})
	default: // FAILED / CANCELED
		result.BuildObserved = true
		result.BuildStatus = watch.Event.Status
		if logFetcher != nil {
			result.BuildLogs = ops.FetchBuildLogs(ctx, client, logFetcher, projectID, watch.Event, 100)
		}
		result.FailureClassification = ops.ClassifyDeployFailure(ops.FailureInput{
			Phase:     ops.PhaseBuild,
			Strategy:  deployStrategyGitPush,
			BuildLogs: result.BuildLogs,
		})
		result.NextActions = fmt.Sprintf(
			"The integration build on %q finished %s. Read failureClassification first; buildLogs carry the diagnostic depth. Fix, commit, and push again.",
			buildHost, watch.Event.Status,
		)
		_ = workflow.RecordDeployAttempt(stateDir, buildHost, workflow.DeployAttempt{
			AttemptedAt:  pushedAt.Format(time.RFC3339),
			Strategy:     deployStrategyGitPush,
			Setup:        input.Setup,
			Error:        fmt.Sprintf("integration build %s", watch.Event.Status),
			FailureClass: topology.FailureClassBuild,
		})
	}
}
