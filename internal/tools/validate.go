package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
)

// ValidateInput is the input type for zerops_validate.
type ValidateInput struct {
	Content  string `json:"content,omitempty"`
	FilePath string `json:"filePath,omitempty"`
	Type     string `json:"type,omitempty"`
}

// RegisterValidate registers the zerops_validate tool.
func RegisterValidate(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_validate",
		Description: "Validate zerops.yml or import.yml configuration. Provide content or filePath.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, input ValidateInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.Validate(input.Content, input.FilePath, input.Type)
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(result), nil, nil
	})
}
