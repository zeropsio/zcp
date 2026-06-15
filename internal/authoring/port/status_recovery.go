package port

import "strconv"

// portPhaseActive is the agent-facing flow-state marker carried on every
// port response ("phase": "port-active"). It is port-tool-owned vocabulary —
// NOT a core lifecycle envelope Phase: the port flow never appears in the
// core StateEnvelope (the boundary keeps authoring out of core), so its
// status/recovery surface is this tool's own envelope.
const portPhaseActive = "port-active"

// PortActiveEnvelope is the recovery shape surfaced when action="status"
// discovers a live PortSession for the current PID. The core
// action="status" lifecycle envelope knows nothing about the port flow
// (authoring is invisible to core), so the port tool carries its own
// recovery envelope, derived purely from the saved session — the
// PortSession's (pid,startTime) identity is the only staleness guard.
//
// The shape is small + self-describing: the agent reads `kind` to route,
// reads `band`/`acquisition`/`iteration` to re-orient, and uses `nextCall`
// to resume the deploy-debug loop with a single follow-up. The most recent
// attempt's derived fix-class is surfaced so a compaction-recovered agent
// knows what its last turn concluded without re-deploying.
type PortActiveEnvelope struct {
	Kind        string          `json:"kind"`
	Tool        string          `json:"tool"`
	Phase       string          `json:"phase"`
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
// compaction-recovery byte-stability).
//
// ps must be non-nil (the caller guards the not-found case before calling).
func BuildPortActiveRecovery(ps *PortSession) PortActiveEnvelope {
	env := PortActiveEnvelope{
		Kind:        portPhaseActive,
		Tool:        toolName,
		Phase:       portPhaseActive,
		Target:      ps.Plan.Target,
		Band:        ps.Plan.Band,
		Acquisition: string(ps.Plan.Acquisition),
		Iteration:   ps.Iteration,
		NextCall:    toolName + ` action="iterate"`,
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
		return "Port deploy-debug loop is mid-flight (no deploy recorded yet — fresh recon). The PortPlan is set; run your first deploy via the existing tools (zerops_deploy / zerops_import), observe the FailureClassification, then call " + toolName + " action=\"iterate\" with the observed class + signals."
	}
	if last.Succeeded {
		return "Port deploy-debug loop's last recorded turn SUCCEEDED. The deployment reached its target state on iteration " + strconv.Itoa(last.Iteration) + ". Continue with the next rubric check (harden / verify) or close the port when the FitCeiling is measured."
	}
	if last.Escalate {
		return "Port deploy-debug loop's last turn flagged an ESCALATION (in-band unfixable, e.g. build OOM). Do NOT redeploy unchanged — switch acquisition strategy or bail with a partial FitCeiling. Last failure class: " + string(last.Class) + "."
	}
	return "Port deploy-debug loop is mid-flight on iteration " + strconv.Itoa(ps.Iteration) + ". Last failure class=" + string(last.Class) + ", derived fix=" + string(last.FixKind) + ". Apply that fix via the existing tools, redeploy, observe the new FailureClassification, then call " + toolName + " action=\"iterate\" with the new class + signals."
}
