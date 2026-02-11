package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// EventsInput is the input type for zerops_events.
type EventsInput struct {
	ServiceHostname string `json:"serviceHostname,omitempty"`
	Limit           int    `json:"limit,omitempty"`
}

// RegisterEvents registers the zerops_events tool.
func RegisterEvents(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_events",
		Description: "Fetch project activity timeline. Aggregates processes and build/deploy events sorted by time.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input EventsInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.Events(ctx, client, projectID, input.ServiceHostname, input.Limit)
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(result), nil, nil
	})
}
