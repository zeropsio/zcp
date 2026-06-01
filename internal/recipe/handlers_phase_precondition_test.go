package recipe

import (
	"path/filepath"
	"strings"
	"testing"
)

// Run-52 Fix 2 (keystone) — build-subagent-prompt refuses when the
// session's current phase does not match the dispatched briefKind's
// owning phase, returning a text recovery hint that chains the agent
// into enter-phase first. Closes the run-51 dispatch-before-enter
// cascade (codebase-content workers dispatched while still at
// PhaseFeature minted fact shells into a phase never entered, and the
// citation validators — scoped to PhaseCodebaseContent — never ran).

// forcePhase drives a session to phase p without walking the gate-checked
// EnterPhase chain: it sets Current and marks every prior phase Completed
// (so downstream code that reads Completed sees a coherent history). Used
// by the run-52 Fix-2 fixture migration — many dispatch tests previously
// dispatched while still at PhaseResearch (the NewSession default) and
// would now refuse under the enter-phase precondition.
func forcePhase(sess *Session, p Phase) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.Completed == nil {
		sess.Completed = map[Phase]bool{}
	}
	target := phaseIndex(p)
	for _, q := range Phases() {
		if phaseIndex(q) < target {
			sess.Completed[q] = true
		}
	}
	sess.Current = p
}

// TestBuildSubagentPrompt_CodebaseContentInFeaturePhase_RefusesWithEnterPhaseRecovery
// is the run-52 Fix-2 RED test. Dispatching briefKind=codebase-content
// while the session is at PhaseFeature must refuse with a recovery hint
// naming both phases + the enter-phase next-call, and MUST NOT seed any
// engine-emitted fact shells (the refusal short-circuits before
// seedEngineEmittedFacts).
func TestBuildSubagentPrompt_CodebaseContentInFeaturePhase_RefusesWithEnterPhaseRecovery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.OutputRoot = dir
	forcePhase(sess, PhaseFeature)

	before, err := sess.FactsLog.Read()
	if err != nil {
		t.Fatalf("read facts before: %v", err)
	}

	in := RecipeInput{Action: "build-subagent-prompt", Slug: "synth-showcase", BriefKind: "codebase-content", Codebase: "api"}
	r := handleBuildSubagentPrompt(sess, in, RecipeResult{Action: "build-subagent-prompt"})

	if r.OK {
		t.Fatalf("expected OK=false when dispatching codebase-content at feature phase; got OK=true")
	}
	for _, want := range []string{"codebase-content", "feature", "enter-phase", "phase=codebase-content"} {
		if !strings.Contains(r.Error, want) {
			t.Errorf("recovery hint missing %q; got %q", want, r.Error)
		}
	}
	if r.Prompt != "" || r.BriefPath != "" {
		t.Errorf("refused dispatch must not produce a brief; Prompt=%dB BriefPath=%q", len(r.Prompt), r.BriefPath)
	}

	after, err := sess.FactsLog.Read()
	if err != nil {
		t.Fatalf("read facts after: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("refused dispatch seeded fact shells: before=%d after=%d (precondition must short-circuit before seedEngineEmittedFacts)", len(before), len(after))
	}
}

// TestBuildSubagentPrompt_CodebaseContentInPhase_Succeeds is the GREEN
// companion: with the session at PhaseCodebaseContent, the same
// codebase-content dispatch composes a brief normally.
func TestBuildSubagentPrompt_CodebaseContentInPhase_Succeeds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, sess.Plan)
	sess.OutputRoot = dir
	forcePhase(sess, PhaseCodebaseContent)

	in := RecipeInput{Action: "build-subagent-prompt", Slug: "synth-showcase", BriefKind: "codebase-content", Codebase: "api"}
	r := handleBuildSubagentPrompt(sess, in, RecipeResult{Action: "build-subagent-prompt"})
	if !r.OK {
		t.Fatalf("expected OK=true dispatching codebase-content in-phase; got Error=%q", r.Error)
	}
	if r.Prompt == "" && r.BriefPath == "" {
		t.Error("in-phase dispatch produced neither inline prompt nor brief path")
	}
}
