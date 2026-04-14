package tools

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// requireWorkflowContext checks that the agent is in an active workflow:
// either a bootstrap/recipe session OR a develop marker for the current process.
// Used by mount and import to ensure the agent has received knowledge before
// performing infrastructure operations.
func requireWorkflowContext(engine *workflow.Engine, stateDir string) *mcp.CallToolResult {
	if engine != nil && engine.HasActiveSession() {
		return nil
	}
	if workflow.HasDevelopMarker(stateDir) {
		return nil
	}
	return convertError(platform.NewPlatformError(
		platform.ErrWorkflowRequired,
		"No active workflow. This tool requires a workflow context.",
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
