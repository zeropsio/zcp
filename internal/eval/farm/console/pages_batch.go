// Package console: this file, pages_batch.go, owns GET /b/<batch> (§8.3
// FM-51's batch page) — split out of pages.go (S-SHELL item 1) so each page
// owns one file.
package console

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

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
	Meta           pageMeta
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
	rows, err := batchWindowRows(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, batch, s.queueState, s.runCache, s.summaryCache, s.logf)
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
	busy := s.cfg.Queue != nil && s.cfg.Queue.BatchBusy(batch)
	data := batchPageData{Meta: s.pageMeta(r, batch, "", busy), BatchID: batch, ModelOptions: observer.Models}
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
