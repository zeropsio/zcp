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
	// Iteration delta for generate retries — shape-aware, plan-specific.
	// An iterating agent has already read the full generate composition;
	// on retry they need a focused delta, not the full ~16-40 KB skeleton
	// guide again. See buildGenerateRetryDelta for the format.
	if iteration > 0 && step == RecipeStepGenerate {
		if delta := buildGenerateRetryDelta(r.Plan, r.lastAttestation()); delta != "" {
			return delta
		}
	}

	// Static guidance from recipe.md.
	guide := resolveRecipeGuidance(step, r.Tier, r.Plan)

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
// tier is the session-level tier (set at RecipeStart, before the plan exists).
func resolveRecipeGuidance(step, tier string, plan *RecipePlan) string {
	md, err := content.GetWorkflow("recipe")
	if err != nil {
		fmt.Fprintf(os.Stderr, "recipe guidance: failed to load recipe.md: %v\n", err)
		return ""
	}

	// Prefer plan tier (set after research), fall back to session tier (set at start).
	effectiveTier := tier
	if plan != nil && plan.Tier != "" {
		effectiveTier = plan.Tier
	}

	switch step {
	case RecipeStepResearch:
		// Showcase: send showcase section FIRST (overrides reference loading),
		// then minimal base (framework identity, build pipeline, decision tree).
		// The showcase section explicitly says "REPLACES the loading above."
		if effectiveTier == RecipeTierShowcase {
			showcase := ExtractSection(md, "research-showcase")
			minimal := ExtractSection(md, "research-minimal")
			return showcase + "\n\n---\n\n" + minimal
		}
		return ExtractSection(md, "research-minimal")

	case RecipeStepGenerate:
		// Generate: composed base (Phase 5 predicate-gated blocks) +
		// fragments deep-dive when needed. The dashboard spec that used to
		// live in a separate `generate-dashboard` section has been merged
		// into the deploy step's sub-agent brief — generate now carries
		// only the inline skeleton-write guidance.
		//
		// Hello-world slugs skip the fragments deep-dive. Their README is one
		// paragraph and the chain recipe demonstrates it in full; the 6 KB
		// writing-style lecture is dead weight there.
		var parts []string
		if body := ExtractSection(md, "generate"); body != "" {
			parts = append(parts, composeSection(body, recipeGenerateBlocks, plan))
		}
		if planNeedsFragmentsDeepDive(plan) {
			if s := ExtractSection(md, "generate-fragments"); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n\n---\n\n")
		}
		return ""

	case RecipeStepProvision:
		// Provision: composed through predicate-gated blocks (Phase 6).
		body := ExtractSection(md, "provision")
		return composeSection(body, recipeProvisionBlocks, plan)

	case RecipeStepDeploy:
		// Deploy: composed through predicate-gated blocks (Phase 7).
		body := ExtractSection(md, "deploy")
		return composeSection(body, recipeDeployBlocks, plan)

	case RecipeStepFinalize:
		return ExtractSection(md, "finalize")

	case RecipeStepClose:
		return ExtractSection(md, "close")

	default:
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

	// Research, Deploy, and Finalize receive no knowledge injection:
	//   - Research: agent fills a form from its own training data + on-demand
	//     zerops_knowledge calls.
	//   - Deploy: static guidance + the iteration loop cover the deploy flow.
	//   - Finalize: agent calls generate-finalize with structured envComments
	//     + projectEnvVariables — it does not hand-write YAML here. The
	//     import.yaml Schema injection that used to live at finalize was
	//     vestigial and Phase 7 of the reshuffle removed it.
	switch step {
	case RecipeStepProvision:
		// Provision-phase rules: service creation, dev/stage patterns, hostname
		// conventions, scaling/immutability, and the Env Vars H3. The Env Vars
		// block is byte-identical to the copy injected at Generate — provision
		// needs the rules when writing import.yaml; generate needs them when
		// writing zerops.yaml. Duplication is sanctioned to keep each step's
		// guidance self-contained.
		//
		// import.yaml Schema is NO LONGER eager-injected as of Phase 8. The
		// inline provision-schema-inline block in recipe.md covers the
		// workspace-import field set the agent actually writes, and points at
		// `zerops_knowledge scope="theme" query="import.yaml Schema"` for
		// exotic fields.
		if s := getCoreSection(kp, "Provision Rules"); s != "" {
			parts = append(parts, "## Provision Rules\n\n"+s)
		}

	case RecipeStepGenerate:
		// Recipe knowledge chain: the source of truth for "how to write zerops.yaml
		// for this framework." Direct predecessor: full content (working zerops.yaml
		// + gotchas). Earlier ancestors: gotchas only.
		if plan != nil {
			if chain := recipeKnowledgeChain(plan, kp); chain != "" {
				parts = append(parts, chain)
			}
		}
		// Discovered env vars: real variable names from provisioned services.
		if len(discoveredEnvVars) > 0 {
			parts = append(parts, formatEnvVarsForGuide(discoveredEnvVars))
		}
		// zerops.yaml Schema injection has been removed as of Phase 8. Hello-
		// world recipes (no chain predecessor) fall back to the inline
		// generate-schema-pointer block in recipe.md, which names the
		// zerops_knowledge call for on-demand retrieval. Recipes with a
		// predecessor already have a working zerops.yaml from the chain.
		//
		// Generate Rules (core.md lifecycle-phase H2) is intentionally NOT
		// injected at generate. The chain recipe demonstrates the same rules
		// in practice as a working zerops.yaml, and the agent already received
		// the rules it needed at provision (the Provision Rules slice). Re-
		// injecting at generate would triple-teach the same content (recipe.md
		// static text + chain example + Generate Rules) for ~11 KB of response
		// bloat. The agent can fetch a specific rule on demand via
		// `zerops_knowledge scope="theme" query="core"` if it needs one.
		//
		// Multi-base knowledge: injected ONLY when the recipe's primary runtime
		// is non-JS yet its build pipeline runs a JS package manager. Most
		// recipes (Node, Go, Rust, Python without JS assets) don't hit this and
		// shouldn't be burdened with zsc install / startWithoutCode details.
		if needsMultiBaseGuidance(plan) {
			parts = append(parts, multiBaseGuidance())
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

// buildGenerateRetryDelta returns a focused delta for iteration > 0 at
// generate. The agent has already read the full generate composition once;
// on retry they need (a) a reminder of what they attested to last time,
// (b) shape-specific failure modes filtered through the plan's predicates,
// and (c) a pointer back to the chain recipe as the source of truth for
// zerops.yaml shape.
//
// Deliberately NOT using BuildIterationDelta — that's a generic escalation
// emitter suited for deploy where "try again with more focus" is the right
// posture. Generate retries benefit from shape-specific failure-mode
// reminders keyed off recipe_plan_predicates.go.
func buildGenerateRetryDelta(plan *RecipePlan, lastAttestation string) string {
	var sb strings.Builder
	sb.WriteString("## Generate — Retry\n\n")
	sb.WriteString("You've already read the full generate guide this session. This is a focused delta.\n\n")

	if lastAttestation != "" {
		sb.WriteString("### What you attested to last iteration\n\n```\n")
		sb.WriteString(lastAttestation)
		sb.WriteString("\n```\n\n")
	}

	sb.WriteString("### Common retry causes\n\n")
	sb.WriteString("- Comment ratio <30% in zerops.yaml — recount, aim for 35%.\n")
	sb.WriteString("- Env var references used guessed names — the provision-step attestation has the authoritative list.\n")
	sb.WriteString("- Missing `setup: dev` block for at least one deployable target.\n")
	sb.WriteString("- dev and prod envVariables bit-identical — mode flags must differ (a structural check fails otherwise).\n")
	sb.WriteString("- README missing one of the three extract fragments.\n")

	if isDualRuntime(plan) {
		sb.WriteString("- Dual-runtime URL references in `run.envVariables` using hardcoded hosts instead of `${STAGE_*}` / `${DEV_*}`.\n")
	}
	if hasBundlerDevServer(plan) {
		sb.WriteString("- Dev-server host-check not updated — framework config still rejects `.zerops.app`.\n")
	}
	if hasSharedCodebaseWorker(plan) {
		sb.WriteString("- Missing `setup: worker` block in the host target's zerops.yaml.\n")
	}
	if hasMultiBaseBuildCommand(plan) {
		sb.WriteString("- Dev `buildCommands` missing the secondary-runtime dependency install (asset pipeline).\n")
	}

	sb.WriteString("\n### Source of truth\n\n")
	sb.WriteString("The injected chain recipe's `## zerops.yaml template` section (from your first read-through this session) is authoritative for shape. Re-read it and diff against your output before submitting.\n")
	return sb.String()
}

// planNeedsFragmentsDeepDive reports whether a plan should receive the
// README fragment writing-style deep-dive (generate-fragments section).
// Hello-world recipes have a simple 1-section README that the chain
// recipe demonstrates in full — the 6KB deep-dive is dead weight.
//
// Same nil-plan default as planNeedsDashboardSpec: include when unknown.
func planNeedsFragmentsDeepDive(plan *RecipePlan) bool {
	if plan == nil {
		return true
	}
	return !strings.HasSuffix(plan.Slug, "-hello-world")
}
