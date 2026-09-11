package console

import (
	"net/http"
	"net/http/httptest"
	"regexp"
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
	if !strings.Contains(body, "1 of 2 quotes unverified") {
		t.Errorf("body missing the unverified-quote count (FM-46):\n%s", body)
	}
	if !strings.Contains(body, "Tool returned stale data") || !strings.Contains(body, "Agent skipped a sanity check") {
		t.Errorf("body missing finding titles:\n%s", body)
	}
}

// TestPages_RunWithoutDoneNoAssessForm pins item 2: the run page never
// renders the Assess form for a run without done.json — there is nothing
// yet to observe.
func TestPages_RunWithoutDoneNoAssessForm(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "nd4", "claude-sonnet-5", []runFixture{
		{runID: "nd4-a", scenario: "a", startedAt: fixedNow(t)().Add(-time.Minute), done: false},
	}, false, nil)

	rr := doGET(t, h, "/r/nd4-a")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /r/nd4-a: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); strings.Contains(body, `action="/r/nd4-a/observe"`) {
		t.Errorf("run page rendered the Assess form for a run without done.json:\n%s", body)
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

// TestPages_FindingsGroupedAndLinked pins §8.3 FM-51's /findings page:
// every finding of every run in the window, grouped by owner then
// severity, each linking to its run and step. Independent oracle:
// fixtureObservation's own two findings (api_test.go) — owner zcp-tool
// (high, step 3) and owner agent (medium, step 99) — plus api.go's own
// findingItemsFromRows ordering (owner asc, then severity rank), used
// read-only.
func TestPages_FindingsGroupedAndLinked(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "fd1", "claude-sonnet-5", []runFixture{
		{runID: "fd1-scn", scenario: "scn", startedAt: now.Add(-1 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"fd1-scn": "passed"})
	seedObservation(t, store, fixtureObservation("fd1-scn"))

	rr := doGET(t, h, "/findings?since=24h")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /findings: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	if !strings.Contains(body, "agent") || !strings.Contains(body, "zcp-tool") {
		t.Errorf("body missing both owners:\n%s", body)
	}
	if !strings.Contains(body, "Tool returned stale data") || !strings.Contains(body, "Agent skipped a sanity check") {
		t.Errorf("body missing both finding titles:\n%s", body)
	}
	if !strings.Contains(body, `href="/r/fd1-scn#s3"`) {
		t.Errorf("body missing the zcp-tool finding's run+step link (#s3):\n%s", body)
	}
	// owner asc: "agent" sorts before "zcp-tool".
	if i, j := strings.Index(body, "Agent skipped a sanity check"), strings.Index(body, "Tool returned stale data"); i < 0 || j < 0 || i > j {
		t.Errorf("findings not grouped owner-ascending (agent before zcp-tool): agent@%d, zcp-tool@%d\n%s", i, j, body)
	}

	rrFiltered := doGET(t, h, "/findings?since=24h&owner=agent")
	bodyFiltered := rrFiltered.Body.String()
	if !strings.Contains(bodyFiltered, "Agent skipped a sanity check") {
		t.Errorf("owner=agent body missing the agent finding:\n%s", bodyFiltered)
	}
	if strings.Contains(bodyFiltered, "Tool returned stale data") {
		t.Errorf("owner=agent body still shows the zcp-tool finding:\n%s", bodyFiltered)
	}
}

// onAttrPattern matches an on*= event-handler attribute (TestPages_
// NoScriptNoInlineStyleNoHandlers) — a leading space keeps it from matching
// inside ordinary prose text or attribute values that merely contain the
// substring "on=".
var onAttrPattern = regexp.MustCompile(` on[a-z]+=`)

// TestPages_NoScriptNoInlineStyleNoHandlers pins §8.2 FM-50: pages use no
// script, no inline style and no external asset (CSP forbids both) — every
// page's HTML has no "<script", no " style=" and no " on[a-z]+=" attribute.
// Independent oracle: FM-50's own words, checked with a plain regex/substring
// scan over the rendered bytes, never by asking the template package
// whether it thinks its own output is safe.
func TestPages_NoScriptNoInlineStyleNoHandlers(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "ns1", "claude-sonnet-5", []runFixture{
		{runID: "ns1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ns1-scn": "passed"})
	seedObservation(t, store, fixtureObservation("ns1-scn"))

	routes := []string{"/", "/b/ns1", "/r/ns1-scn", "/findings"}
	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			rr := doGET(t, h, route)
			if rr.Code != http.StatusOK {
				t.Fatalf("GET %s: got %d, want 200, body=%s", route, rr.Code, rr.Body.String())
			}
			body := rr.Body.String()
			if strings.Contains(body, "<script") {
				t.Errorf("%s contains <script:\n%s", route, body)
			}
			if strings.Contains(body, " style=") {
				t.Errorf("%s contains inline style=:\n%s", route, body)
			}
			if onAttrPattern.MatchString(body) {
				t.Errorf("%s contains an on*= handler attribute:\n%s", route, body)
			}
		})
	}
}

// TestPages_HostileTextEscaped pins that html/template's auto-escaping is
// actually in effect on the run page: a step whose tool result carries a
// literal "<script>alert(1)</script>" (bucket content is not trusted —
// e.g. an agent tool result that happens to include markup) renders
// escaped, never as live markup. Independent oracle: the exact escaped
// form html/template produces for that literal ("&lt;script&gt;…"), not a
// looser "doesn't look risky" check.
func TestPages_HostileTextEscaped(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	const hostile = `<script>alert(1)</script>`

	seedBatch(t, store, "hx1", "off", []runFixture{
		{runID: "hx1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"hx1-scn": "passed"})

	// Overwrite the seeded transcript with one whose tool result carries
	// the hostile literal, keeping every other fixture file untouched.
	resultsDir := "runs/hx1-scn/results/" + testResultsTS + "/scn"
	hostileTranscript := strings.Join([]string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu1","name":"zerops_discover","input":{}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":[{"type":"text","text":"` + hostile + `"}]}]}}`,
	}, "\n") + "\n"
	store.putText(t, resultsDir+"/transcript.jsonl", hostileTranscript)

	rr := doGET(t, h, "/r/hx1-scn")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /r/hx1-scn: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	if strings.Contains(body, hostile) {
		t.Errorf("body contains the hostile literal unescaped:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("body missing the escaped form of the hostile literal:\n%s", body)
	}
}

// TestPages_StylesheetLinksResolve pins §8.2/§8.3: every page's stylesheet
// link resolves to a served text/css file — found live when every page but
// /login linked a stylesheet the static route never embedded (404, unstyled
// pages) and no test fetched it.
func TestPages_StylesheetLinksResolve(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedBatch(t, store, "ss1", "claude-sonnet-5", []runFixture{
		{runID: "ss1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ss1-scn": "passed"})

	linkRE := regexp.MustCompile(`<link rel="stylesheet" href="([^"]+)"`)
	for _, route := range []string{"/login", "/", "/b/ss1", "/r/ss1-scn", "/findings"} {
		t.Run(route, func(t *testing.T) {
			page := doGET(t, h, route)
			if page.Code != http.StatusOK {
				t.Fatalf("GET %s: got %d, want 200", route, page.Code)
			}
			links := linkRE.FindAllStringSubmatch(page.Body.String(), -1)
			if len(links) == 0 {
				t.Fatalf("GET %s: no stylesheet link", route)
			}
			for _, l := range links {
				css := httptest.NewRecorder()
				h.ServeHTTP(css, httptest.NewRequest(http.MethodGet, l[1], nil))
				if css.Code != http.StatusOK {
					t.Errorf("%s links %s: got %d, want 200", route, l[1], css.Code)
				}
				if ct := css.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
					t.Errorf("%s links %s: Content-Type %q, want text/css", route, l[1], ct)
				}
				if !strings.Contains(css.Body.String(), "prefers-color-scheme: dark") {
					t.Errorf("%s links %s: stylesheet has no dark palette", route, l[1])
				}
			}
		})
	}
}

// TestPages_RunInPageLinksResolve pins §8.3 + FM-46: every in-page link on
// the run page lands on an element — evidence cites step 0 for a
// deterministic check row, so the failed-checks section must answer #s0.
func TestPages_RunInPageLinksResolve(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedBatch(t, store, "il1", "claude-sonnet-5", []runFixture{
		{runID: "il1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true,
			checks: [][5]string{{"decision/x", "failed", "never called", "called", "mcpstream"}}},
	}, true, map[string]string{"il1-scn": "failed"})
	obs := fixtureObservation("il1-scn")
	obs.Findings[0].Evidence = append(obs.Findings[0].Evidence, observer.Evidence{Step: 0, Quote: "decision/x failed", Verified: true})
	store.putJSON(t, "runs/il1-scn/observer/"+obs.ObsID+".json", obs)

	page := doGET(t, h, "/r/il1-scn")
	if page.Code != http.StatusOK {
		t.Fatalf("GET /r/il1-scn: got %d, want 200", page.Code)
	}
	body := page.Body.String()
	hrefs := regexp.MustCompile(`href="#([^"]+)"`).FindAllStringSubmatch(body, -1)
	if len(hrefs) == 0 {
		t.Fatal("run page has no in-page links")
	}
	for _, m := range hrefs {
		if !strings.Contains(body, `id="`+m[1]+`"`) {
			t.Errorf("in-page link #%s has no element with that id", m[1])
		}
	}
}
