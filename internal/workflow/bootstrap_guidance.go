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

// ResolveProgressiveGuidance returns environment-aware and mode-filtered guidance for all steps.
// - Discover/provision: base section + optional local addendum (appended).
// - Generate local: self-contained section replaces base + mode-specific.
// - Generate container: base + mode-specific sections.
// - Deploy local: self-contained section replaces SSH deploy.
// - Deploy container: base + optional agents/recovery.
func ResolveProgressiveGuidance(step string, plan *ServicePlan, failureCount int, env Environment) string {
	md, err := content.GetWorkflow("bootstrap")
	if err != nil {
		return ""
	}

	// Non-generate/deploy steps: base section + optional local addendum.
	if step != StepDeploy && step != StepGenerate {
		base := ExtractSection(md, step)
		if env == EnvLocal {
			if local := ExtractSection(md, step+"-local"); local != "" {
				return base + "\n\n---\n\n" + local
			}
		}
		return base
	}

	modes := distinctModes(plan)

	var sections []string

	switch step {
	case StepGenerate:
		sections = appendModeSections(sections, md, "generate", env, modes)

	case StepDeploy:
		sections = appendModeSections(sections, md, "deploy", env, modes)
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
		return ExtractSection(md, step)
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// appendModeSections appends base + mode-specific guidance sections for a step.
// Local mode uses a self-contained "{step}-local" section. Container mode uses
// the base "{step}" section plus per-mode "{step}-standard/dev/simple" sections.
func appendModeSections(sections []string, md, step string, env Environment, modes map[string]bool) []string {
	if env == EnvLocal {
		return append(sections, ExtractSection(md, step+"-local"))
	}
	sections = append(sections, ExtractSection(md, step))
	for _, m := range []string{PlanModeStandard, PlanModeDev, PlanModeSimple} {
		if modes[m] {
			sections = append(sections, ExtractSection(md, step+"-"+m))
		}
	}
	return sections
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
1. zerops_discover includeEnvs=true — are all env var keys present? (keys only, sufficient for cross-ref wiring)
2. Does zerops.yaml envVariables ONLY use discovered variable names?
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
