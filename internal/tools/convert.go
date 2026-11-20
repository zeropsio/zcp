package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// Local aliases of platform process/build/service status values — kept
// so call sites stay readable without sprinkling the platform. prefix
// everywhere. Source: internal/platform/types.go.
const (
	serviceStatusRunning       = platform.ServiceStatusRunning
	serviceStatusActive        = platform.ServiceStatusActive
	serviceStatusNew           = platform.ServiceStatusNew
	serviceStatusReadyToDeploy = platform.ServiceStatusReadyToDeploy
)

const (
	actionStatus                 = "status"
	statusActive                 = platform.ServiceStatusActive
	statusDeployed               = platform.BuildStatusDeployed
	statusBuildFailed            = platform.BuildStatusBuildFailed
	statusPreparingRuntimeFailed = platform.BuildStatusPreparingRuntimeFail
	statusDeployFailed           = platform.BuildStatusDeployFailed
	statusCanceled               = platform.ProcessStatusCanceled
	statusFinished               = platform.ProcessStatusFinished
	statusFailed                 = platform.ProcessStatusFailed
)

// wrapStageErr preserves a typed PlatformError when the underlying err
// already carries one, prefixing the message with stage for UX context;
// otherwise wraps the raw error in a generic ErrAPIError-coded
// PlatformError. Used by lifecycle pipeline sites (ComputeEnvelope,
// LoadAtomCorpus, Synthesize) so a corrupt-work-session error
// surfaces its recovery Suggestion to the LLM instead of being
// flattened to ErrNotImplemented with no suggestion.
func wrapStageErr(stage string, err error) *platform.PlatformError {
	var pe *platform.PlatformError
	if errors.As(err, &pe) {
		out := *pe
		out.Message = fmt.Sprintf("%s: %s", stage, pe.Message)
		return &out
	}
	return platform.NewPlatformError(
		platform.ErrAPIError,
		fmt.Sprintf("%s: %v", stage, err),
		"")
}

// convertError converts an error to a CallToolResult with IsError=true.
// Every error becomes the canonical ErrorWire JSON shape — generic Go
// errors are wrapped as platform.NewPlatformError(ErrUnknown, ...) at
// this boundary so the wire never carries a plain-text error response.
//
// For workflow-aware handlers that should attach a recovery hint or
// multi-check failures, use convertErrorWith(err, opts...) — convertError
// is the bare entry point for handlers without workflow context.
//
// Every optional field (suggestion, apiCode, diagnostic, apiMeta, checks,
// recovery) is emitted only when populated — consumers rely on
// absence-means-empty.
func convertError(err error, opts ...ErrorOption) *mcp.CallToolResult {
	var pe *platform.PlatformError
	var sshFailure *platform.SSHExecError
	switch {
	case errors.As(err, &pe):
	case errors.As(err, &sshFailure):
		// An ssh call no caller coded is an ssh failure, never UNKNOWN.
		pe = platform.NewPlatformError(platform.ErrSSHFailed, err.Error(), "")
	default:
		// Plain Go error → wrap as typed ErrUnknown. Plain-text wire
		// shape eliminated; every error response is typed JSON.
		pe = platform.NewPlatformError(platform.ErrUnknown, err.Error(), "")
	}
	wire := platformErrorToWire(pe)
	// The class and the ssh output come from err itself: a plain error
	// wrapped as ErrUnknown above is not zcp's refusal.
	wire.Class = platform.ClassOf(err)
	var sshErr *platform.SSHExecError
	if wire.Diagnostic == "" && errors.As(err, &sshErr) {
		wire.Diagnostic = sshErr.Output
	}
	for _, opt := range opts {
		opt(&wire)
	}
	// Single owner of the user-owned-credential contract: applied last so it
	// rides every credential-class error regardless of which handler built it
	// (B6b — agents fabricated PATs when the actor was left implicit).
	appendCredentialContract(&wire)
	b, marshalErr := json.Marshal(wire)
	if marshalErr != nil {
		// Fallback: emit a typed marshal-error rather than panicking.
		// This branch is unreachable in practice (ErrorWire is plain
		// JSON-marshalable) but guards against future schema mistakes.
		// The literal fallback can't itself fail to marshal — only
		// declared string fields, no funcs/channels.
		fallback, _ := json.Marshal(ErrorWire{ //nolint:errchkjson // simple-string struct can't fail
			Code:  platform.ErrUnknown,
			Error: fmt.Sprintf("marshal error: %v", marshalErr),
		})
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(fallback)}},
			IsError: true,
		}
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
		IsError: true,
	}
}

// jsonResult marshals v to JSON and returns it as a CallToolResult.
func jsonResult(v any) *mcp.CallToolResult {
	b, err := json.Marshal(v)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("marshal error: %v", err)}},
			IsError: true,
		}
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

// processResult is the tool result of a response whose operation ran as the
// platform process proc (its final state, after pollManageProcess): v as JSON,
// flagged as an error when proc ended FAILED or CANCELED, so a failed change
// never reads as done. The body is unchanged — the agent still reads the
// process, its failReason and its ids. A nil proc (the API answered without
// one) or one still in flight (the poll timed out) stays a success.
func processResult(v any, proc *platform.Process) *mcp.CallToolResult {
	result := jsonResult(v)
	if processFailed(proc) {
		result.IsError = true
	}
	return result
}

// processFailed reports whether proc is a platform process that ended FAILED
// or CANCELED.
func processFailed(proc *platform.Process) bool {
	return proc != nil &&
		(proc.Status == platform.ProcessStatusFailed || proc.Status == platform.ProcessStatusCanceled)
}

// processNext is a result's next action: next, or processFailedNext when proc
// ended FAILED or CANCELED.
func processNext(proc *platform.Process, next string) string {
	if processFailed(proc) {
		return processFailedNext(proc)
	}
	return next
}

// processFailedNext is the sentence a failed or canceled process puts in place
// of a handler's success text.
func processFailedNext(proc *platform.Process) string {
	ended := fmt.Sprintf("The platform's %s process ended %s", proc.ActionName, proc.Status)
	if proc.FailReason != nil && *proc.FailReason != "" {
		ended += ": " + *proc.FailReason
	}
	return ended + ". Nothing it was asked to change is confirmed; read the reason before calling again."
}

// textResult returns a plain text CallToolResult.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// boolPtr returns a pointer to b. Used for optional bool fields in ToolAnnotations.
func boolPtr(b bool) *bool { return &b }

// buildProgressCallback returns an ops.ProgressCallback wired to MCP progress
// notifications if the client provided a progressToken. Returns nil otherwise.
func buildProgressCallback(ctx context.Context, req *mcp.CallToolRequest) ops.ProgressCallback {
	if req == nil || req.Params == nil {
		return nil
	}
	token := req.Params.GetProgressToken()
	if token == nil {
		return nil
	}
	return func(message string, progress, total float64) {
		_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			ProgressToken: token,
			Message:       message,
			Progress:      progress,
			Total:         total,
		})
	}
}
