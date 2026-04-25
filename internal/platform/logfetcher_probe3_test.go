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

// TestProbe_TagsAndCursor — focused probes to answer:
//   - Does `tags=` filter (to surface only build-N entries via the zbuilder tag)?
//   - Does `containerId=` filter?
//   - What are the exact semantics of `from=<id>` as a cursor?
//   - What's the emitted RFC3339 form when the entry is exactly on-the-second?
func TestProbe_TagsAndCursor(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	info, _ := client.GetUserInfo(ctx)
	projects, _ := client.ListProjects(ctx, info.ID)
	projectID := projects[0].ID

	events, err := client.SearchAppVersions(ctx, projectID, 10)
	if err != nil || len(events) == 0 {
		t.Skipf("no app version events: %v", err)
	}
	var buildStack string
	var appVersionID string
	for _, ev := range events {
		if ev.Build != nil && ev.Build.ServiceStackID != nil {
			buildStack = *ev.Build.ServiceStackID
			appVersionID = ev.ID
			break
		}
	}
	if buildStack == "" {
		t.Skip("no build stack found")
	}
	t.Logf("build stack: %s (appVersionId %s)", buildStack, appVersionID)

	access, err := client.GetProjectLog(ctx, projectID)
	if err != nil {
		t.Fatalf("GetProjectLog: %v", err)
	}

	httpC := &http.Client{Timeout: 30 * time.Second}
	base := strings.TrimPrefix(access.URL, "GET ")

	get := func(label string, override map[string]string) []probeItem {
		u, _ := url.Parse(base)
		q := u.Query()
		q.Set("serviceStackId", buildStack)
		q.Set("limit", "30")
		q.Set("desc", "1")
		q.Set("facility", "16")
		for k, v := range override {
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
			return nil
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if resp.StatusCode != 200 {
			t.Logf("[%s] HTTP %d body: %s", label, resp.StatusCode, probeTruncate(string(body), 400))
			return nil
		}
		var pr probeResp
		_ = json.Unmarshal(body, &pr)
		t.Logf("[%s] items=%d bytes=%d", label, len(pr.Items), len(body))
		return pr.Items
	}

	// Baseline on build stack with fac=16.
	base0 := get("build baseline (serviceStackId, facility=16, limit=30)", nil)
	if len(base0) == 0 {
		t.Skip("no items; cannot run cursor/tag probes")
	}
	firstIDs := make([]string, 0, len(base0))
	firstTs := make([]string, 0, len(base0))
	for _, it := range base0 {
		firstIDs = append(firstIDs, it.ID)
		firstTs = append(firstTs, it.Timestamp)
	}
	// Oldest tag value (for tag-filter probe).
	tagVal := base0[len(base0)-1].Tag
	t.Logf("first id (newest) = %s   last id (oldest in window) = %s   tag = %s", firstIDs[0], firstIDs[len(firstIDs)-1], tagVal)

	// 1) `tags=<tag>` (zcli sends this)
	get("tags="+tagVal, map[string]string{"tags": tagVal})
	// 2) Single-tag singular key
	get("tag="+tagVal, map[string]string{"tag": tagVal})
	// 3) Impossible tag value
	get("tags=definitely-nothing-"+time.Now().Format("150405"),
		map[string]string{"tags": "definitely-nothing-" + time.Now().Format("150405")})
	// 4) zbuilder@<appVersionID> — per-build identity tag
	per := "zbuilder@" + appVersionID
	get("tags="+per+" (per-build)", map[string]string{"tags": per})
	// 5) containerId filter
	if len(base0[0].Hostname) > 0 {
		// hostname is container hostname, not the container uuid, so this won't match
		get("containerId=<hostname>", map[string]string{"containerId": base0[0].Hostname})
	}

	// 6) Cursor semantics. Sort ascending, pick 10th from bottom, use as cursor.
	sorted := make([]probeItem, len(base0))
	copy(sorted, base0)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
	mid := sorted[len(sorted)/2]
	t.Logf("cursor anchor (sorted mid): ts=%s id=%s", mid.Timestamp, mid.ID)
	cursored := get("from=<mid-id>", map[string]string{"from": mid.ID})
	if len(cursored) > 0 {
		cs := make([]probeItem, len(cursored))
		copy(cs, cursored)
		sort.Slice(cs, func(i, j int) bool { return cs[i].Timestamp < cs[j].Timestamp })
		t.Logf("   cursored oldest ts=%s id=%s", cs[0].Timestamp, cs[0].ID)
		t.Logf("   cursored newest ts=%s id=%s", cs[len(cs)-1].Timestamp, cs[len(cs)-1].ID)
		t.Logf("   mid.ts          =%s mid.id=%s", mid.Timestamp, mid.ID)
	}

	// Same cursor with desc=0 (ascending) to see semantics change.
	asc := get("from=<mid-id>&desc=0", map[string]string{"from": mid.ID, "desc": "0"})
	if len(asc) > 0 {
		cs := make([]probeItem, len(asc))
		copy(cs, asc)
		sort.Slice(cs, func(i, j int) bool { return cs[i].Timestamp < cs[j].Timestamp })
		t.Logf("   asc oldest ts=%s id=%s", cs[0].Timestamp, cs[0].ID)
		t.Logf("   asc newest ts=%s id=%s", cs[len(cs)-1].Timestamp, cs[len(cs)-1].ID)
	}
}
