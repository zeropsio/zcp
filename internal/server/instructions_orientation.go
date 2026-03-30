package server

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// buildPostBootstrapOrientation generates per-service operational guidance
// from classified services. Returns empty string if no services to describe.
func buildPostBootstrapOrientation(cls serviceClassification) string {
	if len(cls.bootstrapped) == 0 && len(cls.managed) == 0 && len(cls.unmanaged) == 0 {
		return ""
	}

	// Build lookup maps from classification metadata.
	typeMap := cls.typeMap()
	statusMap := cls.statusMap()

	var b strings.Builder

	// Bootstrapped runtime services — full operational blocks.
	if len(cls.bootstrapped) > 0 {
		b.WriteString("## Your Project — Bootstrapped\n\n")
		b.WriteString("ZCP helps you manage this project. Key tools:\n")
		b.WriteString("- zerops_knowledge query=\"...\" — runtime docs, recipes, schemas\n")
		b.WriteString("- zerops_discover — current service state and env vars\n")
		b.WriteString("- zerops_workflow — guided workflows (debug, configure, bootstrap)\n\n")

		runtimeSeen := make(map[string]bool)
		for _, m := range cls.bootstrapped {
			svcType := typeMap[m.Hostname]
			status := statusMap[m.Hostname]
			if status == "" {
				status = "UNKNOWN"
			}
			writeServiceBlock(&b, m, svcType, status)

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

		// Knowledge pointers.
		if len(runtimeSeen) > 0 {
			b.WriteString("### Knowledge\n")
			for base := range runtimeSeen {
				fmt.Fprintf(&b, "- zerops_knowledge query=\"%s\"\n", base)
			}
			b.WriteString("\n")
		}

		// Strategy section.
		writeStrategySection(&b, cls.bootstrapped)
	}

	// Managed infrastructure (db, cache, storage).
	writeManagedSection(&b, cls.managed)

	// Unmanaged runtime services — adoption guidance.
	writeUnmanagedRuntimesSection(&b, cls.unmanaged, cls.mountPaths)

	// Operations (only when bootstrapped services exist).
	if len(cls.bootstrapped) > 0 {
		b.WriteString("### Operations\n")
		b.WriteString("- Debug: zerops_workflow action=\"start\" workflow=\"debug\"\n")
		b.WriteString("- Configure: zerops_workflow action=\"start\" workflow=\"configure\"\n")
		b.WriteString("- Scale: zerops_scale serviceHostname=\"...\"\n")
	}

	return b.String()
}

// typeMap builds hostname→type mapping from all classified services.
func (c *serviceClassification) typeMap() map[string]string {
	m := make(map[string]string, len(c.allServices))
	for _, svc := range c.allServices {
		m[svc.Name] = svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
	}
	return m
}

// statusMap builds hostname→status mapping from all classified services.
func (c *serviceClassification) statusMap() map[string]string {
	m := make(map[string]string, len(c.allServices))
	for _, svc := range c.allServices {
		m[svc.Name] = svc.Status
	}
	return m
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

// writeManagedSection lists managed infrastructure services (databases, caches, storage).
func writeManagedSection(b *strings.Builder, managed []platform.ServiceStack) {
	if len(managed) == 0 {
		return
	}
	b.WriteString("### Managed infrastructure\n")
	for _, svc := range managed {
		fmt.Fprintf(b, "- %s (%s) — %s\n",
			svc.Name,
			svc.ServiceStackTypeInfo.ServiceStackTypeVersionName,
			svc.Status)
	}
	b.WriteString("Env vars: zerops_discover includeEnvs=true\n\n")
}

// writeUnmanagedRuntimesSection lists runtime services without ZCP state and
// provides adoption guidance. mountPaths maps hostname → local mount path.
func writeUnmanagedRuntimesSection(b *strings.Builder, unmanaged []platform.ServiceStack, mountPaths map[string]string) {
	if len(unmanaged) == 0 {
		return
	}
	b.WriteString("### Runtime services needing adoption\n")
	for _, svc := range unmanaged {
		if path, ok := mountPaths[svc.Name]; ok {
			fmt.Fprintf(b, "- %s (%s) — %s — mounted at %s/\n",
				svc.Name,
				svc.ServiceStackTypeInfo.ServiceStackTypeVersionName,
				svc.Status, path)
		} else {
			fmt.Fprintf(b, "- %s (%s) — %s\n",
				svc.Name,
				svc.ServiceStackTypeInfo.ServiceStackTypeVersionName,
				svc.Status)
		}
	}
	b.WriteString("→ Adopt via: zerops_workflow action=\"start\" workflow=\"bootstrap\" (isExisting=true)\n\n")
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
