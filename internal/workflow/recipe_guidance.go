package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/zeropsio/zcp/internal/content"
	"github.com/zeropsio/zcp/internal/knowledge"
)

// buildGuide assembles step-specific guidance with knowledge injection for recipe workflow.
// Follows deploy's pattern: own method on RecipeState, not via assembleGuidance (which is bootstrap-only).
func (r *RecipeState) buildGuide(step string, iteration int, kp knowledge.Provider) string {
	// Iteration delta for deploy retries — replaces normal guidance.
	if iteration > 0 && step == RecipeStepDeploy {
		if delta := buildRecipeIterationDelta(iteration, r.lastAttestation()); delta != "" {
			return delta
		}
	}

	// Static guidance from recipe.md.
	guide := resolveRecipeGuidance(step, r.Plan)

	// Knowledge injection.
	if extra := assembleRecipeKnowledge(step, r.Plan, r.DiscoveredEnvVars, kp); extra != "" {
		guide += "\n\n---\n\n" + extra
	}

	return guide
}

// lastAttestation returns the attestation from the most recently completed step.
func (r *RecipeState) lastAttestation() string {
	for i := r.CurrentStep - 1; i >= 0; i-- {
		if r.Steps[i].Attestation != "" {
			return r.Steps[i].Attestation
		}
	}
	return ""
}

// resolveRecipeGuidance extracts the appropriate static sections from recipe.md.
func resolveRecipeGuidance(step string, plan *RecipePlan) string {
	md, err := content.GetWorkflow("recipe")
	if err != nil {
		fmt.Fprintf(os.Stderr, "recipe guidance: failed to load recipe.md: %v\n", err)
		return ""
	}

	tier := ""
	if plan != nil {
		tier = plan.Tier
	}

	switch step {
	case RecipeStepResearch:
		// Research uses tier-specific section.
		if tier == RecipeTierShowcase {
			return ExtractSection(md, "research-showcase")
		}
		return ExtractSection(md, "research-minimal")

	case RecipeStepGenerate:
		// Generate: base + fragments guidance.
		var parts []string
		if s := ExtractSection(md, "generate"); s != "" {
			parts = append(parts, s)
		}
		if s := ExtractSection(md, "generate-fragments"); s != "" {
			parts = append(parts, s)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n\n---\n\n")
		}
		return ""

	case RecipeStepFinalize:
		return ExtractSection(md, "finalize")

	case RecipeStepClose:
		return ExtractSection(md, "close")

	default:
		// Provision, deploy: use step name directly.
		return ExtractSection(md, step)
	}
}

// assembleRecipeKnowledge gathers step-relevant knowledge from the knowledge store.
// All knowledge retrieval is best-effort — errors are silently skipped.
func assembleRecipeKnowledge(step string, plan *RecipePlan, discoveredEnvVars map[string][]string, kp knowledge.Provider) string {
	if kp == nil {
		return ""
	}
	var parts []string

	switch step {
	case RecipeStepResearch:
		// Rules & pitfalls for informed plan decisions (deployment patterns, service
		// creation rules, runtime-specific settings). The LLM is filling a form here,
		// not writing YAML — full import.yaml schema + preprocessor docs are deferred
		// to provision/finalize where YAML is actually generated.
		if s := getCoreSection(kp, "Rules & Pitfalls"); s != "" {
			parts = append(parts, "## Rules & Pitfalls\n\n"+s)
		}

	case RecipeStepProvision:
		// import.yaml schema for provisioning services.
		if s := getCoreSection(kp, "import.yaml Schema"); s != "" {
			parts = append(parts, "## import.yaml Schema\n\n"+s)
		}

	case RecipeStepGenerate:
		// Runtime briefing for code generation.
		if plan != nil && plan.RuntimeType != "" {
			base, _, _ := strings.Cut(plan.RuntimeType, "@")
			if briefing, err := kp.GetBriefing(base, nil, "", nil); err == nil && briefing != "" {
				parts = append(parts, briefing)
			}
		}
		// Discovered env vars.
		if len(discoveredEnvVars) > 0 {
			parts = append(parts, formatEnvVarsForGuide(discoveredEnvVars))
		}
		// zerops.yaml schema + rules.
		for _, name := range []string{"zerops.yaml Schema", "Rules & Pitfalls"} {
			if s := getCoreSection(kp, name); s != "" {
				parts = append(parts, "## "+name+"\n\n"+s)
			}
		}

	case RecipeStepDeploy:
		// Schema rules for deployment.
		if s := getCoreSection(kp, "Schema Rules"); s != "" {
			parts = append(parts, "## Deploy Rules\n\n"+s)
		}

	case RecipeStepFinalize:
		// import.yaml schema + rules for generating 6 environment-specific import.yaml files.
		// Needs field reference, preprocessor functions (secrets), and import generation rules.
		for _, name := range []string{"import.yaml Schema", "Rules & Pitfalls"} {
			if s := getCoreSection(kp, name); s != "" {
				parts = append(parts, "## "+name+"\n\n"+s)
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// buildRecipeIterationDelta returns focused escalating recovery for recipe deploy iterations.
// Reuses the same escalation tiers as bootstrap.
func buildRecipeIterationDelta(iteration int, lastAttestation string) string {
	if iteration == 0 {
		return ""
	}
	return BuildIterationDelta(RecipeStepDeploy, iteration, nil, lastAttestation)
}
