// Tests for: ops/build_logs.go — FetchBuildLogs best-effort log retrieval.
package ops

import (
	"context"
	"fmt"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestFetchBuildLogs_Success(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithLogAccess(&platform.LogAccess{
			AccessToken: "tok", URL: "https://log.example.com/logs",
		})
	fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
		{Message: "Installing dependencies..."},
		{Message: "npm error code ERESOLVE"},
		{Message: "Build command failed with exit code 1"},
	})

	event := &platform.AppVersionEvent{
		Build: &platform.BuildInfo{
			ServiceStackID: strPtr("build-svc-1"),
		},
	}

	logs := FetchBuildLogs(context.Background(), mock, fetcher, "proj-1", event, 50)
	if len(logs) != 3 {
		t.Fatalf("expected 3 log lines, got %d", len(logs))
	}
	if logs[0] != "Installing dependencies..." {
		t.Errorf("logs[0] = %q, want %q", logs[0], "Installing dependencies...")
	}
	if logs[2] != "Build command failed with exit code 1" {
		t.Errorf("logs[2] = %q, want %q", logs[2], "Build command failed with exit code 1")
	}
}

func TestFetchBuildLogs_NoBuildInfo(t *testing.T) {
	t.Parallel()

	event := &platform.AppVersionEvent{Build: nil}
	logs := FetchBuildLogs(context.Background(), nil, nil, "proj-1", event, 50)
	if logs != nil {
		t.Errorf("expected nil, got %v", logs)
	}
}

func TestFetchBuildLogs_NoBuildServiceID(t *testing.T) {
	t.Parallel()

	event := &platform.AppVersionEvent{
		Build: &platform.BuildInfo{ServiceStackID: nil},
	}
	logs := FetchBuildLogs(context.Background(), nil, nil, "proj-1", event, 50)
	if logs != nil {
		t.Errorf("expected nil, got %v", logs)
	}
}

func TestFetchBuildLogs_LogFetchError_ReturnsNil(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithLogAccess(&platform.LogAccess{
			AccessToken: "tok", URL: "https://log.example.com/logs",
		})
	fetcher := platform.NewMockLogFetcher().WithError(fmt.Errorf("log backend down"))

	event := &platform.AppVersionEvent{
		Build: &platform.BuildInfo{
			ServiceStackID: strPtr("build-svc-1"),
		},
	}

	logs := FetchBuildLogs(context.Background(), mock, fetcher, "proj-1", event, 50)
	if logs != nil {
		t.Errorf("expected nil on fetch error, got %v", logs)
	}
}

func TestFetchBuildWarnings_Success(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithLogAccess(&platform.LogAccess{
			AccessToken: "tok", URL: "https://log.example.com/logs",
		})
	fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
		{Message: "WARN: deployFiles paths not found: dist"},
		{Message: "ERROR: bun build produced no output"},
	})

	event := &platform.AppVersionEvent{
		Build: &platform.BuildInfo{
			ServiceStackID: strPtr("build-svc-1"),
		},
	}

	logs := FetchBuildWarnings(context.Background(), mock, fetcher, "proj-1", event, 20)
	if len(logs) != 2 {
		t.Fatalf("expected 2 warning lines, got %d", len(logs))
	}
	if logs[0] != "WARN: deployFiles paths not found: dist" {
		t.Errorf("logs[0] = %q, want %q", logs[0], "WARN: deployFiles paths not found: dist")
	}
}

func TestFetchBuildWarnings_NoBuildInfo_ReturnsNil(t *testing.T) {
	t.Parallel()

	event := &platform.AppVersionEvent{Build: nil}
	logs := FetchBuildWarnings(context.Background(), nil, nil, "proj-1", event, 20)
	if logs != nil {
		t.Errorf("expected nil, got %v", logs)
	}
}

func TestFetchBuildWarnings_NoWarnings_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithLogAccess(&platform.LogAccess{
			AccessToken: "tok", URL: "https://log.example.com/logs",
		})
	fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{})

	event := &platform.AppVersionEvent{
		Build: &platform.BuildInfo{
			ServiceStackID: strPtr("build-svc-1"),
		},
	}

	logs := FetchBuildWarnings(context.Background(), mock, fetcher, "proj-1", event, 20)
	if len(logs) != 0 {
		t.Errorf("expected 0 lines for no warnings, got %d", len(logs))
	}
}

func TestFetchBuildLogs_GetProjectLogError_ReturnsNil(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithError("GetProjectLog", fmt.Errorf("auth expired"))
	fetcher := platform.NewMockLogFetcher()

	event := &platform.AppVersionEvent{
		Build: &platform.BuildInfo{
			ServiceStackID: strPtr("build-svc-1"),
		},
	}

	logs := FetchBuildLogs(context.Background(), mock, fetcher, "proj-1", event, 50)
	if logs != nil {
		t.Errorf("expected nil on GetProjectLog error, got %v", logs)
	}
}
