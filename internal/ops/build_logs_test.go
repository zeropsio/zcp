// Tests for: ops/build_logs.go — FetchBuildLogs best-effort log retrieval.
package ops

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// recordingLogFetcher wraps MockLogFetcher and captures the LogFetchParams
// passed on each call, so tests can assert Facility/Tags/etc. at the contract
// boundary even when the entry filter would produce identical output.
type recordingLogFetcher struct {
	*platform.MockLogFetcher
	calls []platform.LogFetchParams
}

func newRecordingLogFetcher(entries []platform.LogEntry) *recordingLogFetcher {
	return &recordingLogFetcher{MockLogFetcher: platform.NewMockLogFetcher().WithEntries(entries)}
}

func (r *recordingLogFetcher) FetchLogs(ctx context.Context, access *platform.LogAccess, params platform.LogFetchParams) ([]platform.LogEntry, error) {
	r.calls = append(r.calls, params)
	return r.MockLogFetcher.FetchLogs(ctx, access, params)
}

func (r *recordingLogFetcher) lastCall(t *testing.T) platform.LogFetchParams {
	t.Helper()
	if len(r.calls) == 0 {
		t.Fatalf("recordingLogFetcher: no FetchLogs calls recorded")
	}
	return r.calls[len(r.calls)-1]
}

func TestFetchBuildLogs_Success(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithLogAccess(&platform.LogAccess{
			URL: "https://log.example.com/logs",
		})
	fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
		{Facility: "local0", Tag: "zbuilder@av-1", Message: "Installing dependencies..."},
		{Facility: "local0", Tag: "zbuilder@av-1", Message: "npm error code ERESOLVE"},
		{Facility: "local0", Tag: "zbuilder@av-1", Message: "Build command failed with exit code 1"},
	})

	event := &platform.AppVersionEvent{
		ID: "av-1",
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

// The platform logs a build container's out-of-memory alert on the build's
// service stack without the build's tag (seen live 2026-10-16 on a Next.js
// build: "granted 8.00GB, max: 8.00GB" and "OOM Alert: Process 'next-build
// (v16' terminated"), so a tag-scoped tail ends mid-build with no error line.
// The build's log carries those error lines in time order, once each, and
// nothing from another build or the builder's own chatter.
func TestAFailedBuildsLogCarriesThePlatformsOutOfMemoryAlertForItsBuildContainer(t *testing.T) {
	t.Parallel()

	const (
		creating = "Creating an optimized production build ..."
		npmError = "npm error something earlier"
		granted  = "granted 8.00GB, max: 8.00GB, minFree: 1.00GB"
		oom      = "OOM Alert: Process 'next-build (v16' terminated - memory limit exceeded. Consider increasing the memory limits (min or minFree) in the service configuration."
		restart  = "┃   ZEROPS BUILD CONTAINER STARTED   ┃"
	)
	mock := platform.NewMock().WithLogAccess(&platform.LogAccess{URL: "https://log.example.com/logs"})
	fetcher := newRecordingLogFetcher([]platform.LogEntry{
		{Timestamp: "2026-10-16T03:40:00Z", Facility: "local0", Severity: "Error", Message: "OOM Alert: an older build's alert"},
		{Timestamp: "2026-10-16T03:53:00Z", Facility: "local0", Severity: "Error", Tag: "zbuilder@av-1", Message: npmError},
		{Timestamp: "2026-10-16T03:53:17Z", Facility: "local0", Severity: "Informational", Tag: "zbuilder@av-1", Message: creating},
		{Timestamp: "2026-10-16T03:59:55.737026329Z", Facility: "local0", Severity: "Error", Message: granted},
		{Timestamp: "2026-10-16T03:59:55.737026329Z", Facility: "local0", Severity: "Error", Message: oom},
		{Timestamp: "2026-10-16T03:59:55.7804Z", Facility: "local0", Severity: "Informational", Tag: "zenv", Message: "Error: exit status 1"},
		{Timestamp: "2026-10-16T03:59:59.1957Z", Facility: "local0", Severity: "Informational", Tag: "zbuilder@av-1", Message: restart},
	})
	event := &platform.AppVersionEvent{
		ID: "av-1",
		Build: &platform.BuildInfo{
			ServiceStackID: strPtr("build-svc-1"),
			PipelineStart:  strPtr("2026-10-16T03:51:33.711080515Z"),
		},
	}

	logs := FetchBuildLogs(context.Background(), mock, fetcher, "proj-1", event, 50)

	want := []string{npmError, creating, granted, oom, restart}
	if fmt.Sprint(logs) != fmt.Sprint(want) {
		t.Fatalf("build log =\n%q\nwant\n%q", logs, want)
	}
	alerts := fetcher.lastCall(t)
	if alerts.ServiceID != "build-svc-1" || alerts.Severity != "error" || len(alerts.Tags) != 0 {
		t.Errorf("alert query = %+v, want the build service stack's error lines whatever their tag", alerts)
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
			URL: "https://log.example.com/logs",
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
			URL: "https://log.example.com/logs",
		})
	fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
		{Severity: "Warning", Facility: "local0", Tag: "zbuilder@av-1", Message: "WARN: deployFiles paths not found: dist"},
		{Severity: "Error", Facility: "local0", Tag: "zbuilder@av-1", Message: "ERROR: bun build produced no output"},
	})

	event := &platform.AppVersionEvent{
		ID: "av-1",
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
			URL: "https://log.example.com/logs",
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

// --- Phase 3 RED tests --------------------------------------------------

// TestFetchBuildWarnings_ScopedByTagIdentity — pins I-LOG-2: warnings are
// scoped by tag identity (zbuilder@<appVersionId>), not by time. A previous
// build's warning tagged with a different appVersionId must not leak into
// the current build's result.
func TestFetchBuildWarnings_ScopedByTagIdentity(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithLogAccess(&platform.LogAccess{URL: "https://logs"})

	fetcher := newRecordingLogFetcher([]platform.LogEntry{
		// Stale entry from a previous build — same build stack, different appVersionId.
		{Timestamp: "2026-04-22T06:00:00Z", Severity: "Warning", Facility: "local0", Tag: "zbuilder@OLD", Message: "stale warning"},
		// Current build's warnings.
		{Timestamp: "2026-04-22T06:04:35Z", Severity: "Warning", Facility: "local0", Tag: "zbuilder@NEW", Message: "fresh warning 1"},
		{Timestamp: "2026-04-22T06:04:36Z", Severity: "Warning", Facility: "local0", Tag: "zbuilder@NEW", Message: "fresh warning 2"},
	})

	event := &platform.AppVersionEvent{
		ID: "NEW",
		Build: &platform.BuildInfo{
			ServiceStackID: strPtr("build-svc-1"),
			PipelineStart:  strPtr("2026-04-22T06:04:29.440767629Z"),
		},
	}

	logs := FetchBuildWarnings(context.Background(), mock, fetcher, "proj-1", event, 100)

	if len(logs) != 2 {
		t.Fatalf("expected 2 fresh warnings, got %d: %v", len(logs), logs)
	}
	if logs[0] != "fresh warning 1" || logs[1] != "fresh warning 2" {
		t.Errorf("warnings = %v, want [\"fresh warning 1\" \"fresh warning 2\"]", logs)
	}
	for _, l := range logs {
		if l == "stale warning" {
			t.Errorf("stale warning leaked into fresh result — tag-identity scoping regressed")
		}
	}
}

// TestFetchBuildWarnings_SetsApplicationFacility — pins I-LOG-3: the
// Facility must be "application" so daemon noise is excluded.
func TestFetchBuildWarnings_SetsApplicationFacility(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithLogAccess(&platform.LogAccess{URL: "https://logs"})
	fetcher := newRecordingLogFetcher(nil)
	event := &platform.AppVersionEvent{
		ID:    "appver-1",
		Build: &platform.BuildInfo{ServiceStackID: strPtr("build-svc-1")},
	}

	_ = FetchBuildWarnings(context.Background(), mock, fetcher, "proj-1", event, 100)

	got := fetcher.lastCall(t)
	if got.Facility != "application" {
		t.Errorf("Facility = %q, want %q", got.Facility, "application")
	}
	if got.Severity != "warning" {
		t.Errorf("Severity = %q, want %q", got.Severity, "warning")
	}
	wantTag := "zbuilder@appver-1"
	if len(got.Tags) != 1 || got.Tags[0] != wantTag {
		t.Errorf("Tags = %v, want [%q]", got.Tags, wantTag)
	}
}

// TestFetchBuildWarnings_NoPipelineStartDependency — pins that tag identity
// is the load-bearing filter; Since is not set because the tag filter alone
// is authoritative. If PipelineStart is nil we still return only the current
// build's entries.
func TestFetchBuildWarnings_NoPipelineStartDependency(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithLogAccess(&platform.LogAccess{URL: "https://logs"})
	fetcher := newRecordingLogFetcher([]platform.LogEntry{
		{Timestamp: "2026-04-22T06:00:00Z", Severity: "Warning", Facility: "local0", Tag: "zbuilder@NEW", Message: "w1"},
	})
	event := &platform.AppVersionEvent{
		ID:    "NEW",
		Build: &platform.BuildInfo{ServiceStackID: strPtr("build-svc-1")}, // no PipelineStart
	}

	logs := FetchBuildWarnings(context.Background(), mock, fetcher, "proj-1", event, 100)
	if len(logs) != 1 || logs[0] != "w1" {
		t.Errorf("logs = %v, want [w1]", logs)
	}

	got := fetcher.lastCall(t)
	if !got.Since.IsZero() {
		t.Errorf("Since = %v, want zero — tag identity must not depend on PipelineStart", got.Since)
	}
}

// TestFetchBuildLogs_ScopedByTagIdentity — same invariant applies to
// FetchBuildLogs (called on build failure) — stale entries must not leak.
func TestFetchBuildLogs_ScopedByTagIdentity(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithLogAccess(&platform.LogAccess{URL: "https://logs"})
	fetcher := newRecordingLogFetcher([]platform.LogEntry{
		{Timestamp: "2026-04-22T06:00:00Z", Severity: "Error", Facility: "local0", Tag: "zbuilder@OLD", Message: "previous build error"},
		{Timestamp: "2026-04-22T06:04:50Z", Severity: "Error", Facility: "local0", Tag: "zbuilder@NEW", Message: "this build error"},
	})
	event := &platform.AppVersionEvent{
		ID:    "NEW",
		Build: &platform.BuildInfo{ServiceStackID: strPtr("build-svc-1")},
	}

	logs := FetchBuildLogs(context.Background(), mock, fetcher, "proj-1", event, 50)
	if len(logs) != 1 || logs[0] != "this build error" {
		t.Errorf("FetchBuildLogs = %v, want [\"this build error\"]", logs)
	}

	got := fetcher.lastCall(t)
	if got.Facility != "application" {
		t.Errorf("Facility = %q, want application", got.Facility)
	}
	wantTag := "zbuilder@NEW"
	if len(got.Tags) != 1 || got.Tags[0] != wantTag {
		t.Errorf("Tags = %v, want [%q]", got.Tags, wantTag)
	}
}

// TestFetchRuntimeLogs_AnchoredToContainerCreationStart — pins I-LOG-1 applied
// to the runtime path: logs are scoped to THIS container's lifetime via a Since
// anchor, not the entire persistent service stack history.
func TestFetchRuntimeLogs_AnchoredToContainerCreationStart(t *testing.T) {
	t.Parallel()

	creation := time.Date(2026, 4, 22, 6, 4, 30, 0, time.UTC)

	mock := platform.NewMock().WithLogAccess(&platform.LogAccess{URL: "https://logs"})
	fetcher := newRecordingLogFetcher([]platform.LogEntry{
		{Timestamp: "2026-04-22T06:00:00Z", Severity: "Error", Facility: "local0", Message: "previous container crash"},
		{Timestamp: "2026-04-22T06:04:35Z", Severity: "Error", Facility: "local0", Message: "this container crash"},
	})

	logs := FetchRuntimeLogs(context.Background(), mock, fetcher, "proj-1", "runtime-svc-1", creation, 50)
	if len(logs) != 1 || logs[0] != "this container crash" {
		t.Errorf("runtime logs = %v, want [\"this container crash\"]", logs)
	}

	got := fetcher.lastCall(t)
	if got.Facility != "application" {
		t.Errorf("Facility = %q, want application", got.Facility)
	}
	if !got.Since.Equal(creation) {
		t.Errorf("Since = %v, want %v", got.Since, creation)
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

// stackScopedLogFetcher answers like the live log backend: a query scoped to
// a service stack returns only that stack's lines, an unscoped one the whole
// project's. Tags and facility then filter as the mock does.
type stackScopedLogFetcher struct {
	byStack map[string][]platform.LogEntry
}

func (f stackScopedLogFetcher) FetchLogs(ctx context.Context, access *platform.LogAccess, params platform.LogFetchParams) ([]platform.LogEntry, error) {
	var entries []platform.LogEntry
	for stack, lines := range f.byStack {
		if params.ServiceID == "" || params.ServiceID == stack {
			entries = append(entries, lines...)
		}
	}
	return platform.NewMockLogFetcher().WithEntries(entries).FetchLogs(ctx, access, params)
}

// The runtime prepare phase runs on its own service stack after the build
// and logs under the build's tag (live: appVersion KP0LkKL6QvensaDAsu0uEw,
// stacks buildmemosdev… and preparememosdev…), so the failing prepare
// command's output is part of the build's log.
func TestFetchBuildLogs_AFailedPrepareReturnsThePrepareOutputLoggedOnItsOwnStack(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithLogAccess(&platform.LogAccess{URL: "https://logs"})
	fetcher := stackScopedLogFetcher{byStack: map[string][]platform.LogEntry{
		"build-stack": {
			{Timestamp: "2026-10-15T19:09:36Z", Facility: "local0", Tag: "zbuilder@av-1", Message: "✅ BUILD ARTEFACTS READY TO DEPLOY"},
		},
		"prepare-stack": {
			{Timestamp: "2026-10-15T19:12:04Z", Facility: "local0", Tag: "zbuilder@av-1", Message: "npm error code EEXIST"},
			{Timestamp: "2026-10-15T19:12:04Z", Facility: "local0", Tag: "zbuilder@av-1", Message: "━━━━  ❌ sudo npm install -g pnpm@11  ━━━━"},
		},
		"runtime-stack": {
			{Timestamp: "2026-10-15T19:12:34Z", Facility: "local0", Tag: "zerops@setup", Message: "Successfully accepted remote IP address"},
		},
	}}
	event := &platform.AppVersionEvent{
		ID:    "av-1",
		Build: &platform.BuildInfo{ServiceStackID: strPtr("build-stack")},
	}

	logs := FetchBuildLogs(context.Background(), mock, fetcher, "proj-1", event, 50)

	want := []string{"✅ BUILD ARTEFACTS READY TO DEPLOY", "npm error code EEXIST", "━━━━  ❌ sudo npm install -g pnpm@11  ━━━━"}
	if fmt.Sprint(logs) != fmt.Sprint(want) {
		t.Errorf("build logs = %q, want %q", logs, want)
	}
}
