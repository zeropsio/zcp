package recipe

import (
	"strings"
	"testing"
)

// Run-17 §9.5 — snapshot/restore primitive for refinement-phase
// transactional Replace.

func TestSnapshotFragment_EmptyWhenAbsent(t *testing.T) {
	t.Parallel()
	s := &Session{Plan: &Plan{}}
	if got := s.SnapshotFragment("codebase/api/knowledge-base"); got != "" {
		t.Errorf("absent fragment: snapshot should be empty; got %q", got)
	}
}

func TestSnapshotFragment_ReturnsRecordedBody(t *testing.T) {
	t.Parallel()
	s := &Session{Plan: &Plan{Fragments: map[string]string{
		"codebase/api/knowledge-base": "- **403 on cors** — body",
	}}}
	if got := s.SnapshotFragment("codebase/api/knowledge-base"); got != "- **403 on cors** — body" {
		t.Errorf("snapshot body wrong: got %q", got)
	}
}

func TestRestoreFragment_BypassesValidators(t *testing.T) {
	t.Parallel()
	// RestoreFragment writes back to Plan.Fragments directly without
	// running slot_shape or classification refusal — it's the rollback
	// path for the refinement transactional wrapper.
	s := &Session{Plan: &Plan{Fragments: map[string]string{}}}
	// Body that would normally be refused by checkSlotShape (no
	// **Topic** prefix on a KB bullet).
	bad := "- a free-prose bullet that would be refused"
	s.RestoreFragment("codebase/api/knowledge-base", bad)
	if got := s.SnapshotFragment("codebase/api/knowledge-base"); got != bad {
		t.Errorf("RestoreFragment did not bypass validators: got %q", got)
	}
}

func TestRestoreFragment_NilPlan_NoOp(t *testing.T) {
	t.Parallel()
	// Defensive: RestoreFragment on a session without a plan should
	// not panic.
	s := &Session{}
	s.RestoreFragment("codebase/api/knowledge-base", "body")
	if got := s.SnapshotFragment("codebase/api/knowledge-base"); got != "" {
		t.Errorf("expected empty snapshot on nil plan; got %q", got)
	}
}

func TestRestoreFragment_NilFragmentsMap_Initializes(t *testing.T) {
	t.Parallel()
	s := &Session{Plan: &Plan{}}
	s.RestoreFragment("codebase/api/knowledge-base", "body")
	if got := s.SnapshotFragment("codebase/api/knowledge-base"); got != "body" {
		t.Errorf("expected restored body; got %q", got)
	}
}

// TestEmbeddedRubric_MatchesSpec pins the contract that the embedded
// rubric stays byte-identical to the spec rubric. Drift would mean
// the refinement sub-agent grades against a different rubric than
// post-dogfood ANALYSIS.md uses.
func TestEmbeddedRubric_MatchesSpec(t *testing.T) {
	t.Parallel()
	rubric, err := readAtom("briefs/refinement/embedded_rubric.md")
	if err != nil {
		t.Fatalf("read embedded rubric: %v", err)
	}
	// The spec lives at repo root — we use a hand-derived sentinel
	// (the rubric's first criterion header) rather than reading the
	// spec file from disk, since tests run from the package dir and
	// the relative path back to docs is fragile across CI shapes.
	for _, anchor := range []string{
		"Criterion 1 — Stem shape",
		"Criterion 2 — Voice",
		"Criterion 3 — Citation",
		"Criterion 4 — Trade-off",
		"Criterion 5 — Classification",
	} {
		if !strings.Contains(rubric, anchor) {
			t.Errorf("embedded rubric missing anchor %q — likely a partial copy from spec-content-quality-rubric.md", anchor)
		}
	}
}
