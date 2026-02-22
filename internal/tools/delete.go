package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// DeleteInput is the input type for zerops_delete.
type DeleteInput struct {
	ServiceHostname string `json:"serviceHostname" jsonschema:"Hostname of the service to delete."`
	Confirm         bool   `json:"confirm"         jsonschema:"Must be true to confirm deletion. This is destructive and permanent."`
}

// RegisterDelete registers the zerops_delete tool.
func RegisterDelete(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_delete",
		Description: "Delete a service. Requires confirm=true. This is destructive and permanent.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Delete a service",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteInput) (*mcp.CallToolResult, any, error) {
		proc, err := ops.Delete(ctx, client, projectID, input.ServiceHostname, input.Confirm)
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(proc), nil, nil
	})
}
