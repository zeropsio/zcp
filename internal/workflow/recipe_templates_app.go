package workflow

import (
	"fmt"
	"strings"
)

// GenerateAppREADME returns a scaffold README.md for the app repo.
// Contains correct markers, deploy button, cover image, and structural skeleton.
// The agent fills in the integration-guide and knowledge-base content.
func GenerateAppREADME(plan *RecipePlan) string {
	var b strings.Builder
	title := titleCase(plan.Framework)
	pretty := recipePrettyName(plan.Slug, plan.Framework)
	fw := strings.ToLower(plan.Framework)

	// Title.
	fmt.Fprintf(&b, "# %s %s Recipe App\n\n", title, pretty)

	// Intro extract.
	b.WriteString("<!-- #ZEROPS_EXTRACT_START:intro# -->\n\n")
	fmt.Fprintf(&b, "A minimal [%s](%s) application", title, frameworkURL(plan.Framework))
	if plan.Research.DBDriver != "" && plan.Research.DBDriver != recipeDBNone {
		fmt.Fprintf(&b, " with a %s connection,", dbDisplayNamePlain(plan.Research.DBDriver))
	}
	fmt.Fprintf(&b, " demonstrating %s on [Zerops](https://zerops.io) platform.\n", appDemoDescription(plan))
	fmt.Fprintf(&b, "Used within [%s %s recipe](https://app.zerops.io/recipes/%s) for [Zerops](https://zerops.io) platform.\n",
		title, pretty, plan.Slug)
	b.WriteString("<!-- #ZEROPS_EXTRACT_END:intro# -->\n\n")

	// Deploy button and cover.
	b.WriteString("\u2b07\ufe0f **Full recipe page and deploy with one-click**\n\n")
	fmt.Fprintf(&b, "[![Deploy on Zerops](https://github.com/zeropsio/recipe-shared-assets/blob/main/deploy-button/light/deploy-button.svg)](https://app.zerops.io/recipes/%s?environment=small-production)\n\n", plan.Slug)
	fmt.Fprintf(&b, "![%s cover](https://github.com/zeropsio/recipe-shared-assets/blob/main/covers/svg/cover-%s.svg)\n\n", fw, fw)

	// Integration guide section — H2 outside marker, content inside.
	b.WriteString("## Integration Guide\n\n")
	b.WriteString("<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n\n")
	b.WriteString("### 1. Adding `zerops.yaml`\n\n")
	b.WriteString("The main configuration file \u2014 place at repository root. It tells Zerops how to build, deploy and run your app.\n\n")
	b.WriteString("```yaml\nzerops:\n  # TODO: paste the full zerops.yaml content here with comments\n```\n\n")
	b.WriteString("<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n\n")

	// Knowledge base section.
	b.WriteString("<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n\n")
	b.WriteString("### Gotchas\n\n")
	b.WriteString("- **TODO** \u2014 add framework-specific gotchas for running on Zerops\n\n")
	b.WriteString("<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")

	return b.String()
}

// appDemoDescription returns a short phrase describing what the recipe demonstrates.
func appDemoDescription(plan *RecipePlan) string {
	if plan.Research.DBDriver != "" && plan.Research.DBDriver != recipeDBNone {
		return "database connectivity, migrations, and a health check endpoint"
	}
	return "a health check endpoint and static asset serving"
}

// dbDisplayNamePlain returns a plain-text DB name (no markdown links).
func dbDisplayNamePlain(driver string) string {
	switch driver {
	case svcPostgreSQL, "pgsql":
		return "PostgreSQL"
	case "mysql", svcMariaDB:
		return "MariaDB"
	case "mongodb":
		return "MongoDB"
	}
	return driver
}
