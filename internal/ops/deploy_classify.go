package ops

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// isSSHBuildTriggered checks SSH output for markers indicating a build was
// successfully submitted before the SSH connection dropped (common exit 255).
func isSSHBuildTriggered(output string) bool {
	markers := []string{
		"BUILD ARTEFACTS READY TO DEPLOY",
		"Deploying service",
	}
	for _, m := range markers {
		if strings.Contains(output, m) {
			return true
		}
	}
	return false
}

// classifySSHError converts SSH deploy errors into actionable PlatformErrors.
func classifySSHError(err error, sourceService, targetService string) *platform.PlatformError {
	msg := err.Error()

	switch {
	case strings.Contains(msg, "signal: killed") || strings.Contains(msg, "OOM") || strings.Contains(msg, "out of memory"):
		return platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("SSH deploy from %s to %s killed (likely OOM)", sourceService, targetService),
			fmt.Sprintf("Process killed, likely insufficient RAM. Scale up the source service: zerops_scale serviceHostname=%s minRam=2", sourceService),
		)
	case strings.Contains(msg, "zerops.yml") || strings.Contains(msg, "zerops.yaml"):
		return platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("SSH deploy from %s to %s failed: zerops.yml not found", sourceService, targetService),
			"zerops.yml must be present in workingDir. After deploy, /var/www only contains deployFiles artifacts — dev services must use deployFiles: [.] so zerops.yml survives for SSH cross-service deploys.",
		)
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "no route to host"):
		return platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("SSH deploy from %s to %s failed: cannot reach source service", sourceService, targetService),
			fmt.Sprintf("Cannot reach source service. Verify it's RUNNING: zerops_discover service=%s", sourceService),
		)
	case strings.Contains(msg, "not in a git directory") || strings.Contains(msg, "not a git repository"):
		return platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("SSH deploy from %s to %s failed: working directory is not a git repository", sourceService, targetService),
			"The working directory lost its .git state (common on SSHFS mounts). Retry with freshGit=true to reinitialize.",
		)
	case strings.Contains(msg, "command not found"):
		return platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("SSH deploy from %s to %s failed: command not found", sourceService, targetService),
			"zcli not available on source container. Verify the source service type supports zcli.",
		)
	case strings.Contains(msg, "no space left on device") || strings.Contains(msg, "disk quota exceeded"):
		return platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("SSH deploy from %s to %s failed: disk full", sourceService, targetService),
			fmt.Sprintf("No disk space. Scale up: zerops_scale serviceHostname=%s minDisk=5", sourceService),
		)
	case strings.Contains(msg, "permission denied"):
		return platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("SSH deploy from %s to %s failed: permission denied", sourceService, targetService),
			"Check buildCommands for ownership issues. Files may need chown or chmod.",
		)
	case strings.Contains(msg, "module not found") || strings.Contains(msg, "cannot find module"):
		return platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("SSH deploy from %s to %s failed: module not found", sourceService, targetService),
			"Add dependency install command to buildCommands (e.g., npm install, go mod tidy, bun install).",
		)
	case strings.Contains(msg, "exec format error"):
		return platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("SSH deploy from %s to %s failed: exec format error", sourceService, targetService),
			"Binary architecture mismatch. Ensure build targets linux/amd64 (e.g., GOOS=linux GOARCH=amd64).",
		)
	default:
		return platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("SSH deploy from %s to %s failed: %s", sourceService, targetService, msg),
			"Check the full error output above for diagnosis.",
		)
	}
}
