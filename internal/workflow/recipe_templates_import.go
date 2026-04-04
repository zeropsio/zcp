package workflow

import (
	"fmt"
	"strings"
)

// GenerateEnvImportYAML returns the import.yaml for a specific environment tier.
// Generates rich, framework-aware comments using plan research data.
func GenerateEnvImportYAML(plan *RecipePlan, envIndex int) string {
	if envIndex < 0 || envIndex >= len(envTiers) {
		return ""
	}

	var b strings.Builder

	writeEnvHeader(&b, plan, envIndex)
	writeProjectSection(&b, plan, envIndex)

	b.WriteString("\nservices:\n")

	// All targets appear in all environments — recipes don't have per-env filtering.
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

// writeEnvHeader writes the file-level comment describing the environment purpose.
func writeEnvHeader(b *strings.Builder, plan *RecipePlan, envIndex int) {
	desc := envDescription(plan, envIndex)
	// envDescription starts with "environment ..." — prefix with label.
	full := envTiers[envIndex].IntroLabel + " " + desc
	// Wrap to 80 chars per comment line.
	for _, line := range wrapText(full, 78) {
		fmt.Fprintf(b, "# %s\n", line)
	}
	b.WriteByte('\n')
}

// writeProjectSection writes the project: block with name, corePackage, and envVariables.
func writeProjectSection(b *strings.Builder, plan *RecipePlan, envIndex int) {
	projectName := fmt.Sprintf("%s-%s", plan.Slug, envTiers[envIndex].Suffix)

	if plan.Research.NeedsAppSecret {
		b.WriteString("#zeropsPreprocessor=on\n\n")
	}

	b.WriteString("project:\n")
	fmt.Fprintf(b, "  name: %s\n", projectName)

	if envIndex == 5 {
		writeServiceComment(b,
			"SERIOUS core: dedicated infrastructure for the project's",
			"balancer, logging, and metrics. Eliminates shared-tenant",
			"overhead — essential for consistent latency at scale.")
		b.WriteString("  corePackage: SERIOUS\n")
	}

	if plan.Research.NeedsAppSecret && plan.Research.AppSecretKey != "" {
		writeServiceComment(b,
			fmt.Sprintf("%s at project level — all services sharing", plan.Research.AppSecretKey),
			"the database use the same key, so encrypted data",
			"(sessions, cookies) remains valid across services.")
		b.WriteString("  envVariables:\n")
		fmt.Fprintf(b, "    %s: <@generateRandomString(<32>)>\n", plan.Research.AppSecretKey)
	}
}

// writeDevService writes a dev service block for env 0-1 with rich comments.
func writeDevService(b *strings.Builder, plan *RecipePlan, target RecipeTarget, envIndex int) {
	hostname := target.Hostname + "dev"

	if envIndex == 0 {
		devHint := devStartHint(plan)
		writeServiceComment(b,
			"Development workspace for AI agents. Zerops pulls source code and zerops.yaml from",
			"the 'buildFromGit' repo, then uses the 'dev' zeropsSetup \u2014 which deploys the full",
			fmt.Sprintf("source tree with %s pre-installed. Agent SSHs in and develops", runtimeShortName(plan.RuntimeType)),
			fmt.Sprintf("directly: %s. Subdomain gives the agent a URL to inspect output.", devHint))
	} else {
		devHint := devStartHint(plan)
		pkgCmd := packageAddCmd(plan.Research.PackageManager)
		writeServiceComment(b,
			"Remote development workspace \u2014 Zerops pulls source from the 'buildFromGit' repo and",
			fmt.Sprintf("uses the 'dev' zeropsSetup, which deploys full source code with %s pre-installed.", runtimeShortName(plan.RuntimeType)),
			"SSH in (or mount via SSHFS in your IDE) and develop interactively:",
			fmt.Sprintf("  %s", devHint),
			fmt.Sprintf("  %s", pkgCmd),
			"Subdomain gives you a public URL to preview without configuring local proxies.")
	}

	fmt.Fprintf(b, "  - hostname: %s\n", hostname)
	fmt.Fprintf(b, "    type: %s\n", target.Type)
	b.WriteString("    zeropsSetup: dev\n")
	writeServiceBuildFromGit(b, plan)
	b.WriteString("    enableSubdomainAccess: true\n")
	writeAutoscaling(b, target, envIndex)
	b.WriteByte('\n')
}

// writeStageService writes a stage service block for env 0-1 with rich comments.
func writeStageService(b *strings.Builder, plan *RecipePlan, target RecipeTarget, envIndex int) {
	hostname := target.Hostname + "stage"
	prodSummary := prodBuildSummary(plan)

	if envIndex == 0 {
		writeServiceComment(b,
			"Staging service \u2014 agent deploys here via 'zcli push' to validate the production build",
			fmt.Sprintf("pipeline before considering the task complete. Uses 'prod' setup: %s.", prodSummary))
	} else {
		writeServiceComment(b,
			"Staging service \u2014 run 'zcli push' from your dev container to validate the production",
			fmt.Sprintf("build before merging to your main branch. 'prod' zeropsSetup: %s.", prodSummary))
	}

	fmt.Fprintf(b, "  - hostname: %s\n", hostname)
	fmt.Fprintf(b, "    type: %s\n", target.Type)
	b.WriteString("    zeropsSetup: prod\n")
	writeServiceBuildFromGit(b, plan)
	writeInlineComment(b, "    enableSubdomainAccess: true",
		"Public HTTPS URL for the agent to verify the deployed app")
	writeAutoscaling(b, target, envIndex)
	b.WriteByte('\n')
}

// writeSingleService writes a service entry for env 2-5 with rich comments.
func writeSingleService(b *strings.Builder, plan *RecipePlan, target RecipeTarget, envIndex int) {
	if isRuntimeService(target.Role) {
		writeRuntimeServiceComment(b, plan, target, envIndex)
	} else {
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
		subdomainComment := subdomainInlineComment(envIndex)
		writeInlineComment(b, "    enableSubdomainAccess: true", subdomainComment)
	}

	if isRuntimeService(target.Role) && envIndex >= 4 {
		b.WriteString("    minContainers: 2\n")
	}

	writeAutoscaling(b, target, envIndex)
	b.WriteByte('\n')
}

// writeRuntimeServiceComment writes the comment block for a runtime service in env 2-5.
func writeRuntimeServiceComment(b *strings.Builder, plan *RecipePlan, _ RecipeTarget, envIndex int) {
	prodSummary := prodBuildSummary(plan)

	switch envIndex {
	case 2:
		writeServiceComment(b,
			"Staging service validates that local changes build and run correctly on Zerops",
			"before pushing to production. Zerops pulls source from 'buildFromGit' and uses",
			fmt.Sprintf("the 'prod' zeropsSetup: %s.", prodSummary),
			"Run 'zcli push' from your local machine to trigger a deploy.")
	case 3:
		writeServiceComment(b,
			"Staging app \u2014 mirrors production configuration to catch integration issues before",
			"they reach live users. Zerops pulls source from the 'buildFromGit' repo and runs",
			fmt.Sprintf("the 'prod' zeropsSetup: %s.", prodSummary),
			"Use 'zcli push' or git integration to trigger deploys from your CI pipeline.")
	case 4:
		writeServiceComment(b,
			"Production app \u2014 Zerops pulls source from the 'buildFromGit' repo and uses the 'prod'",
			fmt.Sprintf("zeropsSetup: %s.", prodSummary),
			"minContainers: 2 ensures at least two containers run at all times \u2014 spreading load",
			"and keeping the service available if one container is replaced during a deploy.",
			"Zerops autoscales vertically within the bounds set below.")
	case 5:
		writeServiceComment(b,
			"Production app with dedicated CPU and HA-grade configuration.",
			"minContainers: 2 guarantees availability during rolling deploys and container replacement.",
			"cpuMode: DEDICATED pins CPU cores to this service \u2014 no sharing with other projects,",
			"which gives consistent response times under sustained load.",
			"Zerops autoscales RAM and CPU within the verticalAutoscaling bounds below.")
	}
}

// writeDataServiceComment writes the comment block for a data service.
func writeDataServiceComment(b *strings.Builder, plan *RecipePlan, target RecipeTarget, envIndex int) {
	dbName := dataServiceTypeName(target)
	devHostname, stageHostname := "", ""
	for _, t := range plan.Targets {
		if isRuntimeService(t.Role) {
			devHostname = t.Hostname + "dev"
			stageHostname = t.Hostname + "stage"
			break
		}
	}

	switch envIndex {
	case 0, 1:
		shared := fmt.Sprintf("shared by both %s and %s", devHostname, stageHostname)
		writeServiceComment(b,
			fmt.Sprintf("%s single-node database for development \u2014 %s.", dbName, shared),
			"NON_HA is appropriate for dev/staging where high-availability isn't required.",
			"Priority 10 ensures the database starts before the app services, preventing",
			"connection errors during container startup.")
	case 2:
		writeServiceComment(b,
			fmt.Sprintf("%s database running in Zerops \u2014 accessible from your local machine via", dbName),
			"'zcli vpn up'. Once connected, use $db_connectionString from your Zerops project",
			"environment variables to connect from localhost.",
			"Priority 10 ensures the database starts before the app service.")
	case 3:
		writeServiceComment(b,
			fmt.Sprintf("%s single-node database for staging. NON_HA is appropriate here \u2014 staging", dbName),
			"data is replaceable and does not need production-grade durability.",
			"Priority 10 starts the database first, preventing app connection errors on deploy.")
	case 4:
		writeServiceComment(b,
			fmt.Sprintf("%s single-node database. Zerops encrypts and backs up data automatically.", dbName),
			"For high-traffic production, consider upgrading to HA mode (see Environment 5)",
			"or configuring your own backup export strategy via the Zerops UI.",
			"Priority 10 starts the database before the app \u2014 prevents connection errors on deploy.")
	case 5:
		writeServiceComment(b,
			fmt.Sprintf("%s HA database \u2014 replicates data across multiple nodes so no single node", dbName),
			"failure causes data loss or downtime. Required for production-grade durability.",
			"Dedicated CPU ensures database operations don't compete with other workloads.",
			"Priority 10 starts the database before app containers.")
	}
}

// writeServiceBuildFromGit writes the buildFromGit URL.
func writeServiceBuildFromGit(b *strings.Builder, plan *RecipePlan) {
	fmt.Fprintf(b, "    buildFromGit: %s%s-app\n", RecipeAppRepoBase, plan.Slug)
}

// writeAutoscaling writes the verticalAutoscaling block.
// Env 0-2: minRam only. Env 3: add minFreeRamGB. Env 4-5: full scaling config.
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
			writeInlineComment(b, "      minFreeRamGB: 0.125",
				"~50% reserve \u2014 headroom for traffic spikes")
		} else {
			b.WriteString("      minRam: 0.25\n")
			b.WriteString("      minFreeRamGB: 0.125\n")
		}

	case envIndex == 5:
		if isRT {
			b.WriteString("      cpuMode: DEDICATED\n")
			b.WriteString("      minRam: 0.5\n")
			writeInlineComment(b, "      minFreeRamGB: 0.25",
				"50% reserve \u2014 headroom for traffic spikes and runtime pressure")
		} else {
			b.WriteString("      cpuMode: DEDICATED\n")
			b.WriteString("      minRam: 1\n")
			b.WriteString("      minFreeRamGB: 0.5\n")
		}
	}
}

// --- Comment helpers ---

// writeServiceComment writes a multi-line YAML comment block at service indent (2 spaces).
// Each line is wrapped to fit within 80 total characters.
func writeServiceComment(b *strings.Builder, lines ...string) {
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

// writeInlineComment writes a YAML line followed by an inline comment.
// If the combined line exceeds 80 chars, the comment goes on a separate line above.
func writeInlineComment(b *strings.Builder, yamlLine, comment string) {
	combined := yamlLine + "  # " + comment
	if len(combined) <= 80 {
		fmt.Fprintf(b, "%s\n", combined)
	} else {
		// Extract indent from yamlLine.
		indent := yamlLine[:len(yamlLine)-len(strings.TrimLeft(yamlLine, " "))]
		fmt.Fprintf(b, "%s# %s\n%s\n", indent, comment, yamlLine)
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

// --- Framework-aware description helpers ---

// devStartHint returns a hint for what to run after SSH-ing into the dev container.
func devStartHint(plan *RecipePlan) string {
	if plan.Research.StartCommand != "" {
		return fmt.Sprintf("'%s'", plan.Research.StartCommand)
	}
	rt := strings.ToLower(plan.RuntimeType)
	if strings.HasPrefix(rt, "php") {
		return "editing files directly \u2014 PHP interprets per-request, no restart needed"
	}
	return "the appropriate start command via SSH"
}

// prodBuildSummary returns a short description of what the prod zeropsSetup produces.
func prodBuildSummary(plan *RecipePlan) string {
	rt := strings.ToLower(plan.RuntimeType)
	switch {
	case strings.HasPrefix(rt, "php"):
		return "install optimized Composer dependencies and deploy production-ready artifacts"
	case strings.HasPrefix(rt, "bun"):
		return "bundle the source into standalone files \u2014 no node_modules at runtime"
	case strings.HasPrefix(rt, "node"):
		return "build and deploy optimized artifacts for minimal runtime footprint"
	case strings.HasPrefix(rt, "python"):
		return "install production dependencies and deploy the application"
	case strings.HasPrefix(rt, "go"):
		return "compile a static binary and deploy just the executable"
	case strings.HasPrefix(rt, "rust"):
		return "compile an optimized binary and deploy just the executable"
	case strings.HasPrefix(rt, "dotnet"), strings.HasPrefix(rt, ".net"):
		return "publish a self-contained release build"
	}
	return "build optimized artifacts and deploy minimal runtime files"
}

// runtimeShortName returns a human-readable short name for a runtime type.
// "php-nginx@8.4" → "PHP", "bun@1.2" → "Bun", "node@20" → "Node.js".
func runtimeShortName(runtimeType string) string {
	rt := strings.ToLower(strings.SplitN(runtimeType, "@", 2)[0])
	names := map[string]string{
		"php-nginx": "PHP", "php-apache": "PHP", "php": "PHP",
		"bun": "Bun", "node": "Node.js", "deno": "Deno",
		"python": "Python", "go": "Go", "rust": "Rust",
		"dotnet": ".NET", "java": "Java", "ruby": "Ruby",
		"elixir": "Elixir",
	}
	if name, ok := names[rt]; ok {
		return name
	}
	return titleCase(rt)
}

// packageAddCmd returns the command to add a dependency for a given package manager.
func packageAddCmd(packageManager string) string {
	switch strings.ToLower(packageManager) {
	case "composer":
		return "'composer require <package>' \u2014 add dependencies"
	case "bun":
		return "'bun add <package>'             \u2014 add dependencies"
	case "npm":
		return "'npm install <package>'         \u2014 add dependencies"
	case "yarn":
		return "'yarn add <package>'            \u2014 add dependencies"
	case "pnpm":
		return "'pnpm add <package>'            \u2014 add dependencies"
	case "pip":
		return "'pip install <package>'         \u2014 add dependencies"
	case "go":
		return "'go get <package>'              \u2014 add dependencies"
	case "cargo":
		return "'cargo add <crate>'             \u2014 add dependencies"
	}
	return "'<package-manager> add <package>' \u2014 add dependencies"
}

// dataServiceTypeName returns a human-readable name for a data service target.
// Extracts the base type name: "postgresql@18" → "PostgreSQL", "mariadb@10.11" → "MariaDB".
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

// subdomainInlineComment returns an inline comment for enableSubdomainAccess per env.
func subdomainInlineComment(envIndex int) string {
	switch envIndex {
	case 2:
		return "Preview deployed app via public HTTPS subdomain"
	case 3:
		return "Public HTTPS URL for QA and stakeholder review"
	case 4:
		return "Zerops subdomain \u2014 map your custom domain in the UI"
	case 5:
		return "Map your custom domain in the Zerops UI"
	}
	return "Public HTTPS URL"
}
