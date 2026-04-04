package workflow

import (
	"fmt"
	"slices"
	"strings"
)

// Recipe role constants.
const (
	RecipeRoleApp    = "app"
	RecipeRoleWorker = "worker"
)

// RecipeAppRepoBase is the GitHub org where recipe app repos live.
const RecipeAppRepoBase = "https://github.com/zerops-recipe-apps/"

// Environment tier definitions with folder names (em-dash U+2014).
var envTiers = []struct {
	Index  int
	Folder string
	Suffix string
	Label  string
}{
	{0, "0 \u2014 AI Agent", "ai", "AI Agent"},
	{1, "1 \u2014 Remote (CDE)", "remote", "Remote (CDE)"},
	{2, "2 \u2014 Local", "local", "Local"},
	{3, "3 \u2014 Stage", "stage", "Stage"},
	{4, "4 \u2014 Small Production", "prod", "Small Production"},
	{5, "5 \u2014 Highly-available Production", "ha", "Highly-available Production"},
}

// EnvTierCount returns the number of environment tiers.
func EnvTierCount() int { return len(envTiers) }

// EnvFolder returns the folder name for an environment index.
func EnvFolder(envIndex int) string {
	if envIndex < 0 || envIndex >= len(envTiers) {
		return ""
	}
	return envTiers[envIndex].Folder
}

// BuildFinalizeOutput generates all recipe repo files and returns them as a map.
// Keys are relative paths (e.g., "0 — AI Agent/import.yaml").
// Values are file content strings.
func BuildFinalizeOutput(plan *RecipePlan) map[string]string {
	files := make(map[string]string)

	// Main README.
	files["README.md"] = GenerateRecipeREADME(plan)

	// Per-environment files.
	for i := range envTiers {
		folder := envTiers[i].Folder
		files[folder+"/import.yaml"] = GenerateEnvImportYAML(plan, i)
		files[folder+"/README.md"] = GenerateEnvREADME(plan, i)
	}

	return files
}

// GenerateRecipeREADME returns the main recipe README.md content.
func GenerateRecipeREADME(plan *RecipePlan) string {
	var b strings.Builder

	title := titleCase(plan.Framework)
	if plan.Tier == RecipeTierShowcase {
		fmt.Fprintf(&b, "# Zerops x %s — Showcase\n\n", title)
	} else {
		fmt.Fprintf(&b, "# Zerops x %s\n\n", title)
	}

	fmt.Fprintf(&b, "A %s recipe for [Zerops](https://zerops.io) — ", plan.Framework)
	if plan.Tier == RecipeTierShowcase {
		b.WriteString("a full-featured showcase with multiple services.\n\n")
	} else {
		b.WriteString("a minimal hello-world deployment.\n\n")
	}

	b.WriteString("## Environments\n\n")
	b.WriteString("| # | Environment | Use case |\n")
	b.WriteString("|---|-------------|----------|\n")
	for _, env := range envTiers {
		fmt.Fprintf(&b, "| %d | %s | %s |\n", env.Index, env.Label, envUseCase(env.Index))
	}

	b.WriteString("\n## Quick Start\n\n")
	b.WriteString("Import any environment to your Zerops project:\n\n")
	b.WriteString("```yaml\n")
	fmt.Fprintf(&b, "# Choose an environment folder and import its import.yaml\n")
	b.WriteString("```\n\n")
	b.WriteString("See each environment's README for details.\n")

	return b.String()
}

// GenerateEnvREADME returns the README.md for a specific environment tier.
func GenerateEnvREADME(plan *RecipePlan, envIndex int) string {
	if envIndex < 0 || envIndex >= len(envTiers) {
		return ""
	}
	env := envTiers[envIndex]

	var b strings.Builder
	fmt.Fprintf(&b, "# %s — %s\n\n", plan.Slug, env.Label)
	fmt.Fprintf(&b, "Environment %d: %s\n\n", env.Index, envDescription(envIndex))
	fmt.Fprintf(&b, "## Import\n\n")
	fmt.Fprintf(&b, "Import `import.yaml` to your Zerops project.\n\n")
	fmt.Fprintf(&b, "## Services\n\n")

	for _, t := range plan.Targets {
		if TargetInEnv(t, envIndex) {
			fmt.Fprintf(&b, "- **%s** (%s) — %s\n", t.Hostname, t.Type, t.Role)
		}
	}

	return b.String()
}

// GenerateEnvImportYAML returns the import.yaml for a specific env.
func GenerateEnvImportYAML(plan *RecipePlan, envIndex int) string {
	if envIndex < 0 || envIndex >= len(envTiers) {
		return ""
	}

	var b strings.Builder
	projectName := fmt.Sprintf("%s-%s", plan.Slug, envTiers[envIndex].Suffix)

	if plan.Research.NeedsAppSecret {
		b.WriteString("#zeropsPreprocessor=on\n")
	}
	fmt.Fprintf(&b, "project:\n")
	fmt.Fprintf(&b, "  name: %s\n", projectName)

	// corePackage at project level for env 5.
	if envIndex == 5 {
		b.WriteString("  corePackage: SERIOUS\n")
	}

	b.WriteString("services:\n")

	for _, target := range plan.Targets {
		if !TargetInEnv(target, envIndex) {
			continue
		}
		if isRuntimeService(target.Role) && envIndex <= 1 {
			// Env 0-1: generate dev + stage pair for runtime services.
			writeDevServiceBlock(&b, plan, target, envIndex)
			writeStageServiceBlock(&b, plan, target, envIndex)
		} else {
			writeServiceBlock(&b, plan, target, envIndex)
		}
	}

	return b.String()
}

// writeDevServiceBlock writes the dev service entry for env 0-1.
// zeropsSetup: dev maps this hostname to `setup: dev` in zerops.yaml.
func writeDevServiceBlock(b *strings.Builder, plan *RecipePlan, target RecipeTarget, _ int) {
	hostname := target.Hostname + "dev"
	fmt.Fprintf(b, "  - hostname: %s\n", hostname)
	fmt.Fprintf(b, "    type: %s\n", target.Type)
	b.WriteString("    startWithoutCode: true\n")
	b.WriteString("    maxContainers: 1\n")
	b.WriteString("    zeropsSetup: dev\n")
	b.WriteString("    enableSubdomainAccess: true\n")
	writeServiceEnvSecrets(b, plan)
}

// writeStageServiceBlock writes the stage service entry for env 0-1.
// zeropsSetup: prod maps this hostname to `setup: prod` in zerops.yaml.
func writeStageServiceBlock(b *strings.Builder, plan *RecipePlan, target RecipeTarget, _ int) {
	hostname := target.Hostname + "stage"
	fmt.Fprintf(b, "  - hostname: %s\n", hostname)
	fmt.Fprintf(b, "    type: %s\n", target.Type)
	b.WriteString("    zeropsSetup: prod\n")
	writeServiceBuildFromGit(b, plan)
	b.WriteString("    enableSubdomainAccess: true\n")
	writeServiceEnvSecrets(b, plan)
}

// writeServiceBlock writes a single service entry in import.yaml.
func writeServiceBlock(b *strings.Builder, plan *RecipePlan, target RecipeTarget, envIndex int) {
	fmt.Fprintf(b, "  - hostname: %s\n", target.Hostname)
	fmt.Fprintf(b, "    type: %s\n", target.Type)

	// Data services get priority: 10.
	if !isRuntimeService(target.Role) {
		b.WriteString("    priority: 10\n")
	}

	// HA mode for env 5 data services.
	if IsDataService(target.Role) && envIndex == 5 {
		b.WriteString("    mode: HA\n")
	} else if IsDataService(target.Role) {
		b.WriteString("    mode: NON_HA\n")
	}

	// Runtime services: zeropsSetup + buildFromGit.
	// zeropsSetup: prod maps the bare hostname to `setup: prod` in zerops.yaml.
	if isRuntimeService(target.Role) {
		b.WriteString("    zeropsSetup: prod\n")
		writeServiceBuildFromGit(b, plan)
	}

	// Subdomain access for app services.
	if target.Role == RecipeRoleApp {
		b.WriteString("    enableSubdomainAccess: true\n")
	}

	// Vertical autoscaling for env 3-5.
	writeAutoscaling(b, target, envIndex)

	// Min containers for env 4-5.
	if isRuntimeService(target.Role) && envIndex >= 4 {
		b.WriteString("    minContainers: 2\n")
	}

	// Per-service envSecrets.
	if isRuntimeService(target.Role) {
		writeServiceEnvSecrets(b, plan)
	}
}

// writeServiceEnvSecrets writes envSecrets block on a service (not project).
func writeServiceEnvSecrets(b *strings.Builder, plan *RecipePlan) {
	if !plan.Research.NeedsAppSecret {
		return
	}
	b.WriteString("    envSecrets:\n")
	b.WriteString("      APP_KEY: <@generateRandomString(<32>)>\n")
}

// writeServiceBuildFromGit writes the buildFromGit URL.
func writeServiceBuildFromGit(b *strings.Builder, plan *RecipePlan) {
	fmt.Fprintf(b, "    buildFromGit: %s%s-app\n", RecipeAppRepoBase, plan.Slug)
}

// writeAutoscaling writes verticalAutoscaling block for higher environments.
func writeAutoscaling(b *strings.Builder, target RecipeTarget, envIndex int) {
	if envIndex < 3 {
		return
	}

	needsScaling := (envIndex == 3 && (isRuntimeService(target.Role) || IsDataService(target.Role))) || envIndex >= 4
	if !needsScaling {
		return
	}

	b.WriteString("    verticalAutoscaling:\n")

	switch envIndex {
	case 3:
		b.WriteString("      minFreeRamGB: 0.25\n")
	case 4:
		b.WriteString("      minFreeRamGB: 0.125\n")
	case 5:
		b.WriteString("      minFreeRamGB: 0.25\n")
		if isRuntimeService(target.Role) {
			b.WriteString("      cpuMode: DEDICATED\n")
		}
	}
}

// isRuntimeService returns true for app and worker roles.
func isRuntimeService(role string) bool {
	return role == RecipeRoleApp || role == RecipeRoleWorker
}

// TargetInEnv checks if a target is included in a specific environment.
func TargetInEnv(target RecipeTarget, envIndex int) bool {
	return slices.Contains(target.Environments, fmt.Sprintf("%d", envIndex))
}

// IsDataService returns true for non-runtime service roles.
func IsDataService(role string) bool {
	switch role {
	case "db", "cache", "storage", "search", "mail":
		return true
	}
	return false
}

// envUseCase returns a brief use-case description for an environment tier.
func envUseCase(envIndex int) string {
	switch envIndex {
	case 0:
		return "AI-driven development via ZCP"
	case 1:
		return "Cloud development environment"
	case 2:
		return "Local development with Zerops"
	case 3:
		return "Staging and testing"
	case 4:
		return "Small production deployment"
	case 5:
		return "High-availability production"
	}
	return ""
}

// envDescription returns a longer description for an environment tier.
func envDescription(envIndex int) string {
	switch envIndex {
	case 0:
		return "AI Agent environment for ZCP/AI-driven development workflows."
	case 1:
		return "Remote cloud development environment (CDE) for team collaboration."
	case 2:
		return "Local development setup with Zerops services."
	case 3:
		return "Staging environment for pre-production testing."
	case 4:
		return "Small production with minContainers: 2 for redundancy."
	case 5:
		return "Highly-available production with dedicated CPU, HA mode, and SERIOUS core package."
	}
	return ""
}
