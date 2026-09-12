package console

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// wkCallTimeout bounds how long a test waits for an expected (or absent)
// call before failing — generous enough for -race's slower scheduling,
// short enough that a genuine bug fails fast.
const wkCallTimeout = 2 * time.Second

// wkFakeBucket is an in-memory BucketReader, safe for concurrent use, that
// also records every key/prefix it was asked to read — TestWorker_
// InvalidIDsSkippedBeforeAnyKey inspects those logs directly to prove a
// key was never built for an invalid id.
type wkFakeBucket struct {
	mu      sync.Mutex
	objects map[string][]byte
	gets    []string
	heads   []string
	lists   []string

	// listErrOn/listErr injects a List error for one exact prefix (item
	// 6's "a list error skips the run" fixes) — "" means never.
	listErrOn string
	listErr   error
}

func wkNewFakeBucket() *wkFakeBucket {
	return &wkFakeBucket{objects: make(map[string][]byte)}
}

func (b *wkFakeBucket) put(key string, body []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objects[key] = body
}

func (b *wkFakeBucket) Get(_ context.Context, key string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gets = append(b.gets, key)
	data, ok := b.objects[key]
	if !ok {
		return nil, fmt.Errorf("wkFakeBucket: not found: %s", key)
	}
	return data, nil
}

func (b *wkFakeBucket) Put(_ context.Context, key string, body []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objects[key] = body
	return nil
}

func (b *wkFakeBucket) Head(_ context.Context, key string) (bool, int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.heads = append(b.heads, key)
	data, ok := b.objects[key]
	if !ok {
		return false, 0, nil
	}
	return true, int64(len(data)), nil
}

func (b *wkFakeBucket) List(_ context.Context, prefix string) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lists = append(b.lists, prefix)
	if b.listErrOn != "" && prefix == b.listErrOn {
		return nil, b.listErr
	}
	var keys []string
	for k := range b.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// failListOn makes a later List(prefix) call return err instead of
// scanning objects — item 6's "a list error skips the run" fixes.
func (b *wkFakeBucket) failListOn(prefix string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listErrOn = prefix
	b.listErr = err
}

func (b *wkFakeBucket) calledGet(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Contains(b.gets, key)
}

func (b *wkFakeBucket) calledHead(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Contains(b.heads, key)
}

func (b *wkFakeBucket) calledList(prefix string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Contains(b.lists, prefix)
}

// wkManifestJSON builds a minimal batches/<batch>/manifest.json body.
func wkManifestJSON(t *testing.T, createdAt, observerModel string, runIDs ...string) []byte {
	t.Helper()
	runs := make([]string, 0, len(runIDs))
	for _, id := range runIDs {
		runs = append(runs, fmt.Sprintf(`{"runId":%q,"scenario":"s"}`, id))
	}
	body := fmt.Sprintf(`{"batch":"b","createdAt":%q,"startedAt":%q,"set":"gate","observer":%q,"runs":[%s]}`,
		createdAt, createdAt, observerModel, strings.Join(runs, ","))
	return []byte(body)
}

// wkRecordingObserve is an ObserveFunc that reports every call on a
// channel (so a test can wait for it deterministically instead of racing
// Queue's background goroutine) and blocks until release is closed —
// giving the test full control over when a job "finishes".
type wkRecordingObserve struct {
	calls   chan Job
	release chan struct{}
}

func wkNewRecordingObserve() *wkRecordingObserve {
	return &wkRecordingObserve{
		calls:   make(chan Job, 16),
		release: make(chan struct{}),
	}
}

func (r *wkRecordingObserve) fn(_ context.Context, job Job) error {
	r.calls <- job
	<-r.release
	return nil
}

// wkExpectCall waits up to wkCallTimeout for a call and returns it.
func wkExpectCall(t *testing.T, calls chan Job) Job {
	t.Helper()
	select {
	case job := <-calls:
		return job
	case <-time.After(wkCallTimeout):
		t.Fatal("timed out waiting for an observe call")
		return Job{}
	}
}

// wkExpectNoCall fails if a call arrives within a short window.
func wkExpectNoCall(t *testing.T, calls chan Job) {
	t.Helper()
	select {
	case job := <-calls:
		t.Fatalf("unexpected observe call: %+v", job)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestQueue_AtMostThreeConcurrent pins §8.5 FM-53: Queue runs at most three
// jobs at once, running a fourth only once a slot frees.
func TestQueue_AtMostThreeConcurrent(t *testing.T) {
	obs := wkNewRecordingObserve()
	q := NewQueue(obs.fn)
	ctx := context.Background()

	for i := range 5 {
		if err := q.Enqueue(ctx, Job{RunID: fmt.Sprintf("batch-run%d", i), Batch: "batch"}); err != nil {
			t.Fatalf("Enqueue(%d): %v", i, err)
		}
	}

	for range 3 {
		wkExpectCall(t, obs.calls)
	}
	wkExpectNoCall(t, obs.calls)

	close(obs.release)

	for range 2 {
		wkExpectCall(t, obs.calls)
	}
}

// TestQueue_DuplicateRunRejected pins §8.5 FM-53: a run already queued or
// running is rejected with ErrAlreadyQueued (the 409 signal).
func TestQueue_DuplicateRunRejected(t *testing.T) {
	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	ctx := context.Background()

	if err := q.Enqueue(ctx, Job{RunID: "batch-run1", Batch: "batch"}); err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	wkExpectCall(t, obs.calls)

	err := q.Enqueue(ctx, Job{RunID: "batch-run1", Batch: "batch"})
	if err != ErrAlreadyQueued {
		t.Errorf("second Enqueue error = %v, want ErrAlreadyQueued", err)
	}
}

// TestQueue_BatchBusy pins §8.5: BatchBusy reports true while any run of
// that batch is queued or running, and false once none are.
func TestQueue_BatchBusy(t *testing.T) {
	obs := wkNewRecordingObserve()
	q := NewQueue(obs.fn)
	ctx := context.Background()

	if q.BatchBusy("batch") {
		t.Fatal("BatchBusy = true before any job was enqueued")
	}

	if err := q.Enqueue(ctx, Job{RunID: "batch-run1", Batch: "batch"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	wkExpectCall(t, obs.calls)

	if !q.BatchBusy("batch") {
		t.Error("BatchBusy = false while the run is running")
	}
	if q.BatchBusy("other-batch") {
		t.Error("BatchBusy = true for an unrelated batch")
	}

	close(obs.release)

	if !wkEventually(t, func() bool { return !q.BatchBusy("batch") }) {
		t.Error("BatchBusy stayed true after the run settled")
	}
}

// TestQueue_StatsCountsQueuedAndRunning pins the observer status line's
// read-only accessor (§8.3 FM-51): Stats reports how many jobs are running
// (up to wkMaxConcurrent) and how many are still waiting for a slot.
func TestQueue_StatsCountsQueuedAndRunning(t *testing.T) {
	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	ctx := context.Background()

	if queued, running := q.Stats(); queued != 0 || running != 0 {
		t.Fatalf("Stats before any job = (%d, %d), want (0, 0)", queued, running)
	}

	for i := range 5 {
		if err := q.Enqueue(ctx, Job{RunID: fmt.Sprintf("batch-run%d", i), Batch: "batch"}); err != nil {
			t.Fatalf("Enqueue(%d): %v", i, err)
		}
	}
	for range wkMaxConcurrent {
		wkExpectCall(t, obs.calls)
	}

	if !wkEventually(t, func() bool {
		queued, running := q.Stats()
		return queued == 2 && running == wkMaxConcurrent
	}) {
		queued, running := q.Stats()
		t.Errorf("Stats with 5 enqueued (3 running) = (%d, %d), want (2, %d)", queued, running, wkMaxConcurrent)
	}
}

// wkEventually polls cond until it's true or wkCallTimeout elapses.
func wkEventually(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(wkCallTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// wkFixedNow returns a func() time.Time that always returns t, for
// deterministic Worker tests.
func wkFixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// wkSeedBundle writes a positive-cost meta, proving meaningful work under
// §8.5 without needing transcript fixtures. A run whose batch ended before
// it started writes done.json and nothing else and is never eligible.
func wkSeedBundle(b *wkFakeBucket, runID string) {
	b.put("runs/"+runID+"/results/20260911T110000000Z/s/meta.json", []byte(`{"scenarioId":"s","usage":{"totalCostUsd":0.1}}`))
}

// TestWorker_ObservesDoneRunWithoutObservation pins §8.5 FM-53: a run whose
// batch names an observer model, that has done.json and no observation,
// gets enqueued with the manifest's model and Source "worker".
func TestWorker_ObservesDoneRunWithoutObservation(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	bucket := wkNewFakeBucket()
	bucket.put("batches/b1/manifest.json", wkManifestJSON(t, now.Add(-time.Hour).Format(time.RFC3339), "claude-sonnet-5", "b1-scenario"))
	bucket.put("runs/b1-scenario/done.json", []byte(`{}`))
	wkSeedBundle(bucket, "b1-scenario")

	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	w := NewWorker(WorkerConfig{Bucket: bucket, Queue: q, Now: wkFixedNow(now)})

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	job := wkExpectCall(t, obs.calls)
	if job.RunID != "b1-scenario" || job.Batch != "b1" || job.Model != "claude-sonnet-5" || job.Source != "worker" {
		t.Errorf("enqueued job = %+v, want RunID=b1-scenario Batch=b1 Model=claude-sonnet-5 Source=worker", job)
	}
}

// TestWorker_PreStoreFailure_DoesNotAutoRetry pins §8.5's process-local
// suppression: once an observation attempt fails before storing a result,
// later automatic ticks leave that run alone until an operator retries it.
func TestWorker_PreStoreFailure_DoesNotAutoRetry(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	bucket := wkNewFakeBucket()
	bucket.put("batches/b1/manifest.json", wkManifestJSON(t, now.Add(-time.Hour).Format(time.RFC3339), "claude-sonnet-5", "b1-failed"))
	bucket.put("runs/b1-failed/done.json", []byte(`{}`))
	wkSeedBundle(bucket, "b1-failed")

	calls := make(chan Job, 2)
	q := NewQueue(func(_ context.Context, job Job) error {
		calls <- job
		return fmt.Errorf("store unavailable")
	})
	if err := q.Enqueue(context.Background(), Job{RunID: "b1-failed", Batch: "b1", Model: "claude-sonnet-5", Source: sourceAction}); err != nil {
		t.Fatalf("seed failed attempt: %v", err)
	}
	wkExpectCall(t, calls)
	if !wkEventually(t, func() bool {
		_, failed := q.LastFailure("b1-failed")
		return q.State("b1-failed") == "" && failed
	}) {
		t.Fatal("seed attempt did not settle as a remembered pre-store failure")
	}

	w := NewWorker(WorkerConfig{Bucket: bucket, Queue: q, Now: wkFixedNow(now)})
	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	wkExpectNoCall(t, calls)
}

// TestWorker_SkipsRunWithoutDoneOrWithObservation pins §7.7/§8.5: a run
// with no done.json, and a run that already carries an observation, are
// both left alone.
func TestWorker_SkipsRunWithoutDoneOrWithObservation(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	bucket := wkNewFakeBucket()
	bucket.put("batches/b1/manifest.json", wkManifestJSON(t, now.Add(-time.Hour).Format(time.RFC3339), "claude-sonnet-5", "b1-no-done", "b1-observed"))
	// b1-no-done: no done.json at all.
	bucket.put("runs/b1-observed/done.json", []byte(`{}`))
	wkSeedBundle(bucket, "b1-observed")
	bucket.put("runs/b1-observed/observer/20260911T110000000Z-claude-sonnet-5.json", []byte(`{}`))

	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	w := NewWorker(WorkerConfig{Bucket: bucket, Queue: q, Now: wkFixedNow(now)})

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	wkExpectNoCall(t, obs.calls)
	if q.State("b1-no-done") != "" {
		t.Errorf("State(b1-no-done) = %q, want empty", q.State("b1-no-done"))
	}
	if q.State("b1-observed") != "" {
		t.Errorf("State(b1-observed) = %q, want empty", q.State("b1-observed"))
	}
}

// TestWorker_ManifestOffOrMissingNeverObserves pins §7.7 FM-48: "off" and a
// missing observer field are never observed automatically.
func TestWorker_ManifestOffOrMissingNeverObserves(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	bucket := wkNewFakeBucket()
	bucket.put("batches/off-batch/manifest.json", wkManifestJSON(t, now.Add(-time.Hour).Format(time.RFC3339), "off", "off-batch-scenario"))
	bucket.put("runs/off-batch-scenario/done.json", []byte(`{}`))
	wkSeedBundle(bucket, "off-batch-scenario")
	bucket.put("batches/no-field-batch/manifest.json", fmt.Appendf(nil, `{"batch":"no-field-batch","createdAt":%q,"startedAt":%q,"set":"gate","runs":[{"runId":"no-field-batch-scenario","scenario":"s"}]}`, now.Add(-time.Hour).Format(time.RFC3339), now.Add(-time.Hour).Format(time.RFC3339)))
	bucket.put("runs/no-field-batch-scenario/done.json", []byte(`{}`))
	wkSeedBundle(bucket, "no-field-batch-scenario")

	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	w := NewWorker(WorkerConfig{Bucket: bucket, Queue: q, Now: wkFixedNow(now)})

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	wkExpectNoCall(t, obs.calls)
}

// TestWorker_BatchOlderThan14DaysIgnored pins §8.5 FM-53: a batch whose
// manifest.createdAt is more than 14 days before Now() is ignored.
func TestWorker_BatchOlderThan14DaysIgnored(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	old := now.Add(-15 * 24 * time.Hour)
	bucket := wkNewFakeBucket()
	bucket.put("batches/old-batch/manifest.json", wkManifestJSON(t, old.Format(time.RFC3339), "claude-sonnet-5", "old-batch-scenario"))
	bucket.put("runs/old-batch-scenario/done.json", []byte(`{}`))
	wkSeedBundle(bucket, "old-batch-scenario")

	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	w := NewWorker(WorkerConfig{Bucket: bucket, Queue: q, Now: wkFixedNow(now)})

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	wkExpectNoCall(t, obs.calls)
}

// TestWorker_KillSwitchObservesNothing pins §8.1/§8.5: Disabled makes Tick
// a no-op even over an otherwise-observable run.
func TestWorker_KillSwitchObservesNothing(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	bucket := wkNewFakeBucket()
	bucket.put("batches/b1/manifest.json", wkManifestJSON(t, now.Add(-time.Hour).Format(time.RFC3339), "claude-sonnet-5", "b1-scenario"))
	bucket.put("runs/b1-scenario/done.json", []byte(`{}`))
	wkSeedBundle(bucket, "b1-scenario")

	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	w := NewWorker(WorkerConfig{Bucket: bucket, Queue: q, Now: wkFixedNow(now), Disabled: true})

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	wkExpectNoCall(t, obs.calls)
	if len(bucket.gets) != 0 || len(bucket.heads) != 0 {
		t.Errorf("Disabled Tick made bucket calls: gets=%v heads=%v", bucket.gets, bucket.heads)
	}
}

// TestWorker_InvalidIDsSkippedBeforeAnyKey pins §7.6 FM-47: an invalid
// batch id is never Get'd, and an invalid run id inside an otherwise-valid
// manifest never gets a done.json Head call — proven via the fake bucket's
// call log, not merely by absence of an enqueue.
func TestWorker_InvalidIDsSkippedBeforeAnyKey(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	bucket := wkNewFakeBucket()
	// Invalid batch id (uppercase, fails ^[a-z0-9][a-z0-9-]{0,62}$).
	bucket.put("batches/Bad_Batch/manifest.json", wkManifestJSON(t, now.Add(-time.Hour).Format(time.RFC3339), "claude-sonnet-5", "whatever"))
	// Valid batch, one valid run and one invalid run id.
	bucket.put("batches/b1/manifest.json", wkManifestJSON(t, now.Add(-time.Hour).Format(time.RFC3339), "claude-sonnet-5", "b1-good", "Invalid_Run!!"))
	bucket.put("runs/b1-good/done.json", []byte(`{}`))
	wkSeedBundle(bucket, "b1-good")

	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	w := NewWorker(WorkerConfig{Bucket: bucket, Queue: q, Now: wkFixedNow(now)})

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	job := wkExpectCall(t, obs.calls)
	if job.RunID != "b1-good" {
		t.Errorf("enqueued job.RunID = %q, want b1-good", job.RunID)
	}

	if bucket.calledGet("batches/Bad_Batch/manifest.json") {
		t.Error("Get was called for the invalid batch id's manifest key")
	}
	if bucket.calledHead("runs/Invalid_Run!!/done.json") {
		t.Error("Head was called for the invalid run id's done.json key")
	}
}

// wkShellQuote single-quotes s for embedding in a POSIX shell script.
func wkShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// wkPutSampleRunBundle populates bucket with a full, valid bundle at
// runs/<runID>/... by copying internal/eval/farm/observer/testdata/
// sample-run's fixture files — the same independently-authored fixture
// cmd/zcp's and observer's own tests use.
func wkPutSampleRunBundle(t *testing.T, bucket *wkFakeBucket, runID string) {
	t.Helper()
	const resultsDir = "results/20260911-104838/sample-scenario"
	for _, name := range []string{
		"task-prompt.txt", "transcript.jsonl", "meta.json",
		"verification.json", "self-review.md", "platform-snapshot.json",
	} {
		src := filepath.Join("..", "observer", "testdata", "sample-run", resultsDir, name)
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read fixture %s: %v", src, err)
		}
		bucket.put("runs/"+runID+"/"+resultsDir+"/"+name, data)
	}
}

// wkWriteEnvDumpingClaude writes an executable fake `claude` that ignores
// its input, dumps its own environment (one KEY=VALUE per line) to
// dumpFile, and prints a canned `--output-format json` result. dumpFile's
// path is baked into the script text directly (not passed via an env var)
// so the assertion under test — what env vars the child actually receives
// — isn't itself contaminated by the plumbing.
func wkWriteEnvDumpingClaude(t *testing.T, dir, dumpFile string) string {
	t.Helper()
	out, err := json.Marshal(map[string]any{
		"result":         "{}",
		"total_cost_usd": 0.0,
		"is_error":       false,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\ncat > /dev/null\nenv > " + wkShellQuote(dumpFile) + "\nprintf '%s' " + wkShellQuote(string(out)) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestBucketObserve_StoresErrorStatusAndDoesNotRetry pins FM-48: an
// observation with status "error" (here: a run bundle with no
// results/.../meta.json, so ResultsDir fails before any claude call) is
// stored exactly like any other, and Queue/the store never silently
// retries over it — proven here at the store layer: a second attempt at
// the same moment in time (same ObsID) is refused by the store's
// never-overwrite rule (§7.5) rather than clobbering the first.
func TestBucketObserve_StoresErrorStatusAndDoesNotRetry(t *testing.T) {
	bucket := wkNewFakeBucket()
	// Deliberately no results/.../meta.json under runs/broken-run/ — Observe
	// fails at ResultsDir, before any claude invocation.
	fixedNow := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

	fn := NewBucketObserveFunc(bucket, BucketObserveConfig{
		ClaudePath: "claude", // never reached
		OAuthToken: "test-token",
		Timeout:    time.Minute,
		Environ:    os.Environ,
		Now:        wkFixedNow(fixedNow),
	})

	job := Job{RunID: "broken-run", Batch: "batch1", Model: "claude-sonnet-5"}

	if err := fn(context.Background(), job); err != nil {
		t.Fatalf("first observe call: %v (an error-status observation must still be stored successfully)", err)
	}

	keys, err := bucket.List(context.Background(), "runs/broken-run/observer/")
	if err != nil {
		t.Fatalf("list observer keys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("observer keys = %v, want exactly 1", keys)
	}
	data, err := bucket.Get(context.Background(), keys[0])
	if err != nil {
		t.Fatalf("get %s: %v", keys[0], err)
	}
	var obs observer.Observation
	if err := json.Unmarshal(data, &obs); err != nil {
		t.Fatalf("parse stored observation: %v", err)
	}
	if obs.Status != "error" {
		t.Errorf("stored obs.Status = %q, want error", obs.Status)
	}

	// A second attempt at the exact same moment produces the same ObsID;
	// the store's never-overwrite rule refuses it rather than retrying
	// silently, and the bucket still carries exactly one observation.
	if err := fn(context.Background(), job); err == nil {
		t.Error("second observe call at the same ObsID succeeded, want it refused (store never overwrites)")
	}
	keysAfter, err := bucket.List(context.Background(), "runs/broken-run/observer/")
	if err != nil {
		t.Fatalf("list observer keys after retry: %v", err)
	}
	if len(keysAfter) != 1 {
		t.Errorf("observer keys after retry = %v, want still exactly 1 (no silent clobber/retry)", keysAfter)
	}
}

// TestBucketObserve_ChildNeverSeesSinkKeyOrConsoleToken pins §7.4 FM-44:
// even when the console process holds ZCP_FARM_S3_SECRET and
// ZCP_FARM_CONSOLE_TOKEN, the observer child's environment is exactly
// PATH, HOME, CLAUDE_CODE_OAUTH_TOKEN.
//
// non-parallel: t.Setenv
func TestBucketObserve_ChildNeverSeesSinkKeyOrConsoleToken(t *testing.T) {
	t.Setenv("ZCP_FARM_S3_SECRET", "super-secret-sink-key")
	t.Setenv("ZCP_FARM_CONSOLE_TOKEN", "super-secret-console-token")

	bucket := wkNewFakeBucket()
	wkPutSampleRunBundle(t, bucket, "sample-run")

	tmp := t.TempDir()
	dumpFile := filepath.Join(tmp, "child-env.txt")
	claudePath := wkWriteEnvDumpingClaude(t, tmp, dumpFile)

	fn := NewBucketObserveFunc(bucket, BucketObserveConfig{
		ClaudePath: claudePath,
		OAuthToken: "observer-oauth-token",
		Timeout:    time.Minute,
		Environ:    os.Environ,
		Now:        wkFixedNow(time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)),
	})

	if err := fn(context.Background(), Job{RunID: "sample-run", Batch: "batch1", Model: "claude-sonnet-5"}); err != nil {
		t.Fatalf("observe call: %v", err)
	}

	dump, err := os.ReadFile(dumpFile)
	if err != nil {
		t.Fatalf("read child env dump: %v", err)
	}
	childEnv := string(dump)

	if strings.Contains(childEnv, "super-secret-sink-key") {
		t.Error("child env carries the sink secret (ZCP_FARM_S3_SECRET)")
	}
	if strings.Contains(childEnv, "super-secret-console-token") {
		t.Error("child env carries the console token (ZCP_FARM_CONSOLE_TOKEN)")
	}
	if strings.Contains(childEnv, "ZCP_FARM_S3_SECRET=") || strings.Contains(childEnv, "ZCP_FARM_CONSOLE_TOKEN=") {
		t.Error("child env carries a ZCP_FARM_* key name at all")
	}
	if !strings.Contains(childEnv, "CLAUDE_CODE_OAUTH_TOKEN=observer-oauth-token") {
		t.Error("child env is missing CLAUDE_CODE_OAUTH_TOKEN")
	}
	if !strings.Contains(childEnv, "PATH=") {
		t.Error("child env is missing PATH")
	}
	if !strings.Contains(childEnv, "HOME=") {
		t.Error("child env is missing HOME")
	}
}

// TestQueue_JobOutlivesEnqueueContext pins §8.5: a job runs for the
// console's lifetime, not the enqueuer's — an action enqueues with the HTTP
// request's context, which is canceled as soon as the 202/303 is written.
func TestQueue_JobOutlivesEnqueueContext(t *testing.T) {
	t.Parallel()
	ctxErr := make(chan error, 1)
	q := NewQueue(func(ctx context.Context, _ Job) error {
		time.Sleep(20 * time.Millisecond)
		ctxErr <- ctx.Err()
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	if err := q.Enqueue(ctx, Job{RunID: "batch-run1", Batch: "batch"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	cancel()
	select {
	case err := <-ctxErr:
		if err != nil {
			t.Fatalf("job context error = %v after the enqueuer's context was canceled, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("observe never ran")
	}
}

// TestQueue_ObserveErrorIsLogged pins §8.5: a job that fails before an
// observation can be stored is never silent — the queue logs the run id and
// the error.
func TestQueue_ObserveErrorIsLogged(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var lines []string
	q := NewQueue(func(context.Context, Job) error { return fmt.Errorf("bucket unreachable") })
	q.logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}
	if err := q.Enqueue(context.Background(), Job{RunID: "batch-run1", Batch: "batch"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if !wkEventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, l := range lines {
			if strings.Contains(l, "batch-run1") && strings.Contains(l, "bucket unreachable") {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("no log line naming the run and the error; got %q", lines)
	}
}

// TestQueue_JobInfoAndLastFailure pins §8.5's live status: the queue
// exposes a job's model, source, state and times while it is queued or
// running, and keeps the last failure that happened before anything was
// stored, per run, until the next job for that run starts.
func TestQueue_JobInfoAndLastFailure(t *testing.T) {
	var mu sync.Mutex
	fail := true
	release := make(chan struct{})
	q := NewQueue(func(ctx context.Context, job Job) error {
		<-release
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return fmt.Errorf("claude: signal: killed")
		}
		return nil
	})
	at := time.Date(2026, 9, 11, 18, 52, 0, 0, time.UTC)
	q.now = wkFixedNow(at)
	ctx := context.Background()

	if _, ok := q.Job("batch-run1"); ok {
		t.Fatal("Job reported a job before any was enqueued")
	}
	if err := q.Enqueue(ctx, Job{RunID: "batch-run1", Batch: "batch", Model: "claude-opus-5", Source: "action"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if !wkEventually(t, func() bool { info, ok := q.Job("batch-run1"); return ok && info.State == JobRunning }) {
		t.Fatal("job never reported running")
	}
	info, _ := q.Job("batch-run1")
	if info.Model != "claude-opus-5" || info.Source != "action" || !info.EnqueuedAt.Equal(at) || !info.StartedAt.Equal(at) {
		t.Errorf("Job = %+v, want model/source/enqueued/started set", info)
	}

	close(release)
	if !wkEventually(t, func() bool { _, ok := q.Job("batch-run1"); return !ok }) {
		t.Fatal("job still reported after it settled")
	}
	f, ok := q.LastFailure("batch-run1")
	if !ok || f.Err != "claude: signal: killed" || !f.At.Equal(at) {
		t.Errorf("LastFailure = %+v, %v; want the pre-store error and its time", f, ok)
	}

	mu.Lock()
	fail = false
	mu.Unlock()
	if err := q.Enqueue(ctx, Job{RunID: "batch-run1", Batch: "batch", Model: "claude-sonnet-5", Source: "action"}); err != nil {
		t.Fatalf("second Enqueue: %v", err)
	}
	if !wkEventually(t, func() bool { _, ok := q.LastFailure("batch-run1"); return !ok }) {
		t.Error("LastFailure survived the start of the next job for the run")
	}
}

// TestWorker_SkipsRunWithEmptyBundle pins the never-started rule on the
// worker (§8.5): a run whose batch ended before it did any work has
// done.json but no results/ at all, so an observation of it can only fail
// on a missing task prompt. The worker leaves it alone instead of spending
// a job on it.
func TestWorker_SkipsRunWithEmptyBundle(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	bucket := wkNewFakeBucket()
	bucket.put("batches/b1/manifest.json", wkManifestJSON(t, now.Add(-time.Hour).Format(time.RFC3339), "claude-sonnet-5", "b1-empty", "b1-real"))
	bucket.put("runs/b1-empty/done.json", []byte(`{}`))
	bucket.put("runs/b1-real/done.json", []byte(`{}`))
	wkSeedBundle(bucket, "b1-real")

	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	w := NewWorker(WorkerConfig{Bucket: bucket, Queue: q, Now: wkFixedNow(now)})

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if job := wkExpectCall(t, obs.calls); job.RunID != "b1-real" {
		t.Errorf("enqueued %q, want b1-real (the only run with a bundle)", job.RunID)
	}
	select {
	case job := <-obs.calls:
		t.Errorf("worker also enqueued %q; a run with an empty bundle must be skipped", job.RunID)
	case <-time.After(100 * time.Millisecond):
	}
}
