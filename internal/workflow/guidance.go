package workflow

import (
	"strings"

	"github.com/zeropsio/zcp/internal/knowledge"
)

// GuidanceParams holds all inputs needed to assemble step guidance.
type GuidanceParams struct {
	Step              string
	Mode              string
	Strategy          string
	Env               Environment
	RuntimeType       string
	DependencyTypes   []string
	DiscoveredEnvVars map[string][]string
	Iteration         int
	Plan              *ServicePlan
	LastAttestation   string
	FailureCount      int
	KP                knowledge.Provider
}

// assembleGuidance builds a complete guidance string for a bootstrap step by layering
// static content, runtime knowledge, and session context.
func assembleGuidance(params GuidanceParams) string {
	// Iteration delta (escalating) for deploy retries — replaces normal guidance.
	if params.Iteration > 0 {
		if delta := BuildIterationDelta(params.Step, params.Iteration, params.Plan, params.LastAttestation); delta != "" {
			return delta
		}
	}

	// Layer 1: Static guidance from workflow markdown.
	guide := resolveStaticGuidance(params.Step, params.Plan, params.FailureCount, params.Env)

	// Layers 2-4: Knowledge injection (runtime, schema, env vars).
	if extra := assembleKnowledge(params); extra != "" {
		guide += "\n\n---\n\n" + extra
	}

	return guide
}

// closeGuidance is the static guidance for the administrative close step.
const closeGuidance = `Bootstrap is complete. All services are deployed and healthy.

Complete this step to finalize bootstrap:
→ zerops_workflow action="complete" step="close" attestation="Bootstrap finalized, services operational"

After closing, choose a deployment strategy for each service before deploying again.`

// resolveStaticGuidance extracts the appropriate static sections for a step.
// All steps except close are routed through ResolveProgressiveGuidance for
// environment-aware guidance (local addenda, mode-specific sections).
func resolveStaticGuidance(step string, plan *ServicePlan, failureCount int, env Environment) string {
	if step == StepClose {
		return closeGuidance
	}
	return ResolveProgressiveGuidance(step, plan, failureCount, env)
}

// needsRuntimeKnowledge returns true for bootstrap steps where runtime/dependency knowledge is relevant.
func needsRuntimeKnowledge(step string) bool {
	return step == StepGenerate
}

// assembleKnowledge gathers step-relevant knowledge from the knowledge store.
// All knowledge retrieval is best-effort — errors are silently skipped.
func assembleKnowledge(params GuidanceParams) string {
	if params.KP == nil {
		return ""
	}
	var parts []string

	// Platform model at discover — agent needs conceptual foundation before planning.
	if params.Step == StepDiscover {
		if model, err := params.KP.GetModel(); err == nil && model != "" {
			parts = append(parts, model)
		}
	}

	// import.yaml schema at provision — field structure, preprocessor docs, constraints.
	if params.Step == StepProvision {
		if s := getCoreSection(params.KP, "import.yaml Schema"); s != "" {
			parts = append(parts, "## import.yaml Schema\n\n"+s)
		}
	}

	// Runtime + dependency knowledge at generate step.
	if needsRuntimeKnowledge(params.Step) {
		if params.RuntimeType != "" {
			base, _, _ := strings.Cut(params.RuntimeType, "@")
			if briefing, err := params.KP.GetBriefing(base, nil, params.Mode, nil); err == nil && briefing != "" {
				parts = append(parts, briefing)
			}
		}
		if len(params.DependencyTypes) > 0 {
			if briefing, err := params.KP.GetBriefing("", params.DependencyTypes, "", nil); err == nil && briefing != "" {
				parts = append(parts, briefing)
			}
		}
	}

	// Env vars at generate step.
	if params.Step == StepGenerate && len(params.DiscoveredEnvVars) > 0 {
		parts = append(parts, formatEnvVarsForGuide(params.DiscoveredEnvVars))
	}

	// zerops.yaml field reference at generate step.
	// Rules & Pitfalls deliberately excluded — the runtime briefing already provides
	// correct patterns for the specific runtime; cross-runtime rules are noise.
	if needsRuntimeKnowledge(params.Step) {
		if s := getCoreSection(params.KP, "zerops.yaml Schema"); s != "" {
			parts = append(parts, "## zerops.yaml Schema\n\n"+s)
		}
	}

	// Deploy: no knowledge injection. The 400+ line bootstrap.md deploy section
	// already covers tilde syntax, deploy modes, path invariants, and common issues.
	// The Generate Rules lifecycle block would be ~40% duplication.

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// getCoreSection extracts an H2 section from the core knowledge document.
func getCoreSection(kp knowledge.Provider, name string) string {
	doc, err := kp.Get("zerops://themes/core")
	if err != nil {
		return ""
	}
	sections := doc.H2Sections()
	if s, ok := sections[name]; ok {
		return s
	}
	return ""
}

// getCoreSubsection extracts a specific H3 subsection from an H2 in the core
// knowledge document. Used for surgical injection when the full H2 is too
// broad — e.g., injecting only `verticalAutoscaling` instead of the entire
// `import.yaml Schema` H2. Consumed by Phase 8 of the recipe size-reduction
// refactor (on-demand schema pointer).
//
//nolint:unused // Infrastructure for Phase 8 (docs/implementation-recipe-size-reduction.md).
func getCoreSubsection(kp knowledge.Provider, h2, h3 string) string {
	doc, err := kp.Get("zerops://themes/core")
	if err != nil {
		return ""
	}
	return doc.H3Section(h2, h3)
}
