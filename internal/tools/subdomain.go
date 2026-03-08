package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

const actionEnable = "enable"

// SubdomainInput is the input type for zerops_subdomain.
type SubdomainInput struct {
	ServiceHostname string `json:"serviceHostname" jsonschema:"Hostname of the service to enable/disable subdomain for."`
	Action          string `json:"action"          jsonschema:"Action: enable or disable. Must call enable after first deploy to activate routing."`
}

// RegisterSubdomain registers the zerops_subdomain tool.
func RegisterSubdomain(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_subdomain",
		Description: "Enable or disable zerops.app subdomain for a service. Returns subdomainUrls in the response — this is the ONLY source for subdomain URLs (zerops_discover does not include them). Idempotent — safe to call even if already enabled (returns already_enabled). NOTE: If you set enableSubdomainAccess=true in import YAML, the subdomain URL is pre-configured but routing is NOT active. You MUST call this tool with action=\"enable\" after the first successful deploy to activate L7 balancer routing. Without this call, the subdomain returns 502 even if the app is running internally.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Enable or disable subdomain",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(false),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input SubdomainInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.Subdomain(ctx, client, projectID, input.ServiceHostname, input.Action)
		if err != nil {
			return convertError(err), nil, nil
		}
		if result.Process != nil && result.Process.ID != "" {
			onProgress := buildProgressCallback(ctx, req)
			finalProc, _ := pollManageProcess(ctx, client, result.Process, onProgress)
			result.Process = finalProc
		}
		// API sometimes returns a process that immediately FAILs instead of
		// returning SUBDOMAIN_ALREADY_ENABLED error. Detect and normalize.
		if input.Action == actionEnable && result.Process != nil &&
			result.Process.Status == statusFailed && len(result.SubdomainUrls) > 0 {
			result.Status = "already_enabled"
			result.Process = nil
		}
		if input.Action == actionEnable {
			result.NextActions = nextActionSubdomainEnable
		}
		return jsonResult(result), nil, nil
	})
}
