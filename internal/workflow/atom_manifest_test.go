package workflow

import (
	"strings"
	"testing"
)

// TestAtomManifest_CountMatchesBaseline — the manifest declares exactly
// atomCountBaseline atoms. C-3 ships the scaffolding; C-4 creates the
// files; any future addition or deletion must deliberately update the
// manifest AND the constant together.
func TestAtomManifest_CountMatchesBaseline(t *testing.T) {
	t.Parallel()
	got := len(AllAtoms())
	if got != atomCountBaseline {
		t.Errorf("atom count drift: manifest declares %d atoms, atomCountBaseline = %d; either update the baseline constant or fix the manifest",
			got, atomCountBaseline)
	}
}

// TestAtomManifest_AllIDsUnique — Atom.ID is the stable lookup key for the
// stitcher + test harness. Duplicate IDs would make AtomPath / FindAtom
// non-deterministic.
func TestAtomManifest_AllIDsUnique(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, a := range AllAtoms() {
		if seen[a.ID] {
			t.Errorf("duplicate atom ID: %q", a.ID)
		}
		seen[a.ID] = true
	}
}

// TestAtomManifest_AllPathsUnique — no two atoms claim the same file on
// disk. Cross-checked at build-lint time (C-13) against the actual
// filesystem once atoms land in C-4.
func TestAtomManifest_AllPathsUnique(t *testing.T) {
	t.Parallel()
	seen := map[string]string{}
	for _, a := range AllAtoms() {
		if prev, ok := seen[a.Path]; ok {
			t.Errorf("duplicate atom path %q: %q and %q", a.Path, prev, a.ID)
		}
		seen[a.Path] = a.ID
	}
}

// TestAtomManifest_AudienceEnum — every atom's Audience is one of the
// declared constants. Defends against typos that would drop the atom
// from AtomsByAudience lookups.
func TestAtomManifest_AudienceEnum(t *testing.T) {
	t.Parallel()
	valid := map[string]bool{
		AudienceMain:               true,
		AudienceScaffoldSub:        true,
		AudienceFeatureSub:         true,
		AudienceWriterSub:          true,
		AudienceCodeReviewSub:      true,
		AudienceEditorialReviewSub: true,
		AudienceAny:                true,
	}
	for _, a := range AllAtoms() {
		if !valid[a.Audience] {
			t.Errorf("atom %q has unknown Audience %q", a.ID, a.Audience)
		}
	}
}

// TestAtomManifest_TierEnum — every TierCond is one of the declared
// tier constants.
func TestAtomManifest_TierEnum(t *testing.T) {
	t.Parallel()
	valid := map[string]bool{
		TierAny:      true,
		TierShowcase: true,
		TierMinimal:  true,
	}
	for _, a := range AllAtoms() {
		if !valid[a.TierCond] {
			t.Errorf("atom %q has unknown TierCond %q", a.ID, a.TierCond)
		}
	}
}

// TestAtomManifest_MaxLinesWithin300 — principle P6: every atom ≤300
// lines. Manifest caps are the expected upper bounds from atomic-layout
// .md §1; C-13 lint cross-checks against actual file line counts in C-4+.
func TestAtomManifest_MaxLinesWithin300(t *testing.T) {
	t.Parallel()
	for _, a := range AllAtoms() {
		if a.MaxLines <= 0 {
			t.Errorf("atom %q: MaxLines must be positive, got %d", a.ID, a.MaxLines)
		}
		if a.MaxLines > 300 {
			t.Errorf("atom %q: MaxLines=%d exceeds P6 cap of 300", a.ID, a.MaxLines)
		}
	}
}

// TestAtomManifest_PathPrefixMatchesCategory — atoms in phaseAtoms() live
// under phases/; briefAtoms() under briefs/; principleAtoms() under
// principles/. Guards against misplaced declarations that would make
// AtomsForPhase / AtomsForBrief return wrong results.
func TestAtomManifest_PathPrefixMatchesCategory(t *testing.T) {
	t.Parallel()
	for _, a := range phaseAtoms() {
		if !strings.HasPrefix(a.Path, "phases/") {
			t.Errorf("phase atom %q has non-phases/ path: %q", a.ID, a.Path)
		}
	}
	for _, a := range briefAtoms() {
		if !strings.HasPrefix(a.Path, "briefs/") {
			t.Errorf("brief atom %q has non-briefs/ path: %q", a.ID, a.Path)
		}
	}
	for _, a := range principleAtoms() {
		if !strings.HasPrefix(a.Path, "principles/") {
			t.Errorf("principle atom %q has non-principles/ path: %q", a.ID, a.Path)
		}
	}
}

// TestAtomManifest_AtomsForPhase_Research — spot check the per-phase
// helper against a known-small phase.
func TestAtomManifest_AtomsForPhase_Research(t *testing.T) {
	t.Parallel()
	got := AtomsForPhase("research")
	want := []string{
		"research.entry",
		"research.symbol-contract-derivation",
		"research.completion",
	}
	if len(got) != len(want) {
		t.Fatalf("research phase atom count: got %d, want %d (%+v)", len(got), len(want), got)
	}
	for i, wantID := range want {
		if got[i].ID != wantID {
			t.Errorf("research[%d].ID = %q, want %q", i, got[i].ID, wantID)
		}
	}
}

// TestAtomManifest_AtomsForBrief_EditorialReview — the 10 editorial-review
// atoms added per refinement 2026-04-20 must be discoverable via the
// brief helper for both tiers.
func TestAtomManifest_AtomsForBrief_EditorialReview(t *testing.T) {
	t.Parallel()
	for _, tier := range []string{TierShowcase, TierMinimal} {
		got := AtomsForBrief("editorial-review", tier)
		if len(got) != 10 {
			t.Errorf("editorial-review/%s: got %d atoms, want 10", tier, len(got))
		}
		for _, a := range got {
			if a.Audience != AudienceEditorialReviewSub {
				t.Errorf("editorial-review atom %q has wrong audience %q",
					a.ID, a.Audience)
			}
		}
	}
}

// TestAtomManifest_AtomsForBrief_MinimalTierFiltering — a TierShowcase-only
// atom under phases/ does NOT appear in AtomsForBrief; AtomsForBrief is
// for briefs/*, not phases/*. This test also guards against future
// showcase-only brief atoms slipping into a minimal composition.
func TestAtomManifest_AtomsForBrief_MinimalTierFiltering(t *testing.T) {
	t.Parallel()
	// Every brief atom currently declares TierAny, so minimal and showcase
	// should return identical lists per role. This is an invariant the
	// brief-authoring convention upholds: tier branching lives inside an
	// atom's content (stitcher-resolved), not as separate atoms.
	for _, role := range []string{"scaffold", "feature", "writer", "code-review", "editorial-review"} {
		minimal := AtomsForBrief(role, TierMinimal)
		showcase := AtomsForBrief(role, TierShowcase)
		if len(minimal) != len(showcase) {
			t.Errorf("role %q: minimal returns %d atoms, showcase returns %d",
				role, len(minimal), len(showcase))
		}
	}
}

// TestAtomManifest_AtomPath — lookup helper returns registered paths.
func TestAtomManifest_AtomPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id       string
		wantPath string
	}{
		{"research.entry", "phases/research/entry.md"},
		{"briefs.writer.manifest-contract", "briefs/writer/manifest-contract.md"},
		{"principles.symbol-naming-contract", "principles/symbol-naming-contract.md"},
		{"briefs.editorial-review.porter-premise", "briefs/editorial-review/porter-premise.md"},
	}
	for _, tt := range tests {
		got, ok := AtomPath(tt.id)
		if !ok {
			t.Errorf("AtomPath(%q) not found", tt.id)
			continue
		}
		if got != tt.wantPath {
			t.Errorf("AtomPath(%q) = %q, want %q", tt.id, got, tt.wantPath)
		}
	}
	if _, ok := AtomPath("no.such.atom"); ok {
		t.Error("AtomPath should return false for unknown ID")
	}
}

// TestAtomManifest_TierConditionalAtomsAreInPhases — per atomic-layout
// .md §7, tier branching applies to phases/* atoms. Briefs are TierAny
// (tier handled inside content). This test documents that invariant.
func TestAtomManifest_TierConditionalAtomsAreInPhases(t *testing.T) {
	t.Parallel()
	for _, a := range AllAtoms() {
		if a.TierCond == TierAny {
			continue
		}
		if !strings.HasPrefix(a.Path, "phases/") {
			t.Errorf("non-TierAny atom %q under %q — tier-conditional atoms must live under phases/",
				a.ID, a.Path)
		}
	}
}

// TestAtomManifest_EditorialReviewCount — verifies the 10 editorial-review
// atoms declared per research-refinement 2026-04-20 count matches. Any
// C-7.5 addition must update both this count and the atomCountBaseline.
func TestAtomManifest_EditorialReviewCount(t *testing.T) {
	t.Parallel()
	got := AtomsByAudience(AudienceEditorialReviewSub)
	if len(got) != 10 {
		t.Errorf("editorial-review audience atoms: got %d, want 10", len(got))
	}
}
