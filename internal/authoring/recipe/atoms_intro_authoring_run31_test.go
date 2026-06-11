package recipe

import (
	"strings"
	"testing"
)

// Run-31 F-45 — closure-of-expectation phrasing leaks into tier README
// intros (a tier-summary surface), not just service-block yaml comments.
// Run-31 evidence: `runs/31/environments/5 — Highly-available
// Production/README.md:7` carries the parenthetical "Meilisearch and
// object-storage stay single-shape — no HA option in those families".
// F-33-broader's closure-of-expectation BAD/GOOD example (the run-30
// fix at per_tier_authoring.md:225-302) scopes to service-block yaml
// comments by predicate; the tier README intro is authored from the
// same env-content brief but at a different step in the workflow.
//
// Spirit: porter signals in tier README intros must stay within-tier
// (or the within-tier deployment shape, e.g. "single-instance with the
// 1 GB managed-RAM floor"); cross-scope tails — even parenthetical,
// even framed as "no move available" — pull the porter into the
// cross-tier comparison the brief's intro-authoring step says to avoid.
//
// Atom carries an explicit BAD/GOOD example for the tier-README-intro
// surface so the run-32 sub-agent rewrites the closure-of-expectation
// parenthetical at first write rather than refining it out later.

// TestEnvContentIntroAuthoringAtom_TeachesClosureOfExpectationGuard —
// per_tier_authoring.md is the env-content brief that authors per-tier
// README intros (workflow §2 first bullet) AND import-comments. The
// closure-of-expectation rule applies to BOTH surfaces; the atom must
// carry a worked example for the tier-README-intro shape so the agent
// recognizes the rule extends past the yaml-comment scope F-33-broader
// already pinned.
func TestEnvContentIntroAuthoringAtom_TeachesClosureOfExpectationGuard(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/env-content/per_tier_authoring.md")
	if err != nil {
		t.Fatalf("read per_tier_authoring.md: %v", err)
	}
	// Anchor the new section. The atom must carry a tier-README-intro-
	// scoped closure-of-expectation worked example so the rule's reach
	// is unambiguous.
	wantSection := "Tier README intros — closure-of-expectation guard"
	if !strings.Contains(body, wantSection) {
		t.Fatalf("per_tier_authoring.md missing tier-README-intro closure-of-expectation section %q", wantSection)
	}
	// BAD-shape anchor: the run-31 phrasing the agent emitted at
	// tier 5. Keeping the literal exhibit forces the lesson to be
	// concrete; the agent must see exactly the shape it's forbidden
	// from emitting.
	for _, want := range []string{
		"no HA option in those families",
		"**BAD**",
		"**GOOD**",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("per_tier_authoring.md tier-README-intro guard missing anchor %q", want)
		}
	}
	// The BAD/GOOD pair must live AFTER the section header so the
	// example anchors to the new surface, not the existing yaml-comment
	// section above it. Structural check: the new section header
	// precedes its BAD anchor in the body.
	sectionIdx := strings.Index(body, wantSection)
	if sectionIdx < 0 {
		t.Fatalf("section %q not found (re-checked after Contains assertion)", wantSection)
	}
	tail := body[sectionIdx:]
	badIdx := strings.Index(tail, "**BAD**")
	if badIdx < 0 {
		t.Fatal("tier-README-intro guard section missing BAD example")
	}
	goodIdx := strings.Index(tail, "**GOOD**")
	if goodIdx < 0 || goodIdx < badIdx {
		t.Error("tier-README-intro guard section missing GOOD revision after BAD")
	}
	// The GOOD revision must rephrase to a within-tier deployment-shape
	// description (e.g. "single-instance with the 1 GB managed-RAM
	// floor"), NOT just delete the closure-of-expectation tail.
	// Recipe content-quality at-bar means tier README intros still
	// describe the within-tier shape; the rule isn't "drop the
	// information," it's "rephrase to within-tier vocabulary."
	wantWithinTierKnob := "single-instance"
	if !strings.Contains(strings.ToLower(tail), wantWithinTierKnob) {
		t.Errorf("tier-README-intro GOOD revision missing within-tier descriptor %q (rephrase, don't delete)", wantWithinTierKnob)
	}
}
