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
			fmt.Sprintf("Run zerops_workflow action=\"git-push-setup\" service=%q first to set up GIT_TOKEN, .netrc, and remote URL.", targetService),
		), WithRecoveryStatus())
	}
	return nil
}

const gitTokenCheckCmd = `test -n "$GIT_TOKEN" && echo 1 || echo 0`

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
	projectID string,
	sshDeployer ops.SSHDeployer,
	input DeploySSHInput,
	stateDir string,
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
	branch := input.Branch
	if branch == "" {
		branch = "main"
	}

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
		recordAttempt(fmt.Sprintf("git-push failed: %s", detail), category)
		return convertError(platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("git-push from %s failed: %s", hostname, detail),
			"See the git error above and `failureClassification` for the specific fix. Common cases: non-fast-forward (remote has commits you lack) → `git pull --rebase` then re-push or force-push; auth rejected → re-run zerops_workflow action=\"git-push-setup\" with a fresh PAT; GIT_TOKEN missing → restart the runtime via zerops_manage action=\"restart\" then retry.",
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
		result.Status = "NOTHING_TO_PUSH"
		result.Message = fmt.Sprintf("Nothing to push from %s — remote is up to date", hostname)
	}

	// C2 closure (audit-prerelease-internal-testing-2026-04-29): the
	// pre-fix path stamped attempt.SucceededAt = time.Now() right here,
	// which RecordDeployAttempt then propagated to FirstDeployedAt via
	// stampFirstDeployedAt (work_session.go:220). For
	// BuildIntegration ∈ {webhook, actions} the build was still async at
	// this point — meta.IsDeployed() flipped true while the actual deploy
	// hadn't landed yet. Agents observed Deployed=true post-push, ran
	// zerops_verify against stale state, and retried. Now we record the
	// in-flight push attempt (no SucceededAt) and require explicit
	// record-deploy after the agent observes Status=ACTIVE on
	// zerops_events. The result.NextActions text below names that bridge.
	_ = workflow.RecordDeployAttempt(stateDir, input.TargetService, attempt)

	result.NextActions = fmt.Sprintf(
		"Watch the build via zerops_events serviceHostname=%q until Status=ACTIVE, then ack with zerops_workflow action=\"record-deploy\" targetService=%q. The push transmitted bytes; the platform build runs async and FirstDeployedAt will not stamp until you bridge it.",
		input.TargetService, input.TargetService,
	)

	// Container-side trackTriggerMissingWarning parity (deploy-decomp P4
	// R6). Surfaces the soft warning when the push succeeded but no
	// ZCP-managed BuildIntegration is configured — same shape as the
	// local-git path at deploy_local_git.go:212. UTILITY framing: the
	// user may still have independent CI/CD that ZCP doesn't track.
	var warnings []string
	if warn := trackTriggerMissingWarning(stateDir, hostname); warn != "" {
		warnings = append(warnings, warn)
	}

	return jsonResult(deployGitPushResponse{
		GitPushResult:    result,
		Warnings:         warnings,
		WorkSessionState: sessionAnnotations(stateDir),
	}), nil, nil
}

// deployGitPushResponse wraps the push-git result with the structured
// WorkSessionState lifecycle signal (F5 closure). Same shape as the
// local/batch wrappers; the three exist because their underlying result
// types differ and Go can't embed an interface-typed field the way we'd
// want.
type deployGitPushResponse struct {
	*ops.GitPushResult
	Warnings         []string          `json:"warnings,omitempty"`
	WorkSessionState *WorkSessionState `json:"workSessionState,omitempty"`
}
