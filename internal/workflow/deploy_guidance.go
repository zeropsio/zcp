package workflow

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/content"
)

// StrategyToSection maps deploy strategy constants to deploy.md section names.
var StrategyToSection = map[string]string{
	StrategyPushDev: "deploy-push-dev",
	StrategyPushGit: "deploy-push-git",
	StrategyManual:  "deploy-manual",
}

// strategyDescriptions provides one-line descriptions for strategy alternatives.
var strategyDescriptions = map[string]string{
	StrategyPushDev: "SSH self-deploy from dev container",
	StrategyPushGit: "push to git remote (optional CI/CD)",
	StrategyManual:  "you manage deployments yourself",
}

// readCurrentStrategy reads the deploy strategy for a hostname from its ServiceMeta.
// Returns empty string if meta not found or stateDir is empty.
func readCurrentStrategy(stateDir, hostname string) string {
	if stateDir == "" {
		return ""
	}
	meta, err := ReadServiceMeta(stateDir, hostname)
	if err != nil || meta == nil {
		return ""
	}
	return meta.EffectiveStrategy()
}

// dominantStrategy reads strategy from the first non-stage target's meta.
func dominantStrategy(stateDir string, targets []DeployTarget) string {
	for _, t := range targets {
		if t.Role == DeployRoleStage {
			continue
		}
		if s := readCurrentStrategy(stateDir, t.Hostname); s != "" {
			return s
		}
	}
	return ""
}

// buildPrepareGuide generates personalized prepare step guidance from state.
func buildPrepareGuide(state *DeployState, env Environment, stateDir string) string {
	var sb strings.Builder

	sb.WriteString("## Development & Deploy\n\n")
	sb.WriteString("This is the development workflow. Discover what code exists on the service, implement what the user wants, then deploy and verify.\n")
	sb.WriteString("If the service has only a bootstrap verification server (hello-world with /, /health, /status), replace it with the actual application.\n")
	sb.WriteString("If the service already has application code, modify it according to the user's request.\n\n")

	// Setup summary.
	sb.WriteString("### Your services\n")
	writeTargetSummary(&sb, state)
	strategy := dominantStrategy(stateDir, state.Targets)
	if strategy != "" {
		fmt.Fprintf(&sb, "Mode: %s | Strategy: %s\n\n", state.Mode, strategy)
	} else {
		fmt.Fprintf(&sb, "Mode: %s | Strategy: not set\n\n", state.Mode)
	}

	// Checklist.
	sb.WriteString("### Checklist\n")
	fmt.Fprintf(&sb, "1. zerops.yaml must have `setup: dev` (dev services) and/or `setup: prod` (stage/simple) entries — canonical recipe names, NOT hostnames\n")
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
	sb.WriteString("5. **envVariables are NOT live until deploy.** Editing zerops.yaml does not activate env vars — they appear in the container only after `zerops_deploy`. Do NOT verify with `printenv` or SSH before deploying.\n")
	sb.WriteString("\n")

	// Development workflow — strategy and environment aware.
	writeDevelopmentWorkflow(&sb, state, strategy, env)

	// Platform rules.
	sb.WriteString("### Platform rules\n")
	sb.WriteString("- Deploy = new container — local files lost, only `deployFiles` content survives\n")
	sb.WriteString("- envVariables = declarative config, NOT live until deploy. Never check with `printenv` before deploying.\n")
	sb.WriteString("- `${hostname_varName}` typo = silent literal string, no error from platform\n")
	sb.WriteString("- Build container ≠ run container — different environment\n")
	if env == EnvContainer {
		sb.WriteString("- Code on SSHFS mount — deploy rebuilds, not transfers\n")
		for _, t := range state.Targets {
			if t.Role != DeployRoleStage {
				fmt.Fprintf(&sb, "  - %s: `/var/www/%s/`\n", t.Hostname, t.Hostname)
			}
		}
		sb.WriteString("- Agent Browser (agent-browser.dev) installed — use for browser-based testing/verification of deployed web apps\n")
	}
	sb.WriteString("\n")

	// Strategy note.
	writeStrategyNote(&sb, strategy)

	// Knowledge pointers.
	sb.WriteString(buildKnowledgeMap(state.Targets))

	return sb.String()
}

// buildDeployGuide generates personalized deploy step guidance from state.
func buildDeployGuide(state *DeployState, iteration int, env Environment, stateDir string) string {
	var sb strings.Builder

	strategy := dominantStrategy(stateDir, state.Targets)
	if strategy != "" {
		fmt.Fprintf(&sb, "## Execute — %s mode, %s\n\n", state.Mode, strategy)
	} else {
		fmt.Fprintf(&sb, "## Execute — %s mode, strategy pending\n\n", state.Mode)
	}

	// Iteration escalation replaces workflow on retries.
	if iteration > 0 {
		writeIterationEscalation(&sb, iteration)
		sb.WriteString("\n")
	}

	// Mode-specific workflow steps.
	sb.WriteString("### Workflow\n")
	if env == EnvLocal {
		writeLocalWorkflow(&sb, state.Targets)
	} else {
		switch state.Mode {
		case PlanModeStandard:
			writeStandardWorkflow(&sb, state.Targets)
		case PlanModeDev:
			writeDevWorkflow(&sb, state.Targets)
		case PlanModeSimple:
			writeSimpleWorkflow(&sb, state.Targets)
		default:
			writeStandardWorkflow(&sb, state.Targets)
		}
	}
	sb.WriteString("\n")

	// Key facts — environment-specific.
	sb.WriteString("### Key facts\n")
	sb.WriteString("- zerops_deploy blocks until complete — returns DEPLOYED or BUILD_FAILED with buildLogs\n")
	if env == EnvLocal {
		sb.WriteString("- Deploy = new container on Zerops — only `deployFiles` content persists\n")
		sb.WriteString("- Local code unchanged — edit and re-deploy when ready\n")
		sb.WriteString("- VPN connections survive deploys — no reconnect needed\n")
	} else {
		sb.WriteString("- After deploy: only `deployFiles` content exists. All other local files lost.\n")
		if hasRole(state.Targets, DeployRoleDev) {
			sb.WriteString("- Deploy to dev = new container. ALL previous SSH sessions to that service are dead (exit 255 on old connections).\n")
			sb.WriteString("- Dev server: start manually via NEW SSH after deploy (idle start: zsc noop). Env vars are OS env vars.\n")
		}
		if hasRole(state.Targets, DeployRoleStage) {
			sb.WriteString("- Stage: auto-starts with healthCheck. Zerops monitors and restarts.\n")
		}
	}
	sb.WriteString("- Subdomain persists across re-deploys. Check `zerops_discover` for status and URL.\n\n")

	// Code-only changes shortcut.
	sb.WriteString("### Code-only changes (no zerops.yaml change)\n")
	if env == EnvLocal {
		sb.WriteString("- Edit code locally with hot reload — no redeploy needed for dev.\n")
		sb.WriteString("- Redeploy ONLY when zerops.yaml changes or you want to update the Zerops service.\n\n")
	} else {
		sb.WriteString("- Edit code on mount → restart server via SSH. No redeploy needed.\n")
		sb.WriteString("- Implicit-webserver runtimes (PHP, nginx, static): changes take effect immediately, no restart needed.\n")
		sb.WriteString("- Redeploy ONLY when zerops.yaml changes (envVariables, ports, buildCommands, deployFiles).\n\n")
	}

	// Diagnostic pointers.
	sb.WriteString("### If something breaks\n")
	sb.WriteString("- Build failed → zerops_logs, check buildCommands, dependencies, runtime version\n")
	sb.WriteString("- Container didn't start → check start command, ports, env vars. Deploy = new container.\n")
	sb.WriteString("- Running but unreachable → zerops_subdomain, check ports in zerops.yaml vs app\n")
	sb.WriteString("- zerops_verify unhealthy → check `detail` field for specific failed check\n")
	sb.WriteString("- Process killed (OOM) during SSH work → `zsc scale ram +2GiB 10m` before heavy ops (auto-reverts)\n")

	return sb.String()
}

// buildVerifyGuide returns verify step guidance from deploy.md.
func buildVerifyGuide() string {
	md, err := content.GetWorkflow("develop")
	if err != nil {
		return "Run zerops_verify for each target service. Check health status."
	}
	section := ExtractSection(md, "deploy-verify")
	if section == "" {
		return "Run zerops_verify for each target service. Check health status."
	}
	return section
}

// buildKnowledgeMap returns compact knowledge pointers personalized to target runtime types.
func buildKnowledgeMap(targets []DeployTarget) string {
	var sb strings.Builder
	sb.WriteString("### Knowledge on demand\n")
	sb.WriteString("- zerops.yaml schema: `zerops_knowledge query=\"zerops.yaml schema\"`\n")

	// Personalized runtime pointers from targets.
	seen := make(map[string]bool)
	for _, t := range targets {
		if t.RuntimeType == "" || t.Role == DeployRoleStage {
			continue
		}
		base, _, _ := strings.Cut(t.RuntimeType, "@")
		if seen[base] {
			continue
		}
		seen[base] = true
		fmt.Fprintf(&sb, "- %s (%s): `zerops_knowledge query=\"%s\"`\n", t.Hostname, t.RuntimeType, base)
	}
	if len(seen) == 0 {
		sb.WriteString("- Runtime docs: `zerops_knowledge query=\"<your runtime>\"` (e.g. nodejs, go, php-apache)\n")
	}

	sb.WriteString("- Env var keys: `zerops_discover includeEnvs=true` (keys only). If values needed for troubleshooting: add `includeEnvValues=true`\n")
	return sb.String()
}

// writeDevelopmentWorkflow writes strategy-aware development iteration guidance.
// This tells the LLM HOW to develop and test code before deploying.
func writeDevelopmentWorkflow(sb *strings.Builder, state *DeployState, strategy string, env Environment) {
	sb.WriteString("### Development workflow\n")

	if env == EnvLocal {
		sb.WriteString("Edit code locally. Managed services accessible via VPN (`zcli vpn up`).\n")
		sb.WriteString("Test locally, then deploy when ready.\n\n")
		return
	}

	// Container environment — strategy determines iteration flow.
	switch strategy {
	case StrategyPushGit:
		sb.WriteString("Edit code on the SSHFS mount. When ready:\n")
		sb.WriteString("1. Commit: `ssh {dev} \"cd /var/www && git add -A && git commit -m 'description'\"`\n")
		sb.WriteString("2. Push: `zerops_deploy targetService=\"{dev}\" setup=\"dev\" strategy=\"git-push\"`\n")
		sb.WriteString("3. If CI/CD configured, stage deploys automatically.\n\n")
	case StrategyManual:
		sb.WriteString("Edit code on the SSHFS mount. Tell the user when changes are ready to deploy.\n")
		sb.WriteString("User controls deployment timing.\n\n")
	default:
		if strategy == "" {
			sb.WriteString("Implement your changes. Set deploy strategy before deploying:\n")
			sb.WriteString("`zerops_workflow action=\"strategy\" strategies={...}`\n\n")
		} else {
			writePushDevWorkflow(sb, state)
		}
	}
}

// writePushDevWorkflow writes the push-dev iteration cycle.
func writePushDevWorkflow(sb *strings.Builder, state *DeployState) {
	devHostname := ""
	for _, t := range state.Targets {
		if t.Role == DeployRoleDev || t.Role == DeployRoleSimple {
			devHostname = t.Hostname
			break
		}
	}
	if devHostname == "" {
		return
	}

	if state.Mode == PlanModeSimple {
		fmt.Fprintf(sb, "Edit code on `/var/www/%s/`. After changes:\n", devHostname)
		fmt.Fprintf(sb, "- `zerops_deploy targetService=\"%s\" setup=\"prod\"` — server auto-starts with healthCheck\n", devHostname)
		fmt.Fprintf(sb, "- `zerops_verify serviceHostname=\"%s\"`\n\n", devHostname)
		return
	}

	// Dev/standard: manual SSH start cycle.
	fmt.Fprintf(sb, "Edit code on `/var/www/%s/`. Iteration cycle:\n", devHostname)
	fmt.Fprintf(sb, "1. Edit files on mount — changes appear instantly in container\n")
	fmt.Fprintf(sb, "2. Start/restart server: `ssh %s \"cd /var/www && {start_command}\"` (Bash `run_in_background=true`)\n", devHostname)
	fmt.Fprintf(sb, "3. Test: `ssh %s \"curl -s localhost:{port}/health\"` | jq .\n", devHostname)
	sb.WriteString("4. Repeat until working\n")
	sb.WriteString("- Code-only changes: restart server. No redeploy needed.\n")
	sb.WriteString("- zerops.yaml changes (envVariables, ports): redeploy required — new container kills all SSH sessions.\n")
	fmt.Fprintf(sb, "- After redeploy: open NEW SSH to %s (old sessions dead, exit 255). Then start server again.\n\n", devHostname)
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
	if current == "" {
		sb.WriteString("Not set. Before deploying, discuss with the user and choose:\n")
		for strategy, d := range strategyDescriptions {
			fmt.Fprintf(sb, "- %s (%s)\n", strategy, d)
		}
		sb.WriteString("Set via: `zerops_workflow action=\"strategy\" strategies={...}`\n\n")
		return
	}
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

func writeStandardWorkflow(sb *strings.Builder, targets []DeployTarget) {
	pairs := findDevStagePairs(targets)
	step := 1
	for _, p := range pairs {
		fmt.Fprintf(sb, "%d. Deploy to dev: `zerops_deploy targetService=\"%s\" setup=\"dev\"` — new container, all SSH sessions to %s die\n", step, p.dev, p.dev)
		step++
		fmt.Fprintf(sb, "%d. Start server on dev via NEW SSH connection (old sessions dead, dev uses idle start zsc noop)\n", step)
		step++
		fmt.Fprintf(sb, "%d. Verify dev: `zerops_verify serviceHostname=\"%s\"`\n", step, p.dev)
		step++
		fmt.Fprintf(sb, "%d. Deploy to stage: `zerops_deploy sourceService=\"%s\" targetService=\"%s\" setup=\"prod\"`\n", step, p.dev, p.stage)
		sb.WriteString("   Stage auto-starts (real start command + healthCheck)\n")
		step++
		fmt.Fprintf(sb, "%d. Verify stage: `zerops_verify serviceHostname=\"%s\"`\n", step, p.stage)
		step++
	}
}

func writeDevWorkflow(sb *strings.Builder, targets []DeployTarget) {
	step := 1
	for _, t := range targets {
		if t.Role != DeployRoleDev {
			continue
		}
		fmt.Fprintf(sb, "%d. Deploy: `zerops_deploy targetService=\"%s\" setup=\"dev\"` — new container, all SSH sessions to %s die\n", step, t.Hostname, t.Hostname)
		step++
		fmt.Fprintf(sb, "%d. Start server via NEW SSH connection (old sessions dead, dev uses idle start zsc noop)\n", step)
		step++
		fmt.Fprintf(sb, "%d. Verify: `zerops_verify serviceHostname=\"%s\"`\n", step, t.Hostname)
		step++
	}
}

func writeSimpleWorkflow(sb *strings.Builder, targets []DeployTarget) {
	step := 1
	for _, t := range targets {
		if t.Role != DeployRoleSimple {
			continue
		}
		fmt.Fprintf(sb, "%d. Deploy: `zerops_deploy targetService=\"%s\" setup=\"prod\"` — server auto-starts\n", step, t.Hostname)
		step++
		fmt.Fprintf(sb, "%d. Verify: `zerops_verify serviceHostname=\"%s\"`\n", step, t.Hostname)
		step++
	}
}

// devStagePair groups a dev hostname with its stage hostname.
type devStagePair struct{ dev, stage string }

// findDevStagePairs extracts ordered dev→stage pairs from targets.
func findDevStagePairs(targets []DeployTarget) []devStagePair {
	var pairs []devStagePair
	for i, t := range targets {
		if t.Role != DeployRoleDev {
			continue
		}
		stage := "UNKNOWN"
		if i+1 < len(targets) && targets[i+1].Role == DeployRoleStage {
			stage = targets[i+1].Hostname
		}
		pairs = append(pairs, devStagePair{dev: t.Hostname, stage: stage})
	}
	return pairs
}

func writeLocalWorkflow(sb *strings.Builder, targets []DeployTarget) {
	if len(targets) == 0 {
		return
	}
	step := 1
	for _, t := range targets {
		setup := RecipeSetupProd
		if t.Role == DeployRoleDev {
			setup = RecipeSetupDev
		}
		fmt.Fprintf(sb, "%d. Deploy: `zerops_deploy targetService=\"%s\" setup=\"%s\"` — uploads code, triggers build\n", step, t.Hostname, setup)
		step++
		fmt.Fprintf(sb, "%d. Verify: `zerops_verify serviceHostname=\"%s\"` — check health + subdomain\n", step, t.Hostname)
		step++
	}
}

func writeIterationEscalation(sb *strings.Builder, iteration int) {
	fmt.Fprintf(sb, "### Iteration %d — Diagnostic escalation\n", iteration)
	switch iteration {
	case 1:
		sb.WriteString("Check `zerops_logs severity=\"error\"`. Build failed? → review buildLogs from deploy response.\n")
		sb.WriteString("Container crash? → check start command, ports, env vars.\n")
	case 2:
		sb.WriteString("Systematic check: zerops.yaml config (ports, start, deployFiles),\n")
		sb.WriteString("env var references (typos = literal strings!), runtime version compatibility.\n")
	default:
		sb.WriteString("Present diagnostic summary to user: exact error from logs,\n")
		sb.WriteString("current config state, env var values. User decides next step.\n")
	}
}

func hasRole(targets []DeployTarget, role string) bool {
	for _, t := range targets {
		if t.Role == role {
			return true
		}
	}
	return false
}
