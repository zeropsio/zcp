package workflow

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/zeropsio/zcp/internal/knowledge"
)

// buildGuide assembles a step guide with injected knowledge from the knowledge store.
// Falls back to base guidance if knowledge is unavailable.
func (b *BootstrapState) buildGuide(step string, iteration int, env Environment, kp knowledge.Provider) string {
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
		Env:               env,
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
	sb.WriteString("## Discovered Environment Variables (zerops.yaml wiring — not yet active)\n\n")
	sb.WriteString("**Cross-service references for `run.envVariables` in zerops.yaml. NOT active as OS env vars on the dev container — they activate only after `zerops_deploy`.**\n\n")
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

const bootstrapCompleteMsg = "Bootstrap complete."

// BuildTransitionMessage creates a summary message when bootstrap completes.
// Includes service list, transition hint, and router offerings.
func BuildTransitionMessage(state *WorkflowState) string {
	if state == nil || state.Bootstrap == nil || state.Bootstrap.Plan == nil {
		return bootstrapCompleteMsg
	}

	// Adoption: all targets are existing services — no code was generated or deployed.
	if state.Bootstrap.Plan.IsAllExisting() {
		return buildAdoptionTransitionMessage(state)
	}

	// Managed-only: no runtime targets, just managed services.
	if len(state.Bootstrap.Plan.Targets) == 0 {
		return bootstrapCompleteMsg + "\n\nManaged services provisioned. No runtime services to deploy." +
			"\n\nAvailable operations:\n" +
			"- Scale: `zerops_scale serviceHostname=\"...\"`\n" +
			"- Env vars: `zerops_env action=\"set|delete\"` (reload after: `zerops_manage action=\"reload\"`)\n" +
			"- Investigate: `zerops_workflow action=\"start\" workflow=\"develop\"`"
	}

	var sb strings.Builder
	sb.WriteString(bootstrapCompleteMsg + "\n\n## Services\n\n")
	writeServiceList(&sb, state.Bootstrap.Plan)

	sb.WriteString("\nInfrastructure is verified — services running with a verification server (hello-world). No application code has been written yet.\n\n")
	writeDeployModelPrimer(&sb)

	sb.WriteString("To implement the user's application, start the develop workflow:\n")
	sb.WriteString("`zerops_workflow action=\"start\" workflow=\"develop\"`\n\n")

	sb.WriteString("## What's Next?\n\n")
	sb.WriteString("Infrastructure is ready and verified. Choose your next workflow:\n\n")
	writeOfferingsFooter(&sb, state)

	return sb.String()
}

// buildAdoptionTransitionMessage creates a summary for pure-adoption bootstraps.
// Existing services keep their code and configuration — no hello-world was deployed.
func buildAdoptionTransitionMessage(state *WorkflowState) string {
	var sb strings.Builder
	sb.WriteString(bootstrapCompleteMsg + " Services adopted — existing code and configuration preserved.\n\n## Services\n\n")
	writeServiceList(&sb, state.Bootstrap.Plan)
	sb.WriteString("\n")
	writeDeployModelPrimer(&sb)
	sb.WriteString("## What's Next?\n\n")
	writeOfferingsFooter(&sb, state)

	return sb.String()
}

func writeServiceList(sb *strings.Builder, plan *ServicePlan) {
	for _, t := range plan.Targets {
		mode := t.Runtime.EffectiveMode()
		fmt.Fprintf(sb, "- **%s** (%s, %s mode)\n", t.Runtime.DevHostname, t.Runtime.Type, mode)
		if mode == PlanModeStandard {
			fmt.Fprintf(sb, "  Stage: **%s**\n", t.Runtime.StageHostname())
		}
		for _, d := range t.Dependencies {
			fmt.Fprintf(sb, "  - %s (%s)\n", d.Hostname, d.Type)
		}
	}
}

func writeDeployModelPrimer(sb *strings.Builder) {
	sb.WriteString("## Deploy Model (read before developing)\n\n")
	sb.WriteString("- **Deploy = new container** — each deploy replaces the container. Only `deployFiles` content persists.\n")
	sb.WriteString("- **Code on SSHFS mount** — write code to the local mount (`/var/www/{hostname}/`), not via SSH into the container.\n")
	sb.WriteString("- **prepareCommands need `sudo`** — containers run as `zerops` user. Use `sudo apk add` / `sudo apt-get install`.\n")
	sb.WriteString("- **Build ≠ Run** — build container has `build.base`, run container has `run.base`. Install runtime packages in `run.prepareCommands`.\n\n")
}

func writeOfferingsFooter(sb *strings.Builder, state *WorkflowState) {
	offerings := routeFromBootstrapState(state)
	for i, o := range offerings {
		num := 'A' + rune(i)
		fmt.Fprintf(sb, "**%c) %s**\n", num, titleCase(o.Workflow))
		if o.Hint != "" {
			fmt.Fprintf(sb, "   → `%s`\n", o.Hint)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("**Other operations:**\n")
	sb.WriteString("- Scale: `zerops_scale serviceHostname=\"...\"`\n")
	sb.WriteString("- Env vars: `zerops_env action=\"set|delete\"` (reload after: `zerops_manage action=\"reload\"`)\n")
}

// routeFromBootstrapState generates workflow offerings based on bootstrap state.
// Returns up to 3 primary offerings (develop, cicd, and utilities).
func routeFromBootstrapState(state *WorkflowState) []FlowOffering {
	if state == nil || state.Bootstrap == nil || state.Bootstrap.Plan == nil {
		return nil
	}
	offerings := []FlowOffering{
		{
			Workflow: "develop",
			Priority: 1,
			Hint:     `zerops_workflow action="start" workflow="develop"`,
		},
		{
			Workflow: "cicd",
			Priority: 2,
			Hint:     `zerops_workflow action="start" workflow="cicd"`,
		},
	}
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
