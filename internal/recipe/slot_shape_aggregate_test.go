package recipe

import (
	"strings"
	"testing"
)

// Run-17 §10 — slot-shape refusal aggregation. KB and CLAUDE.md scans
// collect every offender per body so the agent re-authors against the
// full list in one round-trip. R-17-C10 closure (run-16 evidence:
// scaffold-api hit eight successive single-violation refusals on
// CLAUDE.md, naming one hostname each).

func TestCheckSlotShape_AggregatesAllOffenders_KBMultipleAuthorClaim(t *testing.T) {
	t.Parallel()
	body := strings.Join([]string{
		"- **Decompose execOnce keys into migrate + seed** — first offender.",
		"- **Pin synchronize false** — second offender.",
		"- **Use predis client** — third offender.",
	}, "\n")
	violations := checkSlotShape("codebase/api/knowledge-base", body)
	if len(violations) < 3 {
		t.Fatalf("expected at least 3 violations (one per author-claim stem); got %d (%v)", len(violations), violations)
	}
	for _, want := range []string{
		"Decompose execOnce keys into migrate + seed",
		"Pin synchronize false",
		"Use predis client",
	} {
		joined := strings.Join(violations, "\n")
		if !strings.Contains(joined, want) {
			t.Errorf("aggregate refusal should name stem %q; got: %s", want, joined)
		}
	}
}

func TestCheckSlotShape_AggregatesAllOffenders_ClaudeMDMultipleZeropsLeaks(t *testing.T) {
	t.Parallel()
	body := "# api\n\nframing\n\n## Build & run\n\n- run zsc noop\n- call zerops_deploy\n- use zcp sync push\n- zcli login\n\n## Architecture\n\n- src/main.ts"
	violations := checkSlotShape("codebase/api/claude-md", body)
	if len(violations) < 4 {
		t.Fatalf("expected 4 violations (zsc + zerops_* + zcp + zcli); got %d (%v)", len(violations), violations)
	}
	joined := strings.Join(violations, "\n")
	for _, token := range []string{"zsc", "zerops_*", "zcp", "zcli"} {
		if !strings.Contains(joined, "`"+token+"`") {
			t.Errorf("aggregate refusal should name token `%s`; got: %s", token, joined)
		}
	}
}

func TestCheckSlotShape_SingleOffender_StillSingleViolation(t *testing.T) {
	t.Parallel()
	body := "- **Decompose execOnce keys into migrate + seed** — single offender."
	violations := checkSlotShape("codebase/api/knowledge-base", body)
	if len(violations) != 1 {
		t.Errorf("single author-claim stem should produce single violation; got %d (%v)", len(violations), violations)
	}
}

func TestCheckSlotShape_NoOffender_EmptySlice(t *testing.T) {
	t.Parallel()
	body := "- **403 on cross-origin request** — symptom-first stem."
	violations := checkSlotShape("codebase/api/knowledge-base", body)
	if len(violations) != 0 {
		t.Errorf("clean body should produce empty slice; got %v", violations)
	}
}
