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

// --- isManagedType ---

func TestIsManagedType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		typeVersion string
		want        bool
	}{
		{"postgresql@16", true},
		{"mariadb@10.11", true},
		{"valkey@7.2", true},
		{"keydb@6", true},
		{"elasticsearch@8", true},
		{"object-storage", true},
		{"shared-storage", true},
		{"kafka@3", true},
		{"nats@2", true},
		{"meilisearch@1.11", true},
		{"clickhouse@24", true},
		{"qdrant@1", true},
		{"typesense@27", true},
		{"rabbitmq@4", true},
		{"nodejs@22", false},
		{"go@1", false},
		{"php-nginx@8.4", false},
		{"python@3.12", false},
		{"rust@stable", false},
		{"bun@1.2", false},
		{"dotnet@9", false},
	}

	for _, tt := range tests {
		t.Run(tt.typeVersion, func(t *testing.T) {
			t.Parallel()
			got := isManagedType(tt.typeVersion)
			if got != tt.want {
				t.Errorf("isManagedType(%q) = %v, want %v", tt.typeVersion, got, tt.want)
			}
		})
	}
}

// --- Verify orchestrator tests ---

func TestVerify_RuntimeAllPass(t *testing.T) {
	t.Parallel()

	// Subdomain disabled → HTTP checks skip. Log+service checks all pass → healthy.
	// HTTP check functions are tested separately in TestCheckHTTPHealth/TestCheckHTTPStatus.
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}, Status: "RUNNING", SubdomainAccess: false},
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
	if len(result.Checks) != 6 {
		t.Fatalf("Checks count = %d, want 6", len(result.Checks))
	}

	// Log checks should pass.
	findCheck(t, result, "service_running", "pass")
	findCheck(t, result, "no_error_logs", "pass")
	findCheck(t, result, "startup_detected", "pass")
	findCheck(t, result, "no_recent_errors", "pass")
	// HTTP checks skip (no subdomain).
	findCheck(t, result, "http_health", "skip")
	findCheck(t, result, "http_status", "skip")
}

func TestVerify_RuntimeStopped(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}, Status: "READY_TO_DEPLOY"},
		})

	result, err := Verify(context.Background(), mock, platform.NewMockLogFetcher(), http.DefaultClient, "proj-1", "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "unhealthy" {
		t.Errorf("Status = %q, want unhealthy", result.Status)
	}
	if len(result.Checks) != 6 {
		t.Fatalf("Checks count = %d, want 6", len(result.Checks))
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

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}, Status: "RUNNING"},
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

	if result.Status != "degraded" {
		t.Errorf("Status = %q, want degraded", result.Status)
	}
	findCheck(t, result, "no_error_logs", "fail")
}

func TestVerify_RuntimeNoStartup(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}, Status: "RUNNING"},
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

func TestVerify_RuntimeRecentErrors(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}, Status: "RUNNING"},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})

	callCount := 0
	fetcher := &callbackLogFetcher{fn: func(params platform.LogFetchParams) ([]platform.LogEntry, error) {
		if params.Severity == "error" {
			callCount++
			if callCount == 1 {
				return nil, nil // first error check (5m): no errors
			}
			// second error check (2m): has errors
			return []platform.LogEntry{{Message: "recent crash"}}, nil
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

	if result.Status != "degraded" {
		t.Errorf("Status = %q, want degraded", result.Status)
	}
	findCheck(t, result, "no_error_logs", "pass")
	findCheck(t, result, "no_recent_errors", "fail")
}

func TestVerify_RuntimeNoSubdomain(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}, Status: "RUNNING", SubdomainAccess: false},
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

	// No subdomain → HTTP checks skip, but service+logs pass → status depends on checks
	healthCheck := findCheck(t, result, "http_health", "skip")
	if !strings.Contains(healthCheck.Detail, "subdomain not enabled") {
		t.Errorf("http_health detail = %q, want to contain 'subdomain not enabled'", healthCheck.Detail)
	}
	statusCheck := findCheck(t, result, "http_status", "skip")
	if !strings.Contains(statusCheck.Detail, "subdomain not enabled") {
		t.Errorf("http_status detail = %q, want to contain 'subdomain not enabled'", statusCheck.Detail)
	}
}

func TestVerify_ManagedRunning(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}, Status: "RUNNING"},
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
			{ID: "svc-1", Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}, Status: "RESTARTING"},
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
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}, Status: "RUNNING"},
		}).
		WithError("GetProjectLog", fmt.Errorf("log backend down"))

	result, err := Verify(context.Background(), mock, platform.NewMockLogFetcher(), http.DefaultClient, "proj-1", "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Log checks should be skip, not fail.
	for _, c := range result.Checks {
		if c.Name == "no_error_logs" || c.Name == "startup_detected" || c.Name == "no_recent_errors" {
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

func TestCheckHTTPHealth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus string
		wantDetail string
	}{
		{
			name: "200 OK",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"status":"ok"}`)
			},
			wantStatus: "pass",
		},
		{
			name: "502 Bad Gateway",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			},
			wantStatus: "fail",
			wantDetail: "HTTP 502",
		},
		{
			name: "500 Internal Server Error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantStatus: "fail",
			wantDetail: "HTTP 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewTLSServer(tt.handler)
			defer srv.Close()

			c := checkHTTPHealth(context.Background(), srv.Client(), srv.URL)
			if c.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", c.Status, tt.wantStatus)
			}
			if tt.wantDetail != "" && !strings.Contains(c.Detail, tt.wantDetail) {
				t.Errorf("detail = %q, want to contain %q", c.Detail, tt.wantDetail)
			}
		})
	}
}

func TestCheckHTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus string
		wantDetail string
	}{
		{
			name: "all connections ok",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"service":"app","connections":{"db":{"status":"ok"},"cache":{"status":"ok"}}}`)
			},
			wantStatus: "pass",
		},
		{
			name: "connection error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"service":"app","connections":{"db":{"status":"error"}}}`)
			},
			wantStatus: "fail",
			wantDetail: "connection 'db': error",
		},
		{
			name: "no connections, top-level ok",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"service":"app","status":"ok"}`)
			},
			wantStatus: "pass",
		},
		{
			name: "no connections, top-level error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"service":"app","status":"error"}`)
			},
			wantStatus: "fail",
			wantDetail: "status: error",
		},
		{
			name: "not JSON",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `<html>not found</html>`)
			},
			wantStatus: "fail",
			wantDetail: "not JSON",
		},
		{
			name: "HTTP 500",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantStatus: "fail",
			wantDetail: "HTTP 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewTLSServer(tt.handler)
			defer srv.Close()

			c := checkHTTPStatus(context.Background(), srv.Client(), srv.URL)
			if c.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", c.Status, tt.wantStatus)
			}
			if tt.wantDetail != "" && !strings.Contains(c.Detail, tt.wantDetail) {
				t.Errorf("detail = %q, want to contain %q", c.Detail, tt.wantDetail)
			}
		})
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
	t.Fatalf("Check %q not found", name)
	return CheckResult{}
}
