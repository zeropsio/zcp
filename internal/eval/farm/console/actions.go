// Package console: this file, actions.go, owns the console's state-changing
// routes (docs/spec-eval-farm.md §8.5 FM-53) — POST /r/<runId>/observe and
// POST /b/<batch>/observe — plus Server.StartWorker, which runs the
// worker.go Worker (S5a) in the background for the lifetime of the server.
// worker.go itself is a separate slice's file and is never edited here; this
// file only calls its exported Queue/Worker API.
package console

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// sourceAction is Job.Source for a run an operator action queued, as
// opposed to the worker's own schedule (worker.go's wkSourceWorker, §8.5).
const sourceAction = "action"

// defaultWorkerTickInterval is the production worker tick period (§8.5
// FM-53: "every 60 s"). Config.WorkerInterval overrides it in tests.
const defaultWorkerTickInterval = 60 * time.Second

// StartWorker starts cfg.Worker ticking in the background until ctx is
// done (§8.5) — the caller (cmd/zcp/eval_farm_console.go) derives ctx from
// the same signal context that shuts down the HTTP server, so the worker
// stops on shutdown too. A no-op when cfg.Worker is nil: the console still
// serves every page and answers every action with no background loop
// running (e.g. a test server that doesn't exercise the worker).
func (s *Server) StartWorker(ctx context.Context) {
	if s.cfg.Worker == nil {
		return
	}
	interval := s.cfg.WorkerInterval
	if interval <= 0 {
		interval = defaultWorkerTickInterval
	}
	go s.cfg.Worker.Run(ctx, interval)
}

// observerUnavailable reports whether the run/batch observe actions must
// answer 503 instead of enqueueing (§8.5): a missing OAuth credential and
// an unresolvable --claude path each get their own message, both spelled
// out in FM-53; a nil Queue (defensive — never happens in production
// wiring) reads as the same "observer unavailable" case as an unresolvable
// claude path.
func (s *Server) observerUnavailable() (unavailable bool, message string) {
	switch {
	case s.cfg.ObserverCredentialMissing, s.cfg.ObserverAPIKeySet:
		return true, "observer credential missing"
	case s.cfg.ObserverClaudePathUnresolved:
		return true, "observer unavailable"
	case s.cfg.Queue == nil:
		return true, "observer unavailable"
	default:
		return false, ""
	}
}

// actionOriginOK implements §8.2 FM-50's Origin rule for a cookie-
// authenticated state-changing POST: Origin must equal
// "<scheme>://<host>", host = X-Forwarded-Host when present else Host,
// scheme = X-Forwarded-Proto when present else "https" under TLS, "http"
// without.
func actionOriginOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	return origin == scheme+"://"+host
}

// checkActionOrigin enforces the Origin rule for a cookie-authenticated
// request only — a bearer-authenticated POST needs no Origin (§8.2 FM-50).
// Writes 403 and returns false when the check fails.
func (s *Server) checkActionOrigin(w http.ResponseWriter, r *http.Request) bool {
	if _, isBearer := bearerToken(r); isBearer {
		return true
	}
	if !actionOriginOK(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

// respondAction answers an accepted action: 202 for a bearer-authenticated
// request, a 303 redirect back to redirectPath for a cookie-authenticated
// one (§8.5 FM-53).
func (s *Server) respondAction(w http.ResponseWriter, r *http.Request, redirectPath string) {
	if _, isBearer := bearerToken(r); isBearer {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	http.Redirect(w, r, redirectPath, http.StatusSeeOther)
}

// trimObservePath strips prefix and the trailing "/observe" from p, e.g.
// trimObservePath("/r/b1-a/observe", "/r/") = "b1-a".
func trimObservePath(p, prefix string) string {
	return strings.TrimSuffix(strings.TrimPrefix(p, prefix), "/observe")
}

// handleRunObserve implements POST /r/<runId>/observe (§8.5 FM-53): starts
// a new observation version for runId. Checks run cheapest-first: Origin,
// then an explicitly given model against the allowlist, then observer
// availability, and only then resolves runId's batch (the first store
// call) — an invalid runId is rejected by findRunBatch's own FM-47 grammar
// check before any store call. A run without done.json is never enqueued:
// it answers 409 "run not finished" (item 2) — there is nothing yet to
// observe. An absent model is resolved only once done.json is confirmed,
// via resolveActionModel's default chain — it always yields an allowlisted
// model, so no allowlist check follows it.
func (s *Server) handleRunObserve(w http.ResponseWriter, r *http.Request) {
	if !s.checkActionOrigin(w, r) {
		return
	}
	runID := trimObservePath(r.URL.Path, "/r/")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	model := r.FormValue("model")
	if model != "" && !observer.ValidModel(model) {
		http.Error(w, "model must be one of "+strings.Join(observer.Models, ", "), http.StatusBadRequest)
		return
	}
	if unavailable, message := s.observerUnavailable(); unavailable {
		http.Error(w, message, http.StatusServiceUnavailable)
		return
	}

	batchID, _, manifest, err := findRunBatch(r.Context(), s.cfg.Store, runID)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	doneExists, _, err := s.cfg.Store.Head(r.Context(), doneKey(runID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if !doneExists {
		http.Error(w, "run not finished", http.StatusConflict)
		return
	}

	if model == "" {
		model = s.resolveActionModel(r.Context(), runID, manifest.Observer)
	}

	if err := s.cfg.Queue.Enqueue(r.Context(), Job{RunID: runID, Batch: batchID, Model: model, Source: sourceAction}); err != nil {
		if errors.Is(err, ErrAlreadyQueued) {
			http.Error(w, "already queued or running", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	s.respondAction(w, r, "/r/"+runID)
}

// resolveActionModel implements §8.5 FM-53's model default chain for
// POST /r/<runId>/observe when the form carries no model: runID's current
// observation's model, else manifestObserver (the batch manifest's own
// model, "" or ObserverOff when the batch ran without one), else
// observer.DefaultModel. Always returns an allowlisted model.
func (s *Server) resolveActionModel(ctx context.Context, runID, manifestObserver string) string {
	obsStore := observer.NewStore(s.cfg.Store)
	if obsIDs, err := obsStore.ListObservations(ctx, runID); err == nil && len(obsIDs) > 0 {
		if cur, err := obsStore.GetObservation(ctx, runID, obsIDs[len(obsIDs)-1]); err == nil && observer.ValidModel(cur.Model) {
			return cur.Model
		}
	}
	if observer.ValidModel(manifestObserver) {
		return manifestObserver
	}
	return observer.DefaultModel
}

// handleBatchObserve implements POST /b/<batch>/observe (§8.5 FM-53):
// without all=1, queues exactly the runs that need an assessment
// (NeedsAssessment, view.go — done.json exists, not already queued or
// running, and either no observation or the current one failed) — the
// same predicate the batch page's Assess callout counts, so the two never
// disagree; with all=1, queues every run, but answers 409 first when any
// run of the batch is already queued or running. A run without done.json
// is never enqueued either way (item 2) — there is nothing yet to
// observe. A run whose row can't be built (a corrupt observation, item 5)
// is skipped rather than risking a duplicate enqueue on doubt
// (batchWindowRowsWithManifest/fillRowsConcurrently already implement
// this for the non-all path).
func (s *Server) handleBatchObserve(w http.ResponseWriter, r *http.Request) {
	if !s.checkActionOrigin(w, r) {
		return
	}
	batch := trimObservePath(r.URL.Path, "/b/")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	model := r.FormValue("model")
	if !observer.ValidModel(model) {
		http.Error(w, "model must be one of "+strings.Join(observer.Models, ", "), http.StatusBadRequest)
		return
	}
	if unavailable, message := s.observerUnavailable(); unavailable {
		http.Error(w, message, http.StatusServiceUnavailable)
		return
	}
	all := r.FormValue("all") == "1"

	manifest, err := loadManifest(r.Context(), s.cfg.Store, batch)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	if all && s.cfg.Queue.BatchBusy(batch) {
		http.Error(w, "already queued or running", http.StatusConflict)
		return
	}

	if all {
		for _, run := range manifest.Runs {
			doneExists, _, err := s.cfg.Store.Head(r.Context(), doneKey(run.RunID))
			if err != nil || !doneExists {
				continue
			}
			_ = s.cfg.Queue.Enqueue(r.Context(), Job{RunID: run.RunID, Batch: batch, Model: model, Source: sourceAction})
		}
		s.respondAction(w, r, "/b/"+batch)
		return
	}

	rows, err := batchWindowRowsWithManifest(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, batch, manifest, s.queueState, s.runCache, s.summaryCache, s.logf)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	for _, row := range rows {
		if !NeedsAssessment(row, runQueued(s.queueState, row.RunID)) {
			continue
		}
		_ = s.cfg.Queue.Enqueue(r.Context(), Job{RunID: row.RunID, Batch: batch, Model: model, Source: sourceAction})
	}

	s.respondAction(w, r, "/b/"+batch)
}
