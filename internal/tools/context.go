package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/update"
)

// ContextInput is the input type for zerops_context (no parameters).
type ContextInput struct{}

// RegisterContext registers the zerops_context tool.
func RegisterContext(srv *mcp.Server, client platform.Client, cache *ops.StackTypeCache, updateInfo *update.Info) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_context",
		Description: "Get Zerops platform overview — what Zerops is, project/service hierarchy, defaults, live service stacks. Optional orientation, not a prerequisite.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Get Zerops platform context",
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ ContextInput) (*mcp.CallToolResult, any, error) {
		result := ops.GetContext(ctx, client, cache)
		if updateInfo != nil && updateInfo.Available {
			result += fmt.Sprintf(
				"\n\n---\nNOTE: ZCP update available (%s → %s). "+
					"Ask the user to restart the session or run `zcp update` in terminal.",
				updateInfo.CurrentVersion, updateInfo.LatestVersion,
			)
		}
		return textResult(result), nil, nil
	})
}
