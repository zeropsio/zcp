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

const containerEnvironment = `

## Your Role

You are the orchestrator. This container is the control plane — it does NOT serve user traffic, run application code, or host databases. Your job is to create, configure, deploy, and manage OTHER services in the project. All user-facing work happens on those services, never on this container.

### Code Access — Two Mechanisms

**SSHFS mount** (/var/www/{hostname}/): For reading and writing files only. Changes appear instantly on the service container. Use Read/Write/Edit tools normally.
IMPORTANT: /var/www/ (no hostname) is THIS container's own filesystem — not a service.

**SSH** (ssh {hostname} "command"): For ALL commands and processes on services. Package installs, builds, git operations, server management, debugging — everything that isn't file read/write goes through SSH.
Example: ssh appdev "cd /var/www && npm install"

Rule: If it's a file → mount. If it's a command → SSH. Running commands over the SSHFS network mount is orders of magnitude slower and may fail.

### Persistence
File edits on mount survive restarts but not deploys (deploy = new container, only deployFiles content persists). Deploy when: zerops.yml changes, clean rebuild needed, or promote dev → stage. Code-only changes on dev: just restart the server via SSH — no redeploy needed.

zerops_discover always returns the CURRENT state of all services. Call it whenever you need to refresh your understanding.`

const localEnvironment = `

## Your Role

You are managing a Zerops project from a local machine. Code is in the working directory. All infrastructure (services, databases, storage) lives on Zerops — you create and manage it through workflow sessions.

### Deployment
Push code to Zerops via zcli push. zerops.yml must be at repository root. Each deploy = full rebuild + new container.

zerops_discover always returns the CURRENT state of all services. Call it whenever you need to refresh your understanding.`

const routingInstructions = `
IMPORTANT: Zerops operations use two approaches depending on complexity:

workflow sessions — for multi-step operations that need orchestration:
- Create services: zerops_workflow action="start" workflow="bootstrap" (ALWAYS start here for new services)
- Deploy code: zerops_workflow action="start" workflow="deploy" (for push-dev strategy; manual strategy uses zerops_deploy directly)
- Debug issues: zerops_workflow action="start" workflow="debug"
- Configure (env vars, subdomains): zerops_workflow action="start" workflow="configure"
- CI/CD setup: zerops_workflow action="start" workflow="cicd"
- Check workflow state: zerops_workflow action="status" (use after context loss or to resume work)

Direct tools — for simple, isolated operations (no workflow needed):
- Scale a service: zerops_scale serviceHostname="..."
- Deploy directly (manual strategy): zerops_deploy targetService="..."
- Manage lifecycle (start/stop/restart/reload): zerops_manage action="..." serviceHostname="..."
- Search docs: zerops_knowledge query="..."
- Monitor state: zerops_discover

Before writing ANY configuration (import.yml, zerops.yml) or application code, you MUST start a workflow session. Workflows provide env var discovery, correct file paths, and deploy sequencing. For simple operational tasks (scaling, restarting, checking status, manual deploys), use tools directly.`

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

	// Section C: Environment concept — how code access and deploy work.
	if rt.InContainer {
		b.WriteString(containerEnvironment)
		if rt.ServiceName != "" {
			fmt.Fprintf(&b, "\nYou are running on the '%s' service. Other services in this project are yours to manage.", rt.ServiceName)
		}
	} else {
		b.WriteString(localEnvironment)
	}

	// Section D: Project summary (dynamic).
	if summary := buildProjectSummary(ctx, client, projectID, stateDir, rt.ServiceName); summary != "" {
		b.WriteString("\n\n")
		b.WriteString(summary)
	}

	return b.String()
}

// buildWorkflowHint reads the registry and returns hints for all sessions.
// Dead-PID sessions show as resumable with instructions. Returns empty on error.
func buildWorkflowHint(stateDir string) string {
	if stateDir == "" {
		return ""
	}
	sessions, err := workflow.ListSessions(stateDir)
	if err != nil || len(sessions) == 0 {
		return ""
	}

	alive, dead := workflow.ClassifySessions(sessions)

	var hints []string
	for _, s := range alive {
		hint := fmt.Sprintf("Active workflow: %s", s.Workflow)
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
	for _, s := range dead {
		hints = append(hints, fmt.Sprintf(
			"Resumable workflow: %s | intent: %q | session: %s\n  → Call zerops_workflow action=\"resume\" sessionId=\"%s\" to continue.",
			s.Workflow, s.Intent, s.SessionID, s.SessionID))
	}
	return strings.Join(hints, "\n")
}

// buildProjectSummary calls the API to list services and detect project state,
// then uses the router for workflow offerings.
// Returns empty string on failure or nil client (graceful fallback).
func buildProjectSummary(ctx context.Context, client platform.Client, projectID, stateDir, selfHostname string) string {
	if client == nil || projectID == "" {
		return ""
	}

	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return ""
	}

	var b strings.Builder

	// List services (exclude system services and self).
	var userServices int
	if len(services) > 0 {
		b.WriteString("Current services:\n")
		for _, svc := range services {
			if svc.IsSystem() || (selfHostname != "" && svc.Name == selfHostname) {
				continue
			}
			userServices++
			fmt.Fprintf(&b, "- %s (%s) — %s\n",
				svc.Name,
				svc.ServiceStackTypeInfo.ServiceStackTypeVersionName,
				svc.Status)
		}
	}

	if userServices == 0 {
		b.WriteString("Project is empty — no services configured yet.")
	}

	// Detect project state and route.
	projState, err := workflow.DetectProjectState(ctx, client, projectID, selfHostname)
	if err != nil {
		projState = workflow.StateUnknown
	}
	fmt.Fprintf(&b, "\nProject state: %s", projState)

	// State-specific guidance — tell the LLM what to do, not just the state name.
	switch projState {
	case workflow.StateFresh:
		b.WriteString("\nNo user services yet. Start with bootstrap to create your first services.")
	case workflow.StateConformant:
		b.WriteString("\nServices are set up. Deploy code changes or add new services via bootstrap.")
		b.WriteString("\nDo NOT delete existing services without explicit user approval.")
	case workflow.StateNonConformant:
		b.WriteString("\nExisting services found but not in standard dev+stage pattern. Ask the user how to proceed.")
		b.WriteString("\nDo NOT delete existing services without explicit user approval.")
	case workflow.StateUnknown:
		// No guidance — state couldn't be determined.
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

	// If bootstrapped metas exist, generate rich per-service orientation.
	if orientation := buildPostBootstrapOrientation(metas, services, selfHostname); orientation != "" {
		b.WriteString("\n")
		b.WriteString(orientation)
		return b.String()
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
