package workflow

import (
	"fmt"
	"sort"
	"strings"
)

// FlowOffering represents a ranked workflow suggestion from the router.
type FlowOffering struct {
	Workflow string `json:"workflow"`
	Priority int    `json:"priority"`
	Reason   string `json:"reason"`
	Hint     string `json:"hint"`
}

// RouterInput contains the environmental signals used for routing decisions.
type RouterInput struct {
	ProjectState   ProjectState
	ServiceMetas   []*ServiceMeta
	ActiveSessions []SessionEntry
	LiveServices   []string
	Intent         string // optional: user intent for smarter routing
}

// Route takes environmental signals and returns ranked workflow offerings.
// Pure function — no I/O, no side effects.
func Route(input RouterInput) []FlowOffering {
	// Filter stale metas: only keep metas whose hostname is in LiveServices.
	metas := filterStaleMetas(input.ServiceMetas, input.LiveServices)

	// If any metas are incomplete (provisioned but not bootstrapped), prioritize bootstrap.
	if hasIncompleteMetas(metas) {
		offerings := []FlowOffering{{
			Workflow: "bootstrap",
			Priority: 1,
			Reason:   "Incomplete bootstrap detected — services provisioned but not fully set up",
			Hint:     `zerops_workflow action="start" workflow="bootstrap"`,
		}}
		offerings = appendUtilities(offerings)
		if input.Intent != "" {
			offerings = boostByIntent(offerings, input.Intent)
		}
		sort.Slice(offerings, func(i, j int) bool {
			return offerings[i].Priority < offerings[j].Priority
		})
		return offerings
	}

	var offerings []FlowOffering

	switch input.ProjectState {
	case StateFresh:
		offerings = routeFresh(input.ActiveSessions)
	case StateConformant:
		offerings = routeConformant(metas)
	case StateNonConformant:
		offerings = routeNonConformant(metas)
	case StateUnknown:
		offerings = routeUnknown()
	default:
		offerings = routeUnknown()
	}

	// Always append utility workflows if not already present.
	offerings = appendUtilities(offerings)

	// Intent-based priority boost: if user intent matches a workflow, promote it.
	if input.Intent != "" {
		offerings = boostByIntent(offerings, input.Intent)
	}

	sort.Slice(offerings, func(i, j int) bool {
		return offerings[i].Priority < offerings[j].Priority
	})

	return offerings
}

// intentPatterns maps keywords to workflows they suggest.
var intentPatterns = map[string][]string{
	"bootstrap": {"add service", "add new", "create service", "new service", "set up", "setup"},
	"deploy":    {"deploy", "push", "ship", "release", "update code", "redeploy"},
	"debug":     {"broken", "fix", "error", "crash", "debug", "diagnose", "not working", "failing", "issue", "bug"},
	"scale":     {"scale", "slow", "performance", "resources", "cpu", "memory", "ram", "container"},
	"configure": {"config", "env var", "environment", "subdomain", "port", "setting"},
	"cicd":      {"ci/cd", "cicd", "pipeline", "github action", "gitlab ci", "webhook", "automat"},
}

// boostByIntent promotes offerings whose workflow matches intent keywords.
func boostByIntent(offerings []FlowOffering, intent string) []FlowOffering {
	lower := strings.ToLower(intent)
	for i, o := range offerings {
		if patterns, ok := intentPatterns[o.Workflow]; ok {
			for _, p := range patterns {
				if strings.Contains(lower, p) {
					offerings[i].Priority = max(offerings[i].Priority-2, 0)
					offerings[i].Reason += " (matches intent)"
					break
				}
			}
		}
	}
	return offerings
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

// hasIncompleteMetas returns true if any meta has no BootstrappedAt set,
// indicating a bootstrap that started but didn't finish.
func hasIncompleteMetas(metas []*ServiceMeta) bool {
	for _, m := range metas {
		if !m.IsComplete() {
			return true
		}
	}
	return false
}

func routeFresh(sessions []SessionEntry) []FlowOffering {
	for _, s := range sessions {
		if s.Workflow == WorkflowBootstrap {
			return []FlowOffering{{
				Workflow: "bootstrap",
				Priority: 1,
				Reason:   "Bootstrap in progress",
				Hint:     fmt.Sprintf("Resume active bootstrap: zerops_workflow action=\"resume\" sessionId=\"%s\"", s.SessionID),
			}}
		}
	}
	return []FlowOffering{{
		Workflow: "bootstrap",
		Priority: 1,
		Reason:   "Fresh project — no runtime services",
		Hint:     `zerops_workflow action="start" workflow="bootstrap"`,
	}}
}

func routeConformant(metas []*ServiceMeta) []FlowOffering {
	offerings := strategyOfferings(metas)
	if len(offerings) == 0 {
		// No metas — suggest deploy + bootstrap.
		offerings = []FlowOffering{{
			Workflow: "deploy",
			Priority: 1,
			Reason:   "Dev+stage pairs detected",
			Hint:     `zerops_workflow action="start" workflow="deploy"`,
		}}
	}
	offerings = append(offerings, FlowOffering{
		Workflow: "bootstrap",
		Priority: 2,
		Reason:   "Add new services to the project",
		Hint:     `zerops_workflow action="start" workflow="bootstrap"`,
	})
	return offerings
}

func routeNonConformant(metas []*ServiceMeta) []FlowOffering {
	if len(metas) == 0 {
		return []FlowOffering{
			{
				Workflow: "bootstrap",
				Priority: 1,
				Reason:   "Non-conformant project, no tracked services",
				Hint:     `zerops_workflow action="start" workflow="bootstrap"`,
			},
			{
				Workflow: "debug",
				Priority: 2,
				Reason:   "Investigate existing services",
				Hint:     `zerops_workflow action="start" workflow="debug"`,
			},
		}
	}
	// Strategy-based for covered services + bootstrap for uncovered.
	offerings := strategyOfferings(metas)
	if len(offerings) == 0 {
		offerings = []FlowOffering{{
			Workflow: "deploy",
			Priority: 1,
			Reason:   "Tracked services without explicit strategy",
			Hint:     `zerops_workflow action="start" workflow="deploy"`,
		}}
	}
	offerings = append(offerings, FlowOffering{
		Workflow: "bootstrap",
		Priority: 2,
		Reason:   "Bootstrap uncovered services",
		Hint:     `zerops_workflow action="start" workflow="bootstrap"`,
	})
	return offerings
}

func routeUnknown() []FlowOffering {
	return []FlowOffering{
		{Workflow: "bootstrap", Priority: 3, Reason: "Project state unknown", Hint: `zerops_workflow action="start" workflow="bootstrap"`},
		{Workflow: "deploy", Priority: 3, Reason: "Project state unknown", Hint: `zerops_workflow action="start" workflow="deploy"`},
		{Workflow: "debug", Priority: 3, Reason: "Project state unknown", Hint: `zerops_workflow action="start" workflow="debug"`},
		{Workflow: "scale", Priority: 3, Reason: "Project state unknown", Hint: `zerops_workflow action="start" workflow="scale"`},
		{Workflow: "configure", Priority: 3, Reason: "Project state unknown", Hint: `zerops_workflow action="start" workflow="configure"`},
	}
}

// strategyOfferings creates offerings based on the dominant deploy strategy across metas.
func strategyOfferings(metas []*ServiceMeta) []FlowOffering {
	strategies := make(map[string]int)
	for _, m := range metas {
		if m.DeployStrategy != "" {
			strategies[m.DeployStrategy]++
		}
	}
	if len(strategies) == 0 {
		return nil
	}

	// Pick the dominant strategy.
	var dominant string
	var maxCount int
	for s, c := range strategies {
		if c > maxCount {
			dominant = s
			maxCount = c
		}
	}

	switch dominant {
	case StrategyCICD:
		return []FlowOffering{
			{
				Workflow: "cicd",
				Priority: 1,
				Reason:   "CI/CD strategy configured — set up automated pipeline",
				Hint:     `zerops_workflow action="start" workflow="cicd"`,
			},
			{
				Workflow: "deploy",
				Priority: 2,
				Reason:   "Manual deploy also available",
				Hint:     `zerops_workflow action="start" workflow="deploy"`,
			},
		}
	case StrategyPushDev:
		return []FlowOffering{{
			Workflow: "deploy",
			Priority: 1,
			Reason:   "Push-dev strategy configured",
			Hint:     `zerops_workflow action="start" workflow="deploy"`,
		}}
	case StrategyManual:
		return []FlowOffering{{
			Workflow: "manual-deploy",
			Priority: 1,
			Reason:   "Manual deploy strategy configured",
			Hint:     "Deploy manually via your preferred method",
		}}
	default:
		return nil
	}
}

// appendUtilities adds debug, scale, configure at priority 5 if not already present.
func appendUtilities(offerings []FlowOffering) []FlowOffering {
	has := make(map[string]bool, len(offerings))
	for _, o := range offerings {
		has[o.Workflow] = true
	}
	utils := []struct {
		name, hint string
	}{
		{"debug", `zerops_workflow action="start" workflow="debug"`},
		{"scale", `zerops_workflow action="start" workflow="scale"`},
		{"configure", `zerops_workflow action="start" workflow="configure"`},
	}
	for _, u := range utils {
		if !has[u.name] {
			offerings = append(offerings, FlowOffering{
				Workflow: u.name,
				Priority: 5,
				Reason:   "Always available",
				Hint:     u.hint,
			})
		}
	}
	return offerings
}

// FormatOfferings renders offerings into compact system prompt text (max 8 lines).
func FormatOfferings(offerings []FlowOffering) string {
	if len(offerings) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Available workflows:")
	for _, o := range offerings {
		if o.Hint != "" {
			fmt.Fprintf(&b, "\n  [p%d] %s — %s → %s", o.Priority, o.Workflow, o.Reason, o.Hint)
		} else {
			fmt.Fprintf(&b, "\n  [p%d] %s — %s", o.Priority, o.Workflow, o.Reason)
		}
	}
	return b.String()
}
