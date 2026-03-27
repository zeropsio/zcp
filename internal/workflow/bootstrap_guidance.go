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
	return ExtractSection(md, step)
}

// ResolveProgressiveGuidance returns mode-filtered sub-sections for generate and deploy steps,
// or falls back to ResolveGuidance for other steps.
// Each mode-specific section is included at most once based on the distinct modes across all targets.
// In local mode, environment-specific sections (generate-local, deploy-local) are included.
func ResolveProgressiveGuidance(step string, plan *ServicePlan, failureCount int, env Environment) string {
	if step != StepDeploy && step != StepGenerate {
		return ResolveGuidance(step)
	}

	md, err := content.GetWorkflow("bootstrap")
	if err != nil {
		return ""
	}

	modes := distinctModes(plan)

	var sections []string

	switch step {
	case StepGenerate:
		// Base generate section (always).
		sections = append(sections, ExtractSection(md, "generate"))
		// Mode-specific sections.
		if modes[PlanModeStandard] {
			sections = append(sections, ExtractSection(md, "generate-standard"))
		}
		if modes[PlanModeDev] {
			sections = append(sections, ExtractSection(md, "generate-dev"))
		}
		if modes[PlanModeSimple] {
			sections = append(sections, ExtractSection(md, "generate-simple"))
		}
		// Environment-specific section.
		if env == EnvLocal {
			sections = append(sections, ExtractSection(md, "generate-local"))
		}

	case StepDeploy:
		// Local mode replaces the entire deploy section — SSH content is irrelevant.
		if env == EnvLocal {
			sections = append(sections, ExtractSection(md, "deploy-local"))
		} else {
			sections = append(sections, ExtractSection(md, "deploy"))
		}
		// Conditional: agent orchestration for 3+ services.
		if plan != nil && len(plan.Targets) >= 3 {
			sections = append(sections, ExtractSection(md, "deploy-agents"))
		}
		// Conditional: recovery patterns on failure.
		if failureCount > 0 {
			sections = append(sections, ExtractSection(md, "deploy-recovery"))
		}
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

// BuildIterationDelta returns a focused escalating recovery template for deploy iterations.
// Returns empty for non-deploy steps or iteration == 0.
// Escalation tiers: 1-2 = diagnose, 3-4 = systematic check, 5+ = stop and ask user.
func BuildIterationDelta(step string, iteration int, _ *ServicePlan, lastAttestation string) string {
	if step != StepDeploy || iteration == 0 {
		return ""
	}
	remaining := max(maxIterations()-iteration, 0)

	var guidance string
	switch {
	case iteration <= 2:
		guidance = `DIAGNOSE: zerops_logs severity="error" since="5m"
FIX the specific error, then redeploy + verify.`

	case iteration <= 4:
		guidance = `PREVIOUS FIXES FAILED. Systematic check:
1. zerops_discover includeEnvs=true — are all env vars present and correct?
2. Does zerops.yml envVariables ONLY use discovered variable names?
3. Does the app bind 0.0.0.0 (not localhost/127.0.0.1)?
4. Is deployFiles correct? (dev MUST be [.], stage = build output)
5. Is run.ports.port matching what the app actually listens on?
6. Is run.start the RUN command (not a build command)?
Fix what's wrong, redeploy, verify.`

	default:
		guidance = `STOP. Multiple fixes failed. Present to user:
1. What you tried in each iteration
2. Current error (from zerops_logs + zerops_verify)
3. Ask: "Should I continue, or would you like to debug manually?"
Do NOT attempt another fix without user input.`
	}

	return fmt.Sprintf("ITERATION %d (session remaining: %d)\n\nPREVIOUS: %s\n\n%s",
		iteration, remaining, lastAttestation, guidance)
}

// ExtractSection finds a <section name="{name}">...</section> block and returns its content.
func ExtractSection(md, name string) string {
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
