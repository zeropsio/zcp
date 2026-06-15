package recipe

import (
	"path/filepath"
	"strings"
	"testing"
)

// Run-41 — gate tests for the refinement-2 dispatch + close path.
//
// 1. `build-subagent-prompt briefKind=refinement2` flips
//    Refinement2Dispatched on a successful return.
// 2. `complete-phase phase=refinement` refuses (Error set, OK=false)
//    when refinement-1 dispatched but refinement-2 didn't.
// 3. After refinement-2 also dispatches, `complete-phase
//    phase=refinement` closes ok:true.

// TestBuildSubagentPromptRefinement2_FlipsDispatchFlag — the
// flipDispatchFlags helper (run-41 extracted) sets
// Refinement2Dispatched=true after a successful brief build for
// kind=refinement2. The flag is what the refinement-close gate
// reads.
func TestBuildSubagentPromptRefinement2_FlipsDispatchFlag(t *testing.T) {
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
	// Walk to PhaseRefinement so the dispatch resolves a current-phase
	// brief composer correctly.
	for _, p := range []Phase{
		PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature,
		PhaseCodebaseContent, PhaseEnvContent, PhaseFinalize, PhaseRefinement,
	} {
		if err := sess.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase(%s): %v", p, err)
		}
		sess.Completed[p] = true
	}
	sess.Completed[PhaseRefinement] = false
	sess.Current = PhaseRefinement

	if sess.Refinement2Dispatched {
		t.Fatal("Refinement2Dispatched should start false")
	}
	in := RecipeInput{Action: "build-subagent-prompt", BriefKind: "refinement2"}
	r := handleBuildSubagentPrompt(sess, in, RecipeResult{Action: "build-subagent-prompt"})
	if !r.OK {
		t.Fatalf("build-subagent-prompt briefKind=refinement2 failed: %q", r.Error)
	}
	if !sess.Refinement2Dispatched {
		t.Error("Refinement2Dispatched should flip to true after successful brief build")
	}
	// Run-41 dogfood — main agent triage contract MUST surface on the
	// response so the main agent sees it; the contract text in the
	// sub-agent brief is invisible to the main agent. Pin the
	// load-bearing phrases.
	for _, want := range []string{
		"MAIN AGENT",
		"refinement-2 triage contract",
		"ACT / HOLD / ACCEPT",
		"Bulk-HOLD",
		"`advisory` severity does NOT mean ignore",
	} {
		if !strings.Contains(r.Notice, want) {
			t.Errorf("refinement-2 dispatch Notice missing %q; got %q", want, r.Notice)
		}
	}
}

// TestRefinement2MainAgentTriageGuidance_OnlyFiresForRefinement2 —
// the guidance helper is scoped to BriefRefinement2; every other
// brief kind returns empty so its Notice channel is undisturbed.
func TestRefinement2MainAgentTriageGuidance_OnlyFiresForRefinement2(t *testing.T) {
	t.Parallel()
	if got := refinement2MainAgentTriageGuidance(BriefRefinement2); got == "" {
		t.Error("refinement2MainAgentTriageGuidance(BriefRefinement2) returned empty; expected triage contract text")
	}
	for _, kind := range []BriefKind{
		BriefScaffold, BriefFeature, BriefCodebaseContent, BriefClaudeMDAuthor,
		BriefEnvContent, BriefFinalize, BriefRefinement,
	} {
		if got := refinement2MainAgentTriageGuidance(kind); got != "" {
			t.Errorf("refinement2MainAgentTriageGuidance(%v) = %q; want empty for non-refinement2 kinds", kind, got)
		}
	}
}

// TestCompletePhaseRefinement_RefusesWithoutRefinement2 — gate
// refuses the refinement-phase close when refinement-1 has been
// dispatched but refinement-2 has NOT. Error message names the
// missing dispatch + cites cross-surface audit purpose.
func TestCompletePhaseRefinement_RefusesWithoutRefinement2(t *testing.T) {
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
	// Refinement2Dispatched intentionally left false.
	sess.Completed[PhaseRefinement] = false
	sess.Current = PhaseRefinement

	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseRefinement)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if r.OK {
		t.Fatal("expected ok=false when refinement-2 not dispatched")
	}
	if r.Error == "" {
		t.Fatal("expected Error message naming the missing refinement-2 dispatch")
	}
	// Sanity: error names the missing brief kind so the agent can act.
	for _, want := range []string{
		"briefKind=refinement2",
		"cross-surface audit",
	} {
		if !strings.Contains(r.Error, want) {
			t.Errorf("Error message missing substring %q; got %q", want, r.Error)
		}
	}
}

// TestCompletePhaseRefinement_RefusesWithoutRefinement1 — the gate
// also refuses when refinement-1 has not been dispatched (the
// pre-existing F-26 gate still holds; refinement-2 doesn't bypass
// it). Run-41 — both flags must be set in order.
func TestCompletePhaseRefinement_RefusesWithoutRefinement1(t *testing.T) {
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
	// Neither flag set.
	sess.Completed[PhaseRefinement] = false
	sess.Current = PhaseRefinement

	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseRefinement)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if r.OK {
		t.Fatal("expected ok=false when refinement-1 not dispatched")
	}
	if !strings.Contains(r.Error, "refinement-1") {
		t.Errorf("Error should name the missing refinement-1 dispatch; got %q", r.Error)
	}
}

// TestBuildSubagentPromptRefinement2_RefusesCodebaseScope — run-41
// guard against the misuse case where a confused caller passes
// `codebase=<host>` with briefKind=refinement2. Without the refusal,
// flipDispatchFlags would set Refinement2Dispatched=true even though
// the brief composer would have run a codebase-scoped audit that
// skipped the cross-codebase relationship checks the audit exists
// to run. Diagnosis surfaced in run-41 fresh-eyes review.
func TestBuildSubagentPromptRefinement2_RefusesCodebaseScope(t *testing.T) {
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
	sess.Completed[PhaseRefinement] = false
	sess.Current = PhaseRefinement

	in := RecipeInput{Action: "build-subagent-prompt", BriefKind: "refinement2", Codebase: "api"}
	r := handleBuildSubagentPrompt(sess, in, RecipeResult{Action: "build-subagent-prompt"})
	if r.OK {
		t.Fatal("expected ok=false with codebase= set; got OK=true")
	}
	if !strings.Contains(r.Error, "does not accept a codebase scope") {
		t.Errorf("Error should explain the refusal reason; got %q", r.Error)
	}
	if sess.Refinement2Dispatched {
		t.Error("Refinement2Dispatched must stay false after a refused codebase-scoped call")
	}
}
