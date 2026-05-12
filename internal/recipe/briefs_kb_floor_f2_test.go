package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// briefs_kb_floor_f2_test.go — Run-43 F2 substrate pin.
//
// Three-way consensus (codex + opus + user direction): the spec's
// "5-8 bullets per codebase" KB floor is empirically wrong against
// both reference recipes. Codex counted:
//   - /Users/fxck/www/laravel-jetstream-app/README.md:253,272 — 2 KB bullets.
//   - /Users/fxck/www/laravel-showcase-app/README.md:345-349 — 7 KB bullets.
//
// Spec §S5 says "5-8 bullets per codebase, hard cap 8" — jetstream
// (2) is below the floor, showcase (7) is at the upper end. The floor
// is invented; the empirical floor is "what the goldens ship", which
// is "no floor — bullets stand on their own merit, not on count".
//
// This test pins the floor removal across three substrate sources of
// truth:
//   1. docs/spec-content-surfaces.md (the spec itself)
//   2. internal/recipe/content/briefs/refinement2/audit_checklist.md
//      (the `kb-below-floor` defect class is removed)
//   3. internal/recipe/content/briefs/refinement2/phase_entry.md
//      (defectClass enum drops `kb-below-floor`)
//   4. internal/recipe/content/briefs/codebase-content/golden_voice_principles.md
//      (the "2-5 bullets" voice principle replaced with the 2-7 span)

// TestAuditChecklist_KBFloorRemoved — `kb-below-floor` no longer
// appears as a defect class header, in the walk-every-class summary
// footer, or in phase_entry.md's defectClass JSON enum. `kb-over-cap`
// remains (cap-violation still fires; only the floor side goes).
func TestAuditChecklist_KBFloorRemoved(t *testing.T) {
	t.Parallel()
	audit, err := readAtom("briefs/refinement2/audit_checklist.md")
	if err != nil {
		t.Fatalf("read audit_checklist.md: %v", err)
	}
	if strings.Contains(audit, "kb-below-floor") {
		t.Errorf("audit_checklist.md still names `kb-below-floor` — F2 floor removal incomplete")
	}
	// `kb-over-cap` remains.
	if !strings.Contains(audit, "kb-over-cap") {
		t.Errorf("audit_checklist.md must keep `kb-over-cap` defect class")
	}
	phase, err := readAtom("briefs/refinement2/phase_entry.md")
	if err != nil {
		t.Fatalf("read phase_entry.md: %v", err)
	}
	if strings.Contains(phase, "kb-below-floor") {
		t.Errorf("phase_entry.md defectClass enum still names `kb-below-floor` — F2 floor removal incomplete")
	}
	if !strings.Contains(phase, "kb-over-cap") {
		t.Errorf("phase_entry.md must keep `kb-over-cap` in defectClass enum")
	}
}

// TestSpecAlignment_KBNoFloor — docs/spec-content-surfaces.md no
// longer demands a KB floor. The per-surface line-budget table row
// for KB drops "5-8 bullets per codebase" in favor of "no floor;
// cap 8". §"Surface 5 — Length" rewrites to "Cap 8 bullets per
// codebase. No floor — bullets stand on their own merit ...".
func TestSpecAlignment_KBNoFloor(t *testing.T) {
	t.Parallel()
	// Locate the spec file. The spec lives at docs/spec-content-
	// surfaces.md relative to the repository root; tests run from
	// internal/recipe so we walk up two directories.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	specPath := filepath.Join(wd, "..", "..", "docs", "spec-content-surfaces.md")
	body, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	spec := string(body)
	if strings.Contains(spec, "5–8 bullets per codebase") {
		t.Errorf("spec still demands 5-8 bullet floor on KB — F2 incomplete")
	}
	if strings.Contains(spec, "5-8 bullets per codebase") {
		t.Errorf("spec still demands 5-8 bullet floor on KB (ASCII dash variant) — F2 incomplete")
	}
	// Positive shape — spec now states "no floor" with the
	// editorial-test gating language.
	for _, want := range []string{
		"no floor",
		"editorial test",
	} {
		if !strings.Contains(spec, want) {
			t.Errorf("spec missing F2 anchor %q", want)
		}
	}
}

// TestGoldenVoicePrinciples_NoLowerBoundOnKB — golden_voice_principles.md
// does NOT specify "2-5 bullets" (the prior framing was wrong against
// showcase's 7); carries instead the "2 to 7" span framing OR
// equivalent "shape-by-content, not target-by-floor" language.
func TestGoldenVoicePrinciples_NoLowerBoundOnKB(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/golden_voice_principles.md")
	if err != nil {
		t.Fatalf("read golden_voice_principles.md: %v", err)
	}
	if strings.Contains(body, "2-5 bullets") {
		t.Errorf("golden_voice_principles.md still says '2-5 bullets' — F2 floor removal incomplete")
	}
	for _, want := range []string{
		// 2-to-7 span framing — actual golden counts.
		"jetstream",
		"showcase",
		// The framing that replaces the floor: shape-by-content.
		"shape-by-content",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("golden_voice_principles.md missing F2 framing anchor %q", want)
		}
	}
}
