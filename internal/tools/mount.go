package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// MountInput is the input type for zerops_mount.
type MountInput struct {
	Action          string `json:"action"`
	ServiceHostname string `json:"serviceHostname,omitempty"`
}

// RegisterMount registers the zerops_mount tool.
func RegisterMount(srv *mcp.Server, client platform.Client, projectID string, mounter ops.Mounter) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_mount",
		Description: "Mount/unmount service filesystems via SSHFS. Actions: mount, unmount, status.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Mount/unmount service filesystems",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input MountInput) (*mcp.CallToolResult, any, error) {
		if input.Action == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Action is required",
				"Use mount, unmount, or status")), nil, nil
		}

		switch input.Action {
		case "mount":
			result, err := ops.MountService(ctx, client, projectID, mounter, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(result), nil, nil
		case "unmount":
			result, err := ops.UnmountService(ctx, client, projectID, mounter, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(result), nil, nil
		case actionStatus:
			result, err := ops.MountStatus(ctx, client, projectID, mounter, input.ServiceHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(result), nil, nil
		default:
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Invalid action '"+input.Action+"'",
				"Use mount, unmount, or status")), nil, nil
		}
	})
}
