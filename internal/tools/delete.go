package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// DeleteInput is the input type for zerops_delete.
type DeleteInput struct {
	ServiceHostname string `json:"serviceHostname" jsonschema:"Hostname of the service to delete."`
}

// RegisterDelete registers the zerops_delete tool.
// stateDir is the workflow state directory; empty string disables service meta cleanup.
// mounter, when non-nil, enables best-effort SSHFS unmount of the deleted service.
func RegisterDelete(srv *mcp.Server, client platform.Client, projectID string, stateDir string, mounter ops.Mounter, rtInfo runtime.Info) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_delete",
		Description: "Delete a service. This is destructive and permanent. " +
			"IMPORTANT: You MUST have explicit user approval in the current conversation to delete " +
			"THIS SPECIFIC service by name. Never delete proactively — only when the user explicitly asks.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Delete a service",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeleteInput) (*mcp.CallToolResult, any, error) {
		// Guard: prevent self-deletion when running inside a Zerops container.
		if rtInfo.InContainer && strings.EqualFold(input.ServiceHostname, rtInfo.ServiceName) {
			return convertError(platform.NewPlatformError(
				platform.ErrSelfServiceBlocked,
				fmt.Sprintf("Cannot delete %q — ZCP is running on this service", input.ServiceHostname),
				"Delete this service manually via Zerops GUI, zcli, or from a different machine.",
			)), nil, nil
		}

		// Best-effort: unmount SSHFS before delete so the service still exists for clean unmount.
		if mounter != nil {
			if res, umErr := ops.UnmountService(ctx, client, projectID, mounter, input.ServiceHostname); umErr != nil {
				fmt.Fprintf(os.Stderr, "zcp: pre-delete unmount %s: %v\n", input.ServiceHostname, umErr)
			} else {
				fmt.Fprintf(os.Stderr, "zcp: pre-delete unmount %s: %s\n", input.ServiceHostname, res.Status)
			}
		}

		proc, err := ops.Delete(ctx, client, projectID, input.ServiceHostname)
		if err != nil {
			return convertError(err), nil, nil
		}
		onProgress := buildProgressCallback(ctx, req)
		finalProc, _ := pollManageProcess(ctx, client, proc, onProgress)

		// Best-effort: clean up service meta after successful delete+poll.
		if stateDir != "" && finalProc.Status == statusFinished {
			if delErr := workflow.DeleteServiceMeta(stateDir, input.ServiceHostname); delErr != nil {
				fmt.Fprintf(os.Stderr, "zcp: delete service meta %s: %v\n", input.ServiceHostname, delErr)
			}
		}

		return jsonResult(finalProc), nil, nil
	})
}
