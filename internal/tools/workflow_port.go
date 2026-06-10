package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handlePortStart is Phase 0 of the OSS port workflow: deterministic recon
// (Stage A0). The agent supplies a target descriptor (name, acquisition hint,
// declared deps, declared runtimes); the handler resolves the live schema
// catalog, runs workflow.ReconClassify, persists a PortSession sidecar, and
// returns the PortPlan + feasibility band. ZERO deploy, zero network beyond the
// schema fetch the cache already performs for every workflow.
//
// The PortSession wraps a WorkSession (the loop phases reuse work_session's
// record/load/save) and carries the recon PortPlan. Phase 1+ attaches the
// deploy-debug loop on top of this sidecar.
func handlePortStart(
	ctx context.Context,
	schemaCache *schema.Cache,
	input WorkflowInput,
	projectID, stateDir string,
	rt runtime.Info,
) (*mcp.CallToolResult, any, error) {
	if input.PortTarget == nil || input.PortTarget.Name == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Port workflow requires a target descriptor",
			`Pass portTarget={name, acquisitionHint, dependencies:[...], runtimes:[...]} on action="start" workflow="port". The agent researches the OSS off-platform and supplies the structured descriptor; recon classifies it into a PortPlan with no deploy.`,
		), WithRecoveryStatus()), nil, nil
	}

	var schemas *schema.Schemas
	if schemaCache != nil {
		schemas = schemaCache.Get(ctx)
	}

	plan := workflow.ReconClassify(*input.PortTarget, schemas)

	// Persist the recon plan in a PortSession sidecar so the loop phases can
	// resume from it. Best-effort: a missing stateDir (rare) skips persistence
	// rather than failing the recon — the plan is still returned to the agent.
	if stateDir != "" {
		ws := workflow.NewWorkSession(projectID, string(workflow.DetectEnvironment(rt)), "port "+input.PortTarget.Name, nil)
		ps := workflow.NewPortSession(ws, plan)
		if err := workflow.SavePortSession(stateDir, ps); err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}
	}

	return jsonResult(map[string]any{
		"status":   "recon",
		"phase":    string(workflow.PhasePortActive),
		"portPlan": plan,
		"guidance": portReconGuidance(plan),
	}), nil, nil
}

// portReconGuidance frames the recon estimate for the agent: it is an
// ESTIMATE, not the fit ceiling, and a bail is the one true refusal. Plain
// structural string (no atom corpus surface yet — Phase 5 may add one).
func portReconGuidance(plan workflow.PortPlan) string {
	switch plan.Band {
	case workflow.BandBail:
		return "Recon BAILED: this software needs Kubernetes runtime orchestration that Zerops cannot express as prepareCommands/initCommands. See constraints. No deploy attempted."
	case workflow.BandHard:
		return "Recon estimate: HARD. Acquisition + dep mapping below are an ESTIMATE, not the ceiling — the deploy-debug loop measures the true FitCeiling. Expect cross-service init ordering and deep OSS-internals knowledge."
	case workflow.BandMedium:
		return "Recon estimate: MEDIUM. One dependency has no managed equivalent (self-run). The deploy-debug loop will measure the true FitCeiling."
	case workflow.BandEasy:
		return "Recon estimate: EASY. Source/crane acquisition + all deps managed. The deploy-debug loop will measure the true FitCeiling."
	default:
		return "Recon estimate: EASY. Source/crane acquisition + all deps managed. The deploy-debug loop will measure the true FitCeiling."
	}
}
