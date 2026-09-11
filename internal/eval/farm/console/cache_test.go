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
	"fmt"
	"net/http"
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
