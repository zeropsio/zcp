// Package console: RED/GREEN tests for GET /problems (docs/spec-eval-
// farm.md §8.3 FM-51's problems page, over §8.6's problem clustering and
// §8.7's list engine — both already implemented in problems.go). These
// tests pin the page's own wiring (route, query parsing, rendering), not
// BuildProblems' clustering rules, which problems_test.go already covers
// exhaustively.
package console

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// seedFormat2Finding stores a format-2 observation for runID with one
// finding built from the given fields — the shape §8.6 clustering needs
// (Surface/Anchor), which fixtureObservation (api_test.go, format 1) never
// carries.
func seedFormat2Finding(t *testing.T, store *fakeStore, runID string, now time.Time, severity, surface, anchor, title, fix string, step int, quote string) {
	t.Helper()
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: runID,
		ObsID: "20260911T120000000Z-claude-sonnet-5", Model: "claude-sonnet-5",
		CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: title,
		Goal: observer.Goal{Reached: "yes"}, Checks: observer.Checks{Verdict: "passed", Agree: true},
		Findings: []observer.Finding{{
			Severity: severity, Owner: "zcp-tool", Surface: surface, Anchor: anchor,
			Title: title, What: "it went wrong", Fix: fix, LookAt: "internal/x.go",
			Evidence: []observer.Evidence{{Step: step, Quote: quote, Verified: true}},
		}},
		SelfReview: observer.SelfReview{Accurate: "yes"},
	})
}

// TestPages_ProblemsPageRendersClusteredRow pins §8.3's /problems page: a
// format-2 finding renders as one problem row (severity, cause, surface
// prefix, title, anchor, status) that expands via <details> to its member
// (run link, batch, build, severity, cause, title, first found quote with
// its step link).
func TestPages_ProblemsPageRendersClusteredRow(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "pr1", "claude-sonnet-5", []runFixture{
		{runID: "pr1-scn", scenario: "scn", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"pr1-scn": "passed"})
	seedFormat2Finding(t, store, "pr1-scn", now, observer.SeverityHigh,
		"tool:zerops_deploy/deploy", "ZCP_LAUNCH_TOKEN", "Deploy leaks the launch token", "redact it",
		3, "token leaked here")

	rr := doGET(t, h, "/problems")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /problems: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	for _, want := range []string{
		"Deploy leaks the launch token", // title
		"redact it",                     // fix
		"tool:zerops_deploy",            // surface prefix
		"ZCP_LAUNCH_TOKEN",              // anchor
		"Members (1)",
		`href="/r/pr1-scn"`,
		`href="/r/pr1-scn#s3"`,
		"token leaked here",
		"<details",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}

// TestPages_ProblemsRowIsCompact pins item 8 (FIX2): a row's Last/First
// seen dates use the Overview's own short, non-wrapping form (not the long
// "2 Jan 2006, 15:04 UTC" one, which wraps and inflates row height on a
// stacked table), the Runs-hit cell's totals render on one line (no <br>),
// and a problem's members render inside its own row rather than a second
// <tr> (which doubles the stacked-card count per problem on a phone).
func TestPages_ProblemsRowIsCompact(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "cpt1", "claude-sonnet-5", []runFixture{
		{runID: "cpt1-a", scenario: "a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"cpt1-a": "passed"})
	seedFormat2Finding(t, store, "cpt1-a", now, observer.SeverityHigh,
		"tool:zerops_deploy/deploy", "COMPACT_ANCHOR", "Compact row problem", "fix it",
		1, "quote here")

	body := doGET(t, h, "/problems").Body.String()
	if !strings.Contains(body, fmtTimeShort(now)) {
		t.Errorf("body missing the short-form date %q:\n%s", fmtTimeShort(now), body)
	}
	if strings.Contains(body, fmtTime(now)) {
		t.Errorf("body still shows the long-form date %q:\n%s", fmtTime(now), body)
	}
	if i := strings.Index(body, `data-label="Runs hit"`); i >= 0 {
		end := strings.Index(body[i:], "</td>")
		if end < 0 {
			t.Fatalf("Runs-hit cell never closes:\n%s", body)
		}
		if strings.Contains(body[i:i+end], "<br>") {
			t.Errorf("Runs-hit cell still splits its totals onto a second line:\n%s", body[i:i+end])
		}
	} else {
		t.Fatalf("body missing the Runs-hit cell:\n%s", body)
	}
	if strings.Contains(body, `class="problem-members"`) {
		t.Errorf("members still render as a separate table row:\n%s", body)
	}
	if !strings.Contains(body, "Members (1)") || !strings.Contains(body, `href="/r/cpt1-a"`) {
		t.Errorf("body dropped the members list itself:\n%s", body)
	}
}

// seedTwoDistinctProblems seeds one batch with two runs, each producing its
// own single-member problem (distinct anchors so they cluster separately):
// "High problem" (high, started 2h before now) and "Medium problem"
// (medium, started 1h before now) — the shared fixture for the filter/sort/
// summary tests below.
func seedTwoDistinctProblems(t *testing.T, store *fakeStore, now time.Time) {
	t.Helper()
	seedBatch(t, store, "pr2", "claude-sonnet-5", []runFixture{
		{runID: "pr2-a", scenario: "a", startedAt: now.Add(-2 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "pr2-b", scenario: "b", startedAt: now.Add(-1 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"pr2-a": "passed", "pr2-b": "passed"})
	seedFormat2Finding(t, store, "pr2-a", now.Add(-2*time.Hour), observer.SeverityHigh,
		"tool:zerops_deploy/deploy", "ANCHOR_A", "High problem", "fix a", 1, "quote a")
	seedFormat2Finding(t, store, "pr2-b", now.Add(-1*time.Hour), observer.SeverityMedium,
		"tool:zerops_import/import", "ANCHOR_B", "Medium problem", "fix b", 1, "quote b")
}

// TestPages_ProblemsSeverityFilterTooltipNamesThreshold pins item 11
// (FIX2): the severity filter's own options carry a tooltip that says what
// selecting them narrows the list to ("medium or higher", "any severity")
// rather than the plain severity-level definition, which describes the
// severity itself, not the minimum-filter's effect.
func TestPages_ProblemsSeverityFilterTooltipNamesThreshold(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedTwoDistinctProblems(t, store, fixedNow(t)())

	body := doGET(t, h, "/problems").Body.String()
	if !strings.Contains(body, `title="medium or higher"`) {
		t.Errorf("body missing the Medium option's \"medium or higher\" tooltip:\n%s", body)
	}
	if !strings.Contains(body, `title="any severity"`) {
		t.Errorf("body missing the Low option's \"any severity\" tooltip:\n%s", body)
	}
}

// TestPages_ProblemsFilterNarrowsRowsAndShowsCounts pins §8.7: a filter
// link narrows the rendered rows, and each filter option shows its count.
func TestPages_ProblemsFilterNarrowsRowsAndShowsCounts(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()
	seedTwoDistinctProblems(t, store, now)

	all := doGET(t, h, "/problems")
	if all.Code != http.StatusOK {
		t.Fatalf("GET /problems: got %d, want 200, body=%s", all.Code, all.Body.String())
	}
	if body := all.Body.String(); !strings.Contains(body, "High problem") || !strings.Contains(body, "Medium problem") {
		t.Fatalf("unfiltered body missing one of the two problems:\n%s", body)
	}
	// The Cause filter's "ZCP" option (both problems are ZCP-caused) shows
	// its count under the OTHER active filters (§8.7): 2 unfiltered.
	if !strings.Contains(all.Body.String(), `>ZCP <span class="filter-count">2</span>`) {
		t.Errorf("body missing the unfiltered Cause=ZCP count of 2:\n%s", all.Body.String())
	}

	high := doGET(t, h, "/problems?severity=high")
	if high.Code != http.StatusOK {
		t.Fatalf("GET /problems?severity=high: got %d, want 200", high.Code)
	}
	hb := high.Body.String()
	if !strings.Contains(hb, "High problem") {
		t.Errorf("severity=high body missing the high problem:\n%s", hb)
	}
	if strings.Contains(hb, "Medium problem") {
		t.Errorf("severity=high body still shows the medium problem:\n%s", hb)
	}
	// The Cause filter's own count narrows to 1 under severity=high, the
	// other active filter (§8.7: "each filter option shows its count under
	// the other active filters").
	if !strings.Contains(hb, `>ZCP <span class="filter-count">1</span>`) {
		t.Errorf("severity=high body's Cause=ZCP count did not narrow to 1:\n%s", hb)
	}
}

// TestPages_ProblemsSortReordersRows pins §8.7: a sort link reorders the
// rows, and every link on the page keeps the other parameters (severity
// stays set while sort/dir change).
func TestPages_ProblemsSortReordersRows(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()
	seedTwoDistinctProblems(t, store, now)

	asc := doGET(t, h, "/problems?sort=first&dir=asc")
	if asc.Code != http.StatusOK {
		t.Fatalf("GET ?sort=first&dir=asc: got %d, want 200", asc.Code)
	}
	ab := asc.Body.String()
	iHighAsc, iMedAsc := strings.Index(ab, "High problem"), strings.Index(ab, "Medium problem")
	if iHighAsc < 0 || iMedAsc < 0 || iHighAsc > iMedAsc {
		t.Errorf("sort=first&dir=asc: want High (older, first-seen earlier) before Medium; High@%d Medium@%d\n%s", iHighAsc, iMedAsc, ab)
	}

	desc := doGET(t, h, "/problems?sort=first&dir=desc")
	db := desc.Body.String()
	iHighDesc, iMedDesc := strings.Index(db, "High problem"), strings.Index(db, "Medium problem")
	if iHighDesc < 0 || iMedDesc < 0 || iMedDesc > iHighDesc {
		t.Errorf("sort=first&dir=desc: want Medium before High; High@%d Medium@%d\n%s", iHighDesc, iMedDesc, db)
	}

	// Every link on the page keeps the other parameters: with severity=high
	// active, the sort header's own link must still carry severity=high.
	withFilter := doGET(t, h, "/problems?severity=high&sort=first&dir=asc").Body.String()
	if !strings.Contains(withFilter, "severity=high") {
		t.Errorf("a link on the filtered page dropped severity=high:\n%s", withFilter)
	}
}

// TestPages_ProblemsUnknownParameterRendersBadQueryPage pins §8.7: a
// parameter the list does not take is refused with a 400 page naming it
// and a link that drops it.
func TestPages_ProblemsUnknownParameterRendersBadQueryPage(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	rr := doGET(t, h, "/problems?bogus=1")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("GET /problems?bogus=1: got %d, want 400, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "bogus") {
		t.Errorf("400 body does not name the refused parameter:\n%s", body)
	}
	if strings.Contains(body, "bogus=1") {
		t.Errorf("400 body's drop-link still carries the refused parameter:\n%s", body)
	}
}

// TestPages_ProblemsSummaryLineCountsLiveAndHigh pins the page's one-line
// summary: live problem count and high-severity count among them.
func TestPages_ProblemsSummaryLineCountsLiveAndHigh(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()
	seedTwoDistinctProblems(t, store, now)

	body := doGET(t, h, "/problems").Body.String()
	if !strings.Contains(body, "2 live problems") {
		t.Errorf("summary missing \"2 live problems\":\n%s", body)
	}
	if !strings.Contains(body, "1 high") {
		t.Errorf("summary missing \"1 high\":\n%s", body)
	}
}

// TestPages_ProblemsOmitsSurfaceChipWhenEmpty pins item 11 (round-1
// follow-up): a problem with no surface (a format-2 finding whose surface
// is "agent"/"platform"/empty, or a format-1 finding) renders no empty
// surface chip on /problems.
func TestPages_ProblemsOmitsSurfaceChipWhenEmpty(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "ns3", "claude-sonnet-5", []runFixture{
		{runID: "ns3-a", scenario: "a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ns3-a": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "ns3-a", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "x",
		Findings: []observer.Finding{{Severity: "high", Owner: "agent", Title: "format-1 finding, no surface"}},
	})

	body := doGET(t, h, "/problems").Body.String()
	if !strings.Contains(body, "format-1 finding, no surface") {
		t.Fatalf("body missing the problem's title:\n%s", body)
	}
	if strings.Contains(body, `<code class="chip"></code>`) {
		t.Errorf("body renders an empty surface chip:\n%s", body)
	}
}

// TestPages_ProblemsSortChipsAboveStackedTable pins item 15 (round-1
// follow-up): /problems renders its sort options as a chip row above the
// table, like /findings already does.
func TestPages_ProblemsSortChipsAboveStackedTable(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedBatch(t, store, "psc1", "off", []runFixture{
		{runID: "psc1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"psc1-a": "passed"})

	body := doGET(t, h, "/problems").Body.String()
	if !strings.Contains(body, `<span class="k">Sort</span>`) {
		t.Errorf("body missing a sort chip row above the problems table:\n%s", body)
	}
}

// TestPages_ProblemsSurfaceLinkNarrowsList pins the filter-tester's finding:
// a problem row's surface chip is a link (listURL, keeping the other
// parameters) that narrows /problems to that surface.
func TestPages_ProblemsSurfaceLinkNarrowsList(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "sl2", "claude-sonnet-5", []runFixture{
		{runID: "sl2-a", scenario: "a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "sl2-b", scenario: "b", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"sl2-a": "passed", "sl2-b": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "sl2-a", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "x",
		Findings: []observer.Finding{{Severity: "high", Owner: "zcp-tool", Surface: "tool:zerops_deploy/deploy", Title: "deploy surface problem"}},
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "sl2-b", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "y",
		Findings: []observer.Finding{{Severity: "high", Owner: "zcp-tool", Surface: "tool:zerops_scale/scale", Title: "scale surface problem"}},
	})

	body := doGET(t, h, "/problems").Body.String()
	// Problem.Surface is already the clustering key's own truncated prefix
	// (up to the first "/") — the finding's raw "tool:zerops_deploy/deploy"
	// surface becomes the row's "tool:zerops_deploy".
	linkRE := regexp.MustCompile(`href="(/problems\?[^"]*surface=tool%3Azerops_deploy[^"]*)"`)
	m := linkRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("body missing a surface filter link for tool:zerops_deploy:\n%s", body)
	}

	narrowed := doGET(t, h, html.UnescapeString(m[1])).Body.String()
	if !strings.Contains(narrowed, "deploy surface problem") {
		t.Errorf("surface link did not keep the matching problem:\n%s", narrowed)
	}
	if strings.Contains(narrowed, "scale surface problem") {
		t.Errorf("surface link did not narrow out the other problem:\n%s", narrowed)
	}
}

// TestPages_ProblemsMemberLinksNarrowList pins the filter-tester's finding:
// a problem member's scenario/batch/build values are links (each via
// listURL, keeping the other parameters) that narrow /problems.
func TestPages_ProblemsMemberLinksNarrowList(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	// Both members share scenario "scen-a" (seeded below).
	shared := func(runID string) observer.Observation {
		return observer.Observation{
			FormatVersion: observer.ObservationFormat2, RunID: runID,
			ObsID: "20260911T120000000Z-claude-sonnet-5", Model: "claude-sonnet-5", CreatedAt: now,
			Status: "ok", Outcome: observer.OutcomeProblem, Headline: "same tool problem",
			Findings: []observer.Finding{{Severity: "high", Owner: "zcp-tool", Surface: "tool:zerops_deploy/deploy", Title: "always fails this way"}},
		}
	}

	seedBatch(t, store, "ml1", "claude-sonnet-5", []runFixture{
		{runID: "ml1-a", scenario: "scen-a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ml1-a": "passed"})
	seedObservation(t, store, shared("ml1-a"))

	seedBatch(t, store, "ml2", "claude-sonnet-5", []runFixture{
		{runID: "ml2-a", scenario: "scen-a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ml2-a": "passed"})
	seedObservation(t, store, shared("ml2-a"))

	// A second, unrelated problem in a batch with a different scenario,
	// batch id AND build (overwritten below — seedBatch always writes
	// "cand-sha"), so batch/scenario/build filters each have something
	// real to narrow out.
	seedBatch(t, store, "ml3", "claude-sonnet-5", []runFixture{
		{runID: "ml3-x", scenario: "scen-x", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ml3-x": "passed"})
	// Older than ml1/ml2's shared CreatedAt (seedBatch's own fixed
	// timestamp) so it never becomes "the newest build" (§8.6) itself —
	// that would reclassify the ml1/ml2 problem as unconfirmed (not hit
	// on the newest build) and drop it out of status=live, the default
	// this test's own /problems fetch relies on.
	store.putJSON(t, "batches/ml3/manifest.json", farm.BatchManifest{
		Batch: "ml3", CreatedAt: "2026-08-01T00:00:00Z", StartedAt: "2026-08-01T00:00:00Z",
		Set: "gate", CandidateSha256: "other-sha", EvaluatorSha256: "eval-sha", ScenariosDigest: "scn-sha",
		Observer: "claude-sonnet-5", Runs: []farm.ManifestRun{{RunID: "ml3-x", Scenario: "scen-x", ProjectName: "zcp-farm-ml3-x"}},
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "ml3-x", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "z",
		Findings: []observer.Finding{{Severity: "high", Owner: "agent", Title: "unrelated problem"}},
	})

	body := doGET(t, h, "/problems").Body.String()

	scenarioLinkRE := regexp.MustCompile(`href="(/problems\?[^"]*scenario=scen-a[^"]*)"`)
	batchLinkRE := regexp.MustCompile(`href="(/problems\?[^"]*batch=ml1[^"]*)"`)
	buildLinkRE := regexp.MustCompile(`href="(/problems\?[^"]*build=cand-sha[^"]*)"`)

	for name, re := range map[string]*regexp.Regexp{"scenario": scenarioLinkRE, "batch": batchLinkRE, "build": buildLinkRE} {
		m := re.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("body missing a %s filter link on a problem member:\n%s", name, body)
		}
		narrowed := doGET(t, h, html.UnescapeString(m[1])).Body.String()
		if !strings.Contains(narrowed, "always fails this way") {
			t.Errorf("%s link did not keep the matching problem:\n%s", name, narrowed)
		}
		if strings.Contains(narrowed, "unrelated problem") {
			t.Errorf("%s link did not narrow out the unrelated problem:\n%s", name, narrowed)
		}
	}
}
