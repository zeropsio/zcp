// Tests for S6 — complete-phase analog for codebase-content. After
// the replayed sub-agent writes fragments and `stitch` assembles the
// corpus, the sim driver invokes the codebase-content gate set
// against the staged plan + facts + materialized fragments. Refusals
// surface as sim failures, mirroring what the production engine does
// at `complete-phase=codebase-content`.
//
// Spec: docs/zcprecipator3/plans/run-20-prep.md §S6.
package main

import (
	"strings"
	"testing"
)

// TestRunStitch_GatesCodebaseContent_PassesOnCleanCorpus asserts the
// gate-running invocation does not fire spurious refusals against a
// minimal but well-formed corpus. Pinned by run-20 prep S6.
func TestRunStitch_GatesCodebaseContent_PassesOnCleanCorpus(t *testing.T) {
	dir := t.TempDir()
	if err := writeMinimalSimulationOpts(t, dir, false); err != nil {
		t.Fatalf("writeMinimalSimulationOpts: %v", err)
	}
	if err := runStitch([]string{
		"-dir", dir,
		"-rounds", "1",
		"-gates", "codebase-content",
	}); err != nil {
		t.Fatalf("runStitch with codebase-content gates: %v", err)
	}
}

// TestRunStitch_GatesUnknown_Errors asserts an unknown gate-set name
// returns a clear error rather than silently no-op'ing. Pinned by
// run-20 prep S6.
func TestRunStitch_GatesUnknown_Errors(t *testing.T) {
	dir := t.TempDir()
	if err := writeMinimalSimulationOpts(t, dir, false); err != nil {
		t.Fatalf("writeMinimalSimulationOpts: %v", err)
	}
	err := runStitch([]string{
		"-dir", dir,
		"-rounds", "1",
		"-gates", "made-up-phase",
	})
	if err == nil {
		t.Fatalf("expected error for unknown gate-set name")
	}
	if !strings.Contains(err.Error(), "made-up-phase") {
		t.Errorf("error %q does not name the unknown gate-set", err.Error())
	}
}

// TestRunStitch_GatesCodebaseContent_FiresRefusalOnBadFact asserts a
// fact missing required fields trips the default gateFactsValid rail
// (part of the codebase-content gate union) and runStitch returns a
// non-nil error naming the violation. Pinned by run-20 prep S6.
func TestRunStitch_GatesCodebaseContent_FiresRefusalOnBadFact(t *testing.T) {
	dir := t.TempDir()
	if err := writeMinimalSimulationOpts(t, dir, false); err != nil {
		t.Fatalf("writeMinimalSimulationOpts: %v", err)
	}
	// A field_rationale fact missing the required `fieldPath` field
	// fails FactRecord.Validate; gateFactsValid (DefaultGates) emits
	// a fact-invalid violation.
	mustWrite(t, simEnvFacts(dir),
		`{"kind":"field_rationale","topic":"deployFiles","scope":"api","why":"narrows what ships"}`+"\n")
	err := runStitch([]string{
		"-dir", dir,
		"-rounds", "1",
		"-gates", "codebase-content",
	})
	if err == nil {
		t.Fatalf("expected gate refusal for invalid fact; got nil error")
	}
	if !strings.Contains(err.Error(), "fact-invalid") && !strings.Contains(err.Error(), "field_rationale") {
		t.Errorf("error %q does not mention the expected violation", err.Error())
	}
}

// simEnvFacts is the path of the simulation's facts.jsonl, used by
// fact-injection tests above.
func simEnvFacts(dir string) string {
	return dir + "/environments/facts.jsonl"
}
