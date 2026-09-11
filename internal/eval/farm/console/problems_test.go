// Package console: RED/GREEN tests for §8.6 problem clustering
// (plans/farm-console-clarity-2026-09-11-briefs/MODEL.md item 4).
package console

import (
	"net/url"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// --- norm() (§8.6 anchor normalization) -----------------------------------

func TestNorm(t *testing.T) {
	tests := []struct {
		name      string
		s         string
		hostnames []string
		want      string
	}{
		{"hostname replaced as whole token", "service api1 failed to start", []string{"api1"}, "service <host> failed to start"},
		{"hostname inside a longer word is not replaced", "the api1x service failed", []string{"api1"}, "the api1x service failed"},
		{"22-char base62 token replaced", "object cCAaI7SbTz2Y2h1Jt3k9zz not found", nil, "object # not found"},
		{"8+ hex token replaced", "commit deadbeef1234 pushed", nil, "commit # pushed"},
		{"short hex-like token under 8 chars kept", "code ab12cd is fine", nil, "code ab12cd is fine"},
		{"digit run replaced", "retry after 404 ms 12", nil, "retry after # ms #"},
		{"markdown stripped then lowered", "Do **NOT** `override`", nil, "do not override"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := norm(tc.s, tc.hostnames); got != tc.want {
				t.Errorf("norm(%q) = %q, want %q", tc.s, got, tc.want)
			}
		})
	}
}

// --- maskRunSpecific / maskLiteralToken (item 1) ----------------------------

func TestMaskLiteralToken(t *testing.T) {
	tests := []struct {
		name          string
		s, needle, rp string
		want          string
	}{
		{"whole-token literal with internal hyphens replaced", "work/final1-api-node/x", "final1-api-node", "<runpath>", "work/<runpath>/x"},
		{"not replaced when part of a longer token", "work/xfinal1-api-nodey/x", "final1-api-node", "<runpath>", "work/xfinal1-api-nodey/x"},
		{"empty needle is a no-op", "unchanged", "", "<runpath>", "unchanged"},
		{"multiple occurrences all replaced", "a/r1/b/r1/c", "r1", "<x>", "a/<x>/b/<x>/c"},
		{"needle absent leaves s untouched", "no match here", "r1", "<x>", "no match here"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := maskLiteralToken(tc.s, tc.needle, tc.rp); got != tc.want {
				t.Errorf("maskLiteralToken(%q, %q, %q) = %q, want %q", tc.s, tc.needle, tc.rp, got, tc.want)
			}
		})
	}
}

func TestMaskRunSpecific(t *testing.T) {
	tests := []struct {
		name                      string
		s, runID, batch, scenario string
		want                      string
	}{
		{
			"this run's own id masked inside its .zcp-farm path",
			"source mount /home/zerops/.zcp-farm/final1-api-node-postgres-classic-dev/work/apidev missing",
			"final1-api-node-postgres-classic-dev", "final1", "api-node-postgres-classic-dev",
			"source mount /home/zerops/<runpath>/work/apidev missing",
		},
		{
			"a stray OTHER run's .zcp-farm path also collapses",
			"cross-deploy: source zerops.yaml at /home/zerops/.zcp-farm/gate9-other-run/work/x missing",
			"gate9-this-run", "gate9", "this-run",
			"cross-deploy: source zerops.yaml at /home/zerops/<runpath>/work/x missing",
		},
		{
			"no .zcp-farm path, no id occurrence: untouched",
			"PREFLIGHT_FAILED: zerops.yaml not found",
			"r1", "b1", "s1",
			"PREFLIGHT_FAILED: zerops.yaml not found",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := maskRunSpecific(tc.s, tc.runID, tc.batch, tc.scenario); got != tc.want {
				t.Errorf("maskRunSpecific(%q, ...) = %q, want %q", tc.s, got, tc.want)
			}
		})
	}
}

// --- problemKey (§8.6 clustering keys) ------------------------------------

func TestProblemKey(t *testing.T) {
	f2anchor1 := FindingRow{FormatVersion: 2, Surface: "tool:zerops_deploy/deploy", Anchor: "override the running app", RunID: "r1"}
	f2anchor2 := FindingRow{FormatVersion: 2, Surface: "tool:zerops_deploy/other-action", Anchor: "override the running app", RunID: "r2"}
	f2noAnchorSameScenario1 := FindingRow{FormatVersion: 2, Surface: "tool:zerops_deploy/x", Owner: "zcp-tool", Scenario: "s1", RunID: "r3"}
	f2noAnchorSameScenario2 := FindingRow{FormatVersion: 2, Surface: "tool:zerops_deploy/y", Owner: "zcp-tool", Scenario: "s1", RunID: "r4"}
	f2noAnchorDiffScenario := FindingRow{FormatVersion: 2, Surface: "tool:zerops_deploy/x", Owner: "zcp-tool", Scenario: "s2", RunID: "r5"}
	f2agent := FindingRow{FormatVersion: 2, Surface: "agent", RunID: "r6", Index: 0}
	f2agentOther := FindingRow{FormatVersion: 2, Surface: "agent", RunID: "r7", Index: 0}
	f1a := FindingRow{FormatVersion: 1, RunID: "r8", Index: 0}
	f1b := FindingRow{FormatVersion: 1, RunID: "r8", Index: 1}

	if k1, k2 := problemKey(f2anchor1, nil), problemKey(f2anchor2, nil); k1 != k2 {
		t.Errorf("same anchor, different action suffix: keys differ: %q vs %q", k1, k2)
	}
	if k1, k2 := problemKey(f2noAnchorSameScenario1, nil), problemKey(f2noAnchorSameScenario2, nil); k1 != k2 {
		t.Errorf("no-anchor same owner/scenario: keys differ: %q vs %q", k1, k2)
	}
	if k1, k2 := problemKey(f2noAnchorSameScenario1, nil), problemKey(f2noAnchorDiffScenario, nil); k1 == k2 {
		t.Errorf("no-anchor different scenario: keys should differ, both %q", k1)
	}
	if k1, k2 := problemKey(f2agent, nil), problemKey(f2agentOther, nil); k1 == k2 {
		t.Errorf("agent-surface findings from different runs must never cluster, both %q", k1)
	}
	if k1, k2 := problemKey(f1a, nil), problemKey(f1b, nil); k1 == k2 {
		t.Errorf("format-1 findings must never cluster (each its own problem), both %q", k1)
	}
}

// --- BuildProblems: clustering + status/rank (§8.6) -----------------------

// pRun builds one ProblemsRun fixture: a gate-set batch (every test in this
// file uses gate batches — BatchSet's own effect on newestBuildSha is
// covered by other tests) with a current ok observation carrying findings
// (or a clean "ok" outcome when there are none).
func pRun(runID, batch, scenario, build string, batchCreated time.Time, startedAt time.Time, findings ...observer.Finding) ProblemsRun {
	outcome := observer.OutcomeOK
	if len(findings) > 0 {
		outcome = observer.OutcomeProblem
	}
	obs := &observer.Observation{
		Status: "ok", FormatVersion: observer.ObservationFormat2, Outcome: outcome,
		Findings: findings,
	}
	row := RunRow{
		RunID: runID, Batch: batch, Scenario: scenario, StartedAt: startedAt,
		Build: BuildInfo{Sha256: build}, Observation: obs, Outcome: obs.Outcome,
	}
	return ProblemsRun{Row: row, BatchSet: "gate", BatchCreatedAt: batchCreated}
}

// TestBuildProblems_StatusAcrossThreeBuilds pins §8.6's five statuses: a
// problem hit on build1 and build3 (the newest) is recurring; one hit only
// on build3 whose scenario was clean on build1/build2 is new (a
// regression); one hit only on build3 whose scenario was never assessed
// before is first seen; one hit only on build1 (not build3), whose
// scenario WAS assessed clean on build3, is gone; one hit only on build1,
// whose scenario was never assessed on build3 at all, is unconfirmed.
func TestBuildProblems_StatusAcrossThreeBuilds(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	finding := func(title, surface, anchor string) observer.Finding {
		return observer.Finding{Severity: "high", Owner: "zcp-tool", Title: title, Surface: surface, Anchor: anchor, Fix: "fix-" + title}
	}

	runs := make([]ProblemsRun, 0, 8)
	// build1 (oldest), build2 (middle), build3 (newest) — all gate batches.
	runs = append(runs, pRun("b1-recur", "b1", "recurring-scn", "build1", day(1), day(1),
		finding("recurring", "tool:x", "recurring anchor")))
	runs = append(runs, pRun("b1-gone", "b1", "gone-scn", "build1", day(1), day(1),
		finding("gone", "tool:x", "gone anchor")))
	runs = append(runs, pRun("b1-unconfirmed", "b1", "unconfirmed-scn", "build1", day(1), day(1),
		finding("unconfirmed", "tool:x", "unconfirmed anchor")))

	runs = append(runs, pRun("b2-regresses", "b2", "regresses-scn", "build2", day(2), day(2))) // clean on build2

	runs = append(runs, pRun("b3-recur", "b3", "recurring-scn", "build3", day(3), day(3),
		finding("recurring", "tool:x", "recurring anchor")))
	runs = append(runs, pRun("b3-gone", "b3", "gone-scn", "build3", day(3), day(3))) // clean now
	runs = append(runs, pRun("b3-newregress", "b3", "regresses-scn", "build3", day(3), day(3),
		finding("new", "tool:x", "new anchor")))
	runs = append(runs, pRun("b3-firstseen", "b3", "first-seen-scn", "build3", day(3), day(3),
		finding("first seen", "tool:x", "first seen anchor")))
	// unconfirmed-scn is never assessed on build3 at all.

	problems := BuildProblems(runs)

	byTitle := make(map[string]Problem)
	for _, p := range problems {
		byTitle[p.Members[0].Title] = p
	}

	tests := []struct {
		title string
		want  string
	}{
		{"recurring", "recurring"},
		{"new", "new"},
		{"first seen", StatusFirstSeen},
		{"gone", "gone"},
		{"unconfirmed", "unconfirmed"},
	}
	for _, tc := range tests {
		p, ok := byTitle[tc.title]
		if !ok {
			t.Errorf("no problem with title %q; got titles %v", tc.title, keysOf(byTitle))
			continue
		}
		if p.Status != tc.want {
			t.Errorf("problem %q: Status = %q, want %q", tc.title, p.Status, tc.want)
		}
	}
}

func keysOf(m map[string]Problem) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestBuildProblems_FilterLeavesStatusUnchanged pins §8.6: "every other
// filter narrows rows and never changes a status" — status is baked in at
// BuildProblems time over the full since-window set, not recomputed when
// a caller later narrows by batch/scenario.
func TestBuildProblems_FilterLeavesStatusUnchanged(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	finding := func() observer.Finding {
		return observer.Finding{Severity: "high", Owner: "zcp-tool", Title: "x", Surface: "tool:x", Anchor: "anchor"}
	}
	runs := []ProblemsRun{
		pRun("b1-a", "b1", "s1", "build1", day(1), day(1), finding()),
		pRun("b2-a", "b2", "s1", "build2", day(2), day(2), finding()),
	}
	problems := BuildProblems(runs)
	if len(problems) != 1 {
		t.Fatalf("got %d problems, want 1", len(problems))
	}
	if problems[0].Status != "recurring" {
		t.Fatalf("Status = %q, want recurring", problems[0].Status)
	}
	// The same set, unfiltered by the caller here (BuildProblems itself
	// takes no filters — filtering is query.go's job, item 5) still
	// reports the identical status when re-run over the identical input.
	again := BuildProblems(runs)
	if again[0].Status != problems[0].Status {
		t.Errorf("status changed across identical BuildProblems calls: %q vs %q", again[0].Status, problems[0].Status)
	}
}

// TestBuildProblems_ClustersRunSpecificAnchors pins item 1 (FIX2.md
// FIX2-DATA): before norm's existing masking, a finding's own run id, batch
// id and scenario are masked as whole tokens, and any
// ".zcp-farm/<anything>/" path segment collapses to "<runpath>/", so the
// SAME underlying bug reported by independent runs clusters instead of one
// problem per run.
//
// The anchors below are copied verbatim from a real farm bucket's
// /api/problems.json?status=all&since=30d (batch final1, live-checked
// 2026-09-12): the SAME zerops_deploy cross-deploy preflight bug,
// independently observed on six runs. The fix clusters the three runs
// whose anchor is worded identically apart from the run path into ONE
// problem (matching the brief's own illustrative example, the
// api-node-postgres-classic-dev run) — but not all six into one: the
// model wrote three distinct WORDINGS of the same bug across these six
// runs (a plain "source mount ... missing"; one adding a trailing
// "— scaffold zerops.yaml for service ... there"; one adding BOTH that
// suffix and a leading "zerops.yaml not found or invalid:"). §8.6
// clustering is deterministic token masking ("no model is called"), so it
// clusters same-wording anchors together but cannot also bridge a wording
// difference beyond the run path — that residual gap (six runs → three
// clusters, not one) is exactly what item 2's prompt.md rule (pick a
// run-independent anchor going forward) exists to close for future runs.
func TestBuildProblems_ClustersRunSpecificAnchors(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }

	mk := func(runID, scenario, host, anchor string) ProblemsRun {
		f := observer.Finding{
			Severity: "high", Owner: "zcp-tool", Title: "Deploy preflight path disagrees with mount path",
			Surface: "tool:zerops_deploy", Anchor: anchor, Fix: "fix",
		}
		obs := &observer.Observation{
			Status: "ok", FormatVersion: observer.ObservationFormat2, Outcome: observer.OutcomeProblem,
			Findings: []observer.Finding{f},
		}
		row := RunRow{
			RunID: runID, Batch: "final1", Scenario: scenario, StartedAt: day(11),
			Build: BuildInfo{Sha256: "buildfinal1"}, Observation: obs, Outcome: obs.Outcome,
			ServiceHostnames: []string{host},
		}
		return ProblemsRun{Row: row, BatchSet: "gate", BatchCreatedAt: day(11)}
	}

	plain := []ProblemsRun{
		mk("final1-recover-failed-buildfromgit-missing-dep", "recover-failed-buildfromgit-missing-dep", "api",
			`source mount /home/zerops/.zcp-farm/final1-recover-failed-buildfromgit-missing-dep/work/api missing`),
		mk("final1-api-node-postgres-classic-dev", "api-node-postgres-classic-dev", "apidev",
			`source mount /home/zerops/.zcp-farm/final1-api-node-postgres-classic-dev/work/apidev missing`),
		mk("final1-recipe-nestjs-minimal-standard", "recipe-nestjs-minimal-standard", "appdev",
			`source mount /home/zerops/.zcp-farm/final1-recipe-nestjs-minimal-standard/work/appdev missing`),
	}
	suffixed := []ProblemsRun{
		mk("final1-develop-add-managed-dep-to-existing", "develop-add-managed-dep-to-existing", "appdev",
			`source mount /home/zerops/.zcp-farm/final1-develop-add-managed-dep-to-existing/work/appdev missing — scaffold zerops.yaml for service "appdev" there`),
		mk("final1-greenfield-node-postgres-dev-stage", "greenfield-node-postgres-dev-stage", "appdev",
			`source mount /home/zerops/.zcp-farm/final1-greenfield-node-postgres-dev-stage/work/appdev missing — scaffold zerops.yaml for service "appdev" there`),
	}
	prefixedAndSuffixed := []ProblemsRun{
		mk("final1-existing-standard-appdev-only-reminders", "existing-standard-appdev-only-reminders", "appdev",
			`zerops.yaml not found or invalid: source mount /home/zerops/.zcp-farm/final1-existing-standard-appdev-only-reminders/work/appdev missing — scaffold zerops.yaml for service "appdev" there`),
	}
	// Two unrelated real anchors (a different bug) must never merge in.
	unrelated := []ProblemsRun{
		mk("final1-unrelated-events", "unrelated-events", "svc",
			`The build/deploy failure timeline (failureClass + cause) is in zerops_events, not this stream.`),
		mk("final1-unrelated-timeout", "unrelated-timeout", "svc",
			`context deadline exceeded waiting for the process to finish`),
	}

	groups := [][]ProblemsRun{plain, suffixed, prefixedAndSuffixed, unrelated}
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	runs := make([]ProblemsRun, 0, total)
	for _, group := range groups {
		runs = append(runs, group...)
	}
	problems := BuildProblems(runs)

	memberCountFor := func(runID string) int {
		for _, p := range problems {
			for _, m := range p.Members {
				if m.RunID == runID {
					return len(p.Members)
				}
			}
		}
		t.Fatalf("no problem contains run %q", runID)
		return -1
	}

	for _, r := range plain {
		if got := memberCountFor(r.Row.RunID); got != len(plain) {
			t.Errorf("run %q: problem has %d members, want %d (the three identically-worded anchors)", r.Row.RunID, got, len(plain))
		}
	}
	for _, r := range suffixed {
		if got := memberCountFor(r.Row.RunID); got != len(suffixed) {
			t.Errorf("run %q: problem has %d members, want %d (the two suffixed anchors)", r.Row.RunID, got, len(suffixed))
		}
	}
	if got := memberCountFor(prefixedAndSuffixed[0].Row.RunID); got != 1 {
		t.Errorf("prefixed+suffixed anchor: problem has %d members, want 1 (its own wording is unique)", got)
	}
	for _, r := range unrelated {
		if got := memberCountFor(r.Row.RunID); got != 1 {
			t.Errorf("unrelated anchor %q: problem has %d members, want 1 (must never merge)", r.Row.RunID, got)
		}
	}
	if len(problems) != 5 { // 3 + 2 + 1 + 2 unrelated
		t.Errorf("got %d problems, want 5", len(problems))
	}
}

// --- item 7: "a" counts distinct runs; batch-local hit/assessed -----------

// TestBuildProblems_HitOnNewestBuildCountsDistinctRuns pins item 7 (FIX2.md
// FIX2-DATA): "a" (HitOnNewestBuild) is the number of DISTINCT runs hit,
// not the number of clustered findings/members — a single run whose one
// observation contributes two findings to the SAME problem must count as
// one run hit, not two.
func TestBuildProblems_HitOnNewestBuildCountsDistinctRuns(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	f1 := observer.Finding{Severity: "high", Owner: "zcp-tool", Title: "first", Surface: "tool:x"}
	f2 := observer.Finding{Severity: "medium", Owner: "zcp-tool", Title: "second", Surface: "tool:x"}
	runs := []ProblemsRun{
		pRun("r1", "b1", "s1", "build1", day(1), day(1), f1, f2),
	}
	problems := BuildProblems(runs)
	if len(problems) != 1 {
		t.Fatalf("got %d problems, want 1 (both findings share the no-anchor tool:x/zcp-tool/s1 key)", len(problems))
	}
	if len(problems[0].Members) != 2 {
		t.Fatalf("got %d members, want 2", len(problems[0].Members))
	}
	if problems[0].HitOnNewestBuild != 1 {
		t.Errorf("HitOnNewestBuild = %d, want 1 (one distinct run, two findings)", problems[0].HitOnNewestBuild)
	}
}

// TestBuildProblems_BatchLocalHitAndAssessedCounts pins item 7's addition:
// RunsHitByBatch/RunsAssessedByBatch let a batch page say "hit N of M runs
// in this batch" — scoped to one batch, unlike HitOnNewestBuild/
// RunsAssessedOnNewest, which are scoped to "the newest build" across
// every batch in the since window.
func TestBuildProblems_BatchLocalHitAndAssessedCounts(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	finding := func() observer.Finding {
		return observer.Finding{Severity: "high", Owner: "zcp-tool", Title: "x", Surface: "tool:x", Anchor: "same anchor"}
	}
	runs := []ProblemsRun{
		// batchA: 3 runs of s1 assessed, 2 hit.
		pRun("a-hit1", "batchA", "s1", "build1", day(1), day(1), finding()),
		pRun("a-hit2", "batchA", "s1", "build1", day(1), day(1), finding()),
		pRun("a-clean", "batchA", "s1", "build1", day(1), day(1)),
		// batchB: 1 run of s1 assessed, hits.
		pRun("b-hit1", "batchB", "s1", "build2", day(2), day(2), finding()),
	}
	problems := BuildProblems(runs)
	if len(problems) != 1 {
		t.Fatalf("got %d problems, want 1", len(problems))
	}
	p := problems[0]
	if got := p.RunsHitByBatch["batchA"]; got != 2 {
		t.Errorf("RunsHitByBatch[batchA] = %d, want 2", got)
	}
	if got := p.RunsAssessedByBatch["batchA"]; got != 3 {
		t.Errorf("RunsAssessedByBatch[batchA] = %d, want 3", got)
	}
	if got := p.RunsHitByBatch["batchB"]; got != 1 {
		t.Errorf("RunsHitByBatch[batchB] = %d, want 1", got)
	}
	if got := p.RunsAssessedByBatch["batchB"]; got != 1 {
		t.Errorf("RunsAssessedByBatch[batchB] = %d, want 1", got)
	}
}

// --- TestLists_Problems (item 5, §8.7) -------------------------------------

func TestLists_Problems(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	mkProblem := func(status, severity, cause, scenario, batch string, last time.Time) Problem {
		return Problem{
			Status: status, Severity: severity, RunsTotal: 1, LastSeen: last, FirstSeen: last,
			Members: []ProblemMember{{Scenario: scenario, Batch: batch, CauseClass: cause, Severity: severity}},
		}
	}
	problems := []Problem{
		mkProblem(StatusRecurring, observer.SeverityHigh, CauseClassZCP, "s1", "b1", day(3)),
		mkProblem(StatusGone, observer.SeverityLow, CauseClassAgent, "s2", "b1", day(1)),
		mkProblem(StatusNew, observer.SeverityMedium, CauseClassTest, "s3", "b2", day(2)),
	}
	eng := problemEngine()

	t.Run("status default is live", func(t *testing.T) {
		q, err := Parse(problemListSpec(), url.Values{})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(problems, q, day(10))
		if len(got) != 2 {
			t.Fatalf("got %d, want 2 (recurring + new; gone is not live)", len(got))
		}
	})

	t.Run("status=all", func(t *testing.T) {
		q, err := Parse(problemListSpec(), url.Values{"status": {"all"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(problems, q, day(10))
		if len(got) != 3 {
			t.Fatalf("got %d, want 3", len(got))
		}
	})

	t.Run("cause narrows to matching members", func(t *testing.T) {
		q, err := Parse(problemListSpec(), url.Values{"status": {"all"}, "cause": {"zcp"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(problems, q, day(10))
		if len(got) != 1 || got[0].Members[0].Scenario != "s1" {
			t.Fatalf("got %+v, want only the zcp-cause problem", got)
		}
	})

	t.Run("severity is a minimum", func(t *testing.T) {
		q, err := Parse(problemListSpec(), url.Values{"status": {"all"}, "severity": {"medium"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(problems, q, day(10))
		if len(got) != 2 {
			t.Fatalf("got %d, want 2 (high and medium, not low)", len(got))
		}
	})

	t.Run("scenario open filter", func(t *testing.T) {
		q, err := Parse(problemListSpec(), url.Values{"status": {"all"}, "scenario": {"s2"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(problems, q, day(10))
		if len(got) != 1 {
			t.Fatalf("got %d, want 1", len(got))
		}
	})

	t.Run("sort=last desc default direction", func(t *testing.T) {
		q, err := Parse(problemListSpec(), url.Values{"status": {"all"}, "sort": {"last"}})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		got, _ := eng.Apply(problems, q, day(10))
		if got[0].LastSeen != day(3) {
			t.Errorf("got[0].LastSeen = %v, want day 3 (newest first)", got[0].LastSeen)
		}
	})

	t.Run("unknown status value refused", func(t *testing.T) {
		if _, err := Parse(problemListSpec(), url.Values{"status": {"bogus"}}); err == nil {
			t.Error("want an error for an unknown status value")
		}
	})

	t.Run("unknown parameter refused", func(t *testing.T) {
		if _, err := Parse(problemListSpec(), url.Values{"bogus": {"x"}}); err == nil {
			t.Error("want an error for an unknown parameter")
		}
	})

	t.Run("since default is 30d", func(t *testing.T) {
		q, err := Parse(problemListSpec(), url.Values{})
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if q.Since != 30*24*time.Hour {
			t.Errorf("Since = %v, want 30d", q.Since)
		}
	})
}
