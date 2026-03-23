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
	Action          string `json:"action"          jsonschema:"Action: enable or disable. Call once after first deploy of new services."`
}

// RegisterSubdomain registers the zerops_subdomain tool.
func RegisterSubdomain(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_subdomain",
		Description: "Enable or disable zerops.app subdomain. Idempotent. New services need one enable call after first deploy to activate the L7 route. Re-deploys do NOT deactivate it. Check zerops_discover for current status and URL.",
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
