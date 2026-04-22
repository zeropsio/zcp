package workflow

import (
	"fmt"
	"strings"
)

// GenerateAppREADME returns the per-codebase README scaffold placed on the
// SSHFS mount at generate time. The scaffold carries the three fragment
// marker pairs in their canonical trailing-`#` form AND a single HTML-
// comment placeholder between each pair. The writer sub-agent's Edit
// target is the placeholder line — it is NEVER the markers themselves.
// That removes the entire "marker retyped without trailing `#`" failure
// class (v36 F-12, v37 F-12-mutated).
//
// Surrounding structure (title, deploy button + cover image, H2 headings
// outside the markers) stays live so the writer-edited README is a
// complete recipe-page readme once fragments are filled in.
func GenerateAppREADME(plan *RecipePlan) string {
	var b strings.Builder
	title := titleCase(plan.Framework)
	pretty := recipePrettyName(plan.Slug, plan.Framework)
	fw := strings.ToLower(plan.Framework)

	fmt.Fprintf(&b, "# %s %s Recipe App\n\n", title, pretty)

	// Intro fragment: a single placeholder line between the marker pair.
	b.WriteString("<!-- #ZEROPS_EXTRACT_START:intro# -->\n\n")
	b.WriteString(appScaffoldPlaceholder("intro") + "\n\n")
	b.WriteString("<!-- #ZEROPS_EXTRACT_END:intro# -->\n\n")

	// Deploy button and cover image (outside markers, appear in the
	// app repo recipe page as-is — no writer edit required).
	b.WriteString("\u2b07\ufe0f **Full recipe page and deploy with one-click**\n\n")
	fmt.Fprintf(&b, "[![Deploy on Zerops](https://github.com/zeropsio/recipe-shared-assets/blob/main/deploy-button/light/deploy-button.svg)](https://app.zerops.io/recipes/%s?environment=small-production)\n\n", plan.Slug)
	fmt.Fprintf(&b, "![%s cover](https://github.com/zeropsio/recipe-shared-assets/blob/main/covers/svg/cover-%s.svg)\n\n", fw, fw)

	// Integration Guide: H2 outside the marker, single placeholder inside.
	b.WriteString("## Integration Guide\n\n")
	b.WriteString("<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n\n")
	b.WriteString(appScaffoldPlaceholder("integration-guide") + "\n\n")
	b.WriteString("<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n\n")

	// Knowledge Base: H2 outside the marker, single placeholder inside.
	b.WriteString("## Knowledge Base\n\n")
	b.WriteString("<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n\n")
	b.WriteString(appScaffoldPlaceholder("knowledge-base") + "\n\n")
	b.WriteString("<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")

	return b.String()
}

// appScaffoldPlaceholder returns the exact REPLACE-THIS-LINE comment the
// scaffold emits for a given fragment key. isValidAppREADME AND the
// engine-side integration test (TestWriterFlow_NeverRetypesMarkers_
// Integration) both pin this string — if any caller rewrites the
// wording, update those call sites in the same commit.
func appScaffoldPlaceholder(fragment string) string {
	switch fragment {
	case "intro":
		return "<!-- REPLACE THIS LINE with a 1-3 line plain-prose intro naming the runtime + the managed services. No H2/H3 inside the markers. -->"
	case "integration-guide":
		return "<!-- REPLACE THIS LINE with 3-6 H3 items (\"### 1. Adding `zerops.yaml`\", \"### 2. ...\"), each with a fenced code block. -->"
	case "knowledge-base":
		return "<!-- REPLACE THIS LINE with \"### Gotchas\" followed by 3-6 bullets in `**symptom** -- mechanism` form. -->"
	}
	return ""
}
