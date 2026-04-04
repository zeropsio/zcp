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
// Matches the format used by zeropsio/recipes (with ZEROPS_EXTRACT markers
// and deploy-with-one-click links for each environment).
func GenerateRecipeREADME(plan *RecipePlan) string {
	var b strings.Builder

	title := titleCase(plan.Framework)
	tier := "Hello World"
	if plan.Tier == RecipeTierShowcase {
		tier = "Showcase"
	}
	fmt.Fprintf(&b, "# %s %s Recipe\n\n", title, tier)

	// Intro with extract markers.
	b.WriteString("<!-- #ZEROPS_EXTRACT_START:intro# -->\n")
	fmt.Fprintf(&b, "A [%s](%s) application", title, frameworkURL(plan.Framework))
	if plan.Research.DBDriver != "" && plan.Research.DBDriver != "none" {
		fmt.Fprintf(&b, " connected to %s,", dbDisplayName(plan.Research.DBDriver))
	}
	fmt.Fprintf(&b, " running on [Zerops](https://zerops.io) with six ready-made environment configurations")
	b.WriteString(" — from AI agent and remote development to stage and highly-available production.\n")
	b.WriteString("<!-- #ZEROPS_EXTRACT_END:intro# -->\n\n")

	// Environment list with deploy links.
	b.WriteString("Offered in examples for the whole development lifecycle")
	b.WriteString(" — from environments for AI agents like [Claude Code](https://www.anthropic.com/claude-code)")
	b.WriteString(" through environments for remote (CDE) or local development")
	b.WriteString(" to stage and productions of all sizes.\n\n")

	for _, env := range envTiers {
		slug := envSlugSuffix(env.Index)
		fmt.Fprintf(&b, "- **%s** [[info]](/%s) — [[deploy with one click]](https://app.zerops.io/recipes/%s?environment=%s)\n",
			env.Label,
			envFolderURLEncoded(env.Folder),
			plan.Slug,
			slug,
		)
	}

	b.WriteString("\n---\n\n")
	fmt.Fprintf(&b, "Need help setting your project up? Join [Zerops Discord community](https://discord.gg/zeropsio).\n")

	return b.String()
}

// GenerateEnvREADME returns the README.md for a specific environment tier.
// Matches the format used by zeropsio/recipes (with ZEROPS_EXTRACT intro marker).
func GenerateEnvREADME(plan *RecipePlan, envIndex int) string {
	if envIndex < 0 || envIndex >= len(envTiers) {
		return ""
	}
	env := envTiers[envIndex]
	title := titleCase(plan.Framework)
	slug := envSlugSuffix(envIndex)

	var b strings.Builder
	fmt.Fprintf(&b, "# %s %s — %s Environment\n\n", title, tierLabel(plan.Tier), env.Label)
	fmt.Fprintf(&b, "This is %s environment for [%s %s (info + deploy)](https://app.zerops.io/recipes/%s?environment=%s) recipe on [Zerops](https://zerops.io).\n\n",
		aOrAn(env.Label), title, tierLabel(plan.Tier), plan.Slug, slug)

	// Environment intro with extract markers.
	b.WriteString("<!-- #ZEROPS_EXTRACT_START:intro# -->\n")
	fmt.Fprintf(&b, "**%s** %s\n", env.Label, envDescription(envIndex))
	b.WriteString("<!-- #ZEROPS_EXTRACT_END:intro# -->\n")

	return b.String()
}

// tierLabel returns a display label for the recipe tier.
func tierLabel(tier string) string {
	if tier == RecipeTierShowcase {
		return "Showcase"
	}
	return "Hello World"
}

// aOrAn returns "an" for vowel-starting words, "a" otherwise.
func aOrAn(s string) string {
	if len(s) == 0 {
		return "a"
	}
	switch s[0] {
	case 'A', 'E', 'I', 'O', 'U', 'a', 'e', 'i', 'o', 'u':
		return "an"
	}
	return "a"
}

// envSlugSuffix returns the URL-safe environment slug for deploy links.
func envSlugSuffix(envIndex int) string {
	switch envIndex {
	case 0:
		return "ai-agent"
	case 1:
		return "remote-cde"
	case 2:
		return "local"
	case 3:
		return "stage"
	case 4:
		return "small-production"
	case 5:
		return "highly-available-production"
	}
	return ""
}

// envFolderURLEncoded returns the URL-encoded folder name for README links.
func envFolderURLEncoded(folder string) string {
	// Replace spaces and em-dash for URL encoding.
	r := strings.NewReplacer(" ", "%20", "\u2014", "%E2%80%94", "(", "(", ")", ")")
	return r.Replace(folder)
}

// frameworkURL returns a reasonable URL for a framework name.
func frameworkURL(framework string) string {
	urls := map[string]string{
		"laravel": "https://laravel.com",
		"django":  "https://djangoproject.com",
		"rails":   "https://rubyonrails.org",
		"nextjs":  "https://nextjs.org",
		"nuxt":    "https://nuxt.com",
		"bun":     "https://bun.sh",
		"express": "https://expressjs.com",
		"flask":   "https://flask.palletsprojects.com",
		"fastapi": "https://fastapi.tiangolo.com",
		"spring":  "https://spring.io",
		"phoenix": "https://phoenixframework.org",
		"gin":     "https://gin-gonic.com",
		"fiber":   "https://gofiber.io",
		"svelte":  "https://svelte.dev",
		"react":   "https://react.dev",
		"vue":     "https://vuejs.org",
		"angular": "https://angular.dev",
	}
	if u, ok := urls[strings.ToLower(framework)]; ok {
		return u
	}
	return "https://zerops.io"
}

// dbDisplayName returns a display name for a DB driver.
func dbDisplayName(driver string) string {
	switch driver {
	case "postgresql", "pgsql":
		return "[PostgreSQL](https://www.postgresql.org/)"
	case "mysql", "mariadb":
		return "[MariaDB](https://mariadb.org/)"
	case "mongodb":
		return "[MongoDB](https://www.mongodb.com/)"
	}
	return driver
}

// GenerateEnvImportYAML returns the import.yaml for a specific env.
func GenerateEnvImportYAML(plan *RecipePlan, envIndex int) string {
	if envIndex < 0 || envIndex >= len(envTiers) {
		return ""
	}

	var b strings.Builder
	projectName := fmt.Sprintf("%s-%s", plan.Slug, envTiers[envIndex].Suffix)

	if plan.Research.NeedsAppSecret {
		b.WriteString("#zeropsPreprocessor=on\n\n")
	}
	fmt.Fprintf(&b, "project:\n")
	fmt.Fprintf(&b, "  name: %s\n", projectName)

	// corePackage at project level for env 5.
	if envIndex == 5 {
		b.WriteString("  corePackage: SERIOUS\n")
	}

	// Project-level envVariables for shared secrets (all services inherit).
	if plan.Research.NeedsAppSecret && plan.Research.AppSecretKey != "" {
		b.WriteString("  envVariables:\n")
		fmt.Fprintf(&b, "    %s: <@generateRandomString(<32>)>\n", plan.Research.AppSecretKey)
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

// writeDevServiceBlock writes the dev service entry for env 0-1 recipe deliverable.
// Both dev and stage use buildFromGit — Zerops pulls from the recipe repo on import.
func writeDevServiceBlock(b *strings.Builder, plan *RecipePlan, target RecipeTarget, _ int) {
	hostname := target.Hostname + "dev"
	fmt.Fprintf(b, "  - hostname: %s\n", hostname)
	fmt.Fprintf(b, "    type: %s\n", target.Type)
	b.WriteString("    zeropsSetup: dev\n")
	writeServiceBuildFromGit(b, plan)
	b.WriteString("    enableSubdomainAccess: true\n")
}

// writeStageServiceBlock writes the stage service entry for env 0-1 recipe deliverable.
func writeStageServiceBlock(b *strings.Builder, plan *RecipePlan, target RecipeTarget, _ int) {
	hostname := target.Hostname + "stage"
	fmt.Fprintf(b, "  - hostname: %s\n", hostname)
	fmt.Fprintf(b, "    type: %s\n", target.Type)
	b.WriteString("    zeropsSetup: prod\n")
	writeServiceBuildFromGit(b, plan)
	b.WriteString("    enableSubdomainAccess: true\n")
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
