package console

import (
	"context"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

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

	rows, err := loadBatchRows(context.Background(), store, false)
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
