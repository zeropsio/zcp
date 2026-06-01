package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestHandleDevelopBriefing_DerivedAutoComplete_SameIntent_StartsFresh pins the
// P7-review Issue E fix: re-issuing develop-start with the SAME intent on a
// DERIVED auto-complete session must create a FRESH session, not return the
// closed briefing forever. The bug was a raw `existing.ClosedAt == ""` read —
// auto-complete keeps ClosedAt unstamped, so the raw read re-entered the
// same-intent idempotent branch and stuck-looped the agent on close+start-next.
func TestHandleDevelopBriefing_DerivedAutoComplete_SameIntent_StartsFresh(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-18",
		// auto close-mode so the gate can fire (manual/unset would block it).
		CloseDeployMode: topology.CloseModeAuto,
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})

	// Start task A.
	if res, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "task A", Scope: []string{"appdev"}}, runtime.Info{InContainer: true}); err != nil || res.IsError {
		t.Fatalf("first start failed: %v / %s", err, extractText(res))
	}

	// Drive appdev green -> the session is DERIVED auto-complete (unstamped).
	_ = workflow.RecordDeployAttempt(dir, "appdev", workflow.DeployAttempt{AttemptedAt: "t", SucceededAt: "t"})
	_ = workflow.RecordVerifyAttempt(dir, "appdev", workflow.VerifyAttempt{AttemptedAt: "t", PassedAt: "t", Passed: true})

	ws, _ := workflow.CurrentWorkSession(dir)
	if ws == nil || ws.ClosedAt != "" {
		t.Fatalf("precondition: derived-closed session must keep ClosedAt unstamped; got %+v", ws)
	}
	if closed, _, _ := workflow.DeriveCloseState(dir, ws); !closed {
		t.Fatal("precondition: session must be derived auto-complete")
	}

	// Re-start with the SAME intent. Must replace, not return the stuck briefing.
	if res, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "task A", Scope: []string{"appdev"}}, runtime.Info{InContainer: true}); err != nil || res.IsError {
		t.Fatalf("re-start failed: %v / %s", err, extractText(res))
	}
	fresh, _ := workflow.CurrentWorkSession(dir)
	if fresh == nil {
		t.Fatal("expected a fresh session after re-start on a derived-closed session")
	}
	if len(fresh.Deploys) != 0 || len(fresh.Verifies) != 0 {
		t.Errorf("re-start on a derived-closed session must create a FRESH session with no attempt history; got Deploys=%v Verifies=%v",
			fresh.Deploys, fresh.Verifies)
	}
}

// TestLaunchOverlayAddendum pins the P7-review Issue D fix: an in-flight
// launch-production is surfaced as a uniform project overlay on develop status,
// so an open work session (FocusWork) no longer hides it.
func TestLaunchOverlayAddendum(t *testing.T) {
	t.Parallel()
	t.Run("no launch state -> empty", func(t *testing.T) {
		t.Parallel()
		if got := launchOverlayAddendum(t.TempDir(), "proj1"); got != "" {
			t.Errorf("want empty, got %q", got)
		}
	})
	t.Run("active launch -> overlay names target + resume call", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := writeLaunchState(dir, &launchState{
			LaunchID:          "L1",
			SourceProjectID:   "proj1",
			TargetProjectName: "myapp-prod",
			Status:            topology.LaunchStatusReadyToLaunch,
		}); err != nil {
			t.Fatalf("writeLaunchState: %v", err)
		}
		got := launchOverlayAddendum(dir, "proj1")
		if !strings.Contains(got, "myapp-prod") || !strings.Contains(got, `workflow="launch-production"`) {
			t.Errorf("overlay must name the target + resume call; got %q", got)
		}
	})
	t.Run("launch for a different project is not surfaced", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := writeLaunchState(dir, &launchState{
			LaunchID:          "L2",
			SourceProjectID:   "other-proj",
			TargetProjectName: "other-prod",
			Status:            topology.LaunchStatusReadyToLaunch,
		}); err != nil {
			t.Fatalf("writeLaunchState: %v", err)
		}
		if got := launchOverlayAddendum(dir, "proj1"); got != "" {
			t.Errorf("a launch for a different source project must not surface; got %q", got)
		}
	})
}
