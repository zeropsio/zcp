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

//go:embed assets/batches.html assets/batch.html assets/run.html assets/findings.html
var pagesHTMLSrc embed.FS

// pageFuncs are the plain-string-returning helpers pages.html templates use.
// None of them return template.HTML — every value they produce still goes
// through {{...}}'s normal auto-escaping, so a hostile value from the
// bucket (a step result, a model-authored quote) is never trusted as markup
// (TestPages_HostileTextEscaped).
var pageFuncs = template.FuncMap{
	"fmtTime":     func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") },
	"fmtCost":     func(v float64) string { return fmt.Sprintf("$%.4f", v) },
	"fmtDuration": func(sec float64) string { return fmt.Sprintf("%.1fs", sec) },
	"stepAnchor":  func(n int) string { return fmt.Sprintf("s%d", n) },
	"verifiedMark": func(v bool) string {
		if v {
			return "verified"
		}
		return "unverified"
	},
	"verdictClass": func(v string) string {
		switch v {
		case "passed", "failed", "blocked", verdictRunning:
			return "verdict verdict-" + v
		case "not-run":
			return "verdict verdict-not-run"
		default:
			return "verdict verdict-other"
		}
	},
	"severityClass": func(v string) string {
		switch v {
		case "high", "medium", "low":
			return "severity severity-" + v
		default:
			return "severity severity-other"
		}
	},
	"runLink": func(runID string, steps []int) string {
		if len(steps) == 0 {
			return "/r/" + runID
		}
		if steps[0] == 0 {
			return "/r/" + runID + "#failed-checks"
		}
		return fmt.Sprintf("/r/%s#s%d", runID, steps[0])
	},
}

var pagesTemplate = template.Must(template.New("pages").Funcs(pageFuncs).ParseFS(pagesHTMLSrc, "assets/*.html"))

// observeModelOptions is §8.5 FM-53's re-observe model allowlist — S4
// renders the picker, S5b's POST handlers enforce it server-side.
var observeModelOptions = []string{"claude-sonnet-5", "claude-opus-5", "claude-fable-5-1"}

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
	HeadlineOrState string
	FailedCheckIDs  []string
}

func newBatchRunView(row RunRow) batchRunView {
	headline := row.ObserverState
	if row.Observation != nil {
		headline = row.Observation.Headline
	}
	ids := make([]string, len(row.FailedChecks))
	for i, c := range row.FailedChecks {
		ids[i] = c.ID
	}
	return batchRunView{RunRow: row, HeadlineOrState: headline, FailedCheckIDs: ids}
}

// batchPageData is GET /b/<batch> (§8.3 FM-51).
type batchPageData struct {
	BatchID      string
	Runs         []batchRunView
	ModelOptions []string
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
	sort.Slice(rows, func(i, j int) bool { return rows[i].RunID < rows[j].RunID })
	views := make([]batchRunView, len(rows))
	for i, row := range rows {
		views[i] = newBatchRunView(row)
	}
	renderPage(w, "batch", batchPageData{BatchID: batch, Runs: views, ModelOptions: observeModelOptions})
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

	data := runPageData{Row: row, EvidenceSteps: evidenceSteps(row.Observation), ModelOptions: observeModelOptions}
	if row.Observation != nil {
		data.UnverifiedQuotes = unverifiedQuotes(row.Observation)
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
		return "", "", err
	}
	resultsDir, err := observer.ResultsDir(bundle)
	if err != nil {
		return "", "", err
	}
	taskPrompt, err = observer.LoadTaskPrompt(bundle, resultsDir)
	if err != nil {
		return "", "", err
	}
	selfReview, err = observer.LoadSelfReview(bundle, resultsDir)
	if err != nil {
		return "", "", err
	}
	if selfReview == "" {
		selfReview = notRecorded
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

// findingsPageData is GET /findings (§8.3 FM-51).
type findingsPageData struct {
	Since    string
	Owner    string
	Findings []FindingItem
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

	renderPage(w, "findings", findingsPageData{Since: q.Get("since"), Owner: owner, Findings: items})
}
