package recipe

import (
	"strings"
	"testing"
)

// phase_entry_refinement_f7_test.go — Run-43 F7 substrate pin.
//
// Forensics finding B-3 (plans/run-42-forensics.md): run-42
// dispatched a THIRD refinement-class sub-agent (refinement-rulewalk)
// AFTER finalize closed. The rulewalk sub-agent's exit text said
// "Engine will dispatch refinement2 next" — i.e. it didn't know
// refinement-2 had already run. The main agent re-dispatched
// refinement-1 by mistake.
//
// Edit D's commit message (a9dcf8dd) claimed it added a "Main-agent
// orchestration" preamble describing the five-step flow. Post-F7
// verification:
//   (1) Preamble exists; describes the five-step flow.
//   (2) Preamble does NOT EXPLICITLY say "dispatch refinement-1 +
//       refinement-2 each EXACTLY ONCE per recipe".
//   (3) Preamble does NOT acknowledge the engine state-machine —
//       no language about reading RefinementDispatched /
//       Refinement2Dispatched from the prior phase's status
//       response and skipping re-dispatch when the flags are
//       already set.
//
// F7 tightens the preamble to explicitly say: each sub-agent
// dispatches EXACTLY ONCE per recipe; check the status response's
// dispatch flags; do not re-dispatch when the flag is already set.

// TestPhaseEntryRefinement_GuidanceForbidsRedispatch — atom contains
// anti-redispatch language.
func TestPhaseEntryRefinement_GuidanceForbidsRedispatch(t *testing.T) {
	t.Parallel()
	body, err := readAtom("phase_entry/refinement.md")
	if err != nil {
		t.Fatalf("read phase_entry/refinement.md: %v", err)
	}
	for _, want := range []string{
		// Exact-once-per-recipe framing.
		"exactly once per recipe",
		// Engine state-machine acknowledgement — name the dispatch flags.
		"RefinementDispatched",
		"Refinement2Dispatched",
		// Anti-redispatch guidance — read the status response.
		"do NOT re-dispatch",
		// Run-42 evidence anchor (so the LLM has a concrete failure to
		// avoid reproducing).
		"run-42",
		// The check-before-dispatch contract.
		"status",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("phase_entry/refinement.md missing F7 anti-redispatch anchor %q", want)
		}
	}
}
