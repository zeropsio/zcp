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
Zerops operations are managed through workflow sessions.
For any operation (creating services, deploying code, debugging), start with zerops_workflow.
zerops_import and zerops_deploy require an active workflow session and will reject calls without one.

Workflow commands:
- Create services: zerops_workflow action="start" workflow="bootstrap" mode="full"
- Deploy code: zerops_workflow action="start" workflow="deploy" mode="full"
- Debug issues: zerops_workflow action="start" workflow="debug" mode="quick"
- Scale: zerops_workflow action="start" workflow="scale" mode="quick"
- Configure: zerops_workflow action="start" workflow="configure" mode="quick"
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
	if summary := buildProjectSummary(ctx, client, projectID); summary != "" {
		b.WriteString("\n\n")
		b.WriteString(summary)
	}

	return b.String()
}

// buildWorkflowHint reads the local workflow state and returns a 1-line hint.
// Returns empty string on any error (graceful fallback).
func buildWorkflowHint(stateDir string) string {
	if stateDir == "" {
		return ""
	}
	state, err := workflow.LoadSession(stateDir)
	if err != nil {
		return ""
	}

	hint := fmt.Sprintf("Active workflow: %s mode=%s phase=%s", state.Workflow, state.Mode, state.Phase)
	if state.Bootstrap != nil && state.Bootstrap.Active {
		stepNum := state.Bootstrap.CurrentStep + 1
		total := len(state.Bootstrap.Steps)
		stepName := state.Bootstrap.CurrentStepName()
		hint += fmt.Sprintf(" (step %d/%d: %s)", stepNum, total, stepName)
	}
	return hint
}

// buildProjectSummary calls the API to list services and detect project state.
// Returns empty string on failure or nil client (graceful fallback).
func buildProjectSummary(ctx context.Context, client platform.Client, projectID string) string {
	if client == nil || projectID == "" {
		return ""
	}

	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return ""
	}

	if len(services) == 0 {
		return "Project is empty — no services configured yet.\nREQUIRED: zerops_workflow action=\"start\" workflow=\"bootstrap\" mode=\"full\""
	}

	var b strings.Builder
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

	projState, err := workflow.DetectProjectState(ctx, client, projectID)
	if err == nil {
		fmt.Fprintf(&b, "\nProject state: %s", projState)
		switch projState {
		case workflow.StateFresh:
			b.WriteString("\nREQUIRED: zerops_workflow action=\"start\" workflow=\"bootstrap\" mode=\"full\"")
		case workflow.StateConformant:
			b.WriteString("\nREQUIRED: zerops_workflow action=\"start\" workflow=\"deploy\" mode=\"full\"")
		case workflow.StateNonConformant:
			b.WriteString("\nREQUIRED: zerops_workflow action=\"start\" workflow=\"bootstrap\" mode=\"full\"")
		}
	}

	return b.String()
}
