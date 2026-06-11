// Tests for: Cluster E (run-14 §2.E) — content-discipline tighteners.
// Reframes the porter-audience rule as a positive shape (E.1, R-13-5),
// clarifies the IG-scope sweet-spot for showcase recipes (E.3,
// R-13-11), and retires verify-subagent-dispatch from phase-entry
// prescriptions now that build-subagent-prompt makes the verify call
// a no-op (E.2, R-13-8).

package recipe

import (
	"strings"
	"testing"
)

// TestContentAuthoring_TeachesPorterAudienceRule pins E.1: the
// CLAUDE.md authoring section opens with the unconditional
// porter-audience rule. The rule applies across the whole body, not
// only inside the "Dev loop" slot. Closes the run-13 R-13-5 slip
// where agent-authored notes carried `zcli vpn` despite the
// dev-loop content honoring the catalog ban.
func TestContentAuthoring_TeachesPorterAudienceRule(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/scaffold/content_authoring.md")
	if err != nil {
		t.Fatalf("readAtom: %v", err)
	}
	if !strings.Contains(body, "CLAUDE.md is for the porter") {
		t.Error("content_authoring missing 'CLAUDE.md is for the porter' header")
	}
	if !strings.Contains(body, "framework-canonical commands") {
		t.Error("content_authoring missing the framework-canonical-commands rule reach")
	}
}

// TestContentAuthoring_IGCapIsUniform pins run-15 F.5: the IG cap is
// 4-5 items per codebase including engine-emitted IG #1, regardless
// of tier. Showcase recipes do NOT get a higher cap — scope adds
// breadth via more codebases, not more IG items per codebase. This
// reverses the run-14 §E.3 7-10 multiplier (R-13-11) once content-
// quality data showed both reference recipes converging at 4-5.
func TestContentAuthoring_IGCapIsUniform(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/scaffold/content_authoring.md")
	if err != nil {
		t.Fatalf("readAtom: %v", err)
	}
	if !strings.Contains(body, "4-5 items per codebase") {
		t.Error("content_authoring IG-scope section missing the 4-5 uniform cap rule")
	}
	if !strings.Contains(body, "Showcase recipes do not get a higher cap") {
		t.Error("content_authoring IG-scope section missing the no-higher-cap-for-showcase clarification")
	}
	// Negative: the retired 7-10 multiplier must NOT be teaching the agent
	// a higher cap any more.
	if strings.Contains(body, "7-10 items per codebase") {
		t.Error("content_authoring IG-scope still teaches the run-14 7-10 multiplier; F.5 retired it")
	}
}

// TestPhaseEntry_ScaffoldDoesNotPrescribeVerifyDispatch pins E.2:
// phase-entry scaffold no longer prescribes verify-subagent-dispatch.
// build-subagent-prompt's response is dispatched byte-identical;
// there is nothing to verify against (the prompt IS the engine
// output). Run-13 had zero verify calls — the action stays in the
// engine for explicit recovery but the prescribed flow doesn't
// mention it.
func TestPhaseEntry_ScaffoldDoesNotPrescribeVerifyDispatch(t *testing.T) {
	t.Parallel()
	body := loadPhaseEntry(PhaseScaffold)
	if strings.Contains(body, "verify-subagent-dispatch") {
		t.Error("scaffold phase-entry still prescribes verify-subagent-dispatch — engine-composed prompt makes it a no-op")
	}
	if !strings.Contains(body, "build-subagent-prompt") {
		t.Error("scaffold phase-entry should still prescribe build-subagent-prompt as the dispatch composer")
	}
}
