package workflow

// Phase 4 (atom-corpus-verification plan): commit-time coverage gate.
//
// Every atom in the corpus MUST EITHER appear in at least one canonical
// scenario's golden expected atom-IDs OR carry a non-empty
// `coverageExempt:` frontmatter rationale. Atoms that are silently
// uncovered (no golden, no exemption) drift into a state where their
// prose can't be regression-checked by the goldens approach — Phase 4
// closes that gap with a hard test gate.
//
// Companion to TestCorpusCoverage_PinDensity (corpus_pin_density_test.go):
// pin-density verifies atom IDs appear as args to requireAtomIDsContain
// or requireAtomIDsExact in scenarios_test.go (different surface — selection
// reachability via the AST-parsed haystack). Coverage gate verifies
// scenario-fixture coverage (the goldens). Both stay; cross-reference
// in the file headers.

import (
	"testing"
)

// TestCoverageGate is the coverage gate. Every loaded atom must
// either appear in a canonical scenario's expected atom IDs or carry
// a non-empty `coverageExempt:` frontmatter entry. Otherwise the
// atom's prose is dark to the goldens-driven verification approach.
//
// Heuristic for exemption (per plan §4.7): if the atom's typical
// render-occasion appears in <1% of agent sessions, exemption is
// appropriate. Otherwise, add a scenario. Each `coverageExempt:`
// entry MUST have a one-line rationale referencing this heuristic.
// The reviewer treats every exemption as a code-review red flag.
func TestCoverageGate(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	scenarios := canonicalGoldenScenarios()
	if len(scenarios) == 0 {
		t.Fatal("no canonical scenarios — fixtures missing; coverage gate cannot run")
	}

	// Build the union of expected atom IDs across every scenario by
	// running Synthesize against each fixture. Atoms appearing in at
	// least one scenario's render are covered.
	covered := make(map[string]bool, len(corpus))
	for _, scn := range scenarios {
		ids := atomIDsForScenario(t, scn.envelope, corpus)
		for _, id := range ids {
			covered[id] = true
		}
	}

	for _, atom := range corpus {
		if covered[atom.ID] {
			if atom.CoverageExempt != "" {
				// Atom is BOTH covered by a scenario AND carries
				// coverageExempt — drop the exemption.
				t.Errorf("atom %q is covered by ≥1 scenario AND carries coverageExempt %q — drop the exemption (covered + exempt is contradictory)",
					atom.ID, atom.CoverageExempt)
			}
			continue
		}
		if atom.CoverageExempt == "" {
			t.Errorf("atom %q is uncovered (no scenario fires it) AND carries no coverageExempt: rationale — either add a scenario that fires it OR add `coverageExempt: <one-line rationale>` to the frontmatter (reviewer demands strong justification per Phase 4 heuristic: <1%% session frequency)",
				atom.ID)
		}
	}
}
