package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// gitPushPrerequisites is a structured response when GIT_TOKEN is missing.
// Guides the agent through the decision question and setup steps.
type gitPushPrerequisites struct {
	Status       string `json:"status"`
	Message      string `json:"message"`
	Instructions string `json:"instructions"`
}

const gitTokenCheckCmd = `test -n "$GIT_TOKEN" && echo 1 || echo 0`

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

// gitPushSetupPointerInstructions redirects to the central deploy-config
// action; the full setup flow is synthesized there from the atom corpus.
const gitPushSetupPointerInstructions = `Configure push-git via the central deploy-config action:

  zerops_workflow action="strategy" strategies={"%s":"push-git"}

That returns the full setup flow — asks push-only vs full CI/CD, handles
GIT_TOKEN permissions, and covers GitHub Actions / webhook if CI/CD chosen.
After setup completes, retry this push.`

// handleGitPush executes the git-push strategy: push committed code to an
// external git remote. No Zerops build is triggered — no pollDeployBuild.
func handleGitPush(
	ctx context.Context,
	sshDeployer ops.SSHDeployer,
	authInfo auth.Info,
	input DeploySSHInput,
	stateDir string,
) (*mcp.CallToolResult, any, error) {
	attempt := workflow.DeployAttempt{
		AttemptedAt: time.Now().UTC().Format(time.RFC3339),
		Setup:       input.Setup,
		Strategy:    deployStrategyGitPush,
	}
	recordAttempt := func(err string) {
		attempt.Error = err
		_ = workflow.RecordDeployAttempt(stateDir, input.TargetService, attempt)
	}

	if sshDeployer == nil {
		recordAttempt("SSH deployer not configured")
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"SSH deployer not configured",
			"git-push requires a running Zerops container with SSH access",
		)), nil, nil
	}
	if input.TargetService == "" {
		recordAttempt("targetService missing")
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"targetService is required for git-push",
			"Provide the hostname of the service to push from",
		)), nil, nil
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
		recordAttempt(fmt.Sprintf("committed-code check failed: %v", err))
		return convertError(platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("cannot check committed code on %s: %s", hostname, err),
			"Verify the container is running and SSH is accessible",
		)), nil, nil
	}
	if strings.TrimSpace(string(committedOut)) != "1" {
		recordAttempt("no committed code at workingDir")
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"git-push requires committed code at "+workingDir+" on "+hostname,
			"Commit your changes on the container first: ssh "+hostname+` "cd `+workingDir+` && git add -A && git commit -m 'your message'". Then retry.`,
		)), nil, nil
	}

	// Pre-flight: check GIT_TOKEN exists on the container.
	tokenOut, err := sshDeployer.ExecSSH(ctx, hostname, gitTokenCheckCmd)
	if err != nil {
		recordAttempt(fmt.Sprintf("GIT_TOKEN check failed: %v", err))
		return convertError(platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("cannot check GIT_TOKEN on %s: %s", hostname, err),
			"Verify the container is running and SSH is accessible",
		)), nil, nil
	}
	if strings.TrimSpace(string(tokenOut)) == "0" {
		recordAttempt("GIT_TOKEN missing")
		return jsonResult(&gitPushPrerequisites{
			Status:       platform.ErrGitTokenMissing,
			Message:      "GIT_TOKEN is not set. This project env var is required for pushing to a git remote.",
			Instructions: fmt.Sprintf(gitPushSetupPointerInstructions, hostname),
		}), nil, nil
	}

	id := ops.GitIdentity{Name: authInfo.FullName, Email: authInfo.Email}
	cmd := ops.BuildGitPushCommand(workingDir, input.RemoteURL, branch, id)

	output, err := sshDeployer.ExecSSH(ctx, hostname, cmd)
	if err != nil {
		recordAttempt(fmt.Sprintf("git-push failed: %v", err))
		return convertError(platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("git-push from %s failed: %s", hostname, err),
			"Check GIT_TOKEN env var, remote URL, and git status on the container",
		)), nil, nil
	}

	result := &ops.GitPushResult{
		Status:    "PUSHED",
		RemoteURL: input.RemoteURL,
		Branch:    branch,
		Message:   fmt.Sprintf("Code pushed from %s to %s (branch: %s)", hostname, input.RemoteURL, branch),
	}

	// Check for "Everything up-to-date" in output.
	if strings.Contains(string(output), "Everything up-to-date") {
		result.Status = "NOTHING_TO_PUSH"
		result.Message = fmt.Sprintf("Nothing to push from %s — remote is up to date", hostname)
	}

	attempt.SucceededAt = time.Now().UTC().Format(time.RFC3339)
	_ = workflow.RecordDeployAttempt(stateDir, input.TargetService, attempt)

	note, progress := sessionAnnotations(stateDir)
	return jsonResult(deployGitPushResponse{
		GitPushResult:     result,
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
	WorkSessionNote   string                      `json:"workSessionNote,omitempty"`
	AutoCloseProgress *workflow.AutoCloseProgress `json:"autoCloseProgress,omitempty"`
}
