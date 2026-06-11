package recipe

import (
	"slices"
	"strings"
	"testing"
)

// briefs_codebase_content_golden_voice_test.go — Run-43 Edit C
// substrate pin. Spec anchor: docs/spec-content-surfaces.md
// §"Empirical floor" — the two reference recipes
// `laravel-jetstream` + `laravel-showcase` define the voice floor
// for every surface contract.
//
// Run-42 dogfood ([plans/run-42-validation.md §"Substrate priority 3
// — voice-vs-golden classifier"]) observed:
//
//   - KB across three codebases shipped 13 gotcha bullets, all
//     defensive trap-walking; the jetstream golden ships 2
//     operational bullets (`zsc health-check disable`,
//     `zsc scale ram +0.5GB`).
//   - apidev/zerops.yaml comments contained "the pattern is taught
//     in IG #3" / "see IG #5 for the rationale" meta-prose that the
//     golden recipes never use.
//
// Root cause: the codebase-content sub-agent learned voice only
// from spec TEXT; the golden recipes themselves are not embedded
// (large + frequently iterated). Run-43 Edit C closes the gap with
// a new atom (`golden_voice_principles.md`) that describes the
// golden voice on PRINCIPLE — operational over defensive,
// friendly-authority adaptation pattern, self-contained yaml
// comments, citation pattern — in the brief author's own words,
// citing the golden recipes by name as anchors but NOT quoting
// them verbatim.

// TestGoldenVoicePrinciples_AtomPresent asserts the new atom is
// composed into the codebase-content brief body and carries the
// four section headings + the spec cross-link.
func TestGoldenVoicePrinciples_AtomPresent(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth-showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
		},
	}
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	// The atom path lands in brief.Parts.
	wantPart := "briefs/codebase-content/golden_voice_principles.md"
	if !slices.Contains(brief.Parts, wantPart) {
		t.Errorf("brief.Parts missing %q; got %v", wantPart, brief.Parts)
	}
	// The four section headings — operational voice, friendly-authority,
	// yaml-comments-stand-alone, citation pattern.
	for _, want := range []string{
		"Golden voice principles",
		"## Operational voice over defensive trap-cataloging",
		"## Friendly-authority adaptation pattern",
		"## Yaml comments stand alone",
		"## Citation pattern",
		// Golden recipes named as anchors (not quoted).
		"laravel-jetstream",
		"laravel-showcase",
		// Spec cross-link — phrase straddles a wrap so probe both halves.
		"spec calls this out",
		"Why this",
		"the content-quality failure mode",
		// Cross-link to synthesis_workflow.md's complementary rule.
		"synthesis_workflow.md",
		// Cross-link to refinement-1 derived rule F-XSURF-REF (so the
		// substrate connects at the refinement gate).
		"F-XSURF-REF",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("codebase-content brief missing golden-voice anchor %q", want)
		}
	}
}
