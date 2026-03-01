package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// DiscoverInput is the input type for zerops_discover.
type DiscoverInput struct {
	Service     string `json:"service,omitempty"     jsonschema:"Filter by service hostname. Omit to list all services in the project. When discovering env vars for multiple services, omit this parameter — one call returns all."`
	IncludeEnvs bool   `json:"includeEnvs,omitempty" jsonschema:"Include environment variables (both service-level and project-level) in the response. This is the primary way to read env vars. Without service filter, returns env vars for ALL services in one call."`
}

// RegisterDiscover registers the zerops_discover tool.
func RegisterDiscover(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_discover",
		Description: "Discover project and service information. Optionally filter by service hostname and include environment variables. This is the primary tool for reading env vars — use includeEnvs=true.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Discover project and services",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DiscoverInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.Discover(ctx, client, projectID, input.Service, input.IncludeEnvs)
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(result), nil, nil
	})
}
