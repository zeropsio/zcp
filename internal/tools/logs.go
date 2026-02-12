package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// LogsInput is the input type for zerops_logs.
type LogsInput struct {
	ServiceHostname string `json:"serviceHostname"`
	Severity        string `json:"severity,omitempty"`
	Since           string `json:"since,omitempty"`
	Limit           int    `json:"limit,omitempty"`
	Search          string `json:"search,omitempty"`
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
