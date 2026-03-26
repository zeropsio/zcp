package ops

import (
	"errors"
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
//
// Design: only classify OS/SSH-level errors (signal killed, connection refused)
// that NEVER appear in zcli progress output. Everything else goes to the LLM
// as raw output in the Diagnostic field — the LLM is a better text parser
// than any regex we could write.
func classifySSHError(err error, sourceService, targetService string) *platform.PlatformError {
	var sshErr *platform.SSHExecError
	if !errors.As(err, &sshErr) {
		// Non-SSH error (service resolution, validation, etc.) — pass through.
		return platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("SSH deploy from %s to %s failed: %s", sourceService, targetService, err),
			"Check the error message for details.",
		)
	}

	exitMsg := sshErr.Err.Error() // OS-level: "exit status 1", "signal: killed", etc.
	output := sshErr.Output

	pe := &platform.PlatformError{
		Code:       platform.ErrSSHDeployFailed,
		Diagnostic: output, // ALWAYS preserve full output for LLM
	}

	// Only classify OS/SSH-level errors — these NEVER appear in zcli progress output.
	switch {
	case strings.Contains(exitMsg, "signal: killed"):
		pe.Message = fmt.Sprintf("SSH deploy from %s to %s killed (likely OOM)", sourceService, targetService)
		pe.Suggestion = fmt.Sprintf("Process killed. Scale up: zerops_scale serviceHostname=%s minRam=2", sourceService)
	case strings.Contains(exitMsg, "connection refused") || strings.Contains(exitMsg, "no route to host"):
		pe.Message = fmt.Sprintf("SSH deploy from %s to %s failed: cannot reach source service", sourceService, targetService)
		pe.Suggestion = fmt.Sprintf("Verify it's RUNNING: zerops_discover service=%s", sourceService)
	default:
		// Don't classify zcli output — show last lines, let LLM reason about it.
		tail := lastLines(output, 5)
		pe.Message = fmt.Sprintf("SSH deploy from %s to %s failed:\n%s", sourceService, targetService, tail)
		pe.Suggestion = "Check the diagnostic field for full command output."
	}

	return pe
}

// lastLines returns the last n non-empty lines from s.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) <= n {
		return strings.TrimSpace(s)
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
