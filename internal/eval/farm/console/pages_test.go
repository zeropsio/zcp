package console

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// TestPages_RequireAuth pins that every §8.3 FM-51 page route requires auth
// like every other HTML route (FM-50): unauthenticated GET redirects 303 to
// /login. Independent oracle: the exact status/header FM-50 specifies.
func TestPages_RequireAuth(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedBatch(t, store, "pb1", "off", []runFixture{
		{runID: "pb1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"pb1-scn": "passed"})

	routes := []string{"/", "/b/pb1", "/r/pb1-scn", "/findings"}
	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, route, nil))
			if rr.Code != http.StatusSeeOther {
				t.Errorf("GET %s unauthenticated: got %d, want 303", route, rr.Code)
			}
			if loc := rr.Header().Get("Location"); loc != "/login" {
				t.Errorf("GET %s unauthenticated Location: got %q, want /login", route, loc)
			}
		})
	}
}

// TestPages_BatchRowsShowHeadlineOrObserverState pins §8.3 FM-51's
// /b/<batch> row: the current observation's headline when there is one,
// else RunRow.ObserverState verbatim — "not observed" for a done run with
// no observation, "observer off" for a batch whose manifest names no
// model. Independent oracle: the exact vocabulary strings §8.4 FM-52 names
// (view.go's observerStateNotObserved/observerStateOff), not recomputed.
func TestPages_BatchRowsShowHeadlineOrObserverState(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "hb1", "claude-sonnet-5", []runFixture{
		{runID: "hb1-observed", scenario: "observed", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "hb1-pending", scenario: "pending", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"hb1-observed": "passed", "hb1-pending": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "hb1-observed", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: fixedNow(t)(), Status: "ok", Headline: "All good here",
	})

	seedBatch(t, store, "hb2", "off", []runFixture{
		{runID: "hb2-off", scenario: "off-scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"hb2-off": "passed"})

	rr1 := doGET(t, h, "/b/hb1")
	if rr1.Code != http.StatusOK {
		t.Fatalf("GET /b/hb1: got %d, want 200, body=%s", rr1.Code, rr1.Body.String())
	}
	body1 := rr1.Body.String()
	if !strings.Contains(body1, "All good here") {
		t.Errorf("/b/hb1 body missing observed headline %q:\n%s", "All good here", body1)
	}
	if !strings.Contains(body1, "not observed") {
		t.Errorf("/b/hb1 body missing %q for the pending run:\n%s", "not observed", body1)
	}

	rr2 := doGET(t, h, "/b/hb2")
	if rr2.Code != http.StatusOK {
		t.Fatalf("GET /b/hb2: got %d, want 200, body=%s", rr2.Code, rr2.Body.String())
	}
	if body2 := rr2.Body.String(); !strings.Contains(body2, "observer off") {
		t.Errorf("/b/hb2 body missing %q:\n%s", "observer off", body2)
	}
}

// TestPages_RunShowsObservationEvidenceLinksAndMarks pins §8.3 FM-51's
// /r/<runId> observer block: findings with an evidence link to "#s<n>" and
// a verified/unverified mark, plus the unverified-quote count (§7.6 FM-46:
// "every surface that shows an observation shows its unverified-quote
// count"). Independent oracle: fixtureObservation's own hand-authored
// evidence (api_test.go) — step 3 verified, step 99 unverified.
func TestPages_RunShowsObservationEvidenceLinksAndMarks(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "rb1", "claude-sonnet-5", []runFixture{
		{runID: "rb1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"rb1-scn": "passed"})
	seedObservation(t, store, fixtureObservation("rb1-scn"))

	rr := doGET(t, h, "/r/rb1-scn")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /r/rb1-scn: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	if !strings.Contains(body, `href="#s3"`) {
		t.Errorf("body missing evidence link to #s3:\n%s", body)
	}
	if !strings.Contains(body, "verified") {
		t.Errorf("body missing a 'verified' mark:\n%s", body)
	}
	if !strings.Contains(body, "unverified") {
		t.Errorf("body missing an 'unverified' mark:\n%s", body)
	}
	if !strings.Contains(body, "unverified quotes: 1") {
		t.Errorf("body missing the unverified-quote count (FM-46):\n%s", body)
	}
	if !strings.Contains(body, "Tool returned stale data") || !strings.Contains(body, "Agent skipped a sanity check") {
		t.Errorf("body missing finding titles:\n%s", body)
	}
}

// TestPages_RunListsOlderObservationVersions pins §8.3 FM-51's "older
// observation versions" section: every obsId older than the current one
// (§7.5 FM-45: newest obsId is current, older ones stay listed) is shown,
// not just the current. Independent oracle: two hand-seeded obsIds whose
// UTC-timestamp prefix orders them predictably (§7.5's obsId grammar).
func TestPages_RunListsOlderObservationVersions(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "ov1", "claude-sonnet-5", []runFixture{
		{runID: "ov1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ov1-scn": "passed"})

	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "ov1-scn", ObsID: "20260910T090000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC), Status: "ok", Headline: "first pass, stale",
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "ov1-scn", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: fixedNow(t)(), Status: "ok", Headline: "current headline",
	})

	rr := doGET(t, h, "/r/ov1-scn")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /r/ov1-scn: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	if !strings.Contains(body, "current headline") {
		t.Errorf("body missing the current observation's headline:\n%s", body)
	}
	if !strings.Contains(body, "20260910T090000000Z-claude-sonnet-5") {
		t.Errorf("body missing the older obsId:\n%s", body)
	}
	if !strings.Contains(body, "first pass, stale") {
		t.Errorf("body missing the older observation's own headline:\n%s", body)
	}
	// The current obsId appears exactly once (the observer block's "obs:"
	// line) — never again under "older observation versions", which only
	// lists row.OlderObsIDs (view.go: every obsId but the newest).
	if n := strings.Count(body, "20260911T120000000Z-claude-sonnet-5"); n != 1 {
		t.Errorf("current obsId appears %d times, want exactly 1 (not also under older versions):\n%s", n, body)
	}
}

// TestPages_RunStepsHaveAnchorsAndFullText pins §8.3 FM-51's step list:
// every step with anchor "s<n>", collapsible, full input and result — no
// truncation (unlike the observer's own digest, §7.3). Independent oracle:
// fixtureTranscript's own known shape (console_test.go) — #1 user (the
// task prompt), #2 agent text "looking into it", #3 tool zerops_discover
// with input {"project":"p1"} and result "discovered ok".
func TestPages_RunStepsHaveAnchorsAndFullText(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "st1", "off", []runFixture{
		{runID: "st1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"st1-scn": "passed"})

	rr := doGET(t, h, "/r/st1-scn")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /r/st1-scn: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	for _, want := range []string{`id="s1"`, `id="s2"`, `id="s3"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing step anchor %s:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "do the thing for scn") {
		t.Errorf("body missing step 1's full task-prompt text:\n%s", body)
	}
	if !strings.Contains(body, "looking into it") {
		t.Errorf("body missing step 2's agent text:\n%s", body)
	}
	if !strings.Contains(body, "zerops_discover") || !strings.Contains(body, `{&#34;project&#34;:&#34;p1&#34;}`) {
		t.Errorf("body missing step 3's full tool input:\n%s", body)
	}
	if !strings.Contains(body, "discovered ok") {
		t.Errorf("body missing step 3's full tool result:\n%s", body)
	}
	if !strings.Contains(body, "<details") {
		t.Errorf("body has no <details> — steps must be collapsible:\n%s", body)
	}
}

// TestPages_RunShowsForensicCommand pins §8.3 FM-51's exact local forensic
// command: "zcp eval farm pull <runId> --out <dir>" then
// "zcp capture ui <dir>/<runId>/capture". Independent oracle: the spec's
// own literal command text, with the fixture's runId substituted.
func TestPages_RunShowsForensicCommand(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "fc1", "off", []runFixture{
		{runID: "fc1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"fc1-scn": "passed"})

	rr := doGET(t, h, "/r/fc1-scn")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /r/fc1-scn: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	if !strings.Contains(body, "zcp eval farm pull fc1-scn --out") {
		t.Errorf("body missing the pull command:\n%s", body)
	}
	if !strings.Contains(body, "zcp capture ui") || !strings.Contains(body, "fc1-scn/capture") {
		t.Errorf("body missing the capture-ui command:\n%s", body)
	}
}
