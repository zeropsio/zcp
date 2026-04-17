package workflow

import (
	"fmt"
	"strings"
)

// BriefingTarget is a lightweight, non-persisted service target for develop briefings.
// Derived from ServiceMeta + API at briefing time. Unlike DeployTarget, this is
// stateless — no session stores it.
type BriefingTarget struct {
	Hostname    string `json:"hostname"`
	Role        string `json:"role"`
	RuntimeType string `json:"runtimeType,omitempty"`
	HTTPSupport bool   `json:"httpSupport,omitempty"`
	Strategy    string `json:"strategy,omitempty"`
}

// DevelopBriefing is the stateless response from action="start" workflow="develop".
// No session is created — the agent gets knowledge upfront and works freely.
type DevelopBriefing struct {
	Targets  []BriefingTarget `json:"targets"`
	Mode     string           `json:"mode"`
	Strategy string           `json:"strategy"`
	Briefing string           `json:"briefing"`
}

// BuildBriefingTargets constructs BriefingTarget list from ServiceMetas.
// Returns targets and detected mode.
func BuildBriefingTargets(metas []*ServiceMeta) ([]BriefingTarget, string) {
	if len(metas) == 0 {
		return nil, ""
	}

	var targets []BriefingTarget
	mode := ""

	for _, m := range metas {
		metaMode := m.Mode
		if metaMode == "" {
			metaMode = PlanModeStandard
		}
		if mode == "" {
			mode = metaMode
		}

		targets = append(targets, BriefingTarget{
			Hostname: m.Hostname,
			Role:     RoleFromMode(metaMode, m.StageHostname),
			Strategy: m.EffectiveStrategy(),
		})

		if metaMode == PlanModeStandard && m.StageHostname != "" {
			targets = append(targets, BriefingTarget{
				Hostname: m.StageHostname,
				Role:     DeployRoleStage,
				Strategy: m.EffectiveStrategy(),
			})
		}
	}

	return targets, mode
}

// RoleFromMode derives the deploy role from plan mode and stage pairing.
// Used by both briefing target building and pre-flight role derivation.
func RoleFromMode(mode, stageHostname string) string {
	switch mode {
	case PlanModeSimple:
		return DeployRoleSimple
	case PlanModeDev:
		return DeployRoleDev
	default:
		if stageHostname != "" {
			return DeployRoleDev
		}
		return DeployRoleSimple
	}
}

// EnrichBriefingTargets populates RuntimeType and HTTPSupport on briefing targets
// from service API data.
func EnrichBriefingTargets(targets []BriefingTarget, typeMap map[string]string, httpMap map[string]bool) {
	for i := range targets {
		if rt, ok := typeMap[targets[i].Hostname]; ok {
			targets[i].RuntimeType = rt
		}
		targets[i].HTTPSupport = httpMap[targets[i].Hostname]
	}
}

// BuildDevelopBriefing generates the full stateless briefing text.
// Contains everything the LLM needs: service context, development workflow,
// platform rules, knowledge pointers, and strategy-aware close instructions.
func BuildDevelopBriefing(targets []BriefingTarget, strategy, mode string, env Environment, stateDir string) string {
	var sb strings.Builder

	sb.WriteString("## Development & Deploy\n\n")
	sb.WriteString("Discover what code exists on the service, implement what the user wants, then deploy and verify.\n")
	sb.WriteString("If the service has only a bootstrap verification server, replace it with the actual application.\n")
	sb.WriteString("If the service already has application code, modify it according to the user's request.\n\n")

	// Service summary.
	sb.WriteString("### Your services\n")
	writeBriefingTargetSummary(&sb, targets)
	if strategy != "" {
		fmt.Fprintf(&sb, "Mode: %s | Strategy: %s\n\n", mode, strategy)
	} else {
		fmt.Fprintf(&sb, "Mode: %s | Strategy: not set\n\n", mode)
	}

	// Checklist.
	sb.WriteString("### Checklist\n")
	fmt.Fprintf(&sb, "1. zerops.yaml must have `setup: dev` (dev services) and/or `setup: prod` (stage/simple) entries — canonical recipe names, NOT hostnames\n")
	sb.WriteString("2. Env var references (`${hostname_varName}`) must match real variables\n")
	if mode == PlanModeStandard || mode == PlanModeDev {
		sb.WriteString("3. Dev entry: `start: zsc noop --silent`, NO healthCheck\n")
	}
	if mode == PlanModeSimple {
		sb.WriteString("3. Entry must have real `start:` command and `healthCheck` (server auto-starts)\n")
	}
	if mode == PlanModeStandard {
		sb.WriteString("4. Stage entry: real `start:` command + `healthCheck` required\n")
	}
	sb.WriteString("5. **envVariables are NOT live until deploy.** Editing zerops.yaml does not activate env vars — they appear in the container only after `zerops_deploy`. Do NOT verify with `printenv` or SSH before deploying.\n")
	sb.WriteString("\n")

	// Development workflow — strategy and environment aware.
	writeBriefingDevWorkflow(&sb, targets, strategy, mode, env)

	// Platform rules.
	sb.WriteString("### Platform rules\n")
	sb.WriteString("- Deploy = new container — local files lost, only `deployFiles` content survives\n")
	sb.WriteString("- envVariables = declarative config, NOT live until deploy. Never check with `printenv` before deploying.\n")
	sb.WriteString("- `${hostname_varName}` typo = silent literal string, no error from platform\n")
	sb.WriteString("- Build container ≠ run container — different environment\n")
	if env == EnvContainer {
		sb.WriteString("- Code on SSHFS mount — deploy rebuilds, not transfers\n")
		for _, t := range targets {
			if t.Role != DeployRoleStage {
				fmt.Fprintf(&sb, "  - %s: `/var/www/%s/`\n", t.Hostname, t.Hostname)
			}
		}
		sb.WriteString("- Mount recovery: if SSHFS mount becomes stale after deploy, use `zerops_mount action=\"mount\"` to remount\n")
		sb.WriteString("- Agent Browser (agent-browser.dev) installed — use for browser-based testing/verification of deployed web apps\n")
	}
	sb.WriteString("- Service config changes (shared storage, scaling, nginx): use `zerops_import` with `override: true` to update existing services\n")
	sb.WriteString("\n")

	// Strategy note.
	writeStrategyNote(&sb, strategy)

	// Knowledge pointers.
	sb.WriteString(buildBriefingKnowledgeMap(targets))

	// Close instructions — strategy-aware.
	writeCloseInstructions(&sb, targets, strategy, mode, env)

	return sb.String()
}

func writeBriefingTargetSummary(sb *strings.Builder, targets []BriefingTarget) {
	for i, t := range targets {
		if t.Role == DeployRoleStage {
			continue
		}
		fmt.Fprintf(sb, "- %s (%s)", t.Hostname, t.Role)
		if i+1 < len(targets) && targets[i+1].Role == DeployRoleStage {
			fmt.Fprintf(sb, " → %s (stage)", targets[i+1].Hostname)
		}
		sb.WriteString("\n")
	}
}

func writeBriefingDevWorkflow(sb *strings.Builder, targets []BriefingTarget, strategy, mode string, env Environment) {
	sb.WriteString("### Development workflow\n")

	if env == EnvLocal {
		sb.WriteString("Edit code locally. Managed services accessible via VPN (`zcli vpn up`).\n")
		sb.WriteString("Test locally, then deploy when ready.\n\n")
		return
	}

	switch strategy {
	case StrategyPushGit:
		writeBriefingPushGitWorkflow(sb, targets)
	case StrategyManual:
		sb.WriteString("Edit code on the SSHFS mount. Tell the user when changes are ready to deploy.\n")
		sb.WriteString("User controls deployment timing.\n\n")
	default:
		if strategy == "" {
			sb.WriteString("Implement your changes. Set deploy strategy before deploying:\n")
			sb.WriteString("`zerops_workflow action=\"strategy\" strategies={...}`\n\n")
		} else {
			writeBriefingPushDevWorkflow(sb, targets, mode)
		}
	}
}

func writeBriefingPushGitWorkflow(sb *strings.Builder, targets []BriefingTarget) {
	for _, t := range targets {
		if t.Role == DeployRoleStage {
			continue
		}
		fmt.Fprintf(sb, "Edit code on `/var/www/%s/`. When ready:\n", t.Hostname)
		fmt.Fprintf(sb, "1. Commit: `ssh %s \"cd /var/www && git add -A && git commit -m 'description'\"`\n", t.Hostname)
		fmt.Fprintf(sb, "2. Push: `zerops_deploy targetService=\"%s\" strategy=\"git-push\"`\n", t.Hostname)
		sb.WriteString("3. If CI/CD configured, stage deploys automatically.\n\n")
		break // workflow section shows primary dev target; close instructions list all
	}
}

func writeBriefingPushDevWorkflow(sb *strings.Builder, targets []BriefingTarget, mode string) {
	devHostname := ""
	for _, t := range targets {
		if t.Role == DeployRoleDev || t.Role == DeployRoleSimple {
			devHostname = t.Hostname
			break
		}
	}
	if devHostname == "" {
		return
	}

	if mode == PlanModeSimple {
		fmt.Fprintf(sb, "Edit code on `/var/www/%s/`. After changes:\n", devHostname)
		fmt.Fprintf(sb, "- `zerops_deploy targetService=\"%s\" setup=\"prod\"` — server auto-starts with healthCheck\n", devHostname)
		fmt.Fprintf(sb, "- `zerops_verify serviceHostname=\"%s\"`\n\n", devHostname)
		return
	}

	fmt.Fprintf(sb, "Edit code on `/var/www/%s/`. Iteration cycle:\n", devHostname)
	fmt.Fprintf(sb, "1. Edit files on mount — changes appear instantly in container\n")
	fmt.Fprintf(sb, "2. Start/restart server: `ssh %s \"cd /var/www && {start_command}\"` (Bash `run_in_background=true`)\n", devHostname)
	fmt.Fprintf(sb, "3. Test: `ssh %s \"curl -s localhost:{port}/health\"` | jq .\n", devHostname)
	sb.WriteString("4. Repeat until working\n")
	sb.WriteString("- Code-only changes: restart server. No redeploy needed.\n")
	sb.WriteString("- zerops.yaml changes (envVariables, ports): redeploy required — new container kills all SSH sessions.\n")
	fmt.Fprintf(sb, "- After redeploy: open NEW SSH to %s (old sessions dead, exit 255). Then start server again.\n\n", devHostname)
}

// buildBriefingKnowledgeMap returns compact knowledge pointers personalized to briefing targets.
func buildBriefingKnowledgeMap(targets []BriefingTarget) string {
	var sb strings.Builder
	sb.WriteString("### Knowledge on demand\n")
	sb.WriteString("- zerops.yaml schema: `zerops_knowledge query=\"zerops.yaml schema\"`\n")

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

// writeCloseInstructions writes strategy-aware task close instructions.
// This tells the LLM HOW to close the task after work is done.
func writeCloseInstructions(sb *strings.Builder, targets []BriefingTarget, strategy, mode string, env Environment) {
	sb.WriteString("### Closing the task\n")

	switch strategy {
	case StrategyManual:
		sb.WriteString("Code is ready. Inform the user that changes are complete.\n")
		sb.WriteString("Deploy commands for reference (user controls timing):\n")
		writeBriefingDeployCommands(sb, targets, mode, env)
	case StrategyPushGit:
		// Find first dev hostname for prerequisite examples.
		devHost := ""
		for _, t := range targets {
			if t.Role != DeployRoleStage {
				devHost = t.Hostname
				break
			}
		}
		sb.WriteString("When code changes are complete:\n\n")
		sb.WriteString("**Ask the user:** Do you want to just push code to remote, or set up full CI/CD (automatic deploy on every push)?\n\n")
		sb.WriteString("#### Option A: Push code to remote\n\n")
		sb.WriteString("**Prerequisites:**\n")
		sb.WriteString("- `GIT_TOKEN` project env var — GitHub fine-grained token (Contents: Read and write) or GitLab token (write_repository)\n")
		sb.WriteString("- `.netrc` on the container for git auth:\n")
		fmt.Fprintf(sb, "  `ssh %s 'umask 077 && echo \"machine github.com login oauth2 password $GIT_TOKEN\" > ~/.netrc'`\n\n", devHost)
		sb.WriteString("**Steps:**\n")
		step := 1
		for _, t := range targets {
			if t.Role == DeployRoleStage {
				continue
			}
			fmt.Fprintf(sb, "%d. Commit: `ssh %s \"cd /var/www && git add -A && git commit -m 'description'\"`\n", step, t.Hostname)
			step++
			fmt.Fprintf(sb, "%d. Push: `zerops_deploy targetService=\"%s\" strategy=\"git-push\"`\n", step, t.Hostname)
			step++
		}
		sb.WriteString("\n#### Option B: Full CI/CD\n\n")
		sb.WriteString("Run: `zerops_workflow action=\"start\" workflow=\"cicd\"`\n")
		sb.WriteString("This sets up automatic deploy on every git push (GitHub Actions with zcli).\n")
	default:
		sb.WriteString("When code changes are complete, deploy and verify:\n")
		writeBriefingDeployCommands(sb, targets, mode, env)
	}
	sb.WriteString("\n")
}

func writeBriefingDeployCommands(sb *strings.Builder, targets []BriefingTarget, mode string, env Environment) {
	if env == EnvLocal {
		writeBriefingLocalDeploy(sb, targets)
		return
	}

	switch mode {
	case PlanModeStandard:
		writeBriefingStandardDeploy(sb, targets)
	case PlanModeDev:
		writeBriefingDevDeploy(sb, targets)
	case PlanModeSimple:
		writeBriefingSimpleDeploy(sb, targets)
	default:
		writeBriefingStandardDeploy(sb, targets)
	}
}

func writeBriefingStandardDeploy(sb *strings.Builder, targets []BriefingTarget) {
	step := 1
	for i, t := range targets {
		if t.Role != DeployRoleDev {
			continue
		}
		stage := "UNKNOWN"
		if i+1 < len(targets) && targets[i+1].Role == DeployRoleStage {
			stage = targets[i+1].Hostname
		}
		fmt.Fprintf(sb, "%d. `zerops_deploy targetService=\"%s\" setup=\"dev\"`\n", step, t.Hostname)
		step++
		fmt.Fprintf(sb, "%d. Start server via NEW SSH (old sessions dead after deploy)\n", step)
		step++
		fmt.Fprintf(sb, "%d. `zerops_verify serviceHostname=\"%s\"`\n", step, t.Hostname)
		step++
		fmt.Fprintf(sb, "%d. `zerops_deploy sourceService=\"%s\" targetService=\"%s\" setup=\"prod\"`\n", step, t.Hostname, stage)
		step++
		fmt.Fprintf(sb, "%d. `zerops_verify serviceHostname=\"%s\"`\n", step, stage)
		step++
	}
}

func writeBriefingDevDeploy(sb *strings.Builder, targets []BriefingTarget) {
	step := 1
	for _, t := range targets {
		if t.Role != DeployRoleDev {
			continue
		}
		fmt.Fprintf(sb, "%d. `zerops_deploy targetService=\"%s\" setup=\"dev\"`\n", step, t.Hostname)
		step++
		fmt.Fprintf(sb, "%d. Start server via NEW SSH\n", step)
		step++
		fmt.Fprintf(sb, "%d. `zerops_verify serviceHostname=\"%s\"`\n", step, t.Hostname)
		step++
	}
}

func writeBriefingSimpleDeploy(sb *strings.Builder, targets []BriefingTarget) {
	step := 1
	for _, t := range targets {
		if t.Role != DeployRoleSimple {
			continue
		}
		fmt.Fprintf(sb, "%d. `zerops_deploy targetService=\"%s\" setup=\"prod\"`\n", step, t.Hostname)
		step++
		fmt.Fprintf(sb, "%d. `zerops_verify serviceHostname=\"%s\"`\n", step, t.Hostname)
		step++
	}
}

func writeBriefingLocalDeploy(sb *strings.Builder, targets []BriefingTarget) {
	step := 1
	for _, t := range targets {
		setup := RecipeSetupProd
		if t.Role == DeployRoleDev {
			setup = RecipeSetupDev
		}
		fmt.Fprintf(sb, "%d. `zerops_deploy targetService=\"%s\" setup=\"%s\"`\n", step, t.Hostname, setup)
		step++
		fmt.Fprintf(sb, "%d. `zerops_verify serviceHostname=\"%s\"`\n", step, t.Hostname)
		step++
	}
}
