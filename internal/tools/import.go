package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// ImportInput is the input type for zerops_import.
type ImportInput struct {
	Content  string `json:"content,omitempty"`
	FilePath string `json:"filePath,omitempty"`
	DryRun   bool   `json:"dryRun,omitempty"`
}

// RegisterImport registers the zerops_import tool.
func RegisterImport(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_import",
		Description: "Import services from YAML into the current project. Use dryRun=true to preview.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Import services from YAML",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ImportInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.Import(ctx, client, projectID, input.Content, input.FilePath, input.DryRun)
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(result), nil, nil
	})
}
