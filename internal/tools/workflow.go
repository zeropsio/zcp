package tools

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// WorkflowInput is the input type for zerops_workflow.
type WorkflowInput struct {
	Workflow string `json:"workflow,omitempty"`
}

// RegisterWorkflow registers the zerops_workflow tool.
func RegisterWorkflow(srv *mcp.Server, client platform.Client, cache *ops.StackTypeCache) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_workflow",
		Description: "Get step-by-step workflow guidance. Without workflow param returns catalog of available workflows.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Get workflow guidance",
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input WorkflowInput) (*mcp.CallToolResult, any, error) {
		if input.Workflow == "" {
			return textResult(ops.GetWorkflowCatalog()), nil, nil
		}
		content, err := ops.GetWorkflow(input.Workflow)
		if err != nil {
			return convertError(err), nil, nil
		}

		// Inject live stack types into bootstrap/deploy workflows.
		if (input.Workflow == "bootstrap" || input.Workflow == "deploy") && client != nil && cache != nil {
			if types := cache.Get(ctx, client); len(types) > 0 {
				stackList := knowledge.FormatStackList(types)
				content = injectStacks(content, stackList)
			}
		}

		return textResult(content), nil, nil
	})
}

// injectStacks inserts the stack list section into workflow content.
// Replaces content between STACKS markers if present, otherwise inserts before "## Phase 1".
func injectStacks(content, stackList string) string {
	const beginMarker = "<!-- STACKS:BEGIN -->"
	const endMarker = "<!-- STACKS:END -->"

	if beginIdx := strings.Index(content, beginMarker); beginIdx >= 0 {
		if endIdx := strings.Index(content, endMarker); endIdx > beginIdx {
			return content[:beginIdx] + stackList + content[endIdx+len(endMarker):]
		}
	}

	// Fallback: insert before "## Phase 1"
	const anchor = "## Phase 1"
	if idx := strings.Index(content, anchor); idx > 0 {
		return content[:idx] + stackList + "\n---\n\n" + content[idx:]
	}

	return content
}
