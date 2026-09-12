// Package console: this file, pages_batch.go, owns GET /b/<batch> (§8.3
// FM-51's batch page) — split out of pages.go (S-SHELL item 1) so each page
// owns one file. Rewritten for plans/farm-console-clarity-2026-09-11-
// briefs/WAVE2.md's BATCH slice: the five §8.3 blocks (header + vs-previous,
// summary, problems in this batch (§8.6), runs grouped by precedence
// (§8.7), the assess callout (§8.5)).
package console

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// batchRunView narrows a RunRow to what the batch page's runs table (item
// 4) needs: the current observation's headline when there is one, else
// RunRow.ObserverState verbatim (the read model owns that vocabulary — S5b
// makes it emit "observing"; TestPages_BatchRowsShowHeadlineOrObserverState
// pins the exact "not observed"/"observer off" strings), plus the failed/
// blocked check ids, the group-specific "why" line, and the row's own
// live queue status.
type batchRunView struct {
	RunRow
	Headline        string // the current observation's headline, "" without one
	State           string // RunRow.ObserverState, shown when there is no headline
	High            int
	Medium          int
	Low             int
	FailedCheckIDs  []string // at most maxFailedCheckChips
	FailedCheckMore int      // how many more failed checks the row does not list

	// FirstFailedText/FirstFailedTitle are the Failed-and-blocked group's
	// "why" line (item 4): the first failed check in plain words, display
	// text and (when it differs) the uncleaned original for the title
	// attribute — or, for a blocked verdict, the blocked reason (no
	// cleanup needed, it is prose already).
	FirstFailedText  string
	FirstFailedTitle string
	// Reason is the Not-finished group's "with the reason" text (item 4).
	Reason string

	// JobStatusText/PreStoreFailureText are §8.5's per-row live status:
	// "Assessing with <model> — started …" while queued/running, or the
	// last pre-store failure, both nil-safe absent a Queue.
	JobStatusText       string
	PreStoreFailureText string
	Assessment          string
	AssessmentOutcome   string
}

// maxFailedCheckChips caps the failed-check ids a batch row lists.
const maxFailedCheckChips = 3

// observationFailed reports whether obs's status is one the console shows
// as a failure notice rather than a real assessment (item 3: "error" or
// "unparsed", §7.5).
func observationFailed(obs *observer.Observation) bool {
	return obs != nil && (obs.Status == observationStatusError || obs.Status == observationStatusUnparsed)
}

// observerFailedState is the batch row's State when the current
// observation failed (item 3) — shown in place of a headline.
const observerFailedState = "observer failed"

func newBatchRunView(row RunRow) batchRunView {
	state := row.ObserverState
	if assessmentWorkUnavailable(row) || !runDidWork(row) {
		state = row.ObserverStateText
	}
	v := batchRunView{RunRow: row, State: state, Assessment: row.ObserverStateText, AssessmentOutcome: row.Outcome}
	if row.Outcome != "" {
		v.Assessment = assessmentOutcomeLabel(row.Outcome)
	}
	if row.Observation != nil && observationFailed(row.Observation) {
		v.State = observerFailedState
	} else if row.Observation != nil {
		v.Headline = row.Observation.Headline
		for _, f := range row.Observation.Findings {
			switch f.Severity {
			case observer.SeverityHigh:
				v.High++
			case observer.SeverityMedium:
				v.Medium++
			case observer.SeverityLow:
				v.Low++
			}
		}
	}
	for i, c := range row.FailedChecks {
		if i == maxFailedCheckChips {
			v.FailedCheckMore = len(row.FailedChecks) - maxFailedCheckChips
			break
		}
		v.FailedCheckIDs = append(v.FailedCheckIDs, c.ID)
	}
	return v
}

// --- Runs table: precedence groups (§8.3 item 4) --------------------------

// batchRunGroup* name §8.3 item 4's five precedence groups, in the exact
// words the spec uses as headings — a run sits in the first one it fits.
const (
	batchGroupFailedBlocked = "Failed and blocked"
	batchGroupNotFinished   = "Not finished"
	batchGroupNotAssessed   = "Not assessed"
	batchGroupProblems      = "Problems in passed runs"
	batchGroupClean         = "Clean"
)

// batchRunGroupOrder is the precedence order groups render in.
var batchRunGroupOrder = []string{batchGroupFailedBlocked, batchGroupNotFinished, batchGroupNotAssessed, batchGroupProblems, batchGroupClean}

// runGroupFor implements item 4's precedence rule. A run with
// Verdict==failed/blocked always lands in the first group regardless of
// assessment; verdictRunning/farm.VerdictNotRun (both imply !DoneExists,
// view.go's buildRunRow) always land in the second. Only a done, passed
// run reaches the last three groups, split by Outcome (computeOutcome,
// view.go: "" without a current ok observation).
func runGroupFor(row RunRow) string {
	switch row.Verdict {
	case farm.VerdictFailed, farm.VerdictBlocked:
		return batchGroupFailedBlocked
	case verdictRunning, farm.VerdictNotRun:
		return batchGroupNotFinished
	}
	switch row.Outcome {
	case "":
		return batchGroupNotAssessed
	case observer.OutcomeProblem, observer.OutcomeInconclusive:
		return batchGroupProblems
	default: // "ok"
		return batchGroupClean
	}
}

// orderedAsBatchPage returns rows in the batch page's default order: the
// runs list's default sort, bucketed into the precedence groups — the one
// ordering the Overview's dots also use, so they read left to right as the
// batch page does (§8.3).
func orderedAsBatchPage(rows []RunRow, now time.Time) []RunRow {
	q, err := Parse(batchRunsListSpec(), url.Values{})
	if err != nil {
		return rows
	}
	sorted, _ := batchRunsEngine().Apply(rows, q, now)
	out := make([]RunRow, 0, len(sorted))
	for _, g := range batchRunGroupOrder {
		for _, r := range sorted {
			if runGroupFor(r) == g {
				out = append(out, r)
			}
		}
	}
	return out
}

// notFinishedReason is the Not-finished group's "with the reason" text
// (item 4) for a running/stalled/not-started row: VerdictReason already
// carries the not-started/blocked-with-no-bundle detail (view.go); running
// and stalled have no VerdictReason (§8.8: "reason" only applies to
// blocked/not-started), so this names them directly, reusing labels.go's
// own "stalled" tooltip rather than a second copy of its wording.
func notFinishedReason(row RunRow) string {
	switch row.Verdict {
	case verdictRunning:
		if row.Stalled {
			return "stalled — " + verdictTooltip(verdictStalled)
		}
		return "still running"
	case farm.VerdictNotRun:
		if row.VerdictReason != "" {
			return row.VerdictReason
		}
		return "not started"
	default:
		return ""
	}
}

// isOpaqueIDSegment reports whether one "/"-delimited segment of a check
// id is a random, unreadable resource id rather than meaningful text: a
// 22-character base62 token (a Zerops resource id) or an 8-or-more-
// character hex token — the same two shapes problems.go's norm()
// recognizes for an anchor, at segment granularity (a check id's own
// grammar is "/"-delimited, e.g. "expected_service/web/status" —
// controller_test.go), so dropping a whole opaque segment never leaves a
// dangling separator the way sub-token replacement would.
func isOpaqueIDSegment(s string) bool {
	if len(s) == 22 {
		return true
	}
	return len(s) >= 8 && isHexSegment(s)
}

func isHexSegment(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// displayCheckID drops an opaque id segment from a check id for display
// (item 16: "drop opaque id suffixes from display, keep them in the
// title"); an id with none (the common case) passes through unchanged. Id
// never goes empty: an id made entirely of opaque segments is returned
// as-is rather than collapsing to "".
func displayCheckID(id string) string {
	segs := strings.Split(id, "/")
	kept := segs[:0]
	for _, s := range segs {
		if !isOpaqueIDSegment(s) {
			kept = append(kept, s)
		}
	}
	if len(kept) == 0 {
		return id
	}
	return strings.Join(kept, "/")
}

// firstFailedCheckPlain implements item 4's "the first failed check in
// plain words `id: expected …, got …`" — the first entry of checks (view.
// go's buildRunRow appends them in verification.json's own order, so
// "first" is deterministic), with its id cleaned for display
// (displayCheckID) and, only when that changed anything, the uncleaned
// original carried as a title.
func firstFailedCheckPlain(checks []FailedCheck) (display, title string) {
	if len(checks) == 0 {
		return "", ""
	}
	c := checks[0]
	disp := displayCheckID(c.ID)
	expected, observed := formatCheckValue(c.Expected), formatCheckValue(c.Observed)
	display = fmt.Sprintf("%s: expected %s, got %s", disp, expected, observed)
	if disp != c.ID {
		title = fmt.Sprintf("%s: expected %s, got %s", c.ID, expected, observed)
	}
	return display, title
}

// batchRunGroupView is one rendered group of the runs table (item 4).
// Collapsed marks the Clean group, which renders as one line of names
// rather than one row per run.
type batchRunGroupView struct {
	Title     string
	Runs      []batchRunView
	Collapsed bool
}

// batchRunsLabeler names the batch-runs list's filter parameters and
// values (§8.7) for listnav's filter bar, reusing labels.go's own
// vocabulary so this list's chips never say something /terms disagrees
// with.
func batchRunsLabeler() listLabeler {
	return listLabeler{
		Param: listParamLabel,
		Value: func(p, v string) string {
			switch p {
			case paramVerdict:
				return verdictFilterLabel(v)
			case paramOutcome:
				return assessmentOutcomeLabel(v)
			case paramCause:
				return causeClassDisplay[v]
			default:
				return v
			}
		},
		Title: func(p, v string) string {
			if p == paramVerdict {
				return verdictFilterTooltip(v)
			}
			return ""
		},
	}
}

// --- Problems in this batch (§8.6 item 3) ----------------------------------

// batchProblemView is one row of "Problems in this batch": the usual §8.6
// Problem plus whether it also hit the previous batch of the same set.
// HasPrevCompare is false when there is no previous batch to compare
// against (the also/not-in line simply does not render); AlsoInPrev is
// only meaningful when HasPrevCompare is true. HitInBatch is FIX2 item 7's
// own batch-local count — distinct runs of THIS batch (never the previous
// one) carrying one of the problem's members — for "hit N of M runs in
// this batch" (M is the caller's own run count, batchPageData.ObservedM).
type batchProblemView struct {
	Problem
	HasPrevCompare bool
	AlsoInPrev     bool
	HitInBatch     int
}

// hitInBatch counts the distinct runs of batchID among p's members — FIX2
// item 7's own batch-local hit count. A field FIX2-DATA's own slice may
// later add straight to Problem (§8.6's `a`/`b` are global-build counts,
// not batch-local, so the review finding asked for this local one
// separately); this reads it from the members already resolved here rather
// than wait on that, per the coordinator's note that either is fine.
func hitInBatch(p Problem, batchID string) int {
	seen := map[string]bool{}
	for _, m := range p.Members {
		if m.Batch == batchID {
			seen[m.RunID] = true
		}
	}
	return len(seen)
}

// buildBatchProblems implements item 3: §8.6 clustering scoped to this
// batch's own runs plus (when one exists) the previous same-set batch's
// runs — that pair decides which problems are LISTED here at all, and
// whether a problem also hit that previous batch — but status (item 1,
// FIX3: "status must not depend on the request's scope") is computed by
// BuildProblemsScoped over allRuns, the full farm history, never just this
// pair; a problem hit on a build several batches back must still read
// "recurring", not "first seen". A problem clustering to a run outside
// [this batch, previous batch] can now appear among Members (allRuns
// carries the whole farm), but is only ever LISTED here when it also hits
// this batch, or (FIX2 item 7) only the previous one (status gone).
//
// Ambiguity (flagged per _common.md): §8.6/§8.3 do not spell out the run
// set a single batch's page should show ITS OWN problems from, or whether
// "also/not in <previous batch>" replaces or augments the normal five-way
// Status. This resolves it as: list exactly the problems with a member in
// [this batch, previous same-set batch] (bounded, and enough for a
// meaningful "also/not in" comparison); keep rendering the ordinary Status
// label (labels.go's problemStatusLabel, now computed over full history per
// item 1) and ADD the also/not-in annotation alongside it, rather than
// replacing it.
func buildBatchProblems(allRuns []ProblemsRun, rows []RunRow, batchID string, prev *BatchRow, prevRows []RunRow, findStepText StepTextFinder) []batchProblemView {
	inScope := runRowIDSet(rows, prevRows)
	problems := BuildProblemsScoped(allRuns, inScope, findStepText)

	var out []batchProblemView
	for _, p := range problems {
		hitThis, hitPrev := false, false
		for _, m := range p.Members {
			if m.Batch == batchID {
				hitThis = true
			}
			if prev != nil && m.Batch == prev.BatchID {
				hitPrev = true
			}
		}
		// Item 7 (FIX2): a problem gone from this batch but present in the
		// previous one is exactly the good news a fixed bug represents —
		// show it too, not only the ones still hitting this batch.
		if !hitThis && (!hitPrev || p.Status != StatusGone) {
			continue
		}
		view := batchProblemView{Problem: p, HitInBatch: hitInBatch(p, batchID)}
		if prev != nil {
			view.HasPrevCompare = true
			view.AlsoInPrev = hitPrev
		}
		out = append(out, view)
	}
	return out
}

// resolveBatchProblems implements item 3's "Problems in this batch" block:
// either §8.6 clustering (split into live/low severity) over the full farm
// history (allRuns), scoped to [this batch, previous same-set batch]
// (buildBatchProblems' own inScope) — status must not depend on that scope
// (item 1, FIX3) — or, when nothing here has ever been assessed, the
// deterministic-checks fallback. Split out of handleBatchPage so that
// function's own branching doesn't grow with this block's (maintidx).
func (s *Server) resolveBatchProblems(ctx context.Context, rows []RunRow, batchID string, hasPrev bool, prevBatch BatchRow, prevRows []RunRow) (problems, problemsLow []batchProblemView, fallback bool, fallbackChecks []checkFailureRow, err error) {
	if !batchHasAnyAssessment(rows) {
		return nil, nil, true, buildCheckFailureFallback(rows), nil
	}

	var prevPtr *BatchRow
	if hasPrev {
		prevPtr = &prevBatch
	}
	allRuns, err := s.allProblemsRuns(ctx)
	if err != nil {
		return nil, nil, false, nil, err
	}
	for _, p := range buildBatchProblems(allRuns, rows, batchID, prevPtr, prevRows, s.stepTextFinder(ctx)) {
		if p.Severity == observer.SeverityLow {
			problemsLow = append(problemsLow, p)
			continue
		}
		problems = append(problems, p)
	}
	return problems, problemsLow, false, nil, nil
}

// batchHasAnyAssessment reports whether any run of rows has a current ok
// observation (RunRow.Outcome != "") — item 3's "without any assessment"
// fallback trigger.
func batchHasAnyAssessment(rows []RunRow) bool {
	for _, r := range rows {
		if r.Outcome != "" {
			return true
		}
	}
	return false
}

// checkFailureRow is one row of item 3's fallback ("checks that failed in
// two or more runs") — used only when the batch carries no assessment at
// all, clustering the deterministic FailedChecks by id instead of §8.6's
// finding-based clustering.
type checkFailureRow struct {
	ID      string
	Display string
	Title   string // the uncleaned id, only when Display differs from it
	RunIDs  []string
}

// buildCheckFailureFallback implements item 3's fallback: every check id
// that failed or was blocked in two or more of this batch's runs, most-
// affected first, id ascending on a tie.
func buildCheckFailureFallback(rows []RunRow) []checkFailureRow {
	byID := map[string][]string{}
	var order []string
	for _, r := range rows {
		for _, c := range r.FailedChecks {
			if _, ok := byID[c.ID]; !ok {
				order = append(order, c.ID)
			}
			byID[c.ID] = append(byID[c.ID], r.RunID)
		}
	}
	var out []checkFailureRow
	for _, id := range order {
		runIDs := byID[id]
		if len(runIDs) < 2 {
			continue
		}
		disp := displayCheckID(id)
		row := checkFailureRow{ID: id, Display: disp, RunIDs: runIDs}
		if disp != id {
			row.Title = id
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].RunIDs) != len(out[j].RunIDs) {
			return len(out[i].RunIDs) > len(out[j].RunIDs)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// --- Header vs-previous line -----------------------------------------------

// batchDiffView is item 1's "vs <previous batch of the same set>" line.
type batchDiffView struct {
	PreviousBatchID string
	Diff            BatchDiff
	Comparison      batchComparisonView
}

type batchComparisonEntry struct {
	Scenario string
	RunID    string
	Verdict  string
}

type batchComparisonView struct {
	NewlyNotPassing []batchComparisonEntry
	NowPassing      []batchComparisonEntry
	StillNotPassing []batchComparisonEntry
	HasChanges      bool
}

func buildBatchComparison(d BatchDiff, current []RunRow) batchComparisonView {
	byScenario := make(map[string]RunRow, len(current))
	for _, row := range current {
		if assessmentWorkUnavailable(row) {
			continue
		}
		chosen, exists := byScenario[row.Scenario]
		if !exists || (chosen.Verdict == farm.VerdictPassed && row.Verdict != farm.VerdictPassed) {
			byScenario[row.Scenario] = row
		}
	}
	entries := func(scenarios []string) []batchComparisonEntry {
		out := make([]batchComparisonEntry, 0, len(scenarios))
		for _, scenario := range scenarios {
			row := byScenario[scenario]
			out = append(out, batchComparisonEntry{Scenario: scenario, RunID: row.RunID, Verdict: row.Verdict})
		}
		return out
	}
	v := batchComparisonView{
		NewlyNotPassing: entries(d.NewlyFailing),
		NowPassing:      entries(d.Fixed),
		StillNotPassing: entries(d.StillFailing),
	}
	v.HasChanges = len(v.NewlyNotPassing)+len(v.NowPassing)+len(v.StillNotPassing) > 0
	return v
}

// causeCountView is one row of the summary block's per-cause-class
// findings (item 2) — Label pre-resolved (labels.go's causeClassDisplay)
// so the template never needs a class->label lookup of its own.
type causeCountView struct {
	Label        string
	High, Medium int
}

// batchSummaryPart formats one non-zero count with its label, "" when n==0
// (item 9: "zero counts skipped").
func batchSummaryPart(n int, label string) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d %s", n, label)
}

// batchSummaryGroup joins label's non-zero parts with " · ", "" when every
// part is zero — the whole labelled group (e.g. "Goal: ") is then dropped
// rather than left dangling with nothing after the colon (item 9).
func batchSummaryGroup(label string, parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return label + ": " + strings.Join(kept, " · ")
}

// buildBatchSummaryLine implements item 9: one labelled line — "Goal: 8 yes
// · 1 no — Assessment: 3 OK · 6 problem — ZCP findings: 4 medium" —
// replacing the old row of identical chips and the cryptic "ZCP: 0h/4m".
// causeCounts is data.CauseCounts, already narrowed to the classes that
// have any high/medium finding; only the ZCP class' own entry feeds the
// third group (§8.8's own cause-class label, "ZCP").
func buildBatchSummaryLine(goalYes, goalPartly, goalNo, outOK, outProblem, outInconclusive int, causeCounts []causeCountView) string {
	groups := []string{
		batchSummaryGroup("Goal",
			batchSummaryPart(goalYes, "yes"), batchSummaryPart(goalPartly, "partly"), batchSummaryPart(goalNo, "no")),
		batchSummaryGroup("Assessment",
			batchSummaryPart(outOK, "OK"), batchSummaryPart(outProblem, "problem"), batchSummaryPart(outInconclusive, "inconclusive")),
	}
	for _, c := range causeCounts {
		if c.Label != causeClassDisplay[CauseClassZCP] {
			continue
		}
		groups = append(groups, batchSummaryGroup("ZCP findings",
			batchSummaryPart(c.High, "high"), batchSummaryPart(c.Medium, "medium")))
	}
	var kept []string
	for _, g := range groups {
		if g != "" {
			kept = append(kept, g)
		}
	}
	return strings.Join(kept, " — ")
}

// buildInconclusiveBanner implements FIX2 item 7's own top-of-page warning:
// "" unless at least half of totalRuns ended inconclusive (the agent's
// session/turn limit firing is by far the common cause, §7.5's own
// prompt rule), in which case the whole batch is not worth reading past
// this line without a rerun. totalRuns==0 never banners (nothing ran yet).
func buildInconclusiveBanner(inconclusiveN, totalRuns int) string {
	if totalRuns == 0 || inconclusiveN*2 < totalRuns {
		return ""
	}
	return fmt.Sprintf("Inconclusive batch — %d of %d runs ended on the agent's session limit; rerun before reading anything below.", inconclusiveN, totalRuns)
}

// --- Page data --------------------------------------------------------------

// batchPageData is GET /b/<batch> (§8.3 FM-51), rewritten to the five
// blocks WAVE2's BATCH slice requires.
type batchPageData struct {
	Meta pageMeta

	BatchID   string
	CreatedAt time.Time
	Set       string
	Note      string
	Build     BuildInfo

	VsPrevious *batchDiffView

	VerdictCounts                                  []VerdictCount
	Verdicts                                       []verdictDisputeView
	DisputedCount                                  int
	GoalYes, GoalPartly, GoalNo                    int
	OutcomeOK, OutcomeProblem, OutcomeInconclusive int
	CauseCounts                                    []causeCountView
	ZCPHigh, ZCPMedium                             int
	TotalCostUsd                                   float64
	Cost                                           string
	CostUnknownN                                   int
	ObservedN, ObservedM                           int
	UnavailableN                                   int
	// SummaryLine is item 9's one labelled line ("Goal: 8 yes · 1 no —
	// Assessment: 3 OK · 6 problem — ZCP findings: 4 medium"), every zero
	// count (and a wholly-zero group) skipped — replaces the old row of
	// identical chips and the cryptic "ZCP: 0h/4m".
	SummaryLine string

	ProblemsFallback bool
	// Problems holds every non-low problem (item 3's compact list);
	// ProblemsLow holds the low-severity ones, folded behind a "N low"
	// <details> of their own (FIX2 item 7) — the same one-line rows,
	// collapsed by default rather than dropped.
	Problems           []batchProblemView
	ProblemsLow        []batchProblemView
	HistoricalProblems []batchProblemView
	FallbackChecks     []checkFailureRow

	// InconclusiveBanner is FIX2 item 7's own top-of-page warning, "" unless
	// at least half this batch's runs ended inconclusive (buildInconclusiveBanner).
	InconclusiveBanner string

	Nav       listNav
	Groups    []batchRunGroupView
	MatchingN int
	TotalRuns int
	// SortByKey lets the runs table template place each sort header next
	// to the column it actually sorts, with a plain "Why / headline"
	// header (item 2) that sorts nothing in between — mirrors
	// pages_home.go's own SortByKey/sortHeadersByKey.
	SortByKey map[string]SortHeaderView

	Unassessed int
	// UnassessedNeverN/UnassessedFailedN split Unassessed (FIX3 item 2):
	// never had an observation at all, vs. one whose current observation
	// failed (error/unparsed) — they always sum to Unassessed.
	UnassessedNeverN  int
	UnassessedFailedN int
	AnyAssessed       bool
	ReassessEligibleN int
	// ModelOptions is shared with pages_run.go's own model picker (FIX3
	// item 1): one button per allowlisted model, properly labeled instead
	// of a <select> of raw ids — batch forms never preselect one (there is
	// no single "current model" across many runs), so every option's
	// Selected is always false here.
	ModelOptions []modelOptionView
	PrimaryModel modelOptionView
	OtherModels  []modelOptionView
}

// batchModelOptions is the batch forms' own model list (FIX3 item 1): the
// same labeled options the run page's picker uses, but never preselecting
// one — unlike a single run, a batch has no one "current model" shared by
// every one of its runs, so highlighting any single option would be
// arbitrary.
func batchModelOptions() []modelOptionView {
	out := make([]modelOptionView, 0, len(observer.Models))
	for _, m := range observer.Models {
		out = append(out, modelOptionView{Value: m, Label: modelLabel(m)})
	}
	return out
}

// jobStatusText resolves runID's live §8.5 status text for one batch row —
// "" for both when there is no Queue or nothing to report.
func (s *Server) jobStatusText(runID string) (jobText, failureText string) {
	if s.cfg.Queue == nil {
		return "", ""
	}
	if job, ok := s.cfg.Queue.Job(runID); ok {
		jobText = formatQueuedJobText(job)
	}
	if f, ok := s.cfg.Queue.LastFailure(runID); ok {
		failureText = fmt.Sprintf("The attempt at %s failed before anything was stored: %s. Re-assess to retry.", fmtTime(f.At), f.Err)
	}
	return jobText, failureText
}

func (s *Server) populateBatchSummary(data *batchPageData, rows []RunRow) {
	verdictTally := map[string]int{}
	causeCounts := newCauseClassCounts()
	for _, row := range rows {
		if row.Verdict != "" {
			verdictTally[row.Verdict]++
		}
		if assessmentWorkUnavailable(row) {
			data.UnavailableN++
		}
		data.TotalCostUsd += row.CostUsd
		if !row.CostKnown {
			data.CostUnknownN++
		}
		if row.Outcome != "" {
			data.ObservedN++
		}
		if row.Disputed {
			data.DisputedCount++
		}
		causeCounts = mergeCauseClassCounts(causeCounts, row.CauseCounts)
		switch row.Outcome {
		case observer.OutcomeOK:
			data.OutcomeOK++
		case observer.OutcomeProblem:
			data.OutcomeProblem++
		case observer.OutcomeInconclusive:
			data.OutcomeInconclusive++
		}
		if row.Observation != nil && row.Observation.Status == observationStatusOK {
			switch row.Observation.Goal.Reached {
			case "yes":
				data.GoalYes++
			case "partly":
				data.GoalPartly++
			case "no":
				data.GoalNo++
			}
		}
		if NeedsAssessment(row, runQueued(s.queueState, row.RunID)) {
			data.Unassessed++
			if row.Observation == nil {
				data.UnassessedNeverN++
			} else {
				data.UnassessedFailedN++
			}
		}
		if row.Outcome != "" && runDidWork(row) && !assessmentWorkUnavailable(row) {
			data.ReassessEligibleN++
		}
	}
	data.VerdictCounts = orderedVerdictCounts(verdictTally)
	data.Verdicts = verdictDisputeCounts(rows)
	data.ObservedM = len(rows)
	data.Cost = formatBatchCost(BatchRow{TotalCostUsd: data.TotalCostUsd, CostUnknownN: data.CostUnknownN, ObservedM: data.ObservedM})
	data.InconclusiveBanner = buildInconclusiveBanner(data.OutcomeInconclusive, data.ObservedM)
	for _, c := range causeCounts {
		data.CauseCounts = append(data.CauseCounts, causeCountView{Label: causeClassDisplay[c.Class], High: c.High, Medium: c.Medium})
		if c.Class == CauseClassZCP {
			data.ZCPHigh, data.ZCPMedium = c.High, c.Medium
		}
	}
	data.AnyAssessed = data.ReassessEligibleN > 0
	data.SummaryLine = buildBatchSummaryLine(data.GoalYes, data.GoalPartly, data.GoalNo, data.OutcomeOK, data.OutcomeProblem, data.OutcomeInconclusive, data.CauseCounts)
}

func (s *Server) handleBatchPage(w http.ResponseWriter, r *http.Request) {
	batch := strings.TrimPrefix(r.URL.Path, "/b/")
	if !farm.ValidBatchID(batch) {
		http.NotFound(w, r)
		return
	}

	spec := batchRunsListSpec()
	q, qerr := Parse(spec, r.URL.Query())
	if qerr != nil {
		var qe *QueryError
		errors.As(qerr, &qe)
		s.renderBadQuery(w, r, "", qe)
		return
	}

	rows, err := batchWindowRows(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, batch, s.queueState, s.runCache, s.summaryCache, s.logf)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	manifest, err := loadManifest(r.Context(), s.cfg.Store, batch)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	bc := newBatchContext(manifest)

	allBatches, err := loadBatchRows(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, s.queueState, s.runCache, s.summaryCache, s.logf)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	prevBatch, hasPrev := PreviousSameSet(allBatches, BatchRow{BatchID: batch, Set: manifest.Set, CreatedAt: bc.CreatedAt})

	var prevRows []RunRow
	if hasPrev {
		prevRows, err = batchWindowRows(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, prevBatch.BatchID, s.queueState, s.runCache, s.summaryCache, s.logf)
		if err != nil {
			writeStoreError(w, err)
			return
		}
	}

	busy := s.cfg.Queue != nil && s.cfg.Queue.BatchBusy(batch)
	data := batchPageData{
		Meta:         s.pageMeta(r, batch, "", busy),
		BatchID:      batch,
		CreatedAt:    bc.CreatedAt,
		Set:          manifest.Set,
		Note:         manifest.Note,
		Build:        bc.build(),
		ModelOptions: batchModelOptions(),
	}
	if len(data.ModelOptions) > 0 {
		data.PrimaryModel = data.ModelOptions[0]
		data.OtherModels = data.ModelOptions[1:]
	}
	if hasPrev {
		diff := CompareBatches(prevRows, rows)
		data.VsPrevious = &batchDiffView{PreviousBatchID: prevBatch.BatchID, Diff: diff, Comparison: buildBatchComparison(diff, rows)}
	}

	s.populateBatchSummary(&data, rows)

	// Problems in this batch (item 3), or the deterministic-checks
	// fallback when nothing here has ever been assessed — resolveBatchProblems
	// (below) owns the branching so handleBatchPage's own complexity doesn't
	// grow with it (maintidx).
	problems, problemsLow, fallback, fallbackChecks, err := s.resolveBatchProblems(r.Context(), rows, batch, hasPrev, prevBatch, prevRows)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	for _, problem := range append(problems, problemsLow...) {
		if problem.HitInBatch == 0 {
			data.HistoricalProblems = append(data.HistoricalProblems, problem)
			continue
		}
		if problem.Severity == observer.SeverityLow {
			data.ProblemsLow = append(data.ProblemsLow, problem)
		} else {
			data.Problems = append(data.Problems, problem)
		}
	}
	data.ProblemsFallback, data.FallbackChecks = fallback, fallbackChecks

	// Runs table (item 4): filter+sort through the shared engine so
	// counts/links/400 behave exactly like every other list (§8.7), then
	// bucket the already-sorted result into precedence groups — grouping
	// after sorting keeps "sort orders runs within each group" true by
	// construction.
	filtered, counts := batchRunsEngine().Apply(rows, q, s.now())
	data.MatchingN = len(filtered)
	data.TotalRuns = len(rows)
	byGroup := map[string][]batchRunView{}
	for _, row := range filtered {
		v := newBatchRunView(row)
		v.JobStatusText, v.PreStoreFailureText = s.jobStatusText(row.RunID)
		group := runGroupFor(row)
		switch group {
		case batchGroupFailedBlocked:
			switch row.Verdict {
			case farm.VerdictFailed:
				v.FirstFailedText, v.FirstFailedTitle = firstFailedCheckPlain(row.FailedChecks)
			case farm.VerdictBlocked:
				v.FirstFailedText = row.VerdictReason
			}
		case batchGroupNotFinished:
			v.Reason = notFinishedReason(row)
		}
		byGroup[group] = append(byGroup[group], v)
	}
	for _, name := range batchRunGroupOrder {
		rs, ok := byGroup[name]
		if !ok {
			continue
		}
		data.Groups = append(data.Groups, batchRunGroupView{Title: name, Runs: rs, Collapsed: name == batchGroupClean})
	}

	sortLabels := map[string]string{"problem": "Priority", paramScenario: "Scenario", "duration": "Duration", "cost": "Cost"}
	data.Nav = buildListNav("/b/"+batch, spec, q, r.URL.Query(), counts, sortLabels, batchRunsLabeler())
	data.SortByKey = sortHeadersByKey(data.Nav.Sorts)

	renderPage(w, "batch", data)
}
