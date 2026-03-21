package workflow

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/content"
)

// StrategyToSection maps deploy strategy constants to deploy.md section names.
var StrategyToSection = map[string]string{
	StrategyPushDev: "deploy-push-dev",
	StrategyCICD:    "deploy-ci-cd",
	StrategyManual:  "deploy-manual",
}

// strategyDescriptions provides one-line descriptions for strategy alternatives.
var strategyDescriptions = map[string]string{
	StrategyPushDev: "SSH self-deploy from dev container",
	StrategyCICD:    "auto-deploy on git push via webhook",
	StrategyManual:  "you manage deployments yourself",
}

// buildPrepareGuide generates personalized prepare step guidance from state.
func buildPrepareGuide(state *DeployState, env Environment) string {
	var sb strings.Builder

	sb.WriteString("## Deploy Preparation\n\n")

	// Setup summary.
	sb.WriteString("### Your services\n")
	writeTargetSummary(&sb, state)
	fmt.Fprintf(&sb, "Mode: %s | Strategy: %s\n\n", state.Mode, state.Strategy)

	// Checklist.
	sb.WriteString("### Checklist\n")
	hostnames := targetHostnameList(state.Targets)
	fmt.Fprintf(&sb, "1. zerops.yml must exist with `setup:` entries for: %s\n", strings.Join(hostnames, ", "))
	sb.WriteString("2. Env var references (`${hostname_varName}`) must match real variables\n")
	if state.Mode == PlanModeStandard || state.Mode == PlanModeDev {
		sb.WriteString("3. Dev entry: `start: zsc noop --silent`, NO healthCheck\n")
	}
	if state.Mode == PlanModeSimple {
		sb.WriteString("3. Entry must have real `start:` command and `healthCheck` (server auto-starts)\n")
	}
	if state.Mode == PlanModeStandard {
		sb.WriteString("4. Stage entry: real `start:` command + `healthCheck` required\n")
	}
	sb.WriteString("\n")

	// Platform rules.
	sb.WriteString("### Platform rules\n")
	sb.WriteString("- Deploy = new container — local files lost, only `deployFiles` content survives\n")
	sb.WriteString("- `${hostname_varName}` typo = silent literal string, no error from platform\n")
	sb.WriteString("- Build container ≠ run container — different environment\n")
	if env == EnvContainer {
		sb.WriteString("- Code on SSHFS mount is already on the container — deploy rebuilds, not transfers\n")
	}
	sb.WriteString("\n")

	// Strategy note.
	writeStrategyNote(&sb, state.Strategy)

	// Knowledge pointers.
	sb.WriteString(buildKnowledgeMap(state.Targets))

	return sb.String()
}

// buildDeployGuide generates personalized deploy step guidance from state.
func buildDeployGuide(state *DeployState, iteration int, env Environment) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "## Deploy — %s mode, %s\n\n", state.Mode, state.Strategy)

	// Iteration escalation replaces workflow on retries.
	if iteration > 0 {
		writeIterationEscalation(&sb, iteration)
		sb.WriteString("\n")
	}

	// Mode-specific workflow steps.
	sb.WriteString("### Workflow\n")
	switch state.Mode {
	case PlanModeStandard:
		writeStandardWorkflow(&sb, state.Targets, env)
	case PlanModeDev:
		writeDevWorkflow(&sb, state.Targets, env)
	case PlanModeSimple:
		writeSimpleWorkflow(&sb, state.Targets)
	default:
		writeStandardWorkflow(&sb, state.Targets, env)
	}
	sb.WriteString("\n")

	// Key facts.
	sb.WriteString("### Key facts\n")
	sb.WriteString("- zerops_deploy blocks until complete — returns DEPLOYED or BUILD_FAILED with buildLogs\n")
	sb.WriteString("- After deploy: only `deployFiles` content exists. All other local files lost.\n")
	if hasRole(state.Targets, DeployRoleDev) {
		sb.WriteString("- Dev server: start manually after deploy (zsc noop). Env vars are OS env vars.\n")
	}
	if hasRole(state.Targets, DeployRoleStage) {
		sb.WriteString("- Stage: auto-starts with healthCheck. Zerops monitors and restarts.\n")
	}
	sb.WriteString("- Subdomain must be enabled after every deploy (idempotent)\n\n")

	// Diagnostic pointers.
	sb.WriteString("### If something breaks\n")
	sb.WriteString("- Build failed → zerops_logs, check buildCommands, dependencies, runtime version\n")
	sb.WriteString("- Container didn't start → check start command, ports, env vars. Deploy = new container.\n")
	sb.WriteString("- Running but unreachable → zerops_subdomain, check ports in zerops.yml vs app\n")
	sb.WriteString("- zerops_verify unhealthy → check `detail` field for specific failed check\n")

	return sb.String()
}

// buildVerifyGuide returns verify step guidance from deploy.md.
func buildVerifyGuide() string {
	md, err := content.GetWorkflow("deploy")
	if err != nil {
		return "Run zerops_verify for each target service. Check health status."
	}
	section := ExtractSection(md, "deploy-verify")
	if section == "" {
		return "Run zerops_verify for each target service. Check health status."
	}
	return section
}

// buildKnowledgeMap returns compact knowledge pointers for agent on-demand use.
func buildKnowledgeMap(targets []DeployTarget) string {
	var sb strings.Builder
	sb.WriteString("### Knowledge on demand\n")
	sb.WriteString("- zerops.yml schema: `zerops_knowledge query=\"zerops.yml schema\"`\n")
	sb.WriteString("- Env var discovery: `zerops_discover includeEnvs=true`\n")

	// Deduplicate runtime types across targets for unique pointers.
	seen := make(map[string]bool)
	for _, t := range targets {
		if t.Role == DeployRoleStage {
			continue // stage shares runtime with dev
		}
		if !seen[t.Hostname] {
			seen[t.Hostname] = true
			fmt.Fprintf(&sb, "- %s: `zerops_knowledge query=\"<runtime>\"` for runtime-specific config\n", t.Hostname)
		}
	}
	return sb.String()
}

// --- helpers ---

func writeTargetSummary(sb *strings.Builder, state *DeployState) {
	for _, t := range state.Targets {
		if t.Role == DeployRoleStage {
			continue // listed with dev
		}
		fmt.Fprintf(sb, "- %s (%s)", t.Hostname, t.Role)
		// Find paired stage.
		for _, s := range state.Targets {
			if s.Role == DeployRoleStage {
				fmt.Fprintf(sb, " → %s (stage)", s.Hostname)
				break
			}
		}
		sb.WriteString("\n")
	}
}

func writeStrategyNote(sb *strings.Builder, current string) {
	sb.WriteString("### Strategy\n")
	desc := strategyDescriptions[current]
	fmt.Fprintf(sb, "Currently: %s (%s)\n", current, desc)

	var alts []string
	for strategy, d := range strategyDescriptions {
		if strategy != current {
			alts = append(alts, fmt.Sprintf("%s (%s)", strategy, d))
		}
	}
	fmt.Fprintf(sb, "Other options: %s\n", strings.Join(alts, ", "))
	sb.WriteString("Change: `zerops_workflow action=\"strategy\" strategies={...}`\n\n")
}

func writeStandardWorkflow(sb *strings.Builder, targets []DeployTarget, env Environment) {
	dev, stage := "", ""
	for _, t := range targets {
		if t.Role == DeployRoleDev {
			dev = t.Hostname
		}
		if t.Role == DeployRoleStage {
			stage = t.Hostname
		}
	}
	if dev == "" {
		dev = "<dev>"
	}
	if stage == "" {
		stage = "<stage>"
	}

	fmt.Fprintf(sb, "1. Deploy to dev: `zerops_deploy targetService=\"%s\"`\n", dev)
	sb.WriteString("2. Start server on dev manually via SSH (dev uses zsc noop)\n")
	fmt.Fprintf(sb, "3. Enable subdomain: `zerops_subdomain action=\"enable\" serviceHostname=\"%s\"`\n", dev)
	fmt.Fprintf(sb, "4. Verify dev: `zerops_verify serviceHostname=\"%s\"`\n", dev)
	fmt.Fprintf(sb, "5. Deploy to stage: `zerops_deploy sourceService=\"%s\" targetService=\"%s\"`\n", dev, stage)
	sb.WriteString("   Stage auto-starts (real start command + healthCheck)\n")
	fmt.Fprintf(sb, "6. Enable subdomain: `zerops_subdomain action=\"enable\" serviceHostname=\"%s\"`\n", stage)
	fmt.Fprintf(sb, "7. Verify stage: `zerops_verify serviceHostname=\"%s\"`\n", stage)
	_ = env // available for future container-specific hints
}

func writeDevWorkflow(sb *strings.Builder, targets []DeployTarget, env Environment) {
	dev := ""
	for _, t := range targets {
		if t.Role == DeployRoleDev {
			dev = t.Hostname
			break
		}
	}
	if dev == "" {
		dev = "<dev>"
	}

	fmt.Fprintf(sb, "1. Deploy: `zerops_deploy targetService=\"%s\"`\n", dev)
	sb.WriteString("2. Start server manually via SSH (dev uses zsc noop)\n")
	fmt.Fprintf(sb, "3. Enable subdomain: `zerops_subdomain action=\"enable\" serviceHostname=\"%s\"`\n", dev)
	fmt.Fprintf(sb, "4. Verify: `zerops_verify serviceHostname=\"%s\"`\n", dev)
	_ = env
}

func writeSimpleWorkflow(sb *strings.Builder, targets []DeployTarget) {
	hostname := ""
	for _, t := range targets {
		if t.Role == DeployRoleSimple {
			hostname = t.Hostname
			break
		}
	}
	if hostname == "" {
		hostname = "<service>"
	}

	fmt.Fprintf(sb, "1. Deploy: `zerops_deploy targetService=\"%s\"` — server auto-starts\n", hostname)
	fmt.Fprintf(sb, "2. Enable subdomain: `zerops_subdomain action=\"enable\" serviceHostname=\"%s\"`\n", hostname)
	fmt.Fprintf(sb, "3. Verify: `zerops_verify serviceHostname=\"%s\"`\n", hostname)
}

func writeIterationEscalation(sb *strings.Builder, iteration int) {
	fmt.Fprintf(sb, "### Iteration %d — Diagnostic escalation\n", iteration)
	switch iteration {
	case 1:
		sb.WriteString("Check `zerops_logs severity=\"error\"`. Build failed? → review buildLogs from deploy response.\n")
		sb.WriteString("Container crash? → check start command, ports, env vars.\n")
	case 2:
		sb.WriteString("Systematic check: zerops.yml config (ports, start, deployFiles),\n")
		sb.WriteString("env var references (typos = literal strings!), runtime version compatibility.\n")
	default:
		sb.WriteString("Present diagnostic summary to user: exact error from logs,\n")
		sb.WriteString("current config state, env var values. User decides next step.\n")
	}
}

func targetHostnameList(targets []DeployTarget) []string {
	hostnames := make([]string, 0, len(targets))
	for _, t := range targets {
		hostnames = append(hostnames, t.Hostname)
	}
	return hostnames
}

func hasRole(targets []DeployTarget, role string) bool {
	for _, t := range targets {
		if t.Role == role {
			return true
		}
	}
	return false
}
