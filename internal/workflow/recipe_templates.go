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

// GenerateEnvImportYAML returns the import.yaml skeleton for a specific env.
// Returns YAML with service structure — LLM adds comments.
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

	if plan.Research.NeedsAppSecret {
		b.WriteString("  envSecrets:\n")
		fmt.Fprintf(&b, "    APP_KEY: <@generateRandomString(32)>\n")
	}

	b.WriteString("services:\n")

	for _, target := range plan.Targets {
		if !TargetInEnv(target, envIndex) {
			continue
		}
		writeServiceBlock(&b, plan, target, envIndex)
	}

	return b.String()
}

// writeServiceBlock writes a single service entry in import.yaml.
func writeServiceBlock(b *strings.Builder, plan *RecipePlan, target RecipeTarget, envIndex int) {
	fmt.Fprintf(b, "  - hostname: %s\n", serviceHostname(target, envIndex))
	fmt.Fprintf(b, "    type: %s\n", target.Type)

	// Data services get priority: 10.
	if target.Role != RecipeRoleApp && target.Role != RecipeRoleWorker {
		b.WriteString("    priority: 10\n")
	}

	// HA mode for env 5.
	if IsDataService(target.Role) && envIndex == 5 {
		b.WriteString("    mode: HA\n")
	} else if IsDataService(target.Role) {
		b.WriteString("    mode: NON_HA\n")
	}

	// Subdomain access for app services.
	if target.Role == RecipeRoleApp {
		b.WriteString("    enableSubdomainAccess: true\n")
	}

	// Vertical autoscaling for env 3-5.
	writeAutoscaling(b, target, envIndex)

	// Min containers for env 4-5.
	if (target.Role == RecipeRoleApp || target.Role == RecipeRoleWorker) && envIndex >= 4 {
		b.WriteString("    minContainers: 2\n")
	}

	// Dev+prod setups for env 0-1, prod only for 2-5.
	if target.Role == RecipeRoleApp && envIndex <= 1 {
		fmt.Fprintf(b, "    buildFromGit: %s\n", plan.Slug)
	}
}

// writeAutoscaling writes verticalAutoscaling block for higher environments.
func writeAutoscaling(b *strings.Builder, target RecipeTarget, envIndex int) {
	if envIndex < 3 {
		return
	}

	needsScaling := (envIndex == 3 && (target.Role == RecipeRoleApp || target.Role == RecipeRoleWorker || IsDataService(target.Role))) || envIndex >= 4

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
		b.WriteString("      cpuMode: DEDICATED\n")
		if target.Role == RecipeRoleApp || target.Role == RecipeRoleWorker {
			b.WriteString("      corePackage: SERIOUS\n")
		}
	}
}

// serviceHostname returns the hostname for a target in a specific environment.
// Env 0-1 use appdev/appstage pattern for apps; others use bare hostname.
func serviceHostname(target RecipeTarget, envIndex int) string {
	if target.Role == RecipeRoleApp && envIndex <= 1 {
		return target.Hostname + "dev"
	}
	return target.Hostname
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
