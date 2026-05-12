package recipe

import (
	"strings"
	"testing"
)

// briefs_refinement2_findings_classification_g5_test.go — Run-44 G5
// substrate pin.
//
// Run-43 validation §"F1 classification gap at main-agent path":
// the main agent had 3 wasted `record-fragment` calls because the
// classification field was missing on KB ACTs (engine rejected with
// "classification is required for fragments on surface CODEBASE_KB").
// F1 substrate added classification prose in the same brief, but the
// main agent reads the JSON findings, not the prose. So the
// classification gap survives at the path that matters.
//
// Fix: add `classification` as a required field on findings whose
// `suggestedAction` is `reword-conditional` / `rewrite-as-symptom`
// (any action that turns into a `record-fragment mode=replace` on
// KB/IG surfaces). The seven valid values mirror the spec's
// fact-classification taxonomy.

// TestRefinement2PhaseEntry_FindingsSchemaCarriesClassification —
// the findings JSON schema in phase_entry.md MUST include the
// classification field alongside defectClass / severity / etc.,
// and MUST enumerate the seven valid values.
func TestRefinement2PhaseEntry_FindingsSchemaCarriesClassification(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}

	// Locate the JSON schema block (it lives between the "What you
	// produce" header and the closing ``` fence).
	schemaStart := strings.Index(brief.Body, "\"findings\": [")
	if schemaStart < 0 {
		t.Fatal("findings JSON schema block missing from phase_entry.md")
	}
	schemaEnd := strings.Index(brief.Body[schemaStart:], "]\n}")
	if schemaEnd < 0 {
		t.Fatal("findings JSON schema block not terminated")
	}
	schema := brief.Body[schemaStart : schemaStart+schemaEnd]

	// G5 — classification field MUST be present in the schema example.
	if !strings.Contains(schema, `"classification":`) {
		t.Errorf("findings schema missing `\"classification\":` field; schema:\n%s", schema)
	}

	// All seven enum values MUST appear in the schema (the field's
	// enum alternation lists them).
	for _, v := range []string{
		`"platform-invariant"`,
		`"intersection"`,
		`"framework-quirk"`,
		`"library-metadata"`,
		`"scaffold-decision"`,
		`"operational"`,
		`"self-inflicted"`,
	} {
		if !strings.Contains(schema, v) {
			t.Errorf("findings schema missing classification enum value %s", v)
		}
	}
}

// TestRefinement2PhaseEntry_ClassificationFieldGuidanceProse — the
// phase_entry.md prose around the schema MUST explicitly call out that
// classification is required when `suggestedAction` turns into a
// record-fragment replace on KB/IG surfaces, so the audit-LLM emits
// it on the load-bearing finding shapes.
func TestRefinement2PhaseEntry_ClassificationFieldGuidanceProse(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	// The phase_entry.md must explicitly anchor the per-finding
	// classification requirement so the audit-LLM knows when to emit it.
	for _, want := range []string{
		// Required on these actions — record-fragment mode=replace on KB/IG.
		"rewrite-as-symptom",
		"reword-conditional",
		// The required-when-acting-on-S4/S5 framing.
		"CODEBASE_KB",
		"CODEBASE_IG",
		// `drop` action does not require classification (the replacement removes the bullet).
		"drop",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("phase_entry.md missing classification guidance anchor %q", want)
		}
	}
}
