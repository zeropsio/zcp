package console

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

type summaryHeadHookStore struct {
	observer.ObjectStore
	key       string
	afterHead func()
	once      sync.Once
}

func (s *summaryHeadHookStore) Head(ctx context.Context, key string) (bool, int64, error) {
	exists, size, err := s.ObjectStore.Head(ctx, key)
	if key == s.key {
		s.once.Do(s.afterHead)
	}
	return exists, size, err
}

// TestPages_BatchesNewestFirstWithCounts pins §8.3 FM-51's "/" row shape —
// id, created, candidate sha (12 chars), set, count per verdict, total
// cost, observed runs n/m — read straight off loadBatchRows, newest
// manifest.createdAt first. Independent oracle: hand-picked fixture values
// (sha prefixes, per-run costs, per-run verdicts), never recomputed the
// implementation's own way.
func TestPages_BatchesNewestFirstWithCounts(t *testing.T) {
	store := newFakeStore()

	olderManifest := farm.BatchManifest{
		Batch: "bx-older", CreatedAt: "2026-09-01T00:00:00Z", StartedAt: "2026-09-01T00:00:00Z",
		Set: "gate", CandidateSha256: "1111222233334444", EvaluatorSha256: "eval-sha", ScenariosDigest: "scn-sha",
		Observer: "claude-sonnet-5",
		Runs:     []farm.ManifestRun{{RunID: "bx-older-scn", Scenario: "scn", ProjectName: "p"}},
	}
	store.putJSON(t, "batches/bx-older/manifest.json", olderManifest)
	seedRun(t, store, runFixture{
		runID: "bx-older-scn", scenario: "scn", startedAt: time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC),
		durationS: "5s", costUsd: 0.05, taskResult: "passed", done: true,
	})

	newerManifest := farm.BatchManifest{
		Batch: "bx-newer", CreatedAt: "2026-09-05T00:00:00Z", StartedAt: "2026-09-05T00:00:00Z",
		Set: "all", CandidateSha256: "abcdefabcdefabcdefabcdef", EvaluatorSha256: "eval-sha", ScenariosDigest: "scn-sha",
		Observer: "claude-sonnet-5",
		Runs: []farm.ManifestRun{
			{RunID: "bx-newer-a", Scenario: "a", ProjectName: "p"},
			{RunID: "bx-newer-b", Scenario: "b", ProjectName: "p"},
			{RunID: "bx-newer-c", Scenario: "c", ProjectName: "p"},
		},
	}
	store.putJSON(t, "batches/bx-newer/manifest.json", newerManifest)
	store.putJSON(t, "batches/bx-newer/summary.json", farm.BatchSummary{
		Batch: "bx-newer", FinishedAt: "2026-09-05T01:00:00Z", EndedBy: "settled",
		Runs: []farm.SummaryRun{
			{RunID: "bx-newer-a", Scenario: "a", Result: "passed"},
			{RunID: "bx-newer-b", Scenario: "b", Result: "failed"},
			{RunID: "bx-newer-c", Scenario: "c", Result: "blocked"},
		},
	})
	seedRun(t, store, runFixture{
		runID: "bx-newer-a", scenario: "a", startedAt: time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC),
		durationS: "10s", costUsd: 0.10, taskResult: "passed", done: true,
	})
	seedRun(t, store, runFixture{
		runID: "bx-newer-b", scenario: "b", startedAt: time.Date(2026, 9, 5, 1, 5, 0, 0, time.UTC),
		durationS: "8s", costUsd: 0.20, taskResult: "failed", done: true,
	})
	seedRun(t, store, runFixture{
		runID: "bx-newer-c", scenario: "c", startedAt: time.Date(2026, 9, 5, 1, 10, 0, 0, time.UTC),
		durationS: "3s", costUsd: 0.30, taskResult: "blocked", done: true,
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "bx-newer-a", ObsID: "20260905T010000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC), Status: "ok", Headline: "ok",
	})

	rows, err := loadBatchRows(context.Background(), store, false, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("loadBatchRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].BatchID != "bx-newer" || rows[1].BatchID != "bx-older" {
		t.Fatalf("order = [%s, %s], want [bx-newer, bx-older] (newest first)", rows[0].BatchID, rows[1].BatchID)
	}

	newer := rows[0]
	wantCreated := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	if !newer.CreatedAt.Equal(wantCreated) {
		t.Errorf("newer.CreatedAt = %v, want %v", newer.CreatedAt, wantCreated)
	}
	if newer.CandidateSha12 != "abcdefabcdef" {
		t.Errorf("newer.CandidateSha12 = %q, want %q", newer.CandidateSha12, "abcdefabcdef")
	}
	if newer.Set != "all" {
		t.Errorf("newer.Set = %q, want all", newer.Set)
	}

	wantCounts := map[string]int{"passed": 1, "failed": 1, "blocked": 1}
	gotCounts := map[string]int{}
	for _, c := range newer.VerdictCounts {
		gotCounts[c.Verdict] = c.Count
	}
	if len(gotCounts) != len(wantCounts) {
		t.Fatalf("VerdictCounts = %+v, want %+v", gotCounts, wantCounts)
	}
	for v, n := range wantCounts {
		if gotCounts[v] != n {
			t.Errorf("VerdictCounts[%s] = %d, want %d", v, gotCounts[v], n)
		}
	}

	const wantCost = 0.60
	if got := newer.TotalCostUsd; got < wantCost-0.0001 || got > wantCost+0.0001 {
		t.Errorf("newer.TotalCostUsd = %v, want %v", got, wantCost)
	}
	if newer.ObservedN != 1 || newer.ObservedM != 3 {
		t.Errorf("newer observed = %d/%d, want 1/3", newer.ObservedN, newer.ObservedM)
	}

	older := rows[1]
	if older.CandidateSha12 != "111122223333" {
		t.Errorf("older.CandidateSha12 = %q, want %q", older.CandidateSha12, "111122223333")
	}
	if older.ObservedN != 0 || older.ObservedM != 1 {
		t.Errorf("older observed = %d/%d, want 0/1", older.ObservedN, older.ObservedM)
	}
}

// TestLoadBatchRows_KindRequiresCostOrSteps pins item 6 (FIX2.md
// FIX2-DATA): a batch whose every run blocked instantly at $0 — done.json
// exists, but no results/ was ever written (gate1, asm7-9, tracerctl) — is
// batchKindEmpty, not batchKindEvaluation; a batch with a run that did real
// work (StepCount > 0, even at $0 cost) is batchKindEvaluation.
func TestLoadBatchRows_KindRequiresCostOrSteps(t *testing.T) {
	store := newFakeStore()

	blockedManifest := farm.BatchManifest{
		Batch: "blocked-batch", CreatedAt: "2026-09-01T00:00:00Z", StartedAt: "2026-09-01T00:00:00Z",
		Set: "gate", CandidateSha256: "cand-sha", EvaluatorSha256: "eval-sha", ScenariosDigest: "scn-sha",
		Runs: []farm.ManifestRun{{RunID: "blocked-scn", Scenario: "scn", ProjectName: "p"}},
	}
	store.putJSON(t, "batches/blocked-batch/manifest.json", blockedManifest)
	// started.json + done.json only — no results/, so meta.json is never
	// read: StepCount stays 0, CostUsd stays 0 (unknown), DoneExists true.
	store.putJSON(t, "runs/blocked-scn/started.json", map[string]any{
		"runId": "blocked-scn", "scenarioId": "scn", "startedAt": "2026-09-01T00:01:00Z",
	})
	store.putJSON(t, "runs/blocked-scn/done.json", map[string]any{
		"runId": "blocked-scn", "scenarioId": "scn",
		"runnerDimensions": map[string]any{"execution": "blocked"},
	})

	seedBatch(t, store, "worked-batch", "", []runFixture{
		{runID: "worked-scn", scenario: "scn", startedAt: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
			durationS: "1s", costUsd: 0, taskResult: "passed", done: true},
	}, false, nil)

	rows, err := loadBatchRows(context.Background(), store, false, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("loadBatchRows: %v", err)
	}
	byID := make(map[string]BatchRow, len(rows))
	for _, r := range rows {
		byID[r.BatchID] = r
	}
	if got := byID["blocked-batch"].Kind; got != batchKindEmpty {
		t.Errorf("blocked-batch.Kind = %q, want %q (done.json exists but no cost/steps)", got, batchKindEmpty)
	}
	if got := byID["worked-batch"].Kind; got != batchKindEvaluation {
		t.Errorf("worked-batch.Kind = %q, want %q (StepCount > 0 even at $0)", got, batchKindEvaluation)
	}
}

// TestLoadBatchRows_AllUnavailable_KindUnavailable pins the third batch
// state: unreadable evidence cannot establish either meaningful work or a
// proven empty batch.
func TestLoadBatchRows_AllUnavailable_KindUnavailable(t *testing.T) {
	store := newFakeStore()
	seedBatch(t, store, "unavailable-batch", "off", []runFixture{{
		runID: "unavailable-batch-a", scenario: "a", startedAt: fixedNow(t)(),
		durationS: "1s", costUsd: 0.1, taskResult: "passed", done: true,
	}}, false, nil)
	store.failListOn("runs/unavailable-batch-a/results/", fmt.Errorf("temporary results failure"))

	rows, err := loadBatchRows(context.Background(), store, false, nil, nil, nil, nil)
	if err != nil || len(rows) != 1 {
		t.Fatalf("loadBatchRows: rows=%+v err=%v", rows, err)
	}
	if got := rows[0].Kind; got != batchKindUnavailable {
		t.Errorf("Kind = %q, want %q", got, batchKindUnavailable)
	}
	if rows[0].UnavailableN != 1 {
		t.Errorf("UnavailableN = %d, want 1", rows[0].UnavailableN)
	}
}

// TestLoadBatchRows_UnfinishedIsNotProvenEmpty pins the same conservative
// classification for an in-progress run. Its evidence is not available yet,
// so it cannot satisfy the stronger "every run proved zero work" condition.
func TestLoadBatchRows_UnfinishedIsNotProvenEmpty(t *testing.T) {
	store := newFakeStore()
	seedBatch(t, store, "unfinished-batch", "off", []runFixture{{
		runID: "unfinished-batch-a", scenario: "a", startedAt: fixedNow(t)(), done: false,
	}}, false, nil)

	rows, err := loadBatchRows(context.Background(), store, false, nil, nil, nil, nil)
	if err != nil || len(rows) != 1 {
		t.Fatalf("loadBatchRows: rows=%+v err=%v", rows, err)
	}
	if got := rows[0].Kind; got != batchKindUnavailable {
		t.Errorf("Kind = %q, want %q until zero work is proven", got, batchKindUnavailable)
	}
}

// TestLoadBatchRows_SummaryBoundaryUsesOneSnapshot ensures Finished and the
// per-run verdicts are derived from one summary.json read even when the
// caller does not provide the server's long-lived summary cache.
func TestLoadBatchRows_SummaryBoundaryUsesOneSnapshot(t *testing.T) {
	store := newFakeStore()
	seedBatch(t, store, "single-summary", ObserverOff, []runFixture{{
		runID: "single-summary-run", scenario: "deploy", startedAt: fixedNow(t)(),
		durationS: "1s", costUsd: 0.1, taskResult: farm.VerdictPassed, done: true,
	}}, true, map[string]string{"single-summary-run": farm.VerdictPassed})
	store.resetCallLog()

	rows, err := loadBatchRows(context.Background(), store, false, nil, nil, nil, nil)
	if err != nil || len(rows) != 1 {
		t.Fatalf("loadBatchRows: rows=%+v err=%v", rows, err)
	}
	if !rows[0].Finished {
		t.Fatal("Finished = false, want true from summary.json")
	}

	key := summaryKey("single-summary")
	store.mu.Lock()
	defer store.mu.Unlock()
	heads, gets := 0, 0
	for _, got := range store.heads {
		if got == key {
			heads++
		}
	}
	for _, got := range store.gets {
		if got == key {
			gets++
		}
	}
	if heads != 1 || gets != 1 {
		t.Errorf("summary reads = %d HEAD, %d GET; want one consistent snapshot", heads, gets)
	}
}

// TestLoadBatchRows_SummarySnapshot_ResistsConcurrentSharedCacheFill pins a
// single internally consistent view when another request fills the shared
// cache just after this request observes an absent summary. Finished=false
// must stay paired with the unresolved running verdict from that same first
// snapshot.
func TestLoadBatchRows_SummarySnapshot_ResistsConcurrentSharedCacheFill(t *testing.T) {
	store := newFakeStore()
	const batch = "concurrent-summary"
	seedBatch(t, store, batch, ObserverOff, []runFixture{{
		runID: batch + "-run", scenario: "deploy", startedAt: fixedNow(t)(), done: false,
	}}, false, nil)

	shared := newSummaryCache(fixedNow(t))
	summary := farm.BatchSummary{
		Batch: batch, FinishedAt: fixedNow(t)().Format(time.RFC3339), EndedBy: "settled",
		Runs: []farm.SummaryRun{{RunID: batch + "-run", Scenario: "deploy", Result: farm.VerdictBlocked}},
	}
	hooked := &summaryHeadHookStore{
		ObjectStore: store,
		key:         summaryKey(batch),
		afterHead: func() {
			entry := shared.entry(batch)
			entry.mu.Lock()
			entry.found = true
			entry.summary = summary
			entry.mu.Unlock()
		},
	}

	rows, err := loadBatchRows(context.Background(), hooked, false, nil, nil, shared, nil)
	if err != nil || len(rows) != 1 {
		t.Fatalf("loadBatchRows: rows=%+v err=%v", rows, err)
	}
	row := rows[0]
	if row.Finished {
		t.Fatal("Finished = true, want false from this request's first absent-summary snapshot")
	}
	counts := make(map[string]int, len(row.VerdictCounts))
	for _, count := range row.VerdictCounts {
		counts[count.Verdict] = count.Count
	}
	if counts[verdictRunning] != 1 || counts[farm.VerdictBlocked] != 0 {
		t.Errorf("VerdictCounts = %+v, want running=1 and no final verdict from the later shared-cache fill", counts)
	}
}

// --- PreviousSameSet (item 2) ---------------------------------------------

func TestPreviousSameSet(t *testing.T) {
	mk := func(id, set, kind string, created time.Time) BatchRow {
		return BatchRow{BatchID: id, Set: set, Kind: kind, Finished: true, CreatedAt: created}
	}
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }

	rows := []BatchRow{
		mk("g5", "gate", batchKindEvaluation, day(5)),
		mk("g3", "gate", batchKindEvaluation, day(3)),
		mk("g1-empty", "gate", batchKindEmpty, day(1)), // empty: never a candidate
		mk("all4", "all", batchKindEvaluation, day(4)), // wrong set
	}

	got, found := PreviousSameSet(rows, mk("g5", "gate", batchKindEvaluation, day(5)))
	if !found {
		t.Fatal("PreviousSameSet: found = false, want true")
	}
	if got.BatchID != "g3" {
		t.Errorf("PreviousSameSet = %q, want g3 (newest older evaluation batch of the same set)", got.BatchID)
	}

	if _, found := PreviousSameSet(rows, mk("g1-empty", "gate", batchKindEmpty, day(1))); found {
		t.Error("PreviousSameSet found a batch older than the oldest gate batch, want none")
	}
}

// --- CompareBatches (item 2) ----------------------------------------------

func TestCompareBatches(t *testing.T) {
	prev := []RunRow{
		{Scenario: "still-fails", Verdict: farm.VerdictFailed},
		{Scenario: "gets-fixed", Verdict: farm.VerdictBlocked},
		{Scenario: "stays-passing", Verdict: farm.VerdictPassed},
		{Scenario: "regresses", Verdict: farm.VerdictPassed},
	}
	cur := []RunRow{
		{Scenario: "still-fails", Verdict: farm.VerdictFailed},
		{Scenario: "gets-fixed", Verdict: farm.VerdictPassed},
		{Scenario: "stays-passing", Verdict: farm.VerdictPassed},
		{Scenario: "regresses", Verdict: farm.VerdictBlocked},
		{Scenario: "brand-new-failure", Verdict: farm.VerdictNotRun},
	}

	diff := CompareBatches(prev, cur)

	wantNewlyFailing := []string{"brand-new-failure", "regresses"}
	wantFixed := []string{"gets-fixed"}
	wantStillFailing := []string{"still-fails"}

	assertStrSlice(t, "NewlyFailing", diff.NewlyFailing, wantNewlyFailing)
	assertStrSlice(t, "Fixed", diff.Fixed, wantFixed)
	assertStrSlice(t, "StillFailing", diff.StillFailing, wantStillFailing)
}

func TestCompareBatches_UnavailableRowsExcluded(t *testing.T) {
	prev := []RunRow{
		{Scenario: "becomes-unavailable", Verdict: farm.VerdictPassed},
		{Scenario: "new-failure", assessmentWorkError: "old evidence unavailable"},
	}
	cur := []RunRow{
		{Scenario: "becomes-unavailable", assessmentWorkError: "new evidence unavailable"},
		{Scenario: "new-failure", Verdict: farm.VerdictFailed},
	}

	diff := CompareBatches(prev, cur)
	assertStrSlice(t, "NewlyFailing", diff.NewlyFailing, []string{"new-failure"})
	assertStrSlice(t, "Fixed", diff.Fixed, nil)
	assertStrSlice(t, "StillFailing", diff.StillFailing, nil)
}

// TestBuildBatchComparison_DuplicateScenarioUsesAvailableRepresentative pins
// the link target shown after CompareBatches has collapsed duplicate scenario
// rows. An unavailable duplicate cannot replace the readable row, and a
// scenario represented only by unavailable evidence stays out of the diff.
func TestBuildBatchComparison_DuplicateScenarioUsesAvailableRepresentative(t *testing.T) {
	prev := []RunRow{
		{RunID: "prev-duplicate", Scenario: "duplicate", Verdict: farm.VerdictPassed},
		{RunID: "prev-unavailable-only", Scenario: "unavailable-only", Verdict: farm.VerdictPassed},
	}
	current := []RunRow{
		{RunID: "cur-duplicate-readable", Scenario: "duplicate", Verdict: farm.VerdictFailed},
		{RunID: "cur-duplicate-unavailable", Scenario: "duplicate", assessmentWorkError: "temporary evidence failure"},
		{RunID: "cur-unavailable-only", Scenario: "unavailable-only", assessmentWorkError: "temporary evidence failure"},
	}

	diff := CompareBatches(prev, current)
	view := buildBatchComparison(diff, current)
	if len(view.NewlyNotPassing) != 1 {
		t.Fatalf("NewlyNotPassing = %+v, want one readable duplicate scenario", view.NewlyNotPassing)
	}
	got := view.NewlyNotPassing[0]
	if got.Scenario != "duplicate" || got.RunID != "cur-duplicate-readable" || got.Verdict != farm.VerdictFailed {
		t.Errorf("NewlyNotPassing[0] = %+v, want readable failed representative", got)
	}
	if len(view.NowPassing) != 0 || len(view.StillNotPassing) != 0 {
		t.Errorf("comparison includes unavailable-only rows: %+v", view)
	}
}

// --- TestLists_OverviewBatches (item 5, §8.7) -----------------------------

func TestLists_OverviewBatches(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	rows := []BatchRow{
		{BatchID: "b1", Kind: batchKindEvaluation, CreatedAt: day(1), TotalCostUsd: 1, ObservedM: 1, ZCPHigh: 2},
		{BatchID: "b2", Kind: batchKindEvaluation, CreatedAt: day(3), TotalCostUsd: 5, ObservedM: 1, ZCPHigh: 1},
		{BatchID: "b3", Kind: batchKindEmpty, CreatedAt: day(2), TotalCostUsd: 0, ZCPHigh: 0},
	}
	eng := batchEngine()

	t.Run("kind default excludes empty", func(t *testing.T) {
		q, err := Parse(batchListSpec(), url.Values{})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(rows, q, day(10))
		if len(got) != 2 {
			t.Fatalf("got %d rows, want 2 evaluation rows under the default scope: %+v", len(got), got)
		}
	})

	t.Run("kind=all includes empty", func(t *testing.T) {
		q, err := Parse(batchListSpec(), url.Values{"kind": {"all"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(rows, q, day(10))
		if len(got) != 3 {
			t.Fatalf("got %d rows, want 3", len(got))
		}
	})

	t.Run("sort=newest desc default, tie-break batch id", func(t *testing.T) {
		q, err := Parse(batchListSpec(), url.Values{"kind": {"all"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(rows, q, day(10))
		want := []string{"b2", "b3", "b1"}
		for i, id := range want {
			if got[i].BatchID != id {
				t.Errorf("got[%d] = %s, want %s", i, got[i].BatchID, id)
			}
		}
	})

	t.Run("sort=cost", func(t *testing.T) {
		q, err := Parse(batchListSpec(), url.Values{"kind": {"all"}, "sort": {"cost"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(rows, q, day(10))
		if got[0].BatchID != "b2" {
			t.Errorf("got[0] = %s, want b2 (highest cost)", got[0].BatchID)
		}
	})

	t.Run("unknown parameter refused", func(t *testing.T) {
		if _, err := Parse(batchListSpec(), url.Values{"bogus": {"x"}}); err == nil {
			t.Error("want an error for an unknown parameter")
		}
	})

	t.Run("unknown kind value refused", func(t *testing.T) {
		if _, err := Parse(batchListSpec(), url.Values{"kind": {"bogus"}}); err == nil {
			t.Error("want an error for an unknown kind value")
		}
	})

	t.Run("notice and n accepted", func(t *testing.T) {
		q, err := Parse(batchListSpec(), url.Values{"notice": {"queued"}, "n": {"2"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if q.Notice != "queued" || q.N != "2" {
			t.Errorf("Notice/N = %s/%s, want queued/2", q.Notice, q.N)
		}
	})
}

// TestLists_OverviewBatches_DefaultIncludesUnavailableExcludesEmpty pins the
// conservative default scope. Explicit kind filters remain singular and exact.
func TestLists_OverviewBatches_DefaultIncludesUnavailableExcludesEmpty(t *testing.T) {
	rows := []BatchRow{
		{BatchID: "evaluation", Kind: batchKindEvaluation},
		{BatchID: "unavailable", Kind: batchKindUnavailable},
		{BatchID: "empty", Kind: batchKindEmpty},
	}
	spec := batchListSpec()
	eng := batchEngine()
	q, err := Parse(spec, url.Values{})
	if err != nil {
		t.Fatalf("Parse(default): %v", err)
	}
	got, _ := eng.Apply(rows, q, fixedNow(t)())
	defaultKinds := map[string]bool{}
	for _, row := range got {
		defaultKinds[row.Kind] = true
	}
	if len(got) != 2 || !defaultKinds[batchKindEvaluation] || !defaultKinds[batchKindUnavailable] || defaultKinds[batchKindEmpty] {
		t.Fatalf("default rows = %+v, want evaluation and unavailable only", got)
	}

	for _, kind := range []string{batchKindEvaluation, batchKindUnavailable, batchKindEmpty} {
		q, err := Parse(spec, url.Values{paramKind: {kind}})
		if err != nil {
			t.Fatalf("Parse(kind=%s): %v", kind, err)
		}
		got, _ := eng.Apply(rows, q, fixedNow(t)())
		if len(got) != 1 || got[0].Kind != kind {
			t.Errorf("kind=%s rows = %+v, want exactly that kind", kind, got)
		}
	}
	if _, err := Parse(spec, url.Values{paramKind: {batchKindEvaluation + "," + batchKindUnavailable}}); err == nil {
		t.Error("explicit comma-separated kind was accepted, want singular switch")
	}
}

func TestBatchSort_UnknownCost_LastBothDirections(t *testing.T) {
	rows := []BatchRow{
		{BatchID: "known-zero", Kind: batchKindEvaluation, ObservedM: 1, TotalCostUsd: 0},
		{BatchID: "partial", Kind: batchKindEvaluation, ObservedM: 2, CostUnknownN: 1, TotalCostUsd: 2},
		{BatchID: "known-high", Kind: batchKindEvaluation, ObservedM: 1, TotalCostUsd: 5},
		{BatchID: "unknown", Kind: batchKindEvaluation, ObservedM: 2, CostUnknownN: 2},
	}
	want := map[string][]string{
		"asc":  {"known-zero", "partial", "known-high", "unknown"},
		"desc": {"known-high", "partial", "known-zero", "unknown"},
	}
	for _, dir := range []string{"asc", "desc"} {
		t.Run(dir, func(t *testing.T) {
			q, err := Parse(batchListSpec(), url.Values{"sort": {"cost"}, "dir": {dir}})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			got, _ := batchEngine().Apply(rows, q, time.Time{})
			for i, id := range want[dir] {
				if got[i].BatchID != id {
					t.Fatalf("cost %s order[%d] = %q, want %q; full=%+v", dir, i, got[i].BatchID, id, got)
				}
			}
		})
	}
}

func assertStrSlice(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %q, want %q", label, i, got[i], want[i])
		}
	}
}
