package recipe

import (
	"path/filepath"
	"testing"
)

// TestCompletePhaseFinalize_AutoAdvancesToRefinement pins the run-18
// always-on refinement contract: when the finalize gate closes ok
// (no blocking violations), the engine auto-advances Current to
// PhaseRefinement. This is the ONE phase transition the engine drives
// (every other transition is explicit via `enter-phase`).
//
// Refinement is the always-on quality gate; snapshot/restore makes
// every replace safe. Mandatory dispatch closes run-17's failure mode
// where the agent saw notices and declined the optional pass.
func TestCompletePhaseFinalize_AutoAdvancesToRefinement(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", log, dir, nil)

	// Walk the whole phase ladder up to + including finalize so the
	// session ends with Current=PhaseFinalize, Completed[finalize]=true.
	// CompletePhase short-circuits ("already complete → return nil"),
	// blocking=empty, OK=true, and the auto-advance branch fires —
	// isolating the run-18 transition behavior from the finalize gate
	// set (which would require materializing all 6 tier import.yaml
	// files on disk to pass).
	for _, p := range []Phase{
		PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature,
		PhaseCodebaseContent, PhaseEnvContent, PhaseFinalize,
	} {
		if err := sess.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase(%s): %v", p, err)
		}
		sess.Completed[p] = true
	}

	// Drive a finalize complete-phase via the handler entry point.
	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseFinalize)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})

	if !r.OK {
		t.Fatalf("expected ok=true on clean finalize close; got Violations=%v Error=%q", r.Violations, r.Error)
	}
	if r.Status == nil {
		t.Fatalf("expected Status snapshot in response")
	}
	if got := r.Status.Current; got != PhaseRefinement {
		t.Fatalf("expected Current to auto-advance to %q after finalize close, got %q",
			PhaseRefinement, got)
	}
	if !sess.Completed[PhaseFinalize] {
		t.Fatalf("expected Completed[finalize]=true after close")
	}
	if r.Guidance == "" {
		t.Fatalf("expected refinement-phase guidance attached after auto-advance")
	}
}

// TestCompletePhaseEarlierPhases_DoNotAutoAdvance pins that the
// auto-advance is finalize-only. Codebase-content / env-content / etc.
// still require explicit `enter-phase` per system.md's TEACH-side
// "explicit transitions" rule.
func TestCompletePhaseEarlierPhases_DoNotAutoAdvance(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", log, dir, nil)

	for _, p := range []Phase{
		PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature,
		PhaseCodebaseContent,
	} {
		if err := sess.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase(%s): %v", p, err)
		}
		sess.Completed[p] = true
	}

	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseCodebaseContent)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})

	if !r.OK {
		t.Fatalf("expected ok=true; got Violations=%v Error=%q", r.Violations, r.Error)
	}
	// Current stays at codebase-content — agent must explicitly
	// EnterPhase(env-content) next.
	if got := r.Status.Current; got != PhaseCodebaseContent {
		t.Fatalf("expected Current to stay at %q (no auto-advance); got %q",
			PhaseCodebaseContent, got)
	}
}
