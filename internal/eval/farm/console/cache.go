// Package console: this file implements the run-row cache (fix brief
// "farm-console-speed-2026-09-11"): a "/" load, `/findings`, and the
// `/api/*` endpoints rebuild every run's row from the bucket on every
// request even though a run is immutable once its done.json exists (§7.6
// FM-47) and the console itself is the only writer of observations (§8.5).
// runCache caches a run's immutable row data indefinitely and its
// observation separately, on its own TTL/invalidation; summaryCache does
// the same for a batch's summary.json. Neither cache changes the read
// model's output — resolveSummary/runRowCached fall back to the always-
// fresh view.go functions when given a nil cache, so a caller that builds
// no cache (a bare unit test) behaves exactly as before this brief.
package console

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/eval"
	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

const (
	// obsCacheTTL bounds how long a run's cached observation part is
	// served before it is re-read (rule 2b).
	obsCacheTTL = 2 * time.Minute
	// notDoneRecheck bounds how often a run without done.json is re-Head'd
	// (rule 3).
	notDoneRecheck = 15 * time.Second
	// summaryRecheck bounds how often an absent batch summary.json is
	// re-checked (rule 4).
	summaryRecheck = 15 * time.Second
	// coldFillMaxInFlight bounds a cold fill's concurrent row builds
	// (rule 5).
	coldFillMaxInFlight = 8
)

// cachedImmutable is a run's row data that is valid forever once done.json
// exists (rule 1): everything meta.json/verification.json give us, since
// neither file is written again after done.json (§7.6 FM-47). Verdict
// itself is NOT stored here — it is recombined at read time (combineRow)
// from metaTaskResult plus the batch summary's row for this run, which is
// cached separately (summaryCache) and can still be absent when this part
// is first filled.
type cachedImmutable struct {
	Scenario         string
	StartedAt        time.Time
	DurationSec      float64
	CostUsd          float64
	CostKnown        bool
	CandidateSha256  string
	EvaluatorSha256  string
	FailedChecks     []FailedCheck
	StepCount        int
	ServiceHostnames []string
	metaTaskResult   string
	// cacheable is true only when the immutable result files were read as a
	// complete bundle. A done marker can race the final uploads; retaining
	// that partial view would permanently hide data that appears moments
	// later.
	cacheable bool
}

// cachedObservation is a run's observation-part cache (rule 2): the
// current observation plus older ids, and when it was last read from the
// bucket.
type cachedObservation struct {
	obs         *observer.Observation
	olderObsIDs []string
	fetchedAt   time.Time
}

// stepTextCacheValue is one run's cached raw step-search text (item 1/4,
// FIX3): a run is immutable once done.json exists (§7.6 FM-47), so unlike
// the observation part this has no TTL — computed at most once for the
// console's lifetime. ok is cached too (false for a run whose bundle can
// never be loaded), so a doomed read is never retried either.
type stepTextCacheValue struct {
	text string
	ok   bool
}

// runCacheEntry is one run's cache slot. immutable is nil until done.json
// is observed to exist, then permanent; obs is re-read per rule 2's
// TTL/invalidation; notDoneCheckedAt bounds how often a not-yet-done run's
// done.json is re-Head'd (rule 3) and is never consulted again once
// immutable != nil; stepText is filled lazily, only when something actually
// asks for it (StepTextFinder's still-emitted search touches only the
// newest build's own candidate runs, never every run in the cache).
type runCacheEntry struct {
	mu sync.Mutex

	notDoneCheckedAt time.Time
	immutable        *cachedImmutable
	obs              *cachedObservation
	stepText         *stepTextCacheValue
}

// runCache is the Server-owned run-row cache, keyed by run id.
type runCache struct {
	now func() time.Time

	mu      sync.Mutex
	entries map[string]*runCacheEntry
}

func newRunCache(now func() time.Time) *runCache {
	if now == nil {
		now = time.Now
	}
	return &runCache{now: now, entries: make(map[string]*runCacheEntry)}
}

func (c *runCache) entry(runID string) *runCacheEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[runID]
	if !ok {
		e = &runCacheEntry{}
		c.entries[runID] = e
	}
	return e
}

// invalidateObservation drops runID's cached observation part (rule 2a:
// the queue's completion hook, wired by NewServer to Queue.OnComplete) so
// the next read re-fetches it from the bucket. A no-op for a run with no
// cache entry yet.
func (c *runCache) invalidateObservation(runID string) {
	c.mu.Lock()
	e, ok := c.entries[runID]
	c.mu.Unlock()
	if !ok {
		return
	}
	e.mu.Lock()
	e.obs = nil
	e.mu.Unlock()
}

// rawStepSearchText assembles runID's full, searchable step text —
// problems.go's StepTextFinder contract: "the task prompt plus every
// step's agent/thinking text and every tool call's input JSON and result,
// undecoded exactly as §7.3's digest quotes it". BuildSteps (observer/
// steps.go) always numbers the task prompt as step 1's own StepUser text,
// so that is the only "user" step included — a resumed segment's later
// synthesized user-sim step carries no ZCP output worth searching and is
// skipped, unlike the digest's own STEPS section (§7.3), which is a
// display transcript rather than a search corpus and so keeps every step.
// Tool input/result are used verbatim (Step.ToolInputJSON is already the
// raw compacted JSON, never decoded and re-encoded, §7.3) and, unlike the
// digest, never truncated — a search corpus must not lose the very text a
// still-emitted anchor could be hiding in.
func rawStepSearchText(steps []observer.Step) string {
	var b strings.Builder
	for _, s := range steps {
		switch s.Kind {
		case observer.StepUser:
			if s.N == 1 {
				b.WriteString(s.Text)
				b.WriteByte('\n')
			}
		case observer.StepAgent, observer.StepThinking:
			b.WriteString(s.Text)
			b.WriteByte('\n')
		case observer.StepTool:
			b.WriteString(s.ToolInputJSON)
			b.WriteByte('\n')
			b.WriteString(s.ToolResultText)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// stepText resolves runID's raw step-search text through the cache (item
// 1/4, FIX3): computed at most once per run for the console's lifetime — a
// run is immutable once done.json exists (§7.6 FM-47), so there is nothing
// to invalidate the way rule 2 invalidates an observation. A run whose
// bundle can't be loaded (never seeded, or a corrupt/missing input file)
// caches ok=false too, so a repeat still-emitted search never retries a
// doomed read.
func (c *runCache) stepText(ctx context.Context, store observer.ObjectStore, runID string) (string, bool) {
	e := c.entry(runID)

	e.mu.Lock()
	cached := e.stepText
	e.mu.Unlock()
	if cached != nil {
		return cached.text, cached.ok
	}

	var v stepTextCacheValue
	cacheNegative := false
	if steps, err := loadSteps(ctx, store, runID); err == nil {
		v = stepTextCacheValue{text: rawStepSearchText(steps), ok: true}
	} else if errors.Is(err, os.ErrNotExist) || errors.Is(err, observer.ErrResultsNotFound) {
		// A completed run with no result bundle is a terminal no-work run;
		// avoid paying the same doomed lookup repeatedly. Before done.json,
		// the bundle is still being uploaded and the failed read is transient.
		done, _, headErr := store.Head(ctx, doneKey(runID))
		if headErr == nil && done {
			cacheNegative = true
			v = stepTextCacheValue{}
		}
	}

	// A failed read is transient evidence, not a terminal absence. Keep the
	// cache empty so an unfinished bundle can be discovered on a later pass.
	if v.ok || cacheNegative {
		e.mu.Lock()
		e.stepText = &v
		e.mu.Unlock()
	}
	return v.text, v.ok
}

// row resolves run's RunRow through the cache (rules 1-3). No mutex is
// held across a store call (CLAUDE.md: copy under lock, release, then I/O):
// each branch below snapshots what it needs, unlocks, does its bucket
// reads, then locks again only to write the result back.
func (c *runCache) row(ctx context.Context, store observer.ObjectStore, consoleObserverDisabled bool, batchID string, run farm.ManifestRun, bc batchContext, summary farm.BatchSummary, summaryFound bool, queueState func(runID string) string) (RunRow, error) {
	e := c.entry(run.RunID)
	now := c.now()
	queued := runQueued(queueState, run.RunID)

	e.mu.Lock()
	imm := e.immutable
	notDoneCheckedAt := e.notDoneCheckedAt
	e.mu.Unlock()

	if imm == nil {
		if !notDoneCheckedAt.IsZero() && now.Sub(notDoneCheckedAt) < notDoneRecheck {
			return notDoneRow(batchID, run, bc, summary, summaryFound, consoleObserverDisabled, queued, now), nil
		}

		doneExists, _, err := store.Head(ctx, doneKey(run.RunID))
		if err != nil {
			return RunRow{}, fmt.Errorf("console: cache: head done.json: %w", err)
		}
		if !doneExists {
			e.mu.Lock()
			e.notDoneCheckedAt = now
			e.mu.Unlock()
			return notDoneRow(batchID, run, bc, summary, summaryFound, consoleObserverDisabled, queued, now), nil
		}

		built, err := fetchImmutablePart(ctx, store, run, bc.CreatedAt)
		if err != nil {
			return RunRow{}, err
		}
		obsPart, err := fetchObservationPart(ctx, store, run.RunID)
		if err != nil {
			return RunRow{}, err
		}
		obsPart.fetchedAt = now
		e.mu.Lock()
		if built.cacheable {
			e.immutable = &built
		}
		e.obs = &obsPart
		e.mu.Unlock()
		return combineRow(batchID, run, &built, &obsPart, bc, summary, summaryFound, consoleObserverDisabled, queued, now), nil
	}

	e.mu.Lock()
	obsCache := e.obs
	e.mu.Unlock()

	if obsCache == nil || now.Sub(obsCache.fetchedAt) >= obsCacheTTL {
		refreshed, err := fetchObservationPart(ctx, store, run.RunID)
		if err != nil {
			return RunRow{}, err
		}
		refreshed.fetchedAt = now
		e.mu.Lock()
		e.obs = &refreshed
		e.mu.Unlock()
		obsCache = &refreshed
	}

	return combineRow(batchID, run, imm, obsCache, bc, summary, summaryFound, consoleObserverDisabled, queued, now), nil
}

// notDoneRow is a run's row while it has no done.json (settledOrRunning's
// verdict, resolveObserverState's "not observed"/"observing", plus slice
// MODEL's Stalled/VerdictReason/ObserverStateText) — mirrors buildRunRow's
// (view.go) early return exactly.
func notDoneRow(batchID string, run farm.ManifestRun, bc batchContext, summary farm.BatchSummary, summaryFound bool, consoleObserverDisabled bool, queued bool, now time.Time) RunRow {
	row := RunRow{RunID: run.RunID, Batch: batchID, Scenario: run.Scenario, StartedAt: bc.CreatedAt, Build: bc.build()}
	row.Verdict = settledOrRunning(summary, summaryFound, run.RunID)
	row.Stalled = row.Verdict == verdictRunning && isStalled(now, bc.CreatedAt, bc.RunBudgetSec)
	summaryRun, summaryRunFound := findSummaryRun(summary, summaryFound, run.RunID)
	row.VerdictReason = verdictReason(row.Verdict, false, nil, summaryRun, summaryRunFound)
	row.ObserverState = resolveObserverState(consoleObserverDisabled, bc.Observer, false, false, queued, row.Verdict != verdictRunning)
	row.ObserverStateText = observerStateText(now, bc.CreatedAt, consoleObserverDisabled, bc.Observer, false, nil, queued, row.Verdict != verdictRunning, row.VerdictReason)
	row.CauseCounts = newCauseClassCounts()
	return row
}

// combineRow builds the final RunRow for a done run from its cached
// immutable and observation parts plus the live inputs (queued state, the
// batch summary's row for this run, and slice MODEL's now-dependent
// facts) — mirrors buildRunRow's (view.go) second half exactly.
func combineRow(batchID string, run farm.ManifestRun, imm *cachedImmutable, obsPart *cachedObservation, bc batchContext, summary farm.BatchSummary, summaryFound bool, consoleObserverDisabled bool, queued bool, now time.Time) RunRow {
	row := RunRow{
		RunID: run.RunID, Batch: batchID, Scenario: imm.Scenario, StartedAt: imm.StartedAt,
		DurationSec: imm.DurationSec, CostUsd: imm.CostUsd, CostKnown: imm.CostKnown,
		CandidateSha256: imm.CandidateSha256, EvaluatorSha256: imm.EvaluatorSha256,
		DoneExists: true, FailedChecks: imm.FailedChecks, metaTaskResult: imm.metaTaskResult,
		Build: bc.build(), StepCount: imm.StepCount, ServiceHostnames: imm.ServiceHostnames,
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
	row.Verdict = observer.ResolveVerdict(summaryResult, found, imm.metaTaskResult)
	summaryRun, summaryRunFound := findSummaryRun(summary, summaryFound, run.RunID)
	row.VerdictReason = verdictReason(row.Verdict, true, row.FailedChecks, summaryRun, summaryRunFound)

	if obsPart != nil {
		row.OlderObsIDs = obsPart.olderObsIDs
		row.Observation = obsPart.obs
	}
	row.ObserverState = resolveObserverState(consoleObserverDisabled, bc.Observer, true, row.Observation != nil, queued, false)
	row.ObserverStateText = observerStateText(now, bc.CreatedAt, consoleObserverDisabled, bc.Observer, true, row.Observation, queued, false, "")
	row.Outcome = computeOutcome(row.Observation)
	row.Disputed = computeDisputed(row.Observation)
	row.CauseCounts = causeSeverityCounts(row.Observation)
	return row
}

// fetchImmutablePart reads run's immutable row data straight from the
// bucket — mirrors buildRunRow's (view.go) meta/verification/step-count
// reads exactly, including its tolerance of a missing results dir or an
// unparsable meta/verification file (silently skipped, never an error:
// only a bundle-construction failure propagates).
func fetchImmutablePart(ctx context.Context, store observer.ObjectStore, run farm.ManifestRun, manifestCreatedAt time.Time) (cachedImmutable, error) {
	imm := cachedImmutable{Scenario: run.Scenario, StartedAt: manifestCreatedAt}

	bundle, err := observer.NewSinkBundle(ctx, store, run.RunID)
	if err != nil {
		return cachedImmutable{}, fmt.Errorf("console: cache: new bundle: %w", err)
	}
	resultsDir, rdErr := observer.ResultsDir(bundle)
	if rdErr != nil {
		// A completed no-work run can legitimately have no results directory;
		// preserve the row and let callers render unavailable evidence.
		return imm, nil //nolint:nilerr // optional results are unavailable, not a row-load failure
	}
	var verification eval.VerificationDocument
	var snapshot *eval.PlatformSnapshot
	var meta eval.BehavioralResult
	metaOK := false
	verificationOK := false
	stepCountOK := false
	if m, mErr := observer.LoadMeta(bundle, resultsDir); mErr == nil {
		metaOK = true
		meta = m
		if !m.StartedAt.IsZero() {
			imm.StartedAt = m.StartedAt
		}
		imm.DurationSec = time.Duration(m.Duration).Seconds()
		if m.Usage != nil {
			imm.CostUsd = m.Usage.TotalCostUsd
			imm.CostKnown = true
		}
		if m.Task != nil {
			imm.metaTaskResult = string(m.Task.Result)
		}
		imm.CandidateSha256 = m.CandidateSha256
		imm.EvaluatorSha256 = m.EvaluatorSha256
	}
	if v, vErr := observer.LoadVerification(bundle, resultsDir); vErr == nil {
		verificationOK = true
		verification = v
		for _, c := range v.Checks {
			if c.Result == eval.CheckFailed || c.Result == eval.CheckBlocked {
				imm.FailedChecks = append(imm.FailedChecks, FailedCheck{
					ID: c.ID, Result: string(c.Result), Expected: c.Expected, Observed: c.Observed, Source: c.Source,
				})
			}
		}
	}
	if snap, sErr := observer.LoadPlatformSnapshot(bundle, resultsDir); sErr == nil {
		snapshot = snap
	}
	if n, sErr := loadStepCount(bundle, resultsDir, meta); sErr == nil {
		stepCountOK = true
		imm.StepCount = n
	}
	imm.ServiceHostnames = serviceHostnames(snapshot, verification)
	imm.cacheable = metaOK && verificationOK && stepCountOK
	return imm, nil
}

// fetchObservationPart reads runID's current observation and older ids
// straight from the bucket — mirrors buildRunRow's (view.go) observation
// read exactly.
func fetchObservationPart(ctx context.Context, store observer.ObjectStore, runID string) (cachedObservation, error) {
	obsStore := observer.NewStore(store)
	obsIDs, err := obsStore.ListObservations(ctx, runID)
	if err != nil {
		return cachedObservation{}, fmt.Errorf("console: cache: list observations: %w", err)
	}
	var part cachedObservation
	if len(obsIDs) > 0 {
		part.olderObsIDs = obsIDs[:len(obsIDs)-1]
		cur, err := obsStore.GetObservation(ctx, runID, obsIDs[len(obsIDs)-1])
		if err != nil {
			return cachedObservation{}, fmt.Errorf("console: cache: get current observation: %w", err)
		}
		part.obs = &cur
	}
	return part, nil
}

// runRowCached is buildRunRow (view.go) through cache when non-nil (rules
// 1-3); a nil cache always reads fresh, matching the pre-cache behavior
// exactly — used by callers/tests that exercise the read model without a
// Server.
func runRowCached(ctx context.Context, store observer.ObjectStore, cache *runCache, consoleObserverDisabled bool, batchID string, run farm.ManifestRun, bc batchContext, summary farm.BatchSummary, summaryFound bool, queueState func(runID string) string) (RunRow, error) {
	if cache == nil {
		return buildRunRow(ctx, store, consoleObserverDisabled, batchID, run, bc, summary, summaryFound, queueState, time.Now())
	}
	return cache.row(ctx, store, consoleObserverDisabled, batchID, run, bc, summary, summaryFound, queueState)
}

// stepTextFinder builds the problems.go StepTextFinder every §8.6 problem
// caller wires into BuildProblemsScoped (item 1/4, FIX3), backed by s's own
// run cache so the still-emitted search's bucket reads are paid at most
// once per run for the console's lifetime, not once per request — the
// bound that keeps the search itself out of a page's warm-load budget.
func (s *Server) stepTextFinder(ctx context.Context) StepTextFinder {
	return func(runID string) (string, bool) {
		return s.runCache.stepText(ctx, s.cfg.Store, runID)
	}
}

// summaryCacheEntry is one batch's summary.json cache slot (rule 4).
type summaryCacheEntry struct {
	mu        sync.Mutex
	found     bool
	summary   farm.BatchSummary
	checkedAt time.Time
}

// summaryCache is the Server-owned batch-summary cache, keyed by batch id:
// cached indefinitely once found, else re-checked at most every
// summaryRecheck.
type summaryCache struct {
	now func() time.Time

	mu      sync.Mutex
	entries map[string]*summaryCacheEntry
}

func newSummaryCache(now func() time.Time) *summaryCache {
	if now == nil {
		now = time.Now
	}
	return &summaryCache{now: now, entries: make(map[string]*summaryCacheEntry)}
}

func (c *summaryCache) entry(batch string) *summaryCacheEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[batch]
	if !ok {
		e = &summaryCacheEntry{}
		c.entries[batch] = e
	}
	return e
}

// load resolves batch's summary.json through the cache (rule 4).
func (c *summaryCache) load(ctx context.Context, store observer.ObjectStore, batch string) (farm.BatchSummary, bool, error) {
	e := c.entry(batch)

	e.mu.Lock()
	found, cached, checkedAt := e.found, e.summary, e.checkedAt
	e.mu.Unlock()

	if found {
		return cached, true, nil
	}

	now := c.now()
	if !checkedAt.IsZero() && now.Sub(checkedAt) < summaryRecheck {
		return farm.BatchSummary{}, false, nil
	}

	sum, sumFound, err := loadSummary(ctx, store, batch)
	if err != nil {
		return farm.BatchSummary{}, false, err
	}

	e.mu.Lock()
	if sumFound {
		e.found = true
		e.summary = sum
	}
	e.checkedAt = now
	e.mu.Unlock()
	return sum, sumFound, nil
}

// resolveSummary is loadSummary (view.go) through sc when non-nil (rule
// 4); a nil sc always reads fresh, matching the pre-cache behavior exactly.
func resolveSummary(ctx context.Context, store observer.ObjectStore, sc *summaryCache, batch string) (farm.BatchSummary, bool, error) {
	if sc == nil {
		return loadSummary(ctx, store, batch)
	}
	return sc.load(ctx, store, batch)
}

// fillRowsConcurrently builds one RunRow per run in runs, at most
// coldFillMaxInFlight running at once (rule 5). Results land in runs' own
// order regardless of completion order — the same determinism the old
// sequential loop gave for free. A run whose row fails to build is logged
// and preserved as an unavailable manifest-backed row when fallback is
// supplied (§8.4: "skipped ... rather than failing the whole listing").
func fillRowsConcurrently(runs []farm.ManifestRun, build func(run farm.ManifestRun) (RunRow, error), fallback func(run farm.ManifestRun, err error) RunRow, logf func(format string, args ...any)) []RunRow {
	if logf == nil {
		logf = defaultLogf
	}
	type slot struct {
		row RunRow
		ok  bool
	}
	slots := make([]slot, len(runs))
	sem := make(chan struct{}, coldFillMaxInFlight)
	var wg sync.WaitGroup
	for i, run := range runs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, run farm.ManifestRun) {
			defer wg.Done()
			defer func() { <-sem }()
			row, err := build(run)
			if err != nil {
				logf("unavailable run %s: %v", run.RunID, err)
				if fallback != nil {
					slots[i] = slot{row: fallback(run, err), ok: true}
				}
				return
			}
			slots[i] = slot{row: row, ok: true}
		}(i, run)
	}
	wg.Wait()

	rows := make([]RunRow, 0, len(runs))
	for _, s := range slots {
		if s.ok {
			rows = append(rows, s.row)
		}
	}
	return rows
}
