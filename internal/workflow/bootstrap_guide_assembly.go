package workflow

import (
	"fmt"
	"strings"
	"unicode"

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
// Includes service list, deploy strategy selection, CI/CD gate, and router offerings.
func BuildTransitionMessage(state *WorkflowState) string {
	if state == nil || state.Bootstrap == nil || state.Bootstrap.Plan == nil {
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

	// Deployment Strategy Selection
	sb.WriteString("\n## Deploy Strategy\n\n")
	sb.WriteString("Choose how to deploy code to your services:\n\n")
	strategies := state.Bootstrap.Strategies
	if strategies == nil {
		strategies = make(map[string]string)
	}
	for _, t := range state.Bootstrap.Plan.Targets {
		hostname := t.Runtime.DevHostname
		current := strategies[hostname]
		if current == "" {
			current = "(not yet selected)"
		}
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", hostname, current))
	}

	sb.WriteString("\nAvailable strategies:\n")
	sb.WriteString("- **push-dev**: Deploy directly from local machine (dev mode)\n")
	sb.WriteString("- **ci-cd**: Automated deployments via CI/CD pipeline (GitHub Actions, GitLab CI, etc.)\n")
	sb.WriteString("- **manual**: Deploy manually via your preferred method\n\n")

	// CI/CD Gate
	sb.WriteString("## CI/CD Gate\n\n")
	sb.WriteString("If you choose **ci-cd** strategy, you must:\n")
	sb.WriteString("1. Push code to a git repository (GitHub, GitLab, etc.)\n")
	sb.WriteString("2. Configure webhooks or CI/CD integration\n")
	sb.WriteString("3. Ensure the `.zcp/` state directory is NOT committed (add to `.gitignore`)\n\n")

	// Router-based workflow offerings
	sb.WriteString("## What's Next?\n\n")
	sb.WriteString("Infrastructure is ready and verified. Choose your next workflow:\n\n")
	offerings := routeFromBootstrapState(state)
	if len(offerings) > 0 {
		for i, o := range offerings {
			num := 'A' + rune(i)
			sb.WriteString(fmt.Sprintf("**%c) %s** — %s\n", num, titleCase(o.Workflow), o.Reason))
			if o.Hint != "" {
				sb.WriteString(fmt.Sprintf("   → `%s`\n", o.Hint))
			}
			sb.WriteString("\n")
		}
	} else {
		// Fallback if router doesn't provide offerings
		sb.WriteString("**A) Continue deploying** — edit code, push, verify.\n")
		sb.WriteString("   → `zerops_workflow action=\"start\" workflow=\"deploy\"`\n\n")
		sb.WriteString("**B) Set up CI/CD** — connect git repo for automatic deployments.\n")
		sb.WriteString("   → `zerops_workflow action=\"start\" workflow=\"cicd\"`\n\n")
	}

	sb.WriteString("**Other operations:**\n")
	sb.WriteString("- Scale: `zerops_workflow action=\"start\" workflow=\"scale\"`\n")
	sb.WriteString("- Debug: `zerops_workflow action=\"start\" workflow=\"debug\"`\n")
	sb.WriteString("- Configure: `zerops_workflow action=\"start\" workflow=\"configure\"`\n")

	return sb.String()
}

// routeFromBootstrapState generates workflow offerings based on bootstrap state.
// Returns up to 3 primary offerings (deploy, ci-cd, and utilities).
func routeFromBootstrapState(state *WorkflowState) []FlowOffering {
	if state == nil || state.Bootstrap == nil || state.Bootstrap.Plan == nil {
		return nil
	}
	// For bootstrap completion, offer deploy and ci-cd as primary workflows.
	offerings := []FlowOffering{
		{
			Workflow: "deploy",
			Priority: 1,
			Reason:   "Deploy code to services",
			Hint:     `zerops_workflow action="start" workflow="deploy"`,
		},
		{
			Workflow: "cicd",
			Priority: 2,
			Reason:   "Set up automated deployments",
			Hint:     `zerops_workflow action="start" workflow="cicd"`,
		},
	}
	// Append utilities at lower priority.
	offerings = appendUtilities(offerings)
	return offerings
}

// titleCase capitalizes the first letter of a word (replacement for deprecated strings.Title).
func titleCase(s string) string {
	if len(s) == 0 {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
