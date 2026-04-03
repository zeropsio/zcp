package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// ExportInput is the input type for zerops_export.
type ExportInput struct {
	Service string `json:"service,omitempty" jsonschema:"Export a single service by hostname. Omit to export the entire project."`
}

// RegisterExport registers the zerops_export tool.
func RegisterExport(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_export",
		Description: "Export project or service configuration as re-importable YAML. Returns the platform export YAML plus discovered service metadata (mode, scaling, status). Use without service parameter to export the entire project.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Export project/service configuration",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ExportInput) (*mcp.CallToolResult, any, error) {
		if input.Service != "" {
			yaml, err := ops.ExportService(ctx, client, projectID, input.Service)
			if err != nil {
				return convertError(err), nil, nil
			}
			return textResult(yaml), nil, nil
		}

		result, err := ops.ExportProject(ctx, client, projectID)
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(result), nil, nil
	})
}
