package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// observer.NotRecorded matches §7.2's rendering of a missing optional bundle file.

// apiLegendOverrides supplies markdown-legend-only wording for terms whose
// canonical labels.go glossary text defers to a page the markdown API never
// renders ("— see the table below" / "— see the problem status table
// below": /terms is the only surface with such a table beneath the
// glossary), plus terms glossaryTerms does not define at all but an API
// list line prints. Each string transcribes docs/spec-eval-farm.md §8.8's
// own inline prose for that concept — the same source glossaryTerms itself
// transcribes — so this can't drift from the spec even though it can't
// share labels.go's vocab tables (a different slice's write-set this
// round).
var apiLegendOverrides = map[string]string{
	"Verdict":  "passed (every check held) · failed (a check proved the run wrong) · blocked (could not be graded — reason shown) · not started · running · stalled (no result after the batch's deadline, plus 30 minutes)",
	"Severity": "high: the goal was missed, something was destroyed, or (for a test cause) the verdict is wrong · medium: it cost many steps or much time · low: ZCP text or behavior that is wrong but cost this run nothing",
	"Cause":    "ZCP guidance · ZCP tool · Zerops platform · Agent mistake · Test scenario · Test check",
	"Problem":  "The same finding across runs.",
	"Status":   "the problem's status: new (a regression) · first seen · recurring · gone (fixed, or not reproduced) · unconfirmed (not assessed on the newest build)",
	"Outcome":  "the assessment's verdict on the run: OK · Problem · Inconclusive · none (no current ok observation)",
	"Findings": "high/medium finding counts per cause class, e.g. \"zcp 1 high 0 med\"",
	"Hit":      "an assessed run in this call's own scope (its batch, or its since window) where the problem appeared",
}

// glossaryDef looks term up first in apiLegendOverrides, then in labels.go's
// §8.8 glossary (glossaryTerms) — "" for a term neither defines.
func glossaryDef(term string) string {
	if def, ok := apiLegendOverrides[term]; ok {
		return def
	}
	for _, g := range glossaryTerms {
		if g.Term == term {
			return g.Definition
		}
	}
	return ""
}

// legendLine renders §8.4's "every markdown list starts with a one-line
// legend of the terms it uses" from labels.go's authoritative §8.8
// glossary, so the API's wording can never drift from the pages' or
// /terms' own definitions. A term with no glossary entry is skipped rather
// than rendered blank.
func legendLine(terms ...string) string {
	parts := make([]string, 0, len(terms))
	for _, t := range terms {
		if def := glossaryDef(t); def != "" {
			parts = append(parts, t+" = "+def)
		}
	}
	return "Legend: " + strings.Join(parts, " · ") + "\n\n"
}

// queryErrorJSON is writeQueryError's JSON body shape (§8.7 FM-55: "the API
// answers 400 {error, allowed}") — a named type so a test asserting on it
// shares this declaration instead of redeclaring the same tag pair.
type queryErrorJSON struct {
	Error   string   `json:"error"`
	Allowed []string `json:"allowed"`
}

// writeQueryError answers a list endpoint's refused parameter (§8.7 FM-55):
// 400 {error, allowed} on the JSON twin, a one-line text on the markdown
// one — never the page's own rendered 400 (renderBadQuery, listnav.go).
func writeQueryError(w http.ResponseWriter, r *http.Request, qerr *QueryError) {
	if isJSONRequest(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(queryErrorJSON{qerr.Error(), qerr.Allowed}); err != nil {
			fmt.Fprintf(os.Stderr, "console: encode query-error response: %v\n", err)
		}
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintln(w, qerr.Error())
}

// asQueryError narrows err to *QueryError — Parse's only error type
// (query.go) — so every list handler can share one 400 path without
// re-checking the assertion at each call site.
func asQueryError(err error) *QueryError {
	var qerr *QueryError
	if errors.As(err, &qerr) {
		return qerr
	}
	return &QueryError{Param: "query"}
}

// --- §8.4 FM-52's GET /api/batches.md|.json (the Overview's batches table) ---

// VerdictCountItem is a JSON-tagged copy of batches.go's VerdictCount
// (which carries no tags — it is rendered through HTML templates, never
// marshaled): every API type needing this shape narrows to this one
// instead of embedding VerdictCount directly, so an untagged shared field
// never trips musttag on every caller that unmarshals through it.
type VerdictCountItem struct {
	Verdict string `json:"verdict"`
	Count   int    `json:"count"`
}

func verdictCountItems(vs []VerdictCount) []VerdictCountItem {
	out := make([]VerdictCountItem, len(vs))
	for i, v := range vs {
		out[i] = VerdictCountItem(v)
	}
	return out
}

// BatchListItem is one GET /api/batches.md|.json row (§8.4): BatchRow
// (batches.go), narrowed to what the API exposes.
type BatchListItem struct {
	BatchID       string             `json:"batchId"`
	CreatedAt     time.Time          `json:"createdAt"`
	Build         string             `json:"build"`
	Set           string             `json:"set"`
	Kind          string             `json:"kind"`
	VerdictCounts []VerdictCountItem `json:"verdictCounts"`
	CostUsd       float64            `json:"costUsd"`
	CostUnknownN  int                `json:"costUnknownN"`
	ObservedN     int                `json:"observedN"`
	ObservedM     int                `json:"observedM"`
	ZCPHigh       int                `json:"zcpHigh"`
	ZCPMedium     int                `json:"zcpMedium"`
	DisputedCount int                `json:"disputedCount"`
}

func batchListItemFromRow(b BatchRow) BatchListItem {
	return BatchListItem{
		BatchID: b.BatchID, CreatedAt: b.CreatedAt, Build: b.Build.Label(), Set: b.Set, Kind: b.Kind,
		VerdictCounts: verdictCountItems(b.VerdictCounts), CostUsd: b.TotalCostUsd, CostUnknownN: b.CostUnknownN,
		ObservedN: b.ObservedN, ObservedM: b.ObservedM, ZCPHigh: b.ZCPHigh, ZCPMedium: b.ZCPMedium,
		DisputedCount: b.DisputedCount,
	}
}

// renderBatchesMD renders GET /api/batches.md. defaultedKind is true when
// the request carried no kind= parameter at all — §8.7's default
// (kind=evaluation) then applied silently; item 5 (verification round 2)
// wants that said, never assumed. An explicit kind, even kind=evaluation
// itself, prints no such note.
func renderBatchesMD(items []BatchListItem, defaultedKind bool) string {
	var b strings.Builder
	b.WriteString(legendLine("Batch", "ZCP build", "Verdict", "Agent cost"))
	if defaultedKind {
		b.WriteString("(no kind= given: defaulted to kind=evaluation — add kind=all to see every batch)\n\n")
	}
	for _, it := range items {
		verdicts := make([]string, 0, len(it.VerdictCounts))
		for _, vc := range it.VerdictCounts {
			verdicts = append(verdicts, fmt.Sprintf("%s:%d", vc.Verdict, vc.Count))
		}
		cost := fmt.Sprintf("$%.2f", it.CostUsd)
		if it.CostUnknownN > 0 {
			cost += fmt.Sprintf(" (+%d unknown)", it.CostUnknownN)
		}
		fmt.Fprintf(&b, "- %s — %s — %s — %s — %s — cost %s — observed %d/%d\n",
			it.BatchID, it.CreatedAt.UTC().Format(time.RFC3339), it.Build, it.Set,
			strings.Join(verdicts, " "), cost, it.ObservedN, it.ObservedM)
	}
	return b.String()
}

// handleBatchesAPI implements GET /api/batches.md|.json (§8.4): the
// Overview's batches table, through the same query engine (batchEngine,
// batches.go) and query surface (batchListSpec) as the "/" page (§8.7).
func (s *Server) handleBatchesAPI(w http.ResponseWriter, r *http.Request) {
	q, err := Parse(batchListSpec(), r.URL.Query())
	if err != nil {
		writeQueryError(w, r, asQueryError(err))
		return
	}
	rows, err := loadBatchRows(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, s.queueState, s.runCache, s.summaryCache, s.logf)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	filtered, _ := batchEngine().Apply(rows, q, s.now())

	items := make([]BatchListItem, len(filtered))
	for i, row := range filtered {
		items[i] = batchListItemFromRow(row)
	}

	if isJSONRequest(r) {
		writeJSON(w, struct {
			Batches []BatchListItem `json:"batches"`
		}{items})
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, renderBatchesMD(items, r.URL.Query().Get(paramKind) == ""))
}

// --- §8.4/§8.6's GET /api/problems.md|.json ---------------------------------

// ProblemMemberItem is one ProblemItem.Members row (§8.6): a thin view over
// FindingRow plus the run link and step link its most-cited evidence needs.
type ProblemMemberItem struct {
	RunID      string    `json:"runId"`
	Batch      string    `json:"batch"`
	Scenario   string    `json:"scenario"`
	Build      string    `json:"build"`
	StartedAt  time.Time `json:"startedAt"`
	Severity   string    `json:"severity"`
	Cause      string    `json:"cause"`
	CauseClass string    `json:"causeClass"`
	Title      string    `json:"title"`
	LookAt     string    `json:"lookAt"`
	Fix        string    `json:"fix"`
	RunLink    string    `json:"runLink"`
	Quote      string    `json:"quote,omitempty"`
	StepLink   string    `json:"stepLink,omitempty"`
}

func problemMemberItem(m ProblemMember) ProblemMemberItem {
	item := ProblemMemberItem{
		RunID: m.RunID, Batch: m.Batch, Scenario: m.Scenario, Build: m.Build.Label(), StartedAt: m.StartedAt,
		Severity: m.Severity, Cause: m.CauseLabel, CauseClass: m.CauseClass, Title: m.Title, LookAt: m.LookAt, Fix: m.Fix,
		RunLink: fmt.Sprintf("/r/%s#f%d", m.RunID, m.Index+1),
	}
	if len(m.Evidence) > 0 {
		item.Quote = m.Evidence[0].Quote
		if m.Evidence[0].Step > 0 {
			item.StepLink = fmt.Sprintf("/r/%s#s%d", m.RunID, m.Evidence[0].Step)
		}
	}
	return item
}

// ProblemItem is one GET /api/problems.md|.json row (§8.6): a Problem
// (problems.go) with its display fields plus its members, resolved.
type ProblemItem struct {
	Key                  string              `json:"key"`
	Status               string              `json:"status"`
	Severity             string              `json:"severity"`
	CauseLabels          []string            `json:"causeLabels"`
	Surface              string              `json:"surface"`
	Title                string              `json:"title"`
	Fix                  string              `json:"fix"`
	Anchor               string              `json:"anchor"`
	HitOnNewestBuild     int                 `json:"hitOnNewestBuild"`
	RunsAssessedOnNewest int                 `json:"runsAssessedOnNewest"`
	RunsTotal            int                 `json:"runsTotal"`
	BatchesTotal         int                 `json:"batchesTotal"`
	BuildsTotal          int                 `json:"buildsTotal"`
	FirstSeen            time.Time           `json:"firstSeen"`
	LastSeen             time.Time           `json:"lastSeen"`
	Members              []ProblemMemberItem `json:"members"`
}

func problemItemFromProblem(p Problem) ProblemItem {
	members := make([]ProblemMemberItem, len(p.Members))
	for i, m := range p.Members {
		members[i] = problemMemberItem(m)
	}
	return ProblemItem{
		Key: p.Key, Status: p.Status, Severity: p.Severity, CauseLabels: p.CauseLabels,
		Surface: p.Surface, Title: p.Title, Fix: p.Fix, Anchor: p.Anchor,
		HitOnNewestBuild: p.HitOnNewestBuild, RunsAssessedOnNewest: p.RunsAssessedOnNewest,
		RunsTotal: p.RunsTotal, BatchesTotal: p.BatchesTotal, BuildsTotal: p.BuildsTotal,
		FirstSeen: p.FirstSeen, LastSeen: p.LastSeen, Members: members,
	}
}

func renderProblemsMD(items []ProblemItem) string {
	var b strings.Builder
	b.WriteString(legendLine("Problem", "Severity", "Cause", "Surface", "Anchor", "Hit", "Status"))
	for _, p := range items {
		fmt.Fprintf(&b, "- [%s · %s] %s — %s — hit %d/%d in scope — seen %d runs / %d batches / %d builds — status %s\n",
			p.Severity, strings.Join(p.CauseLabels, ","), p.Title, p.Surface,
			p.HitOnNewestBuild, p.RunsAssessedOnNewest, p.RunsTotal, p.BatchesTotal, p.BuildsTotal, p.Status)
		if p.Anchor != "" {
			fmt.Fprintf(&b, "  anchor: %q\n", p.Anchor)
		}
		if p.Fix != "" {
			fmt.Fprintf(&b, "  fix: %s\n", p.Fix)
		}
		for _, m := range p.Members {
			fmt.Fprintf(&b, "  · %s (%s, %s) %s\n", m.RunID, m.Batch, m.Build, m.RunLink)
		}
	}
	return b.String()
}

// forEachBatchRows calls fn with every batch's already-resolved run rows
// and manifest — the "scan every batch, tolerate one that fails to load"
// shape view.go's rowsSinceWindow implements for its own list surfaces,
// written once here for every api.go scan that needs the same tolerance
// (a batch that fails to load is skipped and logged, item 5) but not
// view.go's own StartedAt pre-filtering (each caller applies its own
// window, or none at all, after seeing the rows).
func (s *Server) forEachBatchRows(ctx context.Context, fn func(batchID string, manifest farm.BatchManifest, rows []RunRow)) error {
	batches, err := listBatchIDs(ctx, s.cfg.Store)
	if err != nil {
		return fmt.Errorf("console: scan batches: %w", err)
	}
	for _, b := range batches {
		manifest, err := loadManifest(ctx, s.cfg.Store, b)
		if err != nil {
			s.logf("skip batch %s: load manifest: %v", b, err)
			continue
		}
		rows, err := batchWindowRowsWithManifest(ctx, s.cfg.Store, s.cfg.ObserverDisabled, b, manifest, s.queueState, s.runCache, s.summaryCache, s.logf)
		if err != nil {
			s.logf("skip batch %s: %v", b, err)
			continue
		}
		fn(b, manifest, rows)
	}
	return nil
}

// allRunRows resolves every run row across every batch, with no time
// bounding at all: GET /api/runs.md's own `since`/`batch` filtering (like
// the Overview batches list's, batches.go) happens afterward through the
// query engine (apiRunsEngine.Apply) — the same "load everything, filter
// later" pattern a NoSinceDefault list needs (query.go's ListSpec doc:
// "an absent since leaves the window unbounded").
func (s *Server) allRunRows(ctx context.Context) ([]RunRow, error) {
	var out []RunRow
	err := s.forEachBatchRows(ctx, func(_ string, _ farm.BatchManifest, rows []RunRow) {
		out = append(out, rows...)
	})
	return out, err
}

// problemsRunsSinceWindow resolves every run row across every batch whose
// StartedAt falls in [now-window, now] (view.go's rowsSinceWindow own
// membership rule), paired with each run's batch Set/CreatedAt
// (problems.go's ProblemsRun) — the extra batch-level facts §8.6's builds/
// status computation needs beyond RunRow itself.
func (s *Server) problemsRunsSinceWindow(ctx context.Context, window time.Duration, now time.Time) ([]ProblemsRun, error) {
	since := now.Add(-window)
	var out []ProblemsRun
	err := s.forEachBatchRows(ctx, func(_ string, manifest farm.BatchManifest, rows []RunRow) {
		bc := newBatchContext(manifest)
		for _, row := range rows {
			if row.StartedAt.Before(since) || row.StartedAt.After(now) {
				continue
			}
			out = append(out, ProblemsRun{Row: row, BatchSet: manifest.Set, BatchCreatedAt: bc.CreatedAt})
		}
	})
	return out, err
}

// handleProblemsAPI implements GET /api/problems.md|.json (§8.4/§8.6): the
// same query surface as /problems (§8.7) — since resolved BEFORE
// BuildProblems (status is pinned over that window), every other filter
// applied after through problemEngine, exactly like the page.
func (s *Server) handleProblemsAPI(w http.ResponseWriter, r *http.Request) {
	q, err := Parse(problemListSpec(), r.URL.Query())
	if err != nil {
		writeQueryError(w, r, asQueryError(err))
		return
	}
	now := s.now()
	runs, err := s.problemsRunsSinceWindow(r.Context(), q.Since, now)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	problems := BuildProblems(runs)
	filtered, _ := problemEngine().Apply(problems, q, now)

	items := make([]ProblemItem, len(filtered))
	for i, p := range filtered {
		items[i] = problemItemFromProblem(p)
	}

	if isJSONRequest(r) {
		writeJSON(w, struct {
			Problems []ProblemItem `json:"problems"`
		}{items})
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, renderProblemsMD(items))
}

// --- §8.4's GET /api/digest.md ----------------------------------------------

// digestByteBudget is §8.4's "one call, at most 8 KB" cap.
const digestByteBudget = 8 * 1024

// digestReserve is left unallocated to the problems/failed-runs sections so
// the scope header, the unassessed-count line and a truncation note always
// fit inside digestByteBudget once appended after the budgeted sections.
const digestReserve = 300

// DigestResponse is GET /api/digest.md's JSON twin (§8.4): the same,
// possibly-truncated content the markdown carries.
type DigestResponse struct {
	Batches           []string           `json:"batches"`
	Builds            []string           `json:"builds"`
	VerdictCounts     []VerdictCountItem `json:"verdictCounts"`
	CostUsd           float64            `json:"costUsd"`
	CostUnknownN      int                `json:"costUnknownN"`
	Problems          []ProblemItem      `json:"problems"`
	OmittedProblems   int                `json:"omittedProblems,omitempty"`
	FailedBlockedRuns []RunsListItem     `json:"failedBlockedRuns"`
	OmittedFailedRuns int                `json:"omittedFailedRuns,omitempty"`
	UnassessedCount   int                `json:"unassessedCount"`
	Truncated         bool               `json:"truncated"`
}

// renderDigestHeader renders §8.4's scope header: batches, builds, verdict
// counts, cost.
func renderDigestHeader(windowLabel string, batches, builds []string, vcs []VerdictCountItem, cost float64, costUnknownN int) string {
	var b strings.Builder
	b.WriteString(legendLine("Batch", "ZCP build", "Verdict", "Problem", "Severity", "Cause", "Surface", "Anchor", "Hit", "Status", "Outcome", "Findings", "Agent cost"))
	fmt.Fprintf(&b, "# Digest\n\nwindow: %s\nbatches: %s\nbuilds: %s\n", windowLabel, strings.Join(batches, ", "), strings.Join(builds, ", "))
	verdicts := make([]string, len(vcs))
	for i, vc := range vcs {
		verdicts[i] = fmt.Sprintf("%s:%d", vc.Verdict, vc.Count)
	}
	fmt.Fprintf(&b, "verdicts: %s\n", strings.Join(verdicts, " "))
	cost1 := fmt.Sprintf("$%.2f", cost)
	if costUnknownN > 0 {
		cost1 += fmt.Sprintf(" (+%d unknown)", costUnknownN)
	}
	fmt.Fprintf(&b, "cost: %s\n", cost1)
	return b.String()
}

// renderProblemDigestLine is one digest.md problem line (§8.4: "severity,
// cause, surface, title, anchor, runs hit, status, where to look, fix, one
// run link per problem with its finding anchor").
func renderProblemDigestLine(p ProblemItem) string {
	link := ""
	if len(p.Members) > 0 {
		link = p.Members[0].RunLink
	}
	return fmt.Sprintf("- [%s · %s] %s — %s — anchor %q — hit %d/%d in scope — seen %d runs / %d batches / %d builds — status %s — %s — fix: %s\n",
		p.Severity, strings.Join(p.CauseLabels, ","), p.Title, p.Surface, p.Anchor,
		p.HitOnNewestBuild, p.RunsAssessedOnNewest, p.RunsTotal, p.BatchesTotal, p.BuildsTotal, p.Status, link, p.Fix)
}

// renderFailedRunDigestLine is one digest.md failed/blocked run line (§8.4:
// "failed and blocked runs with their failed checks and headline").
func renderFailedRunDigestLine(it RunsListItem) string {
	var b strings.Builder
	renderRunItemLine(&b, it)
	return b.String()
}

// fitLinesInBudget keeps as many leading lines as fit within budget bytes,
// returning the kept count and the number left over — §8.4's "truncation
// is said, never silent" needs the omitted count, not just a hard cut.
func fitLinesInBudget(budget int, lines []string) (kept, used, omitted int) {
	for _, l := range lines {
		if used+len(l) > budget {
			return kept, used, len(lines) - kept
		}
		used += len(l)
		kept++
	}
	return kept, used, 0
}

// handleDigest implements GET /api/digest.md?batch=<id> or ?since=<window>
// (§8.4): one call, at most 8 KB, for "what did the farm find" — the scope
// header, ranked problems (§8.6), failed/blocked runs, and the count of
// finished runs not yet assessed. batch wins when both are given, like
// /api/runs.md.
// outcomeOrNone is a run's assessment outcome as the API prints it: "none"
// when there is no current ok observation, as runs.md already says (§8.8).
func outcomeOrNone(o string) string {
	if o == "" {
		return outcomeNone
	}
	return o
}

// digestWindowDefault is item 2's default digest window (verification
// round 2: "default the digest's window to 30d") — digest.md previously
// fell through to ParseWindow's own bare 24h fallback, out of step with
// /problems' own 30d default (problemListSpec) over the same kind of data.
const digestWindowDefault = "30d"

// digestListSpec is digest.md's parameter surface: a batch or a window
// (§8.4) — anything else is refused like every other list (§8.7).
func digestListSpec() ListSpec {
	return ListSpec{Open: []string{paramBatch}, HasSince: true, DefaultSince: digestWindowDefault}
}

func (s *Server) handleDigest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := s.now()
	q := r.URL.Query()
	pq, err := Parse(digestListSpec(), q)
	if err != nil {
		writeQueryError(w, r, asQueryError(err))
		return
	}

	var rows []RunRow
	var problemsRuns []ProblemsRun
	var scopeBatches []string
	var windowLabel string

	if batch := q.Get("batch"); batch != "" {
		if !farm.ValidBatchID(batch) {
			http.NotFound(w, r)
			return
		}
		manifest, mErr := loadManifest(ctx, s.cfg.Store, batch)
		if mErr != nil {
			writeStoreError(w, mErr)
			return
		}
		rows, err = batchWindowRows(ctx, s.cfg.Store, s.cfg.ObserverDisabled, batch, s.queueState, s.runCache, s.summaryCache, s.logf)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		bc := newBatchContext(manifest)
		for _, row := range rows {
			problemsRuns = append(problemsRuns, ProblemsRun{Row: row, BatchSet: manifest.Set, BatchCreatedAt: bc.CreatedAt})
		}
		scopeBatches = []string{batch}
		windowLabel = "batch " + batch
	} else {
		// pq.Since is already resolved to digestWindowDefault (§8.7's
		// DefaultSince mechanism, the same one problems.md/findings.md
		// use) when the caller gave no since= — windowLabel mirrors that
		// same resolution for display rather than re-deriving it from the
		// duration.
		sinceParam := q.Get("since")
		if sinceParam == "" {
			sinceParam = digestWindowDefault
		}
		windowLabel = sinceParam
		window := pq.Since
		rows, err = rowsSinceWindow(ctx, s.cfg.Store, s.cfg.ObserverDisabled, window, now, s.queueState, s.runCache, s.summaryCache, s.logf)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		problemsRuns, err = s.problemsRunsSinceWindow(ctx, window, now)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		seen := make(map[string]bool)
		for _, row := range rows {
			if !seen[row.Batch] {
				seen[row.Batch] = true
				scopeBatches = append(scopeBatches, row.Batch)
			}
		}
		sort.Strings(scopeBatches)
	}

	builds := make(map[string]bool)
	verdictCounts := make(map[string]int)
	var totalCost float64
	costUnknownN, unassessed, assessmentFailed := 0, 0, 0
	var failedBlocked []RunsListItem
	for _, row := range rows {
		builds[row.Build.Label()] = true
		verdictCounts[row.Verdict]++
		totalCost += row.CostUsd
		if !row.CostKnown {
			costUnknownN++
		}
		// item 1 (verification round 2): a run whose current observation
		// failed to parse (status error/unparsed) still NEEDS an assessment
		// — a bare Observation == nil check missed it, undercounting
		// "Unassessed" and leaving it out of the "needs an assessment"
		// picture the digest exists to give.
		if NeedsAssessment(row, runQueued(s.queueState, row.RunID)) {
			unassessed++
			if row.Observation != nil && (row.Observation.Status == observationStatusError || row.Observation.Status == observationStatusUnparsed) {
				assessmentFailed++
			}
		}
		if row.Verdict == farm.VerdictFailed || row.Verdict == farm.VerdictBlocked {
			failedBlocked = append(failedBlocked, runsListItemFromRow(row))
		}
	}
	sort.SliceStable(failedBlocked, func(i, j int) bool { return failedBlocked[i].StartedAt.After(failedBlocked[j].StartedAt) })
	buildList := make([]string, 0, len(builds))
	for b := range builds {
		buildList = append(buildList, b)
	}
	sort.Strings(buildList)

	problems := BuildProblems(problemsRuns)
	problemItems := make([]ProblemItem, len(problems))
	for i, p := range problems {
		problemItems[i] = problemItemFromProblem(p)
	}

	header := renderDigestHeader(windowLabel, scopeBatches, buildList, verdictCountItems(orderedVerdictCounts(verdictCounts)), totalCost, costUnknownN)
	budget := digestByteBudget - len(header) - digestReserve
	budget = max(budget, 0)

	problemLines := make([]string, len(problemItems))
	for i, p := range problemItems {
		problemLines[i] = renderProblemDigestLine(p)
	}
	keptProblems, used, omittedProblems := fitLinesInBudget(budget, problemLines)

	runLines := make([]string, len(failedBlocked))
	for i, it := range failedBlocked {
		runLines[i] = renderFailedRunDigestLine(it)
	}
	keptRuns, _, omittedRuns := fitLinesInBudget(budget-used, runLines)

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\nProblems:\n")
	for _, l := range problemLines[:keptProblems] {
		b.WriteString(l)
	}
	if omittedProblems > 0 {
		fmt.Fprintf(&b, "… %d more problem(s) not shown (truncated at %d bytes — see /api/problems.md)\n", omittedProblems, digestByteBudget)
	}
	b.WriteString("\nFailed/blocked runs:\n")
	for _, l := range runLines[:keptRuns] {
		b.WriteString(l)
	}
	if omittedRuns > 0 {
		fmt.Fprintf(&b, "… %d more failed/blocked run(s) not shown (truncated at %d bytes — see /api/runs.md)\n", omittedRuns, digestByteBudget)
	}
	fmt.Fprintf(&b, "\nUnassessed: %d finished run(s) not yet assessed", unassessed)
	if assessmentFailed > 0 {
		fmt.Fprintf(&b, " (%d assessment failed)", assessmentFailed)
	}
	b.WriteString("\n")

	truncated := omittedProblems > 0 || omittedRuns > 0

	if isJSONRequest(r) {
		writeJSON(w, DigestResponse{
			Batches: scopeBatches, Builds: buildList, VerdictCounts: verdictCountItems(orderedVerdictCounts(verdictCounts)),
			CostUsd: totalCost, CostUnknownN: costUnknownN,
			Problems: problemItems[:keptProblems], OmittedProblems: omittedProblems,
			FailedBlockedRuns: failedBlocked[:keptRuns], OmittedFailedRuns: omittedRuns,
			UnassessedCount: unassessed, Truncated: truncated,
		})
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, b.String())
}

// --- §8.4 FM-52 JSON shapes ---

// RunsListObservation is one runs-list row's "observation" field.
type RunsListObservation struct {
	ObsID            string   `json:"obsId"`
	Status           string   `json:"status"`
	Headline         string   `json:"headline"`
	FindingTitles    []string `json:"findingTitles"`
	UnverifiedQuotes int      `json:"unverifiedQuotes"`
}

// CauseCountItem is a JSON-tagged copy of findings_model.go's
// CauseClassCount (untagged — it is rendered through HTML templates,
// never marshaled): kept separate for the same reason as
// VerdictCountItem above (batches.go).
type CauseCountItem struct {
	Class  string `json:"class"`
	High   int    `json:"high"`
	Medium int    `json:"medium"`
	Low    int    `json:"low"`
}

func causeCountItems(cs []CauseClassCount) []CauseCountItem {
	out := make([]CauseCountItem, len(cs))
	for i, c := range cs {
		out[i] = CauseCountItem(c)
	}
	return out
}

// observerStateAssessmentFailed is §8.4's 6th JSON observerState value.
const observerStateAssessmentFailed = "assessment failed"

// apiObserverState implements §8.4's JSON observerState: RunRow.ObserverState
// (view.go's resolveObserverState, its own 5-value precedence) refined with
// a 6th value, observerStateAssessmentFailed, for a run whose current
// observation's status is error/unparsed — a distinction
// resolveObserverState's own enum cannot make without breaking its locked
// signature (the same reasoning view.go's ObserverStateText already applies
// at the text-wording layer, which this sits alongside rather than
// duplicates).
func apiObserverState(row RunRow) string {
	if row.Observation != nil && (row.Observation.Status == observationStatusError || row.Observation.Status == observationStatusUnparsed) {
		return observerStateAssessmentFailed
	}
	return row.ObserverState
}

// RunsListItem is one GET /api/runs.md|.json row (§8.4: "run id, scenario,
// verdict (+ reason), outcome, findings high/medium per cause class, failed
// check ids, headline").
type RunsListItem struct {
	RunID             string               `json:"runId"`
	Batch             string               `json:"batch"`
	Scenario          string               `json:"scenario"`
	Verdict           string               `json:"verdict"`
	VerdictReason     string               `json:"verdictReason,omitempty"`
	StartedAt         time.Time            `json:"startedAt"`
	DurationSec       float64              `json:"durationSec"`
	CostUsd           float64              `json:"costUsd"`
	CostKnown         bool                 `json:"costKnown"`
	Outcome           string               `json:"outcome"`
	CauseCounts       []CauseCountItem     `json:"causeCounts"`
	FailedCheckIDs    []string             `json:"failedCheckIds,omitempty"`
	ObserverState     string               `json:"observerState"`
	ObserverStateText string               `json:"observerStateText"`
	Observation       *RunsListObservation `json:"observation,omitempty"`
}

// RunDetail is GET /api/runs/<runId>.md|.json (FM-52).
type RunDetail struct {
	RunID         string    `json:"runId"`
	Batch         string    `json:"batch"`
	Scenario      string    `json:"scenario"`
	Verdict       string    `json:"verdict"`
	VerdictReason string    `json:"verdictReason,omitempty"`
	StartedAt     time.Time `json:"startedAt"`
	DurationSec   float64   `json:"durationSec"`
	CostUsd       float64   `json:"costUsd"`
	CostKnown     bool      `json:"costKnown"`
	// Build is the candidate's §8.8 display label (BatchListItem/
	// ProblemMemberItem/FindingItem already expose "build" this same way,
	// never as a raw sha) — CandidateSha256/EvaluatorSha256 stay alongside
	// it as the exact identity a caller may still need.
	Build             string                `json:"build"`
	CandidateSha256   string                `json:"candidateSha256"`
	EvaluatorSha256   string                `json:"evaluatorSha256"`
	StepCount         int                   `json:"stepCount"`
	ObserverState     string                `json:"observerState"`
	ObserverStateText string                `json:"observerStateText"`
	Observation       *observer.Observation `json:"observation,omitempty"`
	OlderObsIDs       []string              `json:"olderObsIds"`
	FailedChecks      []FailedCheck         `json:"failedChecks"`
	EvidenceSteps     []int                 `json:"evidenceSteps"`
	// TaskPromptURL and SelfReviewURL are §8.4's "links to the task prompt
	// and self-review": step 1 is always the synthesized task-prompt step
	// (observer.BuildSteps), so the task prompt is exactly steps.md's
	// single-step view of it.
	TaskPromptURL string `json:"taskPromptUrl"`
	SelfReviewURL string `json:"selfReviewUrl"`
}

// StepJSON is one element of GET /api/runs/<runId>/steps.md|.json (FM-52).
type StepJSON struct {
	N         int    `json:"n"`
	Kind      string `json:"kind"`
	Tool      string `json:"tool,omitempty"`
	Input     string `json:"input,omitempty"`
	Result    string `json:"result,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	// CutNote says what a truncated Result dropped (item 3, verification
	// round 2) — in particular whether an evidence citation's quote fell
	// past the cut, and where to find it, mirroring the markdown note
	// cutToolResult already builds.
	CutNote string `json:"cutNote,omitempty"`
	IsError bool   `json:"isError,omitempty"`
	Text    string `json:"text,omitempty"`
}

// FindingItem is one element of GET /api/findings.md|.json (§8.4: "every
// finding in scope: batch, scenario, build, run id, started, severity,
// cause, surface, anchor, title, what, steps, quotes found n/m, where to
// look, fix"). Owner is kept alongside Cause (its §8.8 display label) —
// pages_findings.go (a parallel slice's write-set) still filters/groups on
// it directly.
type FindingItem struct {
	Owner          string    `json:"owner"`
	Batch          string    `json:"batch"`
	Scenario       string    `json:"scenario"`
	Build          string    `json:"build"`
	RunID          string    `json:"runId"`
	StartedAt      time.Time `json:"startedAt"`
	Severity       string    `json:"severity"`
	Cause          string    `json:"cause"`
	CauseClass     string    `json:"causeClass"`
	Surface        string    `json:"surface,omitempty"`
	Anchor         string    `json:"anchor,omitempty"`
	Title          string    `json:"title"`
	What           string    `json:"what"`
	Steps          []int     `json:"steps"`
	QuotesVerified int       `json:"quotesVerified"`
	QuotesTotal    int       `json:"quotesTotal"`
	LookAt         string    `json:"lookAt"`
	Fix            string    `json:"fix"`
}

func isJSONRequest(r *http.Request) bool {
	return strings.HasSuffix(r.URL.Path, ".json")
}

// runsListItemFromRow narrows a RunRow (view.go) to a runs-list row.
func runsListItemFromRow(row RunRow) RunsListItem {
	item := RunsListItem{
		RunID: row.RunID, Batch: row.Batch, Scenario: row.Scenario, Verdict: row.Verdict,
		VerdictReason: row.VerdictReason,
		StartedAt:     row.StartedAt, DurationSec: row.DurationSec, CostUsd: row.CostUsd, CostKnown: row.CostKnown,
		Outcome: outcomeOrNone(row.Outcome), CauseCounts: causeCountItems(row.CauseCounts),
		ObserverState: apiObserverState(row), ObserverStateText: row.ObserverStateText,
	}
	for _, c := range row.FailedChecks {
		item.FailedCheckIDs = append(item.FailedCheckIDs, c.ID)
	}
	sort.Strings(item.FailedCheckIDs)
	if row.Observation != nil {
		item.Observation = &RunsListObservation{
			ObsID: row.Observation.ObsID, Status: row.Observation.Status, Headline: row.Observation.Headline,
			FindingTitles: findingTitles(row.Observation), UnverifiedQuotes: unverifiedQuotes(row.Observation),
		}
	}
	return item
}

func findingTitles(obs *observer.Observation) []string {
	titles := make([]string, 0, len(obs.Findings))
	for _, f := range obs.Findings {
		titles = append(titles, f.Title)
	}
	return titles
}

func unverifiedQuotes(obs *observer.Observation) int {
	n := 0
	for _, f := range obs.Findings {
		for _, e := range f.Evidence {
			if !e.Verified {
				n++
			}
		}
	}
	return n
}

// renderRunItemLine renders one runs-list row's own line: run id, scenario,
// verdict (+ reason), outcome, findings high/medium per cause class, failed
// check ids, headline (§8.4).
func renderRunItemLine(b *strings.Builder, it RunsListItem) {
	headline := "(" + it.ObserverState + ")"
	if it.Observation != nil && it.Observation.Headline != "" {
		headline = it.Observation.Headline
	}
	verdict := it.Verdict
	if it.VerdictReason != "" {
		verdict += " (" + it.VerdictReason + ")"
	}
	outcome := it.Outcome
	if outcome == "" {
		outcome = outcomeNone
	}
	var findings []string
	for _, c := range it.CauseCounts {
		if c.High > 0 || c.Medium > 0 {
			findings = append(findings, fmt.Sprintf("%s %d high %d med", c.Class, c.High, c.Medium))
		}
	}
	fmt.Fprintf(b, "- %s — %s — %s — outcome %s — findings %s — failed checks %s — %s\n",
		it.RunID, it.Scenario, verdict, outcome, strings.Join(findings, " "),
		strings.Join(it.FailedCheckIDs, ","), headline)
}

// groupRunItemsByBatch groups items by Batch, preserving each group's own
// relative order, and returns the group order — batches sorted by their
// own newest row's StartedAt descending, so "grouped under batch headers"
// (§8.4) still reads newest-first overall.
func groupRunItemsByBatch(items []RunsListItem) (order []string, groups map[string][]RunsListItem) {
	groups = make(map[string][]RunsListItem)
	newest := make(map[string]time.Time)
	for _, it := range items {
		if _, ok := groups[it.Batch]; !ok {
			order = append(order, it.Batch)
		}
		groups[it.Batch] = append(groups[it.Batch], it)
		if it.StartedAt.After(newest[it.Batch]) {
			newest[it.Batch] = it.StartedAt
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return newest[order[i]].After(newest[order[j]]) })
	return order, groups
}

// renderRunsListMD renders GET /api/runs.md (§8.4): newest first, grouped
// under "## <batch>" headers.
func renderRunsListMD(items []RunsListItem) string {
	var b strings.Builder
	b.WriteString(legendLine("Run", "Verdict", "Outcome", "Findings"))
	order, groups := groupRunItemsByBatch(items)
	for _, batch := range order {
		fmt.Fprintf(&b, "## %s\n", batch)
		for _, it := range groups[batch] {
			renderRunItemLine(&b, it)
		}
	}
	return b.String()
}

// handleRunsList implements GET /api/runs.md|.json?since=<window> and
// ?batch=<id> (§8.4/§8.7): the same query surface (apiRunsListSpec,
// view.go) as the batch-runs list — verdict/outcome/cause filters,
// sort=newest — through apiRunsEngine, never hand-parsed. batch wins when
// both are given (its own window, not `since`, scopes the result).
func (s *Server) handleRunsList(w http.ResponseWriter, r *http.Request) {
	q, err := Parse(apiRunsListSpec(), r.URL.Query())
	if err != nil {
		writeQueryError(w, r, asQueryError(err))
		return
	}

	ctx := r.Context()
	var rows []RunRow
	if batch, ok := q.Open[paramBatch]; ok && batch != "" {
		if !farm.ValidBatchID(batch) {
			http.NotFound(w, r)
			return
		}
		rows, err = batchWindowRows(ctx, s.cfg.Store, s.cfg.ObserverDisabled, batch, s.queueState, s.runCache, s.summaryCache, s.logf)
		q.Since = 0 // batch wins: its own runs are the whole scope, not a time window
	} else {
		rows, err = s.allRunRows(ctx)
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}

	filtered, _ := apiRunsEngine().Apply(rows, q, s.now())
	items := make([]RunsListItem, len(filtered))
	for i, row := range filtered {
		items[i] = runsListItemFromRow(row)
	}

	if isJSONRequest(r) {
		writeJSON(w, struct {
			Runs []RunsListItem `json:"runs"`
		}{items})
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, renderRunsListMD(items))
}

func runDetailFromRow(row RunRow) RunDetail {
	steps := evidenceSteps(row.Observation)
	older := row.OlderObsIDs
	if older == nil {
		older = []string{}
	}
	failed := row.FailedChecks
	if failed == nil {
		failed = []FailedCheck{}
	}
	if steps == nil {
		steps = []int{}
	}
	return RunDetail{
		RunID: row.RunID, Batch: row.Batch, Scenario: row.Scenario, Verdict: row.Verdict,
		VerdictReason: row.VerdictReason,
		StartedAt:     row.StartedAt, DurationSec: row.DurationSec, CostUsd: row.CostUsd, CostKnown: row.CostKnown,
		Build:           row.Build.Label(),
		CandidateSha256: row.CandidateSha256, EvaluatorSha256: row.EvaluatorSha256, StepCount: row.StepCount,
		ObserverState: apiObserverState(row), ObserverStateText: row.ObserverStateText, Observation: row.Observation,
		OlderObsIDs: older, FailedChecks: failed, EvidenceSteps: steps,
		TaskPromptURL: fmt.Sprintf("/api/runs/%s/steps.md?n=1", row.RunID),
		SelfReviewURL: fmt.Sprintf("/api/runs/%s/self-review.md", row.RunID),
	}
}

// formatStepRanges collapses a sorted, deduplicated step-number list into
// compact ranges ("12-15, 18, 22-23") for the run-detail markdown's
// "the step ranges its evidence cites" line (§8.3 FM-51).
func formatStepRanges(steps []int) string {
	if len(steps) == 0 {
		return "(none)"
	}
	var parts []string
	start, prev := steps[0], steps[0]
	flush := func() {
		if start == prev {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", start, prev))
		}
	}
	for _, n := range steps[1:] {
		if n == prev+1 {
			prev = n
			continue
		}
		flush()
		start, prev = n, n
	}
	flush()
	return strings.Join(parts, ", ")
}

func renderRunDetailMD(d RunDetail) string {
	var b strings.Builder
	verdict := d.Verdict
	if d.VerdictReason != "" {
		verdict += " (" + d.VerdictReason + ")"
	}
	cost := "— (not recorded)"
	if d.CostKnown {
		cost = fmt.Sprintf("$%.4f", d.CostUsd)
	}
	// Item 5 (verification round 2): candidate and evaluator otherwise show
	// as two indistinguishable raw 64-hex shas — use the §8.8 build label
	// for both (the evaluator has no recorded git revision, so it always
	// falls back to BuildInfo's "build <sha12>" form).
	evaluatorBuild := BuildInfo{Sha256: d.EvaluatorSha256}.Label()
	fmt.Fprintf(&b, "# %s\n\nscenario: %s\nbatch: %s\nverdict: %s\nstarted: %s\nduration: %.1fs\nagent cost: %s\nZCP build: %s\nevaluator build: %s\nsteps: %d\nassessment: %s\ntask prompt: %s\nself-review: %s\n\n",
		d.RunID, d.Scenario, d.Batch, verdict, d.StartedAt.UTC().Format(time.RFC3339),
		d.DurationSec, cost, d.Build, evaluatorBuild, d.StepCount, d.ObserverStateText,
		d.TaskPromptURL, d.SelfReviewURL)

	if d.Observation != nil {
		b.WriteString(observer.Render(*d.Observation))
	} else {
		b.WriteString("assessment: none\n")
	}

	b.WriteString("\nFailed/blocked checks:\n")
	if len(d.FailedChecks) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, c := range d.FailedChecks {
			fmt.Fprintf(&b, "- %s %s expected=%q observed=%q source=%s\n", c.ID, c.Result, c.Expected, c.Observed, c.Source)
		}
	}

	b.WriteString("\nEvidence steps: " + formatStepRanges(d.EvidenceSteps) + "\n")
	return b.String()
}

// writeStoreError maps a view.go lookup error to its HTTP status: a not-
// found id (already grammar/existence checked) is 404; anything else is a
// genuine backend failure (502 — the bucket, not the caller, is at fault).
func writeStoreError(w http.ResponseWriter, err error) {
	if isNotFoundErr(err) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.Error(w, err.Error(), http.StatusBadGateway)
}

func isNotFoundErr(err error) bool {
	return errors.Is(err, ErrBatchNotFound) || errors.Is(err, ErrRunNotFound) || errors.Is(err, observer.ErrResultsNotFound)
}

// handleRunsSubroute dispatches every GET /api/runs/<rest> request: a bare
// "<runId>.md|.json" is the run detail; "<runId>/steps.md|.json",
// "<runId>/self-review.md|.json" and "<runId>/files/<path>" are its
// sub-resources (FM-52). Every id is checked against its FM-47 grammar
// before any store call (TestAPI_InvalidRunOrBatchIDRejected).
func (s *Server) handleRunsSubroute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/runs/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}

	runID, tail, hasTail := strings.Cut(rest, "/")
	if !hasTail {
		id, ok := trimMDOrJSON(runID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		s.handleRunDetail(w, r, id)
		return
	}

	switch {
	case tail == "steps.md" || tail == "steps.json":
		s.handleSteps(w, r, runID)
	case tail == "self-review.md" || tail == "self-review.json":
		s.handleSelfReview(w, r, runID)
	case strings.HasPrefix(tail, "files/"):
		s.handleFile(w, r, runID, strings.TrimPrefix(tail, "files/"))
	case strings.HasPrefix(tail, "observations/"):
		s.handleObservation(w, r, runID, strings.TrimPrefix(tail, "observations/"))
	default:
		http.NotFound(w, r)
	}
}

// handleObservation implements GET
// /api/runs/<runId>/observations/<obsId>.md|.json (§8.4): one stored
// observation, verbatim. A missing or foreign obsId is 404.
func (s *Server) handleObservation(w http.ResponseWriter, r *http.Request, runID, obsIDWithExt string) {
	if !farm.ValidRunID(runID) {
		http.NotFound(w, r)
		return
	}
	obsID, ok := trimMDOrJSON(obsIDWithExt)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	store := observer.NewStore(s.cfg.Store)
	ids, err := store.ListObservations(ctx, runID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !slices.Contains(ids, obsID) {
		http.NotFound(w, r)
		return
	}
	obs, err := store.GetObservation(ctx, runID, obsID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if isJSONRequest(r) {
		writeJSON(w, obs)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, observer.Render(obs))
}

func trimMDOrJSON(s string) (string, bool) {
	if base, ok := strings.CutSuffix(s, ".md"); ok {
		return base, true
	}
	if base, ok := strings.CutSuffix(s, ".json"); ok {
		return base, true
	}
	return "", false
}

func (s *Server) handleRunDetail(w http.ResponseWriter, r *http.Request, runID string) {
	if !farm.ValidRunID(runID) {
		http.NotFound(w, r)
		return
	}
	row, err := loadRunRow(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, runID, s.queueState, s.runCache, s.summaryCache)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	detail := runDetailFromRow(row)
	if isJSONRequest(r) {
		writeJSON(w, detail)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, renderRunDetailMD(detail))
}

// loadSteps rebuilds runID's full, untruncated step record (§7.2) straight
// from the bucket.
func loadSteps(ctx context.Context, store observer.ObjectStore, runID string) ([]observer.Step, error) {
	bundle, err := observer.NewSinkBundle(ctx, store, runID)
	if err != nil {
		return nil, fmt.Errorf("console: load steps: new bundle: %w", err)
	}
	resultsDir, err := observer.ResultsDir(bundle)
	if err != nil {
		return nil, fmt.Errorf("console: load steps: results dir: %w", err)
	}
	taskPrompt, err := observer.LoadTaskPrompt(bundle, resultsDir)
	if err != nil {
		return nil, fmt.Errorf("console: load steps: task prompt: %w", err)
	}
	transcript, err := observer.LoadTranscript(bundle, resultsDir)
	if err != nil {
		return nil, fmt.Errorf("console: load steps: transcript: %w", err)
	}
	meta, err := observer.LoadMeta(bundle, resultsDir)
	if err != nil {
		return nil, fmt.Errorf("console: load steps: meta: %w", err)
	}
	return observer.BuildSteps(taskPrompt, transcript, observer.ResumeReplies(meta))
}

// cutToolResult implements §8.4's "a tool result over 2,000 chars is cut
// (said) unless full=1" — reusing pages.go's observerRawCap, the same
// 2,000-char figure §7.5 already uses for a failed observation's raw
// answer. When quote (an evidence citation's exact text, item 3 of the
// verification round) does not survive the from-start cut, the note
// appends the full line that contains it — recover run step 16's own bug,
// where an agent fetching a step to check an anchor quote never saw text
// the cut had already dropped. Returns the (possibly cut) result, whether
// it was cut, and a note describing what — always non-empty when cut is
// true, always empty otherwise.
func cutToolResult(text, quote string, full bool) (result string, cut bool, note string) {
	if full {
		return text, false, ""
	}
	r := []rune(text)
	if len(r) <= observerRawCap {
		return text, false, ""
	}
	kept := string(r[:observerRawCap])
	note = fmt.Sprintf(" …[truncated at %d chars; add full=1 for the rest]", observerRawCap)
	if quote != "" && !strings.Contains(kept, quote) && strings.Contains(text, quote) {
		note += "\n  cited quote (past the cut): " + quoteLine(text, quote)
	}
	return kept, true, note
}

// quoteLineContext is how much text quoteLine keeps on each side of a
// found quote.
const quoteLineContext = 200

// quoteLine returns a short window of text centered on quote's occurrence
// — cutToolResult's "always append the matching line" fallback (item 3) —
// bounded by quoteLineContext characters of context on each side, or the
// nearest newline if closer, so one very long line can't push the quote
// itself back out of the returned snippet (a bug an earlier version of
// this function had: capping a long single-line result from its start
// dropped a quote that sat near the end, the exact failure item 3
// describes). Returns quote itself if text does not actually contain it
// (defensive; every call site already checked strings.Contains first).
func quoteLine(text, quote string) string {
	idx := strings.Index(text, quote)
	if idx < 0 {
		return quote
	}
	end := idx + len(quote)
	start := max(0, idx-quoteLineContext)
	if nl := strings.LastIndexByte(text[start:idx], '\n'); nl >= 0 {
		start += nl + 1
	}
	stop := min(len(text), end+quoteLineContext)
	if nl := strings.IndexByte(text[end:stop], '\n'); nl >= 0 {
		stop = end + nl
	}
	prefix, suffix := "", ""
	if start > 0 {
		prefix = ellipsisMark
	}
	if stop < len(text) {
		suffix = ellipsisMark
	}
	return prefix + text[start:stop] + suffix
}

// ellipsisMark is quoteLine's cut marker on each side of its window.
const ellipsisMark = "…"

func stepJSON(s observer.Step, quote string, full bool) StepJSON {
	out := StepJSON{N: s.N, Kind: string(s.Kind)}
	if s.Kind == observer.StepTool {
		out.Tool, out.Input, out.IsError = s.ToolName, s.ToolInputJSON, s.ToolIsError
		out.Result, out.Truncated, out.CutNote = cutToolResult(s.ToolResultText, quote, full)
		return out
	}
	out.Text = s.Text
	return out
}

func renderStepsMD(steps []observer.Step, quotes map[int]string, full bool) string {
	var b strings.Builder
	for _, s := range steps {
		switch s.Kind {
		case observer.StepTool:
			fmt.Fprintf(&b, "#%d tool %s %s\n", s.N, s.ToolName, s.ToolInputJSON)
			result, cut, note := cutToolResult(s.ToolResultText, quotes[s.N], full)
			if cut {
				result += note
			}
			if s.ToolIsError {
				fmt.Fprintf(&b, "  → ERROR %s\n", result)
			} else {
				fmt.Fprintf(&b, "  → %s\n", result)
			}
		case observer.StepUser, observer.StepThinking, observer.StepAgent:
			fmt.Fprintf(&b, "#%d %s %s\n", s.N, s.Kind, s.Text)
		}
	}
	return b.String()
}

// citedQuotesByStep maps a step number to the first finding-evidence quote
// that cites it, from runID's current observation — item 3's input for
// keeping a truncated tool result's cited text visible. Best-effort: a run
// with no current observation (loadRunRow error, or Observation nil) yields
// an empty map rather than failing the request — steps.md must still work
// without one.
func citedQuotesByStep(ctx context.Context, s *Server, runID string) map[int]string {
	quotes := make(map[int]string)
	row, err := loadRunRow(ctx, s.cfg.Store, s.cfg.ObserverDisabled, runID, s.queueState, s.runCache, s.summaryCache)
	if err != nil || row.Observation == nil {
		return quotes
	}
	for _, f := range row.Observation.Findings {
		for _, e := range f.Evidence {
			if e.Step > 0 {
				if _, exists := quotes[e.Step]; !exists {
					quotes[e.Step] = e.Quote
				}
			}
		}
	}
	return quotes
}

// handleSteps implements GET
// /api/runs/<runId>/steps.md|.json?from=<n>&to=<m>, or ?n=<step> (§8.4): a
// tool result over 2,000 chars is cut (said) unless full=1; an empty
// thinking block is left out.
func (s *Server) handleSteps(w http.ResponseWriter, r *http.Request, runID string) {
	if !farm.ValidRunID(runID) {
		http.NotFound(w, r)
		return
	}
	from, to, err := parseFromToOrN(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	full := r.URL.Query().Get("full") == "1"
	all, err := loadSteps(r.Context(), s.cfg.Store, runID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	quotes := citedQuotesByStep(r.Context(), s, runID)
	var sel []observer.Step
	for _, st := range all {
		if st.N < from || st.N > to {
			continue
		}
		if st.Kind == observer.StepThinking && strings.TrimSpace(st.Text) == "" {
			continue // §8.4: "empty thinking blocks are left out"
		}
		sel = append(sel, st)
	}
	if isJSONRequest(r) {
		out := make([]StepJSON, len(sel))
		for i, st := range sel {
			out[i] = stepJSON(st, quotes[st.N], full)
		}
		writeJSON(w, out)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, renderStepsMD(sel, quotes, full))
}

func parseFromTo(q map[string][]string) (from, to int, err error) {
	get := func(k string) (int, error) {
		v := ""
		if vs, ok := q[k]; ok && len(vs) > 0 {
			v = vs[0]
		}
		if v == "" {
			return 0, fmt.Errorf("%s is required", k)
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("invalid %s: %q", k, v)
		}
		return n, nil
	}
	from, err = get("from")
	if err != nil {
		return 0, 0, fmt.Errorf("console: parse from/to: %w", err)
	}
	to, err = get("to")
	if err != nil {
		return 0, 0, fmt.Errorf("console: parse from/to: %w", err)
	}
	if from > to {
		return 0, 0, fmt.Errorf("from (%d) must be <= to (%d)", from, to)
	}
	return from, to, nil
}

// parseFromToOrN adds §8.4's "?n=<step>" single-step shorthand on top of
// parseFromTo's from/to pair: n wins when from is absent, so a caller that
// wants exactly one step never has to spell out from=n&to=n.
func parseFromToOrN(q map[string][]string) (from, to int, err error) {
	if fromVals, ok := q["from"]; !ok || len(fromVals) == 0 {
		if nVals, ok := q["n"]; ok && len(nVals) > 0 && nVals[0] != "" {
			n, err := strconv.Atoi(nVals[0])
			if err != nil {
				return 0, 0, fmt.Errorf("console: parse n: invalid n: %q", nVals[0])
			}
			return n, n, nil
		}
	}
	return parseFromTo(q)
}

// handleSelfReview implements GET /api/runs/<runId>/self-review.md|.json
// (FM-52).
func (s *Server) handleSelfReview(w http.ResponseWriter, r *http.Request, runID string) {
	if !farm.ValidRunID(runID) {
		http.NotFound(w, r)
		return
	}
	bundle, err := observer.NewSinkBundle(r.Context(), s.cfg.Store, runID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	resultsDir, err := observer.ResultsDir(bundle)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	text, err := observer.LoadSelfReview(bundle, resultsDir)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if text == "" {
		text = observer.NotRecorded
	}
	if isJSONRequest(r) {
		writeJSON(w, struct {
			RunID      string `json:"runId"`
			SelfReview string `json:"selfReview"`
		}{runID, text})
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, text)
}

// handleFile implements GET /api/runs/<runId>/files/<path> (FM-52): one
// bundle file under results/, as text/plain; any other path is 404. Cached
// (server.go's fileCache) once the run's done.json exists — the bundle is
// then immutable (§7.6 FM-47).
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request, runID, filePath string) {
	if !farm.ValidRunID(runID) || !validBundleFilePath(filePath) {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	cacheKey := runID + "/" + filePath

	if data, ok := s.files.get(cacheKey); ok {
		writeTextFile(w, data)
		return
	}

	doneExists, _, err := s.cfg.Store.Head(ctx, doneKey(runID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	bundle, err := observer.NewSinkBundle(ctx, s.cfg.Store, runID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, err := bundle.ReadFile(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if doneExists {
		s.files.set(cacheKey, data)
	}
	writeTextFile(w, data)
}

func writeTextFile(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// validBundleFilePath enforces FM-52's "files/ only under results/"
// boundary: filePath must start with "results/", have a non-empty tail,
// and carry no ".." traversal segment (a raw or percent-decoded one alike
// — net/http already percent-decodes r.URL.Path before this ever runs).
func validBundleFilePath(filePath string) bool {
	if !strings.HasPrefix(filePath, "results/") {
		return false
	}
	if strings.Contains(filePath, "..") {
		return false
	}
	if path.Clean(filePath) != filePath {
		return false
	}
	return len(filePath) > len("results/")
}

// --- §8.4/§8.7's GET /api/findings.md|.json ---------------------------------

// findingItemFromRow narrows a FindingRow (findings_model.go's
// BuildFindingRows) to one GET /api/findings.md|.json row (§8.4).
func findingItemFromRow(f FindingRow) FindingItem {
	steps := make([]int, 0, len(f.Evidence))
	verified, total := 0, 0
	for _, e := range f.Evidence {
		total++
		if e.Verified {
			verified++
		}
		if e.Step > 0 {
			steps = append(steps, e.Step)
		}
	}
	sort.Ints(steps)
	return FindingItem{
		Owner: f.Owner, Batch: f.Batch, Scenario: f.Scenario, Build: f.Build.Label(), RunID: f.RunID, StartedAt: f.StartedAt,
		Severity: f.Severity, Cause: f.CauseLabel, CauseClass: f.CauseClass, Surface: f.Surface, Anchor: f.Anchor,
		Title: f.Title, What: f.What, Steps: steps, QuotesVerified: verified, QuotesTotal: total,
		LookAt: f.LookAt, Fix: f.Fix,
	}
}

func renderFindingsMD(items []FindingItem) string {
	var b strings.Builder
	b.WriteString(legendLine("Finding", "Severity", "Cause", "Quote found"))
	for _, it := range items {
		fmt.Fprintf(&b, "- [%s · %s] %s — %s (%s, started %s, steps %s) — quotes %d/%d — %s\n",
			it.Severity, it.Cause, it.Title, it.Batch, it.RunID, it.StartedAt.UTC().Format(time.RFC3339),
			formatStepRanges(it.Steps), it.QuotesVerified, it.QuotesTotal, it.What)
	}
	return b.String()
}

// handleFindings implements GET /api/findings.md|.json (§8.4/§8.7): the
// same query surface as /findings (findingListSpec, findings_model.go) —
// cause/severity/surface/scenario/batch/build/since, sort=severity default
// — through findingEngine, never hand-parsed.
func (s *Server) handleFindings(w http.ResponseWriter, r *http.Request) {
	q, err := Parse(findingListSpec(), r.URL.Query())
	if err != nil {
		writeQueryError(w, r, asQueryError(err))
		return
	}
	rows, err := s.allRunRows(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	findings := BuildFindingRows(rows)
	filtered, _ := findingEngine().Apply(findings, q, s.now())

	items := make([]FindingItem, len(filtered))
	for i, f := range filtered {
		items[i] = findingItemFromRow(f)
	}

	if isJSONRequest(r) {
		writeJSON(w, struct {
			Findings []FindingItem `json:"findings"`
		}{items})
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, renderFindingsMD(items))
}
