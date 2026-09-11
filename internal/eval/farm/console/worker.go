// Package console implements the hosted view over the farm bucket
// (docs/spec-eval-farm.md §8). This file, worker.go, owns the observer
// queue and worker (§8.5): Queue bounds concurrent observations to three
// and rejects a run already queued or running; Worker discovers finished,
// unobserved runs on a schedule and enqueues them. The rest of package
// console (HTTP routes, pages, actions) is a separate slice landing in
// parallel — every unexported identifier in this file is prefixed wk so
// the two slices merge without collision.
package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// wkMaxConcurrent bounds how many jobs Queue runs at once (§8.5 FM-53: "one
// queue, at most three observations at a time").
const wkMaxConcurrent = 3

// wkJobTimeout bounds one job: the observer's own model call is capped at
// 5 minutes (§7.4); the rest is bucket reads and the final write.
const wkJobTimeout = 10 * time.Minute

// wkObserverWindow bounds how far back Worker.Tick looks for batches to
// observe (§8.5 FM-53: "createdAt is within 14 days").
const wkObserverWindow = 14 * 24 * time.Hour

// wkSourceWorker is Job.Source for a run the worker queued on its own
// schedule, as opposed to an operator action (§8.5).
const wkSourceWorker = "worker"

// Job is one queued or running observation (§8.5).
type Job struct {
	RunID  string
	Batch  string
	Model  string
	Source string
}

// ErrAlreadyQueued is returned by Queue.Enqueue when job.RunID is already
// queued or running (§8.5: "a run already queued or running answers 409").
var ErrAlreadyQueued = errors.New("console: run already queued or running")

// ObserveFunc performs one job's observation. Queue does not inspect its
// return value beyond logging-equivalent bookkeeping — a failed
// observation is still a fully formed Observation the func itself stores
// (see NewBucketObserveFunc); Queue never retries a job on its own (§7.7
// FM-48).
type ObserveFunc func(ctx context.Context, job Job) error

// wkJobState is Queue's per-run bookkeeping: which batch the run belongs to
// (for BatchBusy) and whether it has started running (for State).
type wkJobState struct {
	batch   string
	running bool
}

// Queue is the console's single observation queue: at most wkMaxConcurrent
// jobs run at once, and a run id is queued or running at most once at a
// time (§8.5 FM-53).
type Queue struct {
	mu      sync.Mutex
	jobs    map[string]*wkJobState
	sem     chan struct{}
	observe ObserveFunc
	logf    func(format string, args ...any)

	// OnComplete, when set, is called with a job's run id after that job
	// finishes (successfully or not) — the run-row cache's completion
	// hook (cache.go rule 2a, wired by NewServer): it invalidates the
	// run's cached observation so the next read re-fetches it, rather
	// than waiting on its TTL. Nil is a no-op (e.g. a test Queue built
	// without a Server).
	OnComplete func(runID string)
}

// NewQueue returns a Queue that executes every accepted job through
// observe.
func NewQueue(observe ObserveFunc) *Queue {
	return &Queue{
		jobs:    make(map[string]*wkJobState),
		sem:     make(chan struct{}, wkMaxConcurrent),
		observe: observe,
		logf:    wkStderrLogf,
	}
}

// wkStderrLogf is the queue's default log: a job that fails before an
// observation is stored (§7.7) is reported on stderr, never dropped.
func wkStderrLogf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "console: "+format+"\n", args...)
}

// Enqueue adds job to the queue and starts it running, once a concurrency
// slot frees up, in a new goroutine. Returns ErrAlreadyQueued when
// job.RunID is already queued or running — this is Queue's whole
// duplicate-run guard; the queue is otherwise unbounded in length. The job
// keeps ctx's values but not its cancellation: an action enqueues with the
// HTTP request's context, which ends as soon as the response is written,
// while the job must run to completion (bounded by wkJobTimeout).
func (q *Queue) Enqueue(ctx context.Context, job Job) error {
	q.mu.Lock()
	if _, exists := q.jobs[job.RunID]; exists {
		q.mu.Unlock()
		return ErrAlreadyQueued
	}
	q.jobs[job.RunID] = &wkJobState{batch: job.Batch}
	q.mu.Unlock()

	go q.wkRun(context.WithoutCancel(ctx), job)
	return nil
}

// wkRun executes job through the queue's ObserveFunc once a concurrency
// slot is free (wkMaxConcurrent bound, §8.5), then clears the run's tracked
// state regardless of outcome so a later Enqueue for the same run id (a
// manual re-observe once this one has settled) is accepted.
func (q *Queue) wkRun(ctx context.Context, job Job) {
	q.sem <- struct{}{}
	defer func() { <-q.sem }()

	q.mu.Lock()
	if st, ok := q.jobs[job.RunID]; ok {
		st.running = true
	}
	q.mu.Unlock()

	jobCtx, cancel := context.WithTimeout(ctx, wkJobTimeout)
	if err := q.observe(jobCtx, job); err != nil {
		q.logf("observe %s (model %s, source %s): %v", job.RunID, job.Model, job.Source, err)
	}
	cancel()

	q.mu.Lock()
	delete(q.jobs, job.RunID)
	q.mu.Unlock()

	if q.OnComplete != nil {
		q.OnComplete(job.RunID)
	}
}

// Queue states reported by Queue.State.
const (
	JobQueued  = "queued"
	JobRunning = "running"
)

// State reports runID's queue state: JobQueued, JobRunning, or "" when it
// is neither.
func (q *Queue) State(runID string) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	st, ok := q.jobs[runID]
	if !ok {
		return ""
	}
	if st.running {
		return JobRunning
	}
	return JobQueued
}

// BatchBusy reports whether any run of batch is queued or running (§8.5:
// "so does all=1 while any run of that batch is queued or running").
func (q *Queue) BatchBusy(batch string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, st := range q.jobs {
		if st.batch == batch {
			return true
		}
	}
	return false
}

// BucketReader is the bucket access NewBucketObserveFunc and Worker need —
// satisfied by *farm.SinkClient. Kept as a small interface here (rather
// than importing farm.SinkClient's concrete type into every call site) so
// tests never need a real HTTP server.
type BucketReader interface {
	List(ctx context.Context, prefix string) ([]string, error)
	Get(ctx context.Context, key string) ([]byte, error)
	Head(ctx context.Context, key string) (exists bool, size int64, err error)
	Put(ctx context.Context, key string, body []byte) error
}

// BucketObserveConfig carries NewBucketObserveFunc's fixed inputs: the
// observer invocation's claude binary and credential (§7.4), and the
// environment/clock seams Observe needs.
type BucketObserveConfig struct {
	ClaudePath string
	OAuthToken string
	Timeout    time.Duration
	Environ    func() []string
	Now        func() time.Time
}

// NewBucketObserveFunc returns the real ObserveFunc (§8.5): builds a
// SinkBundle over the run, reads the optional
// batches/<batch>/summary.json, runs observer.Observe, and stores the
// result via observer.Store.PutObservation. An observation with status
// "error" is stored exactly like any other (FM-48) — NewBucketObserveFunc
// returns an error to the Queue only when the bundle can't be constructed
// or the store write itself fails, never merely because the observation's
// own status is "error" or "unparsed".
func NewBucketObserveFunc(bucket BucketReader, cfg BucketObserveConfig) ObserveFunc {
	return func(ctx context.Context, job Job) error {
		bundle, err := observer.NewSinkBundle(ctx, bucket, job.RunID)
		if err != nil {
			return fmt.Errorf("console: observe %s: %w", job.RunID, err)
		}

		var summaryJSON []byte
		if job.Batch != "" {
			if data, getErr := bucket.Get(ctx, "batches/"+job.Batch+"/summary.json"); getErr == nil {
				summaryJSON = data
			}
		}

		obs := observer.Observe(ctx, bundle, observer.ObserveConfig{
			RunID:       job.RunID,
			Model:       job.Model,
			ClaudePath:  cfg.ClaudePath,
			OAuthToken:  cfg.OAuthToken,
			Timeout:     cfg.Timeout,
			Environ:     cfg.Environ,
			Now:         cfg.Now,
			SummaryJSON: summaryJSON,
		})

		store := observer.NewStore(bucket)
		if err := store.PutObservation(ctx, job.RunID, obs); err != nil {
			return fmt.Errorf("console: store observation for %s: %w", job.RunID, err)
		}
		return nil
	}
}

// WorkerConfig carries Worker's fixed inputs (§8.5).
type WorkerConfig struct {
	Bucket BucketReader
	Queue  *Queue
	// Disabled is the ZCP_FARM_OBSERVER=off kill switch (§8.1, §8.5):
	// makes Tick a no-op.
	Disabled bool
	// Now defaults to time.Now when nil.
	Now func() time.Time
}

// Worker discovers finished, unobserved runs and queues them on a schedule
// (§8.5 FM-53).
type Worker struct {
	cfg WorkerConfig
}

// NewWorker returns a Worker over cfg.
func NewWorker(cfg WorkerConfig) *Worker {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Worker{cfg: cfg}
}

// Tick runs one worker pass (§8.5 FM-53): lists batches/, reads each
// manifest, keeps batches whose createdAt is within the 14-day window and
// whose observer names a model (not "off", not empty), and enqueues each
// of their runs that has done.json, carries no observation yet, and has no
// existing queue state. A no-op when the worker is Disabled (the
// ZCP_FARM_OBSERVER=off kill switch, §8.1). Invalid batch or run ids are
// skipped before any key beyond the batches/ listing itself is built (§7.6
// FM-47).
func (w *Worker) Tick(ctx context.Context) error {
	if w.cfg.Disabled {
		return nil
	}

	keys, err := w.cfg.Bucket.List(ctx, "batches/")
	if err != nil {
		return fmt.Errorf("console: list batches: %w", err)
	}

	now := w.cfg.Now()
	for _, batch := range wkDistinctBatchIDs(keys) {
		if !farm.ValidBatchID(batch) {
			continue
		}
		w.wkTickBatch(ctx, batch, now)
	}
	return nil
}

// wkTickBatch handles one valid batch id: reads its manifest, applies the
// observer-model and 14-day-window gates, then walks its runs.
func (w *Worker) wkTickBatch(ctx context.Context, batch string, now time.Time) {
	manifestBytes, err := w.cfg.Bucket.Get(ctx, "batches/"+batch+"/manifest.json")
	if err != nil {
		return
	}
	var manifest farm.BatchManifest
	if jsonErr := json.Unmarshal(manifestBytes, &manifest); jsonErr != nil {
		return
	}
	if manifest.Observer == "" || manifest.Observer == "off" {
		return
	}
	createdAt, err := time.Parse(time.RFC3339, manifest.CreatedAt)
	if err != nil {
		return
	}
	if now.Sub(createdAt) > wkObserverWindow+windowSlack {
		return
	}

	for _, run := range manifest.Runs {
		if !farm.ValidRunID(run.RunID) {
			continue
		}
		w.wkTickRun(ctx, batch, run.RunID, manifest.Observer)
	}
}

// wkTickRun handles one valid run id: enqueues it iff done.json exists,
// the queue has no state for it already, and it carries no observation
// yet. Queue.State is checked before ListObservations (item 6) — cheaper,
// and a run already queued or running never needs its observations
// listed at all; a list error skips the run rather than risking a
// duplicate enqueue on doubt.
func (w *Worker) wkTickRun(ctx context.Context, batch, runID, model string) {
	doneExists, _, err := w.cfg.Bucket.Head(ctx, "runs/"+runID+"/done.json")
	if err != nil || !doneExists {
		return
	}

	if w.cfg.Queue.State(runID) != "" {
		return
	}

	store := observer.NewStore(w.cfg.Bucket)
	obsIDs, err := store.ListObservations(ctx, runID)
	if err != nil || len(obsIDs) > 0 {
		return
	}

	_ = w.cfg.Queue.Enqueue(ctx, Job{RunID: runID, Batch: batch, Model: model, Source: wkSourceWorker})
}

// Run ticks every interval until ctx is done.
func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = w.Tick(ctx)
		}
	}
}

// wkDistinctBatchIDs extracts each distinct "<id>" from a "batches/"
// listing's keys ("batches/<id>/...", §1.1), with no further network call
// — grammar validation happens in the caller, before any per-batch key is
// built (§7.6 FM-47).
func wkDistinctBatchIDs(keys []string) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, key := range keys {
		rest := strings.TrimPrefix(key, "batches/")
		idx := strings.IndexByte(rest, '/')
		if idx <= 0 {
			continue
		}
		id := rest[:idx]
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
