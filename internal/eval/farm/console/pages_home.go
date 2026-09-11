// Package console: this file, pages_home.go, owns GET / (§8.3 FM-51's
// Overview page) — split out of pages.go (S-SHELL item 1) so each page owns
// one file.
package console

import "net/http"

// batchesPageData is GET / (§8.3 FM-51).
type batchesPageData struct {
	Meta    pageMeta
	Batches []BatchRow
}

func (s *Server) handleBatchesPage(w http.ResponseWriter, r *http.Request) {
	rows, err := loadBatchRows(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, s.queueState, s.runCache, s.summaryCache, s.logf)
	if err != nil {
		http.Error(w, "list batches: "+err.Error(), http.StatusInternalServerError)
		return
	}
	renderPage(w, "batches", batchesPageData{Meta: s.pageMeta(r, "Overview", navOverview, s.anyObservationInFlight()), Batches: rows})
}

// anyObservationInFlight reports whether any observation is queued or
// running anywhere on the console (§8.5) — the Overview page's own §8.5
// "a job of this page is in flight" scope, since it shows every batch.
func (s *Server) anyObservationInFlight() bool {
	if s.cfg.Queue == nil {
		return false
	}
	queued, running := s.cfg.Queue.Stats()
	return queued+running > 0
}
