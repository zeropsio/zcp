package tools

import (
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handlePortIterate is the agent-driven deploy-debug loop continuation (Stage
// A1). It is forked at the SAME dispatch seam develop's loop uses — the
// `action="iterate"` case in handleWorkflowAction (develop iterate re-derives
// state via the engine; port iterate re-derives the fix-class from the
// agent-observed failure). The loop is AGENT-DRIVEN: the agent runs the deploy
// via the EXISTING tools (zerops_deploy / zerops_import / zerops_env), observes
// the FailureClassification (class + signals), then calls THIS handler passing
// what it observed. The handler does NOT deploy — it loads the PortSession,
// derives the next fix-class via workflow.DeriveFixClass (reading the
// FailureClass FIRST, never parsing logs), records the attempt outcome on the
// port-owned PortSession (low blast radius — work_session's DeployAttempt is
// untouched), and returns the guidance for the agent's next turn.
func handlePortIterate(input WorkflowInput, stateDir string) *mcp.CallToolResult {
	if stateDir == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"Port workflow requires a state directory",
			"Ensure ZCP is configured with a state directory before iterating the port loop."), WithRecoveryStatus())
	}

	ps, err := workflow.LoadPortSession(stateDir, os.Getpid())
	if err != nil {
		return convertError(err, WithRecoveryStatus())
	}
	if ps == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			"No active port session for this process",
			`Start the port flow first with action="start" workflow="port" portTarget={...}. The deploy-debug loop iterates an existing PortSession.`), WithRecoveryStatus())
	}

	// Success path: the agent reports the deploy reached its target state this
	// turn. Record a non-failing attempt and steer toward the next rubric check.
	if bool(input.PortDeploySucceeded) {
		recorded := ps.RecordPortAttempt(workflow.PortAttempt{
			RecordedAt: time.Now().UTC().Format(time.RFC3339),
			Hostname:   input.TargetService,
			Succeeded:  true,
		})
		if saveErr := workflow.SavePortSession(stateDir, ps); saveErr != nil {
			return convertError(saveErr, WithRecoveryStatus())
		}
		return jsonResult(map[string]any{
			"status":    "port-iterate",
			"phase":     string(workflow.PhasePortActive),
			"iteration": ps.Iteration,
			"attempt":   recorded,
			"guidance":  "Deploy reached its target state. Continue with the next rubric check (boot stability hold / HTTP serve / harden) or close the port once the FitCeiling is measured.",
		})
	}

	// Failure path: the agent MUST report the observed FailureClass it read off
	// the live FailureClassification — the loop reads the class FIRST, never logs.
	class := topology.FailureClass(input.PortFailureClass)
	if class == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Port iterate requires the observed failure class",
			`Run the deploy via the existing tools (zerops_deploy / zerops_import), read the FailureClassification off the response, and pass portFailureClass=<build|start|verify|network|config|credential|other> + portSignals=[...] (the signal IDs from FailureClassification.signals). Or pass portDeploySucceeded=true when the deploy reached its target state.`), WithRecoveryStatus())
	}

	// Derive the fix-class. Prefer glue-zerops.yaml edits; when the agent flags
	// that the only available fix is an import.yaml edit to an existing hostname,
	// surface the override tax explicitly (it costs an iteration + wipes state).
	var fix workflow.PortFixClass
	if bool(input.PortImportOverride) {
		fix = workflow.DeriveImportOverrideFixClass(class, input.PortSignals)
	} else {
		fix = workflow.DeriveFixClass(class, input.PortSignals)
	}

	recorded := ps.RecordPortAttempt(workflow.PortAttempt{
		RecordedAt: time.Now().UTC().Format(time.RFC3339),
		Hostname:   input.TargetService,
		Class:      class,
		Signals:    append([]string(nil), input.PortSignals...),
		FixKind:    fix.Kind,
		Escalate:   fix.Escalate,
	})

	// Evaluate the loop terminators + escalation ladder over the now-updated
	// attempt history (Phase 2). The decision drives one of three outcomes:
	// continue-with-fix (T0), escalate-strategy + re-budget (T1), or stop-bail
	// (a terminator fired). The handler mutates the session (escalation switches
	// the acquisition strategy + sets the re-budget origin; the cap terminator
	// closes the wrapped work session) BEFORE the single persist.
	resp := portIterateDecision(ps, fix, recorded)

	if saveErr := workflow.SavePortSession(stateDir, ps); saveErr != nil {
		return convertError(saveErr, WithRecoveryStatus())
	}
	return jsonResult(resp)
}

// portIterateDecision applies the Phase 2 terminator + escalation logic to the
// just-recorded failing turn and returns the agent-facing response body. It
// MUTATES ps in place (strategy switch + re-budget on T1; wrapped-work-session
// close on the iteration cap) so the caller's single SavePortSession persists
// everything. Kept as a helper so handlePortIterate stays under the cyclo/
// maintidx gates (the Phase 1 pattern).
func portIterateDecision(ps *workflow.PortSession, fix workflow.PortFixClass, recorded workflow.PortAttempt) map[string]any {
	now := time.Now().UTC()
	// progressRose is fed by the harden+score step (action="harden"), not by
	// iterate — a deploy-debug turn does not re-score the rubric. The seam stays
	// false here; the measured tier-rise breaks the phase stall on the harden turn.
	prog := workflow.EvaluatePortProgress(ps, now, false)
	esc := workflow.DecidePortEscalation(ps.Plan, ps.Attempts)

	resp := map[string]any{
		"status":     "port-iterate",
		"phase":      string(workflow.PhasePortActive),
		"iteration":  ps.Iteration,
		"fixClass":   fix,
		"attempt":    recorded,
		"classStall": prog.ClassStall,
		"phaseStall": prog.PhaseStall,
	}

	// T1 escalation: switch the acquisition strategy + re-budget so the cap
	// terminator measures a fresh sub-budget from this turn. Only when the loop
	// has NOT already tripped a stop terminator on this same turn.
	if !prog.Stop && esc.Tier == workflow.PortEscalateT1 {
		ps.Plan.Acquisition = esc.NewStrategy
		ps.RebudgetOrigin = ps.Iteration
		resp["escalation"] = esc
		resp["guidance"] = esc.Reason + ". Switch the acquisition strategy in the glue zerops.yaml (now: " +
			string(esc.NewStrategy) + ") and redeploy. The iteration budget was re-set so the new strategy is not starved."
		return resp
	}

	// A terminator fired → graceful stop. The iteration cap closes the wrapped
	// work session (the existing terminal close reason).
	if prog.Stop {
		resp["stop"] = true
		resp["terminator"] = string(prog.Terminator)
		if prog.Terminator == workflow.PortTermIterationCap {
			closePortWorkSessionOnCap(ps)
		}
		if esc.Tier == workflow.PortEscalateT2Bail {
			resp["escalation"] = esc
		}
		// Attach the measured FitCeiling at the bail/stop point (Phase 3). When
		// the loop scored one via action="harden" it is the honest partial report
		// the agent captures; otherwise the agent must harden+score before
		// capture.
		if ps.FitCeiling != nil {
			resp["fitCeiling"] = ps.FitCeiling
			resp["guidance"] = prog.Reason + " Stop the loop and capture the measured FitCeiling attached here; do NOT keep redeploying."
		} else {
			resp["guidance"] = prog.Reason + ` Stop the loop, then run action="harden" workflow="port" to score the partial FitCeiling before capture; do NOT keep redeploying.`
		}
		return resp
	}

	// T0 stay: apply the derived fix-class and continue.
	resp["guidance"] = fix.Guidance
	return resp
}

// closePortWorkSessionOnCap closes the PortSession's WRAPPED work session with
// CloseReasonIterationCap — the existing terminal close reason (NOT a new one).
// It mirrors workflow.closeWorkSessionOnCap, but operates on the wrapped session
// in-memory (the caller persists it via SavePortSession) rather than the
// current-PID disk work session, because the port loop owns its wrapped session.
// Idempotent: a session already closed is left untouched.
func closePortWorkSessionOnCap(ps *workflow.PortSession) {
	// Guard idempotency on CloseReason (not raw ClosedAt — that read is reserved
	// for the derive sites per the P5 invariant / TestNoRawClosedAtReads). The
	// cap close is the only writer of the wrapped session's CloseReason, so a
	// non-empty CloseReason means we already stamped it.
	if ps.WorkSession == nil || ps.WorkSession.CloseReason != "" {
		return
	}
	ps.WorkSession.ClosedAt = time.Now().UTC().Format(time.RFC3339)
	ps.WorkSession.CloseReason = workflow.CloseReasonIterationCap
}

// handlePortStatus surfaces a live PortSession on action="status" workflow="port".
// PhasePortActive falls through build_plan's empty-Plan case (like
// launch-production), so status returns no Plan — port has its own recovery
// envelope (workflow.BuildPortActiveRecovery, the launch_status_recovery.go
// model, OQ-5). When no session exists for this PID the agent is told to start
// the flow.
func handlePortStatus(stateDir string) *mcp.CallToolResult {
	if stateDir == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"Port workflow requires a state directory",
			"Ensure ZCP is configured with a state directory."), WithRecoveryStatus())
	}
	ps, err := workflow.LoadPortSession(stateDir, os.Getpid())
	if err != nil {
		return convertError(err, WithRecoveryStatus())
	}
	if ps == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			"No active port session for this process",
			`Start the port flow with action="start" workflow="port" portTarget={...}.`), WithRecoveryStatus())
	}
	return jsonResult(workflow.BuildPortActiveRecovery(ps))
}
