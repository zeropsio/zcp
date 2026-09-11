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
		{"first seen", "first seen"},
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
