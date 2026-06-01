package workflow

import (
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestRenderStatus_DevModeClose_SurfacesTransience pins RC-A′: an auto-closed
// session whose required service is a dev-mode dynamic runtime (deferred-start:
// served only by the ephemeral dev-server) must NOT read as plain "all services
// done" — the close line states the state is live-via-dev-server, not durable.
// This is the e2 headline fix: deploy+verify+auto-complete previously implied
// durability for a state that 502s after a container cycle.
func TestRenderStatus_DevModeClose_SurfacesTransience(t *testing.T) {
	t.Parallel()
	closed := time.Now()
	out := RenderStatus(Response{
		Envelope: StateEnvelope{
			Phase: PhaseDevelopClosed,
			Services: []ServiceSnapshot{
				{Hostname: "appdev", TypeVersion: "bun@1.3.9", Mode: topology.ModeDev, RuntimeClass: topology.RuntimeDynamic},
			},
			WorkSession: &WorkSessionSummary{
				Intent:   "weather dashboard",
				Services: []string{"appdev"},
				ClosedAt: &closed,
			},
		},
	})
	if strings.Contains(out, "(all services done)") {
		t.Fatalf("dev-mode close must not read as plain done:\n%s", out)
	}
	low := strings.ToLower(out)
	if !strings.Contains(low, "dev-server") && !strings.Contains(low, "dev_server") && !strings.Contains(low, "not durable") {
		t.Fatalf("dev-mode close must surface transience:\n%s", out)
	}
}

// TestRenderStatus_ImplicitWebClose_NoTransienceCaveat is the control: a
// php-nginx (implicit-web, never deferred-start) close reads as done.
func TestRenderStatus_ImplicitWebClose_NoTransienceCaveat(t *testing.T) {
	t.Parallel()
	closed := time.Now()
	out := RenderStatus(Response{
		Envelope: StateEnvelope{
			Phase: PhaseDevelopClosed,
			Services: []ServiceSnapshot{
				{Hostname: "appdev", TypeVersion: "php-nginx@8.4", Mode: topology.ModeStandard, RuntimeClass: topology.RuntimeImplicitWeb},
			},
			WorkSession: &WorkSessionSummary{
				Intent:   "blog",
				Services: []string{"appdev"},
				ClosedAt: &closed,
			},
		},
	})
	if !strings.Contains(out, "(all services done)") {
		t.Fatalf("durable close must read as done:\n%s", out)
	}
}

// TestRenderStatus_DevModeActive_SurfacesTransience: even before close, an
// active session with a dev-mode dynamic required service surfaces the
// durability caveat so the agent knows the live URL is unsupervised.
func TestRenderStatus_DevModeActive_SurfacesTransience(t *testing.T) {
	t.Parallel()
	out := RenderStatus(Response{
		Envelope: StateEnvelope{
			Phase: PhaseDevelopActive,
			Services: []ServiceSnapshot{
				{Hostname: "appdev", TypeVersion: "bun@1.3.9", Mode: topology.ModeDev, RuntimeClass: topology.RuntimeDynamic},
			},
			WorkSession: &WorkSessionSummary{
				Intent:   "weather dashboard",
				Services: []string{"appdev"},
				Deploys:  map[string][]AttemptInfo{"appdev": {{Success: true}}},
				Verifies: map[string][]AttemptInfo{"appdev": {{Success: true}}},
			},
		},
	})
	low := strings.ToLower(out)
	if !strings.Contains(low, "dev_server") && !strings.Contains(low, "dev-mode") {
		t.Fatalf("active dev-mode session must surface transience:\n%s", out)
	}
}
