package workflow

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/content"
)

// ResolveGuidance extracts the <section name="{step}"> content from bootstrap.md.
// Returns empty string for steps without a section or if bootstrap.md cannot be loaded.
func ResolveGuidance(step string) string {
	md, err := content.GetWorkflow("bootstrap")
	if err != nil {
		return ""
	}
	return extractSection(md, step)
}

// ResolveProgressiveGuidance returns mode-filtered deploy sub-sections for the deploy step,
// or falls back to ResolveGuidance for non-deploy steps.
// Each deploy section is included at most once based on the distinct modes across all targets.
func ResolveProgressiveGuidance(step string, plan *ServicePlan, failureCount int) string {
	if step != StepDeploy {
		return ResolveGuidance(step)
	}

	md, err := content.GetWorkflow("bootstrap")
	if err != nil {
		return ""
	}

	var sections []string
	sections = append(sections, extractSection(md, "deploy-overview"))

	// Collect distinct modes across all targets.
	modes := distinctModes(plan)

	// Include deploy sections for each distinct mode present.
	if modes[PlanModeStandard] {
		sections = append(sections, extractSection(md, "deploy-standard"))
	}
	if modes[PlanModeDev] {
		sections = append(sections, extractSection(md, "deploy-dev"))
	}
	if modes[PlanModeSimple] {
		sections = append(sections, extractSection(md, "deploy-simple"))
	}
	// Iteration guidance applies to standard and dev modes (not simple).
	if modes[PlanModeStandard] || modes[PlanModeDev] {
		sections = append(sections, extractSection(md, "deploy-iteration"))
	}

	if plan != nil && len(plan.Targets) >= 3 {
		sections = append(sections, extractSection(md, "deploy-agents"))
	}

	if failureCount > 0 {
		sections = append(sections, extractSection(md, "deploy-recovery"))
	}

	// Filter empty sections and join.
	var parts []string
	for _, s := range sections {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return ResolveGuidance(step)
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// distinctModes returns the set of effective bootstrap modes across all plan targets.
// Uses EffectiveMode() so that empty BootstrapMode fields default to "standard".
func distinctModes(plan *ServicePlan) map[string]bool {
	if plan == nil {
		return nil
	}
	modes := make(map[string]bool)
	for _, t := range plan.Targets {
		modes[t.Runtime.EffectiveMode()] = true
	}
	return modes
}

const maxBootstrapIterations = 5

// BuildIterationDelta returns a focused ~300-token template for deploy iterations.
// Returns empty for non-deploy steps or iteration == 0.
func BuildIterationDelta(step string, iteration int, plan *ServicePlan, lastAttestation string) string {
	if step != StepDeploy || iteration == 0 {
		return ""
	}

	remaining := max(maxBootstrapIterations-iteration, 0)

	return fmt.Sprintf(`ITERATION %d for step %s

PREVIOUS ATTEMPT:
%s

RECOVERY PATTERNS:
| Error Pattern        | Fix                              | Then              |
|----------------------|----------------------------------|-------------------|
| port already in use  | check initCommands binding       | redeploy          |
| module not found     | verify build.base in zerops.yml  | redeploy          |
| connection refused   | check ${hostname_port} env ref   | redeploy          |
| timeout on /status   | verify 0.0.0.0 binding + port    | redeploy          |
| permission denied    | check deployFiles paths          | redeploy          |

MAX ITERATIONS REMAINING: %d
RECOVERY: Use forceGuide=true to re-fetch full guidance if stuck.`,
		iteration, step, lastAttestation, remaining)
}

// extractSection finds a <section name="{name}">...</section> block and returns its content.
func extractSection(md, name string) string {
	openTag := "<section name=\"" + name + "\">"
	closeTag := "</section>"
	start := strings.Index(md, openTag)
	if start < 0 {
		return ""
	}
	start += len(openTag)
	end := strings.Index(md[start:], closeTag)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(md[start : start+end])
}
