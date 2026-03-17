package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

const baseInstructions = `ZCP manages Zerops PaaS infrastructure.`

const routingInstructions = `
IMPORTANT: All Zerops operations are managed through workflow sessions. Before writing ANY configuration (import.yml, zerops.yml) or application code, you MUST start a workflow session. The workflow provides env var discovery, correct file paths, and deploy sequencing. Writing code before starting the workflow leads to incorrect configurations that must be rewritten.

Workflow commands:
- Create services: zerops_workflow action="start" workflow="bootstrap" (ALWAYS start here for new services)
- Deploy code: zerops_workflow action="start" workflow="deploy"
- Debug issues: zerops_workflow action="start" workflow="debug"
- Scale: zerops_workflow action="start" workflow="scale"
- Configure: zerops_workflow action="start" workflow="configure"
- Monitor: zerops_discover
- Search docs: zerops_knowledge query="..."`

// BuildInstructions returns the MCP instructions message injected into the system prompt.
// It includes base + routing (first), workflow hint, runtime context, and project summary.
// stateDir is the workflow state directory; empty string means no hint.
func BuildInstructions(ctx context.Context, client platform.Client, projectID string, rt runtime.Info, stateDir string) string {
	var b strings.Builder

	// Section A: Base + routing instructions (FIRST — most important for tool selection).
	b.WriteString(baseInstructions)
	b.WriteString(routingInstructions)

	// Section B: Workflow hint (from local state file).
	if hint := buildWorkflowHint(stateDir); hint != "" {
		b.WriteString("\n\n")
		b.WriteString(hint)
	}

	// Section C: Runtime context.
	if rt.ServiceName != "" {
		fmt.Fprintf(&b, "\n\nYou are running inside the Zerops service '%s'. You manage services in the same project.", rt.ServiceName)
	}

	// Section D: Project summary (dynamic).
	if summary := buildProjectSummary(ctx, client, projectID, stateDir); summary != "" {
		b.WriteString("\n\n")
		b.WriteString(summary)
	}

	return b.String()
}

// buildWorkflowHint reads the registry and returns hints for all active sessions.
// Returns empty string on any error (graceful fallback).
func buildWorkflowHint(stateDir string) string {
	if stateDir == "" {
		return ""
	}
	sessions, err := workflow.ListSessions(stateDir)
	if err != nil || len(sessions) == 0 {
		return ""
	}

	var hints []string
	for _, s := range sessions {
		hint := fmt.Sprintf("Active workflow: %s", s.Workflow)

		// For bootstrap sessions, try to load full state for step detail.
		if s.Workflow == "bootstrap" {
			if state, loadErr := workflow.LoadSessionByID(stateDir, s.SessionID); loadErr == nil {
				if state.Bootstrap != nil && state.Bootstrap.Active {
					stepNum := state.Bootstrap.CurrentStep + 1
					total := len(state.Bootstrap.Steps)
					stepName := state.Bootstrap.CurrentStepName()
					hint += fmt.Sprintf(" (step %d/%d: %s)", stepNum, total, stepName)
				}
			}
		}
		hints = append(hints, hint)
	}
	return strings.Join(hints, "\n")
}

// buildProjectSummary calls the API to list services and detect project state,
// then uses the router for workflow offerings.
// Returns empty string on failure or nil client (graceful fallback).
func buildProjectSummary(ctx context.Context, client platform.Client, projectID, stateDir string) string {
	if client == nil || projectID == "" {
		return ""
	}

	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return ""
	}

	var b strings.Builder

	// List services.
	if len(services) > 0 {
		b.WriteString("Current services:\n")
		for _, svc := range services {
			if svc.IsSystem() {
				continue
			}
			fmt.Fprintf(&b, "- %s (%s) — %s\n",
				svc.Name,
				svc.ServiceStackTypeInfo.ServiceStackTypeVersionName,
				svc.Status)
		}
	}

	if len(services) == 0 {
		b.WriteString("Project is empty — no services configured yet.")
	}

	// Detect project state and route.
	projState, err := workflow.DetectProjectState(ctx, client, projectID)
	if err != nil {
		projState = workflow.StateUnknown
	}
	fmt.Fprintf(&b, "\nProject state: %s", projState)

	// State-specific warnings.
	switch projState {
	case workflow.StateFresh:
		// No warning needed for fresh projects.
	case workflow.StateUnknown:
		// No warning needed for unknown state.
	case workflow.StateConformant:
		b.WriteString("\nDo NOT delete existing services without explicit user approval.")
	case workflow.StateNonConformant:
		b.WriteString("\nDo NOT delete existing services without explicit user approval.")
	}

	// Build router input.
	var liveHostnames []string
	for _, svc := range services {
		if !svc.IsSystem() {
			liveHostnames = append(liveHostnames, svc.Name)
		}
	}

	var metas []*workflow.ServiceMeta
	if stateDir != "" {
		metas, _ = workflow.ListServiceMetas(stateDir) // best-effort
	}

	var activeSessions []workflow.SessionEntry
	if stateDir != "" {
		activeSessions, _ = workflow.ListSessions(stateDir) // best-effort
	}

	routerInput := workflow.RouterInput{
		ProjectState:   projState,
		ServiceMetas:   metas,
		ActiveSessions: activeSessions,
		LiveServices:   liveHostnames,
	}
	offerings := workflow.Route(routerInput)
	if formatted := workflow.FormatOfferings(offerings); formatted != "" {
		b.WriteString("\n")
		b.WriteString(formatted)
	}

	return b.String()
}
