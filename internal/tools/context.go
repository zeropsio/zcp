package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
)

// ContextInput is the input type for zerops_context (no parameters).
type ContextInput struct{}

// RegisterContext registers the zerops_context tool.
func RegisterContext(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_context",
		Description: "Get Zerops platform context — fundamentals, rules, service types, defaults. Call this first when working with Zerops.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(false),
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ ContextInput) (*mcp.CallToolResult, any, error) {
		return textResult(ops.GetContext()), nil, nil
	})
}
