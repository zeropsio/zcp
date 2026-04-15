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
	UnmanagedRuntimes []string // runtime hostnames without complete ServiceMeta
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
		offerings = append(offerings, strategyOfferings(metas)...)
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
		if live[m.Hostname] {
			result = append(result, m)
		}
	}
	return result
}

// strategyOfferings creates offerings based on the dominant deploy strategy across metas.
// Deploy is always offered (strategy is resolved within the flow, not before).
func strategyOfferings(metas []*ServiceMeta) []FlowOffering {
	strategies := make(map[string]int)
	for _, m := range metas {
		if s := m.EffectiveStrategy(); s != "" {
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

	if dominant == StrategyPushGit {
		offerings = append(offerings,
			FlowOffering{Workflow: "cicd", Priority: 2, Hint: `zerops_workflow action="start" workflow="cicd"`},
			FlowOffering{Workflow: "export", Priority: 3, Hint: `zerops_workflow action="start" workflow="export"`},
		)
	}

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
		{"cicd", 3, `zerops_workflow action="start" workflow="cicd" — set up CI/CD pipelines`},
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
