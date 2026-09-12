// Package console: RED/GREEN tests for §8.6 problem clustering
// (plans/farm-console-clarity-2026-09-11-briefs/MODEL.md item 4).
package console

import (
	"fmt"
	"net/url"
	"strings"
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
// FIX2-DATA) plus its round-2 addendum (final verification round): before
// norm's existing masking, a finding's own run id, batch id and scenario
// are masked as whole tokens, and any ".zcp-farm/<anything>/" path segment
// collapses to "<runpath>/"; THEN two anchor-kind problems under the same
// surface prefix merge when one's normalized anchor contains the other
// (mergeContainedAnchorClusters) — so the SAME underlying bug reported by
// independent runs clusters into ONE problem, even when the model wrote
// several different WORDINGS of it, not just different run paths.
//
// The anchors below are copied verbatim from a real farm bucket's
// /api/problems.json?status=all&since=30d (batch final1, live-checked
// 2026-09-12): the SAME zerops_deploy cross-deploy preflight bug,
// independently observed on six runs, in three distinct wordings (a plain
// "source mount ... missing"; one adding a trailing "— scaffold
// zerops.yaml for service ... there"; one adding BOTH that suffix and a
// leading "zerops.yaml not found or invalid:"). Round 1 alone (masking
// only) clustered same-wording runs together — three groups of 3+2+1, not
// one; the plain wording is always a substring of the suffixed one, which
// is always a substring of the prefixed+suffixed one, so the containment
// merge chains all three groups into a single problem with all six
// members (a reviewer measured this exact defect at 42 problems → 1 over
// the full live bucket, §8.6).
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

	wantMerged := 0
	for _, group := range [][]ProblemsRun{plain, suffixed, prefixedAndSuffixed} {
		wantMerged += len(group)
	}
	for _, group := range [][]ProblemsRun{plain, suffixed, prefixedAndSuffixed} {
		for _, r := range group {
			if got := memberCountFor(r.Row.RunID); got != wantMerged {
				t.Errorf("run %q: problem has %d members, want %d (all three wordings merge via containment)", r.Row.RunID, got, wantMerged)
			}
		}
	}
	for _, r := range unrelated {
		if got := memberCountFor(r.Row.RunID); got != 1 {
			t.Errorf("unrelated anchor %q: problem has %d members, want 1 (must never merge)", r.Row.RunID, got)
		}
	}
	if len(problems) != 3 { // 1 merged deploy-preflight problem + 2 unrelated
		t.Errorf("got %d problems, want 3", len(problems))
	}
}

// TestBuildProblems_ClustersRouteMenuWordings pins the same round-2
// addendum (item 1) against a second real defect: five /problems rows
// (batch final1's /api/problems.json?status=all&since=30d, live-checked
// 2026-09-12) for the SAME route-menu bug across seven runs (gate6, gate7,
// gate8, gate9, gate10, gate11, final1's recipe-nestjs-minimal-standard
// scenario), each anchor a differently-worded superset or subset of "no
// recipe template" — a reviewer measured this exact defect at 5 problems →
// 1 over the full live bucket.
func TestBuildProblems_ClustersRouteMenuWordings(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	mk := func(runID, batch, anchor string) ProblemsRun {
		f := observer.Finding{
			Severity: "medium", Owner: "zcp-tool", Title: "Route-menu never offered the recipe",
			Surface: "tool:zerops_workflow", Anchor: anchor, Fix: "fix",
		}
		obs := &observer.Observation{
			Status: "ok", FormatVersion: observer.ObservationFormat2, Outcome: observer.OutcomeProblem,
			Findings: []observer.Finding{f},
		}
		row := RunRow{
			RunID: runID, Batch: batch, Scenario: "recipe-nestjs-minimal-standard", StartedAt: day(11),
			Build: BuildInfo{Sha256: "buildroutemenu"}, Observation: obs, Outcome: obs.Outcome,
		}
		return ProblemsRun{Row: row, BatchSet: "gate", BatchCreatedAt: day(11)}
	}
	runs := []ProblemsRun{
		mk("gate9-recipe-nestjs-minimal-standard", "gate9", `"route":"classic","why":"Manual plan — user describes services directly, no recipe template."`),
		mk("final1-recipe-nestjs-minimal-standard", "final1", `"route":"classic","why":"Manual plan — user describes services directly, no recipe template."`),
		mk("gate7-recipe-nestjs-minimal-standard", "gate7", `no recipe template`),
		mk("gate11-recipe-nestjs-minimal-standard", "gate11", `no recipe template`),
		mk("gate10-recipe-nestjs-minimal-standard", "gate10", `Manual plan — user describes services directly, no recipe template.`),
		mk("gate8-recipe-nestjs-minimal-standard", "gate8", `"routeOptions":[{"route":"classic","why":"Manual plan — user describes services directly, no recipe template."}]`),
		mk("gate6-recipe-nestjs-minimal-standard", "gate6", `no recipe template.`),
	}

	problems := BuildProblems(runs)
	if len(problems) != 1 {
		t.Fatalf("got %d problems, want 1 (all seven route-menu anchors merge via containment)", len(problems))
	}
	if len(problems[0].Members) != len(runs) {
		t.Errorf("got %d members, want %d (one per run)", len(problems[0].Members), len(runs))
	}
}

// --- mergeContainedAnchorClusters / splitAnchorKey (item 1 round 2) -------

func TestSplitAnchorKey(t *testing.T) {
	prefix, anchor, ok := splitAnchorKey("anchor|tool:zerops_deploy|source mount missing")
	if !ok || prefix != "tool:zerops_deploy" || anchor != "source mount missing" {
		t.Errorf("got (%q, %q, %v), want (tool:zerops_deploy, source mount missing, true)", prefix, anchor, ok)
	}
	// Extra "|" characters inside the anchor text stay intact — only the
	// first two pipes are structural.
	prefix, anchor, ok = splitAnchorKey(`anchor|tool:x|a|b|c`)
	if !ok || prefix != "tool:x" || anchor != "a|b|c" {
		t.Errorf("got (%q, %q, %v), want (tool:x, a|b|c, true)", prefix, anchor, ok)
	}
	if _, _, ok := splitAnchorKey("noanchor|tool:x|owner|scenario"); ok {
		t.Error("noanchor key: ok = true, want false")
	}
	if _, _, ok := splitAnchorKey("solo|r1|0"); ok {
		t.Error("solo key: ok = true, want false")
	}
}

func TestMergeContainedAnchorClusters(t *testing.T) {
	m := func(runID string) []ProblemMember { return []ProblemMember{{RunID: runID}} }

	t.Run("shorter anchor contained in longer, same prefix: merges", func(t *testing.T) {
		clusters := map[string][]ProblemMember{
			"anchor|tool:x|no recipe template":                        m("r1"),
			"anchor|tool:x|manual plan, no recipe template available": m("r2"),
		}
		order := []string{"anchor|tool:x|no recipe template", "anchor|tool:x|manual plan, no recipe template available"}
		newClusters, newOrder := mergeContainedAnchorClusters(clusters, order)
		if len(newOrder) != 1 {
			t.Fatalf("got %d clusters, want 1: %v", len(newOrder), newOrder)
		}
		if got := len(newClusters[newOrder[0]]); got != 2 {
			t.Errorf("merged cluster has %d members, want 2", got)
		}
		// The longer anchor's key survives.
		if _, anchor, _ := splitAnchorKey(newOrder[0]); anchor != "manual plan, no recipe template available" {
			t.Errorf("surviving anchor = %q, want the longer one", anchor)
		}
	})

	t.Run("different surface prefix: never merges even if contained", func(t *testing.T) {
		clusters := map[string][]ProblemMember{
			"anchor|tool:x|no recipe template":     m("r1"),
			"anchor|tool:y|no recipe template etc": m("r2"),
		}
		order := []string{"anchor|tool:x|no recipe template", "anchor|tool:y|no recipe template etc"}
		_, newOrder := mergeContainedAnchorClusters(clusters, order)
		if len(newOrder) != 2 {
			t.Errorf("got %d clusters, want 2 (different prefixes never merge)", len(newOrder))
		}
	})

	t.Run("shorter side under 12 chars: never merges", func(t *testing.T) {
		clusters := map[string][]ProblemMember{
			"anchor|tool:x|phase: idle":                     m("r1"), // 10 chars
			"anchor|tool:x|reports phase: idle after retry": m("r2"),
		}
		order := []string{"anchor|tool:x|phase: idle", "anchor|tool:x|reports phase: idle after retry"}
		_, newOrder := mergeContainedAnchorClusters(clusters, order)
		if len(newOrder) != 2 {
			t.Errorf("got %d clusters, want 2 (shorter anchor is under the 12-char floor)", len(newOrder))
		}
	})

	t.Run("transitive: A contains B, A contains C, B and C unrelated directly", func(t *testing.T) {
		a := "no recipe template" // 19 chars, the shared hub
		b := "manual plan, " + a
		c := a + ", scaffold it"
		clusters := map[string][]ProblemMember{
			"anchor|tool:x|" + a: m("r1"),
			"anchor|tool:x|" + b: m("r2"),
			"anchor|tool:x|" + c: m("r3"),
		}
		order := []string{"anchor|tool:x|" + a, "anchor|tool:x|" + b, "anchor|tool:x|" + c}
		_, newOrder := mergeContainedAnchorClusters(clusters, order)
		if len(newOrder) != 1 {
			t.Fatalf("got %d clusters, want 1 (transitive merge through the shared hub)", len(newOrder))
		}
	})

	t.Run("noanchor/solo keys pass through untouched", func(t *testing.T) {
		clusters := map[string][]ProblemMember{
			"noanchor|tool:x|zcp-tool|s1": m("r1"),
			"solo|r2|0":                   m("r2"),
		}
		order := []string{"noanchor|tool:x|zcp-tool|s1", "solo|r2|0"}
		newClusters, newOrder := mergeContainedAnchorClusters(clusters, order)
		if len(newOrder) != 2 {
			t.Errorf("got %d clusters, want 2 (unaffected)", len(newOrder))
		}
		if len(newClusters["noanchor|tool:x|zcp-tool|s1"]) != 1 || len(newClusters["solo|r2|0"]) != 1 {
			t.Errorf("newClusters = %+v, want both keys unchanged", newClusters)
		}
	})
}

// TestProblems_MergedAnchorVariant_StillEmitted pins §8.6's rule that a
// merged problem searches every member wording. The longest representative
// anchor may disappear while a shorter accepted wording remains in steps.
func TestProblems_MergedAnchorVariant_StillEmitted(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	finding := func(anchor string) observer.Finding {
		return observer.Finding{Severity: "high", Owner: "zcp-tool", Title: "same", Surface: "tool:x", Anchor: anchor}
	}
	short := "source mount missing in deployment"
	long := "platform reports source mount missing in deployment after retry"
	oldShort := pRun("mv-short", "mv-old", "s1", "build-old", day(1), day(1), finding(short))
	oldLong := pRun("mv-long", "mv-old", "s1", "build-old", day(1), day(2), finding(long))
	newClean := pRun("mv-new", "mv-new", "s1", "build-new", day(2), day(3))
	all := []ProblemsRun{oldShort, oldLong, newClean}

	problems := BuildProblemsScoped(all, map[string]bool{"mv-short": true}, func(runID string) (string, bool) {
		if runID == "mv-new" {
			return short, true
		}
		return "", false
	})
	if len(problems) != 1 {
		t.Fatalf("got %d problems, want one merged problem", len(problems))
	}
	if got := problems[0].Status; got != StatusStillEmitted {
		t.Fatalf("merged problem status = %q, want still-emitted from the shorter member variant", got)
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

// --- item 3 (FIX2 round 2): status independent of the request's scope ----

// TestBuildProblemsScoped_StatusIndependentOfScope pins item 3: a caller
// that has only its own narrow scope's runs (e.g. one batch's digest) sees
// a problem that is really recurring across history as merely
// "first-seen" — BuildProblems, given only that scope, cannot know
// otherwise. BuildProblemsScoped, given the FULL run history plus an
// inScope set, reports the TRUE status (and all-history totals)
// regardless of scope; only the returned InScopeHit reflects the narrower
// scope.
func TestBuildProblemsScoped_StatusIndependentOfScope(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	finding := func() observer.Finding {
		return observer.Finding{Severity: "high", Owner: "zcp-tool", Title: "x", Surface: "tool:x", Anchor: "recurring problem text here"}
	}
	oldRun := pRun("old-hit", "b-old", "s1", "build-old", day(1), day(1), finding())
	newRun := pRun("new-hit", "b-new", "s1", "build-new", day(2), day(2), finding())
	allRuns := []ProblemsRun{oldRun, newRun}

	// The bug item 3 fixes: given only the new batch's own run, the plain
	// (pre-item-3-shaped) call reports first-seen — it never learns the
	// older build hit the same problem too.
	scopedOnly := BuildProblems([]ProblemsRun{newRun})
	if len(scopedOnly) != 1 || scopedOnly[0].Status != StatusFirstSeen {
		t.Fatalf("BuildProblems(scope-only run) = %+v, want one first-seen problem (the bug being fixed)", scopedOnly)
	}

	// The fix: BuildProblemsScoped given the FULL history reports the true
	// status regardless of how narrow the caller's own scope is.
	inScope := map[string]bool{"new-hit": true}
	problems := BuildProblemsScoped(allRuns, inScope, nil)
	if len(problems) != 1 {
		t.Fatalf("got %d problems, want 1", len(problems))
	}
	p := problems[0]
	if p.Status != StatusRecurring {
		t.Errorf("Status = %q, want recurring (computed over ALL history, not the in-scope run alone)", p.Status)
	}
	if p.RunsTotal != 2 {
		t.Errorf("RunsTotal = %d, want 2 (all-history total, unaffected by scope)", p.RunsTotal)
	}
	if p.InScopeHit != 1 {
		t.Errorf("InScopeHit = %d, want 1 (only new-hit is in scope)", p.InScopeHit)
	}
}

// TestBuildProblemsScoped_ExcludesProblemsWithNoInScopeMember pins the
// other half of item 3: scope filters WHICH problems are returned (never
// their status/totals) — a problem with no member in inScopeRunIDs is
// left out entirely, even though it's part of allRuns.
func TestBuildProblemsScoped_ExcludesProblemsWithNoInScopeMember(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	oldOnly := pRun("old-only", "b-old", "s1", "build-old", day(1), day(1),
		observer.Finding{Severity: "high", Owner: "zcp-tool", Title: "old", Surface: "tool:old", Anchor: "old problem anchor text"})
	newRun := pRun("new-run", "b-new", "s2", "build-new", day(2), day(2),
		observer.Finding{Severity: "high", Owner: "zcp-tool", Title: "new", Surface: "tool:new", Anchor: "new problem anchor text"})
	allRuns := []ProblemsRun{oldOnly, newRun}
	inScope := map[string]bool{"new-run": true}

	problems := BuildProblemsScoped(allRuns, inScope, nil)
	if len(problems) != 1 || problems[0].Members[0].RunID != "new-run" {
		t.Fatalf("got %+v, want only the in-scope problem listed", problems)
	}
}

// --- item 2 (FIX2 round 2): still-emitted -----------------------------------

// TestBuildProblemsScoped_StillEmitted pins item 2: before a problem's
// status settles on gone or unconfirmed, its normalized anchor is searched
// in the normalized step text of the newest build's own runs of its
// scenarios; found → still-emitted (a live status) instead of gone/
// unconfirmed. A nil StepTextFinder (BuildProblems) never searches, so the
// plain gone/unconfirmed rule is exactly as before this item.
func TestBuildProblemsScoped_StillEmitted(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	const anchorText = "NOT supervised by any process manager"
	older := pRun("older-hit", "b-old", "s1", "build-old", day(1), day(1),
		observer.Finding{Severity: "medium", Owner: "zcp-guidance", Title: "x", Surface: "tool:zerops_manage", Anchor: anchorText})
	// newer-clean: same scenario, the newest build, assessed but clean (no
	// finding this time) — the candidate-gone precondition.
	newerClean := pRun("newer-clean", "b-new", "s1", "build-new", day(2), day(2))
	allRuns := []ProblemsRun{older, newerClean}
	inScope := map[string]bool{"older-hit": true, "newer-clean": true}

	t.Run("anchor text still appears in the newest build's step text: still-emitted", func(t *testing.T) {
		find := func(runID string) (string, bool) {
			if runID == "newer-clean" {
				return "the service is " + anchorText + " here", true
			}
			return "", false
		}
		problems := BuildProblemsScoped(allRuns, inScope, find)
		if len(problems) != 1 {
			t.Fatalf("got %d problems, want 1", len(problems))
		}
		if problems[0].Status != StatusStillEmitted {
			t.Errorf("Status = %q, want still-emitted", problems[0].Status)
		}
	})

	t.Run("anchor text absent from the newest build's step text: gone", func(t *testing.T) {
		find := func(runID string) (string, bool) { return "nothing relevant here", true }
		problems := BuildProblemsScoped(allRuns, inScope, find)
		if problems[0].Status != StatusGone {
			t.Errorf("Status = %q, want gone", problems[0].Status)
		}
	})

	t.Run("nil finder: unchanged gone behavior", func(t *testing.T) {
		problems := BuildProblemsScoped(allRuns, inScope, nil)
		if problems[0].Status != StatusGone {
			t.Errorf("Status = %q, want gone (no search capability)", problems[0].Status)
		}
	})

	t.Run("BuildProblems (compat wrapper) never searches", func(t *testing.T) {
		problems := BuildProblems(allRuns)
		if problems[0].Status != StatusGone {
			t.Errorf("Status = %q, want gone (BuildProblems passes a nil finder)", problems[0].Status)
		}
	})
}

// TestBuildProblemsScoped_StepReadFailure_PreventsGone pins the conservative
// side of the still-emitted search: a readable negative is not enough to
// establish that an anchor disappeared when another relevant newest-build
// run cannot be read. The problem remains unconfirmed until every candidate
// is readable (or one positively matches).
func TestBuildProblemsScoped_StepReadFailure_PreventsGone(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	const anchorText = "deployment output still lacks the required readiness marker"
	older := pRun("old-hit", "b-old", "s1", "build-old", day(1), day(1),
		observer.Finding{Severity: "medium", Owner: "zcp-guidance", Title: "x", Surface: "tool:zerops_deploy", Anchor: anchorText})
	newerClean := pRun("new-clean", "b-new", "s1", "build-new", day(2), day(2))
	newerUnavailable := pRun("new-unavailable", "b-new", "s1", "build-new", day(2), day(2))
	allRuns := []ProblemsRun{older, newerClean, newerUnavailable}

	problems := BuildProblemsScoped(allRuns, map[string]bool{"old-hit": true}, func(runID string) (string, bool) {
		switch runID {
		case "new-clean":
			return "readable text without the historical anchor", true
		case "new-unavailable":
			return "", false
		default:
			return "", false
		}
	})
	if len(problems) != 1 {
		t.Fatalf("got %d problems, want 1", len(problems))
	}
	if got := problems[0].Status; got != StatusUnconfirmed {
		t.Fatalf("Status = %q, want %q while a relevant candidate is unreadable", got, StatusUnconfirmed)
	}
}

// TestStillEmitted_EmptyAnchorNeverSearches pins item 2's own scope limit:
// a no-anchor or solo problem (empty anchor) keeps today's behavior — the
// search never runs for it, regardless of findStepText.
func TestStillEmitted_EmptyAnchorNeverSearches(t *testing.T) {
	always := func(string) (string, bool) { return "anything at all", true }
	if got := stillEmitted("noanchor|tool:x|zcp-tool|s1", map[string]bool{"s1": true}, "build-new",
		map[string]map[string][]string{"build-new": {"s1": {"r1"}}}, always, nil, map[string]stepTextCacheEntry{}); got {
		t.Error("stillEmitted(no-anchor key) = true, want false")
	}
	if got := stillEmitted("solo|r1|0", map[string]bool{"s1": true}, "build-new",
		map[string]map[string][]string{"build-new": {"s1": {"r1"}}}, always, nil, map[string]stepTextCacheEntry{}); got {
		t.Error("stillEmitted(solo key) = true, want false")
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

// BenchmarkBuildProblemsScoped_StillEmitted measures item 2's own added
// cost in isolation: ~120 problems (the live bucket's own scale), ~40 of
// them anchor-kind and gone/unconfirmed-bound (so they reach the
// still-emitted search), each searching a handful of candidate runs whose
// step text is a few KB. This is the in-process string-matching/masking
// cost ONLY — findStepText here is a map lookup, not real bundle I/O; a
// real page's total cost is dominated by whatever the caller's own
// findStepText implementation does (reading transcript.jsonl per
// candidate run) plus its own caching, which live outside this package's
// write-set. Run: go test ./internal/eval/farm/console/ -bench
// BuildProblemsScoped_StillEmitted -benchtime=10x -run '^$'.
func BenchmarkBuildProblemsScoped_StillEmitted(b *testing.B) {
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	stepText := make(map[string]string)
	allRuns := make([]ProblemsRun, 0, 240)

	// ~40 anchor-kind problems bound for gone/unconfirmed: an older-build
	// hit plus a clean newest-build run per problem (the still-emitted
	// candidate-run shape), each newest-build run carrying ~4KB of step
	// text that does NOT contain the anchor (the common case: most really
	// are gone).
	filler := strings.Repeat("ordinary step output that never mentions the bug. ", 80) // ~4KB
	for i := range 40 {
		scn := fmt.Sprintf("scn%d", i)
		anchor := fmt.Sprintf("distinct anchor text for problem number %d here", i)
		older := pRun(fmt.Sprintf("older-%d", i), "b-old", scn, "build-old", day(1), day(1),
			observer.Finding{Severity: "medium", Owner: "zcp-tool", Title: "t", Surface: "tool:x", Anchor: anchor})
		newer := pRun(fmt.Sprintf("newer-%d", i), "b-new", scn, "build-new", day(2), day(2))
		allRuns = append(allRuns, older, newer)
		stepText[newer.Row.RunID] = filler
	}
	// ~80 more clean recurring/first-seen problems, no search needed, to
	// match the live bucket's rough problem count.
	for i := range 80 {
		scn := fmt.Sprintf("live%d", i)
		anchor := fmt.Sprintf("recurring anchor text for problem number %d here", i)
		f := observer.Finding{Severity: "high", Owner: "zcp-tool", Title: "t", Surface: "tool:y", Anchor: anchor}
		allRuns = append(allRuns,
			pRun(fmt.Sprintf("live-old-%d", i), "b-old", scn, "build-old", day(1), day(1), f),
			pRun(fmt.Sprintf("live-new-%d", i), "b-new", scn, "build-new", day(2), day(2), f))
	}

	inScope := make(map[string]bool, len(allRuns))
	for _, r := range allRuns {
		inScope[r.Row.RunID] = true
	}
	find := func(runID string) (string, bool) {
		text, ok := stepText[runID]
		return text, ok
	}

	for b.Loop() {
		BuildProblemsScoped(allRuns, inScope, find)
	}
}
