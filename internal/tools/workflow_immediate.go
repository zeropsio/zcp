package tools

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// synthesizeImmediateGuidance returns the atom-composed body for a stateless
// workflow (cicd or export). For cicd it additionally prepends a dynamic
// service-context header derived from ServiceMeta so the LLM sees the target
// services up front.
func synthesizeImmediateGuidance(name string, engine *workflow.Engine, rt runtime.Info) (string, error) {
	phase, ok := immediatePhaseFor(name)
	if !ok {
		return "", platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Unknown immediate workflow %q", name),
			"Valid immediate workflows: cicd, export")
	}
	guidance, err := workflow.SynthesizeImmediateWorkflow(phase, workflow.DetectEnvironment(rt))
	if err != nil {
		return "", platform.NewPlatformError(
			platform.ErrNotImplemented,
			fmt.Sprintf("Synthesize %s guidance: %v", name, err),
			"")
	}
	if name == "cicd" && engine != nil {
		if header := buildCICDContext(engine.StateDir()); header != "" {
			guidance = header + "\n\n---\n\n" + guidance
		}
	}
	return guidance, nil
}

// immediatePhaseFor maps an immediate-workflow name to its envelope Phase.
func immediatePhaseFor(name string) (workflow.Phase, bool) {
	switch name {
	case "cicd":
		return workflow.PhaseCICDActive, true
	case "export":
		return workflow.PhaseExportActive, true
	}
	return "", false
}
