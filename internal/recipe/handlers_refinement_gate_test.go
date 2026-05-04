package recipe

import (
	"path/filepath"
	"strings"
	"testing"
)

// Run-23 F-26 — refinement-mandatory gates. The previous always-on
// auto-advance only moved phase state; main-agent dispatch was driven
// by the agent's own decision and silently skipped on 3 of 5 recent
// runs. Two engine-side gates close the gap:
//   - complete-phase phase=finalize refuses unless RefinementDispatched
//   - export gate (separate process) refuses unless RefinementClosed

// TestCompletePhaseFinalize_RefusesWithoutRefinementDispatch — F-26.
func TestCompletePhaseFinalize_RefusesWithoutRefinementDispatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", log, dir, nil)

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
	// must refuse with an actionable error.
	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseFinalize)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if r.OK {
		t.Fatalf("expected refusal when refinement not dispatched; got OK=true")
	}
	if !strings.Contains(r.Error, "build-subagent-prompt briefKind=refinement") {
		t.Errorf("error should name the recovery action; got %q", r.Error)
	}
}

// TestBuildSubagentPrompt_RefinementBrief_FlipsDispatchedFlag — F-26.
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

// TestCompletePhaseFinalize_OkWhenRefinementDispatched — F-26 happy path.
func TestCompletePhaseFinalize_OkWhenRefinementDispatched(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", log, dir, nil)

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

// TestCompletePhaseRefinement_FlipsClosedFlagAndWritesMarker — F-26.
// Closure of refinement flips RefinementClosed AND writes the
// .refinement-closed marker on disk so the export gate (separate
// process) can read the closure signal.
func TestCompletePhaseRefinement_FlipsClosedFlagAndWritesMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", log, dir, nil)

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
	// Reset PhaseRefinement → not yet completed so close fires the
	// flip path below.
	sess.Completed[PhaseRefinement] = false
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
