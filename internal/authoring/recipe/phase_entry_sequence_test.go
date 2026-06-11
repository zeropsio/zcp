// Tests for run-52 Fix 1 — phase-walk guidance must sequence
// enter-phase before any build-subagent-prompt dispatch. completePhase
// returns the NEXT phase's entry atom as guidance, but only PhaseFinalize
// auto-advances; every other transition leaves sess.Current on the old
// phase. So a phase-entry atom whose first next-call is a dispatch (with
// no preceding enter-phase) teaches the agent to dispatch into a
// not-yet-entered phase — the run-51 cascade (now hard-refused by Fix 2,
// but the guidance must still lead the agent through the correct
// sequence rather than into a refusal).

package recipe

import (
	"strings"
	"testing"
)

// TestPhaseAtoms_SequenceEnterPhaseBeforeDispatch asserts that each
// phase-entry atom that dispatches sub-agents names
// `enter-phase ... phase=<self>` BEFORE its first `build-subagent-prompt`
// token.
func TestPhaseAtoms_SequenceEnterPhaseBeforeDispatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		phase Phase
		self  string
	}{
		{PhaseScaffold, "scaffold"},
		{PhaseCodebaseContent, "codebase-content"},
		{PhaseEnvContent, "env-content"},
	}
	for _, tc := range cases {
		t.Run(tc.self, func(t *testing.T) {
			t.Parallel()
			body := loadPhaseEntry(tc.phase)
			if body == "" {
				t.Fatalf("phase-entry atom for %s unavailable", tc.phase)
			}
			enterToken := "enter-phase slug=<slug> phase=" + tc.self
			enterIdx := strings.Index(body, enterToken)
			if enterIdx < 0 {
				t.Fatalf("phase-entry %s missing self enter-phase token %q (must sequence enter-phase before dispatch)", tc.self, enterToken)
			}
			dispatchIdx := strings.Index(body, "build-subagent-prompt")
			if dispatchIdx < 0 {
				t.Fatalf("phase-entry %s has no build-subagent-prompt token", tc.self)
			}
			if enterIdx >= dispatchIdx {
				t.Errorf("phase-entry %s sequences dispatch (idx %d) before self enter-phase (idx %d); enter-phase must come first", tc.self, dispatchIdx, enterIdx)
			}
		})
	}
}
