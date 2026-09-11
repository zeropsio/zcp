package console

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// seedBatchAt is seedBatch (console_test.go) with a caller-chosen createdAt
// instead of the fixture's fixed 2026-09-01 — this file's tests need two
// same-set batches at different times to exercise §8.3 item 1's "vs
// previous batch of the same set" line and §8.7's `newest`/`cost` sort
// keys. Set stays "gate", matching seedBatch's own fixture and every one of
// this file's batches (the "previous batch of the SAME set" rule is what's
// under test, not a set mismatch).
func seedBatchAt(t *testing.T, store *fakeStore, batch, observerModel string, createdAt time.Time, runs []runFixture, withSummary bool, summaryResults map[string]string) {
	t.Helper()

	manifest := farm.BatchManifest{
		Batch: batch, CreatedAt: createdAt.UTC().Format(time.RFC3339), StartedAt: createdAt.UTC().Format(time.RFC3339),
		Set: "gate", CandidateSha256: "cand-sha", EvaluatorSha256: "eval-sha",
		ScenariosDigest: "scn-sha", Observer: observerModel,
	}
	for _, rf := range runs {
		manifest.Runs = append(manifest.Runs, farm.ManifestRun{
			RunID: rf.runID, Scenario: rf.scenario, ProjectName: "zcp-farm-" + rf.runID,
		})
	}
	store.putJSON(t, "batches/"+batch+"/manifest.json", manifest)

	if withSummary {
		summary := farm.BatchSummary{Batch: batch, FinishedAt: createdAt.Add(time.Hour).UTC().Format(time.RFC3339), EndedBy: "settled"}
		for _, rf := range runs {
			result := summaryResults[rf.runID]
			if result == "" {
				continue
			}
			summary.Runs = append(summary.Runs, farm.SummaryRun{RunID: rf.runID, Scenario: rf.scenario, Result: result})
		}
		store.putJSON(t, "batches/"+batch+"/summary.json", summary)
	}

	for _, rf := range runs {
		seedRun(t, store, rf)
	}
}

// TestHome_LatestEvaluationPanel pins §8.3 item 1 end to end: the newest
// gate/all batch's verdict counts with a disputed run counted within its
// verdict ("2 failed (1 disputed)"), one line per failed/blocked run with
// its headline (the current observation's, else the run's own assessment-
// state wording) and "observer disputes: <why>" when the checks were
// judged wrong, and the vs-previous-batch-of-the-same-set line (newly
// failing/fixed/still failing by scenario name).
func TestHome_LatestEvaluationPanel(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatchAt(t, store, "le0", "claude-sonnet-5", now.Add(-2*time.Hour), []runFixture{
		{runID: "le0-alpha", scenario: "alpha", startedAt: now.Add(-2 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "le0-beta", scenario: "beta", startedAt: now.Add(-2 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true},
		{runID: "le0-gamma", scenario: "gamma", startedAt: now.Add(-2 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true},
	}, true, map[string]string{"le0-alpha": "passed", "le0-beta": "failed", "le0-gamma": "failed"})

	seedBatchAt(t, store, "le1", "claude-sonnet-5", now.Add(-1*time.Hour), []runFixture{
		{runID: "le1-alpha", scenario: "alpha", startedAt: now.Add(-1 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true},
		{runID: "le1-beta", scenario: "beta", startedAt: now.Add(-1 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "le1-gamma", scenario: "gamma", startedAt: now.Add(-1 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true},
	}, true, map[string]string{"le1-alpha": "failed", "le1-beta": "passed", "le1-gamma": "failed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "le1-gamma", ObsID: "20260911T110000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now.Add(-1 * time.Hour), Status: "ok", Headline: "gamma looks fine actually",
		Checks: observer.Checks{Verdict: "failed", Agree: false, Judged: []observer.JudgedCheck{
			{ID: "gamma-check", Correct: false, Why: "the check itself is wrong"},
		}},
	})

	rr := doGET(t, h, "/")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	if !strings.Contains(body, "le1") {
		t.Errorf("body missing the newest batch id le1:\n%s", body)
	}
	if !strings.Contains(body, "2 failed (1 disputed)") {
		t.Errorf("body missing the disputed-within-verdict count:\n%s", body)
	}
	if !strings.Contains(body, "1 passed") {
		t.Errorf("body missing the passed count:\n%s", body)
	}
	if !strings.Contains(body, "not assessed") {
		t.Errorf("body missing alpha's fallback headline (not assessed):\n%s", body)
	}
	if !strings.Contains(body, "gamma looks fine actually") {
		t.Errorf("body missing gamma's observation headline:\n%s", body)
	}
	if !strings.Contains(body, "observer disputes") || !strings.Contains(body, "the check itself is wrong") {
		t.Errorf("body missing the observer-disputes line with its why:\n%s", body)
	}
	if !strings.Contains(body, `href="/b/le0"`) {
		t.Errorf("body missing the vs-previous-batch link to le0:\n%s", body)
	}
	if !strings.Contains(body, "newly failing: alpha") {
		t.Errorf("body missing \"newly failing: alpha\":\n%s", body)
	}
	if !strings.Contains(body, "fixed: beta") {
		t.Errorf("body missing \"fixed: beta\":\n%s", body)
	}
	if !strings.Contains(body, "still failing: gamma") {
		t.Errorf("body missing \"still failing: gamma\":\n%s", body)
	}
}

// TestHome_LatestEvaluationNoFinishedBatch pins the "never an empty
// placeholder" rule's other side: with no evaluation batch at all, the
// panel says so in plain words instead of rendering an empty box.
func TestHome_LatestEvaluationNoFinishedBatch(t *testing.T) {
	srv, _, _ := testServer(t)
	body := doGET(t, srv.Handler(), "/").Body.String()
	if !strings.Contains(body, "No evaluation batch has finished yet.") {
		t.Errorf("body missing the no-evaluation-yet message:\n%s", body)
	}
}

// TestHome_TopProblemsFirstFiveLive pins §8.3 item 2: the first five live
// problems (§8.6), ranked severity-first, each linking to /problems — a
// sixth, lower-ranked problem is left out.
func TestHome_TopProblemsFirstFiveLive(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	type seed struct {
		runID, scenario, severity, owner, title string
		offset                                  time.Duration
	}
	seeds := []seed{
		{"tp1-r1", "s1", observer.SeverityHigh, "zcp-tool", "High problem one", -6 * time.Hour},
		{"tp1-r2", "s2", observer.SeverityHigh, "zcp-tool", "High problem two", -5 * time.Hour},
		{"tp1-r3", "s3", observer.SeverityMedium, "agent", "Medium problem one", -4 * time.Hour},
		{"tp1-r4", "s4", observer.SeverityMedium, "agent", "Medium problem two", -3 * time.Hour},
		{"tp1-r5", "s5", observer.SeverityLow, "agent", "Low problem excluded", -2 * time.Hour},
		{"tp1-r6", "s6", observer.SeverityLow, "agent", "Low problem included", -1 * time.Hour},
	}
	runs := make([]runFixture, 0, len(seeds))
	summaries := map[string]string{}
	for _, sd := range seeds {
		runs = append(runs, runFixture{runID: sd.runID, scenario: sd.scenario, startedAt: now.Add(sd.offset), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true})
		summaries[sd.runID] = "passed"
	}
	seedBatchAt(t, store, "tp1", "claude-sonnet-5", now.Add(-6*time.Hour), runs, true, summaries)
	for _, sd := range seeds {
		seedObservation(t, store, observer.Observation{
			FormatVersion: observer.ObservationFormat1, RunID: sd.runID, ObsID: "20260911T110000000Z-claude-sonnet-5",
			Model: "claude-sonnet-5", CreatedAt: now.Add(sd.offset), Status: "ok", Headline: sd.title,
			Findings: []observer.Finding{{
				Severity: sd.severity, Owner: sd.owner, Title: sd.title, What: "what",
				Evidence: []observer.Evidence{{Step: 3, Quote: "discovered ok", Verified: true}},
				LookAt:   "x", Fix: "y",
			}},
		})
	}

	body := doGET(t, h, "/").Body.String()

	for _, want := range []string{"High problem one", "High problem two", "Medium problem one", "Medium problem two", "Low problem included"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing top problem %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Low problem excluded") {
		t.Errorf("body shows a sixth, lower-ranked problem:\n%s", body)
	}
	// Item 12 (round-1 follow-up): each row links to its own stable
	// /problems#<id> fragment, not the bare /problems page.
	if n := strings.Count(body, `href="/problems#`); n < 5 {
		t.Errorf("body has %d links to /problems#<id>, want at least 5:\n%s", n, body)
	}
	if !strings.Contains(body, "hit 1/1 runs on") {
		t.Errorf("body missing the \"how often\" phrase:\n%s", body)
	}
}

// TestHome_BatchesTable pins §8.7's Overview-batches list end to end: an
// empty batch (no run finished) is hidden under the default kind and shown
// under kind=empty/all, per-filter-option counts, a sort link reordering
// the table, links keeping the other parameters, and the 400 page for an
// unknown parameter.
func TestHome_BatchesTable(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatchAt(t, store, "bt1", "off", now.Add(-2*time.Hour), []runFixture{
		{runID: "bt1-a", scenario: "a", startedAt: now.Add(-2 * time.Hour), durationS: "5s", costUsd: 0.10, taskResult: "passed", done: true},
	}, true, map[string]string{"bt1-a": "passed"})
	seedBatchAt(t, store, "bt2", "off", now.Add(-1*time.Hour), []runFixture{
		{runID: "bt2-a", scenario: "a", startedAt: now.Add(-1 * time.Hour), durationS: "5s", costUsd: 1.00, taskResult: "passed", done: true},
	}, true, map[string]string{"bt2-a": "passed"})
	seedBatchAt(t, store, "bt3-empty", "off", now.Add(-30*time.Minute), []runFixture{
		{runID: "bt3-empty-a", scenario: "a", startedAt: now.Add(-30 * time.Minute), done: false},
	}, false, nil)

	t.Run("default hides the empty batch", func(t *testing.T) {
		body := doGET(t, h, "/").Body.String()
		if !strings.Contains(body, "bt1") || !strings.Contains(body, "bt2") {
			t.Errorf("body missing the two evaluation batches:\n%s", body)
		}
		if strings.Contains(body, "bt3-empty") {
			t.Errorf("body shows the empty batch under the default kind filter:\n%s", body)
		}
	})

	t.Run("kind=empty narrows to only the empty batch", func(t *testing.T) {
		table := batchesTableSection(t, doGET(t, h, "/?kind=empty").Body.String())
		if !strings.Contains(table, "bt3-empty") {
			t.Errorf("kind=empty table missing the empty batch:\n%s", table)
		}
		if strings.Contains(table, ">bt1<") || strings.Contains(table, ">bt2<") {
			t.Errorf("kind=empty table still shows an evaluation batch:\n%s", table)
		}
	})

	t.Run("kind=all shows every batch", func(t *testing.T) {
		body := doGET(t, h, "/?kind=all").Body.String()
		for _, id := range []string{"bt1", "bt2", "bt3-empty"} {
			if !strings.Contains(body, id) {
				t.Errorf("kind=all body missing batch %q:\n%s", id, body)
			}
		}
	})

	t.Run("filter option counts are shown", func(t *testing.T) {
		body := doGET(t, h, "/").Body.String()
		if !strings.Contains(body, `<span class="filter-count">2</span>`) {
			t.Errorf("body missing the evaluation option's count of 2:\n%s", body)
		}
		if !strings.Contains(body, `<span class="filter-count">1</span>`) {
			t.Errorf("body missing the empty option's count of 1:\n%s", body)
		}
	})

	t.Run("sort=cost reorders the table and links keep kind=all", func(t *testing.T) {
		table := batchesTableSection(t, doGET(t, h, "/?kind=all&sort=cost&dir=asc").Body.String())
		iCheap, iExpensive := strings.Index(table, ">bt1<"), strings.Index(table, ">bt2<")
		if iCheap < 0 || iExpensive < 0 || iCheap > iExpensive {
			t.Errorf("sort=cost&dir=asc did not order the cheaper batch (bt1) before the pricier one (bt2):\nbt1@%d bt2@%d\n%s", iCheap, iExpensive, table)
		}
		if !strings.Contains(table, "kind=all") {
			t.Errorf("sort link dropped the active kind=all filter:\n%s", table)
		}
	})

	t.Run("an unknown parameter renders the 400 page naming the allowed parameters", func(t *testing.T) {
		rr := doGET(t, h, "/?bogus=1")
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("GET /?bogus=1: got %d, want 400, body=%s", rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		if !strings.Contains(body, "bogus") {
			t.Errorf("400 body does not name the refused parameter:\n%s", body)
		}
		if !strings.Contains(body, "<code>kind</code>") {
			t.Errorf("400 body does not list kind among the allowed parameters:\n%s", body)
		}
	})

	t.Run("a bad value for a known filter renders the 400 page naming its allowed values", func(t *testing.T) {
		rr := doGET(t, h, "/?kind=bogus")
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("GET /?kind=bogus: got %d, want 400, body=%s", rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		if !strings.Contains(body, "<code>kind</code>") {
			t.Errorf("400 body does not name the refused parameter:\n%s", body)
		}
		if !strings.Contains(body, "<code>"+batchKindEvaluation+"</code>") {
			t.Errorf("400 body does not name an allowed kind value:\n%s", body)
		}
	})
}

// batchesTableSection isolates the "Batches" section's own markup from the
// rest of the page — the Latest evaluation panel always names the newest
// batch regardless of the Batches list's own `kind`/sort query, so a test
// asserting which batches THAT list shows must not scan the whole body.
func batchesTableSection(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, "<h2>Batches</h2>")
	if i < 0 {
		t.Fatalf("body has no Batches section:\n%s", body)
	}
	return body[i:]
}

// TestHome_DotsOrderedByPrecedenceWithTooltip pins §8.3 item 3's "one dot
// per run ordered as on the batch page": failed/blocked first, then not
// finished, then not assessed, then a problem in a passed run, then clean —
// each dot's tooltip is "scenario — verdict".
func TestHome_DotsOrderedByPrecedenceWithTooltip(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatchAt(t, store, "dt1", "claude-sonnet-5", now.Add(-1*time.Hour), []runFixture{
		{runID: "dt1-sclean", scenario: "sclean", startedAt: now.Add(-1 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "dt1-sfail", scenario: "sfail", startedAt: now.Add(-1 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "failed", done: true},
		{runID: "dt1-snoass", scenario: "snoass", startedAt: now.Add(-1 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "dt1-sprob", scenario: "sprob", startedAt: now.Add(-1 * time.Hour), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
		{runID: "dt1-srun", scenario: "srun", startedAt: now.Add(-1 * time.Minute), done: false},
	}, true, map[string]string{"dt1-sclean": "passed", "dt1-sfail": "failed", "dt1-snoass": "passed", "dt1-sprob": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "dt1-sclean", ObsID: "20260911T110000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now.Add(-1 * time.Hour), Status: "ok", Headline: "all good",
	})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "dt1-sprob", ObsID: "20260911T110000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now.Add(-1 * time.Hour), Status: "ok", Headline: "problem in a passed run", Outcome: observer.OutcomeProblem,
	})

	body := doGET(t, h, "/").Body.String()

	for _, want := range []string{`title="sfail — failed"`, `title="srun — running"`, `title="snoass — passed"`, `title="sprob — passed"`, `title="sclean — passed"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing dot tooltip %s:\n%s", want, body)
		}
	}

	order := []string{"sfail", "srun", "snoass", "sprob", "sclean"}
	positions := make([]int, len(order))
	for i, s := range order {
		positions[i] = strings.Index(body, `title="`+s+` — `)
	}
	for i := 1; i < len(positions); i++ {
		if positions[i-1] < 0 || positions[i] < 0 || positions[i-1] > positions[i] {
			t.Errorf("dots not in batch-page precedence order %v, positions=%v\n%s", order, positions, body)
		}
	}
}

// TestPages_HomeBatchesTableShortDatesAndSetChip pins item 6 (round-1
// follow-up): the Overview batches table shows a short date ("11 Sep
// 18:31") and the set as a chip, not the long "1 Sep 2026, 00:00 UTC" form.
func TestPages_HomeBatchesTableShortDatesAndSetChip(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedBatch(t, store, "sd1", "off", []runFixture{
		{runID: "sd1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"sd1-a": "passed"})

	body := doGET(t, h, "/").Body.String()
	if !strings.Contains(body, "1 Sep 18:31") && !strings.Contains(body, "1 Sep 00:00") {
		t.Errorf("body missing a short date for the batch's Started column:\n%s", body)
	}
	if strings.Contains(body, `data-label="Started">1 Sep 2026,`) {
		t.Errorf("the batches table's Started cell still shows the long date form:\n%s", body)
	}
	if !strings.Contains(body, `data-label="Set"><span class="chip">gate</span>`) {
		t.Errorf("body does not render the Set column as a chip:\n%s", body)
	}
}

// TestPages_HomeTopProblemsLinksOnlyTitleWithStableID pins item 12
// (round-1 follow-up): "Top problems now" links only the problem's title,
// to /problems#<a stable id derived from the problem's key> — the rest of
// the line is muted, not part of the link.
func TestPages_HomeTopProblemsLinksOnlyTitleWithStableID(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "tp1", "claude-sonnet-5", []runFixture{
		{runID: "tp1-a", scenario: "a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"tp1-a": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat2, RunID: "tp1-a", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "x",
		Findings: []observer.Finding{{Severity: "high", Owner: "zcp-tool", Surface: "tool:zerops_deploy/deploy", Title: "always fails this way"}},
	})

	body := doGET(t, h, "/").Body.String()
	linkRE := regexp.MustCompile(`<a href="(/problems#[^"]+)"><strong>always fails this way</strong></a>`)
	m := linkRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("body missing a title-only link to /problems#<id>:\n%s", body)
	}

	problemsBody := doGET(t, h, "/problems").Body.String()
	frag := strings.TrimPrefix(m[1], "/problems")
	if !strings.Contains(problemsBody, `id="`+strings.TrimPrefix(frag, "#")+`"`) {
		t.Errorf("the linked id %q has no matching element on /problems:\n%s", frag, problemsBody)
	}
}

// TestPages_HomeTopProblemsOmitsSurfaceChipWhenEmpty pins item 11 (round-1
// follow-up): a live problem with no surface renders no surface chip.
func TestPages_HomeTopProblemsOmitsSurfaceChipWhenEmpty(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	now := fixedNow(t)()

	seedBatch(t, store, "ns2", "claude-sonnet-5", []runFixture{
		{runID: "ns2-a", scenario: "a", startedAt: now, durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"ns2-a": "passed"})
	seedObservation(t, store, observer.Observation{
		FormatVersion: observer.ObservationFormat1, RunID: "ns2-a", ObsID: "20260911T120000000Z-claude-sonnet-5",
		Model: "claude-sonnet-5", CreatedAt: now, Status: "ok", Outcome: observer.OutcomeProblem, Headline: "x",
		Findings: []observer.Finding{{Severity: "high", Owner: "agent", Title: "agent messed up"}},
	})

	body := doGET(t, h, "/").Body.String()
	if !strings.Contains(body, "agent messed up") {
		t.Fatalf("body missing the problem's title:\n%s", body)
	}
	if strings.Contains(body, `<code class="chip"></code>`) {
		t.Errorf("body renders an empty surface chip:\n%s", body)
	}
}

// TestPages_HomeBatchesTableSortChipsAboveStackedTable pins item 15
// (round-1 follow-up): the Overview batches list renders its sort options
// as a chip row above the table (stacked tables hide thead on phones), like
// /findings already does.
func TestPages_HomeBatchesTableSortChipsAboveStackedTable(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedBatch(t, store, "sc1", "off", []runFixture{
		{runID: "sc1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"sc1-a": "passed"})

	body := doGET(t, h, "/").Body.String()
	if !strings.Contains(body, `<span class="k">Sort</span>`) {
		t.Errorf("body missing a sort chip row above the batches table:\n%s", body)
	}
}

// TestPages_ListPagesUseWideContainer pins item 6's closing note: a list
// page (Overview, Problems, Findings) may use a wider container
// (main.wrap.wide, max 1280px) than a single batch/run's prose.
func TestPages_ListPagesUseWideContainer(t *testing.T) {
	srv, store, _ := testServer(t)
	h := srv.Handler()
	seedBatch(t, store, "wc1", "off", []runFixture{
		{runID: "wc1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", costUsd: 0.1, taskResult: "passed", done: true},
	}, true, map[string]string{"wc1-a": "passed"})

	for _, route := range []string{"/", "/problems", "/findings"} {
		body := doGET(t, h, route).Body.String()
		if !strings.Contains(body, `<main class="wrap wide">`) {
			t.Errorf("GET %s: main is not the wide container:\n%s", route, body)
		}
	}
	for _, route := range []string{"/b/wc1", "/r/wc1-a"} {
		body := doGET(t, h, route).Body.String()
		if strings.Contains(body, `<main class="wrap wide">`) {
			t.Errorf("GET %s: a detail page must not use the wide container:\n%s", route, body)
		}
	}
}
