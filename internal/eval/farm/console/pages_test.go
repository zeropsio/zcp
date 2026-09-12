package console

import (
	"context"
	"errors"
	"html"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

func TestPages_TermsAnchors_Resolve(t *testing.T) {
	srv, _, _ := testServer(t)
	body := doGET(t, srv.Handler(), "/terms").Body.String()
	for _, id := range []string{"glossary", "verdict", "severity", "cause", "assessment-outcome", "assessment-state", "problem-status"} {
		if strings.Count(body, `href="#`+id+`"`) != 1 || strings.Count(body, `id="`+id+`"`) != 1 {
			t.Errorf("terms anchor %q does not resolve", id)
		}
	}
	for _, label := range []string{"Verdict terms", "Severity terms", "Cause terms", "Assessment outcome terms", "Assessment state terms", "Problem status terms"} {
		want := `<div class="table-wrap" role="region" tabindex="0" aria-label="` + label + `">`
		if !strings.Contains(body, want) {
			t.Errorf("terms table is not a labelled keyboard-scrollable region: %q", label)
		}
	}
}

// TestPages_RequireAuth pins that every §8.3 FM-51 page route requires auth
// like every other HTML route (FM-50): unauthenticated GET redirects 303 to
// /login?next=<its path> (§8.3: "an unauthenticated HTML request is sent
// to /login?next=<its path>"). Independent oracle: the exact status/header
// FM-50 specifies, plus the literal next= rule quoted from §8.3.
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
			want := "/login?next=" + url.QueryEscape(route)
			if loc := rr.Header().Get("Location"); loc != want {
				t.Errorf("GET %s unauthenticated Location: got %q, want %q", route, loc, want)
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

// TestPages_BatchRowShowsObserverFailedNotAssessed pins item 3: on the
// batch page, a run whose current observation failed shows "observer
// failed" instead of a headline, is not counted in "assessed", and is
// counted in the Assess callout.
func TestPages_BatchRowShowsObserverFailedNotAssessed(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "ef2", "claude-sonnet-5", []runFixture{
		{runID: "ef2-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ef2-scn": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "ef2-scn", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: fixedNow(t)(), Status: "error", Error: "boom",
	})

	rr := doGET(t, h, "/b/ef2")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /b/ef2: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	if !strings.Contains(body, "observer failed") {
		t.Errorf("body missing \"observer failed\":\n%s", body)
	}
	if !strings.Contains(body, "0/1</strong> assessed") {
		t.Errorf("body counts the failed observation as assessed, want 0/1 assessed:\n%s", body)
	}
	if !strings.Contains(body, "1 finished run") {
		t.Errorf("body missing the Assess callout counting the failed run as unassessed:\n%s", body)
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
			var styles strings.Builder
			for _, l := range links {
				css := httptest.NewRecorder()
				h.ServeHTTP(css, httptest.NewRequest(http.MethodGet, l[1], nil))
				if css.Code != http.StatusOK {
					t.Errorf("%s links %s: got %d, want 200", route, l[1], css.Code)
				}
				if ct := css.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
					t.Errorf("%s links %s: Content-Type %q, want text/css", route, l[1], ct)
				}
				styles.WriteString(css.Body.String())
			}
			if !strings.Contains(styles.String(), "prefers-color-scheme: dark") {
				t.Errorf("%s linked styles have no system-dark palette", route)
			}
		})
	}
}

// TestPages_TopNavCurrentItemMarked pins §8.3 FM-51's top nav: Overview ·
// Problems · Findings · Terms, with aria-current="page" on exactly the
// current one — a batch/run detail page is under none of them.
func TestPages_TopNavCurrentItemMarked(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedBatch(t, store, "nv1", "off", []runFixture{
		{runID: "nv1-scn", scenario: "scn", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"nv1-scn": "passed"})

	cases := []struct {
		route   string
		current string // the nav link text that must carry aria-current, "" for none
	}{
		{"/", "Overview"},
		{"/findings", "Findings"},
		{"/terms", "Terms"},
		{"/b/nv1", ""},
		{"/r/nv1-scn", ""},
	}
	navLinks := []string{"Overview", "Problems", "Findings", "Terms"}
	for _, c := range cases {
		t.Run(c.route, func(t *testing.T) {
			body := doGET(t, h, c.route).Body.String()
			for _, link := range navLinks {
				marked := false
				for _, m := range regexp.MustCompile(`(?s)<a[^>]*aria-current="page"[^>]*>(.*?)</a>`).FindAllStringSubmatch(body, -1) {
					if regexp.MustCompile(`<[^>]*>`).ReplaceAllString(m[1], "") == link {
						marked = true
					}
				}
				want := link == c.current
				if marked != want {
					t.Errorf("GET %s: nav item %q aria-current = %v, want %v\n%s", c.route, link, marked, want, body)
				}
			}
		})
	}
}

// TestPages_ObserverStatusLineByConfig pins §8.3 FM-51's observer status
// line for each of its documented states — on (with the queue's live
// counts), off (kill switch), and unavailable (credential missing, claude
// unresolved) — the last two hiding every Assess form's availability
// (pageMeta.Observer.Hidden).
func TestPages_ObserverStatusLineByConfig(t *testing.T) {
	cases := []struct {
		name       string
		cfg        func(cfg *Config)
		wantSubstr string
		wantHidden bool
	}{
		{"on, idle queue", func(cfg *Config) { cfg.Queue = NewQueue(func(context.Context, Job) error { return nil }) },
			"Automatic assessment: on · claude-sonnet-5 · 0 queued/running", false},
		{"off (kill switch)", func(cfg *Config) { cfg.ObserverDisabled = true },
			"Automatic assessment: off on this console (ZCP_FARM_OBSERVER=off) — Assess buttons still work", false},
		{"credential missing", func(cfg *Config) { cfg.ObserverCredentialMissing = true },
			"Automatic assessment: unavailable — CLAUDE_CODE_OAUTH_TOKEN is not set on the console service", true},
		{"api key set", func(cfg *Config) { cfg.ObserverAPIKeySet = true },
			"Automatic assessment: unavailable — ANTHROPIC_API_KEY is set on the console service; the observer runs only under the OAuth token", true},
		{"claude unresolved", func(cfg *Config) { cfg.ObserverClaudePathUnresolved = true },
			"Automatic assessment: unavailable — claude not found", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := newFakeStore()
			cfg := Config{Store: store, Token: testToken, Now: fixedNow(t)}
			c.cfg(&cfg)
			srv := NewServer(cfg)

			body := doGET(t, srv.Handler(), "/").Body.String()
			if !strings.Contains(body, c.wantSubstr) {
				t.Errorf("status line missing %q:\n%s", c.wantSubstr, body)
			}
			if got := srv.observerStatusLine().Hidden; got != c.wantHidden {
				t.Errorf("observerStatusLine().Hidden = %v, want %v", got, c.wantHidden)
			}
		})
	}
}

// TestPages_ObserverStatusLineCountsQueueActivity pins that the "on" status
// line's queued/running count reflects the queue's live Stats, not a
// static zero.
func TestPages_ObserverStatusLineCountsQueueActivity(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	q := NewQueue(func(ctx context.Context, job Job) error {
		<-release
		return nil
	})
	store := newFakeStore()
	srv := NewServer(Config{Store: store, Token: testToken, Now: fixedNow(t), Queue: q})

	if err := q.Enqueue(context.Background(), Job{RunID: "r1", Batch: "b1"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if !wkEventually(t, func() bool {
		_, running := q.Stats()
		return running == 1
	}) {
		t.Fatal("job never started running")
	}

	body := doGET(t, srv.Handler(), "/").Body.String()
	if !strings.Contains(body, "Automatic assessment: on · claude-sonnet-5 · 1 queued/running") {
		t.Errorf("status line does not reflect the in-flight job:\n%s", body)
	}
}

// TestPages_RefreshMetaWhileJobInFlight pins §8.5 FM-53: "while any job of
// the page is in flight, the page carries <meta http-equiv=refresh
// content=20>" — for a batch page, any run of that batch; for a run page,
// that run itself; neither once the queue is idle.
func TestPages_RefreshMetaWhileJobInFlight(t *testing.T) {
	const refreshTag = `<meta http-equiv="refresh" content="20">`

	release := make(chan struct{})
	q := NewQueue(func(ctx context.Context, job Job) error {
		<-release
		return nil
	})
	store := newFakeStore()
	srv := NewServer(Config{Store: store, Token: testToken, Now: fixedNow(t), Queue: q})
	seedBatch(t, store, "rf1", "off", []runFixture{
		{runID: "rf1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"rf1-a": "passed"})

	if strings.Contains(doGET(t, srv.Handler(), "/b/rf1").Body.String(), refreshTag) {
		t.Error("batch page carries the refresh tag with an idle queue")
	}
	if strings.Contains(doGET(t, srv.Handler(), "/r/rf1-a").Body.String(), refreshTag) {
		t.Error("run page carries the refresh tag with an idle queue")
	}

	if err := q.Enqueue(context.Background(), Job{RunID: "rf1-a", Batch: "rf1"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if !wkEventually(t, func() bool { return q.State("rf1-a") != "" }) {
		t.Fatal("job never showed as queued/running")
	}

	if !strings.Contains(doGET(t, srv.Handler(), "/b/rf1").Body.String(), refreshTag) {
		t.Error("batch page missing the refresh tag while its run is in flight")
	}
	if !strings.Contains(doGET(t, srv.Handler(), "/r/rf1-a").Body.String(), refreshTag) {
		t.Error("run page missing the refresh tag while it is in flight")
	}

	close(release)
}

// TestPages_NoticeCodesRendered pins §8.3/§8.5's ?notice=<code> callout:
// each documented code renders its own text, "queued" honors ?n=, and an
// unrecognized code renders nothing (§8.3: "an unknown code renders
// nothing").
func TestPages_NoticeCodesRendered(t *testing.T) {
	cases := []struct {
		query   string
		want    string // "" means: no callout at all
		notWant string
	}{
		{"?notice=queued&n=3", "Queued 3 runs for assessment.", ""},
		{"?notice=queued&n=1", "Queued 1 run for assessment.", ""},
		{"?notice=queued&n=0", "Nothing needed an assessment — every finished run already has one.", ""},
		{"?notice=busy", "Already queued or running", ""},
		{"?notice=not-finished", "hasn't finished yet", ""},
		{"?notice=bad-model", "isn't one of the ones this console supports", ""},
		{"?notice=unavailable", "isn't available right now", ""},
		{"?notice=made-up-code", "", `class="callout notice"`},
	}
	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			srv, _, _ := testServer(t)
			body := html.UnescapeString(doGET(t, srv.Handler(), "/"+c.query).Body.String())
			if c.want != "" && !strings.Contains(body, c.want) {
				t.Errorf("body missing notice text %q:\n%s", c.want, body)
			}
			if c.notWant != "" && strings.Contains(body, c.notWant) {
				t.Errorf("body renders a callout for an unknown notice code:\n%s", body)
			}
		})
	}
}

// FM-51: a single labelled primary navigation and skip target remain usable
// on every page, including detail routes which are not primary navigation.
func TestPages_SharedShell_AccessibleNavigation(t *testing.T) {
	t.Parallel()
	srv, store, _ := testServer(t)
	seedBatch(t, store, "shell", "off", []runFixture{{runID: "shell-run", scenario: "shell", startedAt: fixedNow(t)(), costUsd: 0.1, done: true, taskResult: "passed"}}, true, map[string]string{"shell-run": "passed"})
	for _, tc := range []struct {
		path         string
		primaryCount int
	}{
		{"/", 1}, {"/problems", 1}, {"/findings", 1}, {"/terms", 1}, {"/b/shell", 0}, {"/r/shell-run", 0},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			rr := doGET(t, srv.Handler(), tc.path)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rr.Code)
			}
			body := rr.Body.String()
			for _, want := range []string{`href="#main-content"`, `id="main-content"`, `aria-label="Primary navigation"`, `href="/static/vendor/tabler-1.5.1.min.css"`} {
				if !strings.Contains(body, want) {
					t.Errorf("shell missing %q", want)
				}
			}
			if got := strings.Count(body, `aria-current="page"`); got != tc.primaryCount {
				t.Errorf("current primary pages = %d, want %d", got, tc.primaryCount)
			}
			if got := len(regexp.MustCompile(`<h1(?:>|\s)`).FindAllString(body, -1)); got != 1 {
				t.Errorf("H1 count = %d, want 1", got)
			}
		})
	}
}

func TestPages_AutoRefreshPauseIsHTMLOnlyAndPreserved(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	queue := NewQueue(func(context.Context, Job) error { <-release; return nil })
	store := newFakeStore()
	srv := NewServer(Config{Store: store, Token: testToken, Now: fixedNow(t), Queue: queue})
	seedBatch(t, store, "refresh-pref", "claude-sonnet-5", []runFixture{{runID: "refresh-pref-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", done: true}}, true, map[string]string{"refresh-pref-a": "passed"})
	if err := queue.Enqueue(context.Background(), Job{RunID: "refresh-pref-a", Batch: "refresh-pref"}); err != nil {
		t.Fatal(err)
	}
	if !wkEventually(t, func() bool { return queue.State("refresh-pref-a") != "" }) {
		t.Fatal("job did not start")
	}

	auto := doGET(t, srv.Handler(), "/b/refresh-pref")
	if !strings.Contains(auto.Body.String(), `<meta http-equiv="refresh"`) || !strings.Contains(auto.Body.String(), "Pause automatic updates") {
		t.Fatalf("automatic page lacks refresh and pause control: %s", auto.Body.String())
	}
	paused := doGET(t, srv.Handler(), "/b/refresh-pref?refresh=off&sort=scenario")
	body := paused.Body.String()
	if strings.Contains(body, `<meta http-equiv="refresh"`) || !strings.Contains(body, "Automatic updates paused") || !strings.Contains(body, `refresh=off`) {
		t.Fatalf("paused page is not stable: %s", body)
	}
	if !strings.Contains(body, `href="/b/refresh-pref?refresh=off&amp;sort=scenario">Refresh now`) {
		t.Errorf("Refresh now did not retain the preference and list state: %s", body)
	}
	if !strings.Contains(body, `href="/b/refresh-pref?sort=scenario">Resume automatic updates`) {
		t.Errorf("Resume did not remove only the refresh preference: %s", body)
	}
	if strings.Contains(body, `href="/static/app.css?refresh=off"`) || !strings.Contains(body, `href="/static/app.css"`) {
		t.Errorf("stylesheet URL was mutated: %s", body)
	}
	for _, path := range []string{"/?refresh=on", "/problems?refresh=on", "/findings?refresh=on", "/terms?refresh=on", "/b/refresh-pref?refresh=on", "/r/refresh-pref-a?refresh=on", "/terms?refresh=", "/terms?refresh=off&refresh=off"} {
		if got := doGET(t, srv.Handler(), path).Code; got != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400", path, got)
		}
	}
	brokenSrv, brokenStore, _ := testServer(t)
	brokenStore.failListOn("batches/", errors.New("store should not be reached"))
	if got := doGET(t, brokenSrv.Handler(), "/?refresh=on").Code; got != http.StatusBadRequest {
		t.Errorf("invalid Overview query during store outage = %d, want 400 before store I/O", got)
	}
	if got := doGET(t, srv.Handler(), "/api/batches.json?refresh=off").Code; got != http.StatusBadRequest {
		t.Fatalf("API accepted HTML-only refresh parameter: %d", got)
	}

	for _, raw := range []string{"/", "/problems", "/findings?since=7d", "/terms", "/b/refresh-pref", "/r/refresh-pref-a#s3"} {
		got := pageURL(raw, pageMeta{KeepRefresh: true})
		if !strings.Contains(got, "refresh=off") {
			t.Errorf("pageURL(%q) lost preference: %q", raw, got)
		}
	}
	for _, raw := range []string{"#s3", "/api/runs.md", "/static/app.css", "/r/refresh-pref-a/observe", "https://example.com/"} {
		if got := pageURL(raw, pageMeta{KeepRefresh: true}); got != raw {
			t.Errorf("pageURL(%q) = %q", raw, got)
		}
	}

	metaRequest := httptest.NewRequest(http.MethodGet, "/r/refresh-pref-a?refresh=off&obs=old-id&steps=errors&notice=queued&n=4", nil)
	meta := srv.pageMeta(metaRequest, "Run", "", true)
	if meta.RefreshNowURL != "/r/refresh-pref-a?obs=old-id&refresh=off&steps=errors" {
		t.Errorf("RefreshNowURL = %q", meta.RefreshNowURL)
	}
	if meta.ResumeURL != "/r/refresh-pref-a?obs=old-id&steps=errors" {
		t.Errorf("ResumeURL = %q", meta.ResumeURL)
	}

	for _, path := range []string{"/?refresh=off", "/problems?refresh=off", "/findings?refresh=off", "/terms?refresh=off", "/b/refresh-pref?refresh=off", "/r/refresh-pref-a?refresh=off"} {
		rendered := doGET(t, srv.Handler(), path).Body.String()
		for _, want := range []string{`href="/?refresh=off"`, `href="/problems?refresh=off"`, `href="/findings?refresh=off&amp;since=7d"`, `href="/terms?refresh=off"`} {
			if !strings.Contains(rendered, want) {
				t.Errorf("%s lost refresh preference on shell link %s", path, want)
			}
		}
	}
	problems := doGET(t, srv.Handler(), "/problems?refresh=off").Body.String()
	if !strings.Contains(problems, `<input type="hidden" name="refresh" value="off">`) {
		t.Errorf("GET filters do not submit the refresh preference: %s", problems)
	}
}

func TestPages_StackedTablesRetainAccessibleHeaders(t *testing.T) {
	srv, store, _ := testServer(t)
	seedBatch(t, store, "table-a11y", "off", []runFixture{{runID: "table-a11y-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", done: true, taskResult: "failed", checks: [][5]string{{"check/a", "failed", "yes", "no", "result"}}}}, true, map[string]string{"table-a11y-a": "failed"})
	for _, path := range []string{"/", "/b/table-a11y", "/r/table-a11y-a"} {
		body := doGET(t, srv.Handler(), path).Body.String()
		if !strings.Contains(body, `scope="col"`) || !strings.Contains(body, ` headers="`) || !strings.Contains(body, `class="mobile-cell-label" aria-hidden="true"`) {
			t.Errorf("%s lacks explicit stacked-table associations: %s", path, body)
		}
	}
	css := string(appCSS)
	if strings.Contains(css, "table.stack thead { display: none") || strings.Contains(css, "content: attr(data-label)") {
		t.Errorf("stacked tables still depend on hidden headers/generated labels")
	}
}

func TestPages_DisclosureSummariesContainNoNestedInteractiveControls(t *testing.T) {
	for _, name := range []string{"run.html", "batch.html", "problems.html", "findings.html", "lists.html"} {
		raw, err := pagesHTMLSrc.ReadFile("assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for _, summary := range regexp.MustCompile(`(?s)<summary(?:\s[^>]*)?>.*?</summary>`).FindAll(raw, -1) {
			if regexp.MustCompile(`<(?:a|button|input|select|textarea|form)\b`).Match(summary) {
				t.Errorf("%s has an interactive control inside summary: %s", name, summary)
			}
		}
	}
	runTemplate, err := pagesHTMLSrc.ReadFile("assets/run.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(runTemplate), `class="step-disclosure-indicator" aria-hidden="true"`) {
		t.Fatal("step disclosures lack a visible, non-interactive affordance")
	}
}

func TestPages_SortControlsExposeCurrentAndNextDirection(t *testing.T) {
	spec := batchListSpec()
	q, err := Parse(spec, url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	nav := buildListNav("/", spec, q, url.Values{}, nil, homeBatchSortLabels, homeBatchLabeler())
	for _, sort := range nav.Sorts {
		if !strings.Contains(sort.AccessibleName, "activate to sort") {
			t.Errorf("sort %s lacks next direction: %+v", sort.Key, sort)
		}
		if sort.Active && !strings.Contains(sort.AccessibleName, "sorted ") {
			t.Errorf("active sort lacks current direction: %+v", sort)
		}
	}
	for _, name := range []string{"home.html", "batch.html", "problems.html", "findings.html", "lists.html"} {
		raw, err := pagesHTMLSrc.ReadFile("assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), ".AccessibleName") && !strings.Contains(string(raw), `aria-label="{{.AccessibleName}}"`) {
			t.Errorf("%s does not expose centralized sort name", name)
		}
	}
}

func TestAppCSS_MobileNavigationOrderMatchesDOM(t *testing.T) {
	layout, err := pagesHTMLSrc.ReadFile("assets/layout.html")
	if err != nil {
		t.Fatal(err)
	}
	raw := string(layout)
	if strings.Index(raw, "farm-brand") >= strings.Index(raw, "navbar-nav") || strings.Index(raw, "navbar-nav") >= strings.Index(raw, "sidebar-footer") {
		t.Fatal("shell DOM is not brand, primary navigation, sign out")
	}
	css := string(appCSS)
	if strings.Contains(css, ".sidebar-footer { position: absolute") {
		t.Fatal("mobile sign out is absolutely relocated out of DOM order")
	}
}

func TestAppCSS_LightInteractiveTextContrast(t *testing.T) {
	css := string(appCSS)
	if !strings.Contains(css, "--accent: #2a63d1") {
		t.Fatal("light accent is not the measured #2a63d1 token")
	}
	for _, pair := range [][2]string{
		{"#2a63d1", "#ffffff"},
		{"#2a63d1", "#f5f6f8"},
		{"#2a63d1", "#f0f2f5"},
		{"#2a63d1", "#fbe6e4"},
		{"#2a63d1", "#e5ecfb"},
		{"#2a63d1", "#fdf1d3"},
		{"#2a63d1", "#eceef2"},
		{"#1e7a3c", "#e3f5e9"},
		{"#b3261e", "#fbe6e4"},
		{"#8a6200", "#fdf1d3"},
		{"#2a4fb3", "#e5ecfb"},
		{"#5d6470", "#eceef2"},
		{"#a9c1ff", "#1d2742"},
		{"#ff9a92", "#3b1c1b"},
		{"#f0c75e", "#3a2f12"},
		{"#7aa7ff", "#171a20"},
		{"#e7e9ee", "#1e222a"},
	} {
		if ratio := testContrastRatio(pair[0], pair[1]); ratio < 4.5 {
			t.Errorf("contrast %s on %s = %.2f", pair[0], pair[1], ratio)
		}
	}
	for _, selector := range []string{".farm-login .btn-primary, .farm-login .btn-primary:hover, .farm-login .btn-primary:focus, .farm-login .btn-primary:active { color: var(--on-accent); background-color: var(--accent); border-color: var(--accent); }", ".farm-console pre, .farm-console code { color: var(--fg); }", ".farm-console .table th, .farm-console .table th a { color: var(--fg); }", ".farm-console .alert-info { color: var(--info); background: var(--info-bg); }", ".farm-console .alert-warning { color: var(--blocked); background: var(--blocked-bg); }", ".farm-console .alert-danger { color: var(--failed); background: var(--failed-bg); }", ".farm-console a.table-primary { color: var(--accent); }", ".farm-console .skip-link, .farm-console .skip-link:focus { color: var(--accent); background: var(--surface); }"} {
		if !strings.Contains(css, selector) {
			t.Errorf("missing vendor-resistant foreground override %q", selector)
		}
	}
	if ratio := testContrastRatio("#2a63d1", "#f5f6f8"); ratio < 3 {
		t.Errorf("focus outline contrast = %.2f", ratio)
	}
}

func testContrastRatio(fg, bg string) float64 {
	lum := func(hex string) float64 {
		var rgb [3]uint64
		for i := range rgb {
			rgb[i], _ = strconv.ParseUint(hex[1+i*2:3+i*2], 16, 8)
		}
		channel := func(v uint64) float64 {
			x := float64(v) / 255
			if x <= .04045 {
				return x / 12.92
			}
			return math.Pow((x+.055)/1.055, 2.4)
		}
		return .2126*channel(rgb[0]) + .7152*channel(rgb[1]) + .0722*channel(rgb[2])
	}
	a, b := lum(fg), lum(bg)
	if a < b {
		a, b = b, a
	}
	return (a + .05) / (b + .05)
}
