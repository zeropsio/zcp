package workflow

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/knowledge"
)

// buildGuide assembles a step guide with injected knowledge from the knowledge store.
// Falls back to base guidance if knowledge is unavailable.
func (b *BootstrapState) buildGuide(step string, iteration int, _ Environment, kp knowledge.Provider) string {
	var runtimeType string
	var depTypes []string
	if b.Plan != nil {
		runtimeType = b.Plan.RuntimeBase()
		depTypes = b.Plan.DependencyTypes()
	}

	// D5: Env vars injected once at generate, not at deploy.
	var envVars map[string][]string
	if step != StepDeploy {
		envVars = b.DiscoveredEnvVars
	}

	return assembleGuidance(GuidanceParams{
		Step:              step,
		Mode:              b.PlanMode(),
		RuntimeType:       runtimeType,
		DependencyTypes:   depTypes,
		DiscoveredEnvVars: envVars,
		Iteration:         iteration,
		Plan:              b.Plan,
		LastAttestation:   b.lastAttestation(),
		FailureCount:      iteration,
		KP:                kp,
	})
}

// formatEnvVarsForGuide formats discovered env vars as markdown for guide injection.
func formatEnvVarsForGuide(envVars map[string][]string) string {
	var sb strings.Builder
	sb.WriteString("## Discovered Environment Variables\n\n")
	sb.WriteString("**ONLY use these in zerops.yml envVariables. Anything else = empty at runtime.**\n\n")
	for hostname, vars := range envVars {
		sb.WriteString("**" + hostname + "**: ")
		refs := make([]string, len(vars))
		for i, v := range vars {
			refs[i] = "`${" + hostname + "_" + v + "}`"
		}
		sb.WriteString(strings.Join(refs, ", "))
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// BuildTransitionMessage creates a summary message when bootstrap completes.
func BuildTransitionMessage(state *WorkflowState) string {
	if state.Bootstrap == nil || state.Bootstrap.Plan == nil {
		return "Bootstrap complete."
	}
	var sb strings.Builder
	sb.WriteString("Bootstrap complete.\n\n## Services\n\n")

	for _, t := range state.Bootstrap.Plan.Targets {
		mode := t.Runtime.EffectiveMode()
		sb.WriteString(fmt.Sprintf("- **%s** (%s, %s mode)\n", t.Runtime.DevHostname, t.Runtime.Type, mode))
		if mode == PlanModeStandard {
			sb.WriteString(fmt.Sprintf("  Stage: **%s**\n", t.Runtime.StageHostname()))
		}
		for _, d := range t.Dependencies {
			sb.WriteString(fmt.Sprintf("  - %s (%s)\n", d.Hostname, d.Type))
		}
	}

	sb.WriteString("\n## What's Next?\n\n")
	sb.WriteString("Infrastructure is ready and verified. Choose how to continue:\n\n")
	sb.WriteString("**A) Continue deploying the same way** — edit code, push, verify.\n")
	sb.WriteString("   → `zerops_workflow action=\"start\" workflow=\"deploy\"`\n\n")
	sb.WriteString("**B) Set up CI/CD** — connect git repo for automatic deployments.\n")
	sb.WriteString("   → `zerops_workflow action=\"start\" workflow=\"cicd\"`\n\n")
	sb.WriteString("**Other operations:**\n")
	sb.WriteString("- Scale: `zerops_workflow action=\"start\" workflow=\"scale\"`\n")
	sb.WriteString("- Debug: `zerops_workflow action=\"start\" workflow=\"debug\"`\n")
	sb.WriteString("- Configure: `zerops_workflow action=\"start\" workflow=\"configure\"`\n")

	return sb.String()
}
