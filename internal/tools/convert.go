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
	if !errors.As(err, &pe) {
		// Plain Go error → wrap as typed ErrUnknown. Plain-text wire
		// shape eliminated; every error response is typed JSON.
		pe = platform.NewPlatformError(platform.ErrUnknown, err.Error(), "")
	}
	wire := platformErrorToWire(pe)
	for _, opt := range opts {
		opt(&wire)
	}
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
