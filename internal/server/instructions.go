package server

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

const sshfsMountBase = "/var/www"

const baseInstructions = `ZCP manages Zerops PaaS infrastructure.`

const containerEnvironment = `

## Your Role

You are the orchestrator. This container is the control plane — it does NOT serve user traffic, run application code, or host databases. Your job is to create, configure, deploy, and manage OTHER services in the project. All user-facing work happens on those services, never on this container.

### Code Access — Two Mechanisms

**SSHFS mount** (/var/www/{hostname}/): Live service filesystems — these are NOT local files. Changes appear instantly on running containers. Use Read/Write/Edit tools normally.
IMPORTANT: /var/www/ (no hostname) is THIS container's own filesystem — not a service.
IMPORTANT: Before reading, debugging, auditing, or modifying any file under /var/www/{hostname}/, ALWAYS start a workflow session first (debug, bootstrap, or deploy). The workflow gives you platform context — runtime specifics, env var wiring, framework recipes, deploy constraints. Without it you are operating blind on a live service.

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

IMPORTANT: Before reading, debugging, auditing, or modifying code for any Zerops service, ALWAYS start a workflow session first (debug, bootstrap, or deploy). The workflow gives you platform context — runtime specifics, env var wiring, framework recipes, deploy constraints. Without it you are writing code without knowing how the platform will run it.

zerops_discover always returns the CURRENT state of all services. Call it whenever you need to refresh your understanding.`

const routingInstructions = `
IMPORTANT: Zerops operations use two approaches:

workflow sessions — for any work that involves service code (reading, debugging, fixing, writing, deploying):
- Investigate/fix bugs on a service: zerops_workflow action="start" workflow="debug"
- Create new services: zerops_workflow action="start" workflow="bootstrap"
- Adopt existing services into ZCP: zerops_workflow action="start" workflow="bootstrap" (isExisting=true)
- Deploy code: zerops_workflow action="start" workflow="deploy"
- Configure (env vars, subdomains): zerops_workflow action="start" workflow="configure"
- CI/CD setup: zerops_workflow action="start" workflow="cicd"
- Check workflow state: zerops_workflow action="status"

Direct tools — for simple operational tasks (no code changes):
- Scale a service: zerops_scale serviceHostname="..."
- Deploy directly (manual strategy): zerops_deploy targetService="..."
- Manage lifecycle (start/stop/restart/reload): zerops_manage action="..." serviceHostname="..."
- Search docs: zerops_knowledge query="..."
- Monitor state: zerops_discover

Before reading or modifying any file on a service, start a workflow session. Workflows inject platform knowledge (runtime docs, framework recipes, deploy constraints) and discover env vars. This applies to debugging, auditing, and fixing existing code — not just creating new services. For operational tasks that don't touch code (scaling, restarting, checking status), use tools directly.`

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

// serviceClassification categorizes project services into three buckets.
// This is the single classification point — orientation and router both consume it.
type serviceClassification struct {
	bootstrapped        []*workflow.ServiceMeta // runtime services with complete ServiceMeta
	unmanaged           []platform.ServiceStack // runtime services without complete ServiceMeta
	unmanagedNames      []string                // hostnames of unmanaged runtime services
	managed             []platform.ServiceStack // infrastructure services (db, cache, storage)
	allServices         []platform.ServiceStack // all user services for type/status lookup
	total               int                     // total user services (excluding system + self)
	mountPaths          map[string]string       // hostname → mount path (only for actually mounted services)
	metaMap             map[string]*workflow.ServiceMeta
	stageOfBootstrapped map[string]bool // stage hostnames of bootstrapped metas
}

// classifyServices splits live services into bootstrapped runtime, unmanaged runtime,
// and managed infrastructure. Self and system services are excluded.
func classifyServices(services []platform.ServiceStack, metas []*workflow.ServiceMeta, selfHostname string) serviceClassification {
	metaMap := make(map[string]*workflow.ServiceMeta, len(metas))
	stageOf := make(map[string]bool)
	for _, m := range metas {
		metaMap[m.Hostname] = m
		if m.IsComplete() && m.StageHostname != "" {
			stageOf[m.StageHostname] = true
		}
	}

	cls := serviceClassification{
		metaMap:             metaMap,
		stageOfBootstrapped: stageOf,
	}

	for _, svc := range services {
		if svc.IsSystem() || (selfHostname != "" && svc.Name == selfHostname) {
			continue
		}
		cls.total++
		cls.allServices = append(cls.allServices, svc)
		typeName := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName

		if workflow.IsManagedService(typeName) {
			cls.managed = append(cls.managed, svc)
			continue
		}

		// Runtime service — check meta.
		if m, ok := metaMap[svc.Name]; ok && m.IsComplete() {
			cls.bootstrapped = append(cls.bootstrapped, m)
		} else if stageOf[svc.Name] {
			// Stage of a bootstrapped service — not unmanaged.
			continue
		} else {
			cls.unmanagedNames = append(cls.unmanagedNames, svc.Name)
			cls.unmanaged = append(cls.unmanaged, svc)
		}
	}
	return cls
}

// detectMounts checks which services have SSHFS mounts at /var/www/{hostname}.
// Returns a map of hostname → mount path for services that are actually mounted.
func detectMounts(services []platform.ServiceStack) map[string]string {
	mounts := make(map[string]string)
	for _, svc := range services {
		path := sshfsMountBase + "/" + svc.Name
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			mounts[svc.Name] = path
		}
	}
	return mounts
}

// labelFor returns a classification label for the service listing.
func (c *serviceClassification) labelFor(hostname string) string {
	mount := ""
	if path, ok := c.mountPaths[hostname]; ok {
		mount = fmt.Sprintf(" — mounted at %s/", path)
	}

	if m, ok := c.metaMap[hostname]; ok && m.IsComplete() {
		return mount
	}
	if c.stageOfBootstrapped[hostname] {
		return mount
	}
	if _, ok := c.metaMap[hostname]; ok {
		return " — bootstrap incomplete" + mount
	}
	if slices.Contains(c.unmanagedNames, hostname) {
		return " — needs ZCP adoption" + mount
	}
	return mount
}

// buildProjectSummary calls the API to list services, classifies them, then
// generates orientation and router offerings. Returns empty string on failure.
func buildProjectSummary(ctx context.Context, client platform.Client, projectID, stateDir, selfHostname string) string {
	if client == nil || projectID == "" {
		return ""
	}

	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return ""
	}

	// Load metas (best-effort).
	var metas []*workflow.ServiceMeta
	if stateDir != "" {
		metas, _ = workflow.ListServiceMetas(stateDir) // best-effort
	}

	// Classify services and detect actual SSHFS mounts.
	cls := classifyServices(services, metas, selfHostname)
	cls.mountPaths = detectMounts(cls.allServices)

	var b strings.Builder

	// Service listing with classification labels.
	if cls.total == 0 {
		b.WriteString("Project is empty — no services configured yet.")
	} else {
		b.WriteString("Current services:\n")
		for _, svc := range services {
			if svc.IsSystem() || (selfHostname != "" && svc.Name == selfHostname) {
				continue
			}
			label := cls.labelFor(svc.Name)
			fmt.Fprintf(&b, "- %s (%s) — %s%s\n",
				svc.Name,
				svc.ServiceStackTypeInfo.ServiceStackTypeVersionName,
				svc.Status, label)
		}
		b.WriteString("\nDo NOT delete existing services without explicit user approval.")
	}

	// Orientation (per-service detail for bootstrapped + managed + unmanaged).
	if orientation := buildPostBootstrapOrientation(cls); orientation != "" {
		b.WriteString("\n")
		b.WriteString(orientation)
	}

	// Router ALWAYS runs — no short-circuit.
	var liveHostnames []string
	for _, svc := range services {
		if !svc.IsSystem() {
			liveHostnames = append(liveHostnames, svc.Name)
		}
	}

	var activeSessions []workflow.SessionEntry
	if stateDir != "" {
		activeSessions, _ = workflow.ListSessions(stateDir) // best-effort
	}

	routerInput := workflow.RouterInput{
		ServiceMetas:      metas,
		ActiveSessions:    activeSessions,
		LiveServices:      liveHostnames,
		UnmanagedRuntimes: cls.unmanagedNames,
	}
	offerings := workflow.Route(routerInput)
	if formatted := workflow.FormatOfferings(offerings); formatted != "" {
		b.WriteString("\n")
		b.WriteString(formatted)
	}

	return b.String()
}
