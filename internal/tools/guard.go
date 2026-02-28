package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// requireWorkflow checks that a workflow session is active.
// Returns nil (pass) when engine is nil (backward compat / --without-zerops-flow)
// or when a session exists. Returns an error result otherwise.
func requireWorkflow(engine *workflow.Engine) *mcp.CallToolResult {
	if engine == nil {
		return nil
	}
	if engine.HasActiveSession() {
		return nil
	}
	return convertError(platform.NewPlatformError(
		platform.ErrWorkflowRequired,
		"No active workflow session. This tool requires a workflow session.",
		"Start a workflow first: zerops_workflow action=\"start\" workflow=\"bootstrap\"",
	))
}
