// Tests for: ops/logs.go — zerops_logs' facility: the app's log (default), the
// web server's, and the service's newest build's, a running build included.
package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// buildLogFixture is a service whose newest build (av-new, BUILDING) logs
// under its tag beside an older build's lines and the running app's.
func buildLogFixture(searchIndexed bool) (*platform.Mock, *platform.MockLogFetcher) {
	buildStack := "build-stack-1"
	start := recentTS(-300)
	running := platform.AppVersionEvent{ID: "av-new", ServiceStackID: "svc-1", Source: "CLI", Status: statusBuilding, Sequence: 3}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: platform.ServiceStatusActive}}).
		WithLogAccess(&platform.LogAccess{URL: "https://logs.example.com"}).
		WithServiceAppVersions("svc-1", []platform.AppVersionEvent{
			running,
			{ID: "av-old", ServiceStackID: "svc-1", Source: "CLI", Status: statusActive, Sequence: 2},
			{ID: "av-none", ServiceStackID: "svc-1", Source: "NONE", Status: "BACKUP", Sequence: 1},
		})
	if searchIndexed {
		indexed := running
		indexed.Build = &platform.BuildInfo{ServiceStackID: &buildStack, PipelineStart: &start}
		mock = mock.WithAppVersionEvents([]platform.AppVersionEvent{indexed})
	}
	fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
		{Timestamp: recentTS(-3000), Severity: "Informational", Facility: "local0", Tag: "zbuilder@av-old", Message: "old build: npm ci"},
		{Timestamp: recentTS(-240), Severity: "Informational", Facility: "local0", Tag: "zbuilder@av-new", Message: "new build: npm ci"},
		{Timestamp: recentTS(-200), Severity: "Informational", Facility: "local0", Tag: "app", Message: "app: request served"},
		{Timestamp: recentTS(-120), Severity: "Informational", Facility: "local0", Tag: "zbuilder@av-new", Message: "new build: vite build"},
		{Timestamp: recentTS(-60), Severity: "Informational", Facility: "local1", Tag: "nginx", Message: "nginx: GET / 200"},
	})
	return mock, fetcher
}

// TestFetchLogs_Build_ReadsTheNewestBuildWhileItRuns: facility=build reads
// the log of the service's newest build — here still running — by its tag,
// none of an older build's lines or the app's, and says which build it read.
func TestFetchLogs_Build_ReadsTheNewestBuildWhileItRuns(t *testing.T) {
	t.Parallel()
	for _, indexed := range []bool{true, false} {
		name := "indexed by the activity search"
		if !indexed {
			name = "not yet indexed by the activity search"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mock, fetcher := buildLogFixture(indexed)
			result, err := FetchLogs(context.Background(), mock, fetcher, "proj-1", "api", LogFacilityBuild, "", "", 100, "")
			if err != nil {
				t.Fatalf("FetchLogs: %v", err)
			}
			got := make([]string, 0, len(result.Entries))
			for _, e := range result.Entries {
				got = append(got, e.Message)
			}
			if strings.Join(got, "|") != "new build: npm ci|new build: vite build" {
				t.Errorf("entries = %q, want the newest build's lines in order", got)
			}
			if result.Build == nil || result.Build.AppVersionID != "av-new" || result.Build.Status != statusBuilding || !result.Build.Running {
				t.Errorf("build = %+v, want av-new, BUILDING, running", result.Build)
			}
		})
	}
}

// TestFetchLogs_Build_SearchAndLimit: a build read honours search and keeps
// the newest lines up to limit, saying when there are more.
func TestFetchLogs_Build_SearchAndLimit(t *testing.T) {
	t.Parallel()
	mock, fetcher := buildLogFixture(true)
	result, err := FetchLogs(context.Background(), mock, fetcher, "proj-1", "api", LogFacilityBuild, "", "", 1, "")
	if err != nil {
		t.Fatalf("FetchLogs: %v", err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Message != "new build: vite build" || !result.HasMore {
		t.Errorf("limit 1 = %+v, hasMore %v; want the newest line and more", result.Entries, result.HasMore)
	}
	result, err = FetchLogs(context.Background(), mock, fetcher, "proj-1", "api", LogFacilityBuild, "", "", 100, "npm")
	if err != nil {
		t.Fatalf("FetchLogs: %v", err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Message != "new build: npm ci" {
		t.Errorf("search npm = %+v, want the build's npm line only", result.Entries)
	}
}

// TestFetchLogs_Build_NoBuildYet_SaysSo: a service never built has no build
// log, and the empty result says so instead of leaving the agent to guess.
func TestFetchLogs_Build_NoBuildYet_SaysSo(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: platform.ServiceStatusReadyToDeploy}}).
		WithLogAccess(&platform.LogAccess{URL: "https://logs.example.com"}).
		WithServiceAppVersions("svc-1", []platform.AppVersionEvent{{ID: "av-none", ServiceStackID: "svc-1", Source: "NONE", Status: statusActive, Sequence: 1}})
	result, err := FetchLogs(context.Background(), mock, platform.NewMockLogFetcher(), "proj-1", "api", LogFacilityBuild, "", "", 100, "")
	if err != nil {
		t.Fatalf("FetchLogs: %v", err)
	}
	if len(result.Entries) != 0 || result.Build != nil || !strings.Contains(result.EmptyReason, "no build") {
		t.Errorf("result = %+v, want no entries and a reason naming no build", result)
	}
}

// TestFetchLogs_Webserver_ReadsTheWebServersLog: facility=webserver reads the
// web server's log, not the app's.
func TestFetchLogs_Webserver_ReadsTheWebServersLog(t *testing.T) {
	t.Parallel()
	mock, fetcher := buildLogFixture(true)
	result, err := FetchLogs(context.Background(), mock, fetcher, "proj-1", "api", LogFacilityWebserver, "", "", 100, "")
	if err != nil {
		t.Fatalf("FetchLogs: %v", err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Message != "nginx: GET / 200" {
		t.Errorf("entries = %+v, want the web server's line only", result.Entries)
	}
}

// TestFetchLogs_UnknownFacility_NamesTheOnesItReads: a facility zerops_logs
// does not read is refused, naming the ones it does.
func TestFetchLogs_UnknownFacility_NamesTheOnesItReads(t *testing.T) {
	t.Parallel()
	mock, fetcher := buildLogFixture(true)
	_, err := FetchLogs(context.Background(), mock, fetcher, "proj-1", "api", "daemon", "", "", 100, "")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter {
		t.Fatalf("err = %v, want INVALID_PARAMETER", err)
	}
	for _, want := range []string{LogFacilityApplication, LogFacilityWebserver, LogFacilityBuild} {
		if !strings.Contains(pe.Message+" "+pe.Suggestion, want) {
			t.Errorf("refusal %q / %q does not name %q", pe.Message, pe.Suggestion, want)
		}
	}
}
