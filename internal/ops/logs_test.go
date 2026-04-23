// Tests for: plans/analysis/ops.md § ops/logs.go
package ops

import (
	"context"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// recentTS returns a timestamp within parseSince's default 1h window so these
// tests exercise real filter behaviour (the mock applies Since per Phase 2).
func recentTS(offsetSeconds int) string {
	return time.Now().UTC().Add(time.Duration(offsetSeconds) * time.Second).Format(time.RFC3339Nano)
}

func TestFetchLogs_Success(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		}).
		WithLogAccess(&platform.LogAccess{
			AccessToken: "token",
			URL:         "https://logs.example.com",
		})

	fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
		{Timestamp: recentTS(-120), Severity: "info", Facility: "local0", Message: "started"},
		{Timestamp: recentTS(-60), Severity: "info", Facility: "local0", Message: "ready"},
	})

	result, err := FetchLogs(context.Background(), mock, fetcher, "proj-1", "api", "", "", 100, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}
	if result.Entries[0].Message != "started" {
		t.Errorf("expected first message=started, got %s", result.Entries[0].Message)
	}
}

func TestFetchLogs_ServiceNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		})

	fetcher := platform.NewMockLogFetcher()

	_, err := FetchLogs(context.Background(), mock, fetcher, "proj-1", "missing", "", "", 100, "")
	if err == nil {
		t.Fatal("expected error")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrServiceNotFound {
		t.Errorf("expected code %s, got %s", platform.ErrServiceNotFound, pe.Code)
	}
}

func TestFetchLogs_EmptyResult(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		}).
		WithLogAccess(&platform.LogAccess{
			AccessToken: "token",
			URL:         "https://logs.example.com",
		})

	fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{})

	result, err := FetchLogs(context.Background(), mock, fetcher, "proj-1", "api", "", "", 100, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(result.Entries))
	}
	if result.HasMore {
		t.Error("expected hasMore=false for empty result")
	}
}

func TestFetchLogs_HasMore(t *testing.T) {
	t.Parallel()

	// HasMore is true when backend has more entries than requested limit.
	// FetchLogs requests limit+1 internally, so mock must return >limit entries.
	entries := make([]platform.LogEntry, 101)
	for i := range entries {
		entries[i] = platform.LogEntry{Timestamp: recentTS(-i), Severity: "info", Facility: "local0", Message: "log"}
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		}).
		WithLogAccess(&platform.LogAccess{
			AccessToken: "token",
			URL:         "https://logs.example.com",
		})

	fetcher := platform.NewMockLogFetcher().WithEntries(entries)

	result, err := FetchLogs(context.Background(), mock, fetcher, "proj-1", "api", "", "", 100, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasMore {
		t.Error("expected hasMore=true when backend has more entries than limit")
	}
	if len(result.Entries) != 100 {
		t.Errorf("expected 100 entries (trimmed to limit), got %d", len(result.Entries))
	}
}

func TestFetchLogs_HasMore_ExactBoundary(t *testing.T) {
	t.Parallel()

	// Exactly limit entries should NOT report hasMore (no false positive).
	entries := make([]platform.LogEntry, 100)
	for i := range entries {
		entries[i] = platform.LogEntry{Timestamp: recentTS(-i), Severity: "info", Facility: "local0", Message: "log"}
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		}).
		WithLogAccess(&platform.LogAccess{
			AccessToken: "token",
			URL:         "https://logs.example.com",
		})

	fetcher := platform.NewMockLogFetcher().WithEntries(entries)

	result, err := FetchLogs(context.Background(), mock, fetcher, "proj-1", "api", "", "", 100, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasMore {
		t.Error("expected hasMore=false when entries.len == limit (exact boundary)")
	}
}

func TestFetchLogs_InvalidSince(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		})

	fetcher := platform.NewMockLogFetcher()

	_, err := FetchLogs(context.Background(), mock, fetcher, "proj-1", "api", "", "badformat", 100, "")
	if err == nil {
		t.Fatal("expected error for bad since format")
	}
}

func TestFetchLogs_DefaultLimit(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		}).
		WithLogAccess(&platform.LogAccess{
			AccessToken: "token",
			URL:         "https://logs.example.com",
		})

	fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{})

	// limit=0 should default to 100
	result, err := FetchLogs(context.Background(), mock, fetcher, "proj-1", "api", "", "", 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
