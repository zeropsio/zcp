package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// ManageInput is the input type for zerops_manage.
type ManageInput struct {
	Action          string `json:"action"          jsonschema:"Lifecycle action to perform: start, stop, restart, or reload."`
	ServiceHostname string `json:"serviceHostname" jsonschema:"Hostname of the service to manage."`
}

// RegisterManage registers the zerops_manage tool.
func RegisterManage(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_manage",
		Description: "Manage service lifecycle: start, stop, restart, or reload a service. Use reload after env var changes — it's faster (~4s) than restart (~14s) and sufficient for picking up new environment variables.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Manage service lifecycle",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(true),
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
		case "start":
			proc, err := ops.Start(ctx, client, projectID, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(pollManageProcess(ctx, client, proc, onProgress)), nil, nil
		case "stop":
			proc, err := ops.Stop(ctx, client, projectID, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(pollManageProcess(ctx, client, proc, onProgress)), nil, nil
		case "restart":
			proc, err := ops.Restart(ctx, client, projectID, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(pollManageProcess(ctx, client, proc, onProgress)), nil, nil
		case "reload":
			proc, err := ops.Reload(ctx, client, projectID, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(pollManageProcess(ctx, client, proc, onProgress)), nil, nil
		default:
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Invalid action '"+input.Action+"'",
				"Use start, stop, restart, or reload")), nil, nil
		}
	})
}

// pollManageProcess polls a process until completion, returning the final state.
// On timeout/error, returns the original process unchanged.
func pollManageProcess(
	ctx context.Context,
	client platform.Client,
	proc *platform.Process,
	onProgress ops.ProgressCallback,
) *platform.Process {
	if proc == nil || proc.ID == "" {
		return proc
	}
	finalProc, err := ops.PollProcess(ctx, client, proc.ID, onProgress)
	if err != nil {
		return proc // return original on timeout/error
	}
	return finalProc
}
