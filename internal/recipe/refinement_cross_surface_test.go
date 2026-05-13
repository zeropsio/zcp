package recipe

import (
	"path/filepath"
	"strings"
	"testing"
)

// refinement_cross_surface_test.go — Run-46 Item 6 substrate pin.
//
// Run-45 forensic — two cross-codebase KB duplications shipped without
// cross-reference:
//   - apidev IG #5 + worker KB #4 both teach APP_SECRET shadow
//   - apidev KB #3 + worker KB #5 both teach https-Meilisearch
//
// audit_checklist.md §"Cross-surface uniqueness pass" said the audit
// MUST run a second pass after the per-item walk, but compliance was
// invisible — the engine couldn't tell "scanned everything and found
// no duplicates" from "didn't run the pass." Item 6 makes the pass
// verifiable via a sub-agent receipt.
//
// Mechanism: the refinement-2 close-gate (already extended for
// Item 1's walked-ledger) reads sess.Refinement2Ledger and refuses
// when `crossSurfaceUniquenessScanned < total-manifest-items`.

// TestRefinement2CloseGate_RequiresCrossSurfaceUniquenessReport — the
// close-gate refuses refinement-phase close when the ledger's
// `crossSurfaceUniquenessScanned` is zero (or less than the manifest
// total). Reused Item 1's gate path; this test is the Item 6 specific
// assertion.
func TestRefinement2CloseGate_RequiresCrossSurfaceUniquenessReport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, sess.Plan)
	sess.OutputRoot = dir
	if _, err := stitchContent(sess); err != nil {
		t.Fatalf("seed stitch-content: %v", err)
	}
	for _, p := range []Phase{
		PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature,
		PhaseCodebaseContent, PhaseEnvContent, PhaseFinalize, PhaseRefinement,
	} {
		if err := sess.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase(%s): %v", p, err)
		}
		sess.Completed[p] = true
	}
	sess.RefinementDispatched = true
	sess.Refinement2Dispatched = true

	// Build the manifest + populate a walked-ledger that covers every
	// manifest entry (so Item 1's gate path passes) but DOES NOT report
	// a cross-surface uniqueness scan (CrossSurfaceUniquenessScanned=0).
	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	sess.Refinement2Ledger = &Refinement2Ledger{
		Walked: manifest.AllKeys(),
		// CrossSurfaceUniquenessScanned left at 0 — the sub-agent didn't
		// run the pass.
	}
	sess.Completed[PhaseRefinement] = false
	sess.Current = PhaseRefinement

	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseRefinement)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if r.OK {
		t.Fatal("expected ok=false on missing cross-surface uniqueness report; got OK=true")
	}
	for _, want := range []string{"cross-surface uniqueness", "Run-46 Item 6"} {
		if !strings.Contains(r.Error, want) {
			t.Errorf("Error must name the cross-surface uniqueness gate; missing %q. Got: %q", want, r.Error)
		}
	}
}

// TestRefinement2CloseGate_AcceptsFullCrossSurfaceScan — when the
// sub-agent reports `crossSurfaceUniquenessScanned == manifest total`,
// the close-gate accepts the ledger and falls through to the normal
// completePhase path.
func TestRefinement2CloseGate_AcceptsFullCrossSurfaceScan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, sess.Plan)
	sess.OutputRoot = dir
	if _, err := stitchContent(sess); err != nil {
		t.Fatalf("seed stitch-content: %v", err)
	}
	for _, p := range []Phase{
		PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature,
		PhaseCodebaseContent, PhaseEnvContent, PhaseFinalize, PhaseRefinement,
	} {
		if err := sess.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase(%s): %v", p, err)
		}
		sess.Completed[p] = true
	}
	sess.RefinementDispatched = true
	sess.Refinement2Dispatched = true

	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	allKeys := manifest.AllKeys()
	sess.Refinement2Ledger = &Refinement2Ledger{
		Walked:                        allKeys,
		CrossSurfaceUniquenessScanned: len(allKeys),
	}
	// Leave Completed[PhaseRefinement]=true so CompletePhase short-
	// circuits past the gate set (which would require disk surfaces).
	// The Item 6 gate fires BEFORE the gate set, so the short-circuit
	// is isolation-safe for this assertion.
	sess.Current = PhaseRefinement

	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseRefinement)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if !r.OK {
		t.Errorf("expected ok=true with full ledger; got Error=%q", r.Error)
	}
}

// TestEnrichFindings_PersistsCrossSurfacePayload — `enrich-findings`
// passes through the `crossSurfaceUniquenessScanned` + `duplicates`
// input fields to sess.Refinement2Ledger. Item 1 added the data path;
// Item 6 makes the close-gate read it; Run-47 Item A validates walked
// keys against the manifest grammar (fixture populates fragments so
// the manifest enumerates `codebase_kb:api:0` instead of `<empty>`).
func TestEnrichFindings_PersistsCrossSurfacePayload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Fragments = map[string]string{
		"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
	}
	sess.OutputRoot = dir

	in := RecipeInput{
		Action:                        "enrich-findings",
		Findings:                      &FindingsEnvelope{Findings: nil},
		Walked:                        []string{"codebase_kb:api:0"},
		CrossSurfaceUniquenessScanned: 42,
		Duplicates: []string{
			"codebase/api/integration-guide/5 ↔ codebase/worker/knowledge-base#4",
		},
	}
	r := enrichFindingsAction(sess, in, RecipeResult{Action: "enrich-findings"})
	if !r.OK {
		t.Fatalf("enrich-findings returned Error=%q", r.Error)
	}
	if sess.Refinement2Ledger.CrossSurfaceUniquenessScanned != 42 {
		t.Errorf("CrossSurfaceUniquenessScanned = %d, want 42", sess.Refinement2Ledger.CrossSurfaceUniquenessScanned)
	}
	if got := len(sess.Refinement2Ledger.Duplicates); got != 1 {
		t.Errorf("Duplicates len = %d, want 1", got)
	}
}
