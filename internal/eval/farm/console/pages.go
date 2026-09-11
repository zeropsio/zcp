// Package console: HTML pages (docs/spec-eval-farm.md §8.3 FM-51). Every
// page is rendered through html/template (auto-escaping) from the read
// models in view.go, api.go and batches.go — this file adds no store
// writes and no new bucket keys.
package console

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

//go:embed assets/layout.html assets/batches.html assets/batch.html assets/run.html assets/findings.html
var pagesHTMLSrc embed.FS

// pageFuncs are the plain-string-returning helpers pages.html templates use.
// None of them return template.HTML — every value they produce still goes
// through {{...}}'s normal auto-escaping, so a hostile value from the
// bucket (a step result, a model-authored quote) is never trusted as markup
// (TestPages_HostileTextEscaped).
var pageFuncs = template.FuncMap{
	"fmtTime":     fmtTime,
	"fmtCost":     func(v float64) string { return fmt.Sprintf("$%.2f", v) },
	"fmtDuration": fmtDuration,
	"verdictIcon": verdictIcon,
	"verdictKey":  verdictKey,
	"shortTool": func(name string) string {
		return strings.TrimPrefix(strings.TrimPrefix(name, "mcp__zerops__"), "mcp__")
	},
	"preview":    preview,
	"capRaw":     capRaw,
	"joinInts":   joinInts,
	"stepAnchor": func(n int) string { return fmt.Sprintf("s%d", n) },
	"verifiedMark": func(v bool) string {
		if v {
			return "verified"
		}
		return "unverified"
	},
	"verdictClass": func(v string) string {
		switch v {
		case farm.VerdictPassed, farm.VerdictFailed, farm.VerdictBlocked, verdictRunning:
			return "verdict verdict-" + v
		case farm.VerdictNotRun:
			return "verdict verdict-not-run"
		default:
			return "verdict verdict-other"
		}
	},
	"severityClass": func(v string) string {
		switch v {
		case observer.SeverityHigh, observer.SeverityMedium, observer.SeverityLow:
			return "severity severity-" + v
		default:
			return "severity severity-other"
		}
	},
	"runLink": func(runID string, steps []int) string {
		if len(steps) == 0 {
			return "/r/" + runID
		}
		// steps is always FindingItem.Steps (api.go), which already
		// excludes the step-0 CHECKS citation — steps[0] is never 0 here.
		return fmt.Sprintf("/r/%s#s%d", runID, steps[0])
	},
}

// fmtTime renders a timestamp for people: day, month, time, UTC.
func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format("2 Jan 2006, 15:04 UTC")
}

// fmtDuration renders seconds as "45s", "8m 04s" or "1h 02m".
func fmtDuration(sec float64) string {
	d := time.Duration(sec * float64(time.Second)).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// verdictIcon is the text mark shown next to every verdict label, so a
// verdict never depends on colour alone.
func verdictIcon(v string) string {
	switch v {
	case farm.VerdictPassed:
		return "✓"
	case farm.VerdictFailed:
		return "✗"
	case farm.VerdictBlocked:
		return "!"
	case verdictRunning:
		return "…"
	default:
		return "–"
	}
}

// verdictRank orders a batch page's runs problems first: failed, blocked,
// running, not-run, passed, anything else.
func verdictRank(v string) int {
	switch v {
	case farm.VerdictFailed:
		return 0
	case farm.VerdictBlocked:
		return 1
	case verdictRunning:
		return 2
	case farm.VerdictNotRun:
		return 3
	case farm.VerdictPassed:
		return 4
	default:
		return 5
	}
}

// verdictKey maps a verdict onto the stylesheet's known verdict classes.
func verdictKey(v string) string {
	switch v {
	case farm.VerdictPassed, farm.VerdictFailed, farm.VerdictBlocked, verdictRunning, farm.VerdictNotRun:
		return v
	default:
		return "other"
	}
}

// preview is the one-line summary of a step: whitespace collapsed, cut at
// n runes with an ellipsis.
func preview(s string, n int) string {
	flat := strings.Join(strings.Fields(s), " ")
	r := []rune(flat)
	if len(r) <= n {
		return flat
	}
	return string(r[:n]) + "…"
}

// observerRawCap is how much of a failed observation's raw answer the run
// page shows (item 3, matching §7.5's own "the first 2,000 chars of raw").
const observerRawCap = 2000

// capRaw caps s at observerRawCap runes, for the run page's collapsed raw-
// answer preview on an "unparsed" observation.
func capRaw(s string) string {
	r := []rune(s)
	if len(r) <= observerRawCap {
		return s
	}
	return string(r[:observerRawCap])
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, ", ")
}

// findingOwners is §7.5's owner vocabulary, in the order the findings page
// offers it as a filter.
var findingOwners = []string{"zcp-guidance", "zcp-tool", "platform", "agent", "scenario", "evaluator"}

// findingWindows are the findings page's window shortcuts (§8.3 FM-51).
var findingWindows = []string{"24h", "7d", "30d"}

var pagesTemplate = template.Must(template.New("pages").Funcs(pageFuncs).ParseFS(pagesHTMLSrc, "assets/*.html"))

// observer.Models is §8.5 FM-53's re-observe model allowlist — S4
// renders the picker, S5b's POST handlers enforce it server-side.

func renderPage(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pagesTemplate.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template: "+err.Error(), http.StatusInternalServerError)
	}
}

// batchesPageData is GET / (§8.3 FM-51).
type batchesPageData struct {
	Batches []BatchRow
}

func (s *Server) handleBatchesPage(w http.ResponseWriter, r *http.Request) {
	rows, err := loadBatchRows(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, s.queueState)
	if err != nil {
		http.Error(w, "list batches: "+err.Error(), http.StatusInternalServerError)
		return
	}
	renderPage(w, "batches", batchesPageData{rows})
}

// batchRunView narrows a RunRow to what §8.3 FM-51's /b/<batch> row needs:
// the current observation's headline when there is one, else
// RunRow.ObserverState verbatim (the read model owns that vocabulary — S5b
// makes it emit "observing"), plus just the failed/blocked check ids.
type batchRunView struct {
	RunRow
	Headline        string // the current observation's headline, "" without one
	State           string // RunRow.ObserverState, shown when there is no headline
	High            int
	Medium          int
	Low             int
	FailedCheckIDs  []string // at most maxFailedCheckChips
	FailedCheckMore int      // how many more failed checks the row does not list
}

// maxFailedCheckChips caps the failed-check ids a batch row lists.
const maxFailedCheckChips = 3

// observationFailed reports whether obs's status is one the console shows
// as a failure notice rather than a real assessment (item 3: "error" or
// "unparsed", §7.5).
func observationFailed(obs *observer.Observation) bool {
	return obs != nil && (obs.Status == "error" || obs.Status == "unparsed")
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

// batchPageData is GET /b/<batch> (§8.3 FM-51).
type batchPageData struct {
	BatchID        string
	CreatedAt      time.Time
	Set            string
	CandidateSha12 string
	VerdictCounts  []VerdictCount
	TotalCostUsd   float64
	ObservedN      int
	Unassessed     int // finished runs with no observation and none in flight
	Runs           []batchRunView
	ModelOptions   []string
}

func (s *Server) handleBatchPage(w http.ResponseWriter, r *http.Request) {
	batch := strings.TrimPrefix(r.URL.Path, "/b/")
	if !farm.ValidBatchID(batch) {
		http.NotFound(w, r)
		return
	}
	rows, err := batchWindowRows(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, batch, s.queueState)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	sort.Slice(rows, func(i, j int) bool {
		if ri, rj := verdictRank(rows[i].Verdict), verdictRank(rows[j].Verdict); ri != rj {
			return ri < rj
		}
		return rows[i].RunID < rows[j].RunID
	})
	data := batchPageData{BatchID: batch, ModelOptions: observer.Models}
	if manifest, err := loadManifest(r.Context(), s.cfg.Store, batch); err == nil {
		data.CreatedAt, _ = time.Parse(time.RFC3339, manifest.CreatedAt)
		data.Set = manifest.Set
		data.CandidateSha12 = candidateSha12(manifest.CandidateSha256)
	}
	counts := make(map[string]int)
	for _, row := range rows {
		data.Runs = append(data.Runs, newBatchRunView(row))
		counts[row.Verdict]++
		data.TotalCostUsd += row.CostUsd
		switch {
		case row.Observation != nil && !observationFailed(row.Observation):
			data.ObservedN++
		case row.DoneExists && row.ObserverState != observerStateObserving:
			data.Unassessed++
		}
	}
	data.VerdictCounts = orderedVerdictCounts(counts)
	renderPage(w, "batch", data)
}

// runPageData is GET /r/<runId> (§8.3 FM-51).
type runPageData struct {
	Row               RunRow
	Steps             []observer.Step
	TaskPrompt        string
	SelfReview        string
	OlderObservations []observer.Observation
	EvidenceSteps     []int
	UnverifiedQuotes  int
	TotalQuotes       int
	ModelOptions      []string
}

func (s *Server) handleRunPage(w http.ResponseWriter, r *http.Request) {
	runID := strings.TrimPrefix(r.URL.Path, "/r/")
	if !farm.ValidRunID(runID) {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	row, err := loadRunRow(ctx, s.cfg.Store, s.cfg.ObserverDisabled, runID, s.queueState)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	data := runPageData{Row: row, EvidenceSteps: evidenceSteps(row.Observation), ModelOptions: observer.Models}
	if row.Observation != nil {
		data.UnverifiedQuotes = unverifiedQuotes(row.Observation)
		for _, f := range row.Observation.Findings {
			data.TotalQuotes += len(f.Evidence)
		}
	}

	if row.DoneExists {
		steps, err := loadSteps(ctx, s.cfg.Store, runID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		data.Steps = steps

		taskPrompt, selfReview, err := loadRunTexts(ctx, s.cfg.Store, runID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		data.TaskPrompt = taskPrompt
		data.SelfReview = selfReview
	}

	older, err := loadOlderObservations(ctx, s.cfg.Store, runID, row.OlderObsIDs)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	data.OlderObservations = older

	renderPage(w, "run", data)
}

// loadRunTexts reads runId's task prompt and self-review straight from its
// bundle — the same two bundle files api.go's handleSelfReview/loadSteps
// read, via the same observer.Bundle helpers (§7.2).
func loadRunTexts(ctx context.Context, store observer.ObjectStore, runID string) (taskPrompt, selfReview string, err error) {
	bundle, err := observer.NewSinkBundle(ctx, store, runID)
	if err != nil {
		return "", "", fmt.Errorf("console: load run texts: new bundle: %w", err)
	}
	resultsDir, err := observer.ResultsDir(bundle)
	if err != nil {
		return "", "", fmt.Errorf("console: load run texts: results dir: %w", err)
	}
	taskPrompt, err = observer.LoadTaskPrompt(bundle, resultsDir)
	if err != nil {
		return "", "", fmt.Errorf("console: load run texts: task prompt: %w", err)
	}
	selfReview, err = observer.LoadSelfReview(bundle, resultsDir)
	if err != nil {
		return "", "", fmt.Errorf("console: load run texts: self-review: %w", err)
	}
	if selfReview == "" {
		selfReview = observer.NotRecorded
	}
	return taskPrompt, selfReview, nil
}

// loadOlderObservations resolves every older obsId (view.go's
// RunRow.OlderObsIDs, newest-last) to its full document, for the run page's
// "older observation versions" section (§8.3 FM-51).
func loadOlderObservations(ctx context.Context, store observer.ObjectStore, runID string, ids []string) ([]observer.Observation, error) {
	obsStore := observer.NewStore(store)
	out := make([]observer.Observation, 0, len(ids))
	for _, id := range ids {
		obs, err := obsStore.GetObservation(ctx, runID, id)
		if err != nil {
			return nil, fmt.Errorf("console: get older observation %s: %w", id, err)
		}
		out = append(out, obs)
	}
	return out, nil
}

// findingsPageData is GET /findings (§8.3 FM-51). The findings themselves
// reach the template only as Groups (owner-then-severity, §8.4) — there is
// no ungrouped Findings field, since findings.html never reads one.
type findingsPageData struct {
	Since   string
	Owner   string
	Windows []string
	Owners  []string
	Groups  []findingGroup
}

// findingGroup is one owner's findings, in the read model's order.
type findingGroup struct {
	Owner string
	Items []FindingItem
}

func groupFindingsByOwner(items []FindingItem) []findingGroup {
	var groups []findingGroup
	for _, it := range items {
		if len(groups) == 0 || groups[len(groups)-1].Owner != it.Owner {
			groups = append(groups, findingGroup{Owner: it.Owner})
		}
		groups[len(groups)-1].Items = append(groups[len(groups)-1].Items, it)
	}
	return groups
}

func (s *Server) handleFindingsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	window, err := ParseWindow(q.Get("since"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rows, err := rowsSinceWindow(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, window, s.now(), s.queueState)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	items := findingItemsFromRows(rows)

	owner := q.Get("owner")
	if owner != "" {
		filtered := make([]FindingItem, 0, len(items))
		for _, it := range items {
			if it.Owner == owner {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	since := q.Get("since")
	if since == "" {
		since = "24h"
	}
	renderPage(w, "findings", findingsPageData{
		Since: since, Owner: owner, Windows: findingWindows, Owners: findingOwners,
		Groups: groupFindingsByOwner(items),
	})
}
