// Tests for: plans/analysis/ops.md § ops/logs.go
package ops

import (
	"context"
	"strings"
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
			URL: "https://logs.example.com",
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
			URL: "https://logs.example.com",
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
			URL: "https://logs.example.com",
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
			URL: "https://logs.example.com",
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
			URL: "https://logs.example.com",
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

// TestFetchLogs_EmptyResultEnrichment pins B7: a 0-entry result explains WHY
// it is empty from the live service status and points failed/never-started
// services at zerops_events (where their diagnosis actually lives), instead of
// a bare {entries:[],hasMore:false} the agent gropes at.
func TestFetchLogs_EmptyResultEnrichment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		status        string
		priorDeploy   bool
		wantStatus    string
		reasonHas     string
		wantEventsRec bool
	}{
		{"failed → events", platform.ServiceStatusFailed, false, "FAILED", "zerops_events", true},
		{"ready+prior deploy → events", platform.ServiceStatusReadyToDeploy, true, "READY_TO_DEPLOY", "zerops_events", true},
		{"ready, never deployed → deploy first", platform.ServiceStatusReadyToDeploy, false, "READY_TO_DEPLOY", "nothing has been deployed", false},
		{"active → filters, 1h default", platform.ServiceStatusActive, false, "ACTIVE", "last 1h", false},
		{"creating → provisioning", "CREATING", false, "CREATING", "still provisioning", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: tc.status}}).
				WithLogAccess(&platform.LogAccess{URL: "https://logs.example.com"})
			if tc.priorDeploy {
				mock = mock.WithAppVersionEvents([]platform.AppVersionEvent{
					{ID: "av-1", ServiceStackID: "svc-1", Source: "GIT", Status: platform.BuildStatusBuildFailed, Created: "2026-06-05T10:00:00Z"},
				})
			}
			fetcher := platform.NewMockLogFetcher() // no entries → empty
			result, err := FetchLogs(context.Background(), mock, fetcher, "proj-1", "api", "", "", 100, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result.Entries) != 0 {
				t.Fatalf("expected empty entries")
			}
			if result.ServiceStatus != tc.wantStatus {
				t.Errorf("ServiceStatus = %q, want %q", result.ServiceStatus, tc.wantStatus)
			}
			if !strings.Contains(result.EmptyReason, tc.reasonHas) {
				t.Errorf("EmptyReason %q should contain %q", result.EmptyReason, tc.reasonHas)
			}
			if tc.wantEventsRec {
				if result.Recovery == nil || result.Recovery.Tool != "zerops_events" {
					t.Errorf("expected zerops_events recovery, got %+v", result.Recovery)
				}
			} else if result.Recovery != nil {
				t.Errorf("expected no recovery, got %+v", result.Recovery)
			}
		})
	}
}
