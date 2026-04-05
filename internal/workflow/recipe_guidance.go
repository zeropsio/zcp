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
// Recipe workflow is about CREATING framework-specific knowledge — the agent discovers
// and documents framework pitfalls. But PLATFORM invariants (lifecycle phases, port
// ranges, hostname conventions, mode immutability) are NOT framework discoveries —
// they are Zerops rules and must be injected so the agent does not re-derive them
// wrong. Both schema field references AND Rules & Pitfalls are delivered; only the
// framework-specific layer is left for the agent to research.
// All knowledge retrieval is best-effort — errors are silently skipped.
func assembleRecipeKnowledge(step string, plan *RecipePlan, discoveredEnvVars map[string][]string, kp knowledge.Provider) string {
	if kp == nil {
		return ""
	}
	var parts []string

	switch step {
	// Research: no knowledge injection. Agent fills a form about the framework
	// using its own knowledge + on-demand zerops_knowledge calls.

	case RecipeStepProvision:
		// import.yaml field reference for writing service definitions.
		if s := getCoreSection(kp, "import.yaml Schema"); s != "" {
			parts = append(parts, "## import.yaml Schema\n\n"+s)
		}
		// Platform rules: hostname conventions, priority, mode immutability, etc.
		if s := getCoreSection(kp, "Rules & Pitfalls"); s != "" {
			parts = append(parts, "## Rules & Pitfalls\n\n"+s)
		}

	case RecipeStepGenerate:
		// Runtime briefing: base hello-world patterns to extend.
		if plan != nil && plan.RuntimeType != "" {
			base, _, _ := strings.Cut(plan.RuntimeType, "@")
			if briefing, err := kp.GetBriefing(base, nil, "", nil); err == nil && briefing != "" {
				parts = append(parts, briefing)
			}
		}
		// Discovered env vars: real variable names from provisioned services.
		if len(discoveredEnvVars) > 0 {
			parts = append(parts, formatEnvVarsForGuide(discoveredEnvVars))
		}
		// zerops.yaml field reference for writing config.
		if s := getCoreSection(kp, "zerops.yaml Schema"); s != "" {
			parts = append(parts, "## zerops.yaml Schema\n\n"+s)
		}
		// Platform rules: lifecycle matrix (build vs run vs init), port ranges,
		// env var levels, deploy semantics. Critical — the #1 recipe failure is
		// putting a command in the wrong lifecycle phase.
		if s := getCoreSection(kp, "Rules & Pitfalls"); s != "" {
			parts = append(parts, "## Rules & Pitfalls\n\n"+s)
		}
		// Multi-base knowledge: injected ONLY when the recipe's primary runtime
		// is non-JS yet its build pipeline runs a JS package manager. Most
		// recipes (Node, Go, Rust, Python without JS assets) don't hit this and
		// shouldn't be burdened with zsc install / startWithoutCode details.
		if needsMultiBaseGuidance(plan) {
			parts = append(parts, multiBaseGuidance())
		}

	// Deploy: no knowledge injection. Static guidance covers the deploy flow;
	// the iteration loop catches issues via real build/runtime feedback.

	case RecipeStepFinalize:
		// import.yaml field reference for generating 6 environment-specific files.
		if s := getCoreSection(kp, "import.yaml Schema"); s != "" {
			parts = append(parts, "## import.yaml Schema\n\n"+s)
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
