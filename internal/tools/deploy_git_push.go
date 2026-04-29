package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

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
	cmd := fmt.Sprintf(
		`cat %s/zerops.yaml 2>/dev/null || cat %s/zerops.yml 2>/dev/null || true`,
		workingDir, workingDir,
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
			"Run mode-expansion to upgrade ModeDev → ModeStandard (adds a stage half) before deploying with strategy=git-push: zerops_workflow action=\"mode-expansion\" service=\""+targetService+"\"",
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

// committedCodeCheckCmd returns "1" when workingDir contains a git repo
// with at least one commit reachable from HEAD, "0" otherwise. This is the
// real precondition for git-push: the push has to transmit a commit, not a
// platform-level "service deployed" timestamp. The check must NOT mention
// the word "netrc" so the stub dispatcher in tests can distinguish it
// from the GIT_TOKEN check without fuzzy matching.
func committedCodeCheckCmd(workingDir string) string {
	return fmt.Sprintf(
		`test -d %s/.git && git -C %s rev-parse HEAD >/dev/null 2>&1 && echo 1 || echo 0`,
		workingDir, workingDir,
	)
}

// gitPushSetupPointerInstructions redirects to the post-decomposition
// git-push-setup action (deploy-decomp P5). The full setup flow is
// synthesized there from the atom corpus. After git-push-setup completes,
// the agent can independently wire a build integration via
// action=build-integration if desired (orthogonal dimension).
const gitPushSetupPointerInstructions = `Configure git-push capability via the deploy-config actions:

  zerops_workflow action="git-push-setup" service="%s"
  # then optionally:
  zerops_workflow action="build-integration" service="%s" integration="webhook|actions"

git-push-setup walks through GIT_TOKEN / .netrc / remote URL setup;
build-integration wires the ZCP-managed CI integration (independent of
any external CI/CD you may already have). After setup completes, retry
this push.`

// handleGitPush executes the git-push strategy: push committed code to an
// external git remote. No Zerops build is triggered directly from our side,
// but the remote's receipt of the push triggers one — so zerops.yaml still
// needs to be valid. Pre-push validation fetches the file from the container
// via SSH cat and calls the Zerops validator; any failure aborts the push.
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
		recordAttempt("GIT_TOKEN missing", topology.FailureClassCredential)
		return jsonResult(&gitPushPrerequisites{
			Status:       platform.ErrGitTokenMissing,
			Message:      "GIT_TOKEN is not set. This project env var is required for pushing to a git remote.",
			Instructions: fmt.Sprintf(gitPushSetupPointerInstructions, hostname, hostname),
		}), nil, nil
	}

	// Pre-push zerops.yaml validation: the remote's receipt of this push
	// triggers a Zerops build pipeline — we validate the YAML the pipeline
	// is about to consume by running the same platform validator now. YAML
	// lives in the container at workingDir; fetch via SSH cat and pass to
	// the content-based entry point. Any failure aborts the push.
	if target := resolveTargetForValidation(ctx, client, projectID, hostname); target != nil {
		yamlContent, yamlErr := fetchZeropsYamlOverSSH(ctx, sshDeployer, hostname, workingDir)
		if yamlErr == nil && yamlContent != "" {
			setupName := input.Setup
			if setupName == "" {
				setupName = hostname
			}
			if vErr := ops.ValidatePreDeployContent(ctx, client, target, setupName, yamlContent); vErr != nil {
				recordAttempt(fmt.Sprintf("zerops.yaml validation failed: %v", vErr), topology.FailureClassConfig)
				return convertError(vErr), nil, nil
			}
			// Env-var pre-flight (deploy-decomp P4 R5): the build pipeline
			// that runs on the remote's receipt of this push consumes
			// run.envVariables refs at build time; missing peer-service
			// refs cause silent build failures. Validate against live API
			// state now so the user sees actionable feedback before the
			// push transmits. Parse failures fall through (yaml schema is
			// already validated above; a parse miss here is a content
			// mismatch we don't want to escalate to a deploy-blocker).
			if doc, parseErr := ops.ParseZeropsYmlContent([]byte(yamlContent), "zerops.yaml"); parseErr == nil {
				if entry := doc.FindEntry(setupName); entry != nil && len(entry.EnvVariables) > 0 {
					for _, c := range preflightEnvRefs(ctx, client, projectID, hostname, entry) {
						if c.Status == statusFail {
							recordAttempt(fmt.Sprintf("env-var pre-flight failed: %s", c.Detail), topology.FailureClassConfig)
							return convertError(platform.NewPlatformError(
								platform.ErrPreflightFailed,
								"env-var references invalid: "+c.Detail,
								"Fix env-var references in zerops.yaml run.envVariables; ${peer_var} refs must name an existing peer service + env var.",
							), WithRecoveryStatus()), nil, nil
						}
					}
				}
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
		recordAttempt(fmt.Sprintf("git-push failed: %v", err), category)
		_ = output
		return convertError(platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("git-push from %s failed: %s", hostname, err),
			"Check GIT_TOKEN env var, remote URL, and git status on the container",
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
		"Watch the build via zerops_events filterServices=[%q] until Status=ACTIVE, then ack with zerops_workflow action=\"record-deploy\" targetService=%q. The push transmitted bytes; the platform build runs async and FirstDeployedAt will not stamp until you bridge it.",
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

	note, progress := sessionAnnotations(stateDir)
	return jsonResult(deployGitPushResponse{
		GitPushResult:     result,
		Warnings:          warnings,
		WorkSessionNote:   note,
		AutoCloseProgress: progress,
	}), nil, nil
}

// deployGitPushResponse wraps the push-git result with session
// annotations. Same shape as the local/batch wrappers; the three exist
// because their underlying result types differ and Go can't embed an
// interface-typed field the way we'd want.
type deployGitPushResponse struct {
	*ops.GitPushResult
	Warnings          []string                    `json:"warnings,omitempty"`
	WorkSessionNote   string                      `json:"workSessionNote,omitempty"`
	AutoCloseProgress *workflow.AutoCloseProgress `json:"autoCloseProgress,omitempty"`
}
