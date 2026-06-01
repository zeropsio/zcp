package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// launchActiveEnvelope is the JSON shape surfaced when `action=status`
// discovers a non-terminal launch-production state file. Gives the
// agent enough information to resume the workflow with a single
// follow-up call.
//
// Shape stays small and self-describing — the agent reads `kind` to
// route, then uses `productionProjectName` to seed the next call. No
// launchKey field (P-LP-1) and no admin-side state.
type launchActiveEnvelope struct {
	Kind                  string                          `json:"kind"`
	Workflow              string                          `json:"workflow"`
	Status                topology.LaunchProductionStatus `json:"status"`
	LaunchID              string                          `json:"launchId"`
	SourceProjectID       string                          `json:"sourceProjectId"`
	TargetProjectName     string                          `json:"targetProjectName"`
	TargetProjectID       string                          `json:"targetProjectId,omitempty"`
	TargetServiceHostname string                          `json:"targetServiceHostname,omitempty"`
	LastUpdate            string                          `json:"lastUpdate,omitempty"`
	// AmbiguousChoices is populated when MORE than one active launch
	// state matches the source project. Each entry carries the minimum
	// info the agent needs to disambiguate.
	AmbiguousChoices []launchActiveChoice `json:"ambiguousChoices,omitempty"`
	Guidance         string               `json:"guidance"`
	NextCall         string               `json:"nextCall"`
}

// launchActiveChoice is one entry in the ambiguous-choices list.
type launchActiveChoice struct {
	TargetProjectName string                          `json:"targetProjectName"`
	Status            topology.LaunchProductionStatus `json:"status"`
	LastUpdate        string                          `json:"lastUpdate,omitempty"`
}

// renderLaunchActiveRecovery builds the launch-active envelope from a
// findActiveLaunchState result. When the caller passed a single match,
// the envelope points at that target directly. When more than one
// non-terminal state exists for the same source project, the envelope
// surfaces an ambiguity hint and asks the agent to specify
// productionProjectName.
//
// guidance is loaded from the launch-status-recovery atom; falls back
// to a static line when the corpus is unavailable.
func renderLaunchActiveRecovery(corpus []workflow.KnowledgeAtom, active *launchState, all []*launchState) *mcp.CallToolResult {
	guidance := atomBody(corpus, "launch-status-recovery")
	if guidance == "" {
		// Fallback explicitly carries action="start" — launch-production is
		// stateless multi-call narrowing, every advance is action="start"
		// with accumulated inputs. Older atoms / NextCall strings that
		// omitted action="start" led agents to guess action="classify"
		// (FIX 1 from eval root-cause review 2026-05-19).
		guidance = `Launch-production workflow is mid-flight. Re-call zerops_workflow action="start" workflow="launch-production" with the same productionProjectName to advance. Provide a fresh launchKey when the status is ready-to-launch.`
	}

	env := launchActiveEnvelope{
		Kind:                  "launch-active",
		Workflow:              workflowLaunchProduction,
		Status:                active.Status,
		LaunchID:              active.LaunchID,
		SourceProjectID:       active.SourceProjectID,
		TargetProjectName:     active.TargetProjectName,
		TargetProjectID:       active.TargetProjectID,
		TargetServiceHostname: active.TargetServiceHostname,
		LastUpdate:            formatLaunchStateTimestamp(active),
		Guidance:              guidance,
		NextCall:              `zerops_workflow action="start" workflow="launch-production" productionProjectName="` + active.TargetProjectName + `"`,
	}

	if len(all) > 1 {
		choices := make([]launchActiveChoice, 0, len(all))
		for _, s := range all {
			choices = append(choices, launchActiveChoice{
				TargetProjectName: s.TargetProjectName,
				Status:            s.Status,
				LastUpdate:        formatLaunchStateTimestamp(s),
			})
		}
		env.AmbiguousChoices = choices
		env.Guidance = guidance + " (Multiple active launches detected — pick one productionProjectName from ambiguousChoices.)"
	}

	return jsonResult(env)
}

// formatLaunchStateTimestamp returns the RFC3339 form of LastUpdate (or
// empty when zero), wrapped in a helper so the response builder reads
// cleanly.
func formatLaunchStateTimestamp(s *launchState) string {
	if s == nil || s.LastUpdate.IsZero() {
		return ""
	}
	return s.LastUpdate.UTC().Format("2006-01-02T15:04:05Z")
}

// launchOverlayAddendum returns a markdown overlay block describing an in-flight
// or recently-terminal launch-production for sourceProjectID, or "" when none
// exists. Launch state is PROJECT-scoped and recoverable by any PID, so it must
// surface on develop status too (plan I10): it is a uniform project element, NOT
// a preempting focus. Without it an open develop work session (FocusWork) hid
// the launch entirely — ComputeEnvelope lives in internal/workflow/ and cannot
// read this tools-layer state, so the develop status alone never mentions it.
// FocusIdle keeps its dedicated full recovery envelope (it has nothing else to
// show); this addendum is the coexistence (work + launch) surface.
func launchOverlayAddendum(stateDir, sourceProjectID string) string {
	if stateDir == "" || sourceProjectID == "" {
		return ""
	}
	if active, all, _ := findActiveLaunchState(stateDir, sourceProjectID); active != nil {
		s := "### Launch-production in flight (project overlay)\n\n" +
			"A launch-production for \"" + active.TargetProjectName + "\" is mid-flight (status=" +
			string(active.Status) + "). It is recoverable independently of this develop session.\n"
		if len(all) > 1 {
			s += "(Multiple active launches — pick one productionProjectName.)\n"
		}
		s += "Resume: `zerops_workflow action=\"start\" workflow=\"launch-production\" productionProjectName=\"" +
			active.TargetProjectName + "\"`\n"
		return s
	}
	if recent, _, _ := findRecentLaunchState(stateDir, sourceProjectID); recent != nil && isTerminalLaunchStatus(recent.Status) {
		s := "### Launch-production terminal (project overlay)\n\n" +
			"The most recent launch-production for \"" + recent.TargetProjectName + "\" ended " +
			string(recent.Status) + ".\n"
		if recent.Status == topology.LaunchStatusFailed {
			s += "Use `zerops_workflow action=\"reset\"` to clear state before retrying.\n"
		}
		return s
	}
	return ""
}

// renderLaunchTerminalRecovery surfaces a terminal launch-production
// state (launched / failed) on `action="status"` instead of returning
// generic `idle`. FIX 1 PR 1 closure.
//
// Failed state: agent gets the launchID + reset-required guidance so the
// next action is `action="reset"` (PR 2 surface) — not blind retry that
// hits projectEnvDuplicateKey or cached state. Until reset ships, the
// recovery message names the manual state-file path as escape hatch.
//
// Launched state: provides the launchID + targetProjectID + brief
// "launch complete" envelope. Agent learns the launch already finished
// (e.g. compaction lost the launched response).
func renderLaunchTerminalRecovery(corpus []workflow.KnowledgeAtom, terminal *launchState) *mcp.CallToolResult {
	kind := "launch-terminal"
	//exhaustive:ignore — only terminal statuses (Failed/Launched) reach
	// this renderer; non-terminal statuses are gated upstream by
	// isTerminalLaunchStatus and the fallback "launch-terminal" kind keeps
	// the response shape predictable for any future mis-dispatch.
	switch terminal.Status {
	case topology.LaunchStatusFailed:
		kind = "launch-failed"
	case topology.LaunchStatusLaunched:
		kind = "launch-completed"
	}

	guidance := atomBody(corpus, "launch-status-recovery")
	if guidance == "" {
		if terminal.Status == topology.LaunchStatusFailed {
			guidance = `Launch-production reached terminal "failed" state. Use action="reset" to clear state before retrying with a fresh launchKey. Inspect the state file (.zcp/state/launch-production/<launchID>.json) if reset action is unavailable on your binary.`
		} else {
			guidance = `Launch-production completed (status="launched"). The production project is already created; no further start calls are required for this launchID.`
		}
	}

	env := launchActiveEnvelope{
		Kind:                  kind,
		Workflow:              workflowLaunchProduction,
		Status:                terminal.Status,
		LaunchID:              terminal.LaunchID,
		SourceProjectID:       terminal.SourceProjectID,
		TargetProjectName:     terminal.TargetProjectName,
		TargetProjectID:       terminal.TargetProjectID,
		TargetServiceHostname: terminal.TargetServiceHostname,
		LastUpdate:            formatLaunchStateTimestamp(terminal),
		Guidance:              guidance,
	}
	if terminal.Status == topology.LaunchStatusFailed {
		env.NextCall = `zerops_workflow action="reset" workflow="launch-production" productionProjectName="` + terminal.TargetProjectName + `"`
	}
	return jsonResult(env)
}
