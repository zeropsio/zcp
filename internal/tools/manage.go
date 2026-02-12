package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// ManageInput is the input type for zerops_manage.
type ManageInput struct {
	Action          string `json:"action"`
	ServiceHostname string `json:"serviceHostname"`
}

// RegisterManage registers the zerops_manage tool.
func RegisterManage(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_manage",
		Description: "Manage service lifecycle: start, stop, or restart a service.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Manage service lifecycle",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ManageInput) (*mcp.CallToolResult, any, error) {
		if input.Action == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Action is required",
				"Use start, stop, or restart")), nil, nil
		}
		if input.ServiceHostname == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceRequired, "Service hostname is required",
				"Provide serviceHostname parameter")), nil, nil
		}

		switch input.Action {
		case "start":
			proc, err := ops.Start(ctx, client, projectID, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(proc), nil, nil
		case "stop":
			proc, err := ops.Stop(ctx, client, projectID, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(proc), nil, nil
		case "restart":
			proc, err := ops.Restart(ctx, client, projectID, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(proc), nil, nil
		default:
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Invalid action '"+input.Action+"'",
				"Use start, stop, or restart")), nil, nil
		}
	})
}
