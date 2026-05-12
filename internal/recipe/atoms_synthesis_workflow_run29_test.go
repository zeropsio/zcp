package recipe

import (
	"strings"
	"testing"
)

// Run-29 Fix #4 — synthesis_workflow atom: surface ownership +
// authoring order (IG-mechanisms first, yaml-comment-WHY-choices
// second). Refinement suspects (ig-yamlcomment-dup) is the
// defense-in-depth detector tested in refinement_suspects_run29_test.go.

func TestSynthesisWorkflowAtom_AuthoringOrderSection_Present(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	for _, want := range []string{
		"## Surface ownership — mechanisms on IG, field-choices on yaml comments",
		"### Authoring order — IG first, yaml-comments second",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis_workflow.md missing surface-ownership anchor %q", want)
		}
	}
}

func TestSynthesisWorkflowAtom_NamesYAMLCommentsFirst(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	// The authoring-order rule names IG first (mechanisms), yaml-
	// comments second (WHY-choices), KB last (post-deploy symptoms).
	// Spec is explicit: IG-first, yaml-comments-second (per spec-
	// content-surfaces.md §Surface 7). This pin guards the order.
	for _, want := range []string{
		"**Author IG #2-N first.**",
		"**Author zerops.yaml comments second",
		"**Author KB last.**",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis_workflow.md missing authoring-order anchor %q", want)
		}
	}
}

func TestSynthesisWorkflowAtom_BadGoodExamples_BothPresent(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	idx := strings.Index(body, "### Worked example — same-key shadow trap (api codebase)")
	if idx < 0 {
		t.Fatal("worked-example heading missing")
	}
	rest := body[idx:]
	end := strings.Index(rest[1:], "\n## ")
	if end < 0 {
		end = len(rest) - 1
	}
	window := rest[:end+1]
	// Run-43 P1 — voice rule tightened. The worked example still
	// pairs a BAD (mechanism leaking onto Surface 7) with a GOOD
	// (yaml comment self-contained), but the GOOD shape no longer
	// closes with "see IG #N" deferral; voice is mechanism+reason in
	// one breath per spec §"Surface 7".
	for _, want := range []string{
		"**BAD**",
		"**GOOD**",
		"yaml comment teaches the mechanism",
		"in one breath",
	} {
		if !strings.Contains(window, want) {
			t.Errorf("synthesis_workflow.md surface-ownership worked example missing %q", want)
		}
	}
}

// Run-43 P1 supersedes the run-29 F-34 "cross-reference is not a
// license to restate" rule: the new voice rule forbids
// cross-references in yaml comments altogether (mechanism+reason in
// one breath per spec §"Surface 7"). The replacement pin asserts the
// "Yaml comments stand alone" section + the run-42
// cross-surface-reference defect-class anchor.
func TestSynthesisWorkflowAtom_YamlCommentsStandAlone_Present(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	for _, want := range []string{
		"### Yaml comments stand alone",
		"cross-surface-reference",
		"mechanism+reason in one breath",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis_workflow.md missing self-contained-voice anchor %q", want)
		}
	}
	// Old anchor must be GONE — the rule is superseded.
	if strings.Contains(body, "### Cross-reference is not a license to restate") {
		t.Error("legacy 'Cross-reference is not a license to restate' section must be removed; spec §Surface 7 voice now forbids cross-references entirely (run-43 P1)")
	}
}

func TestSynthesisWorkflowAtom_IG1EngineStampedNote_Present(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	for _, want := range []string{
		"### Special case — IG #1 is engine-stamped",
		"the engine emit\nfulfills the contract by construction",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis_workflow.md missing IG #1 engine-stamped special-case anchor %q", want)
		}
	}
}
