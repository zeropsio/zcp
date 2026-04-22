package workflow

import (
	"fmt"
	"sort"
	"strings"
)

// FlowOffering represents a workflow available to the agent.
type FlowOffering struct {
	Workflow string `json:"workflow"`
	Priority int    `json:"priority"`
	Hint     string `json:"hint,omitempty"`
}

// RouterInput contains the environmental signals used for routing decisions.
// Routing is fact-based: no project state classification, just service facts.
type RouterInput struct {
	ServiceMetas      []*ServiceMeta
	ActiveSessions    []SessionEntry
	LiveServices      []string
	LiveServiceStatus map[string]string // hostname → platform Status; used by offerings that need deploy state (export)
	UnmanagedRuntimes []string          // runtime hostnames without complete ServiceMeta
	WorkSession       *WorkSession      // current-PID session, nil when none; used for deploy-history derivation
}

// Route takes environmental signals and returns available workflows.
// Returns factual data — no recommendations, no intent matching.
// The LLM decides what to do based on the facts.
func Route(input RouterInput) []FlowOffering {
	// Filter stale metas: only keep metas whose hostname is in LiveServices.
	metas := filterStaleMetas(input.ServiceMetas, input.LiveServices)

	// Separate complete and incomplete metas.
	var completeMetas []*ServiceMeta
	var incompleteMetas []*ServiceMeta
	for _, m := range metas {
		if m.IsComplete() {
			completeMetas = append(completeMetas, m)
		} else {
			incompleteMetas = append(incompleteMetas, m)
		}
	}

	var offerings []FlowOffering

	// 1. Incomplete bootstrap → offer resume alongside other offerings (not exclusive).
	if len(incompleteMetas) > 0 {
		offerings = append(offerings, FlowOffering{
			Workflow: "bootstrap",
			Priority: 1,
			Hint:     resumeHint(input.ActiveSessions),
		})
	}

	// Use completeMetas for strategy-based offerings below.
	metas = completeMetas

	// 2. Unmanaged runtimes exist → adoption as priority 1.
	if len(input.UnmanagedRuntimes) > 0 {
		offerings = append(offerings, FlowOffering{
			Workflow: "bootstrap",
			Priority: 1,
			Hint: fmt.Sprintf(`zerops_workflow action="start" workflow="bootstrap" — adopt: %s`,
				strings.Join(input.UnmanagedRuntimes, ", ")),
		})
	}

	// 3. Bootstrapped metas exist → strategy-based deploy.
	if len(metas) > 0 {
		offerings = append(offerings, strategyOfferings(metas, input.LiveServiceStatus, input.WorkSession)...)
		// Offer adding new services only when nothing needs adoption.
		if len(input.UnmanagedRuntimes) == 0 {
			offerings = append(offerings, FlowOffering{
				Workflow: "bootstrap",
				Priority: 3,
				Hint:     `zerops_workflow action="start" workflow="bootstrap"`,
			})
		}
	}

	// 4. Nothing at all → create first services.
	if len(metas) == 0 && len(input.UnmanagedRuntimes) == 0 {
		offerings = append(offerings, FlowOffering{
			Workflow: "bootstrap",
			Priority: 1,
			Hint:     bootstrapHint(input.ActiveSessions),
		})
	}

	offerings = appendUtilities(offerings)

	sort.Slice(offerings, func(i, j int) bool {
		return offerings[i].Priority < offerings[j].Priority
	})

	return offerings
}

// resumeHint returns either a resume hint (if a bootstrap session exists) or a start hint.
func resumeHint(sessions []SessionEntry) string {
	for _, s := range sessions {
		if s.Workflow == WorkflowBootstrap {
			return fmt.Sprintf(`zerops_workflow action="resume" sessionId="%s"`, s.SessionID)
		}
	}
	return `zerops_workflow action="start" workflow="bootstrap"`
}

// bootstrapHint returns either a resume hint (if a bootstrap session exists) or a start hint.
func bootstrapHint(sessions []SessionEntry) string {
	return resumeHint(sessions)
}

// filterStaleMetas returns only metas whose Hostname appears in liveServices.
// If liveServices is empty, returns all metas (no filtering).
//
// Local-env metas (Mode = local-stage / local-only) are always retained —
// their Hostname is the Zerops project name, which is never a platform
// service hostname and would otherwise always be filtered out. Stage
// linkage for local-stage is separately checked via StageHostname.
func filterStaleMetas(metas []*ServiceMeta, liveServices []string) []*ServiceMeta {
	if len(liveServices) == 0 {
		return metas
	}
	live := make(map[string]bool, len(liveServices))
	for _, h := range liveServices {
		live[h] = true
	}
	var result []*ServiceMeta
	for _, m := range metas {
		if m.Mode == PlanModeLocalStage || m.Mode == PlanModeLocalOnly {
			result = append(result, m)
			continue
		}
		if live[m.Hostname] {
			result = append(result, m)
		}
	}
	return result
}

// strategyOfferings creates offerings based on the dominant deploy strategy across metas.
// Deploy is always offered (strategy is resolved within the flow, not before).
// liveStatus + ws are threaded for deploy-state derivation (see DeriveDeployed);
// both may be nil/empty — derivation degrades gracefully.
func strategyOfferings(metas []*ServiceMeta, liveStatus map[string]string, ws *WorkSession) []FlowOffering {
	strategies := make(map[string]int)
	for _, m := range metas {
		if s := m.DeployStrategy; s != "" {
			strategies[s]++
		}
	}

	// Find dominant strategy for additional offerings.
	var dominant string
	var maxCount int
	for s, c := range strategies {
		if c > maxCount {
			dominant = s
			maxCount = c
		}
	}

	// Always offer deploy — strategy-aware hint when push-git is dominant.
	developHint := `zerops_workflow action="start" workflow="develop"`
	if dominant == StrategyPushGit {
		developHint += ` — REQUIRED before pushing code to a git remote (handles auth, GIT_TOKEN, push)`
	}
	offerings := []FlowOffering{{
		Workflow: "develop", Priority: 1, Hint: developHint,
	}}

	// Strategy configuration — offer the central deploy-config entry point
	// whenever any bootstrapped service exists. This is where strategies are
	// set (push-dev, push-git, manual) AND, for push-git, the full setup flow
	// (tokens, optional CI/CD, first push) is delivered as a single atom.
	// Replaces the former workflow=cicd as the git-push setup path.
	if len(metas) > 0 {
		offerings = append(offerings, FlowOffering{
			Workflow: "strategy", Priority: 2,
			Hint: `zerops_workflow action="strategy" — configure deploy strategy (push-dev/push-git/manual); push-git returns full setup flow`,
		})
	}

	// Export — offer whenever any bootstrapped service has landed a first
	// deploy. Export works across strategies (push-dev, push-git, manual):
	// it turns any deployed service into a re-importable git repo via
	// zerops_deploy strategy=git-push. Gating on push-git would hide the
	// flow from exactly the users who most need it — teams on push-dev
	// that want to produce a sharable recipe from their workspace.
	//
	// Deploy state comes from DeriveDeployed, which checks session history
	// + platform status — no persistent meta flag (see plan A.3).
	for _, m := range metas {
		for _, h := range m.Hostnames() {
			if DeriveDeployed(h, liveStatus[h], m, ws) {
				offerings = append(offerings, FlowOffering{
					Workflow: "export", Priority: 3,
					Hint: `zerops_workflow action="start" workflow="export" — turn a deployed service into a re-importable git repo (import.yaml + buildFromGit)`,
				})
				goto afterExport
			}
		}
	}
afterExport:

	return offerings
}

// appendUtilities adds recipe, scale at priority 4-5 if not already present.
func appendUtilities(offerings []FlowOffering) []FlowOffering {
	has := make(map[string]bool, len(offerings))
	for _, o := range offerings {
		has[o.Workflow] = true
	}
	utils := []struct {
		name     string
		priority int
		hint     string
	}{
		{"recipe", 4, `zerops_workflow action="start" workflow="recipe" — create recipe repo files`},
		{"scale", 5, `zerops_scale serviceHostname="..." — direct tool, no workflow needed`},
	}
	for _, u := range utils {
		if !has[u.name] {
			offerings = append(offerings, FlowOffering{
				Workflow: u.name,
				Priority: u.priority,
				Hint:     u.hint,
			})
		}
	}
	return offerings
}

// FormatOfferings renders offerings into compact system prompt text.
func FormatOfferings(offerings []FlowOffering) string {
	if len(offerings) == 0 {
		return ""
	}
	var b []byte
	b = append(b, "Available workflows:"...)
	for _, o := range offerings {
		b = append(b, fmt.Sprintf("\n- %s: %s", o.Workflow, o.Hint)...)
	}
	return string(b)
}
