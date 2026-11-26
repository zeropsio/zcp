package ops

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

type unavailableAlertFetcher struct{}

func (unavailableAlertFetcher) FetchLogs(_ context.Context, _ *platform.LogAccess, params platform.LogFetchParams) ([]platform.LogEntry, error) {
	if params.ServiceID != "" {
		return nil, errors.New("alerts unavailable")
	}
	return nil, nil
}

// witness: internal/ops/build_logs.go | if event.ID != versionID { → if event.ID == versionID {
func TestVersionBuildLog_VBL1_ReadsOnlyTheNamedVersion(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, stage, version string
		want                 []string
		wantError            bool
	}{
		{"private older version", "stage", "old", []string{"old failure"}, false},
		{"public older version", "stage.example", "old", []string{"old failure"}, false},
		{"newer version", "stage", "new", []string{"new failure"}, false},
		{"missing version", "stage", "missing", nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc", Name: "stage"}}).
				WithPublicHTTPRoutings(platform.PublicHTTPRouting{Domains: []platform.PublicHTTPDomain{{Name: "stage.example"}}, Locations: []platform.PublicHTTPLocation{{ServiceID: "svc"}}}).
				WithServiceAppVersions("svc", []platform.AppVersionEvent{{ID: "new"}, {ID: "old"}}).
				WithLogAccess(&platform.LogAccess{URL: "https://logs.example"})
			fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
				{Facility: "local0", Tag: "zbuilder@new", Message: "new failure"},
				{Facility: "local0", Tag: "zbuilder@old", Message: "old failure"},
			})
			got, err := FetchVersionBuildLogs(context.Background(), client, fetcher, "p1", tt.stage, tt.version, 100)
			if (err != nil) != tt.wantError || !slices.Equal(got, tt.want) {
				t.Errorf("lines = %q, error = %v; want %q, error %v", got, err, tt.want, tt.wantError)
			}
		})
	}
}

// witness: internal/ops/build_logs.go | if len(entries) == 0 && searchErr != nil { → if false && searchErr != nil {
func TestVersionBuildLog_VBL2_SeparatesUnavailableReadsFromEmptyLogs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, failMethod     string
		logError, alertError bool
		wantError            bool
	}{
		{"empty", "", false, false, false},
		{"version list failed", "ListServiceAppVersions", false, false, true},
		{"build event unreadable", "SearchAppVersions", false, false, true},
		{"log access failed", "GetProjectLog", false, false, true},
		{"log backend failed", "", true, false, true},
		{"build alerts failed", "", false, true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			event := platform.AppVersionEvent{ID: "v1"}
			if tt.alertError {
				stack, started := "build-svc", "2026-11-26T20:00:00Z"
				event.Build = &platform.BuildInfo{ServiceStackID: &stack, PipelineStart: &started}
			}
			client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc", Name: "stage"}}).
				WithServiceAppVersions("svc", []platform.AppVersionEvent{event}).
				WithLogAccess(&platform.LogAccess{URL: "https://logs.example"})
			if tt.failMethod != "" {
				client.WithError(tt.failMethod, errors.New("unavailable"))
			}
			fetcher := platform.NewMockLogFetcher()
			if tt.logError {
				fetcher.WithError(errors.New("unavailable"))
			}
			var source platform.LogFetcher = fetcher
			if tt.alertError {
				source = unavailableAlertFetcher{}
			}
			got, err := FetchVersionBuildLogs(context.Background(), client, source, "p1", "stage", "v1", 100)
			if len(got) != 0 || (err != nil) != tt.wantError {
				t.Errorf("lines = %q, error = %v; want error %v", got, err, tt.wantError)
			}
		})
	}
}

// witness: internal/ops/build_logs.go | if !strings.EqualFold(strings.TrimSuffix(domain.Name, "."), strings.TrimSuffix(stage, ".")) { → if strings.EqualFold(strings.TrimSuffix(domain.Name, "."), strings.TrimSuffix(stage, ".")) {
func TestVersionBuildLog_VBL3_UsesOnlyTheNamedPublicStageRouting(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, stage string
		want        []string
		wantError   bool
	}{
		{"matched host", "stage.example", []string{"stage-svc"}, false},
		{"absent host", "absent.example", nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().WithPublicHTTPRoutings(
				platform.PublicHTTPRouting{Domains: []platform.PublicHTTPDomain{{Name: "other.example"}}, Locations: []platform.PublicHTTPLocation{{ServiceID: "other-svc"}}},
				platform.PublicHTTPRouting{Domains: []platform.PublicHTTPDomain{{Name: "stage.example"}}, Locations: []platform.PublicHTTPLocation{{ServiceID: "stage-svc"}}},
			)
			got, err := stageServiceIDs(context.Background(), client, "p1", tt.stage)
			if !slices.Equal(got, tt.want) || (err != nil) != tt.wantError {
				t.Errorf("services = %q, error = %v; want %q, error %v", got, err, tt.want, tt.wantError)
			}
			if err != nil && !strings.Contains(err.Error(), "stage") {
				t.Errorf("error does not name the stage: %v", err)
			}
		})
	}
}
