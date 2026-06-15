package recipe

import (
	"path/filepath"
	"strings"
	"testing"
)

// run47_item_h_test.go — Run-47 Item H pin tests.
//
// Item H — Typed multi-batch enrich-findings API (closes R8).
//
// Plan: plans/run-47-substrate-fixes.md (Item H).
//
// Sub-agent's audit emission no longer needs to fit a monolithic 58.7 KB
// block — multiple enrich-findings calls accumulate walked entries into
// a BatchedRefinement2Ledger, then promote to Refinement2Ledger only
// after every batch in {1..TotalBatches} has been received. Pin tests
// cover: aggregation, no-latest-wins-loss, single-batch backward-compat,
// close-gate refusal on incomplete batch set, per-batch Item A
// validation, and post-promotion Item C consistency.

// TestEnrichFindings_AggregatesBatches — 4 batches with disjoint walked
// entries (each idKey-grammar-valid per Item A); final aggregated
// sess.Refinement2Ledger.Walked has the union of all four.
func TestEnrichFindings_AggregatesBatches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Fragments = map[string]string{
		"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml":    "zerops:\n  - setup: api\n",
	}
	sess.OutputRoot = dir

	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	all := manifest.AllKeys()
	if len(all) < 4 {
		t.Fatalf("test fixture needs >= 4 manifest entries; got %d", len(all))
	}

	// Partition the manifest's keys into 4 disjoint batches.
	chunks := partitionKeysIntoFourBatches(all)
	if len(chunks) != 4 {
		t.Fatalf("partition produced %d batches; want 4", len(chunks))
	}

	for i, chunk := range chunks {
		batchID := i + 1
		in := RecipeInput{
			Action:       "enrich-findings",
			Findings:     &FindingsEnvelope{Findings: nil},
			Walked:       chunk,
			BatchID:      batchID,
			TotalBatches: 4,
		}
		r := enrichFindingsAction(sess, in, RecipeResult{Action: "enrich-findings"})
		if !r.OK {
			t.Fatalf("batch %d: expected ok=true; got Error=%q", batchID, r.Error)
		}
		if batchID < 4 {
			// Not yet complete: ledger must remain unpromoted.
			if sess.Refinement2Ledger != nil {
				t.Errorf("batch %d (incomplete): Refinement2Ledger should be nil; got %+v", batchID, sess.Refinement2Ledger)
			}
			if sess.BatchedRefinement2Ledger == nil {
				t.Errorf("batch %d (incomplete): BatchedRefinement2Ledger should be non-nil", batchID)
			}
		}
	}

	// After all 4 batches: promotion to Refinement2Ledger; BatchedRefinement2Ledger cleared.
	if sess.Refinement2Ledger == nil {
		t.Fatal("after 4-of-4 batches: Refinement2Ledger must be promoted")
	}
	if sess.BatchedRefinement2Ledger != nil {
		t.Errorf("after promotion: BatchedRefinement2Ledger must be cleared; got %+v", sess.BatchedRefinement2Ledger)
	}
	if got, want := len(sess.Refinement2Ledger.Walked), len(all); got != want {
		t.Errorf("aggregated walked len = %d; want union %d", got, want)
	}
	gotSet := map[string]bool{}
	for _, k := range sess.Refinement2Ledger.Walked {
		gotSet[k] = true
	}
	for _, k := range all {
		if !gotSet[k] {
			t.Errorf("aggregated walked missing key %q", k)
		}
	}
}

// TestEnrichFindings_BatchedLedger_NoLatestWinsLoss — codex requirement;
// walked entries from batch 1 are NOT overwritten by batch 4. Distinct
// from TestEnrichFindings_AggregatesBatches because this pins the
// non-latest-wins semantics specifically: batch 1's entries must still
// be present in the promoted ledger after the last batch landed.
func TestEnrichFindings_BatchedLedger_NoLatestWinsLoss(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Fragments = map[string]string{
		"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml":    "zerops:\n  - setup: api\n",
	}
	sess.OutputRoot = dir

	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	all := manifest.AllKeys()
	if len(all) < 4 {
		t.Fatalf("test fixture needs >= 4 manifest entries; got %d", len(all))
	}
	chunks := partitionKeysIntoFourBatches(all)

	// Track batch 1's keys so we can assert their presence post-promotion.
	batch1Keys := append([]string(nil), chunks[0]...)
	if len(batch1Keys) == 0 {
		t.Fatal("partition produced empty batch 1")
	}

	for i, chunk := range chunks {
		batchID := i + 1
		in := RecipeInput{
			Action:       "enrich-findings",
			Findings:     &FindingsEnvelope{Findings: nil},
			Walked:       chunk,
			BatchID:      batchID,
			TotalBatches: 4,
		}
		r := enrichFindingsAction(sess, in, RecipeResult{Action: "enrich-findings"})
		if !r.OK {
			t.Fatalf("batch %d: expected ok=true; got Error=%q", batchID, r.Error)
		}
	}

	if sess.Refinement2Ledger == nil {
		t.Fatal("after 4-of-4 batches: Refinement2Ledger must be promoted")
	}
	got := map[string]bool{}
	for _, k := range sess.Refinement2Ledger.Walked {
		got[k] = true
	}
	for _, k := range batch1Keys {
		if !got[k] {
			t.Errorf("batch-1 key %q lost after batch 4 — latest-wins loss", k)
		}
	}
}

// TestEnrichFindings_SingleBatchBackwardCompat — call with
// TotalBatches=0 (default) and TotalBatches=1 both use existing
// latest-wins single-batch semantics; existing Item A test
// (TestEnrichFindings_AcceptsValidWalkedIDKey) still passes.
func TestEnrichFindings_SingleBatchBackwardCompat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		totalBatches int
		batchID      int
	}{
		{name: "TotalBatches=0 (default)", totalBatches: 0, batchID: 0},
		{name: "TotalBatches=1", totalBatches: 1, batchID: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
			sess := NewSession("synth-showcase", "dev", log, dir, nil)
			sess.Plan = syntheticShowcasePlan()
			sess.Plan.Fragments = map[string]string{
				"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
				"codebase/api/zerops-yaml":    "zerops:\n  - setup: api\n",
			}
			sess.OutputRoot = dir

			manifest, mErr := BuildRefinement2Manifest(sess.Plan)
			if mErr != nil {
				t.Fatalf("BuildRefinement2Manifest: %v", mErr)
			}
			all := manifest.AllKeys()

			in := RecipeInput{
				Action:       "enrich-findings",
				Findings:     &FindingsEnvelope{Findings: nil},
				Walked:       all,
				BatchID:      tc.batchID,
				TotalBatches: tc.totalBatches,
			}
			r := enrichFindingsAction(sess, in, RecipeResult{Action: "enrich-findings"})
			if !r.OK {
				t.Fatalf("expected ok=true; got Error=%q", r.Error)
			}
			// Single-batch path: promoted directly; no BatchedRefinement2Ledger.
			if sess.BatchedRefinement2Ledger != nil {
				t.Errorf("single-batch path must not allocate BatchedRefinement2Ledger; got %+v", sess.BatchedRefinement2Ledger)
			}
			if sess.Refinement2Ledger == nil {
				t.Fatal("single-batch path must promote ledger immediately")
			}
			if len(sess.Refinement2Ledger.Walked) != len(all) {
				t.Errorf("single-batch walked length: got %d, want %d", len(sess.Refinement2Ledger.Walked), len(all))
			}
		})
	}
}

// TestRefinement2CloseGate_RefusesIncompleteBatchSet — close-gate
// refuses while BatchedRefinement2Ledger is still non-nil
// (ReceivedBatches = {1, 2} of 4 total); refusal names the missing
// batch IDs (3, 4).
func TestRefinement2CloseGate_RefusesIncompleteBatchSet(t *testing.T) {
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

	// Simulate partial batch state: received batches {1, 2} of 4.
	sess.BatchedRefinement2Ledger = &BatchedLedger{
		Walked:          []string{"codebase_kb:api:0"},
		ReceivedBatches: map[int]bool{1: true, 2: true},
		TotalBatches:    4,
	}
	sess.Completed[PhaseRefinement] = false
	sess.Current = PhaseRefinement

	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseRefinement)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if r.OK {
		t.Fatal("expected ok=false on incomplete batch set; got OK=true")
	}
	// Missing batch IDs (3, 4) must be named in the refusal.
	for _, want := range []string{"3", "4"} {
		if !strings.Contains(r.Error, want) {
			t.Errorf("Error must name missing batch ID %q; got %q", want, r.Error)
		}
	}
}

// TestEnrichFindings_BatchedRespectsItemAValidation — codex
// requirement; if any batch contains an invalid idKey per Item A's
// grammar, that batch is refused; prior valid batches' walked entries
// remain in BatchedRefinement2Ledger (not promoted, not lost).
func TestEnrichFindings_BatchedRespectsItemAValidation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Fragments = map[string]string{
		"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml":    "zerops:\n  - setup: api\n",
	}
	sess.OutputRoot = dir

	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	all := manifest.AllKeys()
	if len(all) < 4 {
		t.Fatalf("test fixture needs >= 4 manifest entries; got %d", len(all))
	}
	chunks := partitionKeysIntoFourBatches(all)

	// Batch 1: valid.
	r := enrichFindingsAction(sess, RecipeInput{
		Action:       "enrich-findings",
		Findings:     &FindingsEnvelope{Findings: nil},
		Walked:       chunks[0],
		BatchID:      1,
		TotalBatches: 4,
	}, RecipeResult{Action: "enrich-findings"})
	if !r.OK {
		t.Fatalf("batch 1: expected ok=true; got Error=%q", r.Error)
	}
	if sess.BatchedRefinement2Ledger == nil {
		t.Fatal("after batch 1: BatchedRefinement2Ledger must be set")
	}
	priorWalked := append([]string(nil), sess.BatchedRefinement2Ledger.Walked...)

	// Batch 2: invalid grammar.
	rBad := enrichFindingsAction(sess, RecipeInput{
		Action:       "enrich-findings",
		Findings:     &FindingsEnvelope{Findings: nil},
		Walked:       []string{"S5:codebase/api/knowledge-base/<name>"},
		BatchID:      2,
		TotalBatches: 4,
	}, RecipeResult{Action: "enrich-findings"})
	if rBad.OK {
		t.Fatal("invalid-grammar batch: expected ok=false; got OK=true")
	}
	// Prior batch 1's walked entries must remain in BatchedRefinement2Ledger.
	if sess.BatchedRefinement2Ledger == nil {
		t.Fatal("after refused batch 2: BatchedRefinement2Ledger must still be non-nil")
	}
	if got, want := len(sess.BatchedRefinement2Ledger.Walked), len(priorWalked); got != want {
		t.Errorf("BatchedRefinement2Ledger.Walked mutated by refused batch: got len=%d, want %d", got, want)
	}
	// Batch 2 must NOT be marked received.
	if sess.BatchedRefinement2Ledger.ReceivedBatches[2] {
		t.Error("refused batch 2 must not be marked received")
	}
	if !sess.BatchedRefinement2Ledger.ReceivedBatches[1] {
		t.Error("valid batch 1 must remain marked received")
	}
	// Refinement2Ledger must not be promoted.
	if sess.Refinement2Ledger != nil {
		t.Errorf("Refinement2Ledger promoted prematurely: %+v", sess.Refinement2Ledger)
	}
}

// TestEnrichFindings_BatchedRespectsItemCConsistency — codex
// requirement; after all batches received + promoted, Item C's
// walked-vs-scanned check at the close-gate runs against the aggregated
// walked count. With aggregated walked=manifest-total + scanned=manifest-
// total, the gate accepts.
func TestEnrichFindings_BatchedRespectsItemCConsistency(t *testing.T) {
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
	all := manifest.AllKeys()
	if len(all) < 4 {
		t.Fatalf("test fixture needs >= 4 manifest entries; got %d", len(all))
	}
	chunks := partitionKeysIntoFourBatches(all)

	// Drive 4 batches, with scanned-counter on final batch = manifest total
	// so Item C's walked-vs-scanned check fires on the AGGREGATED walked
	// length, not on any single batch's slice.
	for i, chunk := range chunks {
		batchID := i + 1
		in := RecipeInput{
			Action:       "enrich-findings",
			Findings:     &FindingsEnvelope{Findings: nil},
			Walked:       chunk,
			BatchID:      batchID,
			TotalBatches: 4,
		}
		// Carry the cross-surface uniqueness scan count on the FINAL batch
		// to simulate the typical pattern (a single global counter on the
		// audit's last emission).
		if batchID == 4 {
			in.CrossSurfaceUniquenessScanned = len(all)
		}
		r := enrichFindingsAction(sess, in, RecipeResult{Action: "enrich-findings"})
		if !r.OK {
			t.Fatalf("batch %d: expected ok=true; got Error=%q", batchID, r.Error)
		}
	}

	if sess.Refinement2Ledger == nil {
		t.Fatal("after 4-of-4 batches: Refinement2Ledger must be promoted")
	}
	// Aggregated walked count must equal manifest total.
	if got, want := len(sess.Refinement2Ledger.Walked), len(all); got != want {
		t.Fatalf("aggregated walked = %d, want %d", got, want)
	}
	// Scanned counter must have been carried into the promoted ledger.
	if got, want := sess.Refinement2Ledger.CrossSurfaceUniquenessScanned, len(all); got != want {
		t.Fatalf("promoted CrossSurfaceUniquenessScanned = %d, want %d", got, want)
	}
	// Close-gate must accept (Item C check runs against aggregated walked).
	sess.Current = PhaseRefinement
	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseRefinement)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if !r.OK {
		t.Errorf("close-gate must accept aggregated walked=%d, scanned=%d (manifest=%d); got Error=%q",
			len(sess.Refinement2Ledger.Walked), sess.Refinement2Ledger.CrossSurfaceUniquenessScanned, len(all), r.Error)
	}
}

// TestEnrichFindings_BatchedRejectsOutOfRangeBatchID — codex code
// review surfaced that BatchID=0 / BatchID>TotalBatches with
// TotalBatches>1 silently inflated len(ReceivedBatches) and could
// trigger spurious promotion. Refuse pre-mutation.
func TestEnrichFindings_BatchedRejectsOutOfRangeBatchID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		batchID      int
		totalBatches int
	}{
		{name: "BatchID=0 with TotalBatches=4", batchID: 0, totalBatches: 4},
		{name: "BatchID=5 with TotalBatches=4", batchID: 5, totalBatches: 4},
		{name: "BatchID=-1 with TotalBatches=4", batchID: -1, totalBatches: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
			sess := NewSession("synth-showcase", "dev", log, dir, nil)
			sess.Plan = syntheticShowcasePlan()
			sess.OutputRoot = dir
			in := RecipeInput{
				Action:       "enrich-findings",
				Findings:     &FindingsEnvelope{Findings: nil},
				Walked:       []string{},
				BatchID:      tc.batchID,
				TotalBatches: tc.totalBatches,
			}
			r := enrichFindingsAction(sess, in, RecipeResult{Action: "enrich-findings"})
			if r.OK {
				t.Fatal("expected ok=false on out-of-range batchId; got OK=true")
			}
			if !strings.Contains(r.Error, "batchId") || !strings.Contains(r.Error, "totalBatches") {
				t.Errorf("Error must name both batchId + totalBatches; got %q", r.Error)
			}
			// Accumulator must not have been allocated.
			if sess.BatchedRefinement2Ledger != nil {
				t.Errorf("refused out-of-range batch must not allocate accumulator; got %+v", sess.BatchedRefinement2Ledger)
			}
		})
	}
}

// TestEnrichFindings_BatchedRejectsTotalBatchesShift — codex code
// review: an in-flight accumulator (TotalBatches=4, some received)
// must refuse a later call declaring a different TotalBatches. Without
// this, the accumulator's promotion criterion shifts mid-sequence and
// silently mis-aggregates.
func TestEnrichFindings_BatchedRejectsTotalBatchesShift(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Fragments = map[string]string{
		"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml":    "zerops:\n  - setup: api\n",
	}
	sess.OutputRoot = dir

	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	all := manifest.AllKeys()
	if len(all) < 4 {
		t.Fatalf("test fixture needs >= 4 manifest entries; got %d", len(all))
	}
	chunks := partitionKeysIntoFourBatches(all)

	// Batch 1 of 4: lands.
	r := enrichFindingsAction(sess, RecipeInput{
		Action:       "enrich-findings",
		Findings:     &FindingsEnvelope{Findings: nil},
		Walked:       chunks[0],
		BatchID:      1,
		TotalBatches: 4,
	}, RecipeResult{Action: "enrich-findings"})
	if !r.OK {
		t.Fatalf("batch 1 of 4: expected ok=true; got Error=%q", r.Error)
	}
	priorWalked := append([]string(nil), sess.BatchedRefinement2Ledger.Walked...)

	// Batch 2 of 3 (DIFFERENT totalBatches): refuse pre-mutation.
	rBad := enrichFindingsAction(sess, RecipeInput{
		Action:       "enrich-findings",
		Findings:     &FindingsEnvelope{Findings: nil},
		Walked:       chunks[1],
		BatchID:      2,
		TotalBatches: 3, // shifted!
	}, RecipeResult{Action: "enrich-findings"})
	if rBad.OK {
		t.Fatal("expected ok=false on totalBatches shift; got OK=true")
	}
	if !strings.Contains(rBad.Error, "totalBatches") {
		t.Errorf("Error must name totalBatches; got %q", rBad.Error)
	}
	// Accumulator must NOT have shifted or been mutated.
	if sess.BatchedRefinement2Ledger == nil {
		t.Fatal("refused shift must leave accumulator intact")
	}
	if got := sess.BatchedRefinement2Ledger.TotalBatches; got != 4 {
		t.Errorf("accumulator TotalBatches shifted: got %d, want 4", got)
	}
	if got, want := len(sess.BatchedRefinement2Ledger.Walked), len(priorWalked); got != want {
		t.Errorf("accumulator Walked mutated: got len=%d, want %d", got, want)
	}
	if sess.BatchedRefinement2Ledger.ReceivedBatches[2] {
		t.Error("refused shift must not mark batch 2 received")
	}
}

// TestEnrichFindings_BatchedRejectsDuplicateBatchID — codex code
// review surfaced that a retry of the same batchID would append walked
// entries twice and later fail Item C's walked-vs-scanned consistency
// at close-time. Refuse pre-mutation when the batchID is already in
// the accumulator's ReceivedBatches set.
func TestEnrichFindings_BatchedRejectsDuplicateBatchID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Fragments = map[string]string{
		"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml":    "zerops:\n  - setup: api\n",
	}
	sess.OutputRoot = dir

	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	all := manifest.AllKeys()
	if len(all) < 4 {
		t.Fatalf("test fixture needs >= 4 manifest entries; got %d", len(all))
	}
	chunks := partitionKeysIntoFourBatches(all)

	// Batch 1 of 4: lands.
	r := enrichFindingsAction(sess, RecipeInput{
		Action:       "enrich-findings",
		Findings:     &FindingsEnvelope{Findings: nil},
		Walked:       chunks[0],
		BatchID:      1,
		TotalBatches: 4,
	}, RecipeResult{Action: "enrich-findings"})
	if !r.OK {
		t.Fatalf("batch 1: expected ok=true; got Error=%q", r.Error)
	}
	priorLen := len(sess.BatchedRefinement2Ledger.Walked)

	// Batch 1 retry — same batchID. Refuse.
	rDup := enrichFindingsAction(sess, RecipeInput{
		Action:       "enrich-findings",
		Findings:     &FindingsEnvelope{Findings: nil},
		Walked:       chunks[0],
		BatchID:      1,
		TotalBatches: 4,
	}, RecipeResult{Action: "enrich-findings"})
	if rDup.OK {
		t.Fatal("expected ok=false on duplicate batchID; got OK=true")
	}
	if !strings.Contains(rDup.Error, "batchId=1") {
		t.Errorf("Error must name duplicate batchId; got %q", rDup.Error)
	}
	// Accumulator's Walked must NOT have grown.
	if got := len(sess.BatchedRefinement2Ledger.Walked); got != priorLen {
		t.Errorf("accumulator walked grew on duplicate-batchID refusal: got len=%d, want %d", got, priorLen)
	}
}

// TestEnrichFindings_BatchedRejectsRetryAfterPromotion — codex code
// review (third pass) surfaced the post-promotion retry hole. Once
// the multi-batch sequence promotes to sess.Refinement2Ledger, the
// accumulator is cleared. A late retry of any batchID must NOT
// recreate the accumulator (which would block close-gate with a
// spurious "missing N-1 batches" refusal). Refuse the retry; sub-agent
// must use single-batch latest-wins to replace the promoted ledger.
func TestEnrichFindings_BatchedRejectsRetryAfterPromotion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Fragments = map[string]string{
		"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml":    "zerops:\n  - setup: api\n",
	}
	sess.OutputRoot = dir

	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	all := manifest.AllKeys()
	if len(all) < 4 {
		t.Fatalf("test fixture needs >= 4 manifest entries; got %d", len(all))
	}
	chunks := partitionKeysIntoFourBatches(all)

	// Drive all 4 batches to completion (promotion fires).
	for i, chunk := range chunks {
		batchID := i + 1
		r := enrichFindingsAction(sess, RecipeInput{
			Action:       "enrich-findings",
			Findings:     &FindingsEnvelope{Findings: nil},
			Walked:       chunk,
			BatchID:      batchID,
			TotalBatches: 4,
		}, RecipeResult{Action: "enrich-findings"})
		if !r.OK {
			t.Fatalf("batch %d: expected ok=true; got Error=%q", batchID, r.Error)
		}
	}
	if sess.Refinement2Ledger == nil {
		t.Fatal("after 4-of-4 batches: Refinement2Ledger must be promoted")
	}
	if sess.BatchedRefinement2Ledger != nil {
		t.Fatal("after promotion: BatchedRefinement2Ledger must be cleared")
	}
	promotedWalkedLen := len(sess.Refinement2Ledger.Walked)

	// Now: late retry of batch 4 (e.g. concurrent duplicate emission).
	rRetry := enrichFindingsAction(sess, RecipeInput{
		Action:       "enrich-findings",
		Findings:     &FindingsEnvelope{Findings: nil},
		Walked:       chunks[3],
		BatchID:      4,
		TotalBatches: 4,
	}, RecipeResult{Action: "enrich-findings"})
	if rRetry.OK {
		t.Fatal("expected ok=false on post-promotion retry; got OK=true")
	}
	if !strings.Contains(rRetry.Error, "already promoted") && !strings.Contains(rRetry.Error, "Refinement2Ledger") {
		t.Errorf("Error must explain promotion already happened; got %q", rRetry.Error)
	}
	// Accumulator MUST NOT have been recreated.
	if sess.BatchedRefinement2Ledger != nil {
		t.Errorf("post-promotion retry recreated accumulator; got %+v", sess.BatchedRefinement2Ledger)
	}
	// Promoted ledger MUST be unchanged.
	if got := len(sess.Refinement2Ledger.Walked); got != promotedWalkedLen {
		t.Errorf("promoted Refinement2Ledger.Walked mutated by retry: got len=%d, want %d", got, promotedWalkedLen)
	}

	// And: single-batch replacement (latest-wins) still works after the
	// promoted state — the documented escape hatch.
	rReplace := enrichFindingsAction(sess, RecipeInput{
		Action:       "enrich-findings",
		Findings:     &FindingsEnvelope{Findings: nil},
		Walked:       all,
		BatchID:      0,
		TotalBatches: 0,
	}, RecipeResult{Action: "enrich-findings"})
	if !rReplace.OK {
		t.Errorf("single-batch replacement must work after promotion; got Error=%q", rReplace.Error)
	}
}

// TestEnrichFindings_SingleBatchClearsInFlightAccumulator — codex
// code review (fourth pass) surfaced that a single-batch latest-wins
// call would leave a partial BatchedRefinement2Ledger intact; the
// close-gate's batched-incomplete refusal would then fire spuriously
// even though the replacement ledger landed. Also pins that stale
// remaining batches from the old sequence cannot later promote over
// the newer single-batch ledger.
func TestEnrichFindings_SingleBatchClearsInFlightAccumulator(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Fragments = map[string]string{
		"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml":    "zerops:\n  - setup: api\n",
	}
	sess.OutputRoot = dir

	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	all := manifest.AllKeys()
	if len(all) < 4 {
		t.Fatalf("test fixture needs >= 4 manifest entries; got %d", len(all))
	}
	chunks := partitionKeysIntoFourBatches(all)

	// Land batch 1 of 4: accumulator allocated, incomplete.
	r := enrichFindingsAction(sess, RecipeInput{
		Action:       "enrich-findings",
		Findings:     &FindingsEnvelope{Findings: nil},
		Walked:       chunks[0],
		BatchID:      1,
		TotalBatches: 4,
	}, RecipeResult{Action: "enrich-findings"})
	if !r.OK {
		t.Fatalf("batch 1 of 4: expected ok=true; got Error=%q", r.Error)
	}
	if sess.BatchedRefinement2Ledger == nil {
		t.Fatal("batch 1 of 4: accumulator must exist")
	}

	// Single-batch latest-wins replacement.
	rReplace := enrichFindingsAction(sess, RecipeInput{
		Action:       "enrich-findings",
		Findings:     &FindingsEnvelope{Findings: nil},
		Walked:       all,
		BatchID:      0,
		TotalBatches: 0,
	}, RecipeResult{Action: "enrich-findings"})
	if !rReplace.OK {
		t.Fatalf("single-batch replacement: expected ok=true; got Error=%q", rReplace.Error)
	}
	// Accumulator MUST be cleared.
	if sess.BatchedRefinement2Ledger != nil {
		t.Fatalf("single-batch replacement must clear BatchedRefinement2Ledger; got %+v", sess.BatchedRefinement2Ledger)
	}
	// Promoted ledger must have the replacement content.
	if sess.Refinement2Ledger == nil {
		t.Fatal("single-batch replacement must set Refinement2Ledger")
	}
	if got, want := len(sess.Refinement2Ledger.Walked), len(all); got != want {
		t.Errorf("replacement walked len = %d; want %d", got, want)
	}

	// Stale retry from the OLD multi-batch sequence (e.g. batch 2 of 4)
	// must now be refused — it would attempt to resurrect a stale
	// accumulator over the newer single-batch ledger.
	rStale := enrichFindingsAction(sess, RecipeInput{
		Action:       "enrich-findings",
		Findings:     &FindingsEnvelope{Findings: nil},
		Walked:       chunks[1],
		BatchID:      2,
		TotalBatches: 4,
	}, RecipeResult{Action: "enrich-findings"})
	if rStale.OK {
		t.Fatal("stale multi-batch emission after single-batch replacement: expected ok=false; got OK=true")
	}
	// Promoted ledger MUST still be the single-batch replacement.
	if sess.Refinement2Ledger == nil {
		t.Fatal("stale-batch refusal must leave Refinement2Ledger intact")
	}
	if got, want := len(sess.Refinement2Ledger.Walked), len(all); got != want {
		t.Errorf("stale-batch refusal mutated Refinement2Ledger.Walked: got len=%d, want %d", got, want)
	}
	// And NO accumulator was resurrected.
	if sess.BatchedRefinement2Ledger != nil {
		t.Errorf("stale-batch refusal must not resurrect accumulator; got %+v", sess.BatchedRefinement2Ledger)
	}
}

// partitionKeysIntoFourBatches distributes keys into 4 disjoint chunks
// via round-robin, preserving relative order within each chunk. Helper
// for the batched-enrich pin tests, all of which exercise the
// TotalBatches=4 path that exposed the run-46 monolithic-emission
// overflow class.
func partitionKeysIntoFourBatches(keys []string) [][]string {
	const n = 4
	out := make([][]string, n)
	for i, k := range keys {
		idx := i % n
		out[idx] = append(out[idx], k)
	}
	return out
}
