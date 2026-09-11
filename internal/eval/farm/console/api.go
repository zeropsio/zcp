package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// observer.NotRecorded matches §7.2's rendering of a missing optional bundle file.

// glossaryDef looks term up in labels.go's §8.8 glossary (glossaryTerms) —
// "" for a term not defined there.
func glossaryDef(term string) string {
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

// writeQueryError answers a list endpoint's refused parameter (§8.7 FM-55):
// 400 {error, allowed} on the JSON twin, a one-line text on the markdown
// one — never the page's own rendered 400 (renderBadQuery, listnav.go).
func writeQueryError(w http.ResponseWriter, r *http.Request, qerr *QueryError) {
	if isJSONRequest(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(struct {
			Error   string   `json:"error"`
			Allowed []string `json:"allowed"`
		}{qerr.Error(), qerr.Allowed}); err != nil {
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

func renderBatchesMD(items []BatchListItem) string {
	var b strings.Builder
	b.WriteString(legendLine("Batch", "ZCP build", "Verdict", "Agent cost"))
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
	fmt.Fprint(w, renderBatchesMD(items))
}

// --- §8.4/§8.6's GET /api/problems.md|.json ---------------------------------

// ProblemMemberItem is one ProblemItem.Members row (§8.6): a thin view over
// FindingRow plus the run link and step link its most-cited evidence needs.
type ProblemMemberItem struct {
	RunID     string    `json:"runId"`
	Batch     string    `json:"batch"`
	Build     string    `json:"build"`
	StartedAt time.Time `json:"startedAt"`
	Severity  string    `json:"severity"`
	Cause     string    `json:"cause"`
	Title     string    `json:"title"`
	LookAt    string    `json:"lookAt"`
	Fix       string    `json:"fix"`
	RunLink   string    `json:"runLink"`
	Quote     string    `json:"quote,omitempty"`
	StepLink  string    `json:"stepLink,omitempty"`
}

func problemMemberItem(m ProblemMember) ProblemMemberItem {
	item := ProblemMemberItem{
		RunID: m.RunID, Batch: m.Batch, Build: m.Build.Label(), StartedAt: m.StartedAt,
		Severity: m.Severity, Cause: m.CauseLabel, Title: m.Title, LookAt: m.LookAt, Fix: m.Fix,
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
	b.WriteString(legendLine("Problem", "Severity", "Cause", "Surface", "Anchor"))
	for _, p := range items {
		fmt.Fprintf(&b, "- [%s · %s] %s — %s — hit %d/%d runs on newest build — %d runs · %d batches · %d builds — status %s\n",
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

// RunsListItem is one GET /api/runs.md|.json row (§8.4: "run id, scenario,
// verdict (+ reason), outcome, findings high/medium per cause class, failed
// check ids, headline").
type RunsListItem struct {
	RunID          string               `json:"runId"`
	Batch          string               `json:"batch"`
	Scenario       string               `json:"scenario"`
	Verdict        string               `json:"verdict"`
	VerdictReason  string               `json:"verdictReason,omitempty"`
	StartedAt      time.Time            `json:"startedAt"`
	DurationSec    float64              `json:"durationSec"`
	CostUsd        float64              `json:"costUsd"`
	Outcome        string               `json:"outcome"`
	CauseCounts    []CauseCountItem     `json:"causeCounts"`
	FailedCheckIDs []string             `json:"failedCheckIds,omitempty"`
	ObserverState  string               `json:"observerState"`
	Observation    *RunsListObservation `json:"observation,omitempty"`
}

// RunDetail is GET /api/runs/<runId>.md|.json (FM-52).
type RunDetail struct {
	RunID           string                `json:"runId"`
	Batch           string                `json:"batch"`
	Scenario        string                `json:"scenario"`
	Verdict         string                `json:"verdict"`
	StartedAt       time.Time             `json:"startedAt"`
	DurationSec     float64               `json:"durationSec"`
	CostUsd         float64               `json:"costUsd"`
	CandidateSha256 string                `json:"candidateSha256"`
	EvaluatorSha256 string                `json:"evaluatorSha256"`
	ObserverState   string                `json:"observerState"`
	Observation     *observer.Observation `json:"observation,omitempty"`
	OlderObsIDs     []string              `json:"olderObsIds"`
	FailedChecks    []FailedCheck         `json:"failedChecks"`
	EvidenceSteps   []int                 `json:"evidenceSteps"`
}

// StepJSON is one element of GET /api/runs/<runId>/steps.md|.json (FM-52).
type StepJSON struct {
	N       int    `json:"n"`
	Kind    string `json:"kind"`
	Tool    string `json:"tool,omitempty"`
	Input   string `json:"input,omitempty"`
	Result  string `json:"result,omitempty"`
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
		StartedAt:     row.StartedAt, DurationSec: row.DurationSec, CostUsd: row.CostUsd,
		Outcome: row.Outcome, CauseCounts: causeCountItems(row.CauseCounts),
		ObserverState: row.ObserverState,
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
	if it.Observation != nil {
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
			findings = append(findings, fmt.Sprintf("%s:%d/%d", c.Class, c.High, c.Medium))
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
	b.WriteString(legendLine("Run", "Verdict", "Disputed", "Agent cost"))
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
		StartedAt: row.StartedAt, DurationSec: row.DurationSec, CostUsd: row.CostUsd,
		CandidateSha256: row.CandidateSha256, EvaluatorSha256: row.EvaluatorSha256,
		ObserverState: row.ObserverState, Observation: row.Observation,
		OlderObsIDs: older, FailedChecks: failed, EvidenceSteps: steps,
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
	fmt.Fprintf(&b, "# %s\n\nscenario: %s\nbatch: %s\nverdict: %s\nstarted: %s\nduration: %.1fs\ncost: $%.4f\ncandidate: %s\nevaluator: %s\nobserverState: %s\n\n",
		d.RunID, d.Scenario, d.Batch, d.Verdict, d.StartedAt.UTC().Format(time.RFC3339),
		d.DurationSec, d.CostUsd, d.CandidateSha256, d.EvaluatorSha256, d.ObserverState)

	if d.Observation != nil {
		b.WriteString(observer.Render(*d.Observation))
	} else {
		b.WriteString("Observer: " + observer.NotRecorded + "\n")
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
	default:
		http.NotFound(w, r)
	}
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

func stepJSON(s observer.Step) StepJSON {
	out := StepJSON{N: s.N, Kind: string(s.Kind)}
	if s.Kind == observer.StepTool {
		out.Tool, out.Input, out.Result, out.IsError = s.ToolName, s.ToolInputJSON, s.ToolResultText, s.ToolIsError
		return out
	}
	out.Text = s.Text
	return out
}

func renderStepsMD(steps []observer.Step) string {
	var b strings.Builder
	for _, s := range steps {
		switch s.Kind {
		case observer.StepTool:
			fmt.Fprintf(&b, "#%d tool %s %s\n", s.N, s.ToolName, s.ToolInputJSON)
			if s.ToolIsError {
				fmt.Fprintf(&b, "  → ERROR %s\n", s.ToolResultText)
			} else {
				fmt.Fprintf(&b, "  → %s\n", s.ToolResultText)
			}
		case observer.StepUser, observer.StepThinking, observer.StepAgent:
			fmt.Fprintf(&b, "#%d %s %s\n", s.N, s.Kind, s.Text)
		}
	}
	return b.String()
}

// handleSteps implements GET /api/runs/<runId>/steps.md|.json?from=<n>&to=<m>
// (FM-52): those steps, untruncated.
func (s *Server) handleSteps(w http.ResponseWriter, r *http.Request, runID string) {
	if !farm.ValidRunID(runID) {
		http.NotFound(w, r)
		return
	}
	from, to, err := parseFromTo(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	all, err := loadSteps(r.Context(), s.cfg.Store, runID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var sel []observer.Step
	for _, st := range all {
		if st.N >= from && st.N <= to {
			sel = append(sel, st)
		}
	}
	if isJSONRequest(r) {
		out := make([]StepJSON, len(sel))
		for i, st := range sel {
			out[i] = stepJSON(st)
		}
		writeJSON(w, out)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, renderStepsMD(sel))
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
		Severity: f.Severity, Cause: f.CauseLabel, Surface: f.Surface, Anchor: f.Anchor,
		Title: f.Title, What: f.What, Steps: steps, QuotesVerified: verified, QuotesTotal: total,
		LookAt: f.LookAt, Fix: f.Fix,
	}
}

// findingItemsFromRows is the pre-query-engine findings resolution, kept
// for pages_findings.go (a parallel slice's write-set, still calling it
// directly) — additive alongside findingItemFromRow/handleFindings' own
// query-engine path (§8.7), never removed out from under that file.
func findingItemsFromRows(rows []RunRow) []FindingItem {
	var items []FindingItem
	for _, row := range rows {
		if row.Observation == nil {
			continue
		}
		for _, f := range row.Observation.Findings {
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
			items = append(items, FindingItem{
				Owner: f.Owner, Batch: row.Batch, Scenario: row.Scenario, Build: row.Build.Label(),
				RunID: row.RunID, StartedAt: row.StartedAt, Severity: f.Severity, Cause: CauseLabel(f.Owner),
				Surface: f.Surface, Anchor: f.Anchor, Title: f.Title, What: f.What, Steps: steps,
				QuotesVerified: verified, QuotesTotal: total, LookAt: f.LookAt, Fix: f.Fix,
			})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Owner != items[j].Owner {
			return items[i].Owner < items[j].Owner
		}
		return severityRank(items[i].Severity) < severityRank(items[j].Severity)
	})
	return items
}

func severityRank(sev string) int {
	switch sev {
	case observer.SeverityHigh:
		return 0
	case observer.SeverityMedium:
		return 1
	case observer.SeverityLow:
		return 2
	default:
		return 3
	}
}

func renderFindingsMD(items []FindingItem) string {
	var b strings.Builder
	b.WriteString(legendLine("Finding", "Severity", "Cause", "Surface", "Anchor", "Quote found"))
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
