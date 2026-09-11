package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval"
	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// viewLogf logs a batch or run skipped during a listing scan because its
// manifest/observation/meta could not be read (item 5) — a package var so
// tests can capture it without threading a logger through every read-model
// call (mirrors worker.go's Queue.logf).
var viewLogf = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "console: "+format+"\n", args...)
}

// windowSlack is the safety margin item 7d adds on top of a window (the
// worker's fixed 14-day window, or rowsSinceWindow's ?since= duration)
// before a batch is cheaply skipped by its manifest createdAt alone: a
// batch whose runs took a while can have its own createdAt slightly before
// the window's bare cutoff while still holding a run whose own StartedAt
// falls inside it.
const windowSlack = 2 * time.Hour

// ErrBatchNotFound and ErrRunNotFound are returned by the read model when
// an otherwise grammar-valid id names nothing in the bucket.
var (
	ErrBatchNotFound = errors.New("console: batch not found")
	ErrRunNotFound   = errors.New("console: run not found")
)

func manifestKey(batch string) string { return "batches/" + batch + "/manifest.json" }
func summaryKey(batch string) string  { return "batches/" + batch + "/summary.json" }
func doneKey(runID string) string     { return "runs/" + runID + "/done.json" }

// FailedCheck is one failed/blocked verification.json row (§8.4 FM-52's
// run-detail failedChecks field).
type FailedCheck struct {
	ID       string `json:"id"`
	Result   string `json:"result"`
	Expected string `json:"expected"`
	Observed string `json:"observed"`
	Source   string `json:"source"`
}

// verdictRunning is a run's Verdict while it has started.json but no
// done.json yet — run state comes from the bucket alone (§8.1).
const verdictRunning = "running"

// RunRow is the console's fully-resolved read model for one run — the
// superset GET /api/runs.md (light) and GET /api/runs/<runId>.md (full)
// both narrow down from (§8.4 FM-52). Verdict is verdictRunning for a run
// with no done.json (§7.5, §8.4: "no verdict"); ObserverState is one of
// observed|observing|not observed|observer off|observer disabled (§8.4),
// resolved by resolveObserverState.
type RunRow struct {
	RunID           string
	Batch           string
	Scenario        string
	Verdict         string
	StartedAt       time.Time
	DurationSec     float64
	CostUsd         float64
	CandidateSha256 string
	EvaluatorSha256 string
	DoneExists      bool
	ObserverState   string
	Observation     *observer.Observation
	OlderObsIDs     []string
	FailedChecks    []FailedCheck

	metaTaskResult string
}

// loadManifest reads and parses batches/<batch>/manifest.json, after
// checking batch against the FM-47 grammar (no key is built, no store call
// happens for an invalid id).
func loadManifest(ctx context.Context, store observer.ObjectStore, batch string) (farm.BatchManifest, error) {
	if !farm.ValidBatchID(batch) {
		return farm.BatchManifest{}, fmt.Errorf("%w: %q", ErrBatchNotFound, batch)
	}
	exists, _, err := store.Head(ctx, manifestKey(batch))
	if err != nil {
		return farm.BatchManifest{}, fmt.Errorf("console: head manifest: %w", err)
	}
	if !exists {
		return farm.BatchManifest{}, fmt.Errorf("%w: %s", ErrBatchNotFound, batch)
	}
	body, err := store.Get(ctx, manifestKey(batch))
	if err != nil {
		return farm.BatchManifest{}, fmt.Errorf("console: get manifest: %w", err)
	}
	var m farm.BatchManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return farm.BatchManifest{}, fmt.Errorf("console: parse manifest: %w", err)
	}
	return m, nil
}

// loadSummary reads batches/<batch>/summary.json when it exists; found is
// false (with a nil error) when the batch has not settled yet (§1.4: "a
// batch is running exactly when its manifest exists and its summary does
// not").
func loadSummary(ctx context.Context, store observer.ObjectStore, batch string) (summary farm.BatchSummary, found bool, err error) {
	exists, _, err := store.Head(ctx, summaryKey(batch))
	if err != nil {
		return farm.BatchSummary{}, false, fmt.Errorf("console: head summary: %w", err)
	}
	if !exists {
		return farm.BatchSummary{}, false, nil
	}
	body, err := store.Get(ctx, summaryKey(batch))
	if err != nil {
		return farm.BatchSummary{}, false, fmt.Errorf("console: get summary: %w", err)
	}
	var s farm.BatchSummary
	if err := json.Unmarshal(body, &s); err != nil {
		return farm.BatchSummary{}, false, fmt.Errorf("console: parse summary: %w", err)
	}
	return s, true, nil
}

// listBatchIDs enumerates every batch with a manifest.json, for the
// placeholder "/" batch-link list (§8.3 FM-51, S4) and for scanning a
// since-window across every batch (§8.4).
func listBatchIDs(ctx context.Context, store observer.ObjectStore) ([]string, error) {
	keys, err := store.List(ctx, "batches/")
	if err != nil {
		return nil, fmt.Errorf("console: list batches: %w", err)
	}
	seen := make(map[string]bool)
	var ids []string
	const suffix = "/manifest.json"
	for _, k := range keys {
		rest, ok := strings.CutSuffix(k, suffix)
		if !ok {
			continue
		}
		id := strings.TrimPrefix(rest, "batches/")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// §8.4 FM-52's observerState vocabulary.
const (
	observerStateObserved    = "observed"
	observerStateObserving   = "observing"
	observerStateNotObserved = "not observed"
	observerStateOff         = "observer off"
	observerStateDisabled    = "observer disabled"

	// ObserverOff is the shared "off" sentinel: farm.BatchManifest.Observer's
	// per-batch value (§1.4, §7.7) and ZCP_FARM_OBSERVER's console-wide kill
	// switch (§8.1, §8.5) both use this exact string.
	ObserverOff = "off"
)

// resolveObserverState implements §8.4's observerState vocabulary from
// statically-known bucket state plus queued, the run's live Queue.State
// (§8.5: JobQueued or JobRunning). Precedence: a run in flight reads
// "observing" (operator actions work regardless of the manifest field and
// the kill switch, §8.5 FM-53); a run with no done.json reads "not
// observed"; a run that has an observation reads "observed" whatever its
// manifest or the kill switch say — the label describes the run's data
// first; only then do the kill switch ("observer disabled") and the
// manifest ("observer off") explain why a finished run has none.
func resolveObserverState(consoleDisabled bool, manifestObserver string, doneExists, hasObservation, queued bool) string {
	if queued {
		return observerStateObserving
	}
	if !doneExists {
		return observerStateNotObserved
	}
	if hasObservation {
		return observerStateObserved
	}
	if consoleDisabled {
		return observerStateDisabled
	}
	if manifestObserver == "" || manifestObserver == ObserverOff {
		return observerStateOff
	}
	return observerStateNotObserved
}

// queueState reads runID's live queue state (§8.5) via the Server's Queue —
// "" when the run is neither queued nor running, or when no Queue is wired
// (a test server that doesn't exercise actions/the worker).
func (s *Server) queueState(runID string) string {
	if s.cfg.Queue == nil {
		return ""
	}
	return s.cfg.Queue.State(runID)
}

// runQueued reports whether queueState (nil-safe) marks runID as queued or
// running.
func runQueued(queueState func(runID string) string, runID string) bool {
	if queueState == nil {
		return false
	}
	st := queueState(runID)
	return st == JobQueued || st == JobRunning
}

// buildRunRow resolves one run's full read model (RunRow) from the bucket:
// done.json existence, meta.json/verification.json (via the observer
// package's Bundle readers, §7.2), the batch summary's row (§7.5's verdict
// rule), and the current + older observations (§7.5, §7.6).
func buildRunRow(ctx context.Context, store observer.ObjectStore, consoleObserverDisabled bool, batchID string, run farm.ManifestRun, manifestCreatedAt time.Time, manifestObserver string, summary farm.BatchSummary, summaryFound bool, queueState func(runID string) string) (RunRow, error) {
	row := RunRow{RunID: run.RunID, Batch: batchID, Scenario: run.Scenario, StartedAt: manifestCreatedAt}

	doneExists, _, err := store.Head(ctx, doneKey(run.RunID))
	if err != nil {
		return RunRow{}, fmt.Errorf("console: head done.json: %w", err)
	}
	row.DoneExists = doneExists
	queued := runQueued(queueState, run.RunID)
	if !doneExists {
		row.Verdict = settledOrRunning(summary, summaryFound, run.RunID)
		row.ObserverState = resolveObserverState(consoleObserverDisabled, manifestObserver, doneExists, false, queued)
		return row, nil
	}

	bundle, err := observer.NewSinkBundle(ctx, store, run.RunID)
	if err != nil {
		return RunRow{}, fmt.Errorf("console: build run row: new bundle: %w", err)
	}
	resultsDir, rdErr := observer.ResultsDir(bundle)

	var verification eval.VerificationDocument
	if rdErr == nil {
		if m, mErr := observer.LoadMeta(bundle, resultsDir); mErr == nil {
			if !m.StartedAt.IsZero() {
				row.StartedAt = m.StartedAt
			}
			row.DurationSec = time.Duration(m.Duration).Seconds()
			if m.Usage != nil {
				row.CostUsd = m.Usage.TotalCostUsd
			}
			if m.Task != nil {
				row.metaTaskResult = string(m.Task.Result)
			}
			row.CandidateSha256 = m.CandidateSha256
			row.EvaluatorSha256 = m.EvaluatorSha256
		}
		if v, vErr := observer.LoadVerification(bundle, resultsDir); vErr == nil {
			verification = v
		}
	}

	summaryResult, found := "", false
	if summaryFound {
		for _, r := range summary.Runs {
			if r.RunID == run.RunID {
				summaryResult, found = r.Result, true
				break
			}
		}
	}
	row.Verdict = observer.ResolveVerdict(summaryResult, found, row.metaTaskResult)

	for _, c := range verification.Checks {
		if c.Result == eval.CheckFailed || c.Result == eval.CheckBlocked {
			row.FailedChecks = append(row.FailedChecks, FailedCheck{
				ID: c.ID, Result: string(c.Result), Expected: c.Expected, Observed: c.Observed, Source: c.Source,
			})
		}
	}

	obsStore := observer.NewStore(store)
	obsIDs, err := obsStore.ListObservations(ctx, run.RunID)
	if err != nil {
		return RunRow{}, fmt.Errorf("console: list observations: %w", err)
	}
	if len(obsIDs) > 0 {
		row.OlderObsIDs = obsIDs[:len(obsIDs)-1]
		cur, err := obsStore.GetObservation(ctx, run.RunID, obsIDs[len(obsIDs)-1])
		if err != nil {
			return RunRow{}, fmt.Errorf("console: get current observation: %w", err)
		}
		row.Observation = &cur
	}

	row.ObserverState = resolveObserverState(consoleObserverDisabled, manifestObserver, doneExists, row.Observation != nil, queued)
	return row, nil
}

// settledOrRunning is the verdict of a run with no done.json: the result
// its batch summary already settled it with (blocked: no bundle,
// interrupted, a creation error — §1.4 FM-9), else verdictRunning while the
// batch is still waiting on it (§8.1).
func settledOrRunning(summary farm.BatchSummary, summaryFound bool, runID string) string {
	if summaryFound {
		for _, r := range summary.Runs {
			if r.RunID == runID && r.Result != "" {
				return r.Result
			}
		}
	}
	return verdictRunning
}

// findRunBatch locates the batch owning runID by scanning every batch's
// manifest for a matching run entry (the bucket layout carries no reverse
// index, §1.1) — runID is checked against the FM-47 grammar before any
// store call. A run id is always "<batchId>-<scenario>" (§1.1), so only a
// batch whose id is runID's own "<batch>-" prefix is ever worth a manifest
// load (item 7b) — every other batch is skipped before any store call for
// it.
func findRunBatch(ctx context.Context, store observer.ObjectStore, runID string) (batchID string, run farm.ManifestRun, manifest farm.BatchManifest, err error) {
	if !farm.ValidRunID(runID) {
		return "", farm.ManifestRun{}, farm.BatchManifest{}, fmt.Errorf("%w: %q", ErrRunNotFound, runID)
	}
	batches, err := listBatchIDs(ctx, store)
	if err != nil {
		return "", farm.ManifestRun{}, farm.BatchManifest{}, fmt.Errorf("console: find run batch: %w", err)
	}
	for _, b := range batches {
		if !strings.HasPrefix(runID, b+"-") {
			continue
		}
		m, err := loadManifest(ctx, store, b)
		if err != nil {
			continue
		}
		for _, r := range m.Runs {
			if r.RunID == runID {
				return b, r, m, nil
			}
		}
	}
	return "", farm.ManifestRun{}, farm.BatchManifest{}, fmt.Errorf("%w: %q", ErrRunNotFound, runID)
}

// loadRunRow resolves runID's full RunRow by first locating its batch
// (findRunBatch) and then building the row (runRowCached, cache.go). cache
// and sc are nil-safe (cache.go): nil means always read fresh, exactly the
// pre-cache behavior.
func loadRunRow(ctx context.Context, store observer.ObjectStore, consoleObserverDisabled bool, runID string, queueState func(runID string) string, cache *runCache, sc *summaryCache) (RunRow, error) {
	batchID, run, manifest, err := findRunBatch(ctx, store, runID)
	if err != nil {
		return RunRow{}, fmt.Errorf("console: load run row: %w", err)
	}
	createdAt, _ := time.Parse(time.RFC3339, manifest.CreatedAt)
	summary, summaryFound, err := resolveSummary(ctx, store, sc, batchID)
	if err != nil {
		return RunRow{}, fmt.Errorf("console: load run row: %w", err)
	}
	return runRowCached(ctx, store, cache, consoleObserverDisabled, batchID, run, createdAt, manifest.Observer, summary, summaryFound, queueState)
}

// evidenceSteps returns the sorted, deduplicated step numbers cited by
// obs's findings' evidence (excluding the step-0 CHECKS citation) — §8.4's
// run-detail "the step ranges its evidence cites".
func evidenceSteps(obs *observer.Observation) []int {
	if obs == nil {
		return nil
	}
	set := make(map[int]bool)
	for _, f := range obs.Findings {
		for _, e := range f.Evidence {
			if e.Step > 0 {
				set[e.Step] = true
			}
		}
	}
	out := make([]int, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// batchWindowRows resolves every run row of one batch — used both by
// GET /api/runs.md?batch=<id> and by the since-window scan across batches.
// cache and sc are nil-safe (cache.go).
func batchWindowRows(ctx context.Context, store observer.ObjectStore, consoleObserverDisabled bool, batchID string, queueState func(runID string) string, cache *runCache, sc *summaryCache) ([]RunRow, error) {
	manifest, err := loadManifest(ctx, store, batchID)
	if err != nil {
		return nil, fmt.Errorf("console: batch window rows: %w", err)
	}
	return batchWindowRowsWithManifest(ctx, store, consoleObserverDisabled, batchID, manifest, queueState, cache, sc)
}

// batchWindowRowsWithManifest is batchWindowRows over an already-loaded
// manifest — loadBatchRows (batches.go, item 7c) needs the manifest's own
// fields too, so it loads it once and reuses it here instead of paying for
// a second load. Rows are built in parallel, at most coldFillMaxInFlight at
// a time (fillRowsConcurrently, cache.go rule 5), in the manifest's own
// run order regardless of completion order. A run whose row can't be built
// (a corrupt observation or meta.json, item 5) is skipped and logged,
// never failing the rest of the batch; a manifest/summary read failure
// still fails the whole batch (the caller decides whether that aborts
// everything or just this batch). cache and sc are nil-safe (cache.go).
func batchWindowRowsWithManifest(ctx context.Context, store observer.ObjectStore, consoleObserverDisabled bool, batchID string, manifest farm.BatchManifest, queueState func(runID string) string, cache *runCache, sc *summaryCache) ([]RunRow, error) {
	createdAt, _ := time.Parse(time.RFC3339, manifest.CreatedAt)
	summary, summaryFound, err := resolveSummary(ctx, store, sc, batchID)
	if err != nil {
		return nil, fmt.Errorf("console: batch window rows: %w", err)
	}
	rows := fillRowsConcurrently(manifest.Runs, func(run farm.ManifestRun) (RunRow, error) {
		return runRowCached(ctx, store, cache, consoleObserverDisabled, batchID, run, createdAt, manifest.Observer, summary, summaryFound, queueState)
	})
	return rows, nil
}

// rowsSinceWindow collects every run row across every batch whose resolved
// StartedAt falls within [now-window, now] (§8.4: a run's own
// meta.json.startedAt decides window membership, which can differ from its
// batch's manifest createdAt within the same batch — so, unlike the
// worker's fixed 14-day scan (item 7d), this scan does not pre-skip a
// batch by manifest createdAt alone). A batch that fails to load (a
// corrupt manifest, item 5) is skipped and logged rather than failing the
// whole scan — only the top-level batches/ listing itself can fail the
// call outright. cache and sc are nil-safe (cache.go).
func rowsSinceWindow(ctx context.Context, store observer.ObjectStore, consoleObserverDisabled bool, window time.Duration, now time.Time, queueState func(runID string) string, cache *runCache, sc *summaryCache) ([]RunRow, error) {
	batches, err := listBatchIDs(ctx, store)
	if err != nil {
		return nil, fmt.Errorf("console: rows since window: %w", err)
	}
	since := now.Add(-window)
	var out []RunRow
	for _, b := range batches {
		manifest, err := loadManifest(ctx, store, b)
		if err != nil {
			viewLogf("skip batch %s: load manifest: %v", b, err)
			continue
		}
		rows, err := batchWindowRowsWithManifest(ctx, store, consoleObserverDisabled, b, manifest, queueState, cache, sc)
		if err != nil {
			viewLogf("skip batch %s: %v", b, err)
			continue
		}
		for _, row := range rows {
			if row.StartedAt.Before(since) || row.StartedAt.After(now) {
				continue
			}
			out = append(out, row)
		}
	}
	return out, nil
}
