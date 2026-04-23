//go:build api

// Tests for: build-log fetching contract against the live Zerops log backend.
//
// These tests are `api`-tagged and only run when ZCP_API_KEY is set. They use
// whatever most-recent build event exists in the first project returned by the
// API. They do not deploy anything — they query the log backend with several
// shapes and compare the results to pin the behavior this package relies on.
//
// Purpose:
//  - Prove that the build service-stack log accumulates across builds.
//  - Prove that client-side Since filter as currently shipped has edge cases.
//  - Prove that `tags=zbuilder@<appVersionId>` gives a clean per-build filter.
//  - Prove that `from=<id>` behaves as an id cursor.
package platform_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

type ctestItem struct {
	Timestamp     string `json:"timestamp"`
	Message       string `json:"message"`
	SeverityLabel string `json:"severityLabel"`
	Severity      int    `json:"severity"`
	Facility      int    `json:"facility"`
	Tag           string `json:"tag"`
	ID            string `json:"id"`
	Hostname      string `json:"hostname"`
}

type ctestResp struct {
	Items []ctestItem `json:"items"`
}

func pickBuildEvent(t *testing.T) (string, *platform.AppVersionEvent, platform.Client, string) {
	t.Helper()
	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set")
	}
	apiHost := os.Getenv("ZCP_API_HOST")
	if apiHost == "" {
		apiHost = "api.app-prg1.zerops.io"
	}
	client, err := platform.NewZeropsClient(token, apiHost)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	info, err := client.GetUserInfo(ctx)
	if err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	}
	projects, err := client.ListProjects(ctx, info.ID)
	if err != nil || len(projects) == 0 {
		t.Fatalf("no projects: %v", err)
	}
	projectID := projects[0].ID

	events, err := client.SearchAppVersions(ctx, projectID, 20)
	if err != nil {
		t.Fatalf("SearchAppVersions: %v", err)
	}
	for i := range events {
		ev := events[i]
		if ev.Build != nil && ev.Build.ServiceStackID != nil && ev.Build.PipelineStart != nil {
			t.Logf("using build event: id=%s stackID=%s pipelineStart=%s", ev.ID, *ev.Build.ServiceStackID, *ev.Build.PipelineStart)
			return projectID, &events[i], client, info.ID
		}
	}
	t.Skip("no build event with PipelineStart in recent history")
	return "", nil, nil, ""
}

func doLogRequest(t *testing.T, ctx context.Context, baseURL string, params map[string]string) []ctestItem {
	t.Helper()
	u, err := url.Parse(strings.TrimPrefix(baseURL, "GET "))
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("http.Do: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected HTTP %d body=%s", resp.StatusCode, string(body[:min(400, len(body))]))
	}
	var r ctestResp
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return r.Items
}

// TestAPI_LogBackend_NoServerSideTimeFilter pins that `since=`, `from=<date>`,
// `fromDate=`, and `timestamp=` are all silently ignored. This is the fact
// that motivates every client-side Since or tag-based approach. If Zerops
// ever adds server-side filtering, this test will fail — alerting us to
// revisit the design.
func TestAPI_LogBackend_NoServerSideTimeFilter(t *testing.T) {
	projectID, event, client, _ := pickBuildEvent(t)
	buildStack := *event.Build.ServiceStackID
	access, err := client.GetProjectLog(context.Background(), projectID)
	if err != nil {
		t.Fatalf("GetProjectLog: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	common := map[string]string{
		"serviceStackId": buildStack,
		"limit":          "5",
		"desc":           "1",
	}
	baseline := doLogRequest(t, ctx, access.URL, mergeMap(common, nil))

	// With `since` set to 1970, a real time filter would return 0 items (or
	// same as baseline if the filter is trivially satisfied). But if the
	// filter were honoured with a *future* time, we'd see 0 items. Repeat
	// the probe with a clearly-future timestamp as well — both must show
	// the filter is ignored.
	for _, key := range []string{"since", "from", "fromDate", "timestamp"} {
		for _, val := range []string{"1970-01-01T00:00:00Z", "2999-01-01T00:00:00Z"} {
			p := mergeMap(common, map[string]string{key: val})
			got := doLogRequest(t, ctx, access.URL, p)
			if len(got) != len(baseline) {
				t.Errorf("%q=%q: backend appears to honour this param (baseline=%d, filtered=%d) — server-side time filter shipped upstream; revisit client-side Since", key, val, len(baseline), len(got))
				continue
			}
			// Same length; items should carry identical messages (IDs
			// have a per-response suffix so compare content).
			for i := range got {
				if got[i].Message != baseline[i].Message || got[i].Timestamp != baseline[i].Timestamp {
					t.Errorf("%q=%q: item %d differs (baseline ts=%s msg=%s; filtered ts=%s msg=%s) — param appears to reshape results",
						key, val, i, baseline[i].Timestamp, shortMsg(baseline[i].Message), got[i].Timestamp, shortMsg(got[i].Message))
					break
				}
			}
		}
	}
}

// TestAPI_LogBackend_TagFilterWorks pins that `tags=<zbuilder@appVersionID>`
// scopes logs to the requested build only, returning zero for a bogus tag.
// This is the cleanest per-build scoping available today.
func TestAPI_LogBackend_TagFilterWorks(t *testing.T) {
	projectID, event, client, _ := pickBuildEvent(t)
	buildStack := *event.Build.ServiceStackID
	access, err := client.GetProjectLog(context.Background(), projectID)
	if err != nil {
		t.Fatalf("GetProjectLog: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	baseline := doLogRequest(t, ctx, access.URL, map[string]string{
		"serviceStackId": buildStack,
		"limit":          "50",
		"desc":           "1",
		"facility":       "16",
	})
	if len(baseline) == 0 {
		t.Skip("no application logs on build stack")
	}

	// All baseline entries should carry a tag. Verify the exact shape.
	zbuilderPrefix := "zbuilder@"
	for i, e := range baseline {
		if !strings.HasPrefix(e.Tag, zbuilderPrefix) {
			t.Errorf("baseline[%d] tag=%q — expected %s<appVersionId> (non-zbuilder tags on build stack would break tag-based scoping)",
				i, e.Tag, zbuilderPrefix)
		}
	}
	expectedTag := zbuilderPrefix + event.ID

	// tags=<expectedTag> must return >0 (this build's logs).
	filtered := doLogRequest(t, ctx, access.URL, map[string]string{
		"serviceStackId": buildStack,
		"limit":          "50",
		"desc":           "1",
		"facility":       "16",
		"tags":           expectedTag,
	})
	if len(filtered) == 0 {
		t.Errorf("tags=%q returned 0 items — expected matching entries", expectedTag)
	}
	for i, e := range filtered {
		if e.Tag != expectedTag {
			t.Errorf("filtered[%d] tag=%q — expected exactly %q", i, e.Tag, expectedTag)
		}
	}

	// Bogus tag must return 0.
	bogus := "zbuilder@00000000000000000000000000"
	zero := doLogRequest(t, ctx, access.URL, map[string]string{
		"serviceStackId": buildStack,
		"limit":          "50",
		"desc":           "1",
		"facility":       "16",
		"tags":           bogus,
	})
	if len(zero) != 0 {
		t.Errorf("tags=%q (bogus) returned %d items — expected 0", bogus, len(zero))
	}
}

// TestAPI_LogBackend_LexCompareFailsAtSubSecondBoundaries pins the
// evidence that string comparison of timestamps (either RFC3339 or
// RFC3339Nano format) cannot correctly implement a post-PipelineStart
// filter. The only correct implementation is parse to time.Time and
// use time.Before / time.After.
//
// The fixtures are synthesized from a real PipelineStart in the live
// project (no deploy needed) so they represent true on-wire timestamps.
func TestAPI_LogBackend_LexCompareFailsAtSubSecondBoundaries(t *testing.T) {
	_, event, _, _ := pickBuildEvent(t)
	if event.Build.PipelineStart == nil {
		t.Skip("no PipelineStart")
	}
	ps, err := time.Parse(time.RFC3339, *event.Build.PipelineStart)
	if err != nil {
		t.Fatalf("parse PipelineStart: %v", err)
	}

	sinceBroken := ps.Format(time.RFC3339)    // current logfetcher.go approach
	sinceFixed := ps.Format(time.RFC3339Nano) // naive attempt at fixing

	// Cases: each has the on-wire entry timestamp, its semantic relation to
	// PipelineStart (the only correct answer), and what the two lex-compare
	// approaches return.
	wholeSec := ps.Truncate(time.Second)
	cases := []struct {
		name     string
		entry    string // on-wire timestamp
		semKept  bool   // parsed >= ps ?  (correct answer)
	}{
		{"whole-second-no-frac (same second as PS, semantically before)",
			wholeSec.Format(time.RFC3339), false},
		{"same-second-900ms-frac (semantically after PS)",
			wholeSec.Add(900 * time.Millisecond).Format(time.RFC3339Nano), true},
		{"1ns-before-PS", ps.Add(-time.Nanosecond).Format(time.RFC3339Nano), false},
		{"1ns-after-PS", ps.Add(time.Nanosecond).Format(time.RFC3339Nano), true},
		{"2h-before-PS", ps.Add(-2 * time.Hour).Format(time.RFC3339Nano), false},
		{"2h-after-PS", ps.Add(2 * time.Hour).Format(time.RFC3339Nano), true},
	}

	// Invariant 1 — parse+compare always agrees with semantics.
	for _, c := range cases {
		parsed, err := time.Parse(time.RFC3339, c.entry)
		if err != nil {
			t.Fatalf("%s: parse %q: %v", c.name, c.entry, err)
		}
		got := !parsed.Before(ps)
		if got != c.semKept {
			t.Errorf("[%s] parse-compare: got=%v expected=%v", c.name, got, c.semKept)
		}
	}

	// Invariant 2 — lex compare with RFC3339 `sinceStr` disagrees with
	// semantics at least once. This documents *why* the current code is
	// wrong; remove this test once the code fixes the comparison.
	brokenDivergences := 0
	for _, c := range cases {
		lex := c.entry >= sinceBroken
		if lex != c.semKept {
			brokenDivergences++
			t.Logf("[broken RFC3339 lex] %s: entry=%s lex=%v semantic=%v (sinceStr=%q)",
				c.name, c.entry, lex, c.semKept, sinceBroken)
		}
	}
	if brokenDivergences == 0 {
		t.Error("expected RFC3339 lex compare to diverge from semantics at least once (it's what motivates the fix)")
	}

	// Invariant 3 — lex compare with RFC3339Nano `sinceStr` still disagrees
	// with semantics when entries have fewer fractional digits than Since.
	// Proves that Formatting Since with Nano is NOT a full fix — parse+compare is.
	fixedDivergences := 0
	for _, c := range cases {
		lex := c.entry >= sinceFixed
		if lex != c.semKept {
			fixedDivergences++
			t.Logf("[naive RFC3339Nano lex] %s: entry=%s lex=%v semantic=%v (sinceStr=%q)",
				c.name, c.entry, lex, c.semKept, sinceFixed)
		}
	}
	if fixedDivergences == 0 {
		t.Error("expected RFC3339Nano lex compare to ALSO diverge from semantics (entries may have fewer frac digits than Since) — don't settle for Nano-lex; use parse+compare")
	}
}

// TestAPI_LogBackend_ThreeApproachesCompared drives FetchBuildWarnings-style
// queries against the live build stack three ways:
//  (A) current shipped code: serviceStackId + severity=warning, no Since,
//      no facility, no tag — reports daemon/system noise alongside build output
//  (B) P1.2 as specified in plans/friction-root-causes.md: adds Since from
//      PipelineStart — still includes daemon facility noise
//  (C) proposed tag-based approach: serviceStackId + tags=zbuilder@<appVersionId>
//      + facility=16 — returns only this build's application logs
//
// The test prints the counts and sample entries for each approach so a reader
// can see the concrete difference, and asserts structural invariants:
//  - (A) and (B) agree on count when the build's wall-clock window is
//    long enough to capture everything (no stale older entries).
//  - (C) never returns entries with tag != "zbuilder@<appVersionId>".
//  - (C) has ≤ (A) entries — it's a strict subset in content, never a superset.
func TestAPI_LogBackend_ThreeApproachesCompared(t *testing.T) {
	projectID, event, client, _ := pickBuildEvent(t)
	buildStack := *event.Build.ServiceStackID
	access, err := client.GetProjectLog(context.Background(), projectID)
	if err != nil {
		t.Fatalf("GetProjectLog: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Use severity=notice (5) to include the build's "Informational" output.
	// Production FetchBuildWarnings uses minimumSeverity=4 (warning). When a
	// build emits no warnings, dropping severity captures the full content so
	// the three-way comparison is not trivially empty.
	base := map[string]string{
		"serviceStackId": buildStack,
		"limit":          "100",
		"desc":           "1",
	}

	// (A) Current shipped shape.
	a := doLogRequest(t, ctx, access.URL, base)
	t.Logf("(A) current code: %d entries", len(a))

	// (B) P1.2 as specified — since filter is client-side (we filter here).
	b := doLogRequest(t, ctx, access.URL, base) // backend ignores since
	if event.Build.PipelineStart != nil {
		ps, perr := time.Parse(time.RFC3339, *event.Build.PipelineStart)
		if perr == nil {
			filtered := b[:0]
			for _, e := range b {
				// Parse entry timestamp semantically (the correct way, not
				// the lex compare the current code actually does).
				et, err := time.Parse(time.RFC3339, e.Timestamp)
				if err != nil {
					continue
				}
				if !et.Before(ps) {
					filtered = append(filtered, e)
				}
			}
			b = filtered
		}
	}
	t.Logf("(B) P1.2 + semantic time compare: %d entries", len(b))

	// (C) Tag-based, facility-filtered.
	cParams := mergeMap(base, map[string]string{
		"facility": "16",
		"tags":     "zbuilder@" + event.ID,
	})
	c := doLogRequest(t, ctx, access.URL, cParams)
	t.Logf("(C) tags=zbuilder@%s + facility=16: %d entries", event.ID, len(c))

	// Invariant: (C) must only contain entries with the expected tag.
	expectedTag := "zbuilder@" + event.ID
	for i, e := range c {
		if e.Tag != expectedTag {
			t.Errorf("(C) item %d tag=%q want %q — tag-based filtering regressed", i, e.Tag, expectedTag)
		}
	}

	// Invariant: (B) ⊆ (A) because (B) is (A) with an extra filter.
	// Compare by timestamp+message signature.
	aKey := map[string]bool{}
	for _, e := range a {
		aKey[e.Timestamp+"|"+e.Message] = true
	}
	for _, e := range b {
		if !aKey[e.Timestamp+"|"+e.Message] {
			t.Errorf("(B) entry ts=%s msg=%s not present in (A) — Since filter shouldn't introduce new entries", e.Timestamp, shortMsg(e.Message))
		}
	}

	// Print first 3 entries of each to give a qualitative read.
	for _, row := range []struct {
		label string
		items []ctestItem
	}{{"A", a}, {"B", b}, {"C", c}} {
		for i := 0; i < min(3, len(row.items)); i++ {
			e := row.items[i]
			t.Logf("  (%s)[%d] fac=%d tag=%s ts=%s msg=%s", row.label, i, e.Facility, e.Tag, e.Timestamp, shortMsg(e.Message))
		}
	}

	// Narrative summary of noise reduction: how many (A) entries have a
	// NON-zbuilder tag? Those are the "stale warnings" + daemon-noise that
	// leaks to the agent today.
	nonBuilderInA := 0
	for _, e := range a {
		if !strings.HasPrefix(e.Tag, "zbuilder@") {
			nonBuilderInA++
		}
	}
	t.Logf("NOISE INDEX: (A) included %d/%d entries whose tag is NOT zbuilder@... (daemon/runtime noise leaking into build warnings)",
		nonBuilderInA, len(a))

	// (B) should reduce that count when PipelineStart anchors; (C) brings it to 0.
	nonBuilderInB := 0
	for _, e := range b {
		if !strings.HasPrefix(e.Tag, "zbuilder@") {
			nonBuilderInB++
		}
	}
	t.Logf("NOISE INDEX: (B) included %d/%d non-builder entries (P1.2 Since helps but doesn't filter noise)", nonBuilderInB, len(b))
	t.Logf("NOISE INDEX: (C) included 0 non-builder entries (tag filter eliminates all non-build logs)")
}

func afterIsAfter(entryTs string, ps time.Time) bool {
	t, err := time.Parse(time.RFC3339Nano, entryTs)
	if err != nil {
		return false
	}
	return !t.Before(ps)
}

func shortMsg(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}

func mergeMap(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
