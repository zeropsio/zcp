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
		if IsRuntimeService(target.Role) && !IsUtilityType(target.Type) && envIndex <= 1 {
			writeDevService(&b, plan, target)
			writeStageService(&b, plan, target)
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

	// Agent-authored project comment (e.g. "APP_KEY is used for AES-256-CBC
	// encryption and shared across containers so sessions stay valid."). Emitted
	// above the project: line at indent 0 so it introduces the whole block.
	writeAgentCommentAtIndent(b, plan.ProjectComment, "")

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
func writeDevService(b *strings.Builder, plan *RecipePlan, target RecipeTarget) {
	writeAgentServiceComment(b, plan, target.Hostname)
	fmt.Fprintf(b, "  - hostname: %sdev\n", target.Hostname)
	fmt.Fprintf(b, "    type: %s\n", target.Type)
	b.WriteString("    zeropsSetup: dev\n")
	writeRecipeAppBuildFromGit(b, plan)
	if target.Role == RecipeRoleApp {
		b.WriteString("    enableSubdomainAccess: true\n")
	}
	writeAutoscaling(b, target, 0) // env 0-1 share same scaling
	b.WriteByte('\n')
}

// writeStageService writes a stage service block for env 0-1.
func writeStageService(b *strings.Builder, plan *RecipePlan, target RecipeTarget) {
	writeAgentServiceComment(b, plan, target.Hostname)
	fmt.Fprintf(b, "  - hostname: %sstage\n", target.Hostname)
	fmt.Fprintf(b, "    type: %s\n", target.Type)
	fmt.Fprintf(b, "    zeropsSetup: %s\n", recipeSetupName(target.Role, false))
	writeRecipeAppBuildFromGit(b, plan)
	if target.Role == RecipeRoleApp {
		b.WriteString("    enableSubdomainAccess: true\n")
	}
	writeAutoscaling(b, target, 0)
	b.WriteByte('\n')
}

// writeSingleService writes a service entry for env 2-5 (and non-runtime services in env 0-1).
func writeSingleService(b *strings.Builder, plan *RecipePlan, target RecipeTarget, envIndex int) {
	// Agent-authored framework-specific comment (shared across all 6 envs for this
	// hostname). Emitted first so it reads as the "what this is" intro.
	writeAgentServiceComment(b, plan, target.Hostname)

	// Platform-knowledge comments per service category.
	if ServiceSupportsMode(target.Type) {
		writeManagedServiceComment(b, plan, target, envIndex)
	} else if IsObjectStorageType(target.Type) {
		writeObjectStorageComment(b, envIndex)
	}
	// Runtime + utility: no template comments (agent writes framework-specific ones).

	fmt.Fprintf(b, "  - hostname: %s\n", target.Hostname)
	fmt.Fprintf(b, "    type: %s\n", target.Type)

	// Priority: non-runtime services start before app.
	if !IsRuntimeService(target.Role) {
		b.WriteString("    priority: 10\n")
	}

	// Mode: only managed services that support it.
	if ServiceSupportsMode(target.Type) {
		if envIndex == 5 {
			b.WriteString("    mode: HA\n")
		} else {
			b.WriteString("    mode: NON_HA\n")
		}
	}

	// Recipe runtime services: zeropsSetup + buildFromGit from recipe app repo.
	if IsRuntimeService(target.Role) {
		fmt.Fprintf(b, "    zeropsSetup: %s\n", recipeSetupName(target.Role, false))
		writeRecipeAppBuildFromGit(b, plan)
	}

	// Utility services: zeropsSetup + buildFromGit from utility repo.
	if IsUtilityType(target.Type) && !IsRuntimeService(target.Role) {
		b.WriteString("    zeropsSetup: app\n")
		fmt.Fprintf(b, "    buildFromGit: %s\n", utilityBuildFromGitURL(target.Type))
	}

	// Subdomain: app role + utility services with web UI.
	if target.Role == RecipeRoleApp || IsUtilityType(target.Type) {
		b.WriteString("    enableSubdomainAccess: true\n")
	}

	// minContainers: runtime services in production tiers.
	if IsRuntimeService(target.Role) && envIndex >= 4 {
		b.WriteString("    minContainers: 2\n")
	}

	// Object storage: size and policy instead of autoscaling.
	if IsObjectStorageType(target.Type) {
		b.WriteString("    objectStorageSize: 1\n")
		b.WriteString("    objectStoragePolicy: private\n")
	}

	// Vertical autoscaling: only services that support it.
	if ServiceSupportsAutoscaling(target.Type) {
		writeAutoscaling(b, target, envIndex)
	}

	b.WriteByte('\n')
}

// writeManagedServiceComment writes platform-knowledge comments for managed services.
// Generalizes across db, cache, and search roles — not database-specific.
func writeManagedServiceComment(b *strings.Builder, plan *RecipePlan, target RecipeTarget, envIndex int) {
	typeName := dataServiceTypeName(target)
	kind := managedServiceKind(target.Role)

	switch envIndex {
	case 0, 1:
		hosts := runtimeHostnameList(plan, envIndex)
		writeComment(b,
			fmt.Sprintf("%s single-node %s — shared by %s.", typeName, kind, hosts),
			fmt.Sprintf("NON_HA for dev/staging. Priority 10 starts %s before app.", kind))
	case 2:
		writeComment(b,
			fmt.Sprintf("%s in Zerops — connect from local via 'zcli vpn up'.", typeName),
			fmt.Sprintf("Priority 10 starts %s before the app service.", kind))
	case 3:
		writeComment(b,
			fmt.Sprintf("%s single-node for staging — data is replaceable.", typeName),
			fmt.Sprintf("Priority 10 starts %s first.", kind))
	case 4:
		writeComment(b,
			fmt.Sprintf("%s single-node. Consider HA mode for high-traffic (see env 5).", typeName),
			fmt.Sprintf("Priority 10 starts %s before the app.", kind))
	case 5:
		writeComment(b,
			fmt.Sprintf("%s HA — replicates across nodes, no single point of failure.", typeName),
			"Dedicated CPU for consistent performance.",
			fmt.Sprintf("Priority 10 starts %s before app containers.", kind))
	}
}

// writeObjectStorageComment writes platform-knowledge comments for object storage.
func writeObjectStorageComment(b *strings.Builder, envIndex int) {
	switch envIndex {
	case 0, 1:
		writeComment(b, "S3-compatible object storage (MinIO) for file uploads and assets.")
	case 2:
		writeComment(b, "S3-compatible object storage — access via S3 API with VPN or service env vars.")
	case 3:
		writeComment(b, "S3-compatible object storage for staging assets.")
	case 4, 5:
		writeComment(b, "S3-compatible object storage. Use external backups for critical data.")
	}
}

// runtimeHostnameList returns a natural-language list of runtime hostnames for comments.
func runtimeHostnameList(plan *RecipePlan, envIndex int) string {
	var names []string
	for _, t := range plan.Targets {
		if IsRuntimeService(t.Role) {
			if envIndex <= 1 {
				names = append(names, t.Hostname+"dev", t.Hostname+"stage")
			} else {
				names = append(names, t.Hostname)
			}
		}
	}
	return naturalJoin(names)
}

// writeRecipeAppBuildFromGit writes the buildFromGit URL for recipe app services.
func writeRecipeAppBuildFromGit(b *strings.Builder, plan *RecipePlan) {
	fmt.Fprintf(b, "    buildFromGit: %s%s-app\n", RecipeAppRepoBase, plan.Slug)
}

// writeAutoscaling writes the verticalAutoscaling block per tier.
// Caller must ensure the service type supports autoscaling.
func writeAutoscaling(b *strings.Builder, target RecipeTarget, envIndex int) {
	isRT := IsRuntimeService(target.Role)
	isData := IsDataService(target.Role)
	isUtil := IsUtilityType(target.Type)
	if !isRT && !isData {
		return
	}

	b.WriteString("    verticalAutoscaling:\n")

	switch {
	case envIndex <= 2:
		if isRT && !isUtil {
			b.WriteString("      minRam: 0.5\n")
		} else {
			b.WriteString("      minRam: 0.25\n")
		}

	case envIndex == 3:
		if isRT && !isUtil {
			b.WriteString("      minRam: 0.5\n")
		} else {
			b.WriteString("      minRam: 0.25\n")
		}
		b.WriteString("      minFreeRamGB: 0.25\n")

	case envIndex == 4:
		b.WriteString("      minRam: 0.25\n")
		b.WriteString("      minFreeRamGB: 0.125\n")

	case envIndex == 5:
		if isUtil {
			// Utilities (mailpit): lighter scaling, no DEDICATED.
			b.WriteString("      minRam: 0.25\n")
		} else {
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
}

// --- Comment helpers ---

// writeAgentServiceComment emits the agent-authored comment for a service at
// service indent (2 spaces), keyed by base hostname in plan.ServiceComments.
// No-op if the plan has no entry for this hostname (agent hasn't provided one yet).
func writeAgentServiceComment(b *strings.Builder, plan *RecipePlan, hostname string) {
	if plan == nil || len(plan.ServiceComments) == 0 {
		return
	}
	text, ok := plan.ServiceComments[hostname]
	if !ok || strings.TrimSpace(text) == "" {
		return
	}
	writeAgentCommentAtIndent(b, text, "  ")
}

// writeAgentCommentAtIndent wraps free-form text into # comment lines at the
// given indent. Preserves explicit newlines in the input (for paragraph breaks).
func writeAgentCommentAtIndent(b *strings.Builder, text, indent string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	maxWidth := max(80-len(indent)-2, 20) // "# "
	for _, line := range wrapText(text, maxWidth) {
		if line == "" {
			fmt.Fprintf(b, "%s#\n", indent)
		} else {
			fmt.Fprintf(b, "%s# %s\n", indent, line)
		}
	}
}

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
		"meilisearch": "Meilisearch", "qdrant": "Qdrant", "typesense": "Typesense",
		"clickhouse": "ClickHouse",
	}
	if name, ok := names[strings.ToLower(base)]; ok {
		return name
	}
	return titleCase(base)
}
