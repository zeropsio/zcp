//go:build probe

// Diagnostic probe for the Zerops log backend query surface.
//
// Run: ZCP_API_KEY=... go test ./internal/platform/ -tags=probe -run TestProbe_LogBackend -v
//
// This file is build-tagged and never compiled by the default test runner.
// It exists to answer: what does the live log backend accept, what does it
// return, what are the enum bounds? Results inform plans — not pinned behavior.
package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type probeItem struct {
	ID             string `json:"id"`
	MsgID          string `json:"msgId"`
	Timestamp      string `json:"timestamp"`
	Hostname       string `json:"hostname"`
	Message        string `json:"message"`
	Content        string `json:"content"`
	SeverityLabel  string `json:"severityLabel"`
	Severity       int    `json:"severity"`
	Priority       int    `json:"priority"`
	Facility       int    `json:"facility"`
	FacilityLabel  string `json:"facilityLabel"`
	Tag            string `json:"tag"`
	AppName        string `json:"appName"`
	ProcID         string `json:"procId"`
	StructuredData string `json:"structuredData"`
}

type probeResp struct {
	Items []probeItem `json:"items"`
}

func TestProbe_LogBackend(t *testing.T) {
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

	info, err := client.GetUserInfo(ctx)
	if err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	}
	projects, err := client.ListProjects(ctx, info.ID)
	if err != nil || len(projects) == 0 {
		t.Fatalf("no projects: %v", err)
	}
	projectID := projects[0].ID
	t.Logf("project: %s (%s)", projects[0].Name, projectID)

	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	svcHost := os.Getenv("ZCP_PROBE_SERVICE")
	if svcHost == "" {
		svcHost = "zcp"
	}
	var svcID string
	for _, s := range services {
		if s.Name == svcHost {
			svcID = s.ID
			break
		}
	}
	if svcID == "" {
		names := make([]string, len(services))
		for i, s := range services {
			names[i] = s.Name
		}
		t.Fatalf("service %s not found; available: %v", svcHost, names)
	}
	t.Logf("service: %s (%s)", svcHost, svcID)

	access, err := client.GetProjectLog(ctx, projectID)
	if err != nil {
		t.Fatalf("GetProjectLog: %v", err)
	}
	t.Logf("signed url host: %s", probeURLHost(access.URL))

	httpC := &http.Client{Timeout: 30 * time.Second}

	run := func(label string, overrides map[string]string) {
		t.Helper()
		u, err := url.Parse(strings.TrimPrefix(access.URL, "GET "))
		if err != nil {
			t.Fatalf("url.Parse: %v", err)
		}
		q := u.Query()
		q.Set("serviceStackId", svcID)
		q.Set("limit", "5")
		q.Set("desc", "1")
		for k, v := range overrides {
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
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if resp.StatusCode != 200 {
			t.Logf("[%s] HTTP %d body(<=400): %s", label, resp.StatusCode, probeTruncate(string(body), 400))
			return
		}
		var pr probeResp
		if err := json.Unmarshal(body, &pr); err != nil {
			t.Logf("[%s] JSON decode: %v body(<=400): %s", label, err, probeTruncate(string(body), 400))
			return
		}
		t.Logf("[%s] HTTP 200 items=%d bytes=%d", label, len(pr.Items), len(body))
		// Only print details for small result sets; suppress bulk dumps.
		showMax := 5
		if len(pr.Items) > showMax {
			t.Logf("  (suppressing %d items; showing first/last)", len(pr.Items)-showMax)
			for i, it := range pr.Items[:2] {
				logItem(t, i, it)
			}
			t.Logf("  ...")
			for i := len(pr.Items) - 2; i < len(pr.Items); i++ {
				logItem(t, i, pr.Items[i])
			}
			return
		}
		for i, it := range pr.Items {
			logItem(t, i, it)
		}
	}

	run("baseline (ZCP default: serviceStackId + limit=5 + desc=1)", nil)
	run("with facility=16 (APPLICATION)", map[string]string{"facility": "16"})
	run("with facility=17 (WEBSERVER)", map[string]string{"facility": "17"})
	run("minimumSeverity=4 (warning+)", map[string]string{"minimumSeverity": "4"})
	run("minimumSeverity=0 (emergency only)", map[string]string{"minimumSeverity": "0"})
	run("minimumSeverity=99 (invalid bound)", map[string]string{"minimumSeverity": "99"})
	run("minimumSeverity=-1 (invalid bound)", map[string]string{"minimumSeverity": "-1"})
	run("search=sshfs", map[string]string{"search": "sshfs"})
	run("search=nonexistent-xyz", map[string]string{"search": "nonexistent-xyz"})
	run("since=2026-04-22T00:00:00Z (is server-side time filter accepted?)", map[string]string{"since": "2026-04-22T00:00:00Z"})
	run("from=2026-04-22T00:00:00Z", map[string]string{"from": "2026-04-22T00:00:00Z"})
	run("fromDate=2026-04-22T00:00:00Z", map[string]string{"fromDate": "2026-04-22T00:00:00Z"})
	run("timestamp>=2026-04-22T00:00:00Z", map[string]string{"timestamp": "2026-04-22T00:00:00Z"})
	run("limit=1", map[string]string{"limit": "1"})
	run("limit=1000 (zcli max)", map[string]string{"limit": "1000"})
	run("limit=5000 (over zcli max)", map[string]string{"limit": "5000"})
	run("limit=50000 (extreme)", map[string]string{"limit": "50000"})
	run("desc=0 (ascending)", map[string]string{"desc": "0"})
	run("capture 10 msgIds for cursor probe", map[string]string{"limit": "10"})
}

func logItem(t *testing.T, i int, it probeItem) {
	t.Helper()
	msg := it.Message
	if msg == "" {
		msg = it.Content
	}
	if len(msg) > 80 {
		msg = msg[:80] + "..."
	}
	t.Logf("  [%d] ts=%s sev=%d/%s fac=%d/%s tag=%q id=%q msg=%s",
		i, it.Timestamp, it.Severity, it.SeverityLabel, it.Facility, it.FacilityLabel, it.Tag, it.ID, msg)
}

func probeURLHost(s string) string {
	u, err := url.Parse(strings.TrimPrefix(s, "GET "))
	if err != nil {
		return "(unparseable)"
	}
	return fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, u.Path)
}

func probeTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
