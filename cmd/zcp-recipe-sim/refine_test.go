// Tests for S5 — refinement prompt emit. Spec:
// docs/zcprecipator3/plans/run-20-prep.md §S5.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunEmitRefinement_ProducesPromptReferencingStitchedDir asserts
// that the refinement prompt is composed against the simulation
// directory: it contains a "## Stitched output to refine" pointer
// block (emitted by BuildRefinementBrief when runDir != "") that
// names the simulation root path. Pinned by run-20 prep S5.
func TestRunEmitRefinement_ProducesPromptReferencingStitchedDir(t *testing.T) {
	dir := t.TempDir()
	if err := writeMinimalSimulationOpts(t, dir, false); err != nil {
		t.Fatalf("writeMinimalSimulationOpts: %v", err)
	}
	// Stitch first so the simulation has a stitched corpus to
	// refine.
	if err := runStitch([]string{"-dir", dir, "-rounds", "1"}); err != nil {
		t.Fatalf("runStitch: %v", err)
	}
	if err := runEmitRefinement([]string{"-dir", dir}); err != nil {
		t.Fatalf("runEmitRefinement: %v", err)
	}

	promptPath := filepath.Join(dir, "briefs", "refinement-prompt.md")
	body, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read prompt: %v", err)
	}
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "Stitched output to refine") {
		t.Errorf("prompt missing 'Stitched output to refine' pointer block:\n%s", bodyStr)
	}
	// The composer emits per-codebase pointers — every codebase
	// in the plan should appear by hostname.
	if !strings.Contains(bodyStr, "/api/README.md") {
		t.Errorf("prompt missing per-codebase API pointer; got body:\n%s", bodyStr)
	}
	// Refinement footer is sim-flavored (no `complete-phase` MCP).
	if !strings.Contains(bodyStr, "Closing notes from the engine") {
		t.Errorf("prompt missing closing-notes footer")
	}
	// Fragments dir created.
	if _, err := os.Stat(filepath.Join(dir, "fragments-new", "refinement")); err != nil {
		t.Errorf("fragments-new/refinement/ not created: %v", err)
	}
}

// TestRunEmitRefinement_RequiresDirFlag asserts the CLI rejects an
// invocation without `-dir`. Pinned by run-20 prep S5.
func TestRunEmitRefinement_RequiresDirFlag(t *testing.T) {
	err := runEmitRefinement([]string{})
	if err == nil {
		t.Fatalf("expected error: -dir required")
	}
	if !strings.Contains(err.Error(), "-dir is required") {
		t.Errorf("error %q does not mention -dir", err.Error())
	}
}

// TestRunEmitRefinement_ReadsFactsLog asserts the refinement prompt
// composes against the run's facts log when present (matches
// production: the refinement composer reads facts to surface
// recorded contracts). Pinned by run-20 prep S5.
func TestRunEmitRefinement_ReadsFactsLog(t *testing.T) {
	dir := t.TempDir()
	if err := writeMinimalSimulationOpts(t, dir, false); err != nil {
		t.Fatalf("writeMinimalSimulationOpts: %v", err)
	}
	// Seed a real fact in facts.jsonl so the composer's facts read
	// path is exercised. Empty-file case is also valid (loadFactsJSONL
	// returns empty slice); this asserts non-empty doesn't error.
	factsPath := filepath.Join(dir, "environments", "facts.jsonl")
	mustWrite(t, factsPath, `{"kind":"contract","topic":"nats-subjects","scope":"app/api","why":"shared","fieldPath":"contracts.nats","recordedAt":"2026-04-30T00:00:00Z"}`+"\n")
	if err := runStitch([]string{"-dir", dir, "-rounds", "1"}); err != nil {
		t.Fatalf("runStitch: %v", err)
	}
	if err := runEmitRefinement([]string{"-dir", dir}); err != nil {
		t.Fatalf("runEmitRefinement: %v", err)
	}
	// Assertion is "didn't error" — the facts list itself appears in
	// the brief at composer-discretion (sub-agent B's territory).
}
