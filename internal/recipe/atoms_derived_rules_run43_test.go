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
		"INIT_SEED",
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

// TestDerivedRules_R49_ExecOnceBurnRecoveryRulePresent pins the
// run-49 extension to F-EXECONCE-SEMANTICS that catches burn-recovery
// vocabulary attached to per-deploy (`${appVersionId}`) keys. The
// canonical example: a worked example showing
// `${appVersionId}-migrate` / `${appVersionId}-seed` split with the
// rationale "split so failed seed doesn't burn the migrate key — the
// next deploy retries the seed but already-applied schema is skipped"
// — the keys are per-deploy (auto-fresh on next deploy) but the
// rationale is static-key burn-recovery vocab. Cross-reference:
// `principles/init-commands-model.md:50-60` (distinct-keys-per-step
// rationale is within-deploy lock-collision avoidance, NOT
// cross-deploy burn-recovery).
func TestDerivedRules_R49_ExecOnceBurnRecoveryRulePresent(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/refinement/derived_rules.md")
	if err != nil {
		t.Fatalf("read derived_rules.md: %v", err)
	}
	for _, want := range []string{
		// New failure-mode (b) framing — burn-recovery over per-deploy key.
		"Burn-recovery prose over per-deploy key",
		"burn-recovery vocabulary",
		"`burn`",
		"`burned`",
		"`consumed key`",
		"split keys so failed",
		"touch any source file to rotate appVersionId",
		// Authoritative anchor: burn-recovery belongs to static keys.
		"Burn-recovery semantics belong to **static keys only**",
		// Within-deploy collision is the correct rationale for
		// distinct per-deploy keys, NOT burn-recovery.
		"within-deploy lock-collision avoidance pattern",
		"`${appVersionId}-migrate` / `${appVersionId}-seed`",
		"two commands sharing one `${appVersionId}` collapse to one lock",
		// Run-49 user feedback anchor.
		"Run-49 user feedback",
		"already-applied schema is skipped",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("derived_rules.md missing run-49 burn-recovery anchor %q", want)
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

// TestDerivedRules_P5_CrossSurfaceReferenceRulePresent pins the
// F-XSURF-REF factuality guard. Run-42 dogfood proved the rule is
// necessary; both Surface 7 yaml-comment deferrals
// (apidev/zerops.yaml:47-48 + :63-64 "see IG #N" / "the pattern is
// taught in IG #3") and Surface 3 cross-tier deferrals (environments/1
// + /2 "Same as tier 0") slipped past refinement-1.
//
// Run-43 Edit B broadens the cross-tier pattern enumeration; run-43
// F5 (this revision) reframes the rule from literal-enumeration to
// LLM-judgment-with-examples. User direction: shift the rule from
// pattern-matching to judgment — the patterns listed are NOT
// exhaustive; ask the LLM to judge intent (does this comment defer
// to another surface/tier?), not pattern-match a regex. Tier-2's
// actual wording is *"Same single-instance Postgres shape as the
// previous tier"* — literal-pattern enumeration is whack-a-mole.
// Keep all current example patterns as non-exhaustive examples; add
// "Same X as the previous tier" + the bare "Same X shape" form;
// replace "Scan" framing with "Judge each comment for cross-surface
// or cross-tier deferral intent, using these patterns as
// non-exhaustive examples".
func TestDerivedRules_P5_CrossSurfaceReferenceRulePresent(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/refinement/derived_rules.md")
	if err != nil {
		t.Fatalf("read derived_rules.md: %v", err)
	}
	for _, want := range []string{
		"F-XSURF-REF",
		"mechanism+reason in one breath",
		// Run-43 F5 — LLM-judgment framing.
		"Judge each comment",
		"non-exhaustive examples",
		// Run-43 F5 — tier-2 verbatim wording added as additional example.
		"as the previous tier",
		"Same X shape",
		// Cross-codebase-surface deferral patterns (kept as
		// non-exhaustive examples).
		"see IG #<digit>",
		"the pattern is taught in",
		"live below",
		"at the field site",
		// Cross-tier deferral patterns — original literal forms (kept
		// as non-exhaustive examples).
		"Same as tier <digit>",
		"see tier <digit>",
		// Run-43 Edit B — broadened cross-tier patterns.
		"Same <anything> as tier <digit>",
		"same shape as tier <digit>",
		"as tier <digit>",
		// Run-43 Edit B — verbatim run-42 worked-example anchor.
		"Same dev / stage pair as tier 0",
		// Spec citations.
		"§\"Surface 7\"",
		"§\"Surface 3\"",
		// Run-42 worked-example anchors.
		"apidev/zerops.yaml:47-48",
		"environments/1 + environments/2",
		// Cross-link to synthesis_workflow.md's complementary rule.
		"Yaml comments stand alone",
		// Run-43 F5 — GOOD worked example: self-contained per-tier
		// sizing rationale.
		"Single-instance Postgres on NON_HA",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("derived_rules.md missing F-XSURF-REF anchor %q", want)
		}
	}
}

// TestDerivedRules_P5_CrossSurfaceReferenceRuleSurfacesInBrief —
// refinement brief composer threads the F-XSURF-REF rule into the
// brief body so the sub-agent sees it during rule-walk.
func TestDerivedRules_P5_CrossSurfaceReferenceRuleSurfacesInBrief(t *testing.T) {
	t.Parallel()
	plan := &Plan{Slug: "x", Codebases: []Codebase{{Hostname: "api"}}}
	brief, err := BuildRefinementBrief(plan, nil, "/run", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	for _, want := range []string{
		"F-XSURF-REF",
		"no cross-surface deferrals",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("refinement brief missing F-XSURF-REF surface anchor %q", want)
		}
	}
}
