package recipe

import (
	"strings"
	"testing"
)

// briefs_codebase_content_atom_run28_test.go — Run-28 fix #6 atom
// assertions.
//
// Pins the synthesis_workflow caveat that the friendly-authority
// voice template ("Feel free to swap", "Configure this to use") may
// NOT be applied to alternatives the scaffold/feature phase recorded
// as broken. Closes the run-27 workerdev case where KB + yaml-comments
// endorsed Pattern B as a swap-in even though scaffold facts said it
// crashes at boot.

// TestCodebaseContentSynthesis_VoiceScopeCaveat_Present — Run-28
// fix #6. The synthesis_workflow atom must caveat the friendly-
// authority voice template — agents must NOT apply "Feel free to
// swap" phrasing to alternatives the scaffold/feature phase
// recorded as broken.
func TestCodebaseContentSynthesis_VoiceScopeCaveat_Present(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	for _, want := range []string{
		"never on broken alternatives",
		"Friendly-authority",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis_workflow.md missing voice-scope caveat anchor %q", want)
		}
	}
}

// TestCodebaseContentSynthesis_BadGoodExamples_BothPresent — Run-28
// fix #6. The voice-scope section must show a BAD/GOOD pair so the
// agent has a verbatim shape to compare against.
func TestCodebaseContentSynthesis_BadGoodExamples_BothPresent(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	idx := strings.Index(body, "never on broken alternatives")
	if idx < 0 {
		t.Fatal("voice-scope section heading missing")
	}
	window := body[idx:]
	for _, want := range []string{
		"# BAD",
		"# GOOD",
	} {
		if !strings.Contains(window, want) {
			t.Errorf("voice-scope section missing %q in BAD/GOOD pair", want)
		}
	}
}
