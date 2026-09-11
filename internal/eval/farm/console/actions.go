// Package console: this file, actions.go, owns the console's state-changing
// routes (docs/spec-eval-farm.md §8.5 FM-53) — POST /r/<runId>/observe and
// POST /b/<batch>/observe — plus Server.StartWorker, which runs the
// worker.go Worker (S5a) in the background for the lifetime of the server.
// worker.go itself is a separate slice's file and is never edited here; this
// file only calls its exported Queue/Worker API.
package console

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// sourceAction is Job.Source for a run an operator action queued, as
// opposed to the worker's own schedule (worker.go's wkSourceWorker, §8.5).
const sourceAction = "action"

// Notice codes for a cookie-authenticated action's redirect (§8.5 FM-53,
// §8.3: rendered by pages.go's noticeFromQuery). Kept here, next to the
// code that produces them, rather than exported from pages.go — the two
// sides agree only on these literal strings.
const (
	noticeQueued      = "queued"
	noticeBusy        = "busy"
	noticeNotFinished = "not-finished"
	noticeBadModel    = "bad-model"
	noticeUnavailable = "unavailable"
)

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

// expectedOrigin computes this request's own origin per §8.2 FM-50: host =
// X-Forwarded-Host when present else Host; scheme = X-Forwarded-Proto when
// present else "https" under TLS, "http" without. actionOriginOK and
// refererPathOrDefault both compare against it — "this request's own
// origin" is one function, not two.
func expectedOrigin(r *http.Request) string {
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
	return scheme + "://" + host
}

// actionOriginOK implements §8.2 FM-50's Origin rule for a cookie-
// authenticated state-changing POST: Origin must equal expectedOrigin(r).
func actionOriginOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	return origin == expectedOrigin(r)
}

// refererPathOrDefault implements §8.5 FM-53's cookie-redirect target: the
// request's own Referer path when Referer is same-origin with this
// request (expectedOrigin), else page. Only the path travels — the
// Referer's own query string is dropped; the caller appends its own
// ?notice=<code>.
func refererPathOrDefault(r *http.Request, page string) string {
	referer := r.Header.Get("Referer")
	if referer == "" {
		return page
	}
	u, err := url.Parse(referer)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Path == "" {
		return page
	}
	if u.Scheme+"://"+u.Host != expectedOrigin(r) {
		return page
	}
	return u.Path
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

// respondAction answers a plain accepted action with no §8.5 notice
// vocabulary of its own: 202 for a bearer-authenticated request, a 303
// redirect back to redirectPath for a cookie-authenticated one. Used by
// auth.go's handleLogout (outside this slice) — the observe actions use
// the notice-carrying respondActionAccepted/respondActionRefused below
// instead.
func (s *Server) respondAction(w http.ResponseWriter, r *http.Request, redirectPath string) {
	if _, isBearer := bearerToken(r); isBearer {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	http.Redirect(w, r, redirectPath, http.StatusSeeOther)
}

// actionSkip is one entry of a bearer action response's "skipped" list
// (§8.5 FM-53): a run the action did not queue, with why.
type actionSkip struct {
	RunID  string `json:"runId"`
	Reason string `json:"reason"`
}

// actionResponseBody is the bearer-authenticated 202 body (§8.5 FM-53):
// {queued: [runIds], skipped: [{runId, reason}]}.
type actionResponseBody struct {
	Queued  []string     `json:"queued"`
	Skipped []actionSkip `json:"skipped"`
}

// respondActionAccepted answers a successfully processed action — which
// may have queued zero runs (e.g. a batch observe where nothing needed
// assessment): a bearer request gets 202 with §8.5 FM-53's JSON body; a
// cookie request is redirected to refererPathOrDefault(r, page) with
// ?notice=queued&n=<len(queued)> (pages.go's noticeFromQuery renders it).
func (s *Server) respondActionAccepted(w http.ResponseWriter, r *http.Request, page string, queued []string, skipped []actionSkip) {
	if _, isBearer := bearerToken(r); isBearer {
		if queued == nil {
			queued = []string{}
		}
		if skipped == nil {
			skipped = []actionSkip{}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		if err := json.NewEncoder(w).Encode(actionResponseBody{Queued: queued, Skipped: skipped}); err != nil {
			s.logf("console: encode action response: %v", err)
		}
		return
	}
	s.redirectWithNotice(w, r, page, noticeQueued, len(queued))
}

// respondActionRefused answers an action refused before anything was
// queued: a bearer request keeps the status/one-line text body callers
// already expect (§8.5 FM-53: "409/400/503 as above, with a one-line text
// body"); a cookie request is redirected to refererPathOrDefault(r, page)
// with ?notice=<code> — never the raw status.
func (s *Server) respondActionRefused(w http.ResponseWriter, r *http.Request, page string, status int, text, notice string) {
	if _, isBearer := bearerToken(r); isBearer {
		http.Error(w, text, status)
		return
	}
	s.redirectWithNotice(w, r, page, notice, 0)
}

// redirectWithNotice sends a cookie-authenticated action's caller back to
// refererPathOrDefault(r, page) with ?notice=<code> (+ &n=<n> for
// "queued", §8.5 FM-53). notice is always one of this file's own
// constants (never user input), so a hand-built query string is safe and
// keeps the documented "notice, then n" order — url.Values.Encode would
// alphabetize "n" before "notice".
func (s *Server) redirectWithNotice(w http.ResponseWriter, r *http.Request, page, notice string, n int) {
	dest := refererPathOrDefault(r, page)
	query := "notice=" + notice
	if notice == noticeQueued {
		query += "&n=" + strconv.Itoa(n)
	}
	http.Redirect(w, r, dest+"?"+query, http.StatusSeeOther)
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
	page := "/r/" + runID

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	model := r.FormValue("model")
	if model != "" && !observer.ValidModel(model) {
		s.respondActionRefused(w, r, page, http.StatusBadRequest, "model must be one of "+strings.Join(observer.Models, ", "), noticeBadModel)
		return
	}
	if unavailable, message := s.observerUnavailable(); unavailable {
		s.respondActionRefused(w, r, page, http.StatusServiceUnavailable, message, noticeUnavailable)
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
		s.respondActionRefused(w, r, page, http.StatusConflict, "run not finished", noticeNotFinished)
		return
	}

	if model == "" {
		model = s.resolveActionModel(r.Context(), runID, manifest.Observer)
	}

	if err := s.cfg.Queue.Enqueue(r.Context(), Job{RunID: runID, Batch: batchID, Model: model, Source: sourceAction}); err != nil {
		if errors.Is(err, ErrAlreadyQueued) {
			s.respondActionRefused(w, r, page, http.StatusConflict, "already queued or running", noticeBusy)
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	s.respondActionAccepted(w, r, page, []string{runID}, nil)
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
	page := "/b/" + batch

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	model := r.FormValue("model")
	if !observer.ValidModel(model) {
		s.respondActionRefused(w, r, page, http.StatusBadRequest, "model must be one of "+strings.Join(observer.Models, ", "), noticeBadModel)
		return
	}
	if unavailable, message := s.observerUnavailable(); unavailable {
		s.respondActionRefused(w, r, page, http.StatusServiceUnavailable, message, noticeUnavailable)
		return
	}
	all := r.FormValue("all") == "1"

	manifest, err := loadManifest(r.Context(), s.cfg.Store, batch)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	if all && s.cfg.Queue.BatchBusy(batch) {
		s.respondActionRefused(w, r, page, http.StatusConflict, "already queued or running", noticeBusy)
		return
	}

	ctx := r.Context()
	var queued []string
	var skipped []actionSkip
	enqueue := func(ctx context.Context, runID string) {
		err := s.cfg.Queue.Enqueue(ctx, Job{RunID: runID, Batch: batch, Model: model, Source: sourceAction})
		switch {
		case err == nil:
			queued = append(queued, runID)
		case errors.Is(err, ErrAlreadyQueued):
			skipped = append(skipped, actionSkip{RunID: runID, Reason: "already queued or running"})
		default:
			skipped = append(skipped, actionSkip{RunID: runID, Reason: err.Error()})
		}
	}

	if all {
		for _, run := range manifest.Runs {
			doneExists, _, err := s.cfg.Store.Head(ctx, doneKey(run.RunID))
			if err != nil || !doneExists {
				skipped = append(skipped, actionSkip{RunID: run.RunID, Reason: "run not finished"})
				continue
			}
			enqueue(ctx, run.RunID)
		}
		s.respondActionAccepted(w, r, page, queued, skipped)
		return
	}

	rows, err := batchWindowRowsWithManifest(ctx, s.cfg.Store, s.cfg.ObserverDisabled, batch, manifest, s.queueState, s.runCache, s.summaryCache, s.logf)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	for _, row := range rows {
		if !NeedsAssessment(row, runQueued(s.queueState, row.RunID)) {
			continue
		}
		enqueue(ctx, row.RunID)
	}

	s.respondActionAccepted(w, r, page, queued, skipped)
}
