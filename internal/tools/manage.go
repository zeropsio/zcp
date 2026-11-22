package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// manageResponse wraps a Process with NextActions for the MCP response.
type manageResponse struct {
	*platform.Process
	TimedOut    bool   `json:"timedOut,omitempty"`
	NextActions string `json:"nextActions,omitempty"`
}

// ManageInput is the input type for zerops_manage.
type ManageInput struct {
	Action          string `json:"action"          jsonschema:"Lifecycle action: start, stop, restart, reload."`
	ServiceHostname string `json:"serviceHostname" jsonschema:"Hostname of the service to manage."`
}

// RegisterManage registers the zerops_manage tool.
func RegisterManage(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_manage",
		Description: "Manage service lifecycle: start, stop, restart, reload. After an env change restart, NOT reload — a runtime keeps its boot env; PHP-FPM keeps its boot config. reload (~4s) is for graceful config/code reloads. A Zerops Box is the opposite: reload applies an API key change, keeping sandboxes running; restart/stop kill them.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Manage service lifecycle",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(false),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ManageInput) (*mcp.CallToolResult, any, error) {
		if input.Action == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Action is required",
				"Use start, stop, restart, or reload")), nil, nil
		}
		if input.ServiceHostname == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceRequired, "Service hostname is required",
				"Provide serviceHostname parameter")), nil, nil
		}

		onProgress := buildProgressCallback(ctx, req)

		switch input.Action {
		case actionStart:
			proc, err := ops.Start(ctx, client, projectID, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			finalProc, timedOut := pollManageProcess(ctx, client, proc, onProgress)
			return processResult(manageResponse{Process: finalProc, TimedOut: timedOut, NextActions: processNext(finalProc, nextActionManageStart)}, finalProc), nil, nil
		case "stop":
			proc, svc, err := ops.Stop(ctx, client, projectID, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			finalProc, timedOut := pollManageProcess(ctx, client, proc, onProgress)
			next := withBoxLifecycleNotice(svc, ops.BoxActionStop, nextActionManageStop)
			return processResult(manageResponse{Process: finalProc, TimedOut: timedOut, NextActions: processNext(finalProc, next)}, finalProc), nil, nil
		case "restart":
			proc, svc, err := ops.Restart(ctx, client, projectID, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			finalProc, timedOut := pollManageProcess(ctx, client, proc, onProgress)
			next := withBoxLifecycleNotice(svc, ops.BoxActionRestart, nextActionManageRestart)
			return processResult(manageResponse{Process: finalProc, TimedOut: timedOut, NextActions: processNext(finalProc, next)}, finalProc), nil, nil
		case "reload":
			proc, err := ops.Reload(ctx, client, projectID, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			finalProc, timedOut := pollManageProcess(ctx, client, proc, onProgress)
			return processResult(manageResponse{Process: finalProc, TimedOut: timedOut, NextActions: processNext(finalProc, nextActionManageReload)}, finalProc), nil, nil
		default:
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Invalid action '"+input.Action+"'",
				"Use start, stop, restart, or reload")), nil, nil
		}
	})
}

// withBoxLifecycleNotice puts in front of next what a restart or stop did to
// a Zerops Box's running sandboxes (ops.BoxLifecycleNotice), read from svc —
// the service the stop or restart resolved; next alone for any other service.
func withBoxLifecycleNotice(svc *platform.ServiceStack, action, next string) string {
	if svc == nil {
		return next
	}
	notice := ops.BoxLifecycleNotice(svc.Name, svc.ServiceStackTypeInfo.ServiceStackTypeVersionName, action)
	if notice == "" {
		return next
	}
	return notice + " " + next
}

// pollManageProcess polls a process until completion, returning the final state
// and whether polling timed out. On timeout/error, returns the original process
// with timedOut=true so callers can signal stale data to the LLM.
func pollManageProcess(
	ctx context.Context,
	client platform.Client,
	proc *platform.Process,
	onProgress ops.ProgressCallback,
) (*platform.Process, bool) {
	if proc == nil || proc.ID == "" {
		return proc, false
	}
	finalProc, err := ops.PollProcess(ctx, client, proc.ID, onProgress)
	if err != nil {
		return proc, true // return original on timeout/error
	}
	return finalProc, false
}
