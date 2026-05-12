package recipe

import (
	"strings"
	"testing"
)

// briefs_classification_f1_test.go — Run-43 F1 substrate pin.
//
// Forensics found that every refinement-driven `record-fragment` on
// CODEBASE_KB / CODEBASE_IG surfaces was engine-rejected with
// "classification is required for fragments on surface ..." then
// retried with a guessed value. Two wasted record-fragment calls per
// ambiguous-class fragment + a slow re-read cycle. Recurring across
// run-40 + run-42 main session AND run-42 refinement-pass-1
// sub-session.
//
// Evidence:
//   - Run-40: docs/zcprecipator3/runs/40/SESSION_LOGS/main-session.jsonl:298,303
//   - Run-42 main: docs/zcprecipator3/runs/42/SESSION_LOGS/main-session.jsonl:249-256
//   - Run-42 refinement-pass-1: docs/zcprecipator3/runs/42/SESSION_LOGS/subagents/agent-a5668cb6fec32cadf.jsonl:128-142
//
// Fix: refinement-1 + refinement-2 dispatch briefs MUST instruct the
// agent's record-fragment ACTs to pass `classification: <one of seven
// values>` per docs/spec-content-surfaces.md §"Fact classification
// taxonomy". The seven valid values are: platform-invariant,
// intersection, framework-quirk, library-metadata, scaffold-decision,
// operational, self-inflicted.

// TestBuildRefinementBrief_CarriesClassificationInstructions — pins
// that refinement-1's brief body names the classification field, the
// seven enum values, and cites the spec section.
func TestBuildRefinementBrief_CarriesClassificationInstructions(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinementBrief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	// Field name + the spec anchor.
	for _, want := range []string{
		"classification",
		"record-fragment",
		"Fact classification taxonomy",
		"spec-content-surfaces.md",
		// Seven enum values per spec §"Fact classification taxonomy".
		"platform-invariant",
		"intersection",
		"framework-quirk",
		"library-metadata",
		"scaffold-decision",
		"operational",
		"self-inflicted",
		// Worked example — CODEBASE_KB getting an intersection class.
		"CODEBASE_KB",
		// The forensics-flagged failure mode + recovery.
		"classification is required",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("refinement-1 brief missing classification anchor %q", want)
		}
	}
}

// TestBuildRefinement2Brief_CarriesClassificationInstructions —
// refinement-2 triage contract MUST also name the classification
// requirement: when the main agent ACTs on a finding via
// record-fragment, the classification field is required for
// CODEBASE_KB / CODEBASE_IG surfaces.
func TestBuildRefinement2Brief_CarriesClassificationInstructions(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	for _, want := range []string{
		"classification",
		"Fact classification taxonomy",
		"spec-content-surfaces.md",
		// Seven enum values.
		"platform-invariant",
		"intersection",
		"framework-quirk",
		"library-metadata",
		"scaffold-decision",
		"operational",
		"self-inflicted",
		// Triage-time worked example anchor — main-agent ACT path.
		"CODEBASE_KB",
		"classification is required",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("refinement-2 brief missing classification anchor %q", want)
		}
	}
}
