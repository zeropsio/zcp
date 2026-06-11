package recipe

import (
	"strings"
	"testing"
)

// Run-29 Fix #3 — per_tier_authoring atom + rubric edits.
//
// The within-tier scope rule prevents the run-28 "Switch ... at tier 5"
// drift in service-block comments at tier 3/4. Friendly-authority
// phrasings must name a knob the porter turns WITHIN this tier's yaml,
// not a path that crosses the tier ladder. The rubric anchor at
// embedded_rubric.md gains a `within-tier` qualifier; the forbidden-
// narrative section extends to cover yaml service-block comments AND
// READMEs (refinement-time Notice, NOT a finalize-blocking gate).

func TestPerTierAuthoringAtom_WithinTierScopeSection_Present(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/env-content/per_tier_authoring.md")
	if err != nil {
		t.Fatalf("read per_tier_authoring.md: %v", err)
	}
	if !strings.Contains(body, "## Porter signals MUST be reachable within THIS tier's deployment shape") {
		t.Errorf("per_tier_authoring.md missing within-tier scope section heading")
	}
}

func TestPerTierAuthoringAtom_NamesWithinTierKnobs(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/env-content/per_tier_authoring.md")
	if err != nil {
		t.Fatalf("read per_tier_authoring.md: %v", err)
	}
	for _, want := range []string{
		"`verticalAutoscaling.minRam`",
		"`minFreeRamGB`",
		"`objectStorageSize`",
		"`enableSubdomainAccess`",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("per_tier_authoring.md within-tier knob list missing %q", want)
		}
	}
}

func TestPerTierAuthoringAtom_NamesCrossTierMoves(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/env-content/per_tier_authoring.md")
	if err != nil {
		t.Fatalf("read per_tier_authoring.md: %v", err)
	}
	for _, want := range []string{
		"`mode: NON_HA`",
		"`mode: HA`",
		"`minContainers` jumps",
		"`corePackage` tier changes",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("per_tier_authoring.md cross-tier moves list missing %q", want)
		}
	}
}

func TestPerTierAuthoringAtom_BadGoodExamples_BothPresent(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/env-content/per_tier_authoring.md")
	if err != nil {
		t.Fatalf("read per_tier_authoring.md: %v", err)
	}
	idx := strings.Index(body, "## Porter signals MUST be reachable within THIS tier's deployment shape")
	if idx < 0 {
		t.Fatal("within-tier section heading missing")
	}
	// Anchor on the next ## heading (the worked-example section that
	// follows). The Worked example block lives BETWEEN the within-tier
	// scope heading and the next ## heading.
	rest := body[idx:]
	end := strings.Index(rest[1:], "\n## ")
	if end < 0 {
		t.Fatal("within-tier section has no terminating ## heading")
	}
	window := rest[:end+1]
	for _, want := range []string{
		"**BAD**",
		"**GOOD**",
		"Switch mode to HA",          // BAD example
		"`minRam`",                   // GOOD example anchor (inline in text)
		"verticalAutoscaling.minRam", // GOOD example yaml
	} {
		if !strings.Contains(window, want) {
			t.Errorf("within-tier section missing BAD/GOOD anchor %q", want)
		}
	}
}

// TestPerTierAuthoringAtom_ArrivesAtTierBadExample_Present — F-33.
// Run-29 dogfood evidence: tier 4 db block at runs/29/environments/
// "4 — Small Production"/import.yaml:62-63 shipped "true failover
// arrives at tier 5 with `mode: HA`" — a cross-tier reference the
// existing BAD example (which uses "Switch mode to HA") didn't preempt
// because the agent's verb ("arrives") fell outside the BAD example's
// vocabulary. Atom now carries a second BAD example using the verb
// shapes that slipped past in run-29 ("arrives at tier N" / "available
// at tier N" / "comes online at tier N") with the matching GOOD
// revision: elide the cross-tier reference entirely (the tier table in
// the root README is where the porter learns tier 5 has HA).
func TestPerTierAuthoringAtom_ArrivesAtTierBadExample_Present(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/env-content/per_tier_authoring.md")
	if err != nil {
		t.Fatalf("read per_tier_authoring.md: %v", err)
	}
	for _, want := range []string{
		"arrives at tier",
		"available at tier",
		"comes online at tier",
		"unlocks at tier",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("per_tier_authoring.md missing arrives-at-tier-N BAD example anchor %q", want)
		}
	}
}

// TestPerTierAuthoringAtom_ClosureOfExpectationBadExample_Present —
// Run-30 F-33 PARTIAL. Run-29 closed the "arrives at tier N" BAD-example
// shape, but run-30 sub-agents discovered closure-of-expectation
// workarounds at tier 4/5 service-block comments:
//
//   - "stays single-shape across every tier" (tier 5 storage)
//   - "no HA mode at any tier" (tier 4 search)
//   - "holds at NON_HA even at this tier" (tier 5 search)
//
// These don't NAME a cross-tier MOVE but pull the porter's attention to
// the cross-tier scope ladder, which the per_tier_authoring brief's
// spirit prohibits. Atom carries a third BAD example covering
// closure-of-expectation phrasings with the matching GOOD revision (keep
// the within-tier knob, drop the cross-tier scope tail entirely —
// service-family invariants are research/IG concerns, not service-block-
// comment concerns).
func TestPerTierAuthoringAtom_ClosureOfExpectationBadExample_Present(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/env-content/per_tier_authoring.md")
	if err != nil {
		t.Fatalf("read per_tier_authoring.md: %v", err)
	}
	for _, want := range []string{
		"no HA mode at any tier",
		"stays single-shape across every tier",
		"holds at NON_HA even at this tier",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("per_tier_authoring.md missing closure-of-expectation BAD example anchor %q", want)
		}
	}
	// GOOD revision must drop the cross-scope tail and keep the within-
	// tier knob — verticalAutoscaling.minRam tied to a porter signal.
	// The GOOD revision must NOT itself contain cross-scope vocabulary
	// (the rule's spirit: service-family invariants are research/IG
	// concerns, NOT service-block-comment concerns). The "no replica
	// option on this service family" tail in the original GOOD revision
	// self-contradicted the section's rule — it asserted a service-
	// family invariant inside the comment the section forbids it from.
	goodHeader := "**GOOD** — within-tier knob only:"
	goodIdx := strings.Index(body, goodHeader)
	if goodIdx < 0 {
		t.Fatalf("per_tier_authoring.md missing closure-of-expectation GOOD-revision header %q", goodHeader)
	}
	rest := body[goodIdx:]
	end := strings.Index(rest, "\n```\n")
	if end < 0 {
		t.Fatal("closure-of-expectation GOOD revision yaml fence has no closing ```")
	}
	// Find end of fenced yaml — closing ``` line.
	closeFence := strings.Index(rest[end+5:], "```")
	if closeFence < 0 {
		t.Fatal("closure-of-expectation GOOD revision yaml fence not closed")
	}
	goodBlock := rest[:end+5+closeFence+3]
	if !strings.Contains(strings.ToLower(goodBlock), "bump verticalautoscaling.minram if") {
		t.Errorf("closure-of-expectation GOOD revision missing within-tier adapt path %q", "bump verticalAutoscaling.minRam if")
	}
	if !strings.Contains(goodBlock, "Single-node Meilisearch") {
		t.Errorf("closure-of-expectation GOOD revision missing within-tier descriptor %q", "Single-node Meilisearch")
	}
	for _, forbid := range []string{
		"no replica option on this service family",
		"service family",
		"across every tier",
		"at any tier",
	} {
		if strings.Contains(goodBlock, forbid) {
			t.Errorf("closure-of-expectation GOOD revision must NOT contain cross-scope vocabulary %q (rule's spirit: service-family invariants live in research/IG, not the service-block comment)", forbid)
		}
	}
}

// Run-34 Fix A — embedded_rubric.md was deleted; the within-tier
// scoring qualifier and the forbidden-narrative section were carried
// over to derived_rules.md as the Y8 (no tier-promotion narrative
// inside yaml comments) + T4 (no tier-promotion narrative on tier
// README) rules. The per_tier_authoring.md atom remains the canonical
// home for the positive-shape within-tier teaching at the env-content
// authoring phase; refinement walks Y8 + T4 to flag violations.

func TestDerivedRules_TierPromotionForbid_YamlAndTierReadme(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/refinement/derived_rules.md")
	if err != nil {
		t.Fatalf("read derived_rules.md: %v", err)
	}
	for _, want := range []string{
		"Y8 — no tier-promotion narrative inside yaml comments",
		"T4 — no tier-promotion narrative",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("derived_rules.md missing tier-promotion guard %q", want)
		}
	}
}
