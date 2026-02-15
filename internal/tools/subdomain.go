package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// SubdomainInput is the input type for zerops_subdomain.
type SubdomainInput struct {
	ServiceHostname string `json:"serviceHostname"`
	Action          string `json:"action"`
}

// RegisterSubdomain registers the zerops_subdomain tool.
func RegisterSubdomain(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_subdomain",
		Description: "Enable or disable zerops.app subdomain for a service. Idempotent. NOTE: If you set enableSubdomainAccess=true in import YAML, the subdomain is already configured — do NOT call this tool separately. The subdomain activates automatically after the first deploy.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Enable or disable subdomain",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SubdomainInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.Subdomain(ctx, client, projectID, input.ServiceHostname, input.Action)
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(result), nil, nil
	})
}
