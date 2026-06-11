package recipe

import (
	"path/filepath"
	"testing"
)

// handlers_refinement_f4_test.go — Run-43 F4 substrate pin.
//
// Three-way confirmed (codex + opus + user direction):
// TestCompletePhaseRefinement_FlipsClosedFlagAndWritesMarker
// deliberately sets Completed[PhaseRefinement]=true BEFORE calling
// completePhase, which causes Session.CompletePhase (workflow.go:187-189)
// to short-circuit before running gates. There is NO test that
// exercises Edit D's new behavior — that
// gatesForPhase(PhaseRefinement) re-runs CodebaseContentGates +
// EnvGates against a real fixture, catching post-ACT regressions at
// refinement-close.
//
// This test materializes a refinement-state session, sets
// RefinementDispatched + Refinement2Dispatched (so the dispatch-flag
// gate passes), leaves Completed[PhaseRefinement]=false (so gates
// RUN), injects a regression via sess.Plan.Fragments (a known V-rule
// violation in an IG fragment body), calls complete-phase via the
// production handler path, and asserts r.OK=false +
// r.Violations contains the expected violation code.
//
// Pins Edit D's load-bearing claim: surface validators catch
// post-ACT regressions at refinement-close.

// TestCompletePhaseRefinement_GatesCatchPostACTRegression — Edit D
// end-to-end fixture. Inject `codebase-ig-plain-ordered-list`
// violation (existing gate, well-pinned) into a codebase IG fragment;
// the refinement-close gate set MUST surface it before flipping
// RefinementClosed.
func TestCompletePhaseRefinement_GatesCatchPostACTRegression(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, sess.Plan)
	sess.OutputRoot = dir
	if _, err := stitchContent(sess); err != nil {
		t.Fatalf("seed stitch-content: %v", err)
	}

	for _, p := range []Phase{
		PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature,
		PhaseCodebaseContent, PhaseEnvContent, PhaseFinalize, PhaseRefinement,
	} {
		if err := sess.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase(%s): %v", p, err)
		}
		sess.Completed[p] = true
	}
	sess.RefinementDispatched = true
	sess.Refinement2Dispatched = true
	// Critical: leave Completed[PhaseRefinement]=false so CompletePhase
	// does NOT short-circuit; gates MUST run.
	sess.Completed[PhaseRefinement] = false
	sess.Current = PhaseRefinement

	// Inject a regression — replace api's stitched-clean IG with a
	// plain-ordered-list shape that fires
	// codebase-ig-plain-ordered-list. Mirrors the pattern at
	// gates_test.go:99-101 + handlers_refinement_test.go:32-35.
	if sess.Plan.Fragments == nil {
		sess.Plan.Fragments = map[string]string{}
	}
	sess.Plan.Fragments["codebase/api/integration-guide"] = "1. plain ordered\n2. list shape\n"
	// Re-stitch so the file tree picks up the regression before the
	// gates read it. The validator works against the
	// plan.Fragments-driven assembled README (`collectCodebaseBodies`
	// in gates.go threads the in-memory Fragments map directly).
	if _, err := stitchContent(sess); err != nil {
		t.Fatalf("post-injection stitch-content: %v", err)
	}
	// Run-46 Item 1 + Item 6 — populate walked-ledger AND cross-surface
	// uniqueness scan BEFORE the close call so the gate-set test
	// isolates the surface-validator path. The ledger gates are
	// exercised separately in their own tests.
	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	allKeys := manifest.AllKeys()
	sess.Refinement2Ledger = &Refinement2Ledger{
		Walked:                        allKeys,
		CrossSurfaceUniquenessScanned: len(allKeys),
	}

	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseRefinement)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})

	// The gates MUST surface the injected violation — refinement-close
	// is the catch-net for post-ACT regressions per Edit D.
	if r.OK {
		t.Fatalf("expected ok=false when refinement-close gate catches post-ACT regression; got Error=%q Violations=%v", r.Error, r.Violations)
	}
	found := false
	for _, v := range r.Violations {
		if v.Code == "codebase-ig-plain-ordered-list" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("refinement-close MUST surface codebase-ig-plain-ordered-list (Edit D contract: surface validators re-run at refinement-close); got violations: %v", r.Violations)
	}
	// And the close marker must NOT fire — the regression is a blocker.
	if sess.RefinementClosed {
		t.Error("RefinementClosed should stay false when gates block the close")
	}
	if IsRefinementClosed(dir) {
		t.Error("close marker must not appear on disk when refinement-close blocks")
	}
}
