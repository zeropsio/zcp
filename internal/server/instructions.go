package server

import (
	"fmt"
	"os"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// Instructions delivered to the MCP client at server init are RUNTIME-ONLY.
// Static project rules (routing table, discovery floor, smells, env
// preamble, project idioms) live in CLAUDE.md (env-rendered at zcp init) —
// the strong-adherence surface. MCP init carries only what cannot be
// pre-rendered: per-server-start runtime context.
//
// Two runtime injections feed RuntimeContext:
//   - AdoptionNote — workflow.FormatAdoptionNote(LocalAutoAdopt result),
//     local env only. Empty when no auto-adopt fired or nothing was newly
//     adopted this run.
//   - StateHint — terse summary of active sessions for the current PID.
//     Surfaces live recipe / bootstrap / work sessions so the LLM doesn't
//     blindly call zerops_workflow start and earn an ErrSubagentMisuse on
//     its first tool call. Empty when no sessions are open for this PID.
//
// The strict static/runtime split is the architecture invariant pinned
// by TestBuildInstructions_NoStaticRulesLeak. See plans/instruction-
// surfaces-refactor.md for the full layered ownership model.

// RuntimeContext carries the per-server-start injections that go into the
// MCP `instructions` field. An empty RuntimeContext yields an empty MCP
// init payload, which is valid per MCP protocol and signals "nothing
// notable this run."
type RuntimeContext struct {
	AdoptionNote string
	StateHint    string
}

// BuildInstructions composes the MCP init payload from RuntimeContext.
// Returns "" when both fields are empty. When both are present, joins
// with a blank-line separator so the LLM sees them as two distinct
// notes.
func BuildInstructions(rc RuntimeContext) string {
	var parts []string
	if rc.AdoptionNote != "" {
		parts = append(parts, rc.AdoptionNote)
	}
	if rc.StateHint != "" {
		parts = append(parts, rc.StateHint)
	}
	return strings.Join(parts, "\n\n")
}

// ComposeStateHint builds the active-session summary line(s) for the
// current PID by reading the workflow session registry and the per-PID
// work session file. Returns "" when no sessions are open.
//
// Each line is human-readable, terse, and includes the next-action
// pointer (status call) so the LLM can act on the hint without further
// inference. Multiple active sessions for the same PID join with a
// blank-line separator.
//
// Performance: at most three filesystem reads (registry list, optional
// per-session state load for descriptive detail, work session file).
// Called once at server start.
func ComposeStateHint(stateDir string, pid int) string {
	if stateDir == "" {
		return ""
	}
	var lines []string

	sessions, _ := workflow.ListSessions(stateDir)
	for _, s := range sessions {
		if s.PID != pid {
			continue
		}
		switch s.Workflow {
		case workflow.WorkflowRecipe:
			lines = append(lines, fmt.Sprintf(
				"Active recipe session: %s. Use zerops_recipe action=\"status\" "+
					"for the next action — do NOT start zerops_workflow during "+
					"recipe authoring.",
				describeRecipeSession(stateDir, s)))
		case workflow.WorkflowBootstrap:
			lines = append(lines, fmt.Sprintf(
				"Active bootstrap session (%s). Use zerops_workflow "+
					"action=\"status\" to continue.",
				describeBootstrapSession(stateDir, s)))
		}
	}

	ws, wsErr := workflow.LoadWorkSession(stateDir, pid)
	if wsErr != nil {
		// Don't drop silently — a corrupt work-session file is the only
		// thing standing between the agent and its recovery primitive.
		// Logged to stderr so operators can see it before the agent's
		// first status call surfaces the same error with its
		// recovery Suggestion attached.
		fmt.Fprintf(os.Stderr,
			"zcp: state-hint skipped: load work session for pid=%d: %v\n",
			pid, wsErr)
	}
	if ws != nil && ws.ClosedAt == "" {
		lines = append(lines, fmt.Sprintf(
			"Open develop work session: %q on %v. Use "+
				"zerops_workflow action=\"status\" for current state.",
			ws.Intent, ws.Services))
	}

	return strings.Join(lines, "\n\n")
}

// describeRecipeSession returns "<slug> (phase=<phase>)" when the
// per-session state file is loadable, falling back to intent-or-id when
// it isn't. Bounded by one filesystem read; soft-fails on error so the
// state hint never blocks server startup.
func describeRecipeSession(stateDir string, s workflow.SessionEntry) string {
	state, err := workflow.LoadSessionByID(stateDir, s.SessionID)
	if err != nil || state == nil || state.Recipe == nil {
		if s.Intent != "" {
			return fmt.Sprintf("%q", s.Intent)
		}
		return s.SessionID
	}
	slug := state.Recipe.CurrentStepName()
	if state.Recipe.Plan != nil && state.Recipe.Plan.Slug != "" {
		slug = state.Recipe.Plan.Slug
	}
	step := state.Recipe.CurrentStepName()
	if step != "" {
		return fmt.Sprintf("%s (step=%s)", slug, step)
	}
	return slug
}

// describeBootstrapSession returns "route=<route>, step=<step>" when the
// per-session state file is loadable, falling back to intent-or-id when
// it isn't.
func describeBootstrapSession(stateDir string, s workflow.SessionEntry) string {
	state, err := workflow.LoadSessionByID(stateDir, s.SessionID)
	if err != nil || state == nil || state.Bootstrap == nil {
		if s.Intent != "" {
			return fmt.Sprintf("intent=%q", s.Intent)
		}
		return "session=" + s.SessionID
	}
	step := state.Bootstrap.CurrentStepName()
	route := string(state.Bootstrap.Route)
	if route == "" {
		route = "classic"
	}
	if step == "" {
		return fmt.Sprintf("route=%s", route)
	}
	return fmt.Sprintf("route=%s, step=%s", route, step)
}

// CurrentPID is wrapped for testability — tests substitute to control
// which PID drives ComposeStateHint and downstream callers.
var CurrentPID = os.Getpid
