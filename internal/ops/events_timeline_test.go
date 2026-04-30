// Tests for: plans/analysis/ops.md § events
package ops

import (
	"context"
	"fmt"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestEvents_MergedTimeline(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api"},
		{ID: "svc-2", Name: "db"},
	}

	processes := []platform.ProcessEvent{
		{
			ID:            "p1",
			ActionName:    "stack.start",
			Status:        statusFinished,
			Created:       "2024-01-01T00:03:00Z",
			ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}},
		},
		{
			ID:            "p2",
			ActionName:    "stack.restart",
			Status:        statusFinished,
			Created:       "2024-01-01T00:01:00Z",
			ServiceStacks: []platform.ServiceStackRef{{ID: "svc-2", Name: "db"}},
		},
		{
			ID:            "p3",
			ActionName:    "stack.stop",
			Status:        "PENDING",
			Created:       "2024-01-01T00:05:00Z",
			ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}},
		},
	}

	appVersions := []platform.AppVersionEvent{
		{
			ID:             "av1",
			ServiceStackID: "svc-1",
			Status:         "active",
			Created:        "2024-01-01T00:02:00Z",
		},
		{
			ID:             "av2",
			ServiceStackID: "svc-1",
			Status:         "active",
			Created:        "2024-01-01T00:04:00Z",
			Build:          &platform.BuildInfo{PipelineStart: strPtr("2024-01-01T00:04:01Z")},
		},
	}

	mock := platform.NewMock().
		WithServices(services).
		WithProcessEvents(processes).
		WithAppVersionEvents(appVersions)

	result, err := Events(context.Background(), mock, nil, "proj-1", "", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(result.Events))
	}

	// Sorted descending: 00:05, 00:04, 00:03, 00:02, 00:01.
	if result.Events[0].Timestamp != "2024-01-01T00:05:00Z" {
		t.Errorf("expected first event at 00:05, got %s", result.Events[0].Timestamp)
	}
	if result.Events[4].Timestamp != "2024-01-01T00:01:00Z" {
		t.Errorf("expected last event at 00:01, got %s", result.Events[4].Timestamp)
	}

	// Check that build event (av2 with PipelineStart) is typed as "build".
	var foundBuild bool
	for _, e := range result.Events {
		if e.Timestamp == "2024-01-01T00:04:00Z" && e.Type == "build" {
			foundBuild = true
		}
	}
	if !foundBuild {
		t.Error("expected av2 to be detected as 'build' event")
	}

	// Check that deploy event (av1 without PipelineStart) is typed as "deploy".
	var foundDeploy bool
	for _, e := range result.Events {
		if e.Timestamp == "2024-01-01T00:02:00Z" && e.Type == "deploy" {
			foundDeploy = true
		}
	}
	if !foundDeploy {
		t.Error("expected av1 to be detected as 'deploy' event")
	}
}

func TestEvents_FilterByService(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api"},
		{ID: "svc-2", Name: "db"},
	}

	processes := []platform.ProcessEvent{
		{
			ID:            "p1",
			ActionName:    "stack.start",
			Status:        statusFinished,
			Created:       "2024-01-01T00:01:00Z",
			ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}},
		},
		{
			ID:            "p2",
			ActionName:    "stack.restart",
			Status:        statusFinished,
			Created:       "2024-01-01T00:02:00Z",
			ServiceStacks: []platform.ServiceStackRef{{ID: "svc-2", Name: "db"}},
		},
	}

	mock := platform.NewMock().
		WithServices(services).
		WithProcessEvents(processes).
		WithAppVersionEvents(nil)

	result, err := Events(context.Background(), mock, nil, "proj-1", "api", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Events) != 1 {
		t.Fatalf("expected 1 event for api, got %d", len(result.Events))
	}
	if result.Events[0].Service != "api" {
		t.Errorf("expected service=api, got %s", result.Events[0].Service)
	}
}

func TestEvents_LimitApplied(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{{ID: "svc-1", Name: "api"}}

	processes := make([]platform.ProcessEvent, 10)
	for i := range processes {
		processes[i] = platform.ProcessEvent{
			ID:            fmt.Sprintf("p%d", i),
			ActionName:    "stack.start",
			Status:        statusFinished,
			Created:       fmt.Sprintf("2024-01-01T00:00:%02dZ", i),
			ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1"}},
		}
	}

	mock := platform.NewMock().
		WithServices(services).
		WithProcessEvents(processes).
		WithAppVersionEvents(nil)

	result, err := Events(context.Background(), mock, nil, "proj-1", "", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Events) != 3 {
		t.Errorf("expected 3 events (limit), got %d", len(result.Events))
	}
}

func TestEvents_EmptyResult(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices(nil).
		WithProcessEvents(nil).
		WithAppVersionEvents(nil)

	result, err := Events(context.Background(), mock, nil, "proj-1", "", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(result.Events))
	}
	if result.Summary.Total != 0 {
		t.Errorf("expected total=0, got %d", result.Summary.Total)
	}
}

func TestEvents_DefaultLimit(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices(nil).
		WithProcessEvents(nil).
		WithAppVersionEvents(nil)

	result, err := Events(context.Background(), mock, nil, "proj-1", "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default limit should be applied (50), but with no events it doesn't matter.
	if result.ProjectID != "proj-1" {
		t.Errorf("expected projectId=proj-1, got %s", result.ProjectID)
	}
}

func TestEvents_FilterByService_SummaryCounts(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api"},
		{ID: "svc-2", Name: "db"},
	}

	processes := []platform.ProcessEvent{
		{
			ID:            "p1",
			ActionName:    "stack.start",
			Status:        statusFinished,
			Created:       "2024-01-01T00:01:00Z",
			ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}},
		},
		{
			ID:            "p2",
			ActionName:    "stack.restart",
			Status:        statusFinished,
			Created:       "2024-01-01T00:02:00Z",
			ServiceStacks: []platform.ServiceStackRef{{ID: "svc-2", Name: "db"}},
		},
	}

	appVersions := []platform.AppVersionEvent{
		{
			ID:             "av1",
			ServiceStackID: "svc-1",
			Status:         "active",
			Created:        "2024-01-01T00:03:00Z",
		},
		{
			ID:             "av2",
			ServiceStackID: "svc-2",
			Status:         "active",
			Created:        "2024-01-01T00:04:00Z",
		},
	}

	mock := platform.NewMock().
		WithServices(services).
		WithProcessEvents(processes).
		WithAppVersionEvents(appVersions)

	tests := []struct {
		name          string
		service       string
		wantTotal     int
		wantProcesses int
		wantDeploys   int
	}{
		{
			name:          "filter by api returns only api counts",
			service:       "api",
			wantTotal:     2,
			wantProcesses: 1,
			wantDeploys:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Events(context.Background(), mock, nil, "proj-1", tt.service, 50)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Summary.Total != tt.wantTotal {
				t.Errorf("Summary.Total = %d, want %d", result.Summary.Total, tt.wantTotal)
			}
			if result.Summary.Processes != tt.wantProcesses {
				t.Errorf("Summary.Processes = %d, want %d", result.Summary.Processes, tt.wantProcesses)
			}
			if result.Summary.Deploys != tt.wantDeploys {
				t.Errorf("Summary.Deploys = %d, want %d", result.Summary.Deploys, tt.wantDeploys)
			}
		})
	}
}

// TestEvents_SkipsStartWithoutCode pins the F1 fix (round-3 audit):
// appVersions created by bootstrap with `startWithoutCode: true` (Source="NONE",
// no Build info) MUST be filtered out at the timeline-construction site so
// downstream consumers (recordDeployBuildStatusGate) don't treat the pre-existing
// ACTIVE bootstrap stamp as a successful deploy. progress.go::PollBuild already
// filters at the poll site; this test pins the equivalent filter on the
// generic events path.
func TestEvents_SkipsStartWithoutCode(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{{ID: "svc-1", Name: "api"}}

	appVersions := []platform.AppVersionEvent{
		{
			ID:             "av-startwithoutcode",
			ServiceStackID: "svc-1",
			Status:         "ACTIVE",
			Created:        "2024-01-01T00:01:00Z",
			Source:         "NONE", // bootstrap startWithoutCode marker
			Build:          nil,
		},
		{
			ID:             "av-real-build",
			ServiceStackID: "svc-1",
			Status:         "ACTIVE",
			Created:        "2024-01-01T00:02:00Z",
			Source:         "git", // any non-"NONE" with non-nil Build
			Build:          &platform.BuildInfo{PipelineStart: strPtr("2024-01-01T00:02:01Z")},
		},
	}

	mock := platform.NewMock().
		WithServices(services).
		WithProcessEvents(nil).
		WithAppVersionEvents(appVersions)

	result, err := Events(context.Background(), mock, nil, "proj-1", "api", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Events) != 1 {
		t.Fatalf("expected 1 event (startWithoutCode filtered), got %d:\n%+v",
			len(result.Events), result.Events)
	}
	if result.Events[0].Timestamp != "2024-01-01T00:02:00Z" {
		t.Errorf("expected the real-build event to survive, got timestamp %s",
			result.Events[0].Timestamp)
	}
}

func TestEvents_ParallelFetchError(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithError("SearchProcesses", &platform.PlatformError{
			Code:    platform.ErrAPIError,
			Message: "search failed",
		}).
		WithServices(nil).
		WithAppVersionEvents(nil)

	_, err := Events(context.Background(), mock, nil, "proj-1", "", 50)
	if err == nil {
		t.Fatal("expected error from SearchProcesses")
	}
}
