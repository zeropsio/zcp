package workflow

import (
	"fmt"
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
// IntroLabel is sentence-cased with acronyms preserved (used in extract bold text).
var envTiers = []struct {
	Index      int
	Folder     string
	Suffix     string
	Label      string
	IntroLabel string
}{
	{0, "0 \u2014 AI Agent", "agent", "AI Agent", "AI agent"},
	{1, "1 \u2014 Remote (CDE)", "remote", "Remote (CDE)", "Remote (CDE)"},
	{2, "2 \u2014 Local", "local", "Local", "Local"},
	{3, "3 \u2014 Stage", "stage", "Stage", "Stage"},
	{4, "4 \u2014 Small Production", "small-prod", "Small Production", "Small production"},
	{5, "5 \u2014 Highly-available Production", "ha-prod", "Highly-available Production", "Highly-available production"},
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

	// Main recipe README (for zeropsio/recipes).
	files["README.md"] = GenerateRecipeREADME(plan)

	// Per-environment files (for zeropsio/recipes).
	for i := range envTiers {
		folder := envTiers[i].Folder
		files[folder+"/import.yaml"] = GenerateEnvImportYAML(plan, i)
		files[folder+"/README.md"] = GenerateEnvREADME(plan, i)
	}

	// App README scaffold (correct markers, deploy button, cover).
	// Agent fills in integration-guide and knowledge-base content.
	files["appdev/README.md"] = GenerateAppREADME(plan)

	return files
}

// GenerateRecipeREADME returns the main recipe README.md content.
// Matches the format used by zeropsio/recipes (with ZEROPS_EXTRACT markers,
// deploy button, cover image, and deploy-with-one-click links for each environment).
func GenerateRecipeREADME(plan *RecipePlan) string {
	var b strings.Builder

	title := titleCase(plan.Framework)
	pretty := recipePrettyName(plan.Slug, plan.Framework)
	fmt.Fprintf(&b, "# %s %s Recipe\n\n", title, pretty)

	// Intro with extract markers.
	b.WriteString("<!-- #ZEROPS_EXTRACT_START:intro# -->\n")
	fmt.Fprintf(&b, "A [%s](%s) application", title, frameworkURL(plan.Framework))
	if plan.Research.DBDriver != "" && plan.Research.DBDriver != recipeDBNone {
		fmt.Fprintf(&b, " connected to %s,", dbDisplayName(plan.Research.DBDriver))
	}
	b.WriteString(" running on [Zerops](https://zerops.io) with six ready-made environment configurations")
	b.WriteString(" \u2014 from AI agent and remote development to stage and highly-available production.\n")
	b.WriteString("<!-- #ZEROPS_EXTRACT_END:intro# -->\n\n")

	// Deploy button and cover image.
	b.WriteString("\u2b07\ufe0f **Full recipe page and deploy with one-click**\n\n")
	fmt.Fprintf(&b, "[![Deploy on Zerops](https://github.com/zeropsio/recipe-shared-assets/blob/main/deploy-button/light/deploy-button.svg)](https://app.zerops.io/recipes/%s?environment=small-production)\n\n", plan.Slug)
	fw := strings.ToLower(plan.Framework)
	fmt.Fprintf(&b, "![%s](https://github.com/zeropsio/recipe-shared-assets/blob/main/covers/svg/cover-%s.svg)\n\n", fw, fw)

	// Environment list with deploy links.
	b.WriteString("Offered in examples for the whole development lifecycle")
	b.WriteString(" \u2014 from environments for AI agents like [Claude Code](https://www.anthropic.com/claude-code)")
	b.WriteString(" or [opencode](https://opencode.ai)")
	b.WriteString(" through environments for remote (CDE) or local development")
	b.WriteString(" of each developer to stage and productions of all sizes.\n\n")

	for _, env := range envTiers {
		slug := envSlugSuffix(env.Index)
		fmt.Fprintf(&b, "- **%s** [[info]](/%s) \u2014 [[deploy with one click]](https://app.zerops.io/recipes/%s?environment=%s)\n",
			env.Label,
			envFolderURLEncoded(env.Folder),
			plan.Slug,
			slug,
		)
	}

	b.WriteString("\n---\n\n")
	fmt.Fprintf(&b, "For more advanced examples see all [%s recipes](https://app.zerops.io/recipes?lf=%s) on Zerops.\n\n", title, fw)
	b.WriteString("Need help setting your project up? Join [Zerops Discord community](https://discord.gg/zeropsio).\n")

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
	pretty := recipePrettyName(plan.Slug, plan.Framework)
	slug := envSlugSuffix(envIndex)

	var b strings.Builder
	fmt.Fprintf(&b, "# %s %s \u2014 %s Environment\n\n", title, pretty, env.Label)
	fmt.Fprintf(&b, "This is %s %s environment for [%s %s (info + deploy)](https://app.zerops.io/recipes/%s?environment=%s) recipe on [Zerops](https://zerops.io).\n\n",
		aOrAn(env.Label), strings.ToLower(env.Label), title, pretty, plan.Slug, slug)

	// Environment intro with extract markers.
	b.WriteString("<!-- #ZEROPS_EXTRACT_START:intro# -->\n")
	fmt.Fprintf(&b, "**%s** %s\n", env.IntroLabel, envDescription(plan, envIndex))
	b.WriteString("<!-- #ZEROPS_EXTRACT_END:intro# -->\n")

	return b.String()
}

// recipePrettyName derives a display name from the slug by stripping the framework prefix.
// "laravel-minimal" → "Minimal", "bun-hello-world" → "Hello World", "django-showcase" → "Showcase".
func recipePrettyName(slug, framework string) string {
	prefix := strings.ToLower(framework) + "-"
	name := strings.TrimPrefix(slug, prefix)
	words := strings.Split(name, "-")
	for i, w := range words {
		if w != "" {
			words[i] = titleCase(w)
		}
	}
	return strings.Join(words, " ")
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

// Import YAML generation is in recipe_templates_import.go.

// isRuntimeService returns true for app and worker roles.
func isRuntimeService(role string) bool {
	return role == RecipeRoleApp || role == RecipeRoleWorker
}

// IsDataService returns true for non-runtime service roles.
func IsDataService(role string) bool {
	switch role {
	case "db", "cache", "storage", "search", "mail":
		return true
	}
	return false
}

// envDescription returns a description for an environment tier, dynamically including
// the services present in the plan. Matches the style used by zeropsio/recipes.
func envDescription(plan *RecipePlan, envIndex int) string {
	switch envIndex {
	case 0:
		desc := "environment provides a development space for AI agents to build and version the app."
		if svc := buildServiceIncludesList(plan, envIndex); svc != "" {
			desc += "\n" + svc
		}
		return desc
	case 1:
		desc := "environment allows developers to build the app **within Zerops** via SSH, supporting the full development lifecycle without local tool installation."
		if svc := buildServiceIncludesList(plan, envIndex); svc != "" {
			desc += "\n" + svc
		}
		return desc
	case 2:
		return "environment supports local app development using zCLI VPN for database access, while ensuring valid deployment processes using a staged app in Zerops."
	case 3:
		return "environment uses the same configuration as production, but runs on a single container with lower scaling settings."
	case 4:
		return "environment offers a production-ready setup optimized for moderate throughput."
	case 5:
		return "environment provides a production setup with enhanced scaling, dedicated resources, and HA components for improved durability and performance."
	}
	return ""
}

// buildServiceIncludesList returns "It includes a dev service..., a staging service, and a database."
// based on targets in the plan. All targets appear in all environments.
func buildServiceIncludesList(plan *RecipePlan, envIndex int) string {
	var parts []string
	runtimeSeen := false

	for _, target := range plan.Targets {
		if isRuntimeService(target.Role) {
			if !runtimeSeen {
				runtimeSeen = true
				if envIndex <= 1 {
					parts = append(parts,
						"a dev service with the code repository and necessary development tools",
						"a staging service",
					)
				}
			}
		} else {
			parts = append(parts, dataServiceLabel(target.Role))
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return "It includes " + naturalJoin(parts) + "."
}

// dataServiceLabel returns a human-readable label for a data service role.
func dataServiceLabel(role string) string {
	switch role {
	case "db":
		return "a low-resource database"
	case "cache":
		return "a cache store"
	case "storage":
		return "an object storage"
	case "search":
		return "a search engine"
	case "mail":
		return "a mail service"
	}
	return "a " + role + " service"
}

// naturalJoin joins parts with commas and "and" before the last element.
// ["a", "b", "c"] → "a, b, and c"; ["a", "b"] → "a and b"; ["a"] → "a".
func naturalJoin(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
	}
}
