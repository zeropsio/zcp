package ops

import (
	"context"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// narrowWatchBudgets shrinks the watch tuning for fast failure-path tests.
// non-parallel callers only (mutates package vars).
func narrowWatchBudgets(t *testing.T) {
	t.Helper()
	prevD, prevF, prevI := BuildWatchDiscoverBudget, BuildWatchFollowBudget, BuildWatchPollInterval
	BuildWatchDiscoverBudget, BuildWatchFollowBudget, BuildWatchPollInterval = 30*time.Millisecond, 30*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() {
		BuildWatchDiscoverBudget, BuildWatchFollowBudget, BuildWatchPollInterval = prevD, prevF, prevI
	})
}

// TestWatchIntegrationBuild_Table pins the §6.1 watch outcomes: terminal
// build found (ACTIVE/FAILED), discovery timeout (nothing fired), and the
// pre-push event filter (a build created BEFORE the push belongs to an
// earlier delivery — parse-compare, never lexicographic).
func TestWatchIntegrationBuild_Table(t *testing.T) {
	// non-parallel: narrows the package-level watch budgets.
	narrowWatchBudgets(t)
	pushedAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	t.Run("active build observed", func(t *testing.T) {
		client := platform.NewMock().WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "old", ServiceStackID: "svc-1", Status: "ACTIVE", Created: "2026-06-10T11:00:00Z"},
			{ID: "new", ServiceStackID: "svc-1", Status: "ACTIVE", Created: "2026-06-10T12:00:05.123456Z"},
			{ID: "other", ServiceStackID: "svc-2", Status: "ACTIVE", Created: "2026-06-10T12:00:06Z"},
		})
		res, err := WatchIntegrationBuild(context.Background(), client, "proj", "svc-1", pushedAt, nil)
		if err != nil {
			t.Fatalf("watch: %v", err)
		}
		if !res.Observed || res.Event == nil || res.Event.ID != "new" {
			t.Fatalf("expected the post-push build on svc-1; got %+v", res)
		}
		if res.TimedOut {
			t.Errorf("terminal build must not be timed out")
		}
	})

	t.Run("failed build observed", func(t *testing.T) {
		client := platform.NewMock().WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "b1", ServiceStackID: "svc-1", Status: "FAILED", Created: "2026-06-10T12:01:00Z"},
		})
		res, err := WatchIntegrationBuild(context.Background(), client, "proj", "svc-1", pushedAt, nil)
		if err != nil {
			t.Fatalf("watch: %v", err)
		}
		if !res.Observed || res.Event.Status != "FAILED" {
			t.Fatalf("expected FAILED terminal; got %+v", res)
		}
	})

	t.Run("nothing fires within discovery budget", func(t *testing.T) {
		client := platform.NewMock().WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "pre", ServiceStackID: "svc-1", Status: "ACTIVE", Created: "2026-06-10T11:59:59Z"},
		})
		res, err := WatchIntegrationBuild(context.Background(), client, "proj", "svc-1", pushedAt, nil)
		if err != nil {
			t.Fatalf("watch: %v", err)
		}
		if res.Observed {
			t.Fatalf("pre-push event must not count as this push's build; got %+v", res)
		}
	})
}
