package server

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// buildPostBootstrapOrientation generates per-service operational guidance
// when bootstrapped ServiceMetas exist. Returns empty string if no metas.
func buildPostBootstrapOrientation(
	metas []*workflow.ServiceMeta,
	services []platform.ServiceStack,
	selfHostname string,
) string {
	// Filter to complete metas only.
	var complete []*workflow.ServiceMeta
	for _, m := range metas {
		if m.IsComplete() {
			complete = append(complete, m)
		}
	}
	if len(complete) == 0 {
		return ""
	}

	// Build lookup maps from live API.
	typeMap := make(map[string]string)
	statusMap := make(map[string]string)
	for _, svc := range services {
		if svc.IsSystem() || svc.Name == selfHostname {
			continue
		}
		typeMap[svc.Name] = svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
		statusMap[svc.Name] = svc.Status
	}

	var b strings.Builder
	b.WriteString("## Your Project — Bootstrapped\n\n")
	b.WriteString("ZCP helps you manage this project. Key tools:\n")
	b.WriteString("- zerops_knowledge query=\"...\" — runtime docs, recipes, schemas\n")
	b.WriteString("- zerops_discover — current service state and env vars\n")
	b.WriteString("- zerops_workflow — guided workflows (debug, configure, bootstrap)\n\n")

	// Per-service blocks.
	runtimeSeen := make(map[string]bool)
	for _, m := range complete {
		svcType := typeMap[m.Hostname]
		status := statusMap[m.Hostname]
		if status == "" {
			status = "UNKNOWN"
		}

		writeServiceBlock(&b, m, svcType, status)

		// Track runtime base for knowledge pointers.
		if svcType != "" {
			base, _, _ := strings.Cut(svcType, "@")
			runtimeSeen[base] = true
		}

		// Stage service (standard mode).
		if m.StageHostname != "" {
			stageType := typeMap[m.StageHostname]
			stageStatus := statusMap[m.StageHostname]
			if stageStatus == "" {
				stageStatus = "UNKNOWN"
			}
			writeStageBlock(&b, m, stageType, stageStatus)
		}
	}

	// Managed services (not in metas but in live API).
	writeManagedServices(&b, complete, services, selfHostname)

	// Strategy section.
	writeStrategySection(&b, complete)

	// Knowledge pointers.
	if len(runtimeSeen) > 0 {
		b.WriteString("### Knowledge\n")
		for base := range runtimeSeen {
			fmt.Fprintf(&b, "- zerops_knowledge query=\"%s\"\n", base)
		}
		b.WriteString("\n")
	}

	// Operations.
	b.WriteString("### Operations\n")
	b.WriteString("- Debug: zerops_workflow action=\"start\" workflow=\"debug\"\n")
	b.WriteString("- Configure: zerops_workflow action=\"start\" workflow=\"configure\"\n")
	b.WriteString("- Scale: zerops_scale serviceHostname=\"...\"\n")

	return b.String()
}

// writeServiceBlock writes the per-service orientation block for a runtime service.
func writeServiceBlock(b *strings.Builder, m *workflow.ServiceMeta, svcType, status string) {
	typeStr := svcType
	if typeStr == "" {
		typeStr = "runtime"
	}

	mode := m.Mode
	if mode == "" {
		mode = "standard"
	}

	fmt.Fprintf(b, "### %s (%s) — %s, %s mode\n", m.Hostname, typeStr, status, mode)
	fmt.Fprintf(b, "Mount: /var/www/%s/\n", m.Hostname)
	fmt.Fprintf(b, "Commands: ssh %s \"cd /var/www && ...\"\n", m.Hostname)

	switch mode {
	case "dev", "standard":
		// Dev services use zsc noop — server managed via SSH.
		b.WriteString("Server: manual start via SSH (uses zsc noop — no auto-start)\n")
		fmt.Fprintf(b, "Deploy: zerops_deploy targetService=\"%s\"\n", m.Hostname)
		b.WriteString("After deploy: new container — restart server via SSH (subdomain persists)\n")
		b.WriteString("Code changes on mount: restart server (no redeploy needed)\n")
	case "simple":
		fmt.Fprintf(b, "Deploy: zerops_deploy targetService=\"%s\"\n", m.Hostname)
		b.WriteString("Server auto-starts after deploy (healthCheck monitors)\n")
	}
	b.WriteString("\n")
}

// writeStageBlock writes the stage service block for standard mode.
func writeStageBlock(b *strings.Builder, m *workflow.ServiceMeta, stageType, stageStatus string) {
	typeStr := stageType
	if typeStr == "" {
		typeStr = "runtime"
	}
	fmt.Fprintf(b, "### %s (%s) — %s, stage\n", m.StageHostname, typeStr, stageStatus)
	fmt.Fprintf(b, "Deploy from dev: zerops_deploy sourceService=\"%s\" targetService=\"%s\"\n", m.Hostname, m.StageHostname)
	b.WriteString("Server auto-starts after deploy (healthCheck monitors)\n\n")
}

// writeManagedServices lists services that are in the live API but not in metas (managed services).
func writeManagedServices(b *strings.Builder, metas []*workflow.ServiceMeta, services []platform.ServiceStack, selfHostname string) {
	metaHostnames := make(map[string]bool)
	for _, m := range metas {
		metaHostnames[m.Hostname] = true
		if m.StageHostname != "" {
			metaHostnames[m.StageHostname] = true
		}
	}

	var managed []platform.ServiceStack
	for _, svc := range services {
		if svc.IsSystem() || svc.Name == selfHostname {
			continue
		}
		if !metaHostnames[svc.Name] {
			managed = append(managed, svc)
		}
	}

	for _, svc := range managed {
		fmt.Fprintf(b, "### %s (%s) — %s\n",
			svc.Name,
			svc.ServiceStackTypeInfo.ServiceStackTypeVersionName,
			svc.Status)
		b.WriteString("Env vars: zerops_discover includeEnvs=true\n\n")
	}
}

// writeStrategySection writes strategy-specific guidance based on the dominant strategy.
func writeStrategySection(b *strings.Builder, metas []*workflow.ServiceMeta) {
	strategies := make(map[string]int)
	for _, m := range metas {
		if m.DeployStrategy != "" {
			strategies[m.DeployStrategy]++
		}
	}

	b.WriteString("### Deploy Strategy")

	if len(strategies) == 0 {
		b.WriteString("\nNo strategy set yet. Choose one:\n")
		b.WriteString("→ zerops_workflow action=\"strategy\" strategies={\"hostname\":\"push-dev\"}\n\n")
		return
	}

	// Find dominant strategy.
	var dominant string
	var maxCount int
	for s, c := range strategies {
		if c > maxCount {
			dominant = s
			maxCount = c
		}
	}

	switch dominant {
	case workflow.StrategyManual:
		b.WriteString(": manual\n")
		b.WriteString("You control when to deploy. Call zerops_deploy directly.\n")
		b.WriteString("Switch: zerops_workflow action=\"strategy\" strategies={...}\n\n")
	case workflow.StrategyPushDev:
		b.WriteString(": push-dev\n")
		b.WriteString("Deploy via guided workflow: zerops_workflow action=\"start\" workflow=\"deploy\"\n")
		b.WriteString("Switch: zerops_workflow action=\"strategy\" strategies={...}\n\n")
	case workflow.StrategyCICD:
		b.WriteString(": ci-cd\n")
		b.WriteString("Deploys happen via git webhook: zerops_workflow action=\"start\" workflow=\"cicd\"\n")
		b.WriteString("Switch: zerops_workflow action=\"strategy\" strategies={...}\n\n")
	default:
		b.WriteString("\n")
		b.WriteString("Switch: zerops_workflow action=\"strategy\" strategies={...}\n\n")
	}
}
