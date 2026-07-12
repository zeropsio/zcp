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
	// TokenAcquisition + MintedTokenName are the NON-SECRET delegated-
	// launch forensics (D-7): after a delegated mint the user's dashboard
	// carries a standing token under MintedTokenName even when the launch
	// later failed — a post-compaction status must still be able to name
	// it. Mode + name only; the token VALUE never enters state (P-LP-1).
	TokenAcquisition string `json:"tokenAcquisition,omitempty"`
	MintedTokenName  string `json:"mintedTokenName,omitempty"`
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
		guidance = `Launch-production workflow is mid-flight. Re-call zerops_workflow action="start" workflow="launch-production" with the same productionProjectName to advance. At ready-to-launch follow delegatedLaunch.available: confirmLaunch=true when a delegation is available, launchKey as the fallback.`
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
		// Delegated forensics matter on the NON-terminal path too: a
		// crash between the pre-mint state write and the abort leaves a
		// `launching` state whose TokenAcquisition/MintedTokenName are
		// the only surviving record that a standing dashboard token may
		// exist (D-7). Names/modes only — never values.
		TokenAcquisition: active.TokenAcquisition,
		MintedTokenName:  active.MintedTokenName,
		Guidance:         guidance,
		NextCall:         `zerops_workflow action="start" workflow="launch-production" productionProjectName="` + active.TargetProjectName + `"`,
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
			// Mirror renderLaunchTerminalRecovery's retryability split:
			// only a failed launch WITH a target project needs the
			// destructive reset; a no-target failure retries directly.
			// The staged-token-reuse claim requires the prior attempt to
			// have REACHED staging (hostname persisted) — a pre-staging
			// delegated abort gets the plain retry line.
			switch {
			case recent.TargetProjectID != "":
				s += "Use `zerops_workflow action=\"reset\"` to clear state before retrying.\n"
			case recent.TokenAcquisition == tokenAcquisitionDelegated && recent.TargetServiceHostname != "":
				s += "Retry directly: `zerops_workflow action=\"start\" workflow=\"launch-production\" productionProjectName=\"" +
					recent.TargetProjectName + "\" confirmLaunch=true` (the staged token from the prior attempt is reused).\n"
			case recent.TokenAcquisition == tokenAcquisitionDelegated:
				s += "Retry directly: `zerops_workflow action=\"start\" workflow=\"launch-production\" productionProjectName=\"" +
					recent.TargetProjectName + "\" confirmLaunch=true` (no token was staged; ZCP re-checks delegation availability and otherwise falls back to the manual path).\n"
			default:
				s += "Retry directly: `zerops_workflow action=\"start\" workflow=\"launch-production\" productionProjectName=\"" +
					recent.TargetProjectName + "\"`\n"
			}
		}
		return s
	}
	return ""
}

// renderLaunchTerminalRecovery surfaces a terminal launch-production
// state (launched / failed) on `action="status"` instead of returning
// generic `idle`. FIX 1 PR 1 closure.
//
// Failed state splits on retryability: with a target project the next
// action is `action="reset"` (blind retry would hit
// projectEnvDuplicateKey); with NO target the state is directly
// retryable and the guidance directs the retry (delegated path:
// confirmLaunch=true; staged-token reuse promised only when staging was
// reached) — reset there is abandonment and deletes any staged token.
//
// Launched state: provides the launchID + targetProjectID + brief
// "launch complete" envelope. Agent learns the launch already finished
// (e.g. compaction lost the launched response).
func renderLaunchTerminalRecovery(corpus []workflow.KnowledgeAtom, terminal *launchState) *mcp.CallToolResult {
	const kindLaunchFailed = "launch-failed"
	kind := "launch-terminal"
	//exhaustive:ignore — only terminal statuses (Failed/Launched) reach
	// this renderer; non-terminal statuses are gated upstream by
	// isTerminalLaunchStatus and the fallback "launch-terminal" kind keeps
	// the response shape predictable for any future mis-dispatch.
	switch terminal.Status {
	case topology.LaunchStatusFailed:
		kind = kindLaunchFailed
	case topology.LaunchStatusLaunched:
		kind = "launch-completed"
	}

	guidance := atomBody(corpus, "launch-status-recovery")
	if guidance == "" {
		if terminal.Status == topology.LaunchStatusFailed {
			guidance = `Launch-production reached terminal "failed" state. Follow nextCall for the recovery path. Inspect the state file (.zcp/state/launch-production/<launchID>.json) if the action is unavailable on your binary.`
		} else {
			guidance = `Launch-production completed (status="launched"). The production project is already created; no further start calls are required for this launchID. To TEAR the project DOWN instead (test launch, abandoned), use action="reset" workflow="launch-production" — it deletes the production project via the staged token (read server-side), the staged secret, and this state; never delete via zcli or the raw token value.`
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
		TokenAcquisition:      terminal.TokenAcquisition,
		MintedTokenName:       terminal.MintedTokenName,
		Guidance:              guidance,
	}
	if terminal.Status == topology.LaunchStatusFailed {
		// Failed splits on retryability (the resume-gate branches): with a
		// target project a direct retry is refused (partial project
		// exists) — reset is required; with NO target the state is
		// directly retryable, and steering a retryable DELEGATED failure
		// into reset would delete the staged token while the one-time
		// delegation is already consumed (2026-07-12 audit #1).
		retryable := terminal.TargetProjectID == ""
		delegated := terminal.TokenAcquisition == tokenAcquisitionDelegated
		// staged: the prior attempt REACHED staging (P1 persists the push
		// hostname exactly then) — only that state may promise staged-
		// token reuse. A pre-staging abort (race/indeterminate/empty-
		// token/admin-factory) has no staged token; claiming one would
		// be the lie-class (Codex review, 2026-07-12).
		staged := terminal.TargetServiceHostname != ""
		switch {
		case retryable && delegated && staged:
			env.NextCall = `zerops_workflow action="start" workflow="launch-production" productionProjectName="` + terminal.TargetProjectName + `" confirmLaunch=true`
			env.Guidance += "\n\nThis failed delegated launch is directly retryable: re-call per nextCall — the launch token staged by the prior attempt is reused (no delegation is consumed; none remains on this token). " +
				`Use action="reset" only to ABANDON the launch: reset deletes the staged token, and the manual dashboard walkthrough becomes the only remaining path.`
			if terminal.MintedTokenName != "" {
				env.Guidance += " A standing token named \"" + terminal.MintedTokenName + "\" exists in the Zerops dashboard (tokens outlive the launch; remove it there if you abandon)."
			}
		case retryable && delegated:
			env.NextCall = `zerops_workflow action="start" workflow="launch-production" productionProjectName="` + terminal.TargetProjectName + `" confirmLaunch=true`
			env.Guidance += "\n\nThis failed delegated launch is directly retryable: re-call per nextCall — ZCP first probes for a token staged by a prior attempt, then re-checks delegation availability; when neither resolves, the response falls back to the manual launchKey path."
			if terminal.MintedTokenName != "" {
				env.Guidance += " The mint attempt may have created a standing token named \"" + terminal.MintedTokenName + "\" in the Zerops dashboard — if it exists there, ask the user to regenerate it and re-call with launchKey."
			}
		case retryable:
			env.NextCall = `zerops_workflow action="start" workflow="launch-production" productionProjectName="` + terminal.TargetProjectName + `"`
			env.Guidance += "\n\nThis failed launch is directly retryable: re-call per nextCall and follow the ready-to-launch response (confirmLaunch=true when it advertises an available delegation, launchKey otherwise). " +
				`action="reset" abandons the launch instead.`
		default:
			env.NextCall = `zerops_workflow action="reset" workflow="launch-production" productionProjectName="` + terminal.TargetProjectName + `"`
			if delegated && terminal.MintedTokenName != "" {
				env.Guidance += "\n\nNote: the launch token was delegated-minted; a standing token named \"" + terminal.MintedTokenName + "\" remains in the Zerops dashboard (reset cannot delete tokens — remove it there when abandoning)."
			}
		}
	}
	return jsonResult(env)
}
