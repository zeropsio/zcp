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
	Action          string `json:"action"          jsonschema:"Action: enable or disable. Recovery / production opt-in / disable only — first deploy of an eligible service is handled by zerops_deploy."`
}

// RegisterSubdomain registers the zerops_subdomain tool. httpClient is used
// to verify L7 readiness after a fresh enable — the platform Process reporting
// FINISHED does not guarantee the L7 balancer is serving traffic (propagation
// measured at 440ms–1.3s after Process completion). Tests inject a stub
// HTTPDoer to bypass the wait without real network I/O.
func RegisterSubdomain(srv *mcp.Server, client platform.Client, httpClient ops.HTTPDoer, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_subdomain",
		Description: "Enable or disable zerops.app subdomain. Idempotent. The L7 route is enabled by zerops_deploy on first deploy for eligible modes (dev / stage / simple / standard / local-stage); this tool is for explicit recovery, production opt-in, or disable operations. Check zerops_discover for current status and URL.",
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
			finalProc, timedOut := pollManageProcess(ctx, client, result.Process, onProgress)
			// Surface poll timeouts in Warnings. Discarding timedOut silently
			// meant a 10-minute poll timeout produced stale Process state and
			// the tool returned as if enable had succeeded. Now the caller
			// sees the timeout and can distinguish "confirmed enable" from
			// "unknown state, retry recommended".
			if timedOut {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("process poll timed out for action=%s; state may be stale — retry or check zerops_discover", input.Action))
			}
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
			result.Status = ops.SubdomainStatusAlreadyEnabled
			result.Process = nil
		}
		// Plan 1 commit 5: L7 propagation window is 440ms-1.3s after enable.
		// Wait for each SubdomainUrl to respond with <500 before returning,
		// so the agent's next zerops_verify doesn't race the L7 balancer.
		// Best-effort — timeout appends to Warnings, never fails the call.
		if input.Action == actionEnable && len(result.SubdomainUrls) > 0 {
			for _, url := range result.SubdomainUrls {
				if err := ops.WaitHTTPReady(ctx, httpClient, url); err != nil {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("subdomain %s not HTTP-ready after wait: %v (agent may need to retry verify)", url, err))
				}
			}
		}
		if input.Action == actionEnable {
			result.NextActions = nextActionSubdomainEnable
		}
		return jsonResult(result), nil, nil
	})
}
