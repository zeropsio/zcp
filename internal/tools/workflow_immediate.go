package tools

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// synthesizeImmediateGuidance returns the atom-composed body for a stateless
// workflow. Currently only `export` — `cicd` retired in favour of
// `action=strategy strategies={X:push-git}` as the central deploy-config
// entry point.
func synthesizeImmediateGuidance(name string, _ *workflow.Engine, rt runtime.Info) (string, error) {
	phase, ok := immediatePhaseFor(name)
	if !ok {
		return "", platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Unknown immediate workflow %q", name),
			"Valid immediate workflows: export")
	}
	guidance, err := workflow.SynthesizeImmediateWorkflow(phase, workflow.DetectEnvironment(rt))
	if err != nil {
		return "", platform.NewPlatformError(
			platform.ErrNotImplemented,
			fmt.Sprintf("Synthesize %s guidance: %v", name, err),
			"")
	}
	return guidance, nil
}

// immediatePhaseFor maps an immediate-workflow name to its envelope Phase.
func immediatePhaseFor(name string) (workflow.Phase, bool) {
	if name == "export" {
		return workflow.PhaseExportActive, true
	}
	return "", false
}
