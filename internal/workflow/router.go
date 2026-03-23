package workflow

import (
	"fmt"
	"sort"
)

// FlowOffering represents a workflow available to the agent.
type FlowOffering struct {
	Workflow string `json:"workflow"`
	Priority int    `json:"priority"`
	Hint     string `json:"hint,omitempty"`
}

// RouterInput contains the environmental signals used for routing decisions.
type RouterInput struct {
	ProjectState   ProjectState
	ServiceMetas   []*ServiceMeta
	ActiveSessions []SessionEntry
	LiveServices   []string
}

// Route takes environmental signals and returns available workflows.
// Returns factual data — no recommendations, no intent matching.
// The LLM decides what to do based on the facts.
func Route(input RouterInput) []FlowOffering {
	// Filter stale metas: only keep metas whose hostname is in LiveServices.
	metas := filterStaleMetas(input.ServiceMetas, input.LiveServices)

	// If any metas are incomplete (provisioned but not bootstrapped), prioritize bootstrap.
	if hasIncompleteMetas(metas) {
		offerings := []FlowOffering{{
			Workflow: "bootstrap",
			Priority: 1,
			Hint:     `zerops_workflow action="start" workflow="bootstrap"`,
		}}
		return appendUtilities(offerings)
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

	offerings = appendUtilities(offerings)

	sort.Slice(offerings, func(i, j int) bool {
		return offerings[i].Priority < offerings[j].Priority
	})

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
				Hint:     fmt.Sprintf(`zerops_workflow action="resume" sessionId="%s"`, s.SessionID),
			}}
		}
	}
	return []FlowOffering{{
		Workflow: "bootstrap",
		Priority: 1,
		Hint:     `zerops_workflow action="start" workflow="bootstrap"`,
	}}
}

func routeConformant(metas []*ServiceMeta) []FlowOffering {
	offerings := strategyOfferings(metas)
	if len(offerings) == 0 && !hasStrategy(metas) {
		// No strategy set at all — offer deploy as default.
		offerings = []FlowOffering{{
			Workflow: "deploy",
			Priority: 1,
			Hint:     `zerops_workflow action="start" workflow="deploy"`,
		}}
	}
	offerings = append(offerings, FlowOffering{
		Workflow: "bootstrap",
		Priority: 2,
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
				Hint:     `zerops_workflow action="start" workflow="bootstrap"`,
			},
			{
				Workflow: "debug",
				Priority: 2,
				Hint:     `zerops_workflow action="start" workflow="debug"`,
			},
		}
	}
	offerings := strategyOfferings(metas)
	if len(offerings) == 0 && !hasStrategy(metas) {
		offerings = []FlowOffering{{
			Workflow: "deploy",
			Priority: 1,
			Hint:     `zerops_workflow action="start" workflow="deploy"`,
		}}
	}
	offerings = append(offerings, FlowOffering{
		Workflow: "bootstrap",
		Priority: 2,
		Hint:     `zerops_workflow action="start" workflow="bootstrap"`,
	})
	return offerings
}

func routeUnknown() []FlowOffering {
	return []FlowOffering{
		{Workflow: "bootstrap", Priority: 3, Hint: `zerops_workflow action="start" workflow="bootstrap"`},
		{Workflow: "deploy", Priority: 3, Hint: `zerops_workflow action="start" workflow="deploy"`},
		{Workflow: "debug", Priority: 3, Hint: `zerops_workflow action="start" workflow="debug"`},
		{Workflow: "configure", Priority: 3, Hint: `zerops_workflow action="start" workflow="configure"`},
	}
}

// hasStrategy returns true if any meta has a deploy strategy set.
func hasStrategy(metas []*ServiceMeta) bool {
	for _, m := range metas {
		if m.DeployStrategy != "" {
			return true
		}
	}
	return false
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
			{Workflow: "cicd", Priority: 1, Hint: `zerops_workflow action="start" workflow="cicd"`},
			{Workflow: "deploy", Priority: 2, Hint: `zerops_workflow action="start" workflow="deploy"`},
		}
	case StrategyPushDev:
		return []FlowOffering{{
			Workflow: "deploy", Priority: 1, Hint: `zerops_workflow action="start" workflow="deploy"`,
		}}
	case StrategyManual:
		return nil // Manual strategy: no deploy/cicd workflow. User deploys directly via zerops_deploy.
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
		{"scale", `zerops_scale serviceHostname="..." — direct tool, no workflow needed`},
		{"configure", `zerops_workflow action="start" workflow="configure"`},
	}
	for _, u := range utils {
		if !has[u.name] {
			offerings = append(offerings, FlowOffering{
				Workflow: u.name,
				Priority: 5,
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
		b = append(b, fmt.Sprintf("\n  [p%d] %s — %s", o.Priority, o.Workflow, o.Hint)...)
	}
	return string(b)
}
