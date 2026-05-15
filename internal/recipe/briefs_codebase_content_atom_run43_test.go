package recipe

import (
	"strings"
	"testing"
)

// briefs_codebase_content_atom_run43_test.go — Run-43 P1 substrate pin,
// updated for Run-48 Surface 5 recalibration.
//
// Spec citation: docs/spec-content-surfaces.md §"Surface 5 —
// Per-codebase README: Knowledge Base & Gotchas fragment" (dual-shape
// + self-inflicted-reversible) + §"Surface 7 — Per-codebase
// `zerops.yaml` comments" (anti-cross-reference voice rule) +
// §"Counter-examples — Self-inflicted-reversible".
//
// Run-42 dogfood exposed three contradictions in
// `briefs/codebase-content/synthesis_workflow.md` (yaml cross-ref
// voice, permissive KB discriminator, KEEP-vs-DISCARD mis-classifying
// X-Cache). The Run-48 audit replaced the run-42 KB discriminator
// entirely with the **self-inflicted-reversible litmus**: refuse KB
// content whose symptom fires only when the porter UNDOES a recipe-
// shipped directive. The new brief teaches dual-shape (forward-looking
// H3 OR `### Gotchas` bullets), permits empty KB, and routes the
// run-48 audit's 5 named cases to CLAUDE.md / yaml comments.

// TestSynthesisWorkflow_P1_YamlCommentVoiceIsSelfContained — pin
// the authoring-order step #2 edit. yaml comments must state
// mechanism+reason in one breath; the "see IG #N" cross-surface
// reference framing is dropped.
func TestSynthesisWorkflow_P1_YamlCommentVoiceIsSelfContained(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	// Positive shape — self-contained mechanism+reason in one breath.
	for _, want := range []string{
		"self-contained",
		"yaml comment must stand on its own",
		"State mechanism + reason in one",
		"Mechanism (alias rename) +",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis_workflow.md missing self-contained-voice anchor %q", want)
		}
	}
	// Negative shape — the old permissive language must be gone from
	// the authoring-order section. The atom retains a separate "BAD"
	// example block illustrating an over-reaching yaml comment with a
	// trailing IG reference (kept as a counter-example), so we scope
	// the negative assertion to the authoring-order step #2 prose.
	idx := strings.Index(body, "Author zerops.yaml comments second")
	if idx < 0 {
		t.Fatal("authoring-order step #2 heading missing")
	}
	end := strings.Index(body[idx:], "3. **Author KB last")
	if end < 0 {
		t.Fatal("authoring-order step #3 boundary missing")
	}
	window := body[idx : idx+end]
	if strings.Contains(window, "may cross-reference IG") {
		t.Error("authoring-order step #2 still blesses cross-reference; voice must be self-contained")
	}
}

// TestSynthesisWorkflow_KBSelfInflictedReversibleLitmus — Run-48
// recalibration. The brief teaches the self-inflicted-reversible
// litmus: refuse KB content whose symptom fires only when the porter
// UNDOES a directive the recipe ships. Anchors are positive (presence)
// + negative (the old permissive single-anchor-flip rule is gone).
func TestSynthesisWorkflow_KBSelfInflictedReversibleLitmus(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	for _, want := range []string{
		"Self-inflicted-reversible litmus",
		"UNDOES a directive",
		"null reader",
		"kb-self-inflicted-reversible",
		"ignoreEnvFile",
		"exposedHeaders",
		"`base: static` without `start:`",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis_workflow.md missing self-inflicted-reversible litmus anchor %q", want)
		}
	}
	// Negative — the run-42 permissive rule must be gone.
	if strings.Contains(body, "A single anchor anywhere flips to KB-eligible") {
		t.Error("KB discriminator still carries the permissive single-anchor-flips rule")
	}
}

// TestSynthesisWorkflow_KBDualShape — Run-48 recalibration. The brief
// must teach BOTH valid Surface 5 shapes: forward-looking H3
// operational sections AND symptom-first `### Gotchas` bullets, with
// "pick by content, not by template" guidance.
func TestSynthesisWorkflow_KBDualShape(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	for _, want := range []string{
		"two valid shapes",
		"Forward-looking H3 operational sections",
		"jetstream-shape",
		"### Gotchas",
		"symptom-first",
		"Pick the shape that fits the content",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis_workflow.md missing dual-shape anchor %q", want)
		}
	}
}

// TestSynthesisWorkflow_KBMayBeEmpty — Run-48 recalibration permits
// an empty KB fragment when the IG / yaml comments / CLAUDE.md cover
// everything the porter needs. The brief must teach this as a
// positive outcome (not a defect) and warn against padding.
func TestSynthesisWorkflow_KBMayBeEmpty(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	for _, want := range []string{
		"KB may be empty",
		"positive signal",
		"Don't pad",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis_workflow.md missing empty-KB-permitted anchor %q", want)
		}
	}
}

// TestSynthesisWorkflow_KBPluralAudience — Run-48 recalibration
// teaches KB serves BOTH evaluation and search audiences. The brief
// must call out the plural-audience framing and explicitly name the
// "null reader" (a porter who broke the recipe by following it).
func TestSynthesisWorkflow_KBPluralAudience(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	for _, want := range []string{
		"EVALUATING how to operate",
		"arriving via SEARCH",
		"null reader",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis_workflow.md missing plural-audience anchor %q", want)
		}
	}
}

// TestSynthesisWorkflow_P1_InitCommandsLifetimeDistinction — pin the
// 8.5 anchor in-body completion example uses execOnce key shapes
// consistent with `principles/init-commands-model.md`. The earlier
// shape (`${appVersionId}-migrate` / `${appVersionId}-seed` both
// claimed as "skips a key whose value has already run") is wrong —
// `${appVersionId}` keys re-run every deploy because the resolved
// value changes; static keys (e.g. `bootstrap-seed`) are the
// once-ever shape. Atom must teach both shapes.
func TestSynthesisWorkflow_P1_InitCommandsLifetimeDistinction(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	idx := strings.Index(body, "**8.5 anchor — in-body completion**")
	if idx < 0 {
		t.Fatal("8.5 anchor heading missing")
	}
	end := strings.Index(body[idx:], "**8.5 anchor — descriptive-labeled link variant**")
	if end < 0 {
		t.Fatal("descriptive-labeled link variant boundary missing")
	}
	window := body[idx : idx+end]
	for _, want := range []string{
		"per-deploy gate",
		"once per service lifetime",
		"bootstrap-seed",
		"static key",
		"Match execOnce key shape to lifetime",
	} {
		if !strings.Contains(window, want) {
			t.Errorf("8.5 anchor missing per-deploy-vs-once-ever distinction anchor %q", want)
		}
	}
	// Cross-link to the principle atom must land so an agent reading
	// the brief can navigate to the per-lifetime teaching.
	if !strings.Contains(body, "principles/init-commands-model.md") {
		t.Error("synthesis_workflow.md missing cross-link to principles/init-commands-model.md")
	}
}
