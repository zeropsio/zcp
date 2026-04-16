package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// gitPushPrerequisites is a structured response when GIT_TOKEN is missing.
// Guides the agent through the decision question and setup steps.
type gitPushPrerequisites struct {
	Status       string `json:"status"`
	Message      string `json:"message"`
	Instructions string `json:"instructions"`
}

const gitTokenCheckCmd = `echo "$GIT_TOKEN"`

const gitPushSetupInstructions = `Ask the user: Do you want to just push code to the remote, or set up full CI/CD (automatic deploy on every push)?

**Option A: Push code to remote**
1. Create a GitHub fine-grained token (Contents: Read and write) or GitLab token (write_repository)
2. Set it as project env var: zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]
3. Retry this zerops_deploy command

**Option B: Full CI/CD**
Run: zerops_workflow action="start" workflow="cicd"
This sets up automatic deploy on every git push (GitHub Actions with zcli).`

// handleGitPush executes the git-push strategy: push committed code to an
// external git remote. No Zerops build is triggered — no pollDeployBuild.
func handleGitPush(
	ctx context.Context,
	sshDeployer ops.SSHDeployer,
	authInfo auth.Info,
	input DeploySSHInput,
) (*mcp.CallToolResult, any, error) {
	if sshDeployer == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"SSH deployer not configured",
			"git-push requires a running Zerops container with SSH access",
		)), nil, nil
	}
	if input.TargetService == "" {
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

	// Pre-flight: check GIT_TOKEN exists on the container.
	tokenOut, err := sshDeployer.ExecSSH(ctx, hostname, gitTokenCheckCmd)
	if err == nil && strings.TrimSpace(string(tokenOut)) == "" {
		return jsonResult(&gitPushPrerequisites{
			Status:       "PREREQUISITES_MISSING",
			Message:      "GIT_TOKEN is not set. This project env var is required for pushing to a git remote.",
			Instructions: gitPushSetupInstructions,
		}), nil, nil
	}

	id := ops.GitIdentity{Name: authInfo.FullName, Email: authInfo.Email}
	cmd := ops.BuildGitPushCommand(workingDir, input.RemoteURL, branch, id)

	output, err := sshDeployer.ExecSSH(ctx, hostname, cmd)
	if err != nil {
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

	return jsonResult(result), nil, nil
}
