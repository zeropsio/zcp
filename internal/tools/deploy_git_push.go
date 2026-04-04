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
