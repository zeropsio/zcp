package recipe

import (
	"path/filepath"
	"testing"
)

func TestSession_EnterPhase_MustBeAdjacentForward(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	s := NewSession("synth-showcase", "dev", log, dir, nil)

	// Cannot skip research → scaffold.
	if err := s.EnterPhase(PhaseScaffold); err == nil {
		t.Error("expected error entering non-adjacent phase")
	}
	// Cannot enter provision before research completes.
	if err := s.EnterPhase(PhaseProvision); err == nil {
		t.Error("expected error entering provision before research completes")
	}
}

// TestEnterPhase_RefusesSkippedPhase — run-47 Item I. Engine must still
// refuse a forward jump to a phase that is NOT in s.Completed and NOT
// adjacent-forward. The completed-phase idempotency carve-out must not
// silently mask genuine skipped-phase errors.
func TestEnterPhase_RefusesSkippedPhase(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	s := NewSession("synth-showcase", "dev", log, dir, nil)

	// Mark research + provision complete; session Current = provision
	// (entered+completed) — refinement is far ahead and NOT in
	// Completed. The idempotency carve-out must NOT accept this call.
	s.Completed[PhaseResearch] = true
	s.Completed[PhaseProvision] = true
	s.Current = PhaseProvision

	if err := s.EnterPhase(PhaseRefinement); err == nil {
		t.Error("EnterPhase refinement from provision with no intermediate phases completed must refuse")
	}
}

// TestEnterPhase_AdjacentForwardStillWorks — run-47 Item I regression.
// The completed-phase carve-out must not break adjacent-forward semantics
// when the requested phase is NOT yet in Completed.
func TestEnterPhase_AdjacentForwardStillWorks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	s := NewSession("synth-showcase", "dev", log, dir, nil)

	// Walk research → provision the normal way.
	if _, _, err := s.CompletePhase(nil); err != nil {
		t.Fatalf("CompletePhase research: %v", err)
	}
	if err := s.EnterPhase(PhaseProvision); err != nil {
		t.Fatalf("EnterPhase provision (adjacent-forward, not in Completed): %v", err)
	}
	if s.Current != PhaseProvision {
		t.Errorf("after adjacent-forward enter: Current = %q, want %q", s.Current, PhaseProvision)
	}
}

// TestEnterPhase_SamePhaseStillIdempotent — run-47 Item I regression.
// The original same-phase idempotency at workflow.go:172 must remain
// intact: EnterPhase(s.Current) returns nil with no state change.
func TestEnterPhase_SamePhaseStillIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	s := NewSession("synth-showcase", "dev", log, dir, nil)

	// s.Current is PhaseResearch by default; PhaseResearch is NOT in
	// Completed yet. Same-phase idempotency must work independently of
	// the completed-phase carve-out.
	if err := s.EnterPhase(PhaseResearch); err != nil {
		t.Errorf("EnterPhase(current phase) must be idempotent: got %v", err)
	}
	if s.Current != PhaseResearch {
		t.Errorf("same-phase idempotent call mutated Current: got %q, want %q",
			s.Current, PhaseResearch)
	}
	if s.Completed[PhaseResearch] {
		t.Error("same-phase idempotent call must not mark phase complete")
	}
}

// Run-16 §6.1 — phase enum extends to 7. Walk every adjacency the new
// pipeline introduces and confirm non-adjacent transitions still error.

func TestPhase_AdjacentForward_CodebaseContentAfterFeature(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	s := NewSession("synth-showcase", "dev", log, dir, nil)
	for _, p := range []Phase{PhaseProvision, PhaseScaffold, PhaseFeature} {
		if _, _, err := s.CompletePhase(nil); err != nil {
			t.Fatalf("CompletePhase %q: %v", s.Current, err)
		}
		if err := s.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase %q: %v", p, err)
		}
	}
	if _, _, err := s.CompletePhase(nil); err != nil {
		t.Fatalf("CompletePhase feature: %v", err)
	}
	if err := s.EnterPhase(PhaseCodebaseContent); err != nil {
		t.Errorf("EnterPhase codebase-content after feature should succeed: %v", err)
	}
}

func TestPhase_AdjacentForward_EnvContentAfterCodebaseContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	s := NewSession("synth-showcase", "dev", log, dir, nil)
	for _, p := range []Phase{PhaseProvision, PhaseScaffold, PhaseFeature, PhaseCodebaseContent} {
		if _, _, err := s.CompletePhase(nil); err != nil {
			t.Fatalf("CompletePhase %q: %v", s.Current, err)
		}
		if err := s.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase %q: %v", p, err)
		}
	}
	if _, _, err := s.CompletePhase(nil); err != nil {
		t.Fatalf("CompletePhase codebase-content: %v", err)
	}
	if err := s.EnterPhase(PhaseEnvContent); err != nil {
		t.Errorf("EnterPhase env-content after codebase-content should succeed: %v", err)
	}
}

func TestPhase_AdjacentForward_FinalizeAfterEnvContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	s := NewSession("synth-showcase", "dev", log, dir, nil)
	for _, p := range []Phase{PhaseProvision, PhaseScaffold, PhaseFeature, PhaseCodebaseContent, PhaseEnvContent} {
		if _, _, err := s.CompletePhase(nil); err != nil {
			t.Fatalf("CompletePhase %q: %v", s.Current, err)
		}
		if err := s.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase %q: %v", p, err)
		}
	}
	if _, _, err := s.CompletePhase(nil); err != nil {
		t.Fatalf("CompletePhase env-content: %v", err)
	}
	if err := s.EnterPhase(PhaseFinalize); err != nil {
		t.Errorf("EnterPhase finalize after env-content should succeed: %v", err)
	}
}

func TestPhase_NonAdjacent_FeatureSkipsToFinalize_Errors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	s := NewSession("synth-showcase", "dev", log, dir, nil)
	for _, p := range []Phase{PhaseProvision, PhaseScaffold, PhaseFeature} {
		if _, _, err := s.CompletePhase(nil); err != nil {
			t.Fatalf("CompletePhase %q: %v", s.Current, err)
		}
		if err := s.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase %q: %v", p, err)
		}
	}
	if _, _, err := s.CompletePhase(nil); err != nil {
		t.Fatalf("CompletePhase feature: %v", err)
	}
	if err := s.EnterPhase(PhaseFinalize); err == nil {
		t.Error("EnterPhase finalize directly after feature should error (non-adjacent post-run-16)")
	}
}

func TestSession_PhaseFlow(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	s := NewSession("synth-showcase", "dev", log, dir, nil)

	// Use a no-op gate set so CompletePhase succeeds.
	gates := []Gate{}

	for i, p := range Phases() {
		if s.Current != p {
			t.Fatalf("phase %d: Current = %q, want %q", i, s.Current, p)
		}
		if _, _, err := s.CompletePhase(gates); err != nil {
			t.Fatalf("CompletePhase %q: %v", p, err)
		}
		// Last phase has no next.
		if i == len(Phases())-1 {
			break
		}
		next := Phases()[i+1]
		if err := s.EnterPhase(next); err != nil {
			t.Fatalf("EnterPhase %q: %v", next, err)
		}
	}
}

func TestSession_CompletePhase_BlocksOnViolation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	s := NewSession("synth-showcase", "dev", log, dir, nil)

	failing := Gate{
		Name: "always-fails",
		Run: func(ctx GateContext) []Violation {
			return []Violation{{Code: "test", Message: "always"}}
		},
	}

	violations, notices, err := s.CompletePhase([]Gate{failing})
	if err != nil {
		t.Fatalf("CompletePhase: %v", err)
	}
	if len(violations) != 1 {
		t.Errorf("expected 1 blocking violation, got %d", len(violations))
	}
	if len(notices) != 0 {
		t.Errorf("expected 0 notices, got %d", len(notices))
	}
	if s.Completed[PhaseResearch] {
		t.Error("phase should not be marked complete when gates fire")
	}
}

func TestSession_CompletePhase_NoticeDoesNotBlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	s := NewSession("synth-showcase", "dev", log, dir, nil)

	noticeOnly := Gate{
		Name: "advisory",
		Run: func(ctx GateContext) []Violation {
			return []Violation{{Code: "advisory", Message: "soft", Severity: SeverityNotice}}
		},
	}

	blocking, notices, err := s.CompletePhase([]Gate{noticeOnly})
	if err != nil {
		t.Fatalf("CompletePhase: %v", err)
	}
	if len(blocking) != 0 {
		t.Errorf("expected 0 blocking violations, got %d", len(blocking))
	}
	if len(notices) != 1 {
		t.Errorf("expected 1 notice, got %d", len(notices))
	}
	if !s.Completed[PhaseResearch] {
		t.Error("notice-only gates must not block phase completion")
	}
}

func TestSession_CompletePhase_DefaultSeverityBlocks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	s := NewSession("synth-showcase", "dev", log, dir, nil)

	// Zero-value Severity must keep historical blocking behavior.
	zeroSeverity := Gate{
		Name: "zero-severity",
		Run: func(ctx GateContext) []Violation {
			return []Violation{{Code: "test", Message: "default"}}
		},
	}

	blocking, _, err := s.CompletePhase([]Gate{zeroSeverity})
	if err != nil {
		t.Fatalf("CompletePhase: %v", err)
	}
	if len(blocking) != 1 {
		t.Errorf("expected 1 blocking violation (zero-value Severity), got %d", len(blocking))
	}
	if s.Completed[PhaseResearch] {
		t.Error("zero-severity violation must keep phase open")
	}
}

func TestGateMainAgentRewroteWriterPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		path   string
		author string
		want   bool
	}{
		{"writer-owned README rewrite by main", "codebases/app/README.md", "main", true},
		{"writer-owned README by writer", "codebases/app/README.md", "writer", false},
		{"non-writer path by main", "src/main.ts", "main", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := MainAgentRewroteWriterPath(tc.path, tc.author)
			got := v != nil
			if got != tc.want {
				t.Errorf("got=%v want=%v", got, tc.want)
			}
		})
	}
}
