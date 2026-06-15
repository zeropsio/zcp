package recipe

import (
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

// Run-33 architectural fix #2 + run-34 Fix A — embedded_rubric.md
// retired and deleted; rule-walk against derived_rules.md is the sole
// scoring substrate. The byte-identity-to-spec contract is gone with
// the rubric atom. Per-rule pinning lives in
// `briefs_refinement_test.go::TestBuildRefinementBrief_EmbedsDerivedRules`
// (which asserts rule ids land in the brief body).
