// Package console: this file, pages_batch.go, owns GET /b/<batch> (§8.3
// FM-51's batch page) — split out of pages.go (S-SHELL item 1) so each page
// owns one file. Rewritten for plans/farm-console-clarity-2026-09-11-
// briefs/WAVE2.md's BATCH slice: the five §8.3 blocks (header + vs-previous,
// summary, problems in this batch (§8.6), runs grouped by precedence
// (§8.7), the assess callout (§8.5)).
package console

import (
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
	v := batchRunView{RunRow: row, State: row.ObserverState}
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
	display = fmt.Sprintf("%s: expected %s, got %s", disp, c.Expected, c.Observed)
	if disp != c.ID {
		title = fmt.Sprintf("%s: expected %s, got %s", c.ID, c.Expected, c.Observed)
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
				return verdictLabel(v)
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
				return verdictTooltip(v)
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
// only meaningful when HasPrevCompare is true.
type batchProblemView struct {
	Problem
	HasPrevCompare bool
	AlsoInPrev     bool
}

// buildBatchProblems implements item 3: §8.6 clustering over this batch's
// own runs plus (when one exists) the previous same-set batch's runs — just
// enough cross-batch data for BuildProblems' status computation and for
// deciding, per problem, whether it also hit that previous batch — narrowed
// to the problems that hit THIS batch (a problem clustering to a run
// outside both given batches never happens, since only these two batches'
// runs are fed in).
//
// Ambiguity (flagged per _common.md): §8.6/§8.3 do not spell out the run
// set BuildProblems should see for a single batch's page, or whether
// "also/not in <previous batch>" replaces or augments the normal five-way
// Status. This resolves it as: feed exactly [this batch, previous same-set
// batch] (bounded, and enough for a meaningful newest-build comparison
// without re-scanning the whole bucket); keep rendering the ordinary
// Status label (labels.go's problemStatusLabel) and ADD the also/not-in
// annotation alongside it, rather than replacing it.
func buildBatchProblems(rows []RunRow, batchID, batchSet string, batchCreatedAt time.Time, prev *BatchRow, prevRows []RunRow) []batchProblemView {
	all := make([]ProblemsRun, 0, len(rows)+len(prevRows))
	for _, r := range rows {
		all = append(all, ProblemsRun{Row: r, BatchSet: batchSet, BatchCreatedAt: batchCreatedAt})
	}
	if prev != nil {
		for _, r := range prevRows {
			all = append(all, ProblemsRun{Row: r, BatchSet: prev.Set, BatchCreatedAt: prev.CreatedAt})
		}
	}
	problems := BuildProblems(all)

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
		if !hitThis {
			continue
		}
		view := batchProblemView{Problem: p}
		if prev != nil {
			view.HasPrevCompare = true
			view.AlsoInPrev = hitPrev
		}
		out = append(out, view)
	}
	return out
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
}

// causeCountView is one row of the summary block's per-cause-class
// findings (item 2) — Label pre-resolved (labels.go's causeClassDisplay)
// so the template never needs a class->label lookup of its own.
type causeCountView struct {
	Label        string
	High, Medium int
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
	DisputedCount                                  int
	GoalYes, GoalPartly, GoalNo                    int
	OutcomeOK, OutcomeProblem, OutcomeInconclusive int
	CauseCounts                                    []causeCountView
	TotalCostUsd                                   float64
	CostUnknownN                                   int
	ObservedN, ObservedM                           int

	ProblemsFallback bool
	Problems         []batchProblemView
	FallbackChecks   []checkFailureRow

	Nav    listNav
	Groups []batchRunGroupView

	Unassessed   int
	AnyAssessed  bool
	ModelOptions []string
}

// jobStatusText resolves runID's live §8.5 status text for one batch row —
// "" for both when there is no Queue or nothing to report.
func (s *Server) jobStatusText(runID string) (jobText, failureText string) {
	if s.cfg.Queue == nil {
		return "", ""
	}
	if job, ok := s.cfg.Queue.Job(runID); ok {
		start := job.StartedAt
		if start.IsZero() {
			start = job.EnqueuedAt
		}
		jobText = fmt.Sprintf("Assessing with %s — started %s, usually 1–2 min; the result replaces the one below", job.Model, fmtTime(start))
	}
	if f, ok := s.cfg.Queue.LastFailure(runID); ok {
		failureText = fmt.Sprintf("The attempt at %s failed before anything was stored: %s. Re-assess to retry.", fmtTime(f.At), f.Err)
	}
	return jobText, failureText
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
		ModelOptions: observer.Models,
	}
	if hasPrev {
		diff := CompareBatches(prevRows, rows)
		data.VsPrevious = &batchDiffView{PreviousBatchID: prevBatch.BatchID, Diff: diff}
	}

	verdictTally := map[string]int{}
	causeCounts := newCauseClassCounts()
	for _, row := range rows {
		verdictTally[row.Verdict]++
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
		}
	}
	data.VerdictCounts = orderedVerdictCounts(verdictTally)
	data.ObservedM = len(rows)
	for _, c := range causeCounts {
		data.CauseCounts = append(data.CauseCounts, causeCountView{Label: causeClassDisplay[c.Class], High: c.High, Medium: c.Medium})
	}
	data.AnyAssessed = data.ObservedN > 0

	// Problems in this batch (item 3), or the deterministic-checks
	// fallback when nothing here has ever been assessed.
	if batchHasAnyAssessment(rows) {
		var prevPtr *BatchRow
		if hasPrev {
			prevPtr = &prevBatch
		}
		data.Problems = buildBatchProblems(rows, batch, manifest.Set, bc.CreatedAt, prevPtr, prevRows)
	} else {
		data.ProblemsFallback = true
		data.FallbackChecks = buildCheckFailureFallback(rows)
	}

	// Runs table (item 4): filter+sort through the shared engine so
	// counts/links/400 behave exactly like every other list (§8.7), then
	// bucket the already-sorted result into precedence groups — grouping
	// after sorting keeps "sort orders runs within each group" true by
	// construction.
	filtered, counts := batchRunsEngine().Apply(rows, q, s.now())
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

	renderPage(w, "batch", data)
}
