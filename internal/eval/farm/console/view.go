package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval"
	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// defaultLogf is the production logf (Server.logf, wired in NewServer):
// logs a batch or run skipped during a listing scan because its
// manifest/observation/meta could not be read (item 5). Every free
// function below that logs takes a logf parameter instead of reading a
// package var, nil-safe here so a direct (non-Server) caller — like a
// test — gets the same stderr behavior for free.
func defaultLogf(format string, args ...any) {
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

// cleanRefreshURL implements FIX3 item 4: the meta-refresh tag's own
// reload target with the one-shot ?notice=/?n= action-result params
// (§8.5) stripped — "" when neither is present, so a refreshing page with
// no notice keeps the exact old tag (no url= at all, the browser's own
// "reload current url" default). Without this, a notice set by an action
// (e.g. "Queued 1 run for assessment.") replays on every 20s reload for as
// long as the job it named keeps the page refreshing, long after that
// one-shot event happened.
func cleanRefreshURL(r *http.Request) string {
	v := r.URL.Query()
	if v.Get("notice") == "" && v.Get("n") == "" {
		return ""
	}
	v.Del("notice")
	v.Del("n")
	if enc := v.Encode(); enc != "" {
		return r.URL.Path + "?" + enc
	}
	return r.URL.Path
}

// formatQueuedJobText renders one queue job's own live-status line (§8.5),
// distinguishing JobQueued from JobRunning (item 3, FIX3) — before this
// fix every job in the queue was described as already "Assessing", even
// one still waiting for a free slot (JobInfo.State went unread).
func formatQueuedJobText(info JobInfo) string {
	if info.State == JobQueued {
		return fmt.Sprintf("Queued with %s since %s — waiting for a slot; the result replaces the one below.", info.Model, fmtTime(info.EnqueuedAt))
	}
	t := info.StartedAt
	if t.IsZero() {
		t = info.EnqueuedAt
	}
	return fmt.Sprintf("Assessing with %s — started %s, usually 1–2 min; the result replaces the one below.", info.Model, fmtTime(t))
}

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

// verdictFilterNotStarted is the batch-runs `verdict` filter's own URL
// token for farm.VerdictNotRun (§8.7: "not-started", distinct from the
// stored RunRow.Verdict spelling "not-run") — named once so goconst sees
// one declaration instead of several repeated literals across this file
// and labels.go's verdictFilterLabel/verdictFilterTooltip.
const verdictFilterNotStarted = "not-started"

// runBudgetDefaultSec exists only as documentation of a rejected reading:
// the MODEL brief's item 1 says Stalled defaults an absent runBudgetSec to
// 45 minutes, but §8.1 says the opposite in so many words — "a manifest
// without runBudgetSec never shows stalled." The spec wins (_common.md);
// isStalled follows §8.1, not this brief text.

// BuildInfo names one ZCP build (§8.8): the candidate sha256, plus its git
// commit label when the batch manifest recorded one (§3.3 candidateInfo,
// farm.CandidateInfo). Two binaries are two builds even at one commit
// (§8.8) — Label is display only, Sha256 (via Sha12) is identity.
type BuildInfo struct {
	Sha256   string
	Revision string
	Modified bool
}

// Label renders §8.8's ZCP build label: the git commit (12 chars, "+
// modified" when built from a dirty tree) when known, else "build
// <sha256[:12]>".
func (b BuildInfo) Label() string {
	if b.Revision == "" {
		return "build " + candidateSha12(b.Sha256)
	}
	rev := b.Revision
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if b.Modified {
		return rev + " + modified"
	}
	return rev
}

// Sha12 is the build's identity for the `build` list filter (§8.7: "the
// build filter takes sha256[:12]").
func (b BuildInfo) Sha12() string { return candidateSha12(b.Sha256) }

// batchContext carries the batch-level facts buildRunRow (and its cached
// mirrors in cache.go) need beyond one farm.ManifestRun — everything here
// is constant across every run of the batch, read once from its manifest,
// so batchWindowRowsWithManifest/loadRunRow (both of whose own signatures
// are locked by scan_test.go/cache_test.go/api.go/pages.go) build it once
// and thread it down instead of widening their own parameter lists.
type batchContext struct {
	CreatedAt       time.Time
	Observer        string
	RunBudgetSec    int
	CandidateSha256 string
	CandidateInfo   farm.CandidateInfo
}

// newBatchContext builds a batchContext from a loaded manifest.
func newBatchContext(m farm.BatchManifest) batchContext {
	createdAt, _ := time.Parse(time.RFC3339, m.CreatedAt)
	var info farm.CandidateInfo
	if m.CandidateInfo != nil {
		info = *m.CandidateInfo
	}
	return batchContext{
		CreatedAt: createdAt, Observer: m.Observer, RunBudgetSec: m.RunBudgetSec,
		CandidateSha256: m.CandidateSha256, CandidateInfo: info,
	}
}

// build returns bc's BuildInfo — the batch-level candidate sha plus
// whatever git commit info farm push --candidate recorded (§3.3).
func (bc batchContext) build() BuildInfo {
	return BuildInfo{Sha256: bc.CandidateSha256, Revision: bc.CandidateInfo.Revision, Modified: bc.CandidateInfo.Modified}
}

// stalledGrace is §3.3/§8.1/§8.8's 30-minute grace period past a run's
// budget before it reads "stalled" rather than merely "running".
const stalledGrace = 30 * time.Minute

// isStalled implements §8.1's stalled rule exactly: a manifest with no
// runBudgetSec (<= 0) never shows stalled — the MODEL brief's own text
// ("default 45 min when absent") is a misreading of §8.1's parenthetical
// and is rejected in favor of the spec.
func isStalled(now, manifestCreatedAt time.Time, runBudgetSec int) bool {
	if runBudgetSec <= 0 || manifestCreatedAt.IsZero() {
		return false
	}
	deadline := manifestCreatedAt.Add(time.Duration(runBudgetSec) * time.Second).Add(stalledGrace)
	return now.After(deadline)
}

// assessmentFailureReason names why the current observation failed, for
// observerStateText's "assessment failed — <reason>" (§8.8): the stored
// error message, else a parse-failure sentence, else the error kind, else
// a generic fallback — always non-empty.
func assessmentFailureReason(obs *observer.Observation) string {
	if obs.Error != "" {
		return obs.Error
	}
	if obs.Status == observationStatusUnparsed {
		return "the model's answer did not parse"
	}
	if obs.ErrorKind != "" {
		return obs.ErrorKind
	}
	return "unknown error"
}

// observerStateText implements §8.8's assessment-state vocabulary: a
// human-reading companion to resolveObserverState's internal enum that
// also distinguishes a failed current observation ("assessment failed —
// <reason>") from a genuinely unassessed one — a distinction
// resolveObserverState's own 5-value enum cannot make without breaking
// TestView_ObservedOutranksOffAndDisabled's locked signature.
//
// settled/settledReason are FIX2 item 3's own addition: a run with no
// done.json is either still running ("not assessed — run not finished",
// unchanged) or already settled by its batch's summary (blocked, not-run,
// or any other terminal result reached without a bundle ever landing) — in
// which case there is nothing to assess, ever, not merely "not yet"
// (settledReason, when set, is the same summary detail/error
// verdictReason already surfaces, so the two never disagree).
func observerStateText(now, manifestCreatedAt time.Time, consoleObserverDisabled bool, manifestObserver string, doneExists bool, obs *observer.Observation, queued bool, settled bool, settledReason string) string {
	if queued {
		return "assessing…"
	}
	if !doneExists {
		if settled {
			if settledReason != "" {
				return "nothing to assess — the run left no record: " + settledReason
			}
			return "nothing to assess — the run left no record"
		}
		return "not assessed — run not finished"
	}
	if obs != nil {
		switch obs.Status {
		case observationStatusOK:
			return "assessed"
		case observationStatusError, observationStatusUnparsed:
			return "assessment failed — " + assessmentFailureReason(obs)
		}
	}
	switch {
	case manifestObserver == "" || manifestObserver == ObserverOff:
		return "not assessed — batch ran without observer"
	case consoleObserverDisabled:
		return "not assessed — automatic assessment is off on this console"
	case !manifestCreatedAt.IsZero() && now.Sub(manifestCreatedAt) > wkObserverWindow+windowSlack:
		return "not assessed — older than 14 days (assess by hand)"
	default:
		return "not assessed"
	}
}

// runDidWork reports whether a run actually did anything — a cost above 0
// or at least one recorded step. It is the same rule batches.go uses to
// tell an evaluation batch from an empty one, and the reason is the same:
// a run the batch ended before it started still writes done.json (live:
// 28 runs across gate2-gate5, 0.4s, "no verification.json in bundle"), so
// DoneExists alone says nothing about whether there is anything to read.
func runDidWork(row RunRow) bool {
	if row.assessmentWorkKnown {
		return row.assessmentDidWork
	}
	return meaningfulAssessmentWork(row.CostUsd, row.StepCount)
}

func assessmentWorkUnavailable(row RunRow) bool { return row.assessmentWorkError != "" }

// meaningfulAssessmentWork is §8.5's single eligibility fact: a run did
// work when its recorded cost is positive or its record contains at least
// one step. Keeping the literal predicate here prevents workers, actions,
// counts and forms from drifting into results-directory heuristics.
func meaningfulAssessmentWork(costUsd float64, stepCount int) bool {
	return costUsd > 0 || stepCount > 0
}

// loadAssessmentWork reads only the evidence needed to apply
// meaningfulAssessmentWork. A run with no results directory, or with a
// readable zero-cost meta and no step files, did no work. Transport,
// parsing and other read failures stay errors so callers can report
// unavailable evidence instead of inventing a zero-work verdict.
func loadAssessmentWork(ctx context.Context, store observer.ObjectStore, runID string) (bool, error) {
	bundle, err := observer.NewSinkBundle(ctx, store, runID)
	if err != nil {
		return false, fmt.Errorf("console: assessment eligibility: new bundle: %w", err)
	}
	resultsDir, err := observer.ResultsDir(bundle)
	if errors.Is(err, observer.ErrResultsNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("console: assessment eligibility: list results: %w", err)
	}

	meta, err := observer.LoadMeta(bundle, resultsDir)
	if err != nil {
		return false, fmt.Errorf("console: assessment eligibility: load meta: %w", err)
	}
	stepCount, err := loadStepCount(bundle, resultsDir, meta)
	if err != nil {
		// A run deliberately ended before execution has a readable, known-zero
		// meta record but neither required step input. That exact shape is the
		// only missing-file case that proves zero work. Partial absence, unknown
		// usage and every malformed/transport failure remain unavailable.
		if meta.Usage != nil && meta.Usage.TotalCostUsd == 0 && errors.Is(err, os.ErrNotExist) && stepFilesAbsent(bundle, resultsDir) {
			return false, nil
		}
		return false, fmt.Errorf("console: assessment eligibility: load steps: %w", err)
	}
	costUsd := float64(0)
	if meta.Usage != nil {
		costUsd = meta.Usage.TotalCostUsd
	}
	return meaningfulAssessmentWork(costUsd, stepCount), nil
}

// resolveAssessmentWork enriches a resolved row with §8.5's three-state
// eligibility. Already-loaded positive cost or steps prove work without an
// extra store read. A zero-looking completed row is re-checked through the
// shared loader so cache/read failures remain unavailable rather than being
// collapsed into "never started".
func resolveAssessmentWork(ctx context.Context, store observer.ObjectStore, row RunRow) RunRow {
	if !row.DoneExists {
		return row
	}
	if meaningfulAssessmentWork(row.CostUsd, row.StepCount) {
		row.assessmentWorkKnown = true
		row.assessmentDidWork = true
		return row
	}
	didWork, err := loadAssessmentWork(ctx, store, row.RunID)
	if err != nil {
		row.assessmentWorkError = err.Error()
		row.ObserverStateText = "assessment unavailable — run evidence could not be read"
		return row
	}
	row.assessmentWorkKnown = true
	row.assessmentDidWork = didWork
	if !didWork {
		row.ObserverStateText = "never started — nothing to assess"
	}
	return row
}

// NeedsAssessment implements §8.5's predicate: a run needs an assessment
// when it has done.json, did some work (runDidWork — an empty bundle has
// nothing to assess and an observation of it can only fail), is not queued
// or running (inFlight), and either has no observation or its current one
// failed (status error/unparsed). The batch callout counts exactly these
// runs; POST /b/<batch>/observe queues exactly these (§8.5 FM-53).
func NeedsAssessment(row RunRow, inFlight bool) bool {
	if !row.DoneExists || inFlight || !runDidWork(row) {
		return false
	}
	if row.Observation == nil {
		return true
	}
	return row.Observation.Status == observationStatusError || row.Observation.Status == observationStatusUnparsed
}

// computeOutcome implements item 1's Outcome fact: format 2 as the
// observer stored it; format 1 derived fresh (findings → problem, else
// ok), per §7.5's "outcome derived by the same rule with ending unknown";
// "" ("none") when there is no current observation or its status is not
// ok (§7.5: "none when unobserved or failed" — this is also the fix for
// the "assessed n/m" inconsistency: a failed current observation counts
// as Outcome=="" everywhere, batches.go's ObservedN included).
func computeOutcome(obs *observer.Observation) string {
	if obs == nil || obs.Status != observationStatusOK {
		return ""
	}
	if formatVersionNum(obs.FormatVersion) == 2 {
		return obs.Outcome
	}
	if len(obs.Findings) > 0 {
		return observer.OutcomeProblem
	}
	return observer.OutcomeOK
}

// computeDisputed implements item 1's Disputed fact: the current
// observation exists, is ok, and disagrees with the deterministic checks
// (§7.5/§8.8: "counted under the verdict it disputes, passed included").
func computeDisputed(obs *observer.Observation) bool {
	return obs != nil && obs.Status == observationStatusOK && !obs.Checks.Agree
}

// causeSeverityCounts implements item 1's per-cause-class finding counts:
// zero-filled (newCauseClassCounts) unless obs carries a current ok
// observation's findings (§7.5: "none when unobserved or failed" applies
// here too — a failed observation's findings are not trustworthy data).
func causeSeverityCounts(obs *observer.Observation) []CauseClassCount {
	counts := newCauseClassCounts()
	if obs == nil || obs.Status != observationStatusOK {
		return counts
	}
	for _, f := range obs.Findings {
		addFindingToCounts(counts, f)
	}
	return counts
}

// serviceHostnames implements §8.6's per-run hostname list norm() needs:
// platform-snapshot.json's services plus every hostname a
// verification.json row names as scope, deduplicated and sorted for a
// deterministic replacement order.
func serviceHostnames(snap *eval.PlatformSnapshot, verification eval.VerificationDocument) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(h string) {
		if h == "" || seen[h] {
			return
		}
		seen[h] = true
		out = append(out, h)
	}
	if snap != nil {
		for _, s := range snap.Services {
			add(s.Hostname)
		}
	}
	for _, c := range verification.Checks {
		add(c.Scope)
	}
	sort.Strings(out)
	return out
}

// blockedCheckIDsReason mirrors FM-9's own summary.detail computation, for
// a bundle whose own verdict is blocked before its batch's summary.json
// exists to read a detail from (a run whose done.json lands before its
// batch settles): sorted ids of every check the run's own verdict matches
// (blocked or failed, per verdict), joined with ", " and capped at 5 with
// a "+N more" suffix; the FM-9 literal when there are none.
func blockedCheckIDsReason(verdict string, failedChecks []FailedCheck) string {
	var ids []string
	for _, c := range failedChecks {
		if c.Result == verdict {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		return "no verification.json in bundle"
	}
	sort.Strings(ids)
	if len(ids) > 5 {
		extra := len(ids) - 5
		return strings.Join(ids[:5], ", ") + fmt.Sprintf(" +%d more", extra)
	}
	return strings.Join(ids, ", ")
}

// verdictReason implements item 1's VerdictReason fact: the blocked/not-
// started reason from the batch summary's row (detail, else error) when
// one exists for this run, else — for a completed bundle whose own
// verdict is blocked, before its batch's summary.json exists — the
// blocked check ids (blockedCheckIDsReason). "" for any other verdict
// (passed/failed/running have no "reason" in §8.8's sense).
func verdictReason(verdict string, doneExists bool, failedChecks []FailedCheck, summaryRun farm.SummaryRun, summaryRunFound bool) string {
	if verdict != farm.VerdictBlocked && verdict != farm.VerdictNotRun {
		return ""
	}
	if summaryRunFound {
		if summaryRun.Detail != "" {
			return summaryRun.Detail
		}
		if summaryRun.Error != "" {
			return summaryRun.Error
		}
	}
	if doneExists {
		return blockedCheckIDsReason(verdict, failedChecks)
	}
	return ""
}

// findSummaryRun locates runID's row in summary (found is false when
// summary carries no row for it, e.g. the batch has not settled yet).
func findSummaryRun(summary farm.BatchSummary, summaryFound bool, runID string) (farm.SummaryRun, bool) {
	if !summaryFound {
		return farm.SummaryRun{}, false
	}
	for _, r := range summary.Runs {
		if r.RunID == runID {
			return r, true
		}
	}
	return farm.SummaryRun{}, false
}

// RunRow is the console's fully-resolved read model for one run — the
// superset GET /api/runs.md (light) and GET /api/runs/<runId>.md (full)
// both narrow down from (§8.4 FM-52). Verdict is verdictRunning for a run
// with no done.json (§7.5, §8.4: "no verdict"); ObserverState is one of
// observed|observing|not observed|observer off|observer disabled (§8.4),
// resolved by resolveObserverState.
//
// The fields below this comment (VerdictReason … ObserverStateText) are
// slice MODEL's additive §8.3/§8.5/§8.8 facts (plans/farm-console-
// clarity-2026-09-11-briefs/MODEL.md item 1) — every other field predates
// this slice and keeps its exact prior meaning.
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

	// VerdictReason is the blocked/not-started reason (verdictReason).
	VerdictReason string
	// Stalled is true once a still-running run has passed its budget plus
	// the §8.1 grace period (isStalled) — Verdict itself stays
	// verdictRunning; Stalled is what tells "running" from "stalled" apart.
	Stalled bool
	// CostKnown is true when meta.json recorded usage (CostUsd is
	// meaningful; "$0.00" is otherwise indistinguishable from "unknown").
	CostKnown bool
	// Build is this run's ZCP build (batchContext.build).
	Build BuildInfo
	// Outcome is computeOutcome's result: "ok"|"problem"|"inconclusive"|"".
	Outcome string
	// Disputed is computeDisputed's result.
	Disputed bool
	// CauseCounts is causeSeverityCounts' result: one entry per cause
	// class (causeClassOrder), always present even at zero.
	CauseCounts []CauseClassCount
	// StepCount is the run's total step count (observer.BuildSteps), 0
	// when the transcript/task-prompt could not be read.
	StepCount int
	// ServiceHostnames is serviceHostnames' result — this run's own
	// service hostnames, for §8.6's norm() host replacement.
	ServiceHostnames []string
	// ObserverStateText is observerStateText's result — the §8.8
	// assessment-state wording (a superset of ObserverState: it also
	// distinguishes "assessment failed").
	ObserverStateText string

	// These three internal fields preserve assessment eligibility's
	// work/no-work/unavailable states across the page and action consumers.
	// They are intentionally absent from the public API wire model.
	assessmentWorkKnown bool
	assessmentDidWork   bool
	assessmentWorkError string
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
	// observerStateNoRecord is FIX3 item 8's own addition: a run whose
	// batch already settled it (blocked, not-run, or any other terminal
	// result) without a bundle ever landing — distinct from
	// observerStateNotObserved, which (before this) also covered a run
	// still genuinely running and waiting for its turn. The API (api.go's
	// apiObserverState) and the farm-triage skill read this straight off
	// RunRow.ObserverState, same as every other value here.
	observerStateNoRecord = "no record"

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
// observed" when it is still genuinely running, or "no record" (item 8,
// FIX3) when its batch already settled it without a bundle ever landing —
// there is nothing to wait for, ever; a run that has an observation reads
// "observed" whatever its manifest or the kill switch say — the label
// describes the run's data first; only then do the kill switch ("observer
// disabled") and the manifest ("observer off") explain why a finished run
// has none.
func resolveObserverState(consoleDisabled bool, manifestObserver string, doneExists, hasObservation, queued bool, settled bool) string {
	if queued {
		return observerStateObserving
	}
	if !doneExists {
		if settled {
			return observerStateNoRecord
		}
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
// rule), the current + older observations (§7.5, §7.6), and slice MODEL's
// additive facts (§8.3 item 1) — now is the clock those facts (Stalled,
// ObserverStateText) are evaluated against.
func buildRunRow(ctx context.Context, store observer.ObjectStore, consoleObserverDisabled bool, batchID string, run farm.ManifestRun, bc batchContext, summary farm.BatchSummary, summaryFound bool, queueState func(runID string) string, now time.Time) (RunRow, error) {
	doneExists, _, err := store.Head(ctx, doneKey(run.RunID))
	if err != nil {
		return RunRow{}, fmt.Errorf("console: head done.json: %w", err)
	}
	queued := runQueued(queueState, run.RunID)
	if !doneExists {
		return notDoneRow(batchID, run, bc, summary, summaryFound, consoleObserverDisabled, queued, now), nil
	}
	imm, err := fetchImmutablePart(ctx, store, run, bc.CreatedAt)
	if err != nil {
		return RunRow{}, err
	}
	obsPart, err := fetchObservationPart(ctx, store, run.RunID)
	if err != nil {
		return RunRow{}, err
	}
	return combineRow(batchID, run, &imm, &obsPart, bc, summary, summaryFound, consoleObserverDisabled, queued, now), nil
}

// loadStepCount reads task-prompt.txt/transcript.jsonl and numbers them
// (observer.BuildSteps, §7.2) for RunRow.StepCount. The caller decides
// whether an absence is the canonical zero-work case or unavailable evidence.
func loadStepCount(bundle observer.Bundle, resultsDir string, meta eval.BehavioralResult) (int, error) {
	taskPrompt, err := observer.LoadTaskPrompt(bundle, resultsDir)
	if err != nil {
		return 0, err
	}
	transcript, err := observer.LoadTranscript(bundle, resultsDir)
	if err != nil {
		return 0, err
	}
	steps, err := observer.BuildSteps(taskPrompt, transcript, observer.ResumeReplies(meta))
	if err != nil {
		return 0, err
	}
	return len(steps), nil
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
	bc := newBatchContext(manifest)
	summary, summaryFound, err := resolveSummary(ctx, store, sc, batchID)
	if err != nil {
		return RunRow{}, fmt.Errorf("console: load run row: %w", err)
	}
	row, err := runRowCached(ctx, store, cache, consoleObserverDisabled, batchID, run, bc, summary, summaryFound, queueState)
	if err != nil {
		return unavailableRunRow(batchID, run, bc, summary, summaryFound, consoleObserverDisabled, queueState, err), nil
	}
	return resolveAssessmentWork(ctx, store, row), nil
}

// unavailableRunRow keeps manifest and matching summary context visible when
// a single run's evidence read fails. Evidence-derived fields stay empty.
func unavailableRunRow(batchID string, run farm.ManifestRun, bc batchContext, summary farm.BatchSummary, summaryFound, consoleObserverDisabled bool, queueState func(runID string) string, evidenceErr error) RunRow {
	queued := queueState != nil && queueState(run.RunID) != ""
	summaryRun, found := findSummaryRun(summary, summaryFound, run.RunID)
	verdict := ""
	if found && summaryRun.Result != "" {
		verdict = summaryRun.Result
	}
	settled := found && verdict != "" && verdict != verdictRunning
	return RunRow{RunID: run.RunID, Batch: batchID, Scenario: run.Scenario,
		StartedAt: bc.CreatedAt, Build: bc.build(), Verdict: verdict,
		VerdictReason:       verdictReason(verdict, false, nil, summaryRun, found),
		ObserverState:       resolveObserverState(consoleObserverDisabled, bc.Observer, false, false, queued, settled),
		ObserverStateText:   "assessment unavailable — run evidence could not be read",
		assessmentWorkError: evidenceErr.Error(), CauseCounts: newCauseClassCounts()}
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
func batchWindowRows(ctx context.Context, store observer.ObjectStore, consoleObserverDisabled bool, batchID string, queueState func(runID string) string, cache *runCache, sc *summaryCache, logf func(format string, args ...any)) ([]RunRow, error) {
	manifest, err := loadManifest(ctx, store, batchID)
	if err != nil {
		return nil, fmt.Errorf("console: batch window rows: %w", err)
	}
	return batchWindowRowsWithManifest(ctx, store, consoleObserverDisabled, batchID, manifest, queueState, cache, sc, logf)
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
func batchWindowRowsWithManifest(ctx context.Context, store observer.ObjectStore, consoleObserverDisabled bool, batchID string, manifest farm.BatchManifest, queueState func(runID string) string, cache *runCache, sc *summaryCache, logf func(format string, args ...any)) ([]RunRow, error) {
	bc := newBatchContext(manifest)
	summary, summaryFound, err := resolveSummary(ctx, store, sc, batchID)
	if err != nil {
		return nil, fmt.Errorf("console: batch window rows: %w", err)
	}
	rows := fillRowsConcurrently(manifest.Runs, func(run farm.ManifestRun) (RunRow, error) {
		row, rowErr := runRowCached(ctx, store, cache, consoleObserverDisabled, batchID, run, bc, summary, summaryFound, queueState)
		if rowErr != nil {
			return RunRow{}, rowErr
		}
		return resolveAssessmentWork(ctx, store, row), nil
	}, func(run farm.ManifestRun, err error) RunRow {
		return unavailableRunRow(batchID, run, bc, summary, summaryFound, consoleObserverDisabled, queueState, err)
	}, logf)
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
func rowsSinceWindow(ctx context.Context, store observer.ObjectStore, consoleObserverDisabled bool, window time.Duration, now time.Time, queueState func(runID string) string, cache *runCache, sc *summaryCache, logf func(format string, args ...any)) ([]RunRow, error) {
	if logf == nil {
		logf = defaultLogf
	}
	batches, err := listBatchIDs(ctx, store)
	if err != nil {
		return nil, fmt.Errorf("console: rows since window: %w", err)
	}
	since := now.Add(-window)
	var out []RunRow
	for _, b := range batches {
		manifest, err := loadManifest(ctx, store, b)
		if err != nil {
			logf("skip batch %s: load manifest: %v", b, err)
			continue
		}
		rows, err := batchWindowRowsWithManifest(ctx, store, consoleObserverDisabled, b, manifest, queueState, cache, sc, logf)
		if err != nil {
			logf("skip batch %s: %v", b, err)
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

// --- batch runs / /api/runs.md / run steps lists (§8.7 item 5) ------------

// runVerdictVocab maps a RunRow to §8.8's verdict vocabulary token for the
// `verdict` list filter: passed|failed|blocked|not-started|running|
// stalled — "not-started" for farm.VerdictNotRun, "stalled" for a running
// row whose Stalled fact (isStalled) is true.
func runVerdictVocab(row RunRow) string {
	switch row.Verdict {
	case farm.VerdictPassed, farm.VerdictFailed, farm.VerdictBlocked:
		return row.Verdict
	case farm.VerdictNotRun:
		return verdictFilterNotStarted
	case verdictRunning:
		if row.Stalled {
			return "stalled"
		}
		return verdictRunning
	default:
		return row.Verdict
	}
}

// runOutcomeVocab maps RunRow.Outcome to the `outcome` filter's vocabulary
// (ok|problem|inconclusive|none): "" (unassessed or failed, item 1) reads
// "none".
func runOutcomeVocab(row RunRow) string {
	if row.Outcome == "" {
		return "none"
	}
	return row.Outcome
}

// runHasCause reports whether row has at least one finding in cause class
// (§8.7: "cause ... matches an item when any of its findings is in that
// class").
func runHasCause(row RunRow, class string) bool {
	for _, c := range row.CauseCounts {
		if c.Class == class {
			return c.High+c.Medium+c.Low > 0
		}
	}
	return false
}

// runListClosedFilters is shared by the batch-runs and /api/runs.md
// ListSpecs (§8.7 table: both take verdict/outcome/cause).
func runListClosedFilters() []ClosedFilter {
	return []ClosedFilter{
		{Name: paramVerdict, Allowed: []string{farm.VerdictPassed, farm.VerdictFailed, farm.VerdictBlocked, verdictFilterNotStarted, verdictRunning, "stalled"}},
		{Name: paramOutcome, Allowed: []string{observer.OutcomeOK, observer.OutcomeProblem, observer.OutcomeInconclusive, "none"}},
		{Name: paramCause, Allowed: []string{CauseClassZCP, CauseClassTest, CauseClassAgent, CauseClassPlatform}},
	}
}

// runListMatch is shared by both RunRow engines below.
func runListMatch(row RunRow, name, value string) bool {
	switch name {
	case paramVerdict:
		return runVerdictVocab(row) == value
	case paramOutcome:
		return runOutcomeVocab(row) == value
	case paramCause:
		return runHasCause(row, value)
	case paramBatch:
		return OpenMatch(row.Batch, value)
	}
	return false
}

// runProblemConcern is the batch-runs `problem` sort key's composite field
// (§8.7: "verdict rank, disputed, highest severity, finding count"),
// returned as one ascending-comparable tuple where a HIGHER value at each
// step means "more of a problem" (so the default desc direction shows the
// most concerning runs first): concern inverts pages.go's verdictRank
// (which ranks 0=failed as most urgent) so failed sorts as the highest
// concern value here.
func runProblemConcern(row RunRow) (concern, disputed, maxSeverity, findingCount int) {
	concern = -verdictRank(row.Verdict)
	if row.Disputed {
		disputed = 1
	}
	if row.Observation != nil && row.Observation.Status == observationStatusOK {
		for _, f := range row.Observation.Findings {
			if r := problemSeverityRank(f.Severity); r > maxSeverity {
				maxSeverity = r
			}
			findingCount++
		}
	}
	return concern, disputed, maxSeverity, findingCount
}

func runProblemConcernCompare(a, b RunRow) int {
	ca, da, sa, ta := runProblemConcern(a)
	cb, db, sb, tb := runProblemConcern(b)
	if c := cmpInt(ca, cb); c != 0 {
		return c
	}
	if c := cmpInt(da, db); c != 0 {
		return c
	}
	if c := cmpInt(sa, sb); c != 0 {
		return c
	}
	return cmpInt(ta, tb)
}

// batchRunsListSpec is a batch's runs list's query surface (§8.7): no
// `since`/`batch` filter — the batch is already the caller's scope.
func batchRunsListSpec() ListSpec {
	return ListSpec{
		Closed:      runListClosedFilters(),
		Sorts:       []SortKey{{Name: "problem", DefaultDir: "desc"}, {Name: paramScenario, DefaultDir: "asc"}, {Name: "duration", DefaultDir: "desc"}, {Name: "cost", DefaultDir: "desc"}},
		DefaultSort: "problem",
	}
}

// batchRunsEngine wires batchRunsListSpec to RunRow. duration/cost sort
// unknown-last for a run with no done.json / unrecorded cost (§8.7:
// "duration of a run without done.json" is the table's own example).
func batchRunsEngine() Engine[RunRow] {
	return Engine[RunRow]{
		Spec:  batchRunsListSpec(),
		Match: runListMatch,
		Sorts: map[string]SortSpec[RunRow]{
			"problem": {
				Primary:  runProblemConcernCompare,
				Tiebreak: func(a, b RunRow) int { return cmpString(a.Scenario, b.Scenario) },
			},
			paramScenario: {
				Primary: func(a, b RunRow) int { return cmpString(a.Scenario, b.Scenario) },
			},
			"duration": {
				Primary:  func(a, b RunRow) int { return cmpFloat(a.DurationSec, b.DurationSec) },
				Tiebreak: func(a, b RunRow) int { return cmpString(a.Scenario, b.Scenario) },
				Unknown:  func(r RunRow) bool { return !r.DoneExists },
			},
			"cost": {
				Primary:  func(a, b RunRow) int { return cmpFloat(a.CostUsd, b.CostUsd) },
				Tiebreak: func(a, b RunRow) int { return cmpString(a.Scenario, b.Scenario) },
				Unknown:  func(r RunRow) bool { return !r.CostKnown },
			},
		},
	}
}

// apiRunsListSpec is GET /api/runs.md's query surface (§8.7): batch,
// since, verdict, outcome, cause. §8.7/§8.4 name no default for `since`
// here (unlike /problems and /findings) and the endpoint's other scoping
// param is `batch` — NoSinceDefault leaves the window unbounded absent an
// explicit `since`, matching batchListSpec's reasoning.
func apiRunsListSpec() ListSpec {
	return ListSpec{
		Closed:      runListClosedFilters(),
		Open:        []string{paramBatch},
		Sorts:       []SortKey{{Name: "newest", DefaultDir: "desc"}},
		DefaultSort: "newest",
		HasSince:    true, NoSinceDefault: true,
	}
}

// apiRunsEngine wires apiRunsListSpec to RunRow.
func apiRunsEngine() Engine[RunRow] {
	return Engine[RunRow]{
		Spec:  apiRunsListSpec(),
		Match: runListMatch,
		Time:  func(r RunRow) time.Time { return r.StartedAt },
		Sorts: map[string]SortSpec[RunRow]{
			"newest": {
				Primary:  func(a, b RunRow) int { return cmpTime(a.StartedAt, b.StartedAt) },
				Tiebreak: func(a, b RunRow) int { return cmpString(a.RunID, b.RunID) },
			},
		},
	}
}

// runStepsListSpec is a run's steps list's query surface (§8.7): steps=
// all|cited|errors (default all); no sort key ("record order" — sort/dir
// are not accepted, so Parse refuses them like any unknown parameter);
// /r/ also takes `obs` (§8.3).
func runStepsListSpec() ListSpec {
	return ListSpec{
		Closed:   []ClosedFilter{{Name: "steps", Allowed: []string{filterAll, "cited", "errors"}}},
		Defaults: map[string]string{"steps": filterAll},
		AllowObs: true,
	}
}

// FilterSteps implements the run-steps list's `steps=` filter (§8.7):
// "all" keeps every step in record order (no sort key exists for this
// list); "cited" keeps only steps some finding's evidence cites (per
// citedSteps, evidenceSteps' result as a set); "errors" keeps only tool
// steps whose result was an error.
func FilterSteps(steps []observer.Step, mode string, citedSteps map[int]bool) []observer.Step {
	if mode == "" || mode == filterAll {
		return steps
	}
	out := make([]observer.Step, 0, len(steps))
	for _, s := range steps {
		switch mode {
		case "cited":
			if citedSteps[s.N] {
				out = append(out, s)
			}
		case "errors":
			if s.Kind == observer.StepTool && s.ToolIsError {
				out = append(out, s)
			}
		}
	}
	return out
}
