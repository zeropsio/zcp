package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// requireWorkflow checks that a workflow session is active.
// Fails closed: returns error when engine is nil or no session exists.
func requireWorkflow(engine *workflow.Engine) *mcp.CallToolResult {
	if engine == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"Workflow engine unavailable — state directory could not be determined",
			"Ensure ZCP runs from a valid working directory",
		))
	}
	if engine.HasActiveSession() {
		return nil
	}
	return convertError(platform.NewPlatformError(
		platform.ErrWorkflowRequired,
		"No active workflow session. This tool requires a workflow session.",
		"Start a workflow: workflow=\"bootstrap\" (create/adopt infrastructure) or workflow=\"develop\" (develop/deploy/fix).",
	))
}
