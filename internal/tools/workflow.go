package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
)

// WorkflowInput is the input type for zerops_workflow.
type WorkflowInput struct {
	Workflow string `json:"workflow,omitempty"`
}

// RegisterWorkflow registers the zerops_workflow tool.
func RegisterWorkflow(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_workflow",
		Description: "Get step-by-step workflow guidance. Without workflow param returns catalog of available workflows.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Get workflow guidance",
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(false),
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, input WorkflowInput) (*mcp.CallToolResult, any, error) {
		if input.Workflow == "" {
			return textResult(ops.GetWorkflowCatalog()), nil, nil
		}
		content, err := ops.GetWorkflow(input.Workflow)
		if err != nil {
			return convertError(err), nil, nil
		}
		return textResult(content), nil, nil
	})
}
