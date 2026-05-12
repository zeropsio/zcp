package recipe

import (
	"strings"
	"testing"
)

// briefs_codebase_content_atom_run43_test.go — Run-43 P1 substrate pin.
//
// Spec citation: docs/spec-content-surfaces.md §"Fact classification
// taxonomy" → litmus #4 (Self-inflicted) + §"Self-inflicted (should
// have been discarded)" + §"Surface 7 — Per-codebase `zerops.yaml`
// comments" (anti-cross-reference voice rule).
//
// Run-42 dogfood ([plans/run-42-validation.md "Recommended next
// action" §"Substrate priority 1"]) exposed three contradictions
// inside `briefs/codebase-content/synthesis_workflow.md`:
//
//  1. The authoring-order step #2 (yaml-comment authoring) blessed
//     "see IG #N" cross-surface deferrals — voice the goldens never
//     use and the run-42 refinement-2 audit's `cross-surface-reference`
//     class flags.
//  2. The KB discriminator's tail rule said "a single anchor anywhere
//     flips to KB-eligible" — permissive enough to pass the run-42
//     self-inflicted bullets (apidev KB #2 `UnknownError`, apidev
//     KB #3 `X-Cache returns null`) because each had a shallow
//     Zerops-anchor pass (object-storage gateway / project-scope
//     subdomains).
//  3. The KEEP example block listed `fetch().headers.get('X-Cache')
//     returns null` as an intersection KB-eligible bullet — exactly
//     the run-42 self-inflicted #3 the spec routes to DISCARD.
//
// And separately the in-body completion 8.5 anchor used
// `${appVersionId}-seed` as a "stamps each key into a per-deploy
// ledger and skips it if the key already ran" example, which
// contradicts `internal/recipe/content/principles/init-commands-model.md`
// (`${appVersionId}` resolves to a fresh value every deploy → re-runs
// every deploy; `seed` shape is once-ever and needs a static key).

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

// TestSynthesisWorkflow_P1_KBDiscriminatorAddsSelfInflictedLitmus —
// pin the litmus #4 self-inflicted test in the KB-classification
// section. The tail rule "a single anchor anywhere flips to KB-
// eligible" is replaced with the porter-following-IG#1-verbatim test.
func TestSynthesisWorkflow_P1_KBDiscriminatorAddsSelfInflictedLitmus(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	for _, want := range []string{
		"Self-inflicted litmus test",
		"copying IG #1's shipped",
		"deviates from the shipped config",
		"discard as `self-inflicted`",
		"Fact classification taxonomy",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis_workflow.md missing self-inflicted-litmus anchor %q", want)
		}
	}
	// Negative — the permissive "single anchor flips to KB-eligible"
	// must be gone.
	if strings.Contains(body, "A single anchor anywhere flips to KB-eligible") {
		t.Error("KB discriminator still carries the permissive single-anchor-flips rule")
	}
}

// TestSynthesisWorkflow_P1_DiscardExampleStorageEndpoint — pin the
// worked DISCARD example for the storage_apiHost UnknownError pattern
// from run-42 (apidev KB #2). A porter following IG #1's shipped
// `S3_ENDPOINT: ${storage_apiUrl}` never composes the broken
// `http://${storage_apiHost}` URL → self-inflicted → DISCARD.
func TestSynthesisWorkflow_P1_DiscardExampleStorageEndpoint(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	for _, want := range []string{
		"**DISCARD — `self-inflicted`**",
		"`UnknownError` on first `GetObject`",
		"http://${storage_apiHost}",
		"${storage_apiUrl}",
		"Run-42 dogfood: apidev KB #2",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis_workflow.md missing storage_apiHost DISCARD worked-example anchor %q", want)
		}
	}
}

// TestSynthesisWorkflow_P1_XCacheRemovedFromKeepList — the
// `fetch().headers.get('X-Cache') returns null` example was on the
// KEEP list; spec §"Self-inflicted" routes it to DISCARD because a
// porter following IG #1's shipped CORS config (with
// `exposedHeaders`) never hits it. Pin that the KEEP block no longer
// lists it as intersection and the DISCARD block now does list it as
// self-inflicted.
func TestSynthesisWorkflow_P1_XCacheRemovedFromKeepList(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	// Locate KEEP block.
	idx := strings.Index(body, "**KEEP** (intersection):")
	if idx < 0 {
		t.Fatal("KEEP block heading missing")
	}
	keepEnd := strings.Index(body[idx:], "**DISCARD")
	if keepEnd < 0 {
		t.Fatal("DISCARD block boundary missing after KEEP")
	}
	keepWindow := body[idx : idx+keepEnd]
	if strings.Contains(keepWindow, "X-Cache") {
		t.Error("KEEP block still lists X-Cache example; spec routes to DISCARD (self-inflicted)")
	}
	// DISCARD block — explicit X-Cache shape lands as a worked
	// example. The block lives further down so search from idx.
	tail := body[idx:]
	discardIdx := strings.Index(tail, "**DISCARD — `self-inflicted`**")
	if discardIdx < 0 {
		t.Fatal("DISCARD self-inflicted block missing")
	}
	discardWindow := tail[discardIdx:]
	for _, want := range []string{
		"X-Cache",
		"exposedHeaders",
		"Run-42 dogfood: apidev KB #3",
	} {
		if !strings.Contains(discardWindow, want) {
			t.Errorf("DISCARD self-inflicted block missing X-Cache worked-example anchor %q", want)
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
