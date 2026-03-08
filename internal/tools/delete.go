package tools

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// DeleteInput is the input type for zerops_delete.
type DeleteInput struct {
	ServiceHostname string `json:"serviceHostname" jsonschema:"Hostname of the service to delete."`
	ConfirmHostname string `json:"confirmHostname" jsonschema:"Must exactly match serviceHostname to confirm deletion."`
}

// RegisterDelete registers the zerops_delete tool.
// stateDir is the workflow state directory; empty string disables service meta cleanup.
func RegisterDelete(srv *mcp.Server, client platform.Client, projectID string, stateDir string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_delete",
		Description: "Delete a service. Requires confirm=true. This is destructive and permanent. " +
			"IMPORTANT: You MUST have explicit user approval in the current conversation to delete " +
			"THIS SPECIFIC service by name. Never delete proactively — only when the user explicitly asks. " +
			"Set confirmHostname to exactly match serviceHostname.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Delete a service",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeleteInput) (*mcp.CallToolResult, any, error) {
		proc, err := ops.Delete(ctx, client, projectID, input.ServiceHostname, input.ConfirmHostname)
		if err != nil {
			return convertError(err), nil, nil
		}
		onProgress := buildProgressCallback(ctx, req)
		finalProc, _ := pollManageProcess(ctx, client, proc, onProgress)

		// Best-effort: clean up service meta after successful delete+poll.
		if stateDir != "" && finalProc.Status == "FINISHED" {
			if delErr := workflow.DeleteServiceMeta(stateDir, input.ServiceHostname); delErr != nil {
				fmt.Fprintf(os.Stderr, "zcp: delete service meta %s: %v\n", input.ServiceHostname, delErr)
			}
		}

		return jsonResult(finalProc), nil, nil
	})
}
