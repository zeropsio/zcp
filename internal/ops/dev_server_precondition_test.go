// Tests for: ops/dev_server.go verifyDevServerTarget — the PRECONDITION
// check (not the deleted deploy refusal). A dev server needs a RUNNING
// container to attach to; FAILED / READY_TO_DEPLOY-with-failed-history yield
// a clear ErrInvalidParameter precondition ("deploy it RUNNING first")
// instead of an opaque SSH timeout. Not a deadlock: resolving it is a deploy,
// which no longer gates (plans/deploy-gate-category-error-2026-06-04.md).
package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func assertDevServerPrecondition(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a precondition error, got nil")
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *platform.PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidParameter {
		t.Errorf("code = %s, want %s (precondition, not DIAGNOSIS_REQUIRED)", pe.Code, platform.ErrInvalidParameter)
	}
	if !strings.Contains(pe.Message, "RUNNING") {
		t.Errorf("message should name the RUNNING precondition, got %q", pe.Message)
	}
}

func TestDevServer_FailedReturnsPrecondition(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", Status: platform.ServiceStatusFailed},
		})

	err := verifyDevServerTarget(context.Background(), mock, "proj-1", "appdev")
	assertDevServerPrecondition(t, err)
}

func TestDevServer_ReadyToDeployWithFailedAppVersionReturnsPrecondition(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", Status: platform.ServiceStatusReadyToDeploy},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "svc-1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
		})

	err := verifyDevServerTarget(context.Background(), mock, "proj-1", "appdev")
	assertDevServerPrecondition(t, err)
}

func TestDevServer_RunningPasses(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", Status: platform.ServiceStatusRunning},
		})

	if err := verifyDevServerTarget(context.Background(), mock, "proj-1", "appdev"); err != nil {
		t.Fatalf("RUNNING target should pass: %v", err)
	}
}

// TestDevServer_ReadyToDeployFreshPasses pins that a never-deployed dev
// container (no failed history) is NOT pre-blocked — same as the old gate;
// the SSH layer surfaces its own error if the container truly isn't up.
func TestDevServer_ReadyToDeployFreshPasses(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", Status: platform.ServiceStatusReadyToDeploy},
		})

	if err := verifyDevServerTarget(context.Background(), mock, "proj-1", "appdev"); err != nil {
		t.Fatalf("fresh READY_TO_DEPLOY (no failed history) should pass: %v", err)
	}
}
