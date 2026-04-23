//go:build probe

package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestProbe_BuildLogsAndCursor:
//  - Finds the latest build event for the target service and queries the build
//    service-stack log to confirm stale-warning persistence shape.
//  - Tests whether `id` works as an `idFrom`/`from`/`afterId` cursor.
//  - Confirms whether `search` actually filters (baseline vs. impossible string).
func TestProbe_BuildLogsAndCursor(t *testing.T) {
	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set")
	}
	apiHost := os.Getenv("ZCP_API_HOST")
	if apiHost == "" {
		apiHost = "api.app-prg1.zerops.io"
	}

	client, err := NewZeropsClient(token, apiHost)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	info, _ := client.GetUserInfo(ctx)
	projects, _ := client.ListProjects(ctx, info.ID)
	projectID := projects[0].ID
	t.Logf("project: %s (%s)", projects[0].Name, projectID)

	events, err := client.SearchAppVersions(ctx, projectID, 10)
	if err != nil {
		t.Fatalf("SearchAppVersions: %v", err)
	}
	if len(events) == 0 {
		t.Skip("no app version events in project")
	}
	t.Logf("events (latest %d):", len(events))
	var buildStack string
	var buildEvent *AppVersionEvent
	for i := range events {
		ev := events[i]
		t.Logf("  id=%s stack=%s status=%s src=%s seq=%d build=%v",
			ev.ID, ev.ServiceStackID, ev.Status, ev.Source, ev.Sequence, ev.Build != nil)
		if ev.Build != nil && ev.Build.ServiceStackID != nil {
			t.Logf("    build.stackID=%s pipelineStart=%s pipelineFinish=%s pipelineFailed=%s",
				*ev.Build.ServiceStackID,
				strDeref(ev.Build.PipelineStart),
				strDeref(ev.Build.PipelineFinish),
				strDeref(ev.Build.PipelineFailed))
			if buildStack == "" {
				buildStack = *ev.Build.ServiceStackID
				buildEvent = &events[i]
			}
		}
	}
	if buildStack == "" {
		t.Skip("no build service-stack id found in recent events")
	}
	t.Logf("using build service-stack: %s (from event %s)", buildStack, buildEvent.ID)

	access, err := client.GetProjectLog(ctx, projectID)
	if err != nil {
		t.Fatalf("GetProjectLog: %v", err)
	}

	httpC := &http.Client{Timeout: 30 * time.Second}
	base := strings.TrimPrefix(access.URL, "GET ")

	get := func(label string, params map[string]string) (items []probeItem, status int, raw string) {
		u, _ := url.Parse(base)
		q := u.Query()
		q.Set("serviceStackId", buildStack)
		q.Set("limit", "50")
		q.Set("desc", "1")
		q.Set("facility", "16") // APPLICATION (zcli default)
		for k, v := range params {
			if v == "" {
				q.Del(k)
			} else {
				q.Set(k, v)
			}
		}
		u.RawQuery = q.Encode()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpC.Do(req)
		if err != nil {
			t.Logf("[%s] ERR: %v", label, err)
			return nil, 0, ""
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if resp.StatusCode != 200 {
			t.Logf("[%s] HTTP %d body(<=300): %s", label, resp.StatusCode, probeTruncate(string(body), 300))
			return nil, resp.StatusCode, string(body)
		}
		var pr probeResp
		_ = json.Unmarshal(body, &pr)
		t.Logf("[%s] HTTP 200 items=%d bytes=%d", label, len(pr.Items), len(body))
		return pr.Items, 200, string(body)
	}

	// 1) Baseline: build stack logs, facility=16, no time filter.
	items, _, _ := get("build-stack baseline (fac=16, limit=50)", nil)
	if len(items) == 0 {
		t.Log("no APPLICATION logs on build stack — trying without facility")
		items, _, _ = get("build-stack baseline (no facility, limit=50)", map[string]string{"facility": ""})
	}
	// Show a sample.
	for i := 0; i < minInt(5, len(items)); i++ {
		logItem(t, i, items[i])
	}

	// 2) `search=<clearly absent string>`.
	if len(items) > 0 {
		impossible := "zzz_impossible_search_xyzzy_" + time.Now().Format("20060102150405")
		got, _, _ := get("search=<impossible>", map[string]string{"search": impossible})
		t.Logf("  → search filter %s: %d items (expect 0 if search filters)", impossible, len(got))
	}

	// 3) Cursor probes: pick middle id and try various cursor field names.
	if len(items) >= 10 {
		// Server returns newest-first. Sort to be safe.
		sorted := make([]probeItem, len(items))
		copy(sorted, items)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
		mid := sorted[len(sorted)/2]
		t.Logf("cursor reference: ts=%s id=%s", mid.Timestamp, mid.ID)
		for _, key := range []string{"from", "fromId", "afterId", "after", "idFrom", "cursor", "offsetId"} {
			out, _, _ := get("cursor:"+key+"="+mid.ID, map[string]string{key: mid.ID})
			t.Logf("  → cursor key %q: %d items", key, len(out))
		}
	}

	// 4) Timestamp format variation check.
	formats := map[string]int{}
	for _, it := range items {
		switch {
		case strings.Count(it.Timestamp, ".") == 0:
			formats["no-fractional"]++
		default:
			// Extract fractional width.
			dot := strings.IndexByte(it.Timestamp, '.')
			z := strings.IndexByte(it.Timestamp, 'Z')
			if z < 0 {
				z = len(it.Timestamp)
			}
			w := z - dot - 1
			formats["frac-digits-"+intToStr(w)]++
		}
	}
	t.Logf("timestamp fractional-precision distribution: %v", formats)

	// 5) Test the P1.2 client-side Since scenario: take PipelineStart from
	//    buildEvent, format it RFC3339 and RFC3339Nano, compare to an entry that
	//    is actually before it.
	if buildEvent.Build.PipelineStart != nil {
		ps := *buildEvent.Build.PipelineStart
		t.Logf("PipelineStart from event = %s", ps)
		parsed, err := time.Parse(time.RFC3339, ps)
		if err != nil {
			t.Logf("time.Parse(RFC3339, %q): %v", ps, err)
		} else {
			t.Logf("  parsed as time.Time = %v (ns=%d)", parsed, parsed.Nanosecond())
			t.Logf("  Format RFC3339     = %q", parsed.Format(time.RFC3339))
			t.Logf("  Format RFC3339Nano = %q", parsed.Format(time.RFC3339Nano))
		}
	}
}

func strDeref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	if neg {
		out = append([]byte{'-'}, out...)
	}
	return string(out)
}
