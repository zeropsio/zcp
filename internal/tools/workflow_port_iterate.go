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
	if saveErr := workflow.SavePortSession(stateDir, ps); saveErr != nil {
		return convertError(saveErr, WithRecoveryStatus())
	}

	return jsonResult(map[string]any{
		"status":    "port-iterate",
		"phase":     string(workflow.PhasePortActive),
		"iteration": ps.Iteration,
		"fixClass":  fix,
		"attempt":   recorded,
		"guidance":  fix.Guidance,
	})
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
