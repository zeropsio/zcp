package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// Tests for: Log Fetching (internal/platform/logfetcher.go)

func TestParseLogResponse_ValidJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     string
		wantLen   int
		wantFirst LogEntry
	}{
		{
			name: "well-formed response",
			input: `{"items":[
				{"id":"1","timestamp":"2024-01-01T00:00:00Z","hostname":"app-1","message":"hello","severityLabel":"info"},
				{"id":"2","timestamp":"2024-01-01T00:01:00Z","hostname":"app-2","message":"world","severityLabel":"error"}
			]}`,
			wantLen: 2,
			wantFirst: LogEntry{
				ID: "1", Timestamp: "2024-01-01T00:00:00Z",
				Container: "app-1", Message: "hello", Severity: "info",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entries, err := parseLogResponse([]byte(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(entries) != tt.wantLen {
				t.Fatalf("len = %d, want %d", len(entries), tt.wantLen)
			}
			got := entries[0]
			if got.ID != tt.wantFirst.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.wantFirst.ID)
			}
			if got.Timestamp != tt.wantFirst.Timestamp {
				t.Errorf("Timestamp = %q, want %q", got.Timestamp, tt.wantFirst.Timestamp)
			}
			if got.Container != tt.wantFirst.Container {
				t.Errorf("Container = %q, want %q", got.Container, tt.wantFirst.Container)
			}
			if got.Message != tt.wantFirst.Message {
				t.Errorf("Message = %q, want %q", got.Message, tt.wantFirst.Message)
			}
			if got.Severity != tt.wantFirst.Severity {
				t.Errorf("Severity = %q, want %q", got.Severity, tt.wantFirst.Severity)
			}
		})
	}
}

func TestParseLogResponse_EmptyItems(t *testing.T) {
	t.Parallel()
	entries, err := parseLogResponse([]byte(`{"items":[]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries == nil {
		t.Fatal("entries is nil, want empty slice")
	}
	if len(entries) != 0 {
		t.Errorf("len = %d, want 0", len(entries))
	}
}

func TestParseLogResponse_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := parseLogResponse([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFetchLogs_NilAccess(t *testing.T) {
	t.Parallel()
	f := NewLogFetcher()
	_, err := f.FetchLogs(context.Background(), nil, LogFetchParams{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var pe *PlatformError
	if ok := isPlatformError(err, &pe); !ok {
		t.Fatalf("expected PlatformError, got %T", err)
	}
	if pe.Code != ErrAPIError {
		t.Errorf("code = %q, want %q", pe.Code, ErrAPIError)
	}
}

func TestFetchLogs_URLParsing(t *testing.T) {
	t.Parallel()

	resp := logAPIResponse{Items: []logAPIItem{
		{ID: "1", Timestamp: "2024-01-01T00:00:00Z", Hostname: "h1", Message: "test", SeverityLabel: "info"},
	}}
	respJSON, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respJSON)
	}))
	t.Cleanup(srv.Close)

	tests := []struct {
		name string
		url  string
	}{
		{
			name: "plain URL",
			url:  srv.URL,
		},
		{
			name: "with GET prefix",
			url:  "GET " + srv.URL,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := NewLogFetcher()
			access := &LogAccess{URL: tt.url, AccessToken: "test-token"}
			entries, err := f.FetchLogs(context.Background(), access, LogFetchParams{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(entries) != 1 {
				t.Fatalf("len = %d, want 1", len(entries))
			}
			if entries[0].Message != "test" {
				t.Errorf("Message = %q, want %q", entries[0].Message, "test")
			}
		})
	}
}

func TestFetchLogs_SortAndLimit(t *testing.T) {
	t.Parallel()

	resp := logAPIResponse{Items: []logAPIItem{
		{ID: "3", Timestamp: "2024-01-01T00:03:00Z", Hostname: "h1", Message: "third", SeverityLabel: "info"},
		{ID: "1", Timestamp: "2024-01-01T00:01:00Z", Hostname: "h1", Message: "first", SeverityLabel: "info"},
		{ID: "2", Timestamp: "2024-01-01T00:02:00Z", Hostname: "h1", Message: "second", SeverityLabel: "info"},
	}}
	respJSON, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respJSON)
	}))
	t.Cleanup(srv.Close)

	f := NewLogFetcher()
	access := &LogAccess{URL: srv.URL, AccessToken: "test-token"}
	entries, err := f.FetchLogs(context.Background(), access, LogFetchParams{Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len = %d, want 2", len(entries))
	}
	// Should be chronologically sorted, tail 2 = second and third
	if entries[0].Message != "second" {
		t.Errorf("entries[0].Message = %q, want %q", entries[0].Message, "second")
	}
	if entries[1].Message != "third" {
		t.Errorf("entries[1].Message = %q, want %q", entries[1].Message, "third")
	}
}

// isPlatformError is a test helper to check for PlatformError.
func isPlatformError(err error, target **PlatformError) bool {
	pe, ok := err.(*PlatformError)
	if ok {
		*target = pe
	}
	return ok
}

// ---------------------------------------------------------------------------
// Phase 1 tests — RED first, proving the fundamentals for logging-refactor.
// Live-API contract tests live in logfetcher_build_contract_test.go (tag=api).
// ---------------------------------------------------------------------------

// serveJSON returns an httptest server that writes the given items.
// It also records the last request URL for assertion.
func serveJSON(t *testing.T, items []logAPIItem) (access *LogAccess, lastRequest func() *url.URL) {
	t.Helper()
	respJSON, err := json.Marshal(logAPIResponse{Items: items})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var mu sync.Mutex
	var last *url.URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		u := *r.URL
		last = &u
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respJSON)
	}))
	t.Cleanup(srv.Close)
	return &LogAccess{URL: srv.URL, AccessToken: "test"}, func() *url.URL {
		mu.Lock()
		defer mu.Unlock()
		return last
	}
}

// TestFetchLogs_SinceFilter_ParseCompare — the Since filter must use parsed
// time.Time comparison, not lexicographic string compare. On-wire entries
// arrive with 4–9 fractional digits; lex compare misorders them against a
// Since formatted with a different precision.
func TestFetchLogs_SinceFilter_ParseCompare(t *testing.T) {
	t.Parallel()

	// PipelineStart is typically emitted with 9 fractional digits.
	since := time.Date(2026, 4, 22, 6, 4, 29, 440767629, time.UTC)

	items := []logAPIItem{
		{ID: "a", Timestamp: "2026-04-22T06:04:29Z", Message: "before-ps (same whole sec, no fractional)", SeverityLabel: "info"},
		{ID: "b", Timestamp: "2026-04-22T06:04:29.1Z", Message: "before-ps (100ms into sec)", SeverityLabel: "info"},
		{ID: "c", Timestamp: "2026-04-22T06:04:29.5Z", Message: "after-ps (500ms into sec, past .440)", SeverityLabel: "info"},
		{ID: "d", Timestamp: "2026-04-22T06:04:29.9Z", Message: "after-ps (900ms into sec)", SeverityLabel: "info"},
		{ID: "e", Timestamp: "2026-04-22T06:04:30.000001Z", Message: "after-ps (next sec, 1us)", SeverityLabel: "info"},
	}
	access, _ := serveJSON(t, items)

	f := NewLogFetcher()
	entries, err := f.FetchLogs(context.Background(), access, LogFetchParams{Since: since, Limit: 50})
	if err != nil {
		t.Fatalf("FetchLogs: %v", err)
	}

	// Expect exactly entries c, d, e — they are semantically at/after PipelineStart.
	gotIDs := make([]string, len(entries))
	for i, e := range entries {
		gotIDs[i] = e.ID
	}
	wantIDs := []string{"c", "d", "e"}
	if !equalStrings(gotIDs, wantIDs) {
		t.Errorf("Since-filtered ids = %v, want %v", gotIDs, wantIDs)
	}
}

// TestFetchLogs_FacilityQueryParam — when Facility is set to "application"
// the backend request must carry `facility=16`; empty Facility omits the param.
func TestFetchLogs_FacilityQueryParam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		facility string
		want     string // expected facility query value; "" means param not set
	}{
		{"empty facility omits param", "", ""},
		{"application facility = 16", "application", "16"},
		{"webserver facility = 17", "webserver", "17"},
		{"unknown facility omits param (safe default)", "deadbeef", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			access, lastReq := serveJSON(t, []logAPIItem{{ID: "1", Timestamp: "2026-01-01T00:00:00Z", Message: "x", SeverityLabel: "info"}})
			f := NewLogFetcher()
			_, err := f.FetchLogs(context.Background(), access, LogFetchParams{Facility: tt.facility})
			if err != nil {
				t.Fatalf("FetchLogs: %v", err)
			}
			got := lastReq().Query().Get("facility")
			if got != tt.want {
				t.Errorf("facility query = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFetchLogs_TagsQueryParam — Tags is serialised as CSV in `tags=`.
func TestFetchLogs_TagsQueryParam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tags []string
		want string
	}{
		{"no tags omits param", nil, ""},
		{"empty slice omits param", []string{}, ""},
		{"single tag", []string{"zbuilder@abc"}, "zbuilder@abc"},
		{"multiple tags csv", []string{"zbuilder@abc", "zbuilder@def"}, "zbuilder@abc,zbuilder@def"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			access, lastReq := serveJSON(t, []logAPIItem{{ID: "1", Timestamp: "2026-01-01T00:00:00Z", Message: "x", SeverityLabel: "info"}})
			f := NewLogFetcher()
			_, err := f.FetchLogs(context.Background(), access, LogFetchParams{Tags: tt.tags})
			if err != nil {
				t.Fatalf("FetchLogs: %v", err)
			}
			got := lastReq().Query().Get("tags")
			if got != tt.want {
				t.Errorf("tags query = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFetchLogs_ContainerIDQueryParam — ContainerID is sent as-is.
func TestFetchLogs_ContainerIDQueryParam(t *testing.T) {
	t.Parallel()

	access, lastReq := serveJSON(t, []logAPIItem{{ID: "1", Timestamp: "2026-01-01T00:00:00Z", Message: "x", SeverityLabel: "info"}})
	f := NewLogFetcher()
	_, err := f.FetchLogs(context.Background(), access, LogFetchParams{ContainerID: "container-uuid-123"})
	if err != nil {
		t.Fatalf("FetchLogs: %v", err)
	}
	if got := lastReq().Query().Get("containerId"); got != "container-uuid-123" {
		t.Errorf("containerId query = %q, want %q", got, "container-uuid-123")
	}

	// Empty value must omit the param.
	access2, lastReq2 := serveJSON(t, []logAPIItem{{ID: "1", Timestamp: "2026-01-01T00:00:00Z", Message: "x", SeverityLabel: "info"}})
	_, err = f.FetchLogs(context.Background(), access2, LogFetchParams{})
	if err != nil {
		t.Fatalf("FetchLogs: %v", err)
	}
	if q := lastReq2().Query(); q.Has("containerId") {
		t.Errorf("containerId present when empty: %q", q.Get("containerId"))
	}
}

// TestFetchLogs_SearchClientSide — the backend ignores `search=`, so Search
// is applied client-side as a case-sensitive substring filter on Message.
func TestFetchLogs_SearchClientSide(t *testing.T) {
	t.Parallel()

	items := []logAPIItem{
		{ID: "1", Timestamp: "2026-01-01T00:00:00Z", Message: "foo happens here", SeverityLabel: "info"},
		{ID: "2", Timestamp: "2026-01-01T00:00:01Z", Message: "we have foobar too", SeverityLabel: "info"},
		{ID: "3", Timestamp: "2026-01-01T00:00:02Z", Message: "something totally different", SeverityLabel: "info"},
		{ID: "4", Timestamp: "2026-01-01T00:00:03Z", Message: "FOO in uppercase", SeverityLabel: "info"},
	}
	access, _ := serveJSON(t, items)
	f := NewLogFetcher()

	entries, err := f.FetchLogs(context.Background(), access, LogFetchParams{Search: "foo", Limit: 50})
	if err != nil {
		t.Fatalf("FetchLogs: %v", err)
	}
	// Expect only lowercase "foo" matches — case-sensitive.
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	want := []string{"1", "2"}
	if !equalStrings(ids, want) {
		t.Errorf("Search-filtered ids = %v, want %v", ids, want)
	}
}

// TestFetchLogs_LimitClampLow — Limit of 0 or negative clamps to the default.
func TestFetchLogs_LimitClampLow(t *testing.T) {
	t.Parallel()

	for _, limit := range []int{0, -5, -1} {
		access, lastReq := serveJSON(t, []logAPIItem{{ID: "1", Timestamp: "2026-01-01T00:00:00Z", Message: "x", SeverityLabel: "info"}})
		f := NewLogFetcher()
		_, err := f.FetchLogs(context.Background(), access, LogFetchParams{Limit: limit})
		if err != nil {
			t.Fatalf("Limit=%d: FetchLogs: %v", limit, err)
		}
		if got := lastReq().Query().Get("limit"); got != "100" {
			t.Errorf("Limit=%d: query limit = %q, want 100", limit, got)
		}
	}
}

// TestFetchLogs_LimitClampHigh — Limit above 1000 clamps to 1000.
func TestFetchLogs_LimitClampHigh(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   int
		want string
	}{
		{1000, "1000"},
		{1001, "1000"},
		{5000, "1000"},
		{50000, "1000"},
	}
	for _, tt := range tests {
		access, lastReq := serveJSON(t, []logAPIItem{{ID: "1", Timestamp: "2026-01-01T00:00:00Z", Message: "x", SeverityLabel: "info"}})
		f := NewLogFetcher()
		_, err := f.FetchLogs(context.Background(), access, LogFetchParams{Limit: tt.in})
		if err != nil {
			t.Fatalf("Limit=%d: FetchLogs: %v", tt.in, err)
		}
		if got := lastReq().Query().Get("limit"); got != tt.want {
			t.Errorf("Limit=%d: query limit = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
