package recipe

import (
	"strings"
	"testing"
)

// atoms_derived_rules_run43_test.go — Run-43 P3 + P5 substrate pins.
//
// Refinement-1's derived_rules.md gains two factuality guards that
// run-42 dogfood evidence proved necessary:
//
//   P3 — F-EXECONCE-SEMANTICS — `zsc execOnce` key shape vs claimed
//        lifetime semantics. apidev/zerops.yaml:41-51 claimed
//        once-only semantics on a `${appVersionId}-seed` line whose
//        key changes every deploy → seed re-fires every deploy
//        regardless of the claimed ledger semantics. Reference:
//        `principles/init-commands-model.md:7,:10`.
//
//   P5 — F-XSURF-REF — yaml comments cross-referencing IG / KB /
//        prior tier ("see IG #N", "live below at the field site",
//        "Same as tier 0", "see tier <N>"). Spec §"Surface 7"
//        voice is mechanism+reason in one breath; §"Surface 3"
//        forbids cross-tier shifts in service-block comments.

// TestDerivedRules_P3_ExecOnceSemanticsRulePresent pins the
// F-EXECONCE-SEMANTICS factuality guard. Run-42 dogfood proved the
// rule is necessary; refinement-1 had no derived rule that fired
// on the apidev/zerops.yaml execOnce prose mismatch.
func TestDerivedRules_P3_ExecOnceSemanticsRulePresent(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/refinement/derived_rules.md")
	if err != nil {
		t.Fatalf("read derived_rules.md: %v", err)
	}
	for _, want := range []string{
		"F-EXECONCE-SEMANTICS",
		"`zsc execOnce` key shape must match command lifetime",
		"${appVersionId}",
		"per-deploy gate",
		"once per service lifetime",
		"bootstrap-seed",
		"principles/init-commands-model.md",
		// Run-42 worked example — the prose-vs-mechanism gap.
		"stamps each key",
		"per-deploy ledger",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("derived_rules.md missing F-EXECONCE-SEMANTICS anchor %q", want)
		}
	}
}

// TestDerivedRules_P3_ExecOnceSemanticsRuleSurfacesInBrief — the
// composer threads derived_rules.md into the refinement brief body.
// Pin that the F-EXECONCE-SEMANTICS rule lands in the assembled
// brief so the sub-agent sees it during the rule-walk pass.
func TestDerivedRules_P3_ExecOnceSemanticsRuleSurfacesInBrief(t *testing.T) {
	t.Parallel()
	plan := &Plan{Slug: "x", Codebases: []Codebase{{Hostname: "api"}}}
	brief, err := BuildRefinementBrief(plan, nil, "/run", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	for _, want := range []string{
		"F-EXECONCE-SEMANTICS",
		"key shape must match command lifetime",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("refinement brief missing F-EXECONCE-SEMANTICS surface anchor %q", want)
		}
	}
}
