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
// that the refinement multi-file brief composed against the simulation
// directory carries the "## Stitched output to refine" pointer block
// (rendered into the references part by renderRefinementStitchedPointer
// Block when runDir != "") that names the simulation root path. Pinned
// by run-20 prep S5; updated for the multi-file shape (run-31 Fix #1
// closure) — production refinement persists index.md + N part-*.md to
// `<outputRoot>/.briefs/refinement-phase/` and the stitched-output
// pointer lives in part-4-references.md.
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

	// Multi-file brief lives at <dir>/.briefs/refinement-phase/.
	briefDir := filepath.Join(dir, ".briefs", "refinement-phase")
	indexPath := filepath.Join(briefDir, "index.md")
	indexBody, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	indexStr := string(indexBody)
	// Index renders the recipe-level header at the top + Read order
	// + closing notes at the bottom. Every part must appear by
	// absolute path so the agent can Read it.
	if !strings.Contains(indexStr, "Read order") {
		t.Errorf("index missing 'Read order' section:\n%s", indexStr)
	}
	if !strings.Contains(indexStr, "Closing notes from the engine") {
		t.Errorf("index missing closing-notes footer (sim-flavored)")
	}

	// The stitched-output pointer block lives in the references part
	// (part-4 by current composer ordering). Walk every part-*.md and
	// look for the pointer block + the per-codebase /api/README.md
	// pointer.
	parts, err := os.ReadDir(briefDir)
	if err != nil {
		t.Fatalf("read brief dir: %v", err)
	}
	var sawStitchedHeader, sawAPIPointer bool
	for _, e := range parts {
		if e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), "part-") {
			continue
		}
		body, rErr := os.ReadFile(filepath.Join(briefDir, e.Name()))
		if rErr != nil {
			t.Fatalf("read %s: %v", e.Name(), rErr)
		}
		s := string(body)
		if strings.Contains(s, "Stitched output to refine") {
			sawStitchedHeader = true
		}
		if strings.Contains(s, "/api/README.md") {
			sawAPIPointer = true
		}
	}
	if !sawStitchedHeader {
		t.Errorf("no part file carries 'Stitched output to refine' pointer block")
	}
	if !sawAPIPointer {
		t.Errorf("no part file carries per-codebase /api/README.md pointer")
	}
	// Fragments dir created.
	if _, err := os.Stat(filepath.Join(dir, "fragments-new", "refinement")); err != nil {
		t.Errorf("fragments-new/refinement/ not created: %v", err)
	}
	// Legacy briefs/refinement-prompt.md is now a thin dispatch pointer
	// — must exist (back-compat for the dispatch ergonomics) and must
	// reference the index path.
	pointerBody, err := os.ReadFile(filepath.Join(dir, "briefs", "refinement-prompt.md"))
	if err != nil {
		t.Fatalf("read dispatch pointer: %v", err)
	}
	if !strings.Contains(string(pointerBody), indexPath) {
		t.Errorf("dispatch pointer missing index path %s; got:\n%s", indexPath, string(pointerBody))
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

// TestRunEmitRefinement_MatchesProductionMultiFileShape asserts the
// sim refinement emits the same multi-file shape production does:
// `<outputRoot>/.briefs/refinement-phase/index.md` plus N part-*.md
// part files. Closes the SIM DIVERGENCE pinned in run-31 review
// (replaces TestRunEmitRefinement_DivergesFromProductionMultiFileShape
// — the divergence is now closed by routing through the public
// recipe.BuildRefinementBriefMultiFile entry point). The brief-edits-
// land-identically-in-sim-and-prod contract at
// internal/authoring/recipe/briefs_subagent_prompt.go:32-38 holds because both
// callers route through the same multi-file composer.
func TestRunEmitRefinement_MatchesProductionMultiFileShape(t *testing.T) {
	dir := t.TempDir()
	if err := writeMinimalSimulationOpts(t, dir, false); err != nil {
		t.Fatalf("writeMinimalSimulationOpts: %v", err)
	}
	if err := runStitch([]string{"-dir", dir, "-rounds", "1"}); err != nil {
		t.Fatalf("runStitch: %v", err)
	}
	if err := runEmitRefinement([]string{"-dir", dir}); err != nil {
		t.Fatalf("runEmitRefinement: %v", err)
	}

	// Multi-file brief dir lives under <dir>/.briefs/refinement-phase/.
	// Composer's Persist names the dir <kind>-<codebase>; refinement
	// passes "" for codebase and Persist substitutes "phase".
	briefDir := filepath.Join(dir, ".briefs", "refinement-phase")
	indexPath := filepath.Join(briefDir, "index.md")
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("multi-file index.md missing at %s: %v", indexPath, err)
	}

	// At least one part-*.md must exist alongside index.md (the current
	// composer split lands ≥4 parts: phase-entry, synthesis, rubric,
	// references, optionally context, optionally facts).
	entries, err := os.ReadDir(briefDir)
	if err != nil {
		t.Fatalf("read brief dir: %v", err)
	}
	var parts []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "part-") {
			continue
		}
		parts = append(parts, e.Name())
	}
	if len(parts) < 2 {
		t.Fatalf("expected ≥2 part-*.md files alongside index.md; got %v", parts)
	}

	// Index must reference every part by absolute path in its Read order
	// section — the agent walks the index, then Reads each path.
	indexBody, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	indexStr := string(indexBody)
	if !strings.Contains(indexStr, "Read order") {
		t.Errorf("index missing 'Read order' section")
	}
	for _, p := range parts {
		fullPath := filepath.Join(briefDir, p)
		if !strings.Contains(indexStr, fullPath) {
			t.Errorf("index does not reference part path %s", fullPath)
		}
	}

	// Dispatch pointer at briefs/refinement-prompt.md exists and points
	// at the multi-file index — preserves the sim's single-entry-point
	// dispatch ergonomics across the multi-file transition.
	pointerBody, err := os.ReadFile(filepath.Join(dir, "briefs", "refinement-prompt.md"))
	if err != nil {
		t.Fatalf("read dispatch pointer: %v", err)
	}
	if !strings.Contains(string(pointerBody), indexPath) {
		t.Errorf("dispatch pointer does not reference index %s; got:\n%s", indexPath, string(pointerBody))
	}
	// Replay-adapter (record-fragment → Write file substitution) must
	// land in the dispatch pointer so the agent sees it before walking
	// the index. Substring check on the section header.
	if !strings.Contains(string(pointerBody), "record-fragment → write file") {
		t.Errorf("dispatch pointer missing replay-adapter section; got:\n%s", string(pointerBody))
	}
}
