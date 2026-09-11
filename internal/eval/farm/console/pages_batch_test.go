// Package console: pages_batch_test.go pins GET /b/<batch> (docs/spec-
// eval-farm.md §8.3 FM-51's five blocks, §8.5's assess predicate, §8.6's
// per-batch problems, §8.7's runs list) — plans/farm-console-clarity-
// 2026-09-11-briefs/WAVE2.md's BATCH slice. pages_test.go/batches_test.go
// already pin the parts of this page that predate this slice (headline/
// observer-state text, the refresh tag, top-nav, escaping); this file adds
// only the new behavior.
package console

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// TestPages_BatchRunsGroupedByPrecedence pins item 4: a run sits in the
// first group it fits — Failed and blocked, Not finished, Not assessed,
// Problems in passed runs, Clean — in that order, regardless of scenario
// name order. Independent oracle: the group headings are the spec's own
// words (§8.3), and each scenario is hand-placed into exactly one group by
// its fixture's own verdict/observation.
func TestPages_BatchRunsGroupedByPrecedence(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "gp1", "claude-sonnet-5", []runFixture{
		{runID: "gp1-failed", scenario: "zz-failed", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true,
			checks: [][5]string{{"chk1", "failed", "exp1", "obs1", "src"}}},
		{runID: "gp1-blocked", scenario: "yy-blocked", startedAt: now, done: false},
		{runID: "gp1-running", scenario: "xx-running", startedAt: now, done: false},
		{runID: "gp1-notassessed", scenario: "ww-notassessed", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "gp1-problem", scenario: "vv-problem", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "gp1-clean", scenario: "aa-clean", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{
		"gp1-failed": "failed", "gp1-blocked": "blocked", "gp1-notassessed": "passed",
		"gp1-problem": "passed", "gp1-clean": "passed",
		// gp1-running has no summary row: settledOrRunning leaves it "running".
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "gp1-problem", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Headline: "found something",
		Findings: []observer.Finding{{Severity: "medium", Owner: "agent", Title: "a problem", What: "x", Fix: "y"}},
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "gp1-clean", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Headline: "all clean",
	})

	rr := doGET(t, h, "/b/gp1")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /b/gp1: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	headings := []string{"Failed and blocked", "Not finished", "Not assessed", "Problems in passed runs", "Clean"}
	idx := map[string]int{}
	for _, hd := range headings {
		i := strings.Index(body, hd)
		if i < 0 {
			t.Fatalf("body missing group heading %q:\n%s", hd, body)
		}
		idx[hd] = i
	}
	for i := 1; i < len(headings); i++ {
		if idx[headings[i-1]] >= idx[headings[i]] {
			t.Errorf("group %q (at %d) does not come before %q (at %d)", headings[i-1], idx[headings[i-1]], headings[i], idx[headings[i]])
		}
	}

	bound := func(name string) (int, int) {
		start := idx[name]
		end := len(body)
		for _, hd := range headings {
			if idx[hd] > start && idx[hd] < end {
				end = idx[hd]
			}
		}
		return start, end
	}
	assertScenarioInGroup := func(scenario, group string) {
		start, end := bound(group)
		i := strings.Index(body, scenario)
		if i < start || i > end {
			t.Errorf("scenario %q not found within group %q's own section (group span %d-%d, found at %d)", scenario, group, start, end, i)
		}
	}
	assertScenarioInGroup("zz-failed", "Failed and blocked")
	assertScenarioInGroup("yy-blocked", "Failed and blocked")
	assertScenarioInGroup("xx-running", "Not finished")
	assertScenarioInGroup("ww-notassessed", "Not assessed")
	assertScenarioInGroup("vv-problem", "Problems in passed runs")
	assertScenarioInGroup("aa-clean", "Clean")
}

// TestPages_BatchAssessCalloutUsesNeedsAssessmentPredicate pins item 5:
// the callout counts exactly view.go's NeedsAssessment predicate (a run
// queued/running is excluded even with no observation), "Re-assess all"
// renders only once at least one run is assessed, and neither renders
// when no run of the batch has finished. Independent oracle: view.go's
// own NeedsAssessment doc/behavior, not recomputed.
func TestPages_BatchAssessCalloutUsesNeedsAssessmentPredicate(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	q := NewQueue(func(ctx context.Context, job Job) error {
		<-release
		return nil
	})
	store := newFakeStore()
	srv := NewServer(Config{Store: store, Token: testToken, Now: fixedNow(t), Queue: q})
	h := srv.Handler()

	seedBatch(t, store, "cb3", "claude-sonnet-5", []runFixture{
		{runID: "cb3-inflight", scenario: "inflight", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "cb3-plain", scenario: "plain", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"cb3-inflight": "passed", "cb3-plain": "passed"})

	if err := q.Enqueue(context.Background(), Job{RunID: "cb3-inflight", Batch: "cb3", Model: "claude-sonnet-5"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if !wkEventually(t, func() bool { return q.State("cb3-inflight") != "" }) {
		t.Fatal("job never showed as queued/running")
	}

	t.Run("in-flight run excluded, none assessed yet: no re-assess-all", func(t *testing.T) {
		body := doGET(t, h, "/b/cb3").Body.String()
		if !strings.Contains(body, "1 finished run") {
			t.Errorf("want exactly 1 counted (the in-flight run excluded):\n%s", body)
		}
		if strings.Contains(body, "2 finished run") {
			t.Errorf("in-flight run must not be counted:\n%s", body)
		}
		if strings.Contains(body, "Re-assess every run of this batch") {
			t.Errorf("re-assess-all must not render before any run is assessed:\n%s", body)
		}
	})

	seedBatch(t, store, "cb4", "claude-sonnet-5", []runFixture{
		{runID: "cb4-a", scenario: "a", startedAt: fixedNow(t)(), done: false},
	}, false, nil)
	t.Run("no run finished: neither callout", func(t *testing.T) {
		body := doGET(t, h, "/b/cb4").Body.String()
		if strings.Contains(body, "finished run") {
			t.Errorf("no run finished, want no assess callout:\n%s", body)
		}
		if strings.Contains(body, "Re-assess every run of this batch") {
			t.Errorf("no run finished, want no re-assess-all:\n%s", body)
		}
	})

	seedBatch(t, store, "cb5", "claude-sonnet-5", []runFixture{
		{runID: "cb5-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"cb5-a": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "cb5-a", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: fixedNow(t)(), Status: "ok", Headline: "fine",
	})
	t.Run("everything assessed: re-assess-all shows, no unassessed callout", func(t *testing.T) {
		body := doGET(t, h, "/b/cb5").Body.String()
		if strings.Contains(body, "finished run") {
			t.Errorf("nothing left to assess, want no assess callout:\n%s", body)
		}
		if !strings.Contains(body, "Re-assess every run of this batch") {
			t.Errorf("at least one run assessed, want re-assess-all:\n%s", body)
		}
	})
}

// TestPages_BatchVsPreviousBatchLine pins item 1's "vs <previous batch of
// the same set>" line: newly failing/fixed/still failing by scenario name.
// Independent oracle: view.go's CompareBatches, exercised end to end
// through two hand-built same-set batches whose scenario "s1"/"s2"/"s3"
// verdicts were chosen to hit exactly one of the three buckets each.
// seedBatch pins every manifest's CreatedAt to the same instant, so
// PreviousSameSet's own tie-break (batch id, ascending = older) decides
// order — "vp1" sorts before "vp2".
func TestPages_BatchVsPreviousBatchLine(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "vp1", "claude-sonnet-5", []runFixture{
		{runID: "vp1-s1", scenario: "s1", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true},
		{runID: "vp1-s2", scenario: "s2", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "vp1-s3", scenario: "s3", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true},
	}, true, map[string]string{"vp1-s1": "failed", "vp1-s2": "passed", "vp1-s3": "failed"})

	seedBatch(t, store, "vp2", "claude-sonnet-5", []runFixture{
		{runID: "vp2-s1", scenario: "s1", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "vp2-s2", scenario: "s2", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true},
		{runID: "vp2-s3", scenario: "s3", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true},
	}, true, map[string]string{"vp2-s1": "passed", "vp2-s2": "failed", "vp2-s3": "failed"})

	rr := doGET(t, h, "/b/vp2")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /b/vp2: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	if !strings.Contains(body, `href="/b/vp1"`) {
		t.Errorf("body missing a link to the previous batch vp1:\n%s", body)
	}
	if !strings.Contains(body, "fixed: s1") {
		t.Errorf("body missing 'fixed: s1':\n%s", body)
	}
	if !strings.Contains(body, "newly failing: s2") {
		t.Errorf("body missing 'newly failing: s2':\n%s", body)
	}
	if !strings.Contains(body, "still failing: s3") {
		t.Errorf("body missing 'still failing: s3':\n%s", body)
	}

	// The oldest batch has no older same-set batch to compare against —
	// its own page renders no vs-previous line at all.
	bodyOldest := doGET(t, h, "/b/vp1").Body.String()
	if strings.Contains(bodyOldest, "vs <a") || strings.Contains(bodyOldest, "href=\"/b/vp2\"") {
		t.Errorf("oldest batch must not render a vs-previous line:\n%s", bodyOldest)
	}
}

// TestPages_BatchProblemsSection pins item 3: a problem hitting this
// batch is marked "also in"/"not in" the previous same-set batch, and a
// batch with no assessment at all falls back to "checks that failed in
// two or more runs" instead of §8.6 clustering. Independent oracle:
// problems.go's own clustering key (a format-2 finding without an anchor
// whose surface is "tool:..." clusters by surface+owner+scenario, §8.6),
// exercised through two same-set batches whose two runs share one finding
// surface/scenario/owner (so they cluster into the same problem) and a
// third, unassessed batch whose two runs share one failed check id.
func TestPages_BatchProblemsSection(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	shared := func(runID string) observer.Observation {
		return observer.Observation{
			FormatVersion: observer.ObservationFormat2, RunID: runID,
			ObsID: "20260911T120000000Z-claude-sonnet-5", Model: "claude-sonnet-5", CreatedAt: now,
			Status: "ok", Outcome: observer.OutcomeProblem, Headline: "same tool problem",
			Findings: []observer.Finding{{Severity: "high", Owner: "zcp-tool", Surface: "tool:zerops_deploy/deploy", Title: "always fails this way"}},
		}
	}

	seedBatch(t, store, "pp1", "claude-sonnet-5", []runFixture{
		{runID: "pp1-a", scenario: "a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"pp1-a": "passed"})
	seedObservation(t, store, shared("pp1-a"))

	seedBatch(t, store, "pp2", "claude-sonnet-5", []runFixture{
		{runID: "pp2-a", scenario: "a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"pp2-a": "passed"})
	seedObservation(t, store, shared("pp2-a"))

	t.Run("hits both batches: also in", func(t *testing.T) {
		body := doGET(t, h, "/b/pp2").Body.String()
		if !strings.Contains(body, "always fails this way") {
			t.Fatalf("body missing the problem's title:\n%s", body)
		}
		if !strings.Contains(body, "also in") || !strings.Contains(body, `href="/b/pp1"`) {
			t.Errorf("body missing 'also in' + a link to pp1:\n%s", body)
		}
	})
	t.Run("the previous batch itself has no older batch: no also/not-in line", func(t *testing.T) {
		body := doGET(t, h, "/b/pp1").Body.String()
		if strings.Contains(body, "also in") || strings.Contains(body, "not in") {
			t.Errorf("oldest batch's problems must carry no also/not-in line:\n%s", body)
		}
	})

	seedBatch(t, store, "pp3", "claude-sonnet-5", []runFixture{
		{runID: "pp3-a", scenario: "a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true,
			checks: [][5]string{{"chk9", "failed", "exp", "obs", "src"}}},
		{runID: "pp3-b", scenario: "b", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true,
			checks: [][5]string{{"chk9", "failed", "exp", "obs", "src"}}},
	}, true, map[string]string{"pp3-a": "failed", "pp3-b": "failed"})

	t.Run("no assessment at all: fallback to repeated failed checks", func(t *testing.T) {
		body := doGET(t, h, "/b/pp3").Body.String()
		if !strings.Contains(body, "checks that failed in two or more runs") {
			t.Errorf("body missing the fallback notice:\n%s", body)
		}
		if !strings.Contains(body, "chk9") || !strings.Contains(body, "2 runs") {
			t.Errorf("body missing the repeated check id and its 2-run count:\n%s", body)
		}
	})
}

// TestPages_BatchRunsListRendersFilterSortCountAnd400 pins §8.7 for the
// batch-runs list AT THE PAGE (not the bare Engine, already view_test.go's
// TestLists_BatchRuns): a filter link narrows the rendered rows, a sort
// link reorders them, counts are shown, links keep the other parameter,
// and an unknown parameter renders the 400 page. Independent oracle: two
// hand-picked runs whose verdict differs (one filter value has count 1,
// the other 0) and whose scenario names sort predictably.
func TestPages_BatchRunsListRendersFilterSortCountAnd400(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "lb1", "claude-sonnet-5", []runFixture{
		{runID: "lb1-zzz", scenario: "zzz", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "lb1-aaa", scenario: "aaa", startedAt: now, durationS: "9s", costUsd: 0.2, taskResult: "failed", done: true,
			checks: [][5]string{{"chk1", "failed", "e", "o", "s"}}},
	}, true, map[string]string{"lb1-zzz": "passed", "lb1-aaa": "failed"})

	t.Run("counts shown", func(t *testing.T) {
		body := doGET(t, h, "/b/lb1").Body.String()
		if !strings.Contains(body, `href="/b/lb1?verdict=failed"`) || !strings.Contains(body, `<span class="filter-count">1</span>`) {
			t.Errorf("body missing the verdict filter option with its count:\n%s", body)
		}
	})

	t.Run("filter link narrows rendered rows, keeps other params", func(t *testing.T) {
		body := doGET(t, h, "/b/lb1?verdict=failed&outcome=none").Body.String()
		if !strings.Contains(body, "aaa") {
			t.Errorf("verdict=failed body missing the failed run:\n%s", body)
		}
		if strings.Contains(body, ">zzz<") {
			t.Errorf("verdict=failed body still shows the passed run:\n%s", body)
		}
		if !strings.Contains(body, "outcome=none") {
			t.Errorf("filter links must keep the other active parameter (outcome=none):\n%s", body)
		}
	})

	t.Run("sort link reorders", func(t *testing.T) {
		body := doGET(t, h, "/b/lb1?sort=scenario&dir=asc").Body.String()
		if i, j := strings.Index(body, ">aaa<"), strings.Index(body, ">zzz<"); i < 0 || j < 0 || i > j {
			t.Errorf("sort=scenario&dir=asc did not order aaa before zzz:\n%s", body)
		}
	})

	t.Run("unknown parameter renders the 400 page", func(t *testing.T) {
		rr := doGET(t, h, "/b/lb1?bogus=1")
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("GET /b/lb1?bogus=1: got %d, want 400", rr.Code)
		}
		if body := rr.Body.String(); !strings.Contains(body, "<code>bogus</code>") {
			t.Errorf("400 page must name the refused parameter:\n%s", body)
		}
	})
}

// TestPages_BatchRunsTableHeaderMatchesDataColumns pins item 2 (round-1
// follow-up): the runs table has one header per data column — the missing
// "Why / headline" header — and every group heading / collapsed / empty
// row spans all five.
func TestPages_BatchRunsTableHeaderMatchesDataColumns(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "hc1", "off", []runFixture{
		{runID: "hc1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"hc1-a": "passed"})

	body := doGET(t, h, "/b/hc1").Body.String()
	if !strings.Contains(body, "Why / headline") {
		t.Errorf("runs table is missing the Why/headline header:\n%s", body)
	}
	if strings.Contains(body, `colspan="4"`) {
		t.Errorf("a group/collapsed/empty row still spans only 4 columns:\n%s", body)
	}
	if !strings.Contains(body, `colspan="5"`) {
		t.Errorf("no row spans all 5 columns:\n%s", body)
	}
}

// TestPages_BatchProblemsCompactWithLowFolded pins item 3 (round-1
// follow-up): "Problems in this batch" renders one compact <details> line
// per non-low problem (severity, cause, title, runs hit), with low-severity
// problems folded into one "n low" line — never the old multi-line card.
func TestPages_BatchProblemsCompactWithLowFolded(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "cp1", "claude-sonnet-5", []runFixture{
		{runID: "cp1-hi", scenario: "hi", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "cp1-lo", scenario: "lo", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"cp1-hi": "passed", "cp1-lo": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "cp1-hi", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "high sev",
		Findings: []observer.Finding{{Severity: "high", Owner: "zcp-tool", Surface: "tool:zerops_deploy/deploy", Title: "always fails this way"}},
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "cp1-lo", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "low sev",
		Findings: []observer.Finding{{Severity: "low", Owner: "zcp-tool", Surface: "tool:zerops_scale/scale", Title: "minor wording issue"}},
	})

	body := doGET(t, h, "/b/cp1").Body.String()
	if !strings.Contains(body, "always fails this way") {
		t.Fatalf("body missing the high-severity problem's title:\n%s", body)
	}
	if strings.Contains(body, "minor wording issue") {
		t.Errorf("body shows the low-severity problem's own title instead of folding it:\n%s", body)
	}
	if !strings.Contains(body, "1 low") {
		t.Errorf("body missing the folded \"1 low\" line:\n%s", body)
	}
	if !strings.Contains(body, "<details") || !strings.Contains(body, "<summary>") {
		t.Errorf("problems block is not rendered as compact <details>/<summary>:\n%s", body)
	}
}

// TestPages_BatchSummaryLabelledLine pins item 9 (round-1 follow-up): the
// batch summary renders one labelled line ("Goal: ... — Assessment: ... —
// ZCP findings: ..."), every zero count skipped, replacing the old row of
// identical chips.
func TestPages_BatchSummaryLabelledLine(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "sl1", "claude-sonnet-5", []runFixture{
		{runID: "sl1-a", scenario: "a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "sl1-b", scenario: "b", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"sl1-a": "passed", "sl1-b": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "sl1-a", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeOK,
		Goal: observer.Goal{Reached: "yes"}, Headline: "fine",
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "sl1-b", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem,
		Goal: observer.Goal{Reached: "no"}, Headline: "broken",
		Findings: []observer.Finding{{Severity: "medium", Owner: "zcp-tool", Title: "x"}},
	})

	body := doGET(t, h, "/b/sl1").Body.String()
	if !strings.Contains(body, "Goal: 1 yes · 1 no") {
		t.Errorf("body missing the Goal summary line:\n%s", body)
	}
	if !strings.Contains(body, "Assessment: 1 OK · 1 problem") {
		t.Errorf("body missing the Assessment summary line:\n%s", body)
	}
	if !strings.Contains(body, "ZCP findings: 1 medium") {
		t.Errorf("body missing the ZCP findings summary line:\n%s", body)
	}
	if strings.Contains(body, "0 partly") || strings.Contains(body, "0 inconclusive") {
		t.Errorf("body shows a zero count instead of skipping it:\n%s", body)
	}
}

// TestPages_BatchProblemStatusUsesChipNotPill pins item 16 (round-1
// follow-up): the batch page's problem status badge uses .chip, not a bare
// .pill (which is reserved for yes/no/partly).
func TestPages_BatchProblemStatusUsesChipNotPill(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "ps1", "claude-sonnet-5", []runFixture{
		{runID: "ps1-a", scenario: "a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ps1-a": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "ps1-a", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "x",
		Findings: []observer.Finding{{Severity: "high", Owner: "zcp-tool", Surface: "tool:x/y", Title: "z"}},
	})

	body := doGET(t, h, "/b/ps1").Body.String()
	if !strings.Contains(body, `<span class="chip" title="`) {
		t.Errorf("problem status is not rendered with .chip:\n%s", body)
	}
	if strings.Contains(body, `<span class="pill" title="`) {
		t.Errorf("problem status still uses a bare .pill:\n%s", body)
	}
}

// TestPages_BatchBreadcrumbRootReadsOverview pins item 17 (round-1
// follow-up): the batch page's breadcrumb root reads "Overview", matching
// the run page's own crumb, instead of the old "Batches".
func TestPages_BatchBreadcrumbRootReadsOverview(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedBatch(t, store, "cr1", "off", []runFixture{
		{runID: "cr1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"cr1-a": "passed"})

	body := doGET(t, h, "/b/cr1").Body.String()
	if !strings.Contains(body, `<a href="/">Overview</a>`) {
		t.Errorf("breadcrumb root does not read Overview:\n%s", body)
	}
	if strings.Contains(body, `<a href="/">Batches</a>`) {
		t.Errorf("breadcrumb root still reads Batches:\n%s", body)
	}
}

// TestPages_BatchFirstFailedLineRewritesMonotonicClockText pins item 19 for
// the batch page's own failed-checks line (firstFailedCheckPlain).
func TestPages_BatchFirstFailedLineRewritesMonotonicClockText(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()

	seedBatch(t, store, "bmc1", "off", []runFixture{
		{runID: "bmc1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true,
			checks: [][5]string{{"c1", "failed", "2026-09-11 18:33:47 +0000 UTC m=+0.5", "still running", "mcp"}}},
	}, true, map[string]string{"bmc1-a": "failed"})

	body := doGET(t, h, "/b/bmc1").Body.String()
	if !strings.Contains(body, "2026-09-11T18:33:47Z") {
		t.Errorf("body missing the RFC3339-normalized expected text:\n%s", body)
	}
	if strings.Contains(body, "m=+0.5") {
		t.Errorf("body still shows the raw monotonic-clock suffix:\n%s", body)
	}
}

// TestPages_BatchProblemsOmitsSurfaceChipWhenEmpty pins item 11 (round-1
// follow-up) on the batch page's own compact problems block.
func TestPages_BatchProblemsOmitsSurfaceChipWhenEmpty(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "bns1", "claude-sonnet-5", []runFixture{
		{runID: "bns1-a", scenario: "a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"bns1-a": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "bns1-a", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "x",
		Findings: []observer.Finding{{Severity: "high", Owner: "agent", Title: "format-1 finding, no surface"}},
	})

	body := doGET(t, h, "/b/bns1").Body.String()
	if !strings.Contains(body, "format-1 finding, no surface") {
		t.Fatalf("body missing the problem's title:\n%s", body)
	}
	if strings.Contains(body, `<code class="chip"></code>`) {
		t.Errorf("body renders an empty surface chip:\n%s", body)
	}
}

// TestPages_BatchRunsSortChipsAboveStackedTable pins item 15 (round-1
// follow-up): the batch page's runs list renders its sort options as a
// chip row above the table.
func TestPages_BatchRunsSortChipsAboveStackedTable(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedBatch(t, store, "bsc1", "off", []runFixture{
		{runID: "bsc1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"bsc1-a": "passed"})

	body := doGET(t, h, "/b/bsc1").Body.String()
	if !strings.Contains(body, `<span class="k">Sort</span>`) {
		t.Errorf("body missing a sort chip row above the runs table:\n%s", body)
	}
}
