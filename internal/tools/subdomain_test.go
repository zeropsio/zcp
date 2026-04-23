// Tests for: subdomain.go — zerops_subdomain MCP tool handler.

package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// stubHTTPAlwaysOK is a minimal ops.HTTPDoer that returns 200 for every
// request without touching the network. Used by subdomain tests that don't
// exercise the L7-readiness path — tests that do exercise it use their own
// sequencingHTTP stub (or override via OverrideHTTPReadyConfigForTest).
type stubHTTPAlwaysOK struct{}

func (stubHTTPAlwaysOK) Do(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
}

var okHTTP ops.HTTPDoer = stubHTTPAlwaysOK{}

func TestSubdomainTool_EnableReturnsUrls(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app",
				Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
		}).
		WithService(&platform.ServiceStack{
			ID: "svc-1", Name: "app",
			Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
		}).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "myproject", Status: statusActive,
			SubdomainHost: "abc1.prg1.zerops.app",
		}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-enable-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, okHTTP, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "app", "action": "enable",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var sr ops.SubdomainResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &sr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(sr.SubdomainUrls) != 1 {
		t.Fatalf("expected 1 subdomain URL, got %d: %v", len(sr.SubdomainUrls), sr.SubdomainUrls)
	}
	want := "https://app-abc1-3000.prg1.zerops.app"
	if sr.SubdomainUrls[0] != want {
		t.Errorf("SubdomainUrls[0] = %q, want %q", sr.SubdomainUrls[0], want)
	}
}

func TestSubdomainTool_EnableReturnsUrls_BarePrefix(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app",
				Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
		}).
		WithService(&platform.ServiceStack{
			ID: "svc-1", Name: "app",
			Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
		}).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "myproject", Status: statusActive,
			SubdomainHost: "abc1", // bare prefix — no domain suffix
		}).
		WithServiceEnv("svc-1", []platform.EnvVar{
			{ID: "env-1", Key: "zeropsSubdomain", Content: "https://app-abc1-3000.prg1.zerops.app"},
		}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-enable-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, okHTTP, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "app", "action": "enable",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var sr ops.SubdomainResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &sr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(sr.SubdomainUrls) != 1 {
		t.Fatalf("expected 1 subdomain URL, got %d: %v", len(sr.SubdomainUrls), sr.SubdomainUrls)
	}
	want := "https://app-abc1-3000.prg1.zerops.app"
	if sr.SubdomainUrls[0] != want {
		t.Errorf("SubdomainUrls[0] = %q, want %q", sr.SubdomainUrls[0], want)
	}
}

func TestSubdomainTool_Enable_PollsProcess(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-enable-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, okHTTP, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "api", "action": "enable",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var sr ops.SubdomainResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &sr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if sr.Action != "enable" {
		t.Errorf("action = %q, want %q", sr.Action, "enable")
	}
	if sr.Process == nil {
		t.Fatal("expected process in result")
	}
	if sr.Process.Status != statusFinished {
		t.Errorf("process status = %q, want %q (tool must poll to completion)", sr.Process.Status, statusFinished)
	}
}

func TestSubdomainTool_Disable_PollsProcess(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-disable-svc-1",
			Status: statusFinished,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, okHTTP, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "api", "action": "disable",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var sr ops.SubdomainResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &sr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if sr.Process == nil {
		t.Fatal("expected process in result")
	}
	if sr.Process.Status != statusFinished {
		t.Errorf("process status = %q, want %q (tool must poll to completion)", sr.Process.Status, statusFinished)
	}
}

func TestSubdomainTool_InvalidAction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, okHTTP, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "api", "action": "toggle",
	})

	if !result.IsError {
		t.Error("expected IsError for invalid action")
	}
}

func TestSubdomainTool_EmptyHostname(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, okHTTP, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "", "action": "enable",
	})

	if !result.IsError {
		t.Error("expected IsError for empty hostname")
	}
}

func TestSubdomainTool_Enable_FailedProcess_TreatedAsAlreadyEnabled(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app",
				Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
		}).
		WithService(&platform.ServiceStack{
			ID: "svc-1", Name: "app",
			Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
		}).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "myproject", Status: statusActive,
			SubdomainHost: "abc1.prg1.zerops.app",
		}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-enable-svc-1",
			Status: statusFailed, // API sometimes returns FAILED instead of error
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, okHTTP, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "app", "action": "enable",
	})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var sr ops.SubdomainResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &sr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if sr.Status != "already_enabled" {
		t.Errorf("status = %q, want %q", sr.Status, "already_enabled")
	}
	if sr.Process != nil {
		t.Error("process should be nil for already_enabled")
	}
	if len(sr.SubdomainUrls) == 0 {
		t.Error("expected subdomainUrls to be populated")
	}
	// Plan 1 commit 3: FAILED normalization must NOT silently swallow the
	// platform's signal. Warnings records the normalization reason so callers
	// can distinguish a genuine TOCTOU race from a future unknown failure
	// mode that happens to pass the URLs-present heuristic.
	if len(sr.Warnings) == 0 {
		t.Error("expected Warnings to be populated when FAILED process is normalized")
	}
}

// Plan 1 commit 3: FailReason from the FAILED process must survive the
// normalization. If the platform ever introduces a new failure mode, the
// reason is visible in Warnings rather than silently dropped.
func TestSubdomainTool_Enable_FailedWithFailReason_PreservedInWarnings(t *testing.T) {
	t.Parallel()
	reason := "subdomain config drift — investigate"
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app",
				Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
		}).
		WithService(&platform.ServiceStack{
			ID: "svc-1", Name: "app",
			Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
		}).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "myproject", Status: statusActive,
			SubdomainHost: "abc1.prg1.zerops.app",
		}).
		WithProcess(&platform.Process{
			ID:         "proc-subdomain-enable-svc-1",
			Status:     statusFailed,
			FailReason: &reason,
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, okHTTP, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "app", "action": "enable",
	})

	var sr ops.SubdomainResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &sr); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if len(sr.Warnings) == 0 {
		t.Fatal("expected Warnings to be populated")
	}
	// At least one warning must reference the FailReason string so a reader
	// can identify the underlying cause.
	found := false
	for _, w := range sr.Warnings {
		if strings.Contains(w, reason) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Warnings must preserve FailReason %q; got %v", reason, sr.Warnings)
	}
}

// Plan 1 commit 4: process poll failure (timeout, transient API error on
// GetProcess, etc.) previously discarded via `_ :=`, so the tool returned as
// if enable succeeded even though the state was unknown. Now the timeout
// surfaces in Warnings and the agent can decide to retry or check status.
func TestSubdomainTool_Enable_PollFailure_SurfacedAsWarning(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app",
				Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
		}).
		WithService(&platform.ServiceStack{
			ID: "svc-1", Name: "app", SubdomainAccess: false,
			Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
		}).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "myproject", Status: statusActive,
			SubdomainHost: "abc1.prg1.zerops.app",
		}).
		// Enable returns a Process ID; GetProcess (used by pollManageProcess)
		// fails — simulates a transient API hiccup during the poll.
		WithError("GetProcess", &platform.PlatformError{
			Code:    "API_ERROR",
			Message: "transient failure",
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, okHTTP, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "app", "action": "enable",
	})
	if result.IsError {
		t.Fatalf("poll failure must not fail the tool call: %s", getTextContent(t, result))
	}

	var sr ops.SubdomainResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &sr); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if len(sr.Warnings) == 0 {
		t.Fatal("expected Warnings populated on poll timeout, got empty")
	}
	found := false
	for _, w := range sr.Warnings {
		if strings.Contains(w, "poll timed out") || strings.Contains(w, "stale") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Warnings must mention poll timeout/stale state; got %v", sr.Warnings)
	}
}

// Plan 1 commit 5: after enable returns with SubdomainUrls, tool must wait
// for each URL to respond <500 before returning. Empirical L7 propagation
// window is 440ms-1.3s — without this wait the agent's next zerops_verify
// races the L7 balancer and can get a false fail on http_root.
type sequencingHTTPFor500 struct {
	remaining5xx int
}

func (s *sequencingHTTPFor500) Do(*http.Request) (*http.Response, error) {
	if s.remaining5xx > 0 {
		s.remaining5xx--
		return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func TestSubdomainTool_Enable_WaitsForHTTPReady(t *testing.T) {
	t.Parallel()
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 500*time.Millisecond)
	defer restore()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app",
				Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
		}).
		WithService(&platform.ServiceStack{
			ID: "svc-1", Name: "app", SubdomainAccess: false,
			Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
		}).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "myproject", Status: statusActive,
			SubdomainHost: "abc1.prg1.zerops.app",
		}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-enable-svc-1",
			Status: statusFinished,
		})

	// Stub HTTP returns 502 twice (L7 warming), then 200. WaitHTTPReady
	// must retry and eventually succeed without any warning.
	stub := &sequencingHTTPFor500{remaining5xx: 2}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, stub, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "app", "action": "enable",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var sr ops.SubdomainResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &sr); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	// No warnings when WaitHTTPReady eventually succeeds.
	for _, w := range sr.Warnings {
		if strings.Contains(w, "not HTTP-ready") {
			t.Errorf("should not warn when retry succeeded, got: %s", w)
		}
	}
}

// If HTTP readiness times out, the tool must NOT fail the call — appends
// warning and returns. Verify still has a chance to reach the URL on its
// own probe.
func TestSubdomainTool_Enable_HTTPReadyTimeout_WarningNotFatal(t *testing.T) {
	t.Parallel()
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 10*time.Millisecond)
	defer restore()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app",
				Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
		}).
		WithService(&platform.ServiceStack{
			ID: "svc-1", Name: "app", SubdomainAccess: false,
			Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
		}).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "myproject", Status: statusActive,
			SubdomainHost: "abc1.prg1.zerops.app",
		}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-enable-svc-1",
			Status: statusFinished,
		})

	// Stub HTTP always returns 503 — WaitHTTPReady will time out.
	stub := &sequencingHTTPFor500{remaining5xx: 999}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, stub, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "app", "action": "enable",
	})
	if result.IsError {
		t.Fatalf("HTTP timeout must NOT fail the tool call: %s", getTextContent(t, result))
	}

	var sr ops.SubdomainResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &sr); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	found := false
	for _, w := range sr.Warnings {
		if strings.Contains(w, "not HTTP-ready") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Warnings to mention HTTP readiness timeout; got %v", sr.Warnings)
	}
}

func TestSubdomainTool_EmptyAction(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterSubdomain(srv, mock, okHTTP, "proj-1")

	result := callTool(t, srv, "zerops_subdomain", map[string]any{
		"serviceHostname": "api", "action": "",
	})

	if !result.IsError {
		t.Error("expected IsError for empty action")
	}
}
