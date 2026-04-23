package tools

import (
	"context"
	"fmt"

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
		// Belt-and-suspenders TOCTOU race handling: check-before-enable in
		// ops.Subdomain normally prevents this branch from firing (authoritative
		// GetService read short-circuits before EnableSubdomainAccess is
		// called). But if the subdomain got enabled between our check and our
		// call, the platform returns a Process that immediately FAILs with
		// error.code=noSubdomainPorts. Normalize to already_enabled when URLs
		// are present — but preserve the FailReason in Warnings so a genuine
		// platform error (distinct failure mode not yet observed) never gets
		// silently swallowed.
		if input.Action == actionEnable && result.Process != nil &&
			result.Process.Status == statusFailed && len(result.SubdomainUrls) > 0 {
			reason := "FAILED process normalized to already_enabled (URLs present — likely TOCTOU race with concurrent enable)"
			if result.Process.FailReason != nil && *result.Process.FailReason != "" {
				reason = fmt.Sprintf("%s: %s", reason, *result.Process.FailReason)
			}
			result.Warnings = append(result.Warnings, reason)
			result.Status = "already_enabled"
			result.Process = nil
		}
		if input.Action == actionEnable {
			result.NextActions = nextActionSubdomainEnable
		}
		return jsonResult(result), nil, nil
	})
}
