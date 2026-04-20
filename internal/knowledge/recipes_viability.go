package knowledge

import (
	"fmt"
	"strings"
)

// RecipeViabilityRules defines the thresholds a recipe must meet to be selected
// as the bootstrap happy path. Stub recipes (e.g. a 6-line frontmatter-only
// file) must fail the gate so route selection falls through to classic
// bootstrap instead of handing the LLM a near-empty knowledge payload.
//
// Thresholds are calibrated so every recipe listed as "audited=yes" in
// docs/spec-recipe-quality-process.md Status passes the gate.
type RecipeViabilityRules struct {
	// MinBodyLines — number of non-frontmatter lines required in the recipe body.
	MinBodyLines int
	// RequiredSections — case-insensitive `## `-prefixed headings that must all
	// appear in the body.
	RequiredSections []string
	// MinCodeFences — minimum number of fenced code blocks (``` fences) in the body.
	MinCodeFences int
}

// DefaultRecipeViabilityRules is the production threshold set.
//
//nolint:gochecknoglobals // intentional default policy
var DefaultRecipeViabilityRules = RecipeViabilityRules{
	MinBodyLines:     200,
	RequiredSections: []string{"overview", "deploy", "verify"},
	MinCodeFences:    1,
}

// ViabilityResult reports pass/fail with the specific reasons when a recipe
// fails. Populated reasons are empty when Passed is true.
type ViabilityResult struct {
	Passed  bool
	Reasons []string
}

// CheckViability applies the rules to a recipe's raw markdown content (body
// including frontmatter — the check strips frontmatter internally).
func CheckViability(content string, rules RecipeViabilityRules) ViabilityResult {
	body := stripFrontmatter(content)
	var reasons []string

	lines := strings.Count(body, "\n") + 1
	if lines < rules.MinBodyLines {
		reasons = append(reasons, fmt.Sprintf("body has %d lines, need %d", lines, rules.MinBodyLines))
	}

	// parseH2Sections honours fenced code blocks — a `## Deploy` inside a
	// YAML example won't be counted as a real section heading.
	sections := parseH2Sections(body)
	lowered := make(map[string]struct{}, len(sections))
	for name := range sections {
		lowered[strings.ToLower(name)] = struct{}{}
	}
	for _, section := range rules.RequiredSections {
		if _, ok := lowered[strings.ToLower(section)]; !ok {
			reasons = append(reasons, fmt.Sprintf("missing required section: ## %s", section))
		}
	}

	// Every opening fence has a matching closing fence, so pairs = total/2.
	fencePairs := strings.Count(body, "\n```") / 2
	if fencePairs < rules.MinCodeFences {
		reasons = append(reasons, fmt.Sprintf("has %d code fences, need %d", fencePairs, rules.MinCodeFences))
	}

	return ViabilityResult{
		Passed:  len(reasons) == 0,
		Reasons: reasons,
	}
}

// stripFrontmatter removes a leading `---\n...\n---\n` block. Returns the
// original content when no frontmatter is present.
func stripFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---\n") {
		return content
	}
	rest := content[4:]
	_, after, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return content
	}
	return after
}
