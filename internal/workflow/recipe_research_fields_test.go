package workflow

import (
	"strings"
	"testing"
)

// TestResearchMinimal_NamesTopLevelFieldsExplicitly — v8.99 doc fix.
// The research-minimal section must explicitly name every top-level
// recipePlan field so the agent cannot guess invented names like
// "recipeType" (observed in a real run: the agent read the "Type" column
// header in the recipe-kind table, conflated it with a field name, and
// submitted recipePlan with "recipeType": "showcase" — rejected by the
// schema's additionalProperties:false).
//
// This test locks the fix in: every top-level field must appear as a
// literal substring in research-minimal.
func TestResearchMinimal_NamesTopLevelFieldsExplicitly(t *testing.T) {
	t.Parallel()

	body := sectionContent(t, "research-minimal")

	// Every top-level recipePlan field name. Keep in sync with RecipePlan
	// in recipe.go — if a new field is added there, add it here too.
	fields := []string{
		"framework",
		"tier",
		"slug",
		"runtimeType",
		"buildBases",
		"decisions",
		"research",
		"targets",
		"features",
	}
	for _, f := range fields {
		// Accept both bare and backticked forms.
		if !strings.Contains(body, f) {
			t.Errorf("research-minimal must name top-level field %q literally (v8.99 doc-fix bar); otherwise the agent invents names like \"recipeType\"", f)
		}
	}
}

// TestResearchMinimal_TierValuesNamedExplicitly — v8.99 doc fix.
// The three valid `tier` values must appear literally in research-minimal
// so the agent submits the right string. Missing any of the three was the
// ambiguity that let the agent guess.
func TestResearchMinimal_TierValuesNamedExplicitly(t *testing.T) {
	t.Parallel()

	body := sectionContent(t, "research-minimal")
	for _, v := range []string{`"hello-world"`, `"minimal"`, `"showcase"`} {
		if !strings.Contains(body, v) {
			t.Errorf("research-minimal must name tier value %s literally", v)
		}
	}
}

// TestResearchMinimal_RejectsInventedFieldNames — v8.99 doc fix.
// research-minimal MUST NOT use the string "recipeType" anywhere — neither
// as a field name, column header, or prose term. Reintroducing it lets
// the agent re-invent the submission error.
func TestResearchMinimal_RejectsInventedFieldNames(t *testing.T) {
	t.Parallel()

	body := sectionContent(t, "research-minimal")
	for _, banned := range []string{"recipeType", "recipe_type"} {
		if strings.Contains(body, banned) {
			t.Errorf("research-minimal must NOT contain invented field name %q — it was the source of the v8.98-era submission rejection (agent read it from doc prose and submitted it verbatim)", banned)
		}
	}
}
