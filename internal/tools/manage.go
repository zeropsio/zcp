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
	NextActions string `json:"nextActions,omitempty"`
}

// ManageInput is the input type for zerops_manage.
type ManageInput struct {
	Action          string `json:"action"                    jsonschema:"Lifecycle action: start, stop, restart, reload, connect-storage, disconnect-storage."`
	ServiceHostname string `json:"serviceHostname"           jsonschema:"Hostname of the service to manage."`
	StorageHostname string `json:"storageHostname,omitempty" jsonschema:"Hostname of shared-storage service. Required for connect-storage/disconnect-storage."`
}

// RegisterManage registers the zerops_manage tool.
func RegisterManage(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_manage",
		Description: "Manage service lifecycle: start, stop, restart, reload, connect-storage, disconnect-storage. Use reload after env var changes (~4s, faster than restart ~14s). Use connect-storage/disconnect-storage to attach/detach a shared-storage volume to a runtime service (mounts at /mnt/{storageHostname}). Requires storageHostname parameter.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Manage service lifecycle",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ManageInput) (*mcp.CallToolResult, any, error) {
		if input.Action == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Action is required",
				"Use start, stop, restart, reload, connect-storage, or disconnect-storage")), nil, nil
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
			finalProc := pollManageProcess(ctx, client, proc, onProgress)
			return jsonResult(manageResponse{Process: finalProc, NextActions: nextActionManageReload}), nil, nil
		case "connect-storage":
			if input.StorageHostname == "" {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidParameter, "storageHostname is required for connect-storage",
					"Provide storageHostname parameter with the shared-storage service hostname")), nil, nil
			}
			proc, err := ops.ConnectStorage(ctx, client, projectID, input.ServiceHostname, input.StorageHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(pollManageProcess(ctx, client, proc, onProgress)), nil, nil
		case "disconnect-storage":
			if input.StorageHostname == "" {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidParameter, "storageHostname is required for disconnect-storage",
					"Provide storageHostname parameter with the shared-storage service hostname")), nil, nil
			}
			proc, err := ops.DisconnectStorage(ctx, client, projectID, input.ServiceHostname, input.StorageHostname)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(pollManageProcess(ctx, client, proc, onProgress)), nil, nil
		default:
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Invalid action '"+input.Action+"'",
				"Use start, stop, restart, reload, connect-storage, or disconnect-storage")), nil, nil
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
