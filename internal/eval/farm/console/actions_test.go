package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// --- test helpers -----------------------------------------------------

// actionServerOpts configures newActionServer beyond testServer's fixed
// shape (console_test.go) — action tests need a Queue (and sometimes a
// Worker) wired into Config, which testServer never sets.
type actionServerOpts struct {
	queue                        *Queue
	worker                       *Worker
	workerInterval               time.Duration
	observerDisabled             bool
	observerCredentialMissing    bool
	observerClaudePathUnresolved bool
}

// newActionServer builds a Server + fakeStore with the same fixed clock and
// fast failure delay as testServer (console_test.go), plus opts' action
// wiring.
func newActionServer(t *testing.T, opts actionServerOpts) (*Server, *fakeStore) {
	t.Helper()
	store := newFakeStore()
	cfg := Config{
		Store:                        store,
		Token:                        testToken,
		Now:                          fixedNow(t),
		LoginFailDelay:               time.Millisecond,
		Sleep:                        func(time.Duration) {},
		Queue:                        opts.queue,
		Worker:                       opts.worker,
		WorkerInterval:               opts.workerInterval,
		ObserverDisabled:             opts.observerDisabled,
		ObserverCredentialMissing:    opts.observerCredentialMissing,
		ObserverClaudePathUnresolved: opts.observerClaudePathUnresolved,
	}
	return NewServer(cfg), store
}

// doBearerPOST issues a POST authenticated with the bearer header, form-
// encoding vals as the body.
func doBearerPOST(t *testing.T, h http.Handler, path string, vals url.Values) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+testToken)
	h.ServeHTTP(rr, req)
	return rr
}

// doCookiePOST issues a POST authenticated with a valid session cookie
// (newSessionCookie, auth.go — same package) and an Origin header matching
// httptest.NewRequest's default Host ("example.com") over plain HTTP,
// satisfying §8.2 FM-50's Origin rule by default. origin overrides that
// default when non-empty (including "" meaning "send no Origin header at
// all" is expressed by the caller passing noOrigin).
func doCookiePOST(t *testing.T, srv *Server, h http.Handler, path string, vals url.Values, origin string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(newSessionCookie(testToken, srv.now()))
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	h.ServeHTTP(rr, req)
	return rr
}

const defaultTestOrigin = "http://example.com"

// seedObserveBatch seeds a minimal batch with the given runs (all "done",
// no observation) via seedBatch/seedRun (console_test.go).
func seedObserveBatch(t *testing.T, store *fakeStore, batch, observerModel string, runIDs ...string) {
	t.Helper()
	now := fixedNow(t)()
	runs := make([]runFixture, 0, len(runIDs))
	for _, id := range runIDs {
		runs = append(runs, runFixture{
			runID: id, scenario: strings.TrimPrefix(id, batch+"-"), startedAt: now.Add(-time.Hour),
			durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true,
		})
	}
	seedBatch(t, store, batch, observerModel, runs, false, nil)
}

// --- TestConsole_WorkerStartsAndStopsWithServer ------------------------

// TestConsole_WorkerStartsAndStopsWithServer pins §8.5 FM-53's wiring:
// Server.StartWorker runs the configured Worker until its context is done,
// and stops ticking once that context is canceled.
func TestConsole_WorkerStartsAndStopsWithServer(t *testing.T) {
	now := fixedNow(t)()
	bucket := wkNewFakeBucket()
	bucket.put("batches/b1/manifest.json", wkManifestJSON(t, now.Add(-time.Hour).Format(time.RFC3339), "claude-sonnet-5", "b1-scenario"))
	bucket.put("runs/b1-scenario/done.json", []byte(`{}`))

	obs := wkNewRecordingObserve()
	q := NewQueue(obs.fn)
	w := NewWorker(WorkerConfig{Bucket: bucket, Queue: q, Now: wkFixedNow(now)})

	srv, _ := newActionServer(t, actionServerOpts{queue: q, worker: w, workerInterval: 5 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	srv.StartWorker(ctx)

	// The first tick enqueues b1-scenario — wait for it deterministically
	// via the recording observe func rather than a fixed sleep.
	wkExpectCall(t, obs.calls)
	close(obs.release)

	cancel()
	// Give the worker goroutine a moment to observe ctx.Done() and stop;
	// wkEventually polls rather than sleeping a fixed amount.
	if !wkEventually(t, func() bool { return q.State("b1-scenario") == "" }) {
		t.Fatal("job never settled after the single tick")
	}

	// Drain any ticks that were already in flight when cancel() fired, then
	// prove no further tick happens once the worker has stopped.
	for {
		select {
		case job := <-obs.calls:
			t.Logf("drained an in-flight tick for %s (queued before cancel)", job.RunID)
			continue
		default:
		}
		break
	}
	wkExpectNoCall(t, obs.calls)
}

// --- TestView_ObserverStateObservingAndDisabled -------------------------

// TestView_ObserverStateObservingAndDisabled pins §8.4 FM-52's observerState
// vocabulary additions: a queued/running run reads "observing" (on the read
// model, via GET /api/runs.json, and on GET /api/runs.md's headline slot);
// the kill switch makes a done, unobserved run read "observer disabled".
func TestView_ObserverStateObservingAndDisabled(t *testing.T) {
	t.Run("queued/running run reads observing", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()

		seedObserveBatch(t, store, "obv1", "claude-sonnet-5", "obv1-a")

		if err := q.Enqueue(context.Background(), Job{RunID: "obv1-a", Batch: "obv1", Model: "claude-sonnet-5"}); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
		wkExpectCall(t, obs.calls)

		jsonRR := doGET(t, h, "/api/runs.json?batch=obv1")
		var out struct {
			Runs []RunsListItem `json:"runs"`
		}
		if err := json.Unmarshal(jsonRR.Body.Bytes(), &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(out.Runs) != 1 || out.Runs[0].ObserverState != observerStateObserving {
			t.Fatalf("runs.json = %+v, want one run with observerState=%q", out.Runs, observerStateObserving)
		}

		mdBody := doGET(t, h, "/api/runs.md?batch=obv1").Body.String()
		if !strings.Contains(mdBody, "("+observerStateObserving+")") {
			t.Errorf("runs.md missing (%s):\n%s", observerStateObserving, mdBody)
		}
	})

	t.Run("kill switch reads observer disabled", func(t *testing.T) {
		srv, store := newActionServer(t, actionServerOpts{observerDisabled: true})
		h := srv.Handler()

		seedObserveBatch(t, store, "obv2", "claude-sonnet-5", "obv2-a")

		jsonRR := doGET(t, h, "/api/runs.json?batch=obv2")
		var out struct {
			Runs []RunsListItem `json:"runs"`
		}
		if err := json.Unmarshal(jsonRR.Body.Bytes(), &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(out.Runs) != 1 || out.Runs[0].ObserverState != observerStateDisabled {
			t.Fatalf("runs.json = %+v, want one run with observerState=%q", out.Runs, observerStateDisabled)
		}

		mdBody := doGET(t, h, "/api/runs.md?batch=obv2").Body.String()
		if !strings.Contains(mdBody, "("+observerStateDisabled+")") {
			t.Errorf("runs.md missing (%s):\n%s", observerStateDisabled, mdBody)
		}
	})
}

// --- TestNewBucketObserveFunc_ForwardsJobSourceToObservation ------------

// TestNewBucketObserveFunc_ForwardsJobSourceToObservation pins the wiring
// this slice closes: NewBucketObserveFunc's returned ObserveFunc must pass
// job.Source through to observer.ObserveConfig.Source, so the stored
// observation records whether a worker tick or a console action produced
// it (§7.5: "source is worker/action/local — supplied by the caller,
// never guessed"). No run bundle is seeded (mirrors
// TestBucketObserve_StoresErrorStatusAndDoesNotRetry, worker_test.go):
// Observe fails at ResultsDir before any claude call, but obs.Source is
// set unconditionally before that failure branch (observer.Observe).
func TestNewBucketObserveFunc_ForwardsJobSourceToObservation(t *testing.T) {
	bucket := wkNewFakeBucket()
	fixedNow := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

	fn := NewBucketObserveFunc(bucket, BucketObserveConfig{
		ClaudePath: "claude", // never reached
		OAuthToken: "test-token",
		Timeout:    time.Minute,
		Environ:    os.Environ,
		Now:        wkFixedNow(fixedNow),
	})

	for _, job := range []Job{
		{RunID: "src-worker-run", Batch: "batch1", Model: "claude-sonnet-5", Source: wkSourceWorker},
		{RunID: "src-action-run", Batch: "batch1", Model: "claude-sonnet-5", Source: sourceAction},
	} {
		if err := fn(context.Background(), job); err != nil {
			t.Fatalf("observe %s: %v", job.RunID, err)
		}
		keys, err := bucket.List(context.Background(), "runs/"+job.RunID+"/observer/")
		if err != nil || len(keys) != 1 {
			t.Fatalf("list observer keys for %s: %v (keys=%v)", job.RunID, err, keys)
		}
		data, err := bucket.Get(context.Background(), keys[0])
		if err != nil {
			t.Fatalf("get %s: %v", keys[0], err)
		}
		var obs observer.Observation
		if err := json.Unmarshal(data, &obs); err != nil {
			t.Fatalf("parse stored observation: %v", err)
		}
		if obs.Source != job.Source {
			t.Errorf("stored obs.Source for %s = %q, want %q (job.Source)", job.RunID, obs.Source, job.Source)
		}
	}
}

// --- TestActions_RunObserveAddsVersion ----------------------------------

// TestActions_RunObserveAddsVersion pins §8.5 FM-53's happy path for
// POST /r/<runId>/observe: a cookie-authenticated request (with a matching
// Origin) redirects 303 back to the run page; a bearer-authenticated
// request answers 202. Both actually enqueue the job.
func TestActions_RunObserveAddsVersion(t *testing.T) {
	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	srv, store := newActionServer(t, actionServerOpts{queue: q})
	h := srv.Handler()

	seedObserveBatch(t, store, "ra1", "claude-sonnet-5", "ra1-cookie", "ra1-bearer")

	cookieRR := doCookiePOST(t, srv, h, "/r/ra1-cookie/observe", url.Values{"model": {"claude-sonnet-5"}}, defaultTestOrigin)
	if cookieRR.Code != http.StatusSeeOther {
		t.Fatalf("cookie POST /r/ra1-cookie/observe: got %d, want 303, body=%s", cookieRR.Code, cookieRR.Body.String())
	}
	if loc := cookieRR.Header().Get("Location"); loc != "/r/ra1-cookie?notice=queued&n=1" {
		t.Errorf("cookie POST Location: got %q, want /r/ra1-cookie?notice=queued&n=1", loc)
	}
	job := wkExpectCall(t, obs.calls)
	if job.RunID != "ra1-cookie" || job.Batch != "ra1" || job.Model != "claude-sonnet-5" {
		t.Errorf("enqueued job = %+v", job)
	}

	bearerRR := doBearerPOST(t, h, "/r/ra1-bearer/observe", url.Values{"model": {"claude-sonnet-5"}})
	if bearerRR.Code != http.StatusAccepted {
		t.Fatalf("bearer POST /r/ra1-bearer/observe: got %d, want 202, body=%s", bearerRR.Code, bearerRR.Body.String())
	}
	job = wkExpectCall(t, obs.calls)
	if job.RunID != "ra1-bearer" {
		t.Errorf("enqueued job.RunID = %q, want ra1-bearer", job.RunID)
	}
}

// --- TestActions_RunObserveModelDefaultChain ----------------------------

// TestActions_RunObserveModelDefaultChain pins §8.5 FM-53's model default
// chain for POST /r/<runId>/observe when the form carries no model at
// all: the run's current observation's model, else the batch manifest's,
// else observer.DefaultModel.
func TestActions_RunObserveModelDefaultChain(t *testing.T) {
	t.Run("no observation: falls back to the manifest's model", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()

		seedObserveBatch(t, store, "mc1", "claude-opus-5", "mc1-a")

		rr := doBearerPOST(t, h, "/r/mc1-a/observe", url.Values{})
		if rr.Code != http.StatusAccepted {
			t.Fatalf("POST /r/mc1-a/observe with no model: got %d, want 202, body=%s", rr.Code, rr.Body.String())
		}
		job := wkExpectCall(t, obs.calls)
		if job.Model != "claude-opus-5" {
			t.Errorf("enqueued job.Model = %q, want the manifest's claude-opus-5", job.Model)
		}
	})

	t.Run("current observation exists: its model wins over the manifest's", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()

		seedObserveBatch(t, store, "mc2", "claude-opus-5", "mc2-a")
		seedObservation(t, store, fixtureObservation("mc2-a")) // Model: claude-sonnet-5

		rr := doBearerPOST(t, h, "/r/mc2-a/observe", url.Values{})
		if rr.Code != http.StatusAccepted {
			t.Fatalf("POST /r/mc2-a/observe with no model: got %d, want 202, body=%s", rr.Code, rr.Body.String())
		}
		job := wkExpectCall(t, obs.calls)
		if job.Model != "claude-sonnet-5" {
			t.Errorf("enqueued job.Model = %q, want the current observation's claude-sonnet-5", job.Model)
		}
	})

	t.Run("neither exists: falls back to observer.DefaultModel", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()

		seedObserveBatch(t, store, "mc3", ObserverOff, "mc3-a")

		rr := doBearerPOST(t, h, "/r/mc3-a/observe", url.Values{})
		if rr.Code != http.StatusAccepted {
			t.Fatalf("POST /r/mc3-a/observe with no model, manifest off: got %d, want 202, body=%s", rr.Code, rr.Body.String())
		}
		job := wkExpectCall(t, obs.calls)
		if job.Model != observer.DefaultModel {
			t.Errorf("enqueued job.Model = %q, want observer.DefaultModel %q", job.Model, observer.DefaultModel)
		}
	})
}

// --- TestActions_ModelOutsideAllowlist400 -------------------------------

// TestActions_ModelOutsideAllowlist400 pins §8.5 FM-53: a model outside the
// allowlist (claude-sonnet-5, claude-opus-5, claude-fable-5-1) is rejected
// 400, on both the run and the batch route, and nothing is enqueued.
func TestActions_ModelOutsideAllowlist400(t *testing.T) {
	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	srv, store := newActionServer(t, actionServerOpts{queue: q})
	h := srv.Handler()

	seedObserveBatch(t, store, "ma1", "claude-sonnet-5", "ma1-a")

	rr := doBearerPOST(t, h, "/r/ma1-a/observe", url.Values{"model": {"gpt-5"}})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("POST /r/ma1-a/observe model=gpt-5: got %d, want 400", rr.Code)
	}
	if want := "claude-sonnet-5, claude-opus-5, claude-fable-5-1"; !strings.Contains(rr.Body.String(), want) {
		t.Errorf("400 body = %q, want it to list the models in order %q", rr.Body.String(), want)
	}

	rr = doBearerPOST(t, h, "/b/ma1/observe", url.Values{"model": {"gpt-5"}})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("POST /b/ma1/observe model=gpt-5: got %d, want 400", rr.Code)
	}

	wkExpectNoCall(t, obs.calls)
}

// --- TestActions_CookiePostWrongOrMissingOrigin403 ----------------------

// TestActions_CookiePostWrongOrMissingOrigin403 pins §8.2 FM-50: a cookie-
// authenticated POST with a wrong or absent Origin is refused 403, before
// anything is enqueued; a bearer-authenticated POST needs no Origin at all.
func TestActions_CookiePostWrongOrMissingOrigin403(t *testing.T) {
	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	srv, store := newActionServer(t, actionServerOpts{queue: q})
	h := srv.Handler()

	seedObserveBatch(t, store, "oa1", "claude-sonnet-5", "oa1-a")

	rr := doCookiePOST(t, srv, h, "/r/oa1-a/observe", url.Values{"model": {"claude-sonnet-5"}}, "http://evil.example")
	if rr.Code != http.StatusForbidden {
		t.Errorf("cookie POST with wrong Origin: got %d, want 403", rr.Code)
	}

	rr = doCookiePOST(t, srv, h, "/r/oa1-a/observe", url.Values{"model": {"claude-sonnet-5"}}, "")
	if rr.Code != http.StatusForbidden {
		t.Errorf("cookie POST with no Origin: got %d, want 403", rr.Code)
	}

	// The literal string "null" is what a browser sends for Origin on a
	// navigate-mode form POST from an opaque origin — still refused, never
	// treated as "no Origin header at all" (item 1: fixing Referrer-Policy
	// so browsers stop sending this must not loosen this check).
	rr = doCookiePOST(t, srv, h, "/r/oa1-a/observe", url.Values{"model": {"claude-sonnet-5"}}, "null")
	if rr.Code != http.StatusForbidden {
		t.Errorf("cookie POST with Origin: null: got %d, want 403", rr.Code)
	}

	wkExpectNoCall(t, obs.calls)

	// A bearer POST with no Origin at all still succeeds.
	bearerRR := doBearerPOST(t, h, "/r/oa1-a/observe", url.Values{"model": {"claude-sonnet-5"}})
	if bearerRR.Code != http.StatusAccepted {
		t.Fatalf("bearer POST with no Origin: got %d, want 202, body=%s", bearerRR.Code, bearerRR.Body.String())
	}
	wkExpectCall(t, obs.calls)
}

// --- TestActions_RunObserveWithoutDone409 -------------------------------

// TestActions_RunObserveWithoutDone409 pins item 2: a run without
// done.json is never enqueued — POST /r/<runId>/observe answers 409 "run
// not finished" instead.
func TestActions_RunObserveWithoutDone409(t *testing.T) {
	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	srv, store := newActionServer(t, actionServerOpts{queue: q})
	h := srv.Handler()

	seedBatch(t, store, "nd1", "claude-sonnet-5", []runFixture{
		{runID: "nd1-a", scenario: "a", startedAt: fixedNow(t)().Add(-time.Minute), done: false},
	}, false, nil)

	rr := doBearerPOST(t, h, "/r/nd1-a/observe", url.Values{"model": {"claude-sonnet-5"}})
	if rr.Code != http.StatusConflict {
		t.Fatalf("POST /r/nd1-a/observe without done.json: got %d, want 409, body=%s", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); !strings.Contains(body, "run not finished") {
		t.Errorf("409 body = %q, want it to contain %q", body, "run not finished")
	}
	wkExpectNoCall(t, obs.calls)
}

// --- TestActions_BatchObserveSkipsRunsWithoutDone -----------------------

// TestActions_BatchObserveSkipsRunsWithoutDone pins item 2: POST
// /b/<batch>/observe never enqueues a run without done.json, with or
// without all=1.
func TestActions_BatchObserveSkipsRunsWithoutDone(t *testing.T) {
	t.Run("without all=1", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()

		seedBatch(t, store, "nd2", "claude-sonnet-5", []runFixture{
			{runID: "nd2-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
			{runID: "nd2-b", scenario: "b", startedAt: fixedNow(t)().Add(-time.Minute), done: false},
		}, false, nil)

		rr := doBearerPOST(t, h, "/b/nd2/observe", url.Values{"model": {"claude-sonnet-5"}})
		if rr.Code != http.StatusAccepted {
			t.Fatalf("POST /b/nd2/observe: got %d, want 202, body=%s", rr.Code, rr.Body.String())
		}
		job := wkExpectCall(t, obs.calls)
		if job.RunID != "nd2-a" {
			t.Errorf("enqueued job.RunID = %q, want nd2-a (the only done run)", job.RunID)
		}
		wkExpectNoCall(t, obs.calls)
	})

	t.Run("with all=1", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()

		seedBatch(t, store, "nd3", "claude-sonnet-5", []runFixture{
			{runID: "nd3-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
			{runID: "nd3-b", scenario: "b", startedAt: fixedNow(t)().Add(-time.Minute), done: false},
		}, false, nil)

		rr := doBearerPOST(t, h, "/b/nd3/observe", url.Values{"model": {"claude-sonnet-5"}, "all": {"1"}})
		if rr.Code != http.StatusAccepted {
			t.Fatalf("POST /b/nd3/observe all=1: got %d, want 202, body=%s", rr.Code, rr.Body.String())
		}
		job := wkExpectCall(t, obs.calls)
		if job.RunID != "nd3-a" {
			t.Errorf("enqueued job.RunID = %q, want nd3-a even with all=1 (nd3-b has no done.json)", job.RunID)
		}
		wkExpectNoCall(t, obs.calls)
	})
}

// --- TestActions_BatchObserveOnlyUnobservedUnlessAll --------------------

// TestActions_BatchObserveOnlyUnobservedUnlessAll pins §8.5 FM-53: without
// all=1, POST /b/<batch>/observe queues only runs with no observation yet;
// with all=1, it queues every run of the batch.
func TestActions_BatchObserveOnlyUnobservedUnlessAll(t *testing.T) {
	t.Run("without all=1", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()

		seedObserveBatch(t, store, "bo1", "claude-sonnet-5", "bo1-a", "bo1-b")
		seedObservation(t, store, fixtureObservation("bo1-a"))

		rr := doBearerPOST(t, h, "/b/bo1/observe", url.Values{"model": {"claude-sonnet-5"}})
		if rr.Code != http.StatusAccepted {
			t.Fatalf("POST /b/bo1/observe: got %d, want 202, body=%s", rr.Code, rr.Body.String())
		}
		job := wkExpectCall(t, obs.calls)
		if job.RunID != "bo1-b" {
			t.Errorf("enqueued job.RunID = %q, want bo1-b (the only unobserved run)", job.RunID)
		}
		wkExpectNoCall(t, obs.calls)
	})

	t.Run("with all=1", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()

		seedObserveBatch(t, store, "bo2", "claude-sonnet-5", "bo2-a", "bo2-b")
		seedObservation(t, store, fixtureObservation("bo2-a"))

		rr := doBearerPOST(t, h, "/b/bo2/observe", url.Values{"model": {"claude-sonnet-5"}, "all": {"1"}})
		if rr.Code != http.StatusAccepted {
			t.Fatalf("POST /b/bo2/observe all=1: got %d, want 202, body=%s", rr.Code, rr.Body.String())
		}
		got := map[string]bool{}
		got[wkExpectCall(t, obs.calls).RunID] = true
		got[wkExpectCall(t, obs.calls).RunID] = true
		if !got["bo2-a"] || !got["bo2-b"] {
			t.Errorf("enqueued runs = %v, want both bo2-a and bo2-b", got)
		}
	})
}

// --- TestActions_BatchObserveWithoutAllRetriesFailedObservation ---------

// TestActions_BatchObserveWithoutAllRetriesFailedObservation pins §8.5
// FM-53's needs-an-assessment predicate (view.go's NeedsAssessment): a run
// with done.json whose CURRENT observation failed (status error/unparsed)
// still needs an assessment. POST /b/<batch>/observe without all=1 must
// queue it, exactly the set the batch page's Assess callout counts — this
// closes the live dead-end where the callout counted a failed assessment
// but the action, which used to skip a run on any observation at all
// regardless of status, never queued it.
func TestActions_BatchObserveWithoutAllRetriesFailedObservation(t *testing.T) {
	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	srv, store := newActionServer(t, actionServerOpts{queue: q})
	h := srv.Handler()

	seedObserveBatch(t, store, "rf1", "claude-sonnet-5", "rf1-ok", "rf1-failed", "rf1-unassessed")

	okObs := fixtureObservation("rf1-ok")
	seedObservation(t, store, okObs)

	failedObs := okObs
	failedObs.RunID = "rf1-failed"
	failedObs.ObsID = "20260911T130000000Z-claude-sonnet-5"
	failedObs.Status = "error"
	failedObs.Error = "boom"
	failedObs.Findings = nil
	seedObservation(t, store, failedObs)

	rr := doBearerPOST(t, h, "/b/rf1/observe", url.Values{"model": {"claude-sonnet-5"}})
	if rr.Code != http.StatusAccepted {
		t.Fatalf("POST /b/rf1/observe: got %d, want 202, body=%s", rr.Code, rr.Body.String())
	}

	got := map[string]bool{}
	got[wkExpectCall(t, obs.calls).RunID] = true
	got[wkExpectCall(t, obs.calls).RunID] = true
	if !got["rf1-failed"] || !got["rf1-unassessed"] {
		t.Errorf("enqueued runs = %v, want rf1-failed (retry a failed observation) and rf1-unassessed (never assessed)", got)
	}
	if got["rf1-ok"] {
		t.Errorf("enqueued runs = %v, want rf1-ok NOT re-queued (its current observation is ok)", got)
	}
	wkExpectNoCall(t, obs.calls)
}

// --- TestActions_DuplicateOrAllWhileInFlight409 -------------------------

// TestActions_DuplicateOrAllWhileInFlight409 pins §8.5 FM-53: a run already
// queued or running answers 409 for POST /r/<runId>/observe; all=1 answers
// 409 while any run of the batch is queued or running.
func TestActions_DuplicateOrAllWhileInFlight409(t *testing.T) {
	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	srv, store := newActionServer(t, actionServerOpts{queue: q})
	h := srv.Handler()

	seedObserveBatch(t, store, "du1", "claude-sonnet-5", "du1-a", "du1-b")

	first := doBearerPOST(t, h, "/r/du1-a/observe", url.Values{"model": {"claude-sonnet-5"}})
	if first.Code != http.StatusAccepted {
		t.Fatalf("first POST /r/du1-a/observe: got %d, want 202", first.Code)
	}
	wkExpectCall(t, obs.calls)

	dup := doBearerPOST(t, h, "/r/du1-a/observe", url.Values{"model": {"claude-sonnet-5"}})
	if dup.Code != http.StatusConflict {
		t.Errorf("duplicate POST /r/du1-a/observe: got %d, want 409", dup.Code)
	}

	allRR := doBearerPOST(t, h, "/b/du1/observe", url.Values{"model": {"claude-sonnet-5"}, "all": {"1"}})
	if allRR.Code != http.StatusConflict {
		t.Errorf("POST /b/du1/observe all=1 while du1-a in flight: got %d, want 409", allRR.Code)
	}
	wkExpectNoCall(t, obs.calls)
}

// --- TestActions_BearerObserveJSONBody ----------------------------------

// TestActions_BearerObserveJSONBody pins §8.5 FM-53's bearer response
// shape for both routes: a bearer request gets 202 with a JSON body
// {queued: [runIds], skipped: [{runId, reason}]} — never an empty body.
func TestActions_BearerObserveJSONBody(t *testing.T) {
	t.Run("/r/<runId>/observe: queued carries the one run id", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()

		seedObserveBatch(t, store, "jb1", "claude-sonnet-5", "jb1-a")

		rr := doBearerPOST(t, h, "/r/jb1-a/observe", url.Values{"model": {"claude-sonnet-5"}})
		if rr.Code != http.StatusAccepted {
			t.Fatalf("POST /r/jb1-a/observe: got %d, want 202, body=%s", rr.Code, rr.Body.String())
		}
		wkExpectCall(t, obs.calls)

		var body actionResponseBody
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal 202 body: %v (body=%s)", err, rr.Body.String())
		}
		if len(body.Queued) != 1 || body.Queued[0] != "jb1-a" || len(body.Skipped) != 0 {
			t.Errorf("202 body = %+v, want queued=[jb1-a] skipped=[]", body)
		}
	})

	t.Run("/b/<batch>/observe: queued carries every run it enqueued", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()

		seedObserveBatch(t, store, "jb2", "claude-sonnet-5", "jb2-a", "jb2-b")

		rr := doBearerPOST(t, h, "/b/jb2/observe", url.Values{"model": {"claude-sonnet-5"}})
		if rr.Code != http.StatusAccepted {
			t.Fatalf("POST /b/jb2/observe: got %d, want 202, body=%s", rr.Code, rr.Body.String())
		}
		wkExpectCall(t, obs.calls)
		wkExpectCall(t, obs.calls)

		var body actionResponseBody
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal 202 body: %v (body=%s)", err, rr.Body.String())
		}
		got := map[string]bool{}
		for _, id := range body.Queued {
			got[id] = true
		}
		if len(body.Queued) != 2 || !got["jb2-a"] || !got["jb2-b"] || len(body.Skipped) != 0 {
			t.Errorf("202 body = %+v, want queued=[jb2-a jb2-b] skipped=[]", body)
		}
	})

	t.Run("/b/<batch>/observe all=1: a run without done.json is reported skipped", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()

		seedBatch(t, store, "jb3", "claude-sonnet-5", []runFixture{
			{runID: "jb3-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
			{runID: "jb3-b", scenario: "b", startedAt: fixedNow(t)().Add(-time.Minute), done: false},
		}, false, nil)

		rr := doBearerPOST(t, h, "/b/jb3/observe", url.Values{"model": {"claude-sonnet-5"}, "all": {"1"}})
		if rr.Code != http.StatusAccepted {
			t.Fatalf("POST /b/jb3/observe all=1: got %d, want 202, body=%s", rr.Code, rr.Body.String())
		}
		wkExpectCall(t, obs.calls)

		var body actionResponseBody
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal 202 body: %v (body=%s)", err, rr.Body.String())
		}
		if len(body.Queued) != 1 || body.Queued[0] != "jb3-a" {
			t.Errorf("202 body.Queued = %v, want [jb3-a]", body.Queued)
		}
		if len(body.Skipped) != 1 || body.Skipped[0].RunID != "jb3-b" || body.Skipped[0].Reason == "" {
			t.Errorf("202 body.Skipped = %+v, want one entry for jb3-b with a non-empty reason", body.Skipped)
		}
	})
}

// --- TestActions_OriginFallsBackToHostWithoutForwardedHeaders -----------

// TestActions_OriginFallsBackToHostWithoutForwardedHeaders pins §8.2 FM-50:
// without X-Forwarded-Host/-Proto, the expected Origin falls back to plain
// Host over http (no TLS) — and an Origin using https instead is rejected.
func TestActions_OriginFallsBackToHostWithoutForwardedHeaders(t *testing.T) {
	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	srv, store := newActionServer(t, actionServerOpts{queue: q})
	h := srv.Handler()

	seedObserveBatch(t, store, "of1", "claude-sonnet-5", "of1-a")

	ok := doCookiePOST(t, srv, h, "/r/of1-a/observe", url.Values{"model": {"claude-sonnet-5"}}, "http://example.com")
	if ok.Code != http.StatusSeeOther {
		t.Fatalf("Origin http://example.com (matches Host, no forwarded headers): got %d, want 303, body=%s", ok.Code, ok.Body.String())
	}
	wkExpectCall(t, obs.calls)

	seedObserveBatch(t, store, "of2", "claude-sonnet-5", "of2-a")
	wrongScheme := doCookiePOST(t, srv, h, "/r/of2-a/observe", url.Values{"model": {"claude-sonnet-5"}}, "https://example.com")
	if wrongScheme.Code != http.StatusForbidden {
		t.Errorf("Origin https://example.com over plain http: got %d, want 403", wrongScheme.Code)
	}
	wkExpectNoCall(t, obs.calls)
}

// --- TestActions_CookieObserveRedirectsWithNotice -----------------------

// TestActions_CookieObserveRedirectsWithNotice pins §8.5 FM-53: a cookie
// request is always sent back to the page it came from (its own
// same-origin Referer path, else the run/batch page) with ?notice=<code>
// — queued (with n), busy, not-finished, bad-model, unavailable — never
// the raw status code.
func TestActions_CookieObserveRedirectsWithNotice(t *testing.T) {
	t.Run("success: notice=queued&n=1, no Referer falls back to the run page", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()
		seedObserveBatch(t, store, "cn1", "claude-sonnet-5", "cn1-a")

		rr := doCookiePOST(t, srv, h, "/r/cn1-a/observe", url.Values{"model": {"claude-sonnet-5"}}, defaultTestOrigin)
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("got %d, want 303, body=%s", rr.Code, rr.Body.String())
		}
		if loc := rr.Header().Get("Location"); loc != "/r/cn1-a?notice=queued&n=1" {
			t.Errorf("Location = %q, want /r/cn1-a?notice=queued&n=1", loc)
		}
		wkExpectCall(t, obs.calls)
	})

	t.Run("bad model: notice=bad-model", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()
		seedObserveBatch(t, store, "cn2", "claude-sonnet-5", "cn2-a")

		rr := doCookiePOST(t, srv, h, "/r/cn2-a/observe", url.Values{"model": {"gpt-5"}}, defaultTestOrigin)
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("got %d, want 303, body=%s", rr.Code, rr.Body.String())
		}
		if loc := rr.Header().Get("Location"); loc != "/r/cn2-a?notice=bad-model" {
			t.Errorf("Location = %q, want /r/cn2-a?notice=bad-model", loc)
		}
		wkExpectNoCall(t, obs.calls)
	})

	t.Run("not finished: notice=not-finished", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()
		seedBatch(t, store, "cn3", "claude-sonnet-5", []runFixture{
			{runID: "cn3-a", scenario: "a", startedAt: fixedNow(t)().Add(-time.Minute), done: false},
		}, false, nil)

		rr := doCookiePOST(t, srv, h, "/r/cn3-a/observe", url.Values{"model": {"claude-sonnet-5"}}, defaultTestOrigin)
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("got %d, want 303, body=%s", rr.Code, rr.Body.String())
		}
		if loc := rr.Header().Get("Location"); loc != "/r/cn3-a?notice=not-finished" {
			t.Errorf("Location = %q, want /r/cn3-a?notice=not-finished", loc)
		}
		wkExpectNoCall(t, obs.calls)
	})

	t.Run("busy: notice=busy", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()
		seedObserveBatch(t, store, "cn4", "claude-sonnet-5", "cn4-a")

		first := doBearerPOST(t, h, "/r/cn4-a/observe", url.Values{"model": {"claude-sonnet-5"}})
		if first.Code != http.StatusAccepted {
			t.Fatalf("first POST: got %d, want 202", first.Code)
		}
		wkExpectCall(t, obs.calls)

		rr := doCookiePOST(t, srv, h, "/r/cn4-a/observe", url.Values{"model": {"claude-sonnet-5"}}, defaultTestOrigin)
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("got %d, want 303, body=%s", rr.Code, rr.Body.String())
		}
		if loc := rr.Header().Get("Location"); loc != "/r/cn4-a?notice=busy" {
			t.Errorf("Location = %q, want /r/cn4-a?notice=busy", loc)
		}
	})

	t.Run("unavailable: notice=unavailable", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q, observerCredentialMissing: true})
		h := srv.Handler()
		seedObserveBatch(t, store, "cn5", "claude-sonnet-5", "cn5-a")

		rr := doCookiePOST(t, srv, h, "/r/cn5-a/observe", url.Values{"model": {"claude-sonnet-5"}}, defaultTestOrigin)
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("got %d, want 303, body=%s", rr.Code, rr.Body.String())
		}
		if loc := rr.Header().Get("Location"); loc != "/r/cn5-a?notice=unavailable" {
			t.Errorf("Location = %q, want /r/cn5-a?notice=unavailable", loc)
		}
		wkExpectNoCall(t, obs.calls)
	})

	t.Run("same-origin Referer: redirects to its path (dropping its own query), not the default page", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()
		seedObserveBatch(t, store, "cn6", "claude-sonnet-5", "cn6-a")

		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/r/cn6-a/observe", strings.NewReader(url.Values{"model": {"claude-sonnet-5"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(newSessionCookie(testToken, srv.now()))
		req.Header.Set("Origin", defaultTestOrigin)
		req.Header.Set("Referer", "http://example.com/b/cn6?sort=cost")
		h.ServeHTTP(rr, req)

		if rr.Code != http.StatusSeeOther {
			t.Fatalf("got %d, want 303, body=%s", rr.Code, rr.Body.String())
		}
		if loc := rr.Header().Get("Location"); loc != "/b/cn6?notice=queued&n=1" {
			t.Errorf("Location = %q, want /b/cn6?notice=queued&n=1 (the Referer's path, dropping its own query)", loc)
		}
		wkExpectCall(t, obs.calls)
	})

	t.Run("cross-origin Referer: falls back to the default page", func(t *testing.T) {
		obs := wkNewRecordingObserve()
		defer close(obs.release)
		q := NewQueue(obs.fn)
		srv, store := newActionServer(t, actionServerOpts{queue: q})
		h := srv.Handler()
		seedObserveBatch(t, store, "cn7", "claude-sonnet-5", "cn7-a")

		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/r/cn7-a/observe", strings.NewReader(url.Values{"model": {"claude-sonnet-5"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(newSessionCookie(testToken, srv.now()))
		req.Header.Set("Origin", defaultTestOrigin)
		req.Header.Set("Referer", "http://evil.example/somewhere")
		h.ServeHTTP(rr, req)

		if rr.Code != http.StatusSeeOther {
			t.Fatalf("got %d, want 303, body=%s", rr.Code, rr.Body.String())
		}
		if loc := rr.Header().Get("Location"); loc != "/r/cn7-a?notice=queued&n=1" {
			t.Errorf("Location = %q, want /r/cn7-a?notice=queued&n=1 (cross-origin Referer ignored)", loc)
		}
		wkExpectCall(t, obs.calls)
	})
}

// --- TestActions_WorkDespiteKillSwitchAndOffManifest --------------------

// TestActions_WorkDespiteKillSwitchAndOffManifest pins §8.5 FM-53: "Actions
// work regardless of the manifest field and the kill switch."
func TestActions_WorkDespiteKillSwitchAndOffManifest(t *testing.T) {
	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)
	srv, store := newActionServer(t, actionServerOpts{queue: q, observerDisabled: true})
	h := srv.Handler()

	seedObserveBatch(t, store, "wk1", "off", "wk1-a")

	rr := doBearerPOST(t, h, "/r/wk1-a/observe", url.Values{"model": {"claude-sonnet-5"}})
	if rr.Code != http.StatusAccepted {
		t.Fatalf("POST /r/wk1-a/observe despite kill switch + manifest off: got %d, want 202, body=%s", rr.Code, rr.Body.String())
	}
	job := wkExpectCall(t, obs.calls)
	if job.RunID != "wk1-a" {
		t.Errorf("enqueued job.RunID = %q, want wk1-a", job.RunID)
	}
}

// --- TestActions_MissingOAuthToken503 -----------------------------------

// TestActions_MissingOAuthToken503 pins §8.5 FM-53: without
// CLAUDE_CODE_OAUTH_TOKEN, actions answer 503 "observer credential missing"
// and enqueue nothing; a separately-configured unresolved --claude path
// answers 503 "observer unavailable".
func TestActions_MissingOAuthToken503(t *testing.T) {
	obs := wkNewRecordingObserve()
	defer close(obs.release)
	q := NewQueue(obs.fn)

	srv, store := newActionServer(t, actionServerOpts{queue: q, observerCredentialMissing: true})
	h := srv.Handler()
	seedObserveBatch(t, store, "mo1", "claude-sonnet-5", "mo1-a")

	rr := doBearerPOST(t, h, "/r/mo1-a/observe", url.Values{"model": {"claude-sonnet-5"}})
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /r/mo1-a/observe with missing OAuth token: got %d, want 503, body=%s", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); !strings.Contains(body, "observer credential missing") {
		t.Errorf("503 body = %q, want it to contain %q", body, "observer credential missing")
	}
	wkExpectNoCall(t, obs.calls)

	srv2, store2 := newActionServer(t, actionServerOpts{queue: q, observerClaudePathUnresolved: true})
	h2 := srv2.Handler()
	seedObserveBatch(t, store2, "mo2", "claude-sonnet-5", "mo2-a")

	rr2 := doBearerPOST(t, h2, "/r/mo2-a/observe", url.Values{"model": {"claude-sonnet-5"}})
	if rr2.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /r/mo2-a/observe with unresolved claude path: got %d, want 503, body=%s", rr2.Code, rr2.Body.String())
	}
	if body := rr2.Body.String(); !strings.Contains(body, "observer unavailable") {
		t.Errorf("503 body = %q, want it to contain %q", body, "observer unavailable")
	}
	wkExpectNoCall(t, obs.calls)
}
