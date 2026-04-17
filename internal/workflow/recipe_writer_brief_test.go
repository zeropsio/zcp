package workflow

import (
	"strings"
	"testing"
)

// TestBriefValidationRegistry_CoversKnownContentCheckFamilies asserts that
// every content-check family declared in contentCheckFailSuffixes has a
// corresponding validation command in the writer-brief registry. v8.86
// §3.2: writers learn the check rules upfront as runnable validations
// they execute against their own draft before returning. Parity between
// the gate-side check family and the brief-side validation is the
// load-bearing invariant.
func TestBriefValidationRegistry_CoversKnownContentCheckFamilies(t *testing.T) {
	t.Parallel()
	// Prefix-suffix families surface with many check-name shapes
	// (e.g. "workerdev_content_reality"). The registry is keyed on the
	// stable family identifier — the suffix minus the leading underscore.
	for _, suffix := range contentCheckFailSuffixes {
		family := strings.TrimPrefix(suffix, "_")
		if _, ok := BriefValidationCommands[family]; !ok {
			t.Errorf("BriefValidationCommands missing family %q (suffix %q)", family, suffix)
		}
	}
	// Hostnameless families with stable names must also be covered.
	for name := range contentCheckFailNames {
		if _, ok := BriefValidationCommands[name]; !ok {
			t.Errorf("BriefValidationCommands missing named family %q", name)
		}
	}
}

func TestBriefValidationCommand_ReturnsRunnableShell(t *testing.T) {
	t.Parallel()
	// Every validation command must look like shell: start with an
	// identifier (awk, grep, python, ratio computation, etc.) and contain
	// no placeholder tokens the writer can't resolve.
	for name, cmd := range BriefValidationCommands {
		if strings.TrimSpace(cmd) == "" {
			t.Errorf("empty validation command for %q", name)
			continue
		}
		if strings.Contains(cmd, "TODO") || strings.Contains(cmd, "PLACEHOLDER") {
			t.Errorf("validation command for %q has placeholder: %s", name, cmd)
		}
	}
}

func TestBuildWriterBrief_IncludesValidationSectionForEveryFamily(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Tier: RecipeTierShowcase,
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22", Role: RecipeRoleAPI},
			{Hostname: "app", Type: "static", DevBase: "nodejs@22", Role: RecipeRoleApp},
		},
	}

	input := WriterBriefInput{
		Plan:         plan,
		FactsLogBody: "sample facts body",
		ContractSpec: "contract_spec:\n  http_endpoints: ...\n",
	}
	brief, err := BuildWriterBrief(input)
	if err != nil {
		t.Fatalf("BuildWriterBrief: %v", err)
	}
	if brief == "" {
		t.Fatal("empty brief")
	}

	// Required section markers.
	for _, needle := range []string{
		"INPUT",
		"OUTPUT",
		"VALIDATION",
		"ITERATE UNTIL CLEAN",
		"facts log",
		"contract spec",
	} {
		if !strings.Contains(brief, needle) {
			t.Errorf("brief missing %q", needle)
		}
	}

	// Validation section must list at least every known content-check family.
	for _, suffix := range contentCheckFailSuffixes {
		family := strings.TrimPrefix(suffix, "_")
		if !strings.Contains(brief, family) {
			t.Errorf("brief missing family %q", family)
		}
	}
}

func TestBuildWriterBrief_IncludesFactsLogBody(t *testing.T) {
	t.Parallel()
	input := WriterBriefInput{
		Plan:         &RecipePlan{Tier: RecipeTierShowcase},
		FactsLogBody: "UNIQUE-TOKEN-abc-123",
	}
	brief, err := BuildWriterBrief(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(brief, "UNIQUE-TOKEN-abc-123") {
		t.Errorf("brief must include facts log body verbatim")
	}
}

func TestBuildWriterBrief_SizeBounds(t *testing.T) {
	t.Parallel()
	input := WriterBriefInput{
		Plan: &RecipePlan{
			Tier: RecipeTierShowcase,
			Targets: []RecipeTarget{
				{Hostname: "api", Type: "nodejs@22", Role: RecipeRoleAPI},
				{Hostname: "app", Type: "static", DevBase: "nodejs@22", Role: RecipeRoleApp},
				{Hostname: "worker", Type: "nodejs@22", IsWorker: true},
			},
		},
		FactsLogBody: strings.Repeat("fact-line\n", 40),
		ContractSpec: "contract_spec:\n  http_endpoints: {}\n",
	}
	brief, err := BuildWriterBrief(input)
	if err != nil {
		t.Fatal(err)
	}
	// Plan §3.2 targets ~5-8KB for the brief. We allow up to 25KB per the
	// deploy-readmes substep budget. A brief that blows past 25 KB has
	// regressed — flag it.
	if len(brief) > 25_000 {
		t.Errorf("writer brief too large: %d bytes (budget 25 KB)", len(brief))
	}
	if len(brief) < 500 {
		t.Errorf("writer brief suspiciously small: %d bytes", len(brief))
	}
}
