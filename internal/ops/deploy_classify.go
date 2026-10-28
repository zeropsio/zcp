package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// zcliDeployingLine is what zcli push prints once its package is uploaded,
// as it asks the platform to build and deploy it (zcli i18n
// PushDeployDeployingStart).
const zcliDeployingLine = "Deploying service"

// isSSHBuildTriggered checks SSH output for markers indicating a build was
// successfully submitted before the SSH connection dropped (common exit 255).
func isSSHBuildTriggered(output string) bool {
	markers := []string{
		"BUILD ARTEFACTS READY TO DEPLOY",
		zcliDeployingLine,
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
	// The push's own report lines (what its upload carried, DM-9) are read
	// out; the rest is zcli's output, kept whole for the LLM.
	upload, output := splitUploadReport(sshErr.Output)

	pe := &platform.PlatformError{
		Code:       platform.ErrSSHDeployFailed,
		Diagnostic: output, // ALWAYS preserve full output for LLM
	}

	// Only classify OS/SSH-level errors — these NEVER appear in zcli progress output.
	switch {
	case strings.Contains(output, crewSaveFailedMarker):
		saveStderr, _, _ := strings.Cut(output, crewSaveFailedMarker)
		pe.Message = fmt.Sprintf("SSH deploy from %s to %s refused: the Mate's crew save failed, so this deploy is held and crewmates' unsaved work is not lost:\n%s",
			sourceService, targetService, lastLines(saveStderr, 5))
		pe.Suggestion = "Fix what the crew save reports (its stderr is above), then deploy again."
	case upload.unfinished:
		// The push's remote side waited for the upload by its size and it was
		// still running: slow, not broken — stopPush ended it before zcli
		// asked for the build.
		within := "within its budget"
		if upload.budget > 0 {
			within = "within " + upload.budget.String()
		}
		pe.Message = fmt.Sprintf("SSH deploy from %s to %s %s %s %s; the push was stopped and started no build",
			sourceService, targetService, uploadUnfinishedPhrase, uploadWhat(upload), within)
		pe.Suggestion = uploadUnfinishedSuggestion
	case errors.Is(sshErr.Err, context.DeadlineExceeded):
		// The ssh client was stopped at its ceiling, not the remote command: a
		// push stopped before zcli said it was deploying is ended by stopPush.
		pe.Message = fmt.Sprintf("SSH deploy from %s to %s timed out: it %s %s; the push was stopped and started no build",
			sourceService, targetService, uploadUnfinishedPhrase, uploadWhat(upload))
		pe.Suggestion = uploadUnfinishedSuggestion
	case errors.Is(sshErr.Err, context.Canceled):
		pe.Message = fmt.Sprintf("SSH deploy from %s to %s was cancelled before it finished uploading %s; the push was stopped and started no build",
			sourceService, targetService, uploadWhat(upload))
		pe.Suggestion = "Nothing is building from this deploy. Deploy again when ready."
	case strings.Contains(output, zcliClosedPipe):
		// zcli's upload connection closed while it still sent the package;
		// the signal transport:zcli-upload-closed says what to try.
		pe.Message = fmt.Sprintf("SSH deploy from %s to %s failed: the connection carrying the upload of %s closed before it was sent; no build started:\n%s",
			sourceService, targetService, uploadWhat(upload), lastLines(output, 5))
		pe.Suggestion = "Check the failureClassification for what to try."
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
