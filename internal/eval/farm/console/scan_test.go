// Package console: RED tests for the code-review fix brief's error-
// isolation and scan-cost items (5 and 7) — grouped here rather than split
// across api_test.go/pages_test.go/batches_test.go because they exercise
// view.go/batches.go's internal read-model functions directly, at the
// call-counting-fake level the other test files don't otherwise need.
package console

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// --- item 5: one bad object never takes a page down ---------------------

// TestAPI_CorruptObservationSkippedRestRenders pins item 5: a run whose
// current observation JSON cannot be parsed is skipped (and logged to
// stderr) rather than failing the whole listing — "/" and
// "/api/findings.md" both still answer 200 with the good run's data.
func TestAPI_CorruptObservationSkippedRestRenders(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "cb1", "claude-sonnet-5", []runFixture{
		{runID: "cb1-good", scenario: "good", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "cb1-bad", scenario: "bad", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.2, taskResult: "passed", done: true},
	}, true, map[string]string{"cb1-good": "passed", "cb1-bad": "passed"})
	seedObservation(t, store, fixtureObservation("cb1-good"))
	// A corrupt observation object under cb1-bad's observer/ prefix — valid
	// key grammar, invalid JSON body.
	store.putText(t, "runs/cb1-bad/observer/20260911T120000000Z-claude-sonnet-5.json", "{not json")

	rrRoot := doGET(t, h, "/")
	if rrRoot.Code != http.StatusOK {
		t.Fatalf("GET /: got %d, want 200, body=%s", rrRoot.Code, rrRoot.Body.String())
	}
	if !strings.Contains(rrRoot.Body.String(), "cb1") {
		t.Errorf("GET / missing the batch despite one corrupt run:\n%s", rrRoot.Body.String())
	}

	rrFindings := doGET(t, h, "/api/findings.md?since=24h")
	if rrFindings.Code != http.StatusOK {
		t.Fatalf("GET /api/findings.md: got %d, want 200, body=%s", rrFindings.Code, rrFindings.Body.String())
	}
	if !strings.Contains(rrFindings.Body.String(), "Tool returned stale data") {
		t.Errorf("GET /api/findings.md missing the good run's finding:\n%s", rrFindings.Body.String())
	}
}

// TestAPI_CorruptManifestSkipsOnlyThatBatch pins item 5: a batch whose
// manifest.json cannot be parsed is skipped entirely (logged to stderr),
// while every other batch still renders.
func TestAPI_CorruptManifestSkipsOnlyThatBatch(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "cm2-good", "claude-sonnet-5", []runFixture{
		{runID: "cm2-good-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"cm2-good-a": "passed"})
	store.putText(t, "batches/cm2-bad/manifest.json", "{not json")

	rr := doGET(t, h, "/")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "cm2-good") {
		t.Errorf("GET / missing the good batch:\n%s", body)
	}
}

// TestView_StoreListErrorFailsTheWholeListing pins item 5's other half: a
// genuine store error on the top-level batches/ List call still fails the
// whole listing (never silently renders an empty page).
func TestView_StoreListErrorFailsTheWholeListing(t *testing.T) {
	store := newFakeStore()
	store.failListOn("batches/", errors.New("bucket unreachable"))

	if _, err := loadBatchRows(context.Background(), store, false, nil); err == nil {
		t.Error("loadBatchRows with a failing batches/ List returned nil error, want the store error propagated")
	}
	if _, err := rowsSinceWindow(context.Background(), store, false, time.Hour, fixedNow(t)(), nil); err == nil {
		t.Error("rowsSinceWindow with a failing batches/ List returned nil error, want the store error propagated")
	}
}

// --- item 7: bucket scan cost ---------------------------------------------

// TestView_FindRunBatchTouchesOnlyMatchingBatch pins item 7b: findRunBatch
// only loads the manifest of a batch whose id is runID's "<batch>-"
// prefix — proven by the call-counting fake, not merely by the correct
// return value.
func TestView_FindRunBatchTouchesOnlyMatchingBatch(t *testing.T) {
	store := newFakeStore()
	seedBatch(t, store, "b1", "claude-sonnet-5", []runFixture{
		{runID: "b1-x", scenario: "x", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"b1-x": "passed"})
	seedBatch(t, store, "b2", "claude-sonnet-5", []runFixture{
		{runID: "b2-y", scenario: "y", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"b2-y": "passed"})

	batchID, run, _, err := findRunBatch(context.Background(), store, "b2-y")
	if err != nil {
		t.Fatalf("findRunBatch: %v", err)
	}
	if batchID != "b2" || run.RunID != "b2-y" {
		t.Fatalf("findRunBatch = (%s, %+v), want (b2, b2-y)", batchID, run)
	}
	if store.calledGet("batches/b1/manifest.json") {
		t.Error("findRunBatch loaded batch b1's manifest despite b2-y not matching its \"b1-\" prefix")
	}
	if !store.calledGet("batches/b2/manifest.json") {
		t.Error("findRunBatch never loaded batch b2's manifest")
	}
}

// TestView_LoadBatchRowsLoadsEachManifestOnce pins item 7c: loadBatchRows
// reads a batch's manifest exactly once, not once for its own fields and
// again inside the per-run scan.
func TestView_LoadBatchRowsLoadsEachManifestOnce(t *testing.T) {
	store := newFakeStore()
	seedBatch(t, store, "lo1", "claude-sonnet-5", []runFixture{
		{runID: "lo1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "lo1-b", scenario: "b", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"lo1-a": "passed", "lo1-b": "passed"})

	if _, err := loadBatchRows(context.Background(), store, false, nil); err != nil {
		t.Fatalf("loadBatchRows: %v", err)
	}

	got := 0
	for _, k := range store.gets {
		if k == "batches/lo1/manifest.json" {
			got++
		}
	}
	if got != 1 {
		t.Errorf("batches/lo1/manifest.json Get'd %d times, want exactly 1", got)
	}
}

// TestPages_SecondBatchesLoadCachesManifests pins item 7a: a manifest is
// cached in memory once loaded through a Server — a second "/" load issues
// no manifest GET.
func TestPages_SecondBatchesLoadCachesManifests(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedBatch(t, store, "mc1", "claude-sonnet-5", []runFixture{
		{runID: "mc1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"mc1-a": "passed"})

	if rr := doGET(t, h, "/"); rr.Code != http.StatusOK {
		t.Fatalf("first GET /: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	store.resetCallLog()
	if rr := doGET(t, h, "/"); rr.Code != http.StatusOK {
		t.Fatalf("second GET /: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	if store.calledGet("batches/mc1/manifest.json") {
		t.Error("second GET / re-fetched the batch manifest — item 7a's manifest cache is not being hit")
	}
}

// TestWorker_BatchWithinWindowSlackStillObserved pins item 7d: a batch
// created just past the bare 14-day window, but still inside the 2-hour
// slack, is still ticked.
func TestWorker_BatchWithinWindowSlackStillObserved(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	createdAt := now.Add(-(wkObserverWindow + time.Hour)) // 1h past 14 days, inside the 2h slack
	bucket := wkNewFakeBucket()
	bucket.put("batches/b1/manifest.json", wkManifestJSON(t, createdAt.Format(time.RFC3339), "claude-sonnet-5", "b1-scenario"))
	bucket.put("runs/b1-scenario/done.json", []byte(`{}`))

	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	w := NewWorker(WorkerConfig{Bucket: bucket, Queue: q, Now: wkFixedNow(now)})

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	job := wkExpectCall(t, obs.calls)
	if job.RunID != "b1-scenario" {
		t.Errorf("enqueued job.RunID = %q, want b1-scenario", job.RunID)
	}
}
