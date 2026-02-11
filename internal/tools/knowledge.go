package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
)

// KnowledgeInput is the input type for zerops_knowledge.
type KnowledgeInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

// RegisterKnowledge registers the zerops_knowledge tool.
func RegisterKnowledge(srv *mcp.Server, store knowledge.Provider) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_knowledge",
		Description: "Search Zerops knowledge base using BM25. Use specific terms like service names, config keys, or error messages.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, input KnowledgeInput) (*mcp.CallToolResult, any, error) {
		if input.Query == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Query is required", "Provide a search query")), nil, nil
		}
		results := store.Search(input.Query, input.Limit)
		return jsonResult(results), nil, nil
	})
}
