package workflow

import "strconv"

// PortActiveEnvelope is the lifecycle-recovery shape surfaced when
// action="status" discovers a live PortSession for the current PID. Like
// launch-production (PhaseLaunchProductionActive), PhasePortActive falls through
// build_plan's empty-Plan case, so action="status" returns no Plan — the port
// flow needs its own recovery envelope (the launch_status_recovery.go model,
// OQ-5). NET-NEW: it does not reuse the recipe-engine EngineVersion mechanism;
// the PortSession's (pid,startTime) identity is the only staleness guard, and
// the envelope is derived purely from the saved session.
//
// The shape is small + self-describing: the agent reads `kind` to route, reads
// `band`/`acquisition`/`iteration` to re-orient, and uses `nextCall` to resume
// the deploy-debug loop with a single follow-up. The most recent attempt's
// derived fix-class is surfaced so a compaction-recovered agent knows what its
// last turn concluded without re-deploying.
type PortActiveEnvelope struct {
	Kind        string          `json:"kind"`
	Workflow    string          `json:"workflow"`
	Phase       Phase           `json:"phase"`
	Target      string          `json:"target"`
	Band        FeasibilityBand `json:"band"`
	Acquisition string          `json:"acquisition"`
	Iteration   int             `json:"iteration"`
	// LastAttempt is the most recent recorded loop-turn outcome (nil when the
	// loop has not deployed yet — fresh recon). Lets the agent see the last
	// derived fix-class without re-deploying.
	LastAttempt *PortAttempt `json:"lastAttempt,omitempty"`
	// Guidance frames the resume — what the loop is mid-flight on and the
	// next semantic action (apply the last fix, then iterate).
	Guidance string `json:"guidance"`
	// NextCall is a copy-pasteable resume call.
	NextCall string `json:"nextCall"`
}

// BuildPortActiveRecovery derives the recovery envelope from a saved PortSession.
// Pure: given the same session it returns the same envelope (deterministic for
// compaction-recovery byte-stability). Mirrors renderLaunchActiveRecovery's
// shape; the tools layer wraps the result in jsonResult.
//
// ps must be non-nil (the caller guards the not-found case before calling).
func BuildPortActiveRecovery(ps *PortSession) PortActiveEnvelope {
	env := PortActiveEnvelope{
		Kind:        "port-active",
		Workflow:    WorkflowPort,
		Phase:       PhasePortActive,
		Target:      ps.Plan.Target,
		Band:        ps.Plan.Band,
		Acquisition: string(ps.Plan.Acquisition),
		Iteration:   ps.Iteration,
		NextCall:    `zerops_workflow action="iterate" workflow="port"`,
	}

	if n := len(ps.Attempts); n > 0 {
		last := ps.Attempts[n-1]
		env.LastAttempt = &last
	}

	env.Guidance = portRecoveryGuidance(ps, env.LastAttempt)
	return env
}

// portRecoveryGuidance frames the mid-flight resume. The branches cover the
// three states a recovered session can be in: never-deployed (fresh recon),
// mid-loop with a pending fix, or escalation-flagged.
func portRecoveryGuidance(ps *PortSession, last *PortAttempt) string {
	if last == nil {
		return "Port deploy-debug loop is mid-flight (no deploy recorded yet — fresh recon). The PortPlan is set; run your first deploy via the existing tools (zerops_deploy / zerops_import), observe the FailureClassification, then call action=\"iterate\" workflow=\"port\" with the observed class + signals."
	}
	if last.Succeeded {
		return "Port deploy-debug loop's last recorded turn SUCCEEDED. The deployment reached its target state on iteration " + strconv.Itoa(last.Iteration) + ". Continue with the next rubric check (harden / verify) or close the port when the FitCeiling is measured."
	}
	if last.Escalate {
		return "Port deploy-debug loop's last turn flagged an ESCALATION (in-band unfixable, e.g. build OOM). Do NOT redeploy unchanged — switch acquisition strategy or bail with a partial FitCeiling. Last failure class: " + string(last.Class) + "."
	}
	return "Port deploy-debug loop is mid-flight on iteration " + strconv.Itoa(ps.Iteration) + ". Last failure class=" + string(last.Class) + ", derived fix=" + string(last.FixKind) + ". Apply that fix via the existing tools, redeploy, observe the new FailureClassification, then call action=\"iterate\" workflow=\"port\" with the new class + signals."
}
