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

const gitPushSetupInstructions = `Ask the user: Do you want to just push code to the remote, or set up full CI/CD (automatic deploy on every push)?

**Option A: Push code to remote**
GitHub fine-grained token permissions: **Contents: Read and write** (that's all)
1. GitHub → Settings → Developer settings → Fine-grained tokens → select repo
2. Set it: zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]
3. Retry this zerops_deploy command

**Option B: Full CI/CD (push → automatic deploy)**
GitHub fine-grained token permissions: **Contents: Read and write** + **Secrets: Read and write** + **Workflows: Read and write**
1. Create token with all three permissions above
2. Set it: zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]
3. Run: zerops_workflow action="start" workflow="cicd"`

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

	// Pre-flight: git-push requires a landed first deploy — the container
	// must already have code to commit + push. Run the default self-deploy
	// first; git-push only takes over once FirstDeployedAt is stamped.
	meta, _ := workflow.ReadServiceMeta(stateDir, hostname)
	if meta == nil || !meta.IsDeployed() {
		recordAttempt("service not deployed — git-push requires a first deploy")
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"git-push requires a successful first deploy on "+hostname,
			"Run zerops_deploy targetService=\""+hostname+"\" without the strategy argument first (default self-deploy). After a passing verify stamps FirstDeployedAt, retry with strategy=git-push.",
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
			Instructions: gitPushSetupInstructions,
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

	return jsonResult(deployGitPushResponse{
		GitPushResult:     result,
		WorkSessionNote:   workSessionNote(stateDir),
		AutoCloseProgress: workflow.AutoCloseProgressFor(stateDir),
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
