// Tests for: ops/failed_context.go — LatestFailedAppVersionContext helper.
// Pinned by these tests:
//   - empty / non-failure history returns nil
//   - most-recent failed appVersion is classified via the same path that
//     ops/events.go uses (FailurePhaseFromStatus + ClassifyDeployFailure)
//   - SuggestedReadTool routes diagnostic deep-dive to zerops_logs
package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

func TestLatestFailedAppVersionContext_NoHistoryReturnsNil(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		services    []platform.ServiceStack
		appVersions []platform.AppVersionEvent
	}{
		{
			name: "service-not-listed",
			services: []platform.ServiceStack{
				{ID: "svc-other", Name: "other"},
			},
			appVersions: nil,
		},
		{
			name: "no-app-versions",
			services: []platform.ServiceStack{
				{ID: "svc-1", Name: "api"},
			},
			appVersions: nil,
		},
		{
			name: "only-active-app-versions",
			services: []platform.ServiceStack{
				{ID: "svc-1", Name: "api"},
			},
			appVersions: []platform.AppVersionEvent{
				{ID: "av-1", ServiceStackID: "svc-1", Status: "ACTIVE", Created: "2026-05-05T10:00:00Z"},
			},
		},
		{
			name: "failure-on-other-service",
			services: []platform.ServiceStack{
				{ID: "svc-1", Name: "api"},
				{ID: "svc-2", Name: "worker"},
			},
			appVersions: []platform.AppVersionEvent{
				{ID: "av-1", ServiceStackID: "svc-2", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
			},
		},
		{
			name: "skip-startwithoutcode",
			services: []platform.ServiceStack{
				{ID: "svc-1", Name: "api"},
			},
			appVersions: []platform.AppVersionEvent{
				{ID: "av-1", ServiceStackID: "svc-1", Source: "NONE", Status: "ACTIVE", Created: "2026-05-05T10:00:00Z"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().
				WithServices(tc.services).
				WithAppVersionEvents(tc.appVersions)
			fetcher := platform.NewMockLogFetcher()

			got, err := LatestFailedAppVersionContext(context.Background(), client, fetcher, "p-1", "api")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != nil {
				t.Fatalf("expected nil context, got %+v", got)
			}
		})
	}
}

func TestLatestFailedAppVersionContext_ReturnsClassifiedRecent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		appVersions       []platform.AppVersionEvent
		wantFailureClass  topology.FailureClass
		wantSuggestedTool string
		wantArgFacility   string
	}{
		{
			name: "most-recent-build-failed",
			appVersions: []platform.AppVersionEvent{
				// Most recent first — API returns sorted desc by created.
				{ID: "av-3", ServiceStackID: "svc-1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T12:00:00Z"},
				{ID: "av-2", ServiceStackID: "svc-1", Status: "ACTIVE", Created: "2026-05-05T11:00:00Z"},
				{ID: "av-1", ServiceStackID: "svc-1", Status: platform.BuildStatusDeployFailed, Created: "2026-05-05T10:00:00Z"},
			},
			wantFailureClass:  topology.FailureClassBuild,
			wantSuggestedTool: "zerops_logs",
			wantArgFacility:   "application",
		},
		{
			name: "deploy-failed-most-recent",
			appVersions: []platform.AppVersionEvent{
				{ID: "av-2", ServiceStackID: "svc-1", Status: platform.BuildStatusDeployFailed, Created: "2026-05-05T12:00:00Z"},
				{ID: "av-1", ServiceStackID: "svc-1", Status: "ACTIVE", Created: "2026-05-05T10:00:00Z"},
			},
			wantFailureClass:  topology.FailureClassStart,
			wantSuggestedTool: "zerops_logs",
		},
		{
			name: "preparing-runtime-failed",
			appVersions: []platform.AppVersionEvent{
				{ID: "av-1", ServiceStackID: "svc-1", Status: platform.BuildStatusPreparingRuntimeFail, Created: "2026-05-05T10:00:00Z"},
			},
			wantFailureClass:  topology.FailureClassStart,
			wantSuggestedTool: "zerops_logs",
		},
		{
			name: "skip-failure-on-other-service-pick-this-one",
			appVersions: []platform.AppVersionEvent{
				{ID: "av-other", ServiceStackID: "svc-2", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T13:00:00Z"},
				{ID: "av-1", ServiceStackID: "svc-1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
			},
			wantFailureClass:  topology.FailureClassBuild,
			wantSuggestedTool: "zerops_logs",
			wantArgFacility:   "application",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "api"},
					{ID: "svc-2", Name: "worker"},
				}).
				WithAppVersionEvents(tc.appVersions)
			fetcher := platform.NewMockLogFetcher()

			got, err := LatestFailedAppVersionContext(context.Background(), client, fetcher, "p-1", "api")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil {
				t.Fatalf("expected non-nil context")
			}
			if got.FailureClass != tc.wantFailureClass {
				t.Errorf("FailureClass = %q, want %q", got.FailureClass, tc.wantFailureClass)
			}
			if got.FailureCause == "" {
				t.Errorf("FailureCause is empty; classifier baseline should populate it")
			}
			if got.SuggestedReadTool != tc.wantSuggestedTool {
				t.Errorf("SuggestedReadTool = %q, want %q", got.SuggestedReadTool, tc.wantSuggestedTool)
			}
			if got.SuggestedArgs["serviceHostname"] != "api" {
				t.Errorf("SuggestedArgs[serviceHostname] = %q, want %q", got.SuggestedArgs["serviceHostname"], "api")
			}
			if tc.wantArgFacility != "" && got.SuggestedArgs["facility"] != tc.wantArgFacility {
				t.Errorf("SuggestedArgs[facility] = %q, want %q", got.SuggestedArgs["facility"], tc.wantArgFacility)
			}
			if got.FailedAt.IsZero() {
				t.Errorf("FailedAt is zero; should parse from av.Created")
			}
		})
	}
}

// TestLatestFailedAppVersionContext_PassesThroughClassifyDeployFailure pins
// that the helper feeds the same FailureInput to ClassifyDeployFailure that
// ops/events.go does — so the agent sees one diagnostic vocabulary across
// async-build event timelines and explicit pre-flight gates.
func TestLatestFailedAppVersionContext_PassesThroughClassifyDeployFailure(t *testing.T) {
	t.Parallel()

	av := platform.AppVersionEvent{
		ID:             "av-1",
		ServiceStackID: "svc-1",
		Status:         platform.BuildStatusBuildFailed,
		Created:        "2026-05-05T10:00:00Z",
	}
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
		WithAppVersionEvents([]platform.AppVersionEvent{av})
	fetcher := platform.NewMockLogFetcher()

	got, err := LatestFailedAppVersionContext(context.Background(), client, fetcher, "p-1", "api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatalf("expected non-nil context")
	}

	// Same input fed to ClassifyDeployFailure directly — categories must align.
	cls := ClassifyDeployFailure(FailureInput{
		Phase:  FailurePhaseFromStatus(av.Status),
		Status: av.Status,
	})
	if cls == nil {
		t.Fatalf("ClassifyDeployFailure baseline returned nil for BUILD_FAILED")
	}
	if string(got.FailureClass) != string(cls.Category) {
		t.Errorf("helper FailureClass=%q diverges from ClassifyDeployFailure Category=%q", got.FailureClass, cls.Category)
	}
	if !strings.Contains(got.FailureCause, cls.LikelyCause) && got.FailureCause != cls.LikelyCause {
		t.Errorf("helper FailureCause=%q diverges from ClassifyDeployFailure LikelyCause=%q", got.FailureCause, cls.LikelyCause)
	}
}

// TestLatestFailedAppVersionContext_WaitingToBuildFallback pins R6-P2: a stuck
// WAITING_TO_BUILD appVersion is classified as a build failure ONLY when a
// FAILED build PROCESS is bound to the service (stuck), and stays nil when no
// such process exists (genuinely queued — the queued-vs-stuck guard).
func TestLatestFailedAppVersionContext_WaitingToBuildFallback(t *testing.T) {
	t.Parallel()
	// Source non-empty so the appVersion isn't skipped as a startWithoutCode stamp.
	waiting := []platform.AppVersionEvent{
		{ID: "av-1", ServiceStackID: "svc-1", Source: "GIT", Status: "WAITING_TO_BUILD", Created: "2026-05-05T10:00:00Z"},
	}

	t.Run("failed_build_process_classifies_as_build", func(t *testing.T) {
		t.Parallel()
		client := platform.NewMock().
			WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
			WithAppVersionEvents(waiting).
			WithProcessEvents([]platform.ProcessEvent{
				{ID: "p-1", ActionName: "stack.build", Status: platform.ProcessStatusFailed,
					ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}}},
			})
		got, err := LatestFailedAppVersionContext(context.Background(), client, nil, "p-1", "api")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil || got.FailureClass != topology.FailureClassBuild {
			t.Fatalf("stuck WAITING_TO_BUILD with a FAILED build process should classify as build, got %+v", got)
		}
	})

	t.Run("no_failed_process_stays_nil", func(t *testing.T) {
		t.Parallel()
		client := platform.NewMock().
			WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api"}}).
			WithAppVersionEvents(waiting).
			WithProcessEvents([]platform.ProcessEvent{
				{ID: "p-1", ActionName: "stack.build", Status: platform.ProcessStatusRunning,
					ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}}},
			})
		got, err := LatestFailedAppVersionContext(context.Background(), client, nil, "p-1", "api")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("genuinely-queued WAITING_TO_BUILD (no FAILED process) must stay nil, got %+v", got)
		}
	})
}
