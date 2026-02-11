package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// ProcessInput is the input type for zerops_process.
type ProcessInput struct {
	ProcessID string `json:"processId"`
	Action    string `json:"action,omitempty"`
}

// RegisterProcess registers the zerops_process tool.
func RegisterProcess(srv *mcp.Server, client platform.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_process",
		Description: "Check status or cancel an async process. Default action is 'status'.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ProcessInput) (*mcp.CallToolResult, any, error) {
		action := input.Action
		if action == "" {
			action = "status"
		}

		switch action {
		case "status":
			result, err := ops.GetProcessStatus(ctx, client, input.ProcessID)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(result), nil, nil
		case "cancel":
			result, err := ops.CancelProcess(ctx, client, input.ProcessID)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(result), nil, nil
		default:
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"Invalid action '"+action+"'",
				"Use 'status' or 'cancel'",
			)), nil, nil
		}
	})
}
