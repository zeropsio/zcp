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
func resolveStaticGuidance(step string, plan *ServicePlan, failureCount int, env Environment) string {
	if step == StepGenerate || step == StepDeploy {
		return ResolveProgressiveGuidance(step, plan, failureCount, env)
	}
	if step == StepClose {
		return closeGuidance
	}
	return ResolveGuidance(step)
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

	// import.yml schema at provision.
	if params.Step == StepProvision {
		if s := getCoreSection(params.KP, "import.yml Schema"); s != "" {
			parts = append(parts, "## import.yml Schema\n\n"+s)
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

	// zerops.yml schema + rules at generate step.
	if needsRuntimeKnowledge(params.Step) {
		for _, name := range []string{"zerops.yml Schema", "Rules & Pitfalls"} {
			if s := getCoreSection(params.KP, name); s != "" {
				parts = append(parts, "## "+name+"\n\n"+s)
			}
		}
	}

	// Schema rules at bootstrap deploy step.
	if params.Step == StepDeploy {
		if s := getCoreSection(params.KP, "Schema Rules"); s != "" {
			parts = append(parts, "## Deploy Rules\n\n"+s)
		}
	}

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
