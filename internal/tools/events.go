package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// EventsInput is the input type for zerops_events.
type EventsInput struct {
	ServiceHostname string `json:"serviceHostname,omitempty" jsonschema:"Filter events by service hostname. Omit to see all project events."`
	Limit           int    `json:"limit,omitempty"           jsonschema:"Maximum number of events to return."`
}

// RegisterEvents registers the zerops_events tool. The fetcher enables
// failed-event enrichment: BUILD_FAILED / DEPLOY_FAILED / PREPARING_RUNTIME_FAILED
// appVersion events get classified via the deploy-failure classifier so
// the response carries `failureClass` + `failureCause` alongside the raw
// `failReason` — same diagnostic shape the synchronous deploy path
// produces in DeployResult.FailureClassification.
func RegisterEvents(srv *mcp.Server, client platform.Client, fetcher platform.LogFetcher, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_events",
		Description: "Fetch project activity timeline. Aggregates processes and build/deploy events sorted by time. ALWAYS filter by serviceHostname (project-level includes stale builds from other services). Stop polling after stack.build shows FINISHED. Check stack.build process for build status, NOT appVersion (different events). Failed appVersion events carry structured failureClass + failureCause for build/deploy diagnosis.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Fetch project activity timeline",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input EventsInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.Events(ctx, client, fetcher, projectID, input.ServiceHostname, input.Limit)
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(result), nil, nil
	})
}
