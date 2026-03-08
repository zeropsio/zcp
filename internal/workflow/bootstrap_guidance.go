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

// implicitWebserverTypes are runtime types that auto-start without manual server startup.
var implicitWebserverTypes = map[string]bool{
	"php-nginx": true, "php-apache": true, "nginx": true, "static": true,
}

// hasNonImplicitWebserverRuntime checks if any plan target has a runtime type
// that is NOT an implicit webserver (php-nginx, php-apache, nginx, static).
func hasNonImplicitWebserverRuntime(plan *ServicePlan) bool {
	if plan == nil {
		return false
	}
	for _, t := range plan.Targets {
		base, _, _ := strings.Cut(t.Runtime.Type, "@")
		if !implicitWebserverTypes[base] {
			return true
		}
	}
	return false
}

// ResolveProgressiveGuidance returns mode-filtered deploy sub-sections for the deploy step,
// or falls back to ResolveGuidance for non-deploy steps.
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

	if hasNonImplicitWebserverRuntime(plan) {
		sections = append(sections, extractSection(md, "deploy-standard"))
		sections = append(sections, extractSection(md, "deploy-iteration"))
	} else {
		sections = append(sections, extractSection(md, "deploy-simple"))
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

const maxBootstrapIterations = 5

// BuildIterationDelta returns a focused ~300-token template for deploy iterations.
// Returns empty for non-deploy steps or iteration == 0.
func BuildIterationDelta(step string, iteration int, plan *ServicePlan, lastAttestation string) string {
	if step != StepDeploy || iteration == 0 {
		return ""
	}

	remaining := maxBootstrapIterations - iteration
	if remaining < 0 {
		remaining = 0
	}

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
