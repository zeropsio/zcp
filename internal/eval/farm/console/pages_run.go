// Package console: this file, pages_run.go, owns GET /r/<runId> (§8.3
// FM-51's run page) — split out of pages.go (S-SHELL item 1) so each page
// owns one file.
package console

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// runPageData is GET /r/<runId> (§8.3 FM-51).
type runPageData struct {
	Meta              pageMeta
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
	row, err := loadRunRow(ctx, s.cfg.Store, s.cfg.ObserverDisabled, runID, s.queueState, s.runCache, s.summaryCache)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	busy := runQueued(s.queueState, runID)
	data := runPageData{
		Meta: s.pageMeta(r, row.Scenario, "", busy),
		Row:  row, EvidenceSteps: evidenceSteps(row.Observation), ModelOptions: observer.Models,
	}
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
