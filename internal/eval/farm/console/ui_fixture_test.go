package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// uiFixtureRoute is one named browser destination. Want is a stable phrase
// from the public HTML response, not an implementation helper or CSS detail.
type uiFixtureRoute struct {
	State   string   `json:"state"`
	Path    string   `json:"path"`
	Status  int      `json:"status"`
	Want    string   `json:"-"`
	WantAll []string `json:"-"`
	Forbid  []string `json:"-"`
}

type uiFixtureVariant struct {
	Name   string
	Server *Server
	Routes []uiFixtureRoute
}

type uiFixtureSuite struct {
	Variants []uiFixtureVariant
	release  chan struct{}
	once     sync.Once
}

func (s *uiFixtureSuite) close() { s.once.Do(func() { close(s.release) }) }

// uiFixtureSleeper keeps the login-delay seam race-safe when a browser and
// automated probes reject tokens concurrently. The fixture never sleeps.
type uiFixtureSleeper struct {
	mu    sync.Mutex
	calls []time.Duration
}

func (s *uiFixtureSleeper) Sleep(d time.Duration) {
	s.mu.Lock()
	s.calls = append(s.calls, d)
	s.mu.Unlock()
}

type uiFixtureErrorStore struct{}

func (uiFixtureErrorStore) Get(context.Context, string) ([]byte, error) {
	return nil, errors.New("fixture object store unavailable")
}
func (uiFixtureErrorStore) Put(context.Context, string, []byte) error {
	return errors.New("fixture object store unavailable")
}
func (uiFixtureErrorStore) Head(context.Context, string) (bool, int64, error) {
	return false, 0, errors.New("fixture object store unavailable")
}
func (uiFixtureErrorStore) List(context.Context, string) ([]string, error) {
	return nil, errors.New("fixture object store unavailable")
}

// TestUIFixtureServer is both the automated fixture contract and the opt-in
// browser harness for the actual Handler. Its normal form probes every named
// route without opening a port. Set ZCP_FARM_UI_FIXTURE=1 to keep dynamically
// allocated loopback servers alive for CUA/manual browser verification:
//
//	ZCP_FARM_UI_FIXTURE=1 go test ./internal/eval/farm/console \
//	  -run '^TestUIFixtureServer$' -count=1 -timeout=0 -v
//
// The log prints a JSON route manifest and the local-only test token. One login
// on localhost authenticates every listed port. Stop the command to shut down.
func TestUIFixtureServer(t *testing.T) {
	suite := newUIFixtureSuite(t)

	required := []string{
		"login-initial", "login-rejected", "login-safe-return", "login-expired-session", "overview-latest-and-previous", "overview-empty-history", "overview-no-match",
		"overview-evaluation-history", "overview-empty-batch-history", "overview-all-history", "overview-unknown-cost", "observer-on-with-queue",
		"observer-off", "observer-unavailable", "action-notice", "action-notice-busy", "action-notice-not-finished", "html-invalid-filter", "html-missing-route",
		"html-missing-batch", "html-missing-run", "html-missing-assessment", "html-overview-store-error", "html-list-store-error",
		"html-findings-store-error", "html-batch-store-error", "html-run-store-error",
		"problems-all-statuses", "problems-regressed", "problems-first-seen", "problems-recurring", "problems-still-emitted", "problems-gone",
		"overview-no-problems-covered", "problems-unassessed-only", "problems-unavailable-warning", "problems-full-history-narrow-window",
		"problems-unconfirmed", "problems-expanded-members", "problems-filter-sort", "problems-no-source", "problems-no-match", "findings-quotes", "findings-filter-sort", "findings-no-source",
		"findings-no-match", "batch-five-groups", "batch-no-previous", "batch-filtered-runs", "batch-no-assessment", "batch-partial-assessment", "batch-all-assessed",
		"batch-zero-cost", "batch-unknown-cost", "batch-no-eligible-assessment",
		"run-passed", "run-failed", "run-blocked", "run-not-started", "run-running", "run-stalled", "run-not-assessed", "run-assessment-error",
		"run-assessment-unparsed", "run-failed-current-older-success", "run-historical-assessment", "run-queued", "run-pre-store-failure",
		"run-partial-record", "run-zero-cost", "run-unknown-cost", "run-disputed-check", "run-inconclusive-goal", "run-quote-found-not-found", "run-long-content", "run-cited-steps", "run-error-steps", "terms-navigation",
	}
	seen := make(map[string]bool)
	for _, variant := range suite.Variants {
		for _, route := range variant.Routes {
			if seen[route.State] {
				t.Fatalf("duplicate fixture state %q", route.State)
			}
			seen[route.State] = true
			rr := doGET(t, variant.Server.Handler(), route.Path)
			if rr.Code != route.Status {
				t.Errorf("%s %s: status = %d, want %d; body=%s", variant.Name, route.Path, rr.Code, route.Status, rr.Body.String())
				continue
			}
			if route.Want != "" && !strings.Contains(rr.Body.String(), route.Want) {
				t.Errorf("%s %s: body missing %q; body=%s", variant.Name, route.Path, route.Want, rr.Body.String())
			}
			for _, want := range route.WantAll {
				if !strings.Contains(rr.Body.String(), want) {
					t.Errorf("%s %s: body missing companion %q; body=%s", variant.Name, route.Path, want, rr.Body.String())
				}
			}
			for _, forbidden := range route.Forbid {
				if strings.Contains(rr.Body.String(), forbidden) {
					t.Errorf("%s %s: body unexpectedly contains %q", variant.Name, route.Path, forbidden)
				}
			}
			if got := rr.Header().Get("Content-Security-Policy"); got == "" {
				t.Errorf("%s %s: missing Content-Security-Policy", variant.Name, route.Path)
			}
		}
	}
	for _, state := range required {
		if !seen[state] {
			t.Errorf("fixture manifest missing required state %q", state)
		}
	}
	assertUIFixtureAuthStates(t, suite.Variants[0].Server)

	if os.Getenv("ZCP_FARM_UI_FIXTURE") != "1" {
		return
	}
	serveUIFixture(t, suite)
}

func assertUIFixtureAuthStates(t *testing.T, srv *Server) {
	t.Helper()
	h := srv.Handler()
	form := url.Values{"token": {"fixture-wrong-token"}, "next": {"/r/ui-current-deploy"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	wantRejectedLocation := "/login?error=1&next=" + url.QueryEscape("/r/ui-current-deploy")
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != wantRejectedLocation || len(rr.Result().Cookies()) != 0 {
		t.Errorf("rejected login did not retain its safe return path: status=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}
	req = httptest.NewRequest(http.MethodGet, wantRejectedLocation, nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `aria-invalid="true"`) ||
		!strings.Contains(rr.Body.String(), `role="alert"`) || !strings.Contains(rr.Body.String(), "/r/ui-current-deploy") ||
		strings.Contains(rr.Body.String(), "fixture-wrong-token") {
		t.Errorf("rejected login did not render its associated field error: status=%d body=%s", rr.Code, rr.Body.String())
	}

	form.Set("token", testToken)
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/r/ui-current-deploy" {
		t.Errorf("accepted login safe return: status=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: sessionCookieValue(testToken, srv.now().Add(-time.Minute))})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther || !strings.HasPrefix(rr.Header().Get("Location"), "/login?next=") {
		t.Errorf("expired fixture session: status=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}
}

func newUIFixtureSuite(t *testing.T) *uiFixtureSuite {
	t.Helper()
	now := fixedNow(t)
	sleeper := &uiFixtureSleeper{}
	store := newFakeStore()
	seedUIFixture(t, store)

	release := make(chan struct{})
	suite := &uiFixtureSuite{release: release}
	var queue *Queue
	t.Cleanup(func() {
		suite.close()
		if queue != nil {
			uiWaitFor(t, func() bool {
				queued, running := queue.Stats()
				return queued == 0 && running == 0
			}, "fixture queue shutdown")
		}
	})
	started := make(chan string, wkMaxConcurrent)
	failed := make(chan struct{}, 1)
	queue = NewQueue(func(_ context.Context, job Job) error {
		if job.RunID == "ui-live-prestore" {
			failed <- struct{}{}
			return errors.New("fixture assessment stopped before storage")
		}
		started <- job.RunID
		<-release
		return nil
	})
	queue.now = now
	queue.logf = func(string, ...any) {}
	if err := queue.Enqueue(context.Background(), Job{RunID: "ui-live-prestore", Batch: "ui-live", Model: observer.DefaultModel, Source: sourceAction}); err != nil {
		t.Fatalf("enqueue pre-store fixture: %v", err)
	}
	<-failed
	uiWaitFor(t, func() bool { _, ok := queue.LastFailure("ui-live-prestore"); return ok }, "pre-store failure")
	for _, id := range []string{"ui-live-running-a", "ui-live-running-b", "ui-live-running-c"} {
		if err := queue.Enqueue(context.Background(), Job{RunID: id, Batch: "ui-live", Model: observer.DefaultModel, Source: sourceAction}); err != nil {
			t.Fatalf("enqueue held fixture %s: %v", id, err)
		}
	}
	for range wkMaxConcurrent {
		<-started
	}
	if err := queue.Enqueue(context.Background(), Job{RunID: "ui-live-queued", Batch: "ui-live", Model: observer.DefaultModel, Source: sourceAction}); err != nil {
		t.Fatalf("enqueue held fixture ui-live-queued: %v", err)
	}
	uiWaitFor(t, func() bool {
		queued, running := queue.Stats()
		return queued == 1 && running == wkMaxConcurrent
	}, "three running and one queued fixture jobs")

	available := NewServer(Config{
		Store: store, Token: testToken, Now: now, Queue: queue,
		LoginFailDelay: time.Millisecond, Sleep: sleeper.Sleep,
	})
	available.logf = func(string, ...any) {}
	newStaticServer := func(mutate func(*Config)) *Server {
		variantStore := newFakeStore()
		seedUIFixture(t, variantStore)
		cfg := Config{Store: variantStore, Token: testToken, Now: now, Queue: NewQueue(func(context.Context, Job) error { return nil }), LoginFailDelay: time.Millisecond, Sleep: sleeper.Sleep}
		mutate(&cfg)
		srv := NewServer(cfg)
		srv.logf = func(string, ...any) {}
		return srv
	}
	cleanStore := newFakeStore()
	seedUICleanFixture(t, cleanStore, true)
	cleanServer := NewServer(Config{Store: cleanStore, Token: testToken, Now: now, Queue: NewQueue(func(context.Context, Job) error { return nil }), LoginFailDelay: time.Millisecond, Sleep: sleeper.Sleep})
	cleanServer.logf = func(string, ...any) {}
	unassessedStore := newFakeStore()
	seedUICleanFixture(t, unassessedStore, false)
	unassessedServer := NewServer(Config{Store: unassessedStore, Token: testToken, Now: now, Queue: NewQueue(func(context.Context, Job) error { return nil }), LoginFailDelay: time.Millisecond, Sleep: sleeper.Sleep})
	unassessedServer.logf = func(string, ...any) {}

	variants := []uiFixtureVariant{
		{Name: "available", Server: available, Routes: availableUIFixtureRoutes()},
		{Name: "observer-off", Server: newStaticServer(func(cfg *Config) { cfg.ObserverDisabled = true }), Routes: []uiFixtureRoute{{State: "observer-off", Path: "/", Status: http.StatusOK, Want: "off on this console"}}},
		{Name: "observer-unavailable", Server: newStaticServer(func(cfg *Config) { cfg.ObserverCredentialMissing = true }), Routes: []uiFixtureRoute{{State: "observer-unavailable", Path: "/r/ui-states-not-assessed", Status: http.StatusOK, Want: "unavailable — CLAUDE_CODE_OAUTH_TOKEN"}}},
		{Name: "empty-source", Server: NewServer(Config{Store: newFakeStore(), Token: testToken, Now: now, Queue: NewQueue(func(context.Context, Job) error { return nil }), LoginFailDelay: time.Millisecond, Sleep: sleeper.Sleep}), Routes: []uiFixtureRoute{
			{State: "overview-empty-history", Path: "/", Status: http.StatusOK, Want: "No evaluation batch has finished yet"},
			{State: "problems-no-source", Path: "/problems", Status: http.StatusOK, Want: "No runs in this window"},
			{State: "findings-no-source", Path: "/findings", Status: http.StatusOK, Want: "No source runs in this window"},
		}},
		{Name: "clean-source", Server: cleanServer, Routes: []uiFixtureRoute{{State: "overview-no-problems-covered", Path: "/", Status: http.StatusOK, WantAll: []string{"No current problems found.", "Assessment coverage is 1 of 1 runs"}}}},
		{Name: "unassessed-source", Server: unassessedServer, Routes: []uiFixtureRoute{{State: "problems-unassessed-only", Path: "/problems", Status: http.StatusOK, WantAll: []string{"No assessed runs in this window", "Runs without one do not establish that the build is clean"}}}},
		{Name: "store-error", Server: NewServer(Config{Store: uiFixtureErrorStore{}, Token: testToken, Now: now, Queue: NewQueue(func(context.Context, Job) error { return nil }), LoginFailDelay: time.Millisecond, Sleep: sleeper.Sleep}), Routes: []uiFixtureRoute{
			{State: "html-overview-store-error", Path: "/", Status: http.StatusInternalServerError, Want: "Overview unavailable"},
			{State: "html-list-store-error", Path: "/problems", Status: http.StatusBadGateway, Want: "Problems unavailable"},
			{State: "html-findings-store-error", Path: "/findings", Status: http.StatusBadGateway, Want: "Findings unavailable"},
			{State: "html-batch-store-error", Path: "/b/ui-current", Status: http.StatusBadGateway, Want: "Batch data unavailable"},
			{State: "html-run-store-error", Path: "/r/ui-current-deploy", Status: http.StatusBadGateway, Want: "Run data unavailable"},
		}},
	}
	suite.Variants = variants
	return suite
}

func seedUICleanFixture(t *testing.T, store *fakeStore, assessed bool) {
	t.Helper()
	now := fixedNow(t)()
	seedBatchAt(t, store, "ui-clean", "claude-sonnet-5", now.Add(-10*time.Minute), []runFixture{{
		runID: "ui-clean-a", scenario: "clean-coverage", startedAt: now.Add(-10 * time.Minute), durationS: "10s", costUsd: 0.02, done: true, taskResult: farm.VerdictPassed,
	}}, true, map[string]string{"ui-clean-a": farm.VerdictPassed})
	if assessed {
		seedObservation(t, store, observer.Observation{
			FormatVersion: observer.ObservationFormat2, RunID: "ui-clean-a", ObsID: "20260911T115100000Z-claude-sonnet-5",
			Model: "claude-sonnet-5", CreatedAt: now.Add(-9 * time.Minute), Status: "ok", Outcome: observer.OutcomeOK, Headline: "The run completed without a recorded problem",
			Goal: observer.Goal{Reached: "yes"}, Checks: observer.Checks{Verdict: farm.VerdictPassed, Agree: true},
		})
	}
}

func uiWaitFor(t *testing.T, ready func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func availableUIFixtureRoutes() []uiFixtureRoute {
	return []uiFixtureRoute{
		{State: "login-initial", Path: "/login", Status: http.StatusOK, Want: "Console token"},
		{State: "login-rejected", Path: "/login?error=1&next=/r/ui-current-deploy", Status: http.StatusOK, Want: "Wrong token"},
		{State: "login-safe-return", Path: "/login?next=/r/ui-current-deploy", Status: http.StatusOK, Want: "/r/ui-current-deploy"},
		{State: "login-expired-session", Path: "/login", Status: http.StatusOK, Want: "Sign in"},
		{State: "overview-latest-and-previous", Path: "/", Status: http.StatusOK, Want: "Latest evaluation"},
		{State: "overview-evaluation-history", Path: "/?kind=evaluation", Status: http.StatusOK, Want: "Evaluation"},
		{State: "overview-empty-batch-history", Path: "/?kind=empty", Status: http.StatusOK, Want: "ui-empty"},
		{State: "overview-all-history", Path: "/?kind=all", Status: http.StatusOK, Want: "ui-live"},
		{State: "overview-unknown-cost", Path: "/?kind=all&sort=cost&dir=asc", Status: http.StatusOK, Want: "unknown"},
		{State: "observer-on-with-queue", Path: "/?notice=queued&n=4", Status: http.StatusOK, Want: "4 queued/running"},
		{State: "action-notice", Path: "/b/ui-live?notice=queued&n=4", Status: http.StatusOK, Want: "Queued 4 runs for assessment"},
		{State: "action-notice-busy", Path: "/b/ui-live?notice=busy", Status: http.StatusOK, Want: "Already queued or running"},
		{State: "action-notice-not-finished", Path: "/r/ui-states-stalled?notice=not-finished", Status: http.StatusOK, Want: "hasn&#39;t finished yet"},
		{State: "overview-no-match", Path: "/?kind=empty&since=1h", Status: http.StatusOK, Want: "No batches match"},
		{State: "problems-all-statuses", Path: "/problems?status=all&since=90d", Status: http.StatusOK, Want: "matching problem"},
		{State: "problems-regressed", Path: "/problems?status=new&since=90d", Status: http.StatusOK, Want: "state-badge state-new"},
		{State: "problems-first-seen", Path: "/problems?status=first-seen&since=90d", Status: http.StatusOK, Want: "state-badge state-first-seen"},
		{State: "problems-recurring", Path: "/problems?status=recurring&since=90d", Status: http.StatusOK, Want: "state-badge state-recurring"},
		{State: "problems-still-emitted", Path: "/problems?status=still-emitted&since=90d", Status: http.StatusOK, Want: "state-badge state-still-emitted"},
		{State: "problems-gone", Path: "/problems?status=gone&since=90d", Status: http.StatusOK, Want: "state-badge state-gone"},
		{State: "problems-unconfirmed", Path: "/problems?status=unconfirmed&since=90d", Status: http.StatusOK, Want: "state-badge state-unconfirmed"},
		{State: "problems-unavailable-warning", Path: "/problems?status=unconfirmed&since=90d", Status: http.StatusOK, WantAll: []string{"Unavailable: no assessed runs on the newest build for this problem's scenarios", "Current time window"}},
		{State: "problems-full-history-narrow-window", Path: "/problems?status=recurring&since=24h", Status: http.StatusOK, WantAll: []string{"state-badge state-recurring", "2 runs", "2 batches", "Current time window"}},
		{State: "problems-expanded-members", Path: "/problems?status=all&since=90d", Status: http.StatusOK, Want: "Members ("},
		{State: "problems-filter-sort", Path: "/problems?cause=zcp&severity=medium&status=all&since=90d&sort=last&dir=asc", Status: http.StatusOK, Want: "Reset filters"},
		{State: "problems-no-match", Path: "/problems?scenario=missing", Status: http.StatusOK, Want: "No problems match these filters"},
		{State: "findings-quotes", Path: "/findings?since=90d", Status: http.StatusOK, WantAll: []string{">quote found</span>", ">quote not found</span>", "discovered ok", "a quote that is absent from the cited step"}},
		{State: "findings-filter-sort", Path: "/findings?cause=zcp&severity=medium&surface=tool%3Azerops_import&scenario=import&batch=ui-current&build=bc0000000000&since=90d&sort=newest&dir=asc", Status: http.StatusOK, WantAll: []string{"Cause: ZCP", "Severity: Medium", "Surface: tool:zerops_import", "Scenario: import", "Batch: ui-current", "Build: bc0000000000", "Window: 90d", "Newest", "> ▲</span>", "A service-name conflict leaves the agent without a recovery path", "Reset filters"}},
		{State: "findings-no-match", Path: "/findings?scenario=missing", Status: http.StatusOK, Want: "No findings match these filters"},
		{State: "batch-five-groups", Path: "/b/ui-states", Status: http.StatusOK, WantAll: []string{"Failed and blocked", "Not finished", "Not assessed", "Problems in passed runs", "Clean"}},
		{State: "batch-no-previous", Path: "/b/ui-previous", Status: http.StatusOK, Want: "No earlier finished batch"},
		{State: "batch-filtered-runs", Path: "/b/ui-states?verdict=blocked", Status: http.StatusOK, Want: "blocked"},
		{State: "batch-no-assessment", Path: "/b/ui-live", Status: http.StatusOK, Want: "not assessed"},
		{State: "batch-partial-assessment", Path: "/b/ui-states", Status: http.StatusOK, WantAll: []string{"AI assessment", "2/9", "not assessed", "assessment failed", "inconclusive"}},
		{State: "batch-all-assessed", Path: "/b/ui-previous", Status: http.StatusOK, Want: "6/6"},
		{State: "batch-zero-cost", Path: "/b/ui-states", Status: http.StatusOK, Want: "$0.00"},
		{State: "batch-unknown-cost", Path: "/b/ui-unknown-cost", Status: http.StatusOK, Want: "unknown"},
		{State: "batch-no-eligible-assessment", Path: "/b/ui-empty", Status: http.StatusOK, Want: "Not finished"},
		{State: "run-passed", Path: "/r/ui-states-clean", Status: http.StatusOK, Want: "passed", Forbid: []string{"Disputed"}},
		{State: "run-failed", Path: "/r/ui-states-failed", Status: http.StatusOK, Want: "failed"},
		{State: "run-blocked", Path: "/r/ui-states-blocked", Status: http.StatusOK, Want: "blocked"},
		{State: "run-not-started", Path: "/r/ui-states-not-started", Status: http.StatusOK, Want: "not started"},
		{State: "run-running", Path: "/r/ui-live-running-a", Status: http.StatusOK, WantAll: []string{`verdict-running big`, `Started <strong><span title="Not recorded">—</span>`, `Duration <strong><span title="Not recorded">—</span>`}},
		{State: "run-stalled", Path: "/r/ui-states-stalled", Status: http.StatusOK, Want: "stalled"},
		{State: "run-not-assessed", Path: "/r/ui-states-not-assessed", Status: http.StatusOK, Want: "not assessed"},
		{State: "run-assessment-error", Path: "/r/ui-states-assessment-error", Status: http.StatusOK, Want: "assessment failed"},
		{State: "run-assessment-unparsed", Path: "/r/ui-states-assessment-unparsed", Status: http.StatusOK, Want: "answer did not parse"},
		{State: "run-failed-current-older-success", Path: "/r/ui-current-deploy", Status: http.StatusOK, Want: "The newest assessment"},
		{State: "run-historical-assessment", Path: "/r/ui-current-deploy?obs=20260911T100000000Z-claude-sonnet-5", Status: http.StatusOK, Want: "earlier assessment"},
		{State: "run-queued", Path: "/r/ui-live-queued", Status: http.StatusOK, Want: "Queued"},
		{State: "run-pre-store-failure", Path: "/r/ui-live-prestore", Status: http.StatusOK, Want: "failed before anything was stored"},
		{State: "run-partial-record", Path: "/r/ui-partial-record", Status: http.StatusOK, WantAll: []string{`verdict-passed big`, `Duration <strong>18s`, "This run did not record a transcript, so steps are unavailable.", "Task prompt", "do the thing for partial-record"}},
		{State: "run-zero-cost", Path: "/r/ui-states-not-assessed", Status: http.StatusOK, Want: "$0.00"},
		{State: "run-unknown-cost", Path: "/r/ui-unknown-cost-a", Status: http.StatusOK, Want: "Not recorded"},
		{State: "run-disputed-check", Path: "/r/ui-current-checks", Status: http.StatusOK, Want: "Disputed"},
		{State: "run-inconclusive-goal", Path: "/r/ui-states-problem", Status: http.StatusOK, Want: "inconclusive"},
		{State: "run-quote-found-not-found", Path: "/r/ui-states-problem", Status: http.StatusOK, WantAll: []string{"quote found", "quote not found", "discovered ok", "missing recovery quotation"}},
		{State: "run-long-content", Path: "/r/ui-current-deploy", Status: http.StatusOK, Want: "recorded steps"},
		{State: "run-cited-steps", Path: "/r/ui-current-deploy?steps=cited", Status: http.StatusOK, Want: "1 matching steps"},
		{State: "run-error-steps", Path: "/r/ui-current-deploy?steps=errors", Status: http.StatusOK, Want: "1 matching steps"},
		{State: "terms-navigation", Path: "/terms", Status: http.StatusOK, Want: "Problem status"},
		{State: "html-invalid-filter", Path: "/problems?bogus=1", Status: http.StatusBadRequest, Want: "Unknown filter"},
		{State: "html-missing-route", Path: "/missing", Status: http.StatusNotFound, Want: "Page not found"},
		{State: "html-missing-batch", Path: "/b/missing", Status: http.StatusNotFound, Want: "Batch not found"},
		{State: "html-missing-run", Path: "/r/missing", Status: http.StatusNotFound, Want: "Run not found"},
		{State: "html-missing-assessment", Path: "/r/ui-current-deploy?obs=missing", Status: http.StatusNotFound, Want: "Assessment not found"},
	}
}

func serveUIFixture(t *testing.T, suite *uiFixtureSuite) {
	t.Helper()
	type manifestRoute struct {
		Variant string `json:"variant"`
		State   string `json:"state"`
		URL     string `json:"url"`
		Status  int    `json:"status"`
	}
	servers := make([]*http.Server, 0, len(suite.Variants))
	routeN := 0
	for _, variant := range suite.Variants {
		routeN += len(variant.Routes)
	}
	manifest := make([]manifestRoute, 0, routeN)
	for _, variant := range suite.Variants {
		var lc net.ListenConfig
		listener, err := lc.Listen(context.Background(), "tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen for %s fixture: %v", variant.Name, err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		base := fmt.Sprintf("http://localhost:%d", port)
		httpServer := &http.Server{Handler: variant.Server.Handler(), ReadHeaderTimeout: 5 * time.Second}
		servers = append(servers, httpServer)
		for _, route := range variant.Routes {
			manifest = append(manifest, manifestRoute{Variant: variant.Name, State: route.State, URL: base + route.Path, Status: route.Status})
		}
		go func(name string) {
			if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Errorf("serve %s fixture: %v", name, err)
			}
		}(variant.Name)
	}
	t.Cleanup(func() {
		for _, server := range servers {
			if err := server.Close(); err != nil {
				t.Errorf("close fixture server: %v", err)
			}
		}
	})
	sort.Slice(manifest, func(i, j int) bool {
		if manifest[i].Variant != manifest[j].Variant {
			return manifest[i].Variant < manifest[j].Variant
		}
		return manifest[i].State < manifest[j].State
	})
	var body strings.Builder
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		t.Fatalf("marshal route manifest: %v", err)
	}
	t.Logf("local-only test token: %s", testToken)
	t.Logf("UI fixture route manifest:\n%s", body.String())
	select {}
}

// seedUIFixture records a small, realistic register and history. These are
// synthetic examples, not assertions about a deployed farm or platform.
//
//nolint:maintidx // Declarative cross-page fixture inventory is intentionally kept together so shared run IDs and history transitions remain reviewable.
func seedUIFixture(t *testing.T, store *fakeStore) {
	t.Helper()
	now := fixedNow(t)()
	const currentSHA = "bc00000000000000000000000000000000000000000000000000000000000002"
	cases := []struct{ scenario, severity, owner, surface, anchor, title, fix string }{
		{"deploy", "high", "zcp-guidance", "recipe:nodejs", "Start the runtime before deploying", "Deployment guidance skips the first application release", "Name the first deployment step after the runtime becomes ready."},
		{"import", "high", "zcp-tool", "tool:zerops_import", "IMPORT_SERVICE_CONFLICT", "A service-name conflict leaves the agent without a recovery path", "Return the conflicting service and the supported recovery action."},
		{"database", "high", "platform", "tool:zerops_deploy", "Waiting for service readiness", "Database readiness never settles after a successful import", "Inspect service readiness independently of the import result."},
		{"checks", "high", "evaluator", "check:readiness", "expected READY, observed PENDING", "The readiness check grades an incomplete platform response as failure", "Keep unavailable evidence separate from a proven failed check."},
		{"scope", "medium", "agent", "tool:zerops_discover", "project scope is required", "Agent repeats discovery without selecting the requested project", "Use the project returned by the first discovery call."},
		{"env", "medium", "zcp-guidance", "recipe:python", "Configure the connection string", "The connection-string example omits the managed database hostname", "Show the managed service hostname in the setup example."},
		{"port", "medium", "zcp-tool", "tool:zerops_deploy", "runtime port mismatch", "Port validation does not explain which setting to change", "Name the conflicting ports and the configuration field to edit."},
		{"build", "medium", "zcp-guidance", "recipe:go", "build artifacts are required", "The build recipe leaves the output path ambiguous", "Describe the exact build artifact path."},
		{"labels", "low", "scenario", "scenario:static-site", "Check the public page", "The scenario accepts an empty response as a working public page", "Check the expected page content as well as its status."},
		{"history", "low", "zcp-tool", "tool:zerops_discover", "activity still pending", "Completed activity remains labelled pending in the follow-up response", "Use the current activity status in follow-up output."},
	}
	for _, batch := range []struct {
		id, sha string
		age     time.Duration
		n       int
	}{
		{"ui-previous", "af00000000000000000000000000000000000000000000000000000000000001", 48 * time.Hour, 6},
		{"ui-current", currentSHA, 2 * time.Hour, len(cases)},
	} {
		at := now.Add(-batch.age)
		runs := make([]runFixture, 0, batch.n)
		results := map[string]string{}
		for i, c := range cases[:batch.n] {
			id := batch.id + "-" + c.scenario
			verdict := farm.VerdictPassed
			var checks [][5]string
			if i < 4 {
				verdict = farm.VerdictFailed
				checks = [][5]string{{"readiness", "failed", "READY", "PENDING", "service snapshot"}}
			}
			runs = append(runs, runFixture{runID: id, scenario: c.scenario, startedAt: at, durationS: "6m20s", costUsd: 0.28 + float64(i)*0.08, done: true, taskResult: verdict, checks: checks})
			results[id] = verdict
		}
		seedBatchAt(t, store, batch.id, "claude-sonnet-5", at, runs, true, results)
		manifest := farm.BatchManifest{Batch: batch.id, CreatedAt: at.Format(time.RFC3339), StartedAt: at.Format(time.RFC3339), Set: "gate", CandidateSha256: batch.sha, EvaluatorSha256: "fixture-evaluator", Observer: "claude-sonnet-5"}
		for i, c := range cases[:batch.n] {
			id := batch.id + "-" + c.scenario
			manifest.Runs = append(manifest.Runs, farm.ManifestRun{RunID: id, Scenario: c.scenario, ProjectName: "zcp-farm-" + id})
			obs := observer.Observation{
				FormatVersion: observer.ObservationFormat2, RunID: id, ObsID: at.Format("20060102T150405000Z") + "-claude-sonnet-5", Model: "claude-sonnet-5", CreatedAt: at.Add(7 * time.Minute), Status: "ok", Outcome: observer.OutcomeProblem, Headline: c.title,
				Goal: observer.Goal{Reached: "partly", Why: "The run progressed, but the recorded issue remained."}, Checks: observer.Checks{Verdict: results[id], Agree: i != 3},
				Findings: []observer.Finding{{Severity: c.severity, Owner: c.owner, Surface: c.surface, Anchor: c.anchor, Title: c.title, What: "The recorded tool result did not give the agent enough information to finish the next step.", Fix: c.fix, Evidence: []observer.Evidence{{Step: 3, Quote: "discovered ok", Verified: true}}}},
			}
			// These deliberate transitions give /problems every §8.6 status:
			// deploy regresses, import recurs, database is gone, checks remain
			// emitted in raw evidence, scope is unconfirmed, and env is first seen.
			switch {
			case batch.id == "ui-previous" && (c.scenario == "deploy" || c.scenario == "env"):
				obs.Outcome, obs.Headline, obs.Findings = observer.OutcomeOK, "The first deployment completed cleanly", nil
			case batch.id == "ui-current" && (c.scenario == "database" || c.scenario == "checks"):
				obs.Outcome, obs.Headline, obs.Findings = observer.OutcomeOK, "The current run no longer reports this problem", nil
			case batch.id == "ui-current" && c.scenario == "scope":
				obs.Status, obs.Outcome, obs.Headline, obs.Findings = "unparsed", "", "", nil
				obs.Raw = "fixture observer output stopped before valid JSON"
			case batch.id == "ui-current" && c.scenario == "import":
				obs.Findings[0].Evidence = append(obs.Findings[0].Evidence, observer.Evidence{Step: 4, Quote: "a quote that is absent from the cited step", Verified: false})
			}
			seedObservation(t, store, obs)
		}
		store.putJSON(t, fmt.Sprintf("batches/%s/manifest.json", batch.id), manifest)
	}
	checksDir := "runs/ui-current-checks/results/" + testResultsTS + "/checks"
	store.putText(t, checksDir+"/transcript.jsonl", strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"fixture-check","name":"zerops_deploy","input":{"project":"p1"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"fixture-check","content":[{"type":"text","text":"expected READY, observed PENDING"}]}]}}`,
	}, "\n")+"\n")

	// Give the principal run-detail fixture enough real evidence to exercise
	// long transcripts, filtered canonical links, JSON formatting and history.
	deployDir := "runs/ui-current-deploy/results/" + testResultsTS + "/deploy"
	transcript := make([]string, 0, 23)
	transcript = append(transcript,
		`{"type":"user","message":{"content":[{"type":"text","text":"Deploy the application and verify the first release."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"I will inspect the project and service state before deploying."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"fixture-discover","name":"zerops_discover","input":{"project":"p1","duplicate":1,"duplicate":2,"large":9007199254740993}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"fixture-discover","is_error":true,"content":[{"type":"text","text":"discovered ok"}]}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":""}]}}`,
	)
	for i := range 18 {
		transcript = append(transcript, fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"text","text":"Evidence note %02d: verified the next deployment prerequisite and retained the original sequence number."}]}}`, i+1))
	}
	store.putText(t, deployDir+"/transcript.jsonl", strings.Join(transcript, "\n")+"\n")
	deployAt := now.Add(-2 * time.Hour)
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "ui-current-deploy",
		ObsID: deployAt.Add(-10*time.Minute).Format("20060102T150405000Z") + "-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: deployAt.Add(-10 * time.Minute), Status: "error",
		ErrorKind: observer.ErrorKindModel, Error: "fixture provider response ended before an assessment was stored",
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "ui-current-deploy",
		ObsID: deployAt.Add(20*time.Minute).Format("20060102T150405000Z") + "-claude-opus-5",
		Model: "claude-opus-5", CreatedAt: deployAt.Add(20 * time.Minute), Status: "error",
		ErrorKind: observer.ErrorKindModel, Error: "fixture model process exited before returning a complete assessment",
	})

	statesAt := now.Add(-time.Hour)
	stateRuns := []runFixture{
		{runID: "ui-states-failed", scenario: "failed-check", startedAt: statesAt, durationS: "22s", costUsd: 0.11, done: true, taskResult: farm.VerdictFailed, checks: [][5]string{{"deploy/status", "failed", "READY", "FAILED", "service detail"}}},
		{runID: "ui-states-blocked", scenario: "blocked-check", startedAt: statesAt, durationS: "24s", costUsd: 0.10, done: true, taskResult: farm.VerdictBlocked, checks: [][5]string{{"deploy/readiness", "blocked", "READY", "", "service detail"}}},
		{runID: "ui-states-stalled", scenario: "stalled-run", startedAt: statesAt, done: false},
		{runID: "ui-states-not-started", scenario: "not-started", startedAt: statesAt, done: true, neverStarted: true},
		{runID: "ui-states-not-assessed", scenario: "not-assessed", startedAt: statesAt, durationS: "31s", costUsd: 0, done: true, taskResult: farm.VerdictPassed},
		{runID: "ui-states-problem", scenario: "passed-with-problem", startedAt: statesAt, durationS: "48s", costUsd: 0.14, done: true, taskResult: farm.VerdictPassed},
		{runID: "ui-states-clean", scenario: "clean", startedAt: statesAt, durationS: "16s", costUsd: 0.06, done: true, taskResult: farm.VerdictPassed},
		{runID: "ui-states-assessment-error", scenario: "assessment-error", startedAt: statesAt, durationS: "27s", costUsd: 0.08, done: true, taskResult: farm.VerdictPassed},
		{runID: "ui-states-assessment-unparsed", scenario: "assessment-unparsed", startedAt: statesAt, durationS: "29s", costUsd: 0.08, done: true, taskResult: farm.VerdictPassed},
	}
	seedBatchAt(t, store, "ui-states", "claude-sonnet-5", statesAt, stateRuns, true, map[string]string{
		"ui-states-failed": farm.VerdictFailed, "ui-states-blocked": farm.VerdictBlocked,
		"ui-states-stalled": farm.VerdictNotRun, "ui-states-not-started": farm.VerdictNotRun,
		"ui-states-not-assessed": farm.VerdictPassed, "ui-states-problem": farm.VerdictPassed, "ui-states-clean": farm.VerdictPassed,
		"ui-states-assessment-error": farm.VerdictPassed, "ui-states-assessment-unparsed": farm.VerdictPassed,
	})
	statesManifest := farm.BatchManifest{
		Batch: "ui-states", CreatedAt: statesAt.Format(time.RFC3339), StartedAt: statesAt.Format(time.RFC3339), Set: "gate",
		CandidateSha256: currentSHA, EvaluatorSha256: "fixture-evaluator", Observer: "claude-sonnet-5", RunBudgetSec: 60,
	}
	for _, rf := range stateRuns {
		statesManifest.Runs = append(statesManifest.Runs, farm.ManifestRun{RunID: rf.runID, Scenario: rf.scenario, ProjectName: "zcp-farm-" + rf.runID})
	}
	store.putJSON(t, "batches/ui-states/manifest.json", statesManifest)
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "ui-states-problem", ObsID: statesAt.Format("20060102T150405000Z") + "-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: statesAt.Add(time.Minute), Status: "ok", Outcome: observer.OutcomeInconclusive, Headline: "The run passed, but the recovery path stayed unclear",
		Goal: observer.Goal{Reached: "no", Why: "The available evidence does not prove the requested recovery path worked."}, Findings: []observer.Finding{{
			Severity: observer.SeverityMedium, Owner: "zcp-tool", Surface: "tool:zerops_deploy", Title: "Recovery guidance is incomplete",
			What:     "The recovery explanation is too long to scan quickly, especially in a narrow layout where the operator needs the next action before the forensic detail.",
			Evidence: []observer.Evidence{{Step: 3, Quote: "discovered ok", Verified: true}, {Step: 2, Quote: "missing recovery quotation", Verified: false}},
			Fix:      "Lead with the concrete recovery action, then retain the diagnostic evidence below it.",
		}},
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "ui-states-clean", ObsID: statesAt.Format("20060102T150405000Z") + "-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: statesAt.Add(time.Minute), Status: "ok", Outcome: observer.OutcomeOK, Headline: "The run completed without a recorded problem",
		Goal: observer.Goal{Reached: "yes"}, Checks: observer.Checks{Verdict: farm.VerdictPassed, Agree: true},
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "ui-states-assessment-error", ObsID: statesAt.Add(2*time.Minute).Format("20060102T150405000Z") + "-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: statesAt.Add(2 * time.Minute), Status: "error", ErrorKind: observer.ErrorKindModel, Error: "fixture model process exited before storing an assessment",
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "ui-states-assessment-unparsed", ObsID: statesAt.Add(3*time.Minute).Format("20060102T150405000Z") + "-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: statesAt.Add(3 * time.Minute), Status: "unparsed", Raw: "This is deliberately not valid observation JSON.",
	})
	// Keep real transcript evidence but remove usage, producing a partially
	// known batch cost while the run remains meaningful and assessable.
	store.putJSON(t, "runs/ui-states-problem/results/"+testResultsTS+"/passed-with-problem/meta.json", map[string]any{
		"scenarioId": "passed-with-problem", "suiteId": "gate", "mode": "two-shot-resume",
		"startedAt": statesAt.Format(time.RFC3339Nano), "duration": "48s", "evaluatorSha256": "eval-sha", "candidateSha256": "cand-sha",
		"task": map[string]any{"mode": "required", "result": farm.VerdictPassed, "frozenAt": statesAt.Format(time.RFC3339Nano)},
	})

	unknownAt := now.Add(-3 * time.Hour)
	seedBatchAt(t, store, "ui-unknown-cost", "claude-sonnet-5", unknownAt, []runFixture{{
		runID: "ui-unknown-cost-a", scenario: "unknown-cost", startedAt: unknownAt, durationS: "25s", done: true, taskResult: farm.VerdictPassed,
	}}, true, map[string]string{"ui-unknown-cost-a": farm.VerdictPassed})
	store.putJSON(t, "runs/ui-unknown-cost-a/results/"+testResultsTS+"/unknown-cost/meta.json", map[string]any{
		"scenarioId": "unknown-cost", "suiteId": "gate", "mode": "two-shot-resume",
		"startedAt": unknownAt.Format(time.RFC3339Nano), "duration": "25s", "evaluatorSha256": "eval-sha", "candidateSha256": "cand-sha",
		"task": map[string]any{"mode": "required", "result": farm.VerdictPassed, "frozenAt": unknownAt.Format(time.RFC3339Nano)},
	})

	emptyAt := now.Add(-4 * time.Hour)
	seedBatchAt(t, store, "ui-empty", "off", emptyAt, []runFixture{{runID: "ui-empty-a", scenario: "never-started", startedAt: emptyAt, done: true, neverStarted: true}}, true, map[string]string{"ui-empty-a": farm.VerdictNotRun})

	liveAt := now.Add(-5 * time.Minute)
	liveRuns := []runFixture{
		{runID: "ui-live-running-a", scenario: "live-running-a", startedAt: liveAt, done: false},
		{runID: "ui-live-running-b", scenario: "live-running-b", startedAt: liveAt, durationS: "13s", costUsd: 0.03, done: true, taskResult: farm.VerdictPassed},
		{runID: "ui-live-running-c", scenario: "live-running-c", startedAt: liveAt, durationS: "14s", costUsd: 0.03, done: true, taskResult: farm.VerdictPassed},
		{runID: "ui-live-queued", scenario: "live-queued", startedAt: liveAt, durationS: "15s", costUsd: 0.03, done: true, taskResult: farm.VerdictPassed},
		{runID: "ui-live-prestore", scenario: "pre-store-failure", startedAt: liveAt, durationS: "16s", costUsd: 0.03, done: true, taskResult: farm.VerdictPassed},
	}
	seedBatchAt(t, store, "ui-live", "claude-sonnet-5", liveAt, liveRuns, false, nil)
	liveManifest := farm.BatchManifest{Batch: "ui-live", CreatedAt: liveAt.Format(time.RFC3339), StartedAt: liveAt.Format(time.RFC3339), Set: "adhoc", CandidateSha256: currentSHA, EvaluatorSha256: "fixture-evaluator", Observer: "claude-sonnet-5"}
	for _, rf := range liveRuns {
		liveManifest.Runs = append(liveManifest.Runs, farm.ManifestRun{RunID: rf.runID, Scenario: rf.scenario, ProjectName: "zcp-farm-" + rf.runID})
	}
	store.putJSON(t, "batches/ui-live/manifest.json", liveManifest)

	partialAt := now.Add(-30 * time.Minute)
	seedBatchAt(t, store, "ui-partial", "claude-sonnet-5", partialAt, []runFixture{{
		runID: "ui-partial-record", scenario: "partial-record", startedAt: partialAt, durationS: "18s", costUsd: 0.04, done: true, taskResult: farm.VerdictPassed,
	}}, true, map[string]string{"ui-partial-record": farm.VerdictPassed})
	store.putJSON(t, "batches/ui-partial/manifest.json", farm.BatchManifest{
		Batch: "ui-partial", CreatedAt: partialAt.Format(time.RFC3339), StartedAt: partialAt.Format(time.RFC3339), Set: "adhoc",
		CandidateSha256: currentSHA, EvaluatorSha256: "fixture-evaluator", Observer: "claude-sonnet-5",
		Runs: []farm.ManifestRun{{RunID: "ui-partial-record", Scenario: "partial-record", ProjectName: "zcp-farm-ui-partial-record"}},
	})
	store.mu.Lock()
	delete(store.objects, "runs/ui-partial-record/results/"+testResultsTS+"/partial-record/transcript.jsonl")
	store.mu.Unlock()
}
