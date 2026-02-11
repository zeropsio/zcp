// Tests for: plans/analysis/ops.md § ops/logs.go
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

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
		{Timestamp: "2024-01-01T00:00:00Z", Severity: "info", Message: "started"},
		{Timestamp: "2024-01-01T00:00:01Z", Severity: "info", Message: "ready"},
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

	entries := make([]platform.LogEntry, 100)
	for i := range entries {
		entries[i] = platform.LogEntry{Timestamp: "2024-01-01T00:00:00Z", Severity: "info", Message: "log"}
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
		t.Error("expected hasMore=true when entries.len >= limit")
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
