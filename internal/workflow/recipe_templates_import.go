package workflow

import (
	"fmt"
	"strings"
)

// GenerateEnvImportYAML returns the import.yaml for a specific environment tier.
// Generates structural YAML with platform-knowledge comments. Framework-specific
// comments are the agent's responsibility — the 30% comment ratio checker enforces this.
func GenerateEnvImportYAML(plan *RecipePlan, envIndex int) string {
	if envIndex < 0 || envIndex >= len(envTiers) {
		return ""
	}

	var b strings.Builder

	writeEnvHeader(&b, plan, envIndex)
	writeProjectSection(&b, plan, envIndex)

	b.WriteString("\nservices:\n")

	for _, target := range plan.Targets {
		if isRuntimeService(target.Role) && envIndex <= 1 {
			writeDevService(&b, plan, target, envIndex)
			writeStageService(&b, plan, target, envIndex)
		} else {
			writeSingleService(&b, plan, target, envIndex)
		}
	}

	return b.String()
}

// writeEnvHeader writes the file-level comment block describing the tier purpose.
func writeEnvHeader(b *strings.Builder, plan *RecipePlan, envIndex int) {
	desc := envDescription(plan, envIndex)
	full := envTiers[envIndex].IntroLabel + " " + desc
	for _, line := range wrapText(full, 78) {
		fmt.Fprintf(b, "# %s\n", line)
	}
	b.WriteByte('\n')
}

// writeProjectSection writes the project: block.
func writeProjectSection(b *strings.Builder, plan *RecipePlan, envIndex int) {
	projectName := fmt.Sprintf("%s-%s", plan.Slug, envTiers[envIndex].Suffix)

	if plan.Research.NeedsAppSecret {
		b.WriteString("#zeropsPreprocessor=on\n\n")
	}

	b.WriteString("project:\n")
	fmt.Fprintf(b, "  name: %s\n", projectName)

	if envIndex == 5 {
		writeComment(b,
			"SERIOUS core: dedicated infrastructure for balancer,",
			"logging, and metrics. Required for production scale.")
		b.WriteString("  corePackage: SERIOUS\n")
	}

	if plan.Research.NeedsAppSecret && plan.Research.AppSecretKey != "" {
		b.WriteString("  envVariables:\n")
		fmt.Fprintf(b, "    %s: <@generateRandomString(<32>)>\n", plan.Research.AppSecretKey)
	}
}

// writeDevService writes a dev service block for env 0-1.
func writeDevService(b *strings.Builder, plan *RecipePlan, target RecipeTarget, _ int) {
	fmt.Fprintf(b, "  - hostname: %sdev\n", target.Hostname)
	fmt.Fprintf(b, "    type: %s\n", target.Type)
	b.WriteString("    zeropsSetup: dev\n")
	writeServiceBuildFromGit(b, plan)
	b.WriteString("    enableSubdomainAccess: true\n")
	writeAutoscaling(b, target, 0) // env 0-1 share same scaling
	b.WriteByte('\n')
}

// writeStageService writes a stage service block for env 0-1.
func writeStageService(b *strings.Builder, plan *RecipePlan, target RecipeTarget, _ int) {
	fmt.Fprintf(b, "  - hostname: %sstage\n", target.Hostname)
	fmt.Fprintf(b, "    type: %s\n", target.Type)
	b.WriteString("    zeropsSetup: prod\n")
	writeServiceBuildFromGit(b, plan)
	b.WriteString("    enableSubdomainAccess: true\n")
	writeAutoscaling(b, target, 0)
	b.WriteByte('\n')
}

// writeSingleService writes a service entry for env 2-5.
func writeSingleService(b *strings.Builder, plan *RecipePlan, target RecipeTarget, envIndex int) {
	if IsDataService(target.Role) {
		writeDataServiceComment(b, plan, target, envIndex)
	}

	fmt.Fprintf(b, "  - hostname: %s\n", target.Hostname)
	fmt.Fprintf(b, "    type: %s\n", target.Type)

	if !isRuntimeService(target.Role) {
		b.WriteString("    priority: 10\n")
	}

	if IsDataService(target.Role) {
		if envIndex == 5 {
			b.WriteString("    mode: HA\n")
		} else {
			b.WriteString("    mode: NON_HA\n")
		}
	}

	if isRuntimeService(target.Role) {
		b.WriteString("    zeropsSetup: prod\n")
		writeServiceBuildFromGit(b, plan)
	}

	if target.Role == RecipeRoleApp {
		b.WriteString("    enableSubdomainAccess: true\n")
	}

	if isRuntimeService(target.Role) && envIndex >= 4 {
		b.WriteString("    minContainers: 2\n")
	}

	writeAutoscaling(b, target, envIndex)
	b.WriteByte('\n')
}

// writeDataServiceComment writes platform-knowledge comments for data services.
// These are always-true regardless of framework.
func writeDataServiceComment(b *strings.Builder, plan *RecipePlan, target RecipeTarget, envIndex int) {
	dbName := dataServiceTypeName(target)

	switch envIndex {
	case 0, 1:
		devH, stageH := "", ""
		for _, t := range plan.Targets {
			if isRuntimeService(t.Role) {
				devH = t.Hostname + "dev"
				stageH = t.Hostname + "stage"
				break
			}
		}
		writeComment(b,
			fmt.Sprintf("%s single-node database — shared by %s and %s.", dbName, devH, stageH),
			"NON_HA for dev/staging. Priority 10 starts DB before app.")
	case 2:
		writeComment(b,
			fmt.Sprintf("%s in Zerops — connect from local via 'zcli vpn up'.", dbName),
			"Priority 10 starts DB before the app service.")
	case 3:
		writeComment(b,
			fmt.Sprintf("%s single-node for staging — data is replaceable.", dbName),
			"Priority 10 starts DB first.")
	case 4:
		writeComment(b,
			fmt.Sprintf("%s single-node. Zerops encrypts and backs up automatically.", dbName),
			"Consider HA mode for high-traffic (see env 5).",
			"Priority 10 starts DB before the app.")
	case 5:
		writeComment(b,
			fmt.Sprintf("%s HA — replicates across nodes, no single point of failure.", dbName),
			"Dedicated CPU for consistent query performance.",
			"Priority 10 starts DB before app containers.")
	}
}

// writeServiceBuildFromGit writes the buildFromGit URL.
func writeServiceBuildFromGit(b *strings.Builder, plan *RecipePlan) {
	fmt.Fprintf(b, "    buildFromGit: %s%s-app\n", RecipeAppRepoBase, plan.Slug)
}

// writeAutoscaling writes the verticalAutoscaling block per tier.
func writeAutoscaling(b *strings.Builder, target RecipeTarget, envIndex int) {
	isRT := isRuntimeService(target.Role)
	isData := IsDataService(target.Role)
	if !isRT && !isData {
		return
	}

	b.WriteString("    verticalAutoscaling:\n")

	switch {
	case envIndex <= 2:
		if isRT {
			b.WriteString("      minRam: 0.5\n")
		} else {
			b.WriteString("      minRam: 0.25\n")
		}

	case envIndex == 3:
		if isRT {
			b.WriteString("      minRam: 0.5\n")
		} else {
			b.WriteString("      minRam: 0.25\n")
		}
		b.WriteString("      minFreeRamGB: 0.25\n")

	case envIndex == 4:
		if isRT {
			b.WriteString("      minRam: 0.25\n")
			b.WriteString("      minFreeRamGB: 0.125\n")
		} else {
			b.WriteString("      minRam: 0.25\n")
			b.WriteString("      minFreeRamGB: 0.125\n")
		}

	case envIndex == 5:
		b.WriteString("      cpuMode: DEDICATED\n")
		if isRT {
			b.WriteString("      minRam: 0.5\n")
			b.WriteString("      minFreeRamGB: 0.25\n")
		} else {
			b.WriteString("      minRam: 1\n")
			b.WriteString("      minFreeRamGB: 0.5\n")
		}
	}
}

// --- Comment helpers ---

// writeComment writes a multi-line YAML comment at service indent (2 spaces).
// Lines auto-wrap to fit within 80 total characters.
func writeComment(b *strings.Builder, lines ...string) {
	const indent = "  "
	const maxWidth = 80 - 2 - 2 // 80 - indent - "# "
	for _, line := range lines {
		if len(line) <= maxWidth {
			fmt.Fprintf(b, "%s# %s\n", indent, line)
		} else {
			for _, wrapped := range wrapText(line, maxWidth) {
				fmt.Fprintf(b, "%s# %s\n", indent, wrapped)
			}
		}
	}
}

// wrapText wraps text to the given width, breaking at word boundaries.
func wrapText(text string, width int) []string {
	var lines []string
	for paragraph := range strings.SplitSeq(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		current := words[0]
		for _, w := range words[1:] {
			if len(current)+1+len(w) > width {
				lines = append(lines, current)
				current = w
			} else {
				current += " " + w
			}
		}
		lines = append(lines, current)
	}
	return lines
}

// --- Data helpers ---

// dataServiceTypeName returns a human-readable name for a data service target.
func dataServiceTypeName(target RecipeTarget) string {
	base := strings.SplitN(target.Type, "@", 2)[0]
	names := map[string]string{
		"postgresql": "PostgreSQL", "mariadb": "MariaDB", "mysql": "MySQL",
		"mongodb": "MongoDB", "keydb": "KeyDB", "valkey": "Valkey",
		"elasticsearch": "Elasticsearch", "opensearch": "OpenSearch",
		"rabbitmq": "RabbitMQ", "nats": "NATS",
	}
	if name, ok := names[strings.ToLower(base)]; ok {
		return name
	}
	return titleCase(base)
}
