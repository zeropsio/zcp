package recipe

import (
	"path/filepath"
	"testing"
)

func TestSession_EnterPhase_MustBeAdjacentForward(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	s := NewSession("synth-showcase", log, dir, nil)

	// Cannot skip research → scaffold.
	if err := s.EnterPhase(PhaseScaffold); err == nil {
		t.Error("expected error entering non-adjacent phase")
	}
	// Cannot enter provision before research completes.
	if err := s.EnterPhase(PhaseProvision); err == nil {
		t.Error("expected error entering provision before research completes")
	}
}

func TestSession_PhaseFlow(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	s := NewSession("synth-showcase", log, dir, nil)

	// Use a no-op gate set so CompletePhase succeeds.
	gates := []Gate{}

	for i, p := range Phases() {
		if s.Current != p {
			t.Fatalf("phase %d: Current = %q, want %q", i, s.Current, p)
		}
		if _, err := s.CompletePhase(gates); err != nil {
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
	s := NewSession("synth-showcase", log, dir, nil)

	failing := Gate{
		Name: "always-fails",
		Run: func(ctx GateContext) []Violation {
			return []Violation{{Code: "test", Message: "always"}}
		},
	}

	violations, err := s.CompletePhase([]Gate{failing})
	if err != nil {
		t.Fatalf("CompletePhase: %v", err)
	}
	if len(violations) != 1 {
		t.Errorf("expected 1 violation, got %d", len(violations))
	}
	if s.Completed[PhaseResearch] {
		t.Error("phase should not be marked complete when gates fire")
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
