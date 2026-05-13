package recipe

import (
	"path/filepath"
	"strings"
	"testing"
)

// Run-23 F-26 (superseded by run-43 Edit D / P6 consolidation) —
// refinement-mandatory gates lived at TWO sites pre-Edit D:
//   - finalize-close demanded RefinementDispatched (F-26 site)
//   - refinement-close demanded BOTH RefinementDispatched +
//     Refinement2Dispatched (Run-41 site)
//
// Run-42 dogfood proved the dual-gate scheme produced "three
// refinement passes, wrong order" runs: main agent dispatched
// refinement-1 + refinement-2 during finalize-close demand iteration
// (because the finalize gate refused), then redispatched refinement-1
// at refinement phase (because phase-8 entry guidance led there).
// Run-43 Edit D consolidates: finalize-close runs no refinement
// demand; refinement-close enforces both dispatched flags AND re-runs
// surface validators (so ACTs the main agent made on refinement-2
// findings get re-validated at refinement-close, not implicitly at
// finalize-close iteration).
//
// Export gate (separate process) still refuses unless
// RefinementClosed — unchanged by Edit D.

// TestCompletePhaseFinalize_DoesNotDemandRefinementDispatch — Edit D
// inversion of the original F-26 gate. Finalize-close MUST close
// cleanly without refinement dispatched; the demand has moved to
// refinement-phase-close. Pinned as inversion (not removed) to
// prevent re-introduction of the dual-gate scheme.
func TestCompletePhaseFinalize_DoesNotDemandRefinementDispatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)

	for _, p := range []Phase{
		PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature,
		PhaseCodebaseContent, PhaseEnvContent, PhaseFinalize,
	} {
		if err := sess.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase(%s): %v", p, err)
		}
		sess.Completed[p] = true
	}
	// RefinementDispatched is the zero value (false). complete-phase
	// finalize MUST close cleanly under Edit D — the gate is gone.
	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseFinalize)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if !r.OK {
		t.Fatalf("expected ok=true when finalize closes without refinement dispatch (Edit D); got Error=%q Violations=%v", r.Error, r.Violations)
	}
	// And the error string must NOT mention the now-removed gate
	// recovery action — surfacing it would be a regression.
	if strings.Contains(r.Error, "build-subagent-prompt briefKind=refinement") {
		t.Errorf("finalize-close should not name the refinement recovery action; got %q", r.Error)
	}
}

// TestBuildSubagentPrompt_RefinementBrief_FlipsDispatchedFlag — F-26
// (unchanged by Edit D). The flag still flips on dispatch; it is
// still consumed by the refinement-close gate (and by the export
// gate via RefinementClosed downstream).
func TestBuildSubagentPrompt_RefinementBrief_FlipsDispatchedFlag(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: dir + "/run",
	})
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	if sess.RefinementDispatched {
		t.Fatal("RefinementDispatched should start false")
	}
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "build-subagent-prompt", Slug: "synth-showcase",
		BriefKind: "refinement",
	})
	if !res.OK {
		t.Fatalf("build-subagent-prompt refinement: %+v", res)
	}
	if !sess.RefinementDispatched {
		t.Error("RefinementDispatched should flip to true after refinement brief dispatch")
	}
}

// TestCompletePhaseFinalize_OkWhenRefinementDispatched — F-26 happy
// path sanity check, kept as a passive guard against the flag
// BLOCKING finalize close in either direction.
func TestCompletePhaseFinalize_OkWhenRefinementDispatched(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)

	for _, p := range []Phase{
		PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature,
		PhaseCodebaseContent, PhaseEnvContent, PhaseFinalize,
	} {
		if err := sess.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase(%s): %v", p, err)
		}
		sess.Completed[p] = true
	}
	sess.RefinementDispatched = true

	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseFinalize)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if !r.OK {
		t.Fatalf("expected ok=true with RefinementDispatched=true; got Error=%q", r.Error)
	}
}

// TestCompletePhaseRefinement_GateSetIncludesSurfaceValidators —
// Edit D extension of gatesForPhase(PhaseRefinement). After the main
// agent ACTs on refinement-2 findings (record-fragment mode=replace
// against codebase + env surfaces), the refinement-close path must
// re-run the surface validators to catch any defects the ACTs
// introduced. Previously the only catch was implicit, via the
// finalize-close validator iteration (because the main agent ACTed
// during finalize-close demand); after Edit D consolidation it must
// be explicit at refinement-close.
//
// Pins: the gate set returned by gatesForPhase(PhaseRefinement)
// contains the surface-validator names from CodebaseContentGates +
// EnvGates in addition to the stitched-matches-plan safety net.
func TestCompletePhaseRefinement_GateSetIncludesSurfaceValidators(t *testing.T) {
	t.Parallel()
	gates := gatesForPhase(PhaseRefinement)
	names := make(map[string]struct{}, len(gates))
	for _, g := range gates {
		names[g.Name] = struct{}{}
	}
	if _, ok := names["stitched-matches-plan"]; !ok {
		t.Errorf("PhaseRefinement gates missing stitched-matches-plan safety net; got %v", gateNames(gates))
	}
	// Cover at least one well-known gate name from each set so the
	// inclusion is asserted by structure, not by enumerating every
	// gate (the surface-validator sets evolve).
	cbGates := CodebaseContentGates()
	codebaseSentinels := make([]string, 0, len(cbGates))
	for _, g := range cbGates {
		codebaseSentinels = append(codebaseSentinels, g.Name)
	}
	envGatesList := EnvGates()
	envSentinels := make([]string, 0, len(envGatesList))
	for _, g := range envGatesList {
		envSentinels = append(envSentinels, g.Name)
	}
	if len(codebaseSentinels) == 0 {
		t.Fatal("CodebaseContentGates() empty — surface validators must exist for Edit D to be meaningful")
	}
	if len(envSentinels) == 0 {
		t.Fatal("EnvGates() empty — surface validators must exist for Edit D to be meaningful")
	}
	missingCodebase := make([]string, 0, len(codebaseSentinels))
	for _, want := range codebaseSentinels {
		if _, ok := names[want]; !ok {
			missingCodebase = append(missingCodebase, want)
		}
	}
	if len(missingCodebase) > 0 {
		t.Errorf("PhaseRefinement gate set missing CodebaseContentGates: %v; got %v", missingCodebase, gateNames(gates))
	}
	missingEnv := make([]string, 0, len(envSentinels))
	for _, want := range envSentinels {
		if _, ok := names[want]; !ok {
			missingEnv = append(missingEnv, want)
		}
	}
	if len(missingEnv) > 0 {
		t.Errorf("PhaseRefinement gate set missing EnvGates: %v; got %v", missingEnv, gateNames(gates))
	}
}

// TestCompletePhaseRefinement_FlipsClosedFlagAndWritesMarker — F-26.
// Closure of refinement flips RefinementClosed AND writes the
// .refinement-closed marker on disk so the export gate (separate
// process) can read the closure signal.
//
// Run-43 Edit D / P6 — refinement-close gate set now includes surface
// validators (CodebaseContentGates + EnvGates). Materializing every
// surface (IG/KB/CLAUDE per codebase + env-comments per tier +
// priority-justification blocks etc.) for a flag-flip pin would
// require the full content-phase fixture; that's covered by the
// dedicated content + env close tests. This test isolates the
// marker-flip transition by leaving `Completed[PhaseRefinement]=true`
// so CompletePhase's "already complete → return nil" short-circuit
// fires (same pattern as TestCompletePhaseFinalize_AutoAdvancesToRefinement).
// The dispatch-flag-gate (refinement1 + refinement2 dispatched) and
// the surface-validator-set inclusion are pinned separately by
// TestCompletePhaseRefinement_GateSetIncludesSurfaceValidators +
// the refinement-dispatch-flag tests.
func TestCompletePhaseRefinement_FlipsClosedFlagAndWritesMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	// Run-40 fix-up #3 — refinement-close gate path needs a session
	// shape that can render an OutputRoot snapshot; stage scaffold
	// surfaces so OutputRoot exists for the marker write.
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
	// Run-41 — refinement-2 (cross-surface audit) must also dispatch
	// before complete-phase phase=refinement closes. Set the flag so
	// this test exercises the close path, not the dispatch gate.
	sess.Refinement2Dispatched = true
	// Run-46 Item 1 — walked-ledger receipt gate. Set a full-coverage
	// ledger so the close path runs past the gate (other tests exercise
	// the gate's refusal branch).
	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	sess.Refinement2Ledger = &Refinement2Ledger{Walked: manifest.AllKeys()}
	// Run-43 Edit D — leave Completed[PhaseRefinement]=true so
	// CompletePhase's short-circuit isolates the flag-flip path from
	// the surface-validator set (which would require materializing
	// every codebase + env surface to pass). The flag-flip below is
	// still exercised because the OK=true branch is taken on the
	// short-circuit return.
	sess.Current = PhaseRefinement

	if sess.RefinementClosed {
		t.Fatal("RefinementClosed should start false")
	}
	if IsRefinementClosed(dir) {
		t.Fatal("close marker should not exist before close")
	}
	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseRefinement)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if !r.OK {
		t.Fatalf("expected ok=true on refinement close; got Error=%q Violations=%v", r.Error, r.Violations)
	}
	if !sess.RefinementClosed {
		t.Error("RefinementClosed should flip to true on close")
	}
	if !IsRefinementClosed(dir) {
		t.Error("close marker should exist after close")
	}
}
