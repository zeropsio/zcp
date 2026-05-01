// Tests for the `stitch` subcommand. The multi-stitch idempotence
// assertion (S1) is the regression detector for the engine inline-
// yaml block-doubling bug fixed by E1.
package main

import (
	"testing"
)

// TestRunStitch_TwoRounds_PassesWhenIdempotent asserts the basic
// shape of the multi-stitch idempotence path: with a fixture that
// does NOT trigger the inline-yaml comment injection, the engine
// assemble + write loop is idempotent and runStitch -rounds=2
// returns nil. Pinned by run-20 prep S1.
func TestRunStitch_TwoRounds_PassesWhenIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := writeMinimalSimulationOpts(t, dir, false); err != nil {
		t.Fatalf("writeMinimalSimulation: %v", err)
	}
	if err := runStitch([]string{"-dir", dir, "-rounds", "2"}); err != nil {
		t.Fatalf("runStitch -rounds=2 (no yaml-comments fragment): %v", err)
	}
}

// TestRunStitch_SingleRound_BackCompat verifies a single-round stitch
// (rounds=1) still works for callers that don't want the byte-diff
// assertion. Default of -rounds is 2; callers opt out explicitly.
func TestRunStitch_SingleRound_BackCompat(t *testing.T) {
	dir := t.TempDir()
	if err := writeMinimalSimulationOpts(t, dir, false); err != nil {
		t.Fatalf("writeMinimalSimulation: %v", err)
	}
	if err := runStitch([]string{"-dir", dir, "-rounds", "1"}); err != nil {
		t.Fatalf("runStitch -rounds=1: %v", err)
	}
}

// TestRunStitch_TwoRounds_WithYamlCommentFragments_StillIdempotent is
// the post-E1 closure. With a `zerops-yaml-comments/...` fragment
// present, pre-E1 the engine's `injectZeropsYamlComments` re-stamped
// without stripping; round 2 then read the on-disk yaml that round 1's
// WriteCodebaseYAMLWithComments stamped, re-injected the same blocks,
// and the codebase README's IG #1 inline yaml accumulated duplicate
// `# #` blocks (run-19 inline-yaml block-doubling regression).
//
// With E1's strip-then-inject in `assemble.go:144` the assembler is
// idempotent; runStitch -rounds=2 finds round 2 byte-equal to round 1.
// This test is the regression-detection mechanism — should it ever
// fire, the engine has lost the strip and the run-19 regression has
// returned. Pinned by run-20 prep S1 + E1.
func TestRunStitch_TwoRounds_WithYamlCommentFragments_StillIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := writeMinimalSimulationOpts(t, dir, true); err != nil {
		t.Fatalf("writeMinimalSimulation: %v", err)
	}
	if err := runStitch([]string{"-dir", dir, "-rounds", "2"}); err != nil {
		t.Fatalf("runStitch -rounds=2 with yaml-comments fragment: %v\n(if the failure names \"multi-stitch idempotence violated\", the engine strip in assemble.go:144 has been removed; restore E1)", err)
	}
}
