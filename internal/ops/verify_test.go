// Tests for: zerops_verify — health verification checks.
package ops

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// callbackLogFetcher returns different log entries based on the fetch params.
type callbackLogFetcher struct {
	fn func(params platform.LogFetchParams) ([]platform.LogEntry, error)
}

func (f *callbackLogFetcher) FetchLogs(_ context.Context, _ *platform.LogAccess, params platform.LogFetchParams) ([]platform.LogEntry, error) {
	return f.fn(params)
}

// --- isManagedCategory ---

func TestIsManagedCategory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		category string
		want     bool
	}{
		{"STANDARD", true},
		{"SHARED_STORAGE", true},
		{"OBJECT_STORAGE", true},
		{"USER", false},
		{"CORE", false},
		{"BUILD", false},
		{"INTERNAL", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			t.Parallel()
			got := isManagedCategory(tt.category)
			if got != tt.want {
				t.Errorf("isManagedCategory(%q) = %v, want %v", tt.category, got, tt.want)
			}
		})
	}
}

// --- classifyRuntime ---

func TestClassifyRuntime_Dynamic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		serviceType string
		hasPorts    bool
	}{
		{"nodejs@22", true},
		{"go@1", true},
		{"bun@1.2", true},
		{"python@3.12", true},
		{"rust@stable", true},
		{"java@21", true},
		{"deno@2", true},
		{"dotnet@8", true},
	}

	for _, tt := range tests {
		t.Run(tt.serviceType, func(t *testing.T) {
			t.Parallel()
			got := classifyRuntime(tt.serviceType, tt.hasPorts)
			if got != RuntimeDynamic {
				t.Errorf("classifyRuntime(%q, %v) = %v, want RuntimeDynamic", tt.serviceType, tt.hasPorts, got)
			}
		})
	}
}

func TestClassifyRuntime_Static(t *testing.T) {
	t.Parallel()

	tests := []struct {
		serviceType string
		hasPorts    bool
	}{
		{"static", true},
		{"nginx@1.22", true},
	}

	for _, tt := range tests {
		t.Run(tt.serviceType, func(t *testing.T) {
			t.Parallel()
			got := classifyRuntime(tt.serviceType, tt.hasPorts)
			if got != RuntimeStatic {
				t.Errorf("classifyRuntime(%q, %v) = %v, want RuntimeStatic", tt.serviceType, tt.hasPorts, got)
			}
		})
	}
}

func TestClassifyRuntime_Implicit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		serviceType string
		hasPorts    bool
	}{
		{"php-apache@8.3", true},
		{"php-nginx@8.4", true},
	}

	for _, tt := range tests {
		t.Run(tt.serviceType, func(t *testing.T) {
			t.Parallel()
			got := classifyRuntime(tt.serviceType, tt.hasPorts)
			if got != RuntimeImplicit {
				t.Errorf("classifyRuntime(%q, %v) = %v, want RuntimeImplicit", tt.serviceType, tt.hasPorts, got)
			}
		})
	}
}

func TestClassifyRuntime_Worker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		serviceType string
		hasPorts    bool
	}{
		{"nodejs@22", false},
		{"go@1", false},
		{"python@3.12", false},
	}

	for _, tt := range tests {
		t.Run(tt.serviceType, func(t *testing.T) {
			t.Parallel()
			got := classifyRuntime(tt.serviceType, tt.hasPorts)
			if got != RuntimeWorker {
				t.Errorf("classifyRuntime(%q, %v) = %v, want RuntimeWorker", tt.serviceType, tt.hasPorts, got)
			}
		})
	}
}

// --- Verify orchestrator tests ---

func TestVerify_DynamicRuntime_AllChecks(t *testing.T) {
	t.Parallel()

	// Dynamic runtime (nodejs) with subdomain disabled → HTTP checks skip. Log+service checks all pass.
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: "ACTIVE", SubdomainAccess: false, Ports: []platform.Port{{Port: 3000}}},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})

	fetcher := &callbackLogFetcher{fn: func(params platform.LogFetchParams) ([]platform.LogEntry, error) {
		if params.Severity == "error" {
			return nil, nil // no errors
		}
		if params.Search != "" {
			return []platform.LogEntry{{Message: "listening on port 3000"}}, nil
		}
		return nil, nil
	}}

	result, err := Verify(context.Background(), mock, fetcher, http.DefaultClient, "proj-1", "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Hostname != "app" {
		t.Errorf("Hostname = %q, want %q", result.Hostname, "app")
	}
	if result.Type != "runtime" {
		t.Errorf("Type = %q, want %q", result.Type, "runtime")
	}
	if result.Status != "healthy" {
		t.Errorf("Status = %q, want %q", result.Status, "healthy")
	}
	// Dynamic: service_running, error_logs, startup_detected, http_root = 4
	if len(result.Checks) != 4 {
		t.Fatalf("Checks count = %d, want 4; checks: %v", len(result.Checks), checkNames(result.Checks))
	}

	findCheck(t, result, "service_running", "pass")
	findCheck(t, result, "error_logs", "pass")
	findCheck(t, result, "startup_detected", "pass")
	// HTTP check skip (no subdomain).
	findCheck(t, result, "http_root", "skip")
}

func TestVerify_RuntimeStopped(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: "READY_TO_DEPLOY", Ports: []platform.Port{{Port: 3000}}},
		})

	result, err := Verify(context.Background(), mock, platform.NewMockLogFetcher(), http.DefaultClient, "proj-1", "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "unhealthy" {
		t.Errorf("Status = %q, want unhealthy", result.Status)
	}
	// Dynamic stopped: 4 checks (service_running fail + 3 skip)
	if len(result.Checks) != 4 {
		t.Fatalf("Checks count = %d, want 4; checks: %v", len(result.Checks), checkNames(result.Checks))
	}
	if result.Checks[0].Status != "fail" {
		t.Errorf("service_running status = %q, want fail", result.Checks[0].Status)
	}
	if !strings.Contains(result.Checks[0].Detail, "READY_TO_DEPLOY") {
		t.Errorf("service_running detail = %q, want to contain READY_TO_DEPLOY", result.Checks[0].Detail)
	}
	for i := 1; i < len(result.Checks); i++ {
		if result.Checks[i].Status != "skip" {
			t.Errorf("Check %q: status = %q, want skip", result.Checks[i].Name, result.Checks[i].Status)
		}
	}
}

func TestVerify_RuntimeErrorLogs(t *testing.T) {
	t.Parallel()

	// Error logs are advisory (CheckInfo) — they should NOT cause degraded status.
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: "RUNNING", Ports: []platform.Port{{Port: 3000}}},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})

	fetcher := &callbackLogFetcher{fn: func(params platform.LogFetchParams) ([]platform.LogEntry, error) {
		if params.Severity == "error" {
			return []platform.LogEntry{{Message: "TypeError: cannot read property"}}, nil
		}
		if params.Search != "" {
			return []platform.LogEntry{{Message: "listening"}}, nil
		}
		return nil, nil
	}}

	result, err := Verify(context.Background(), mock, fetcher, http.DefaultClient, "proj-1", "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "healthy" {
		t.Errorf("Status = %q, want healthy", result.Status)
	}
	findCheck(t, result, "error_logs", "info")
}

func TestVerify_RuntimeNoStartup(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: "RUNNING", Ports: []platform.Port{{Port: 3000}}},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})

	fetcher := &callbackLogFetcher{fn: func(params platform.LogFetchParams) ([]platform.LogEntry, error) {
		return nil, nil // no entries for any query
	}}

	result, err := Verify(context.Background(), mock, fetcher, http.DefaultClient, "proj-1", "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "degraded" {
		t.Errorf("Status = %q, want degraded", result.Status)
	}
	findCheck(t, result, "startup_detected", "fail")
}

func TestVerify_RuntimeNoSubdomain(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: "RUNNING", SubdomainAccess: false, Ports: []platform.Port{{Port: 3000}}},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})

	fetcher := &callbackLogFetcher{fn: func(params platform.LogFetchParams) ([]platform.LogEntry, error) {
		if params.Search != "" {
			return []platform.LogEntry{{Message: "listening"}}, nil
		}
		return nil, nil
	}}

	result, err := Verify(context.Background(), mock, fetcher, http.DefaultClient, "proj-1", "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No subdomain → http_root skips, but service+logs pass
	httpRoot := findCheck(t, result, "http_root", "skip")
	if !strings.Contains(httpRoot.Detail, "subdomain not enabled") {
		t.Errorf("http_root detail = %q, want to contain 'subdomain not enabled'", httpRoot.Detail)
	}
}

func TestVerify_RuntimeCrashLoop(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: "RUNNING", Ports: []platform.Port{{Port: 3000}}},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})

	fetcher := &callbackLogFetcher{fn: func(params platform.LogFetchParams) ([]platform.LogEntry, error) {
		if params.Severity == "error" {
			return []platform.LogEntry{{Message: "Error: Cannot find module 'express'"}}, nil
		}
		if params.Search != "" {
			return nil, nil // no startup marker
		}
		return nil, nil
	}}

	result, err := Verify(context.Background(), mock, fetcher, http.DefaultClient, "proj-1", "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "degraded" {
		t.Errorf("Status = %q, want degraded (crash loop = running + errors + no startup)", result.Status)
	}

	findCheck(t, result, "service_running", "pass")
	findCheck(t, result, "error_logs", "info")
	findCheck(t, result, "startup_detected", "fail")
}

func TestVerify_StaticRuntime_SkipsStatusAndStartup(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "web", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "static", ServiceStackTypeCategoryName: "USER"}, Status: "RUNNING", SubdomainAccess: false, Ports: []platform.Port{{Port: 80}}},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})

	result, err := Verify(context.Background(), mock, platform.NewMockLogFetcher(), http.DefaultClient, "proj-1", "web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "healthy" {
		t.Errorf("Status = %q, want healthy", result.Status)
	}
	// Static: service_running + http_root = 2 checks (no logs, no startup, no status)
	if len(result.Checks) != 2 {
		t.Fatalf("Checks count = %d, want 2; checks: %v", len(result.Checks), checkNames(result.Checks))
	}
	findCheck(t, result, "service_running", "pass")
	findCheck(t, result, "http_root", "skip") // no subdomain
}

func TestVerify_ImplicitWebserver_SkipsStartup(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "phpapp", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "php-nginx@8.4", ServiceStackTypeCategoryName: "USER"}, Status: "RUNNING", SubdomainAccess: false, Ports: []platform.Port{{Port: 80}}},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})

	fetcher := &callbackLogFetcher{fn: func(params platform.LogFetchParams) ([]platform.LogEntry, error) {
		return nil, nil
	}}

	result, err := Verify(context.Background(), mock, fetcher, http.DefaultClient, "proj-1", "phpapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "healthy" {
		t.Errorf("Status = %q, want healthy", result.Status)
	}
	// Implicit: service_running + error_logs + http_root = 3 checks (no startup)
	if len(result.Checks) != 3 {
		t.Fatalf("Checks count = %d, want 3; checks: %v", len(result.Checks), checkNames(result.Checks))
	}
	findCheck(t, result, "service_running", "pass")
	findCheck(t, result, "error_logs", "pass")
	findCheck(t, result, "http_root", "skip") // no subdomain

	// Verify startup_detected is NOT present.
	for _, c := range result.Checks {
		if c.Name == "startup_detected" {
			t.Error("startup_detected should not be present for implicit webserver")
		}
	}
}

func TestVerify_WorkerRuntime_NoHTTPChecks(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "worker", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: "RUNNING"},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})

	fetcher := &callbackLogFetcher{fn: func(params platform.LogFetchParams) ([]platform.LogEntry, error) {
		return nil, nil
	}}

	result, err := Verify(context.Background(), mock, fetcher, http.DefaultClient, "proj-1", "worker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "healthy" {
		t.Errorf("Status = %q, want healthy", result.Status)
	}
	// Worker: service_running + error_logs = 2 checks
	if len(result.Checks) != 2 {
		t.Fatalf("Checks count = %d, want 2; checks: %v", len(result.Checks), checkNames(result.Checks))
	}
	findCheck(t, result, "service_running", "pass")
	findCheck(t, result, "error_logs", "pass")

	// No HTTP checks or startup_detected for workers.
	for _, c := range result.Checks {
		switch c.Name {
		case "http_root", "startup_detected":
			t.Errorf("check %q should not be present for worker runtime", c.Name)
		}
	}
}

func TestVerify_ManagedRunning(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16", ServiceStackTypeCategoryName: "STANDARD"}, Status: "RUNNING"},
		})

	result, err := Verify(context.Background(), mock, platform.NewMockLogFetcher(), http.DefaultClient, "proj-1", "db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Hostname != "db" {
		t.Errorf("Hostname = %q, want db", result.Hostname)
	}
	if result.Type != "managed" {
		t.Errorf("Type = %q, want managed", result.Type)
	}
	if result.Status != "healthy" {
		t.Errorf("Status = %q, want healthy", result.Status)
	}
	if len(result.Checks) != 1 {
		t.Fatalf("Checks count = %d, want 1", len(result.Checks))
	}
	if result.Checks[0].Name != "service_running" {
		t.Errorf("Check name = %q, want service_running", result.Checks[0].Name)
	}
	if result.Checks[0].Status != "pass" {
		t.Errorf("Check status = %q, want pass", result.Checks[0].Status)
	}
}

func TestVerify_ManagedStopped(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16", ServiceStackTypeCategoryName: "STANDARD"}, Status: "RESTARTING"},
		})

	result, err := Verify(context.Background(), mock, platform.NewMockLogFetcher(), http.DefaultClient, "proj-1", "db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "unhealthy" {
		t.Errorf("Status = %q, want unhealthy", result.Status)
	}
	if result.Checks[0].Status != "fail" {
		t.Errorf("service_running status = %q, want fail", result.Checks[0].Status)
	}
	if !strings.Contains(result.Checks[0].Detail, "RESTARTING") {
		t.Errorf("detail = %q, want to contain RESTARTING", result.Checks[0].Detail)
	}
}

func TestVerify_LogFetchError(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: "RUNNING", Ports: []platform.Port{{Port: 3000}}},
		}).
		WithError("GetProjectLog", fmt.Errorf("log backend down"))

	result, err := Verify(context.Background(), mock, platform.NewMockLogFetcher(), http.DefaultClient, "proj-1", "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Log checks should be skip, not fail.
	for _, c := range result.Checks {
		if c.Name == "error_logs" || c.Name == "startup_detected" {
			if c.Status != "skip" {
				t.Errorf("Check %q: status = %q, want skip", c.Name, c.Status)
			}
			if !strings.Contains(c.Detail, "log backend unavailable") {
				t.Errorf("Check %q: detail = %q, want to contain 'log backend unavailable'", c.Name, c.Detail)
			}
		}
	}
}

func TestVerify_ServiceNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})

	_, err := Verify(context.Background(), mock, platform.NewMockLogFetcher(), http.DefaultClient, "proj-1", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent service")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrServiceNotFound {
		t.Errorf("error code = %q, want %q", pe.Code, platform.ErrServiceNotFound)
	}
}

// --- Individual check function tests ---

func TestCheckHTTPRoot_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Hello World")
	}))
	defer srv.Close()

	c := checkHTTPRoot(context.Background(), srv.Client(), srv.URL)
	if c.Status != CheckPass {
		t.Errorf("status = %q, want pass", c.Status)
	}
	if c.Name != "http_root" {
		t.Errorf("name = %q, want http_root", c.Name)
	}
	if c.HTTPStatus != 200 {
		t.Errorf("httpStatus = %d, want 200", c.HTTPStatus)
	}
}

// TestCheckHTTPRoot_NonFailingStatuses locks the "any non-5xx is a
// pass" rule. Any response from the HTTP server proves it is listening
// and serving HTTP — which is the only question http_root asks. 4xx
// is legitimate for API-only services that don't route the root path.
// 3xx is legitimate for frameworks that redirect / to /app or similar.
// The rule change was made after every showcase run ever flagged apidev
// as "degraded" because /api/health works but / returns 404.
func TestCheckHTTPRoot_NonFailingStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		handler        http.HandlerFunc
		wantHTTPStatus int
	}{
		{
			name: "404 Not Found (API-only service with no root route)",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, "Not Found")
			},
			wantHTTPStatus: 404,
		},
		{
			name: "401 Unauthorized (auth-gated root)",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, "Unauthorized")
			},
			wantHTTPStatus: 401,
		},
		{
			name: "405 Method Not Allowed",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusMethodNotAllowed)
			},
			wantHTTPStatus: 405,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewTLSServer(tt.handler)
			defer srv.Close()

			c := checkHTTPRoot(context.Background(), srv.Client(), srv.URL)
			if c.Status != CheckPass {
				t.Errorf("status = %q, want pass (any HTTP response proves the server is alive)", c.Status)
			}
			if c.HTTPStatus != tt.wantHTTPStatus {
				t.Errorf("httpStatus = %d, want %d", c.HTTPStatus, tt.wantHTTPStatus)
			}
		})
	}
}

// TestCheckHTTPRoot_ServerError_Fail locks the other half of the rule:
// 5xx responses DO fail the check. The server is reachable but broken.
// This is the only status-based failure mode for http_root.
func TestCheckHTTPRoot_ServerError_Fail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		handler        http.HandlerFunc
		wantDetail     string
		wantHTTPStatus int
	}{
		{
			name: "502 Bad Gateway",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
				fmt.Fprint(w, "Bad Gateway")
			},
			wantDetail:     "HTTP 502",
			wantHTTPStatus: 502,
		},
		{
			name: "500 Internal Server Error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, "Internal Server Error")
			},
			wantDetail:     "HTTP 500",
			wantHTTPStatus: 500,
		},
		{
			name: "503 Service Unavailable",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			},
			wantDetail:     "HTTP 503",
			wantHTTPStatus: 503,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewTLSServer(tt.handler)
			defer srv.Close()

			c := checkHTTPRoot(context.Background(), srv.Client(), srv.URL)
			if c.Status != CheckFail {
				t.Errorf("status = %q, want fail", c.Status)
			}
			if !strings.Contains(c.Detail, tt.wantDetail) {
				t.Errorf("detail = %q, want to contain %q", c.Detail, tt.wantDetail)
			}
			if c.HTTPStatus != tt.wantHTTPStatus {
				t.Errorf("httpStatus = %d, want %d", c.HTTPStatus, tt.wantHTTPStatus)
			}
		})
	}
}

func TestBatchLogChecks_NoErrors_Pass(t *testing.T) {
	t.Parallel()

	logAccess := &platform.LogAccess{URL: "http://logs.test"}
	fetcher := &callbackLogFetcher{fn: func(params platform.LogFetchParams) ([]platform.LogEntry, error) {
		return nil, nil // no errors
	}}

	checks := batchLogChecks(context.Background(), fetcher, logAccess, nil, "svc-1")
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].Name != "error_logs" {
		t.Errorf("name = %q, want error_logs", checks[0].Name)
	}
	if checks[0].Status != CheckPass {
		t.Errorf("status = %q, want pass", checks[0].Status)
	}
}

func TestBatchLogChecks_ErrorsFound_Info(t *testing.T) {
	t.Parallel()

	logAccess := &platform.LogAccess{URL: "http://logs.test"}
	fetcher := &callbackLogFetcher{fn: func(params platform.LogFetchParams) ([]platform.LogEntry, error) {
		if params.Severity == "error" {
			return []platform.LogEntry{
				{Message: "Error: something broke"},
				{Message: "Error: another thing"},
			}, nil
		}
		return nil, nil
	}}

	checks := batchLogChecks(context.Background(), fetcher, logAccess, nil, "svc-1")
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].Status != CheckInfo {
		t.Errorf("status = %q, want info", checks[0].Status)
	}
	if !strings.Contains(checks[0].Detail, "Error: something broke") {
		t.Errorf("detail = %q, want to contain error message", checks[0].Detail)
	}
}

func TestBatchLogChecks_LogBackendError_Skip(t *testing.T) {
	t.Parallel()

	checks := batchLogChecks(context.Background(), platform.NewMockLogFetcher(), nil, fmt.Errorf("log backend down"), "svc-1")
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].Status != CheckSkip {
		t.Errorf("status = %q, want skip", checks[0].Status)
	}
}

func TestCheckServiceRunning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     string
		wantStatus string
	}{
		{"running", "RUNNING", "pass"},
		{"active", "ACTIVE", "pass"},
		{"ready to deploy", "READY_TO_DEPLOY", "fail"},
		{"restarting", "RESTARTING", "fail"},
		{"stopped", "STOPPED", "fail"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := &platform.ServiceStack{Status: tt.status}
			c := checkServiceRunning(svc)
			if c.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", c.Status, tt.wantStatus)
			}
		})
	}
}

// --- Status aggregation ---

func TestVerify_StatusAggregation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		checks []CheckResult
		want   string
	}{
		{
			name: "all pass → healthy",
			checks: []CheckResult{
				{Status: "pass"}, {Status: "pass"}, {Status: "pass"},
			},
			want: "healthy",
		},
		{
			name: "pass and skip → healthy",
			checks: []CheckResult{
				{Status: "pass"}, {Status: "skip"}, {Status: "pass"},
			},
			want: "healthy",
		},
		{
			name: "one fail with pass → degraded",
			checks: []CheckResult{
				{Status: "pass"}, {Status: "fail"}, {Status: "pass"},
			},
			want: "degraded",
		},
		{
			name: "service_running fail → unhealthy",
			checks: []CheckResult{
				{Name: "service_running", Status: "fail"}, {Status: "skip"},
			},
			want: "unhealthy",
		},
		{
			name: "info with pass → healthy",
			checks: []CheckResult{
				{Status: "pass"}, {Status: "info"}, {Status: "pass"},
			},
			want: "healthy",
		},
		{
			name: "info with skip → healthy",
			checks: []CheckResult{
				{Status: "pass"}, {Status: "info"}, {Status: "skip"},
			},
			want: "healthy",
		},
		{
			name: "info with fail → degraded (fail wins)",
			checks: []CheckResult{
				{Status: "pass"}, {Status: "info"}, {Status: "fail"},
			},
			want: "degraded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := aggregateStatus(tt.checks)
			if got != tt.want {
				t.Errorf("aggregateStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- truncateBody ---

func TestTruncateBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		max  int
		want string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"truncated", "hello world", 5, "hello..."},
		{"empty", "", 10, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncateBody([]byte(tt.body), tt.max)
			if got != tt.want {
				t.Errorf("truncateBody(%q, %d) = %q, want %q", tt.body, tt.max, got, tt.want)
			}
		})
	}
}

// --- VerifyAll ---

func TestVerifyAll_AllHealthy(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: "RUNNING", Ports: []platform.Port{{Port: 3000}}},
			{ID: "svc-2", Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16", ServiceStackTypeCategoryName: "STANDARD"}, Status: "RUNNING"},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})

	fetcher := &callbackLogFetcher{fn: func(params platform.LogFetchParams) ([]platform.LogEntry, error) {
		if params.Search != "" {
			return []platform.LogEntry{{Message: "listening"}}, nil
		}
		return nil, nil
	}}

	result, err := VerifyAll(context.Background(), mock, fetcher, http.DefaultClient, "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "healthy" {
		t.Errorf("Status = %q, want healthy", result.Status)
	}
	if len(result.Services) != 2 {
		t.Errorf("Services count = %d, want 2", len(result.Services))
	}
	if !strings.Contains(result.Summary, "2/2") {
		t.Errorf("Summary = %q, want to contain '2/2'", result.Summary)
	}
}

func TestVerifyAll_MixedResults(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: "READY_TO_DEPLOY", Ports: []platform.Port{{Port: 3000}}},
			{ID: "svc-2", Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16", ServiceStackTypeCategoryName: "STANDARD"}, Status: "RUNNING"},
		})

	result, err := VerifyAll(context.Background(), mock, platform.NewMockLogFetcher(), http.DefaultClient, "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "degraded" {
		t.Errorf("Status = %q, want degraded", result.Status)
	}
	if len(result.Services) != 2 {
		t.Errorf("Services count = %d, want 2", len(result.Services))
	}
	if !strings.Contains(result.Summary, "1/2") {
		t.Errorf("Summary = %q, want to contain '1/2'", result.Summary)
	}
}

func TestVerifyAll_EmptyProject(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{})

	result, err := VerifyAll(context.Background(), mock, platform.NewMockLogFetcher(), http.DefaultClient, "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "healthy" {
		t.Errorf("Status = %q, want healthy", result.Status)
	}
	if len(result.Services) != 0 {
		t.Errorf("Services count = %d, want 0", len(result.Services))
	}
}

func TestVerifyAll_SingleListServicesCall(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app1", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: "RUNNING", Ports: []platform.Port{{Port: 3000}}},
			{ID: "svc-2", Name: "app2", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "go@1", ServiceStackTypeCategoryName: "USER"}, Status: "RUNNING", Ports: []platform.Port{{Port: 8080}}},
			{ID: "svc-3", Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16", ServiceStackTypeCategoryName: "STANDARD"}, Status: "RUNNING"},
			{ID: "svc-4", Name: "cache", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "valkey@8", ServiceStackTypeCategoryName: "STANDARD"}, Status: "RUNNING"},
			{ID: "svc-5", Name: "worker", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "python@3.12", ServiceStackTypeCategoryName: "USER"}, Status: "RUNNING"},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})

	fetcher := &callbackLogFetcher{fn: func(params platform.LogFetchParams) ([]platform.LogEntry, error) {
		if params.Search != "" {
			return []platform.LogEntry{{Message: "listening"}}, nil
		}
		return nil, nil
	}}

	result, err := VerifyAll(context.Background(), mock, fetcher, http.DefaultClient, "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Services) != 5 {
		t.Errorf("Services count = %d, want 5", len(result.Services))
	}

	// VerifyAll should call ListServices exactly once, not 1+N times.
	calls := mock.CallCounts["ListServices"]
	if calls != 1 {
		t.Errorf("ListServices called %d times, want 1 (was %d before fix)", calls, 1+len(result.Services))
	}
}

// findCheck finds a check by name and asserts its status.
func findCheck(t *testing.T, result *VerifyResult, name, wantStatus string) CheckResult {
	t.Helper()
	for _, c := range result.Checks {
		if c.Name == name {
			if c.Status != wantStatus {
				t.Errorf("Check %q: status = %q, want %q", name, c.Status, wantStatus)
			}
			return c
		}
	}
	t.Fatalf("Check %q not found in %v", name, checkNames(result.Checks))
	return CheckResult{}
}

// checkNames returns a slice of check names for debug output.
func checkNames(checks []CheckResult) []string {
	names := make([]string, len(checks))
	for i, c := range checks {
		names[i] = c.Name
	}
	return names
}
