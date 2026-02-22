package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// LogsInput is the input type for zerops_logs.
type LogsInput struct {
	ServiceHostname string `json:"serviceHostname"    jsonschema:"Hostname of the service to fetch logs from."`
	Severity        string `json:"severity,omitempty" jsonschema:"Filter by log severity: WARNING or ERROR. Omit for all severities."`
	Since           string `json:"since,omitempty"    jsonschema:"Fetch logs since this time. RFC3339 format (e.g. 2024-01-15T10:00:00Z) or relative duration (e.g. 1h, 30m)."`
	Limit           int    `json:"limit,omitempty"    jsonschema:"Maximum number of log entries to return. Default: 100."`
	Search          string `json:"search,omitempty"   jsonschema:"Full-text search filter applied to log messages."`
}

// RegisterLogs registers the zerops_logs tool.
func RegisterLogs(srv *mcp.Server, client platform.Client, fetcher platform.LogFetcher, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_logs",
		Description: "Fetch runtime logs from a service. Filter by severity, time range, and search text.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Fetch service logs",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input LogsInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.FetchLogs(ctx, client, fetcher, projectID,
			input.ServiceHostname, input.Severity, input.Since, input.Limit, input.Search)
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(result), nil, nil
	})
}
