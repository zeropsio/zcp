package recipe

import (
	"strings"
	"testing"
)

// atoms_derived_rules_kb_defensive_g7_test.go — Run-44 G7 substrate pin.
//
// Run-43 validation §"Substrate priority 3 — refinement-1 operational-
// voice rule": KB across 3 codebases is 9/10 defensive trap-cataloging;
// goldens are operational-leaning. Refinement-1's derived_rules.md has
// KB1-KB6 but no operational-vs-defensive enforcement.
//
// Codex pre-validation: per-bullet rule would over-fire on legitimate
// defensive bullets; preferable as a WHOLE-KB defensive-floor rule.
//
// Fix: add a NEW derived rule `KB-DEFENSIVE-FLOOR` — advisory severity,
// whole-KB scope (not per-bullet). Fires when a codebase's KB has zero
// bullets containing porter-action prose. Markers (drawn from the
// jetstream + showcase goldens):
//   - `zsc <command>` invocations
//   - "Feel free to" / "feel free to" phrasings
//   - "Bump … when …" / "Bump … if …" phrasings
//   - "Replace … once …" / "Configure this to" phrasings
//   - "Rotate via …" phrasings
//
// If a KB has 1+ bullets with any of these markers, the rule passes.
// If ZERO bullets carry porter-action prose, fire advisory; cross-link
// to golden_voice_principles.md.

// TestRefinementBrief_KBDefensiveFloor_DerivedRulePresent — the
// derived_rules.md atom MUST carry a new whole-KB rule named
// KB-DEFENSIVE-FLOOR (or equivalent), advisory severity, with the
// porter-action prose markers listed and the golden_voice_principles.md
// cross-reference.
func TestRefinementBrief_KBDefensiveFloor_DerivedRulePresent(t *testing.T) {
	t.Parallel()
	atom, err := readAtom("briefs/refinement/derived_rules.md")
	if err != nil {
		t.Fatalf("read derived_rules.md: %v", err)
	}
	// Rule name anchor (the substrate-spec id is KB-DEFENSIVE-FLOOR).
	if !strings.Contains(atom, "KB-DEFENSIVE-FLOOR") {
		t.Errorf("derived_rules.md missing new rule KB-DEFENSIVE-FLOOR")
	}
	// Porter-action prose markers — at least these five drawn from the
	// goldens.
	for _, want := range []string{
		"zsc",
		"Feel free to",
		"Bump",
		"Replace",
		"Rotate via",
		// Whole-KB scope (not per-bullet) — explicit.
		"whole-KB",
		// Advisory severity (soft).
		"advisory",
		// Cross-reference to golden_voice_principles.md
		"golden_voice_principles",
	} {
		if !strings.Contains(atom, want) {
			t.Errorf("derived_rules.md missing G7 KB-DEFENSIVE-FLOOR anchor %q", want)
		}
	}
}
