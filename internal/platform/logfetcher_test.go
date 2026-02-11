package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Tests for: design/zcp-prd.md section 5.7 (Log Fetching)

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
