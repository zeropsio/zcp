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

const baseInstructions = `ZCP manages Zerops PaaS infrastructure.
Every code task = one develop workflow. Start before ANY code changes:
  zerops_workflow action="start" workflow="develop"
The workflow refreshes service state, mounts, and guidance.
After deploy, immediately start a new workflow for the next task.
Other workflows: bootstrap (new infrastructure), recipe (6 env tiers), cicd (CI/CD pipelines).
Direct tools (no workflow needed): zerops_scale, zerops_manage, zerops_env, zerops_subdomain, zerops_discover, zerops_knowledge`

const containerEnvironment = `
Control plane container — manages OTHER services, does not serve traffic.
Files: /var/www/{hostname}/ = SSHFS mount to live service (not local). Commands: ssh {hostname} "..."
Edits on mount survive restarts but not deploys. zerops_discover refreshes service state.`

const localEnvironment = `
Local machine — code in working directory, infrastructure on Zerops.
Deploy: zcli push (zerops.yaml at repo root, each deploy = new container).
zerops_discover refreshes service state.`

// routingInstructions is intentionally empty — routing merged into baseInstructions
// to fit within the 2KB MCP instructions limit (Claude Code v2.1.84+).
const routingInstructions = ``

// BuildInstructions returns the MCP instructions message injected into the system prompt.
// It includes base + routing (first), workflow hint, runtime context, and project summary.
// stateDir is the workflow state directory; empty string means no hint.
func BuildInstructions(ctx context.Context, client platform.Client, projectID string, rt runtime.Info, stateDir string) string {
	var b strings.Builder

	// Section A: Base + routing instructions (FIRST — most important for tool selection).
	// ZCP_INSTRUCTION_BASE env var overrides for eval A/B testing.
	if override := os.Getenv("ZCP_INSTRUCTION_BASE"); override != "" {
		b.WriteString(override)
	} else {
		b.WriteString(baseInstructions)
		b.WriteString(routingInstructions)
	}

	// Section B: Workflow hint (from local state file).
	if hint := buildWorkflowHint(stateDir); hint != "" {
		b.WriteString("\n\n")
		b.WriteString(hint)
	}

	// Section C: Environment concept — how code access and deploy work.
	if rt.InContainer {
		if override := os.Getenv("ZCP_INSTRUCTION_CONTAINER"); override != "" {
			b.WriteString(override)
		} else {
			b.WriteString(containerEnvironment)
		}
		if rt.ServiceName != "" {
			fmt.Fprintf(&b, "\nYou are running on the '%s' service. Other services in this project are yours to manage.", rt.ServiceName)
		}
	} else {
		if override := os.Getenv("ZCP_INSTRUCTION_LOCAL"); override != "" {
			b.WriteString(override)
		} else {
			b.WriteString(localEnvironment)
		}
	}

	// Section D: Project summary (dynamic).
	if summary := buildProjectSummary(ctx, client, projectID, stateDir, rt.ServiceName); summary != "" {
		b.WriteString("\n\n")
		b.WriteString(summary)
	}

	return b.String()
}

// buildWorkflowHint reads the registry and returns hints for all sessions.
// Dead-PID sessions are shown as resumable — the Engine auto-recovers them
// on startup via the persisted active_session file.
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
	// Show alive sessions as active.
	for _, s := range alive {
		hints = append(hints, buildSessionHint(stateDir, s, "Active"))
	}
	// Show dead sessions as resumable — don't delete them.
	// Engine auto-recovery or explicit Resume will claim them.
	for _, s := range dead {
		hints = append(hints, buildSessionHint(stateDir, s, "Resumable"))
	}
	return strings.Join(hints, "\n")
}

// buildSessionHint formats a single session hint with step progress.
func buildSessionHint(stateDir string, s workflow.SessionEntry, prefix string) string {
	hint := fmt.Sprintf("%s workflow: %s | intent: %q | session: %s", prefix, s.Workflow, s.Intent, s.SessionID)
	if prefix == "Resumable" {
		hint += fmt.Sprintf("\n  → Call zerops_workflow action=\"resume\" sessionId=%q to continue.", s.SessionID)
	}
	state, loadErr := workflow.LoadSessionByID(stateDir, s.SessionID)
	if loadErr != nil {
		return hint
	}
	switch s.Workflow {
	case "bootstrap":
		if state.Bootstrap != nil && state.Bootstrap.Active {
			hint += fmt.Sprintf(" (step %d/%d: %s)", state.Bootstrap.CurrentStep+1, len(state.Bootstrap.Steps), state.Bootstrap.CurrentStepName())
		}
	case "develop":
		if state.Deploy != nil && state.Deploy.Active {
			hint += fmt.Sprintf(" (step %d/%d: %s)", state.Deploy.CurrentStep+1, len(state.Deploy.Steps), state.Deploy.CurrentStepName())
		}
	case "recipe":
		if state.Recipe != nil && state.Recipe.Active {
			hint += fmt.Sprintf(" (step %d/%d: %s)", state.Recipe.CurrentStep+1, len(state.Recipe.Steps), state.Recipe.CurrentStepName())
			if state.Recipe.Plan != nil {
				hint += fmt.Sprintf(" [%s %s]", state.Recipe.Plan.Framework, state.Recipe.Plan.Tier)
			}
		}
	}
	return hint
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
		return " — auto-adopted on develop workflow start" + mount
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
		b.WriteString("Project is empty — no services configured yet.\nBootstrap creates infrastructure first, then develop workflow handles all app development.")
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
