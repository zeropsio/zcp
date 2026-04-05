package tools

import (
	"fmt"
	"os"
	"path/filepath"

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

// requireAdoption checks that all given hostnames are tracked by ServiceMeta
// (bootstrapped or adopted). Returns nil if all are known.
//
// Skips the check when:
//   - stateDir is empty (no state directory configured)
//   - services/ directory doesn't exist (no bootstrap ever ran — gate activates
//     once the first service is adopted, giving a clean migration path)
func requireAdoption(stateDir string, hostnames ...string) *mcp.CallToolResult {
	if stateDir == "" {
		return nil
	}
	// Gate activates only after first bootstrap creates the services directory.
	servicesDir := filepath.Join(stateDir, "services")
	if _, err := os.Stat(servicesDir); os.IsNotExist(err) {
		return nil
	}
	for _, h := range hostnames {
		if h == "" {
			continue
		}
		if !workflow.IsKnownService(stateDir, h) {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceNotFound,
				fmt.Sprintf("Service %q is not adopted by ZCP — deploy blocked", h),
				fmt.Sprintf("Adopt it first: zerops_workflow action=\"start\" workflow=\"bootstrap\" (with isExisting=true for %s)", h),
			))
		}
	}
	return nil
}
