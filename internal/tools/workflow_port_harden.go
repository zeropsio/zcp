package tools

import (
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handlePortHarden is the Stage A2 harden + score step (Phase 3). After the
// deploy-debug loop has the app building/booting/serving, the agent runs the
// harden probes via the EXISTING tools — write a persistence sentinel → redeploy
// → re-read; inject readiness/health; scale ≥2 + verify HA — and reports what it
// OBSERVED. This handler does NOT deploy or call ops: it grades the agent-reported
// rubric observations (the loop can't understand a foreign app's health endpoint,
// so the agent authors + observes the probes), builds the measured FitCeiling,
// and persists it on the PortSession. The handler is the agent-driven mirror of
// iterate — guidance + checkpoints out, observed results in, pure grader in the
// middle.
func handlePortHarden(input WorkflowInput, stateDir string) *mcp.CallToolResult {
	if stateDir == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"Port workflow requires a state directory",
			"Ensure ZCP is configured with a state directory before hardening the port."), WithRecoveryStatus())
	}

	ps, err := workflow.LoadPortSession(stateDir, os.Getpid())
	if err != nil {
		return convertError(err, WithRecoveryStatus())
	}
	if ps == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			"No active port session for this process",
			`Start the port flow first with action="start" workflow="port", run the deploy-debug loop to building/booting/serving, then action="harden".`), WithRecoveryStatus())
	}

	// No rubric observations yet → return the harden PLAN (the checkpoints the
	// agent must run before it can report results). This is the guidance surface.
	if input.PortRubric == nil {
		hp := workflow.PlanHarden(ps.Plan)
		return jsonResult(map[string]any{
			"status":     "port-harden-plan",
			"phase":      string(workflow.PhasePortActive),
			"hardenPlan": hp,
			"guidance":   "Run these harden checkpoints via the existing tools, then call action=\"harden\" workflow=\"port\" again with portRubric={...} reporting what you OBSERVED: (1) build/boot/serve — confirm C1/C2/C3 after a STABILITY HOLD (an ACTIVE-then-exit / crash-loop is NOT stable). (2) author a core-flow probe (C4) — the loop can't infer a foreign app's health endpoint. (3) per durable dependency, write a sentinel → redeploy → re-read (C5; the container FS is ephemeral — assert on the managed/storage surface). (4) scale ≥2 + flip mode-bearing managed deps to HA + verify (C6; throughput-scaling is DISTINCT from HA replication).",
		})
	}

	fc := scorePortFitCeiling(ps, input)

	// progressRose: did the measured honored-tier RISE versus the previously
	// scored ceiling? This is the Phase 2 phase-stall seam (default false) that
	// Phase 3 now feeds — a rising ceiling means the loop advanced this turn even
	// if the fix-class phase did not, so it breaks the phase-stall streak.
	progressRose := measuredTierRose(ps, fc)

	ps.FitCeiling = &fc
	if saveErr := workflow.SavePortSession(stateDir, ps); saveErr != nil {
		return convertError(saveErr, WithRecoveryStatus())
	}

	return jsonResult(map[string]any{
		"status":       "port-harden",
		"phase":        string(workflow.PhasePortActive),
		"fitCeiling":   fc,
		"progressRose": progressRose,
		"guidance":     portHardenGuidance(fc),
	})
}

// scorePortFitCeiling grades the agent-reported rubric observations into a
// measured FitCeiling. Pure-ish glue: it adapts the wire input to the workflow
// graders + builder (all pure). Kept as a helper so handlePortHarden stays under
// the cyclo/maintidx gates (the prior-phase pattern).
func scorePortFitCeiling(ps *workflow.PortSession, input WorkflowInput) workflow.FitCeiling {
	r := input.PortRubric
	var hr workflow.HardenResults
	if r.Harden != nil {
		hr = workflow.HardenResults{
			SentinelSurvivedRedeploy: bool(r.Harden.SentinelSurvivedRedeploy),
			SentinelOnDurableSurface: bool(r.Harden.SentinelOnDurableSurface),
			AppContainers:            r.Harden.AppContainers,
			HADeps:                   r.Harden.HADeps,
			HAVerifyPassed:           bool(r.Harden.HAVerifyPassed),
		}
	}
	// Derive the FILTERED HA breakdown ONCE, then grade C6 off it — the grade and
	// the emitted topology (Phase 4) consume the same ManagedHADeps list, so they
	// cannot disagree (a bogus/storage/unplanned haDeps entry is dropped here).
	ha := workflow.DeriveAchievableHA(ps.Plan, hr.AppContainers, hr.HADeps)
	c5, c6 := workflow.GradeHarden(hr, ha)

	rubric := workflow.PortRubric{Grades: []workflow.PortGrade{
		workflow.C1Builds(bool(r.BuildSucceeded), bool(r.BuildHadWarnings)),
		workflow.C2BootsStable(bool(r.ReachedActive), bool(r.StableAfterHold)),
		workflow.C3ServesHTTP(bool(r.HTTPRootPassed)),
		workflow.C4CoreFlow(bool(r.CoreFlowProbePassed)),
		c5,
		c6,
	}}

	in := workflow.BuildFitCeilingInput{
		Plan:             ps.Plan,
		Rubric:           rubric,
		HA:               ha,
		FinalAcquisition: ps.Plan.Acquisition,
		ExtraConstraints: input.PortUnresolved,
	}
	if g := input.PortGlueRepo; g != nil {
		in.Glue = workflow.GlueRepo{
			URL:               g.URL,
			CommittedSHA:      g.CommittedSHA,
			BuildFromGitReady: bool(g.BuildFromGitReady),
		}
	}
	return workflow.BuildFitCeiling(in)
}

// measuredTierRose reports whether the new FitCeiling's honored ceiling is higher
// than the session's previously-scored ceiling. A first feasible score (no prior
// ceiling) counts as a rise.
func measuredTierRose(ps *workflow.PortSession, next workflow.FitCeiling) bool {
	if !next.Feasible {
		return false
	}
	prev, hadPrev := ps.MeasuredCeiling()
	if !hadPrev {
		return true
	}
	return next.MeasuredCeiling > prev
}

// portHardenGuidance frames the measured FitCeiling for the agent at the Stage A
// checkpoint.
func portHardenGuidance(fc workflow.FitCeiling) string {
	if !fc.Feasible {
		return "Measured FitCeiling: INFEASIBLE — the build/boot/serve gate (C1/C2/C3) did not pass, so no deployment tier is honored. Read whatDoesnt + the honored-tier reasons, fix the gate via the deploy-debug loop (action=\"iterate\"), then re-harden. Do NOT publish an infeasible port."
	}
	return "Measured FitCeiling scored. This is the HONEST report: whatRuns / whatDoesnt and the per-tier honored verdicts (each excluded tier carries its reason — ship a tier ONLY if its rubric prerequisite is met). The measured ceiling is the truth; the recon band was only an estimate. Stage A stops here at the checkpoint — capture (Phase 4) publishes the honored tiers + glue-repo separately so a human can inspect the port first."
}
