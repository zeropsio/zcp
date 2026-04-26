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

// TestContentAuthoring_IGScopeClarifiesShowcaseMultiplier pins E.3:
// the §I sweet-spot rule mentions the showcase scope multiplier
// (7-10 items per codebase when the recipe ships 5 managed-service
// categories). Closes R-13-11's "9 items > 7 ceiling" false alarm.
func TestContentAuthoring_IGScopeClarifiesShowcaseMultiplier(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/scaffold/content_authoring.md")
	if err != nil {
		t.Fatalf("readAtom: %v", err)
	}
	if !strings.Contains(body, "showcase recipes") && !strings.Contains(body, "showcase recipe") {
		t.Error("content_authoring IG-scope section missing showcase scope clarification")
	}
	if !strings.Contains(body, "7-10") {
		t.Error("content_authoring IG-scope section missing the 7-10 showcase multiplier")
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
