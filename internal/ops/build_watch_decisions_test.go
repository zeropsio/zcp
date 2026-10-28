package ops

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestWatchIntegrationBuild_Decisions(t *testing.T) {
	t.Parallel()
	t.Run("Decision: An integration build observed as BUILD_FAILED ends its watch immediately and reports that observed failure.", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			pushedAt := time.Now()
			client := platform.NewMock().WithAppVersionEvents([]platform.AppVersionEvent{
				{ID: "old", ServiceStackID: "svc-1", Status: "ACTIVE", Created: pushedAt.Add(-time.Second).Format(time.RFC3339Nano)},
				{ID: "ours", ServiceStackID: "svc-1", Status: "BUILD_FAILED", Created: pushedAt.Format(time.RFC3339Nano)},
			})
			progress := 0
			result, err := WatchIntegrationBuild(context.Background(), client, "proj", "svc-1", pushedAt, func(string, float64, float64) { progress++ })
			if err != nil {
				t.Fatal(err)
			}
			if !result.Observed || result.Event == nil || result.Event.ID != "ours" || result.Event.Status != "BUILD_FAILED" || result.TimedOut || progress != 0 || time.Since(pushedAt) != 0 {
				t.Fatalf("ASSERTION: observed BUILD_FAILED must return immediately without timeout or progress; got result=%+v progress=%d elapsed=%s", result, progress, time.Since(pushedAt))
			}
		})
	})
	t.Run("Decision: A push whose build is not yet observed remains landed and unconfirmed when observation ends; it must not become a failed build.", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			pushedAt := time.Now()
			client := platform.NewMock().WithAppVersionEvents([]platform.AppVersionEvent{
				{ID: "old", ServiceStackID: "svc-1", Status: "BUILD_FAILED", Created: pushedAt.Add(-time.Second).Format(time.RFC3339Nano)},
			})
			result, err := WatchIntegrationBuild(context.Background(), client, "proj", "svc-1", pushedAt, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.Observed || result.Event != nil || result.TimedOut {
				t.Fatalf("ASSERTION: discovery expiry must preserve an unobserved build; got %+v", result)
			}
		})
	})
}

func TestWatchIntegrationBuild_Lifecycle_KeepsUnknownPending(t *testing.T) {
	t.Parallel()
	failedAt := "2026-10-28T12:00:00Z"
	for _, tc := range []struct {
		name     string
		status   string
		build    *platform.BuildInfo
		timedOut bool
	}{
		{name: "runtime preparation failed", status: "PREPARING_RUNTIME_FAILED"},
		{name: "deploy failed", status: "DEPLOY_FAILED"},
		{name: "pipeline failure overrides running status", status: "BUILDING", build: &platform.BuildInfo{PipelineFailed: &failedAt}},
		{name: "unknown stays pending", status: "UNKNOWN_FUTURE_STATUS", timedOut: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				pushedAt := time.Now()
				client := platform.NewMock().WithAppVersionEvents([]platform.AppVersionEvent{{ID: "ours", ServiceStackID: "svc-1", Status: tc.status, Build: tc.build, Created: pushedAt.Format(time.RFC3339Nano)}})
				result, err := WatchIntegrationBuild(context.Background(), client, "proj", "svc-1", pushedAt, nil)
				if err != nil {
					t.Fatal(err)
				}
				if !result.Observed || result.Event == nil || result.Event.ID != "ours" || result.Event.Status != tc.status || result.TimedOut != tc.timedOut {
					t.Fatalf("ASSERTION: lifecycle receipt must preserve known terminal and unknown pending policies; got %+v", result)
				}
				if !tc.timedOut && time.Since(pushedAt) != 0 {
					t.Fatalf("ASSERTION: known failure must end observation immediately; elapsed=%s", time.Since(pushedAt))
				}
			})
		})
	}
}
