// Package console: RED tests for the fix brief "farm-console-speed-2026-09-11"
// (plans/farm-console-speed-2026-09-11.md): a Server-owned run-row cache
// (cache.go) that answers a warm "/" / "/findings" / "/api/*" load from
// memory, and a bounded-parallel cold fill. Grouped here, at scan_test.go's
// call-counting-fake level, rather than split across pages_test.go/
// api_test.go/batches_test.go, because these tests exercise the cache's own
// TTL/invalidation rules directly.
package console

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
)

// mutableClock is a settable func() time.Time for tests that need to
// advance "now" between calls (the 15s/2m TTL rules) — fixedNow (console_
// test.go) is deliberately fixed and cannot express that.
type mutableClock struct {
	mu  sync.Mutex
	cur time.Time
}

func TestFillRowsConcurrently_RowReadFailure_PreservesManifestRun(t *testing.T) {
	t.Run("observer list failure", testS2bObserverFailure)
	t.Run("results list failure", testS2bResultsFailure)
	for _, filename := range []string{"task-prompt.txt", "transcript.jsonl"} {
		t.Run("missing "+filename, func(t *testing.T) { testS2bMissingRequired(t, filename) })
	}
	t.Run("meta disappears after results discovery", testS2bMetaDisappears)
	for _, tc := range []struct{ name, body string }{
		{name: "malformed json", body: "{malformed"},
		{name: "invalid duration", body: `{"duration":"not-a-duration","startedAt":"2026-09-11T12:00:00Z"}`},
		{name: "invalid startedAt", body: `{"duration":"1s","startedAt":"not-a-timestamp"}`},
	} {
		t.Run(tc.name, func(t *testing.T) { testS2bMalformed(t, tc.body, tc.name == "malformed json") })
	}
}

func testS2bObserverFailure(t *testing.T) {
	ctx, store, manifest := seedS2bPair(t, "s2b")
	rowsStore := &transientListStore{fakeStore: store, failPrefix: "runs/s2b-bad/observer/", failures: 1}
	cache := newRunCache(fixedNow(t))
	rows, err := batchWindowRowsWithManifest(ctx, rowsStore, false, "s2b", manifest, nil, cache, newSummaryCache(fixedNow(t)), nil)
	if err != nil || len(rows) != 2 || rows[0].RunID != "s2b-good" || rows[1].RunID != "s2b-bad" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if rows[1].Verdict != farm.VerdictFailed || !assessmentWorkUnavailable(rows[1]) || rows[1].DoneExists || rows[1].Observation != nil {
		t.Fatalf("fallback=%+v", rows[1])
	}
	actionStore := &transientListStore{fakeStore: store, failPrefix: "runs/s2b-bad/observer/", failures: 1}
	srv := NewServer(Config{Store: actionStore, Token: testToken, Now: fixedNow(t), Queue: NewQueue(func(context.Context, Job) error { return nil })})
	rr := doBearerPOST(t, srv.Handler(), "/b/s2b/observe", url.Values{"model": {"claude-sonnet-5"}, "all": {"1"}})
	if rr.Code != http.StatusAccepted || !strings.Contains(rr.Body.String(), `"reason":"assessment evidence unavailable"`) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	recovered, err := batchWindowRowsWithManifest(ctx, rowsStore, false, "s2b", manifest, nil, cache, newSummaryCache(fixedNow(t)), nil)
	if err != nil || assessmentWorkUnavailable(recovered[1]) || recovered[1].Observation == nil {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
}

func seedS2bPair(t *testing.T, batch string) (context.Context, *fakeStore, farm.BatchManifest) {
	t.Helper()
	store := newFakeStore()
	now := fixedNow(t)()
	seedBatch(t, store, batch, "claude-sonnet-5", []runFixture{{runID: batch + "-good", scenario: "good", startedAt: now.Add(-time.Hour), durationS: "5s", taskResult: "passed", done: true}, {runID: batch + "-bad", scenario: "bad", startedAt: now.Add(-time.Hour), durationS: "7s", taskResult: "failed", done: true}}, true, map[string]string{batch + "-good": "passed", batch + "-bad": "failed"})
	seedObservation(t, store, fixtureObservation(batch+"-good"))
	seedObservation(t, store, fixtureObservation(batch+"-bad"))
	manifest, err := loadManifest(context.Background(), store, batch)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return context.Background(), store, manifest
}

func testS2bResultsFailure(t *testing.T) {
	ctx, store, manifest := seedS2bPair(t, "s2b-results")
	wrapped := &transientListStore{fakeStore: store, failPrefix: "runs/s2b-results-bad/results/", failures: 1}
	detail, err := loadRunRow(ctx, wrapped, false, "s2b-results-bad", nil, newRunCache(fixedNow(t)), newSummaryCache(fixedNow(t)))
	if err != nil || !assessmentWorkUnavailable(detail) || detail.DoneExists {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
	wrapped = &transientListStore{fakeStore: store, failPrefix: "runs/s2b-results-bad/results/", failures: 1}
	rows, err := batchWindowRowsWithManifest(ctx, wrapped, false, "s2b-results", manifest, nil, newRunCache(fixedNow(t)), newSummaryCache(fixedNow(t)), nil)
	if err != nil || len(rows) != 2 || !assessmentWorkUnavailable(rows[1]) || rows[1].DoneExists {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func testS2bMalformed(t *testing.T, body string, corruptVerification bool) {
	t.Helper()
	ctx, store, manifest := seedS2bPair(t, "s2b-malformed")
	key := "runs/s2b-malformed-bad/results/" + testResultsTS + "/bad/"
	if corruptVerification {
		key += "verification.json"
	} else {
		key += "meta.json"
	}
	store.putText(t, key, body)
	rows, err := batchWindowRowsWithManifest(ctx, store, false, "s2b-malformed", manifest, nil, newRunCache(fixedNow(t)), newSummaryCache(fixedNow(t)), nil)
	if err != nil || len(rows) != 2 || !assessmentWorkUnavailable(rows[1]) || rows[1].DoneExists {
		t.Fatalf("batch rows=%+v err=%v", rows, err)
	}
	detail, err := loadRunRow(ctx, store, false, "s2b-malformed-bad", nil, newRunCache(fixedNow(t)), newSummaryCache(fixedNow(t)))
	if err != nil || !assessmentWorkUnavailable(detail) || detail.DoneExists || detail.Verdict != rows[1].Verdict {
		t.Fatalf("detail=%+v batch=%+v err=%v", detail, rows[1], err)
	}
}

func testS2bMissingRequired(t *testing.T, filename string) {
	t.Helper()
	ctx, store, manifest := seedS2bPair(t, "s2b-missing")
	key := "runs/s2b-missing-bad/results/" + testResultsTS + "/bad/" + filename
	store.mu.Lock()
	delete(store.objects, key)
	store.mu.Unlock()
	rows, err := batchWindowRowsWithManifest(ctx, store, false, "s2b-missing", manifest, nil, nil, nil, nil)
	if err != nil || len(rows) != 2 || !assessmentWorkUnavailable(rows[1]) || rows[1].DoneExists {
		t.Fatalf("batch rows=%+v err=%v", rows, err)
	}
	detail, err := loadRunRow(ctx, store, false, "s2b-missing-bad", nil, newRunCache(fixedNow(t)), newSummaryCache(fixedNow(t)))
	if err != nil || !assessmentWorkUnavailable(detail) || detail.DoneExists {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
}

func testS2bMetaDisappears(t *testing.T) {
	ctx, store, manifest := seedS2bPair(t, "s2b-meta-disappears")
	metaKey := "runs/s2b-meta-disappears-bad/results/" + testResultsTS + "/bad/meta.json"
	wrapped := &missingGetStore{fakeStore: store, missingKey: metaKey}
	rows, err := batchWindowRowsWithManifest(ctx, wrapped, false, "s2b-meta-disappears", manifest, nil, nil, nil, nil)
	if err != nil || len(rows) != 2 || !assessmentWorkUnavailable(rows[1]) || rows[1].DoneExists {
		t.Fatalf("meta disappearance rows=%+v err=%v, want unavailable fallback", rows, err)
	}
}

type transientListStore struct {
	*fakeStore
	failPrefix string
	failures   int
}

type missingGetStore struct {
	*fakeStore
	missingKey string
}

func (s *missingGetStore) Get(ctx context.Context, key string) ([]byte, error) {
	if key == s.missingKey {
		return nil, fmt.Errorf("temporary metadata disappearance: %w", os.ErrNotExist)
	}
	return s.fakeStore.Get(ctx, key)
}

func (s *transientListStore) List(ctx context.Context, prefix string) ([]string, error) {
	if prefix == s.failPrefix && s.failures > 0 {
		s.failures--
		return nil, errors.New("temporary observer read failure")
	}
	return s.fakeStore.List(ctx, prefix)
}

func newMutableClock(start time.Time) *mutableClock {
	return &mutableClock{cur: start}
}

func (c *mutableClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur
}

func (c *mutableClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cur = c.cur.Add(d)
}

// TestCache_SecondBatchesLoadReadsNothingForFinishedRuns pins rules 1-2: a
// second load of a finished, already-observed run issues zero Gets, Heads
// or Lists under runs/ — everything about it came from the cache.
func TestCache_SecondBatchesLoadReadsNothingForFinishedRuns(t *testing.T) {
	store := newFakeStore()
	now := fixedNow(t)
	seedBatch(t, store, "sc1", "claude-sonnet-5", []runFixture{
		{runID: "sc1-a", scenario: "a", startedAt: now().Add(-time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"sc1-a": "passed"})
	seedObservation(t, store, fixtureObservation("sc1-a"))

	cache := newRunCache(now)
	sc := newSummaryCache(now)

	if _, err := loadBatchRows(context.Background(), store, false, nil, cache, sc, nil); err != nil {
		t.Fatalf("first loadBatchRows: %v", err)
	}
	store.resetCallLog()

	if _, err := loadBatchRows(context.Background(), store, false, nil, cache, sc, nil); err != nil {
		t.Fatalf("second loadBatchRows: %v", err)
	}

	for _, k := range store.heads {
		if strings.HasPrefix(k, "runs/") {
			t.Errorf("second load Head'd %s, want zero run reads for a finished, fresh run", k)
		}
	}
	for _, k := range store.gets {
		if strings.HasPrefix(k, "runs/") {
			t.Errorf("second load Get'd %s, want zero run reads for a finished, fresh run", k)
		}
	}
	for _, k := range store.lists {
		if strings.HasPrefix(k, "runs/") {
			t.Errorf("second load Listed %s, want zero run reads for a finished, fresh run", k)
		}
	}
}

// TestCache_ObservationReReadAfterQueueJobCompletes pins rule 2a: the
// queue's completion hook invalidates exactly the run whose job just
// finished — a later load re-reads only that run's observation, never its
// immutable part, and never another run's observation.
func TestCache_ObservationReReadAfterQueueJobCompletes(t *testing.T) {
	obs := wkNewRecordingObserve()
	q := NewQueue(obs.fn)
	srv, store := newActionServer(t, actionServerOpts{queue: q})

	seedBatch(t, store, "cq1", "claude-sonnet-5", []runFixture{
		{runID: "cq1-x", scenario: "x", startedAt: fixedNow(t)().Add(-time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "cq1-y", scenario: "y", startedAt: fixedNow(t)().Add(-time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"cq1-x": "passed", "cq1-y": "passed"})
	seedObservation(t, store, fixtureObservation("cq1-x"))
	seedObservation(t, store, fixtureObservation("cq1-y"))

	if _, err := loadBatchRows(context.Background(), store, false, srv.queueState, srv.runCache, srv.summaryCache, srv.logf); err != nil {
		t.Fatalf("warm-up loadBatchRows: %v", err)
	}

	if err := q.Enqueue(context.Background(), Job{RunID: "cq1-x", Batch: "cq1", Model: "claude-sonnet-5"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	wkExpectCall(t, obs.calls)
	close(obs.release)
	if !wkEventually(t, func() bool { return q.State("cq1-x") == "" }) {
		t.Fatal("job for cq1-x never finished")
	}

	store.resetCallLog()
	if _, err := loadBatchRows(context.Background(), store, false, srv.queueState, srv.runCache, srv.summaryCache, srv.logf); err != nil {
		t.Fatalf("loadBatchRows after completion: %v", err)
	}

	if !store.calledList("runs/cq1-x/observer/") {
		t.Error("cq1-x's observation was not re-read after its queue job completed")
	}
	if store.calledList("runs/cq1-y/observer/") {
		t.Error("cq1-y's observation was re-read despite no completion for it")
	}
	if store.calledHead("runs/cq1-x/done.json") {
		t.Error("cq1-x's immutable part (done.json) was re-checked — it must stay cached forever once seen done")
	}
}

// TestCache_StepTextComputedOnceThenCached pins item 1/4 (FIX3): a run's
// raw step-search text — problems.go's StepTextFinder hook for the
// still-emitted search — is read from the bucket at most once for the
// console's lifetime. Unlike the observation part (cache.go rule 2, a 2m
// TTL) a run's steps never change once done.json exists (§7.6 FM-47), so
// there is no TTL to respect at all: a second call must read nothing from
// the bucket.
func TestCache_StepTextComputedOnceThenCached(t *testing.T) {
	store := newFakeStore()
	now := fixedNow(t)
	seedBatch(t, store, "st1", "off", []runFixture{
		{runID: "st1-a", scenario: "a", startedAt: now(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, false, nil)

	cache := newRunCache(now)
	text1, ok1 := cache.stepText(context.Background(), store, "st1-a")
	if !ok1 {
		t.Fatalf("stepText: ok = false, want true")
	}
	if !strings.Contains(text1, "discovered ok") {
		t.Errorf("stepText = %q, want it to contain fixtureTranscript's own tool result", text1)
	}
	store.resetCallLog()

	text2, ok2 := cache.stepText(context.Background(), store, "st1-a")
	if !ok2 || text2 != text1 {
		t.Errorf("second stepText = %q,%v want the same cached value", text2, ok2)
	}
	if len(store.gets) != 0 || len(store.lists) != 0 {
		t.Errorf("second stepText call touched the bucket: gets=%v lists=%v", store.gets, store.lists)
	}
}

// TestCache_StepTextMissingBundleCachedAsNotOK pins the same rule for a
// completed no-work run whose bundle can never be loaded (StepTextFinder's
// own doc comment: "ok is false when the run's bundle isn't available ...
// never an error") — a repeat call must not retry the doomed read either.
func TestCache_StepTextMissingBundleCachedAsNotOK(t *testing.T) {
	store := newFakeStore()
	now := fixedNow(t)
	cache := newRunCache(now)
	store.putJSON(t, doneKey("st-missing"), map[string]any{"runId": "st-missing", "scenarioId": "missing"})

	if _, ok := cache.stepText(context.Background(), store, "st-missing"); ok {
		t.Fatalf("stepText for a run with no bundle: ok = true, want false")
	}
	store.resetCallLog()
	if _, ok := cache.stepText(context.Background(), store, "st-missing"); ok {
		t.Fatalf("second stepText: ok = true, want false")
	}
	if len(store.gets) != 0 || len(store.lists) != 0 {
		t.Errorf("second stepText call for a missing bundle retried the bucket: gets=%v lists=%v", store.gets, store.lists)
	}
}

// TestCache_TransientStepRead_Recovers pins the cache contract that an
// unavailable or unfinished bundle read is not immutable evidence. Once the
// bundle becomes readable, a later search must retry and return its text.
func TestCache_TransientStepRead_Recovers(t *testing.T) {
	store := newFakeStore()
	now := fixedNow(t)
	cache := newRunCache(now)

	if _, ok := cache.stepText(context.Background(), store, "st-recover"); ok {
		t.Fatal("initial stepText: ok = true, want false for an unavailable bundle")
	}

	seedRun(t, store, runFixture{
		runID: "st-recover", scenario: "recover", startedAt: now(),
		durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true,
	})
	text, ok := cache.stepText(context.Background(), store, "st-recover")
	if !ok {
		t.Fatal("stepText after bundle became readable: ok = false, want true")
	}
	if !strings.Contains(text, "discovered ok") {
		t.Errorf("stepText after recovery = %q, want fixture transcript", text)
	}
}

// TestCache_UnfinishedBundle_RecoversAfterCompletion ensures a done marker
// observed before the result bundle is complete cannot freeze a partial row.
func TestCache_UnfinishedBundle_RecoversAfterCompletion(t *testing.T) {
	store := newFakeStore()
	now := fixedNow(t)
	seedBatch(t, store, "ub", "off", []runFixture{{
		runID: "ub-recover", scenario: "recover", startedAt: now(), done: false,
	}}, false, nil)
	store.putJSON(t, "runs/ub-recover/done.json", map[string]any{"runId": "ub-recover", "scenarioId": "recover"})

	cache := newRunCache(now)
	summaryCache := newSummaryCache(now)
	partial, err := loadRunRow(context.Background(), store, false, "ub-recover", nil, cache, summaryCache)
	if err != nil {
		t.Fatalf("partial loadRunRow: %v", err)
	}
	if !partial.DoneExists || partial.CostKnown || partial.StepCount != 0 {
		t.Fatalf("partial row = done=%v costKnown=%v steps=%d, want done with incomplete optional data", partial.DoneExists, partial.CostKnown, partial.StepCount)
	}

	seedRun(t, store, runFixture{
		runID: "ub-recover", scenario: "recover", startedAt: now(), durationS: "5s",
		costUsd: 0.25, taskResult: "passed", done: true,
	})
	complete, err := loadRunRow(context.Background(), store, false, "ub-recover", nil, cache, summaryCache)
	if err != nil {
		t.Fatalf("completed loadRunRow: %v", err)
	}
	if !complete.CostKnown || complete.CostUsd != 0.25 || complete.StepCount == 0 {
		t.Fatalf("completed row = costKnown=%v cost=%v steps=%d, want completed bundle data", complete.CostKnown, complete.CostUsd, complete.StepCount)
	}
}

// TestCache_PartReadFailure_DoesNotFreezePartialRow ensures a malformed
// immutable file is retried after the source is repaired instead of leaving
// the cache with a permanently incomplete row.
func TestCache_PartReadFailure_DoesNotFreezePartialRow(t *testing.T) {
	store := newFakeStore()
	now := fixedNow(t)
	seedBatch(t, store, "pr", "off", []runFixture{{
		runID: "pr-recover", scenario: "recover", startedAt: now(),
		durationS: "5s", costUsd: 0.4, taskResult: "passed", done: true,
	}}, false, nil)
	store.putText(t, "runs/pr-recover/results/20260911-104803/recover/meta.json", "{malformed")

	cache := newRunCache(now)
	summaryCache := newSummaryCache(now)
	partial, err := loadRunRow(context.Background(), store, false, "pr-recover", nil, cache, summaryCache)
	if err != nil {
		t.Fatalf("partial loadRunRow: %v", err)
	}
	if partial.CostKnown {
		t.Fatal("partial row unexpectedly reported a cost from malformed meta.json")
	}

	seedRun(t, store, runFixture{
		runID: "pr-recover", scenario: "recover", startedAt: now(), durationS: "5s",
		costUsd: 0.4, taskResult: "passed", done: true,
	})
	complete, err := loadRunRow(context.Background(), store, false, "pr-recover", nil, cache, summaryCache)
	if err != nil {
		t.Fatalf("repaired loadRunRow: %v", err)
	}
	if !complete.CostKnown || complete.CostUsd != 0.4 {
		t.Fatalf("repaired row = costKnown=%v cost=%v, want 0.4", complete.CostKnown, complete.CostUsd)
	}
}

// TestCache_ObservationReReadAfterTwoMinutes pins rule 2b: past the
// observation's 2-minute TTL it is re-read; the immutable part is never
// re-Head'd regardless.
func TestCache_ObservationReReadAfterTwoMinutes(t *testing.T) {
	store := newFakeStore()
	clock := newMutableClock(fixedNow(t)())

	seedBatch(t, store, "ct1", "claude-sonnet-5", []runFixture{
		{runID: "ct1-a", scenario: "a", startedAt: clock.now().Add(-time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ct1-a": "passed"})
	seedObservation(t, store, fixtureObservation("ct1-a"))

	cache := newRunCache(clock.now)
	sc := newSummaryCache(clock.now)

	if _, err := loadBatchRows(context.Background(), store, false, nil, cache, sc, nil); err != nil {
		t.Fatalf("warm-up: %v", err)
	}

	store.resetCallLog()
	clock.advance(90 * time.Second)
	if _, err := loadBatchRows(context.Background(), store, false, nil, cache, sc, nil); err != nil {
		t.Fatalf("at +90s: %v", err)
	}
	if store.calledList("runs/ct1-a/observer/") {
		t.Error("observation re-read before the 2-minute TTL elapsed")
	}
	if store.calledHead("runs/ct1-a/done.json") {
		t.Error("immutable part re-checked — done.json is never re-Head'd once cached")
	}

	store.resetCallLog()
	clock.advance(31 * time.Second) // total +121s, past the 2-minute TTL
	if _, err := loadBatchRows(context.Background(), store, false, nil, cache, sc, nil); err != nil {
		t.Fatalf("at +121s: %v", err)
	}
	if !store.calledList("runs/ct1-a/observer/") {
		t.Error("observation was not re-read past the 2-minute TTL")
	}
	if store.calledHead("runs/ct1-a/done.json") {
		t.Error("immutable part re-checked past the observation TTL — it must stay cached indefinitely")
	}
}

// TestCache_RunningRunRecheckedAfter15s pins rule 3: a run without
// done.json is re-Head'd at most every 15s; once done.json appears, the
// next check past that window fills its full row.
func TestCache_RunningRunRecheckedAfter15s(t *testing.T) {
	store := newFakeStore()
	clock := newMutableClock(fixedNow(t)())

	seedBatch(t, store, "rr1", "claude-sonnet-5", []runFixture{
		{runID: "rr1-a", scenario: "a", startedAt: clock.now(), done: false},
	}, false, nil)

	cache := newRunCache(clock.now)
	sc := newSummaryCache(clock.now)

	row0, err := loadRunRow(context.Background(), store, false, "rr1-a", nil, cache, sc)
	if err != nil {
		t.Fatalf("initial loadRunRow: %v", err)
	}
	if row0.DoneExists {
		t.Fatal("row0.DoneExists = true, want false (no done.json seeded yet)")
	}

	store.resetCallLog()
	clock.advance(10 * time.Second)
	if _, err := loadRunRow(context.Background(), store, false, "rr1-a", nil, cache, sc); err != nil {
		t.Fatalf("at +10s: %v", err)
	}
	if store.calledHead("runs/rr1-a/done.json") {
		t.Error("done.json re-checked before the 15s recheck window elapsed")
	}

	store.resetCallLog()
	clock.advance(6 * time.Second) // total +16s since the initial check
	if _, err := loadRunRow(context.Background(), store, false, "rr1-a", nil, cache, sc); err != nil {
		t.Fatalf("at +16s: %v", err)
	}
	if !store.calledHead("runs/rr1-a/done.json") {
		t.Error("done.json was not re-checked past the 15s recheck window")
	}

	// The run finishes: done.json and its bundle now exist.
	seedRun(t, store, runFixture{
		runID: "rr1-a", scenario: "a", startedAt: clock.now(), durationS: "5s", costUsd: 0.2, taskResult: "passed", done: true,
	})

	store.resetCallLog()
	clock.advance(16 * time.Second) // past the last check's own 15s window
	row, err := loadRunRow(context.Background(), store, false, "rr1-a", nil, cache, sc)
	if err != nil {
		t.Fatalf("after done: %v", err)
	}
	if !row.DoneExists {
		t.Fatal("row.DoneExists = false once done.json exists")
	}
	if row.Verdict != "passed" {
		t.Errorf("row.Verdict = %q, want passed", row.Verdict)
	}
}

// TestCache_SummaryCachedOnceExists pins rule 4: an absent batch summary is
// re-checked at most every 15s; once present it is cached forever.
func TestCache_SummaryCachedOnceExists(t *testing.T) {
	store := newFakeStore()
	clock := newMutableClock(fixedNow(t)())

	seedBatch(t, store, "su1", "claude-sonnet-5", []runFixture{
		{runID: "su1-a", scenario: "a", startedAt: clock.now().Add(-time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, false, nil) // no summary yet

	sc := newSummaryCache(clock.now)

	if _, found, err := sc.load(context.Background(), store, "su1"); err != nil || found {
		t.Fatalf("initial load: found=%v err=%v, want found=false", found, err)
	}

	store.resetCallLog()
	clock.advance(10 * time.Second)
	if _, found, err := sc.load(context.Background(), store, "su1"); err != nil || found {
		t.Fatalf("at +10s: found=%v err=%v", found, err)
	}
	if store.calledHead("batches/su1/summary.json") {
		t.Error("summary re-checked before the 15s recheck window elapsed")
	}

	store.resetCallLog()
	clock.advance(6 * time.Second) // total +16s
	if _, found, err := sc.load(context.Background(), store, "su1"); err != nil || found {
		t.Fatalf("at +16s: found=%v err=%v", found, err)
	}
	if !store.calledHead("batches/su1/summary.json") {
		t.Error("summary was not re-checked past the 15s recheck window")
	}

	store.putJSON(t, "batches/su1/summary.json", farm.BatchSummary{
		Batch: "su1", FinishedAt: clock.now().Format(time.RFC3339), EndedBy: "settled",
		Runs: []farm.SummaryRun{{RunID: "su1-a", Scenario: "a", Result: "passed"}},
	})

	store.resetCallLog()
	clock.advance(16 * time.Second)
	sum, found, err := sc.load(context.Background(), store, "su1")
	if err != nil || !found {
		t.Fatalf("after summary appears: found=%v err=%v", found, err)
	}
	if len(sum.Runs) != 1 {
		t.Fatalf("summary.Runs = %v, want 1 entry", sum.Runs)
	}

	store.resetCallLog()
	if _, found, err := sc.load(context.Background(), store, "su1"); err != nil || !found {
		t.Fatalf("second read after found: found=%v err=%v", found, err)
	}
	if store.calledHead("batches/su1/summary.json") || store.calledGet("batches/su1/summary.json") {
		t.Error("summary re-read once already found — it must be cached forever")
	}
}

// blockingStore wraps a fakeStore, blocking every Head/Get/List call under
// "runs/" until its gate is released, while tracking the peak number of
// such calls in flight at once — TestCache_ColdFillAtMostEightConcurrent's
// proof that a cold fill never exceeds coldFillMaxInFlight concurrent
// bucket reads. Calls outside "runs/" (batch manifest/summary) pass through
// unblocked, since rule 5's bound is about per-run row building, not the
// batch-level reads that precede it.
type blockingStore struct {
	*fakeStore
	mu       sync.Mutex
	inFlight int
	maxSeen  int
	gate     chan struct{}
}

func newBlockingStore(inner *fakeStore) *blockingStore {
	return &blockingStore{fakeStore: inner, gate: make(chan struct{})}
}

func (b *blockingStore) enter() {
	b.mu.Lock()
	b.inFlight++
	if b.inFlight > b.maxSeen {
		b.maxSeen = b.inFlight
	}
	b.mu.Unlock()
	<-b.gate
}

func (b *blockingStore) leave() {
	b.mu.Lock()
	b.inFlight--
	b.mu.Unlock()
}

func (b *blockingStore) currentInFlight() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inFlight
}

func (b *blockingStore) maxInFlight() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxSeen
}

func (b *blockingStore) Head(ctx context.Context, key string) (bool, int64, error) {
	if strings.HasPrefix(key, "runs/") {
		b.enter()
		defer b.leave()
	}
	return b.fakeStore.Head(ctx, key)
}

func (b *blockingStore) Get(ctx context.Context, key string) ([]byte, error) {
	if strings.HasPrefix(key, "runs/") {
		b.enter()
		defer b.leave()
	}
	return b.fakeStore.Get(ctx, key)
}

func (b *blockingStore) List(ctx context.Context, prefix string) ([]string, error) {
	if strings.HasPrefix(prefix, "runs/") {
		b.enter()
		defer b.leave()
	}
	return b.fakeStore.List(ctx, prefix)
}

// TestCache_ColdFillAtMostEightConcurrent pins rule 5: a cold fill over many
// runs never runs more than coldFillMaxInFlight bucket reads at once.
func TestCache_ColdFillAtMostEightConcurrent(t *testing.T) {
	inner := newFakeStore()
	now := fixedNow(t)()
	runs := make([]runFixture, 0, 12)
	for i := range 12 {
		id := fmt.Sprintf("cf1-r%02d", i)
		runs = append(runs, runFixture{
			runID: id, scenario: fmt.Sprintf("s%02d", i), startedAt: now.Add(-time.Hour),
			durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true,
		})
	}
	seedBatch(t, inner, "cf1", "claude-sonnet-5", runs, false, nil)

	manifest, err := loadManifest(context.Background(), inner, "cf1")
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}

	bs := newBlockingStore(inner)
	cache := newRunCache(fixedNow(t))
	sc := newSummaryCache(fixedNow(t))

	done := make(chan []RunRow, 1)
	go func() {
		rows, buildErr := batchWindowRowsWithManifest(context.Background(), bs, false, "cf1", manifest, nil, cache, sc, nil)
		if buildErr != nil {
			t.Errorf("batchWindowRowsWithManifest: %v", buildErr)
		}
		done <- rows
	}()

	if !wkEventually(t, func() bool { return bs.currentInFlight() == coldFillMaxInFlight }) {
		t.Fatalf("in-flight reads never reached the %d-way bound (max seen so far %d)", coldFillMaxInFlight, bs.maxInFlight())
	}
	close(bs.gate)

	var rows []RunRow
	select {
	case rows = <-done:
	case <-time.After(wkCallTimeout):
		t.Fatal("timed out waiting for the cold fill to finish after releasing the gate")
	}

	if len(rows) != len(runs) {
		t.Fatalf("got %d rows, want %d", len(rows), len(runs))
	}
	if bs.maxInFlight() > coldFillMaxInFlight {
		t.Errorf("max in-flight reads = %d, want <= %d", bs.maxInFlight(), coldFillMaxInFlight)
	}
}

// TestCache_OutputUnchanged pins the brief's headline constraint: the read
// model's output must not change. GET /api/runs.json and
// /api/findings.json are byte-identical cold (first load, building every
// row from the bucket) vs warm (second load, answered from the cache).
func TestCache_OutputUnchanged(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "ou1", "claude-sonnet-5", []runFixture{
		{
			runID: "ou1-a", scenario: "a", startedAt: now.Add(-2 * time.Hour), durationS: "5s", costUsd: 0.1,
			taskResult: "passed", done: true,
		},
		{
			runID: "ou1-b", scenario: "b", startedAt: now.Add(-time.Hour), durationS: "8s", costUsd: 0.2,
			taskResult: "failed", done: true, checks: [][5]string{{"c1", "failed", "x", "y", "verify"}},
		},
	}, true, map[string]string{"ou1-a": "passed", "ou1-b": "failed"})
	seedObservation(t, store, fixtureObservation("ou1-a"))

	seedBatch(t, store, "ou2", "claude-sonnet-5", []runFixture{
		{runID: "ou2-a", scenario: "a", startedAt: now.Add(-30 * time.Minute), done: false},
	}, false, nil)

	runsCold := doGET(t, h, "/api/runs.json?since=7d")
	findingsCold := doGET(t, h, "/api/findings.json?since=7d")
	if runsCold.Code != http.StatusOK || findingsCold.Code != http.StatusOK {
		t.Fatalf("cold: runs=%d findings=%d", runsCold.Code, findingsCold.Code)
	}

	runsWarm := doGET(t, h, "/api/runs.json?since=7d")
	findingsWarm := doGET(t, h, "/api/findings.json?since=7d")
	if runsWarm.Code != http.StatusOK || findingsWarm.Code != http.StatusOK {
		t.Fatalf("warm: runs=%d findings=%d", runsWarm.Code, findingsWarm.Code)
	}

	if runsCold.Body.String() != runsWarm.Body.String() {
		t.Errorf("GET /api/runs.json differs cold vs warm:\ncold: %s\nwarm: %s", runsCold.Body.String(), runsWarm.Body.String())
	}
	if findingsCold.Body.String() != findingsWarm.Body.String() {
		t.Errorf("GET /api/findings.json differs cold vs warm:\ncold: %s\nwarm: %s", findingsCold.Body.String(), findingsWarm.Body.String())
	}
	if !strings.Contains(runsCold.Body.String(), "ou1-a") {
		t.Errorf("cold runs.json missing seeded run: %s", runsCold.Body.String())
	}
}
