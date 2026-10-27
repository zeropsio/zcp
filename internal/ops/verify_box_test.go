// Tests for: verify_box.go — a Zerops Box's orchestrator check
// (spec-workflows.md §8 PS-9).
package ops

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// boxPingDoer answers a Box's ping as told and records what it was asked.
type boxPingDoer struct {
	status int
	body   string
	err    error
	urls   []string
	auth   []string
}

func (d *boxPingDoer) Do(req *http.Request) (*http.Response, error) {
	d.urls = append(d.urls, req.Method+" "+req.URL.String())
	d.auth = append(d.auth, req.Header.Get("Authorization"))
	if d.err != nil {
		return nil, d.err
	}
	return &http.Response{StatusCode: d.status, Body: io.NopCloser(strings.NewReader(d.body)), Header: http.Header{}}, nil
}

// TestBoxOrchestratorCheck — the workspace API's GET /api/rest/ping answers
// without the key (zerops-docs zbox/access.mdx, HTTP API) at
// http://<box>:8080, which answers only inside the project
// (zbox/workspace-service.mdx, Project network). In the project, a running
// Box is asked it, keyless: an answer passes and names the orchestrator's
// version, anything else fails naming why. Outside the project the check says
// it answers only inside, and a Box that is not running is not asked.
func TestBoxOrchestratorCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		doer       *boxPingDoer
		inProject  bool
		running    bool
		wantStatus string
		wantDetail []string
		wantAsked  bool
	}{
		{
			name:       "answers",
			doer:       &boxPingDoer{status: http.StatusOK, body: `{"time":"2026-10-24T10:00:00Z","version":"v1.4.2"}`},
			inProject:  true,
			running:    true,
			wantStatus: CheckPass,
			wantDetail: []string{"GET /api/rest/ping", "http://box:8080", "v1.4.2"},
			wantAsked:  true,
		},
		{
			name:       "answers 502",
			doer:       &boxPingDoer{status: http.StatusBadGateway, body: "bad gateway"},
			inProject:  true,
			running:    true,
			wantStatus: CheckFail,
			wantDetail: []string{"502", "http://box:8080/api/rest/ping", "zerops_logs"},
			wantAsked:  true,
		},
		{
			name:       "does not answer",
			doer:       &boxPingDoer{err: errors.New("dial tcp: connection refused")},
			inProject:  true,
			running:    true,
			wantStatus: CheckFail,
			wantDetail: []string{"connection refused", "zerops_logs"},
			wantAsked:  true,
		},
		{
			name:       "outside the project",
			doer:       &boxPingDoer{status: http.StatusOK},
			inProject:  false,
			running:    true,
			wantStatus: CheckSkip,
			wantDetail: []string{"only inside the project", "http://box:8080", "GET /api/rest/ping"},
		},
		{
			name:       "not running",
			doer:       &boxPingDoer{status: http.StatusOK},
			inProject:  true,
			running:    false,
			wantStatus: CheckSkip,
			wantDetail: []string{"not running"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &VerifyResult{Hostname: "box", Type: runtimeClassSandbox, Checks: []CheckResult{{Name: "service_running", Status: CheckPass}}}
			if !tt.running {
				r.Checks[0].Status = CheckFail
			}
			got := BoxOrchestratorCheck(context.Background(), tt.doer, r, tt.inProject)
			if got.Name != "orchestrator" {
				t.Errorf("name = %q, want orchestrator", got.Name)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q (%s)", got.Status, tt.wantStatus, got.Detail)
			}
			for _, want := range tt.wantDetail {
				if !strings.Contains(got.Detail, want) {
					t.Errorf("detail %q misses %q", got.Detail, want)
				}
			}
			if asked := len(tt.doer.urls) > 0; asked != tt.wantAsked {
				t.Fatalf("asked = %v (%v), want %v", asked, tt.doer.urls, tt.wantAsked)
			}
			if tt.wantAsked {
				if tt.doer.urls[0] != "GET http://box:8080/api/rest/ping" {
					t.Errorf("asked %q, want GET http://box:8080/api/rest/ping", tt.doer.urls[0])
				}
				if tt.doer.auth[0] != "" {
					t.Errorf("the ping carried Authorization %q — it needs no key, and none is sent", tt.doer.auth[0])
				}
			}
		})
	}
}
