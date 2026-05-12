package recipe

import (
	"strings"
	"testing"
)

// briefs_refinement2_kb_ig_dup_g4_test.go — Run-44 G4 substrate pin.
//
// Run-43 validation §"(4) appdev KB-IG duplication held weakly":
// refinement-2 correctly caught KB #1 + #2 dupe with IG #2 + #3 as
// advisories. Main agent HELD on "each KB body answers a different
// question shape than its IG counterpart". Codex independent read:
// IG #3 already quoted "Blocked request. This host is not allowed."
// at lines 117-130; KB #1 restated the same symptom/fix at 158-160.
// IG #2 taught the literal-token trap at 97-115; KB #2 restated at
// 162-164. Two appdev KB bullets are pure IG echoes; weak HOLD.
//
// Fix: add a severity-promotion clause to `kb-ig-duplication` — when
// the KB body's stem SYMPTOM AND the fix MECHANISM both appear in
// the matching IG body (prose + code), promote severity to **blocker**
// with `suggestedAction: "drop"`. Pure IG-echo KBs lose the
// borderline-defensibility cover.

// TestRefinement2Brief_KBIGDup_BlockerPromotionClause — the
// kb-ig-duplication class section MUST carry an explicit
// severity-promotion clause for the "stem symptom + fix mechanism both
// in IG body" case.
func TestRefinement2Brief_KBIGDup_BlockerPromotionClause(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	idx := strings.Index(brief.Body, "## Defect class: kb-ig-duplication")
	if idx < 0 {
		t.Fatal("kb-ig-duplication class header missing")
	}
	tail := brief.Body[idx:]
	next := strings.Index(tail[1:], "\n## Defect class:")
	var chunk string
	if next > 0 {
		chunk = tail[:next+1]
	} else {
		chunk = tail[:min(len(tail), 4096)]
	}
	// G4 promotion clause anchors.
	for _, want := range []string{
		// The new clause must explicitly name "blocker" promotion.
		"promote",
		// Trigger condition — stem symptom AND fix mechanism both in IG.
		"stem symptom",
		"fix mechanism",
		// suggestedAction on the promoted finding is `drop` (pure IG echo).
		"suggestedAction",
		"drop",
		// Pin the "pure IG echo" framing so the rule's intent is clear.
		"pure IG echo",
	} {
		if !strings.Contains(chunk, want) {
			t.Errorf("kb-ig-duplication class missing G4 promotion-clause anchor %q", want)
		}
	}
}
