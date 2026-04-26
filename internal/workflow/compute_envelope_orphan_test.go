package workflow

import (
	"context"
	"os"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// G4 — orphan-meta visibility. computeOrphanMetas + IdleOrphan tests.
// Plan: plans/open-findings-resolution-2026-04-26.md §4.6 Phase 2 + 3.

func TestComputeOrphanMetas_LiveDeleted_FlagsAsOrphan(t *testing.T) {
	t.Parallel()
	metas := []*ServiceMeta{{
		Hostname: "appdev", Mode: topology.PlanModeDev,
		BootstrappedAt: "2026-04-25", // complete
	}}
	out := computeOrphanMetas(nil, metas, nil, nil)
	if len(out) != 1 {
		t.Fatalf("orphans = %d, want 1", len(out))
	}
	if out[0].Hostname != "appdev" {
		t.Errorf("hostname = %q, want appdev", out[0].Hostname)
	}
	if out[0].Reason != OrphanReasonLiveDeleted {
		t.Errorf("reason = %q, want %q", out[0].Reason, OrphanReasonLiveDeleted)
	}
}

func TestComputeOrphanMetas_IncompleteWithDeadSession_FlagsLost(t *testing.T) {
	t.Parallel()
	deadPID := 9999999
	metas := []*ServiceMeta{{
		Hostname: "appdev", Mode: topology.PlanModeDev,
		BootstrapSession: "sess-dead",
		// no BootstrappedAt — incomplete
	}}
	alivePIDs := map[int]struct{}{} // empty: no PIDs alive
	sessionByID := map[string]int{"sess-dead": deadPID}

	out := computeOrphanMetas(nil, metas, alivePIDs, sessionByID)
	if len(out) != 1 {
		t.Fatalf("orphans = %d, want 1", len(out))
	}
	if out[0].Reason != OrphanReasonIncompleteLost {
		t.Errorf("reason = %q, want %q (dead session PID)", out[0].Reason, OrphanReasonIncompleteLost)
	}
}

func TestComputeOrphanMetas_IncompleteWithMissingSessionRecord_FlagsLost(t *testing.T) {
	t.Parallel()
	metas := []*ServiceMeta{{
		Hostname: "appdev", Mode: topology.PlanModeDev,
		BootstrapSession: "sess-vanished",
	}}
	alivePIDs := map[int]struct{}{os.Getpid(): {}} // some other session alive
	sessionByID := map[string]int{}                // sess-vanished not in registry

	out := computeOrphanMetas(nil, metas, alivePIDs, sessionByID)
	if len(out) != 1 {
		t.Fatalf("orphans = %d, want 1", len(out))
	}
	if out[0].Reason != OrphanReasonIncompleteLost {
		t.Errorf("reason = %q, want %q (missing session record)", out[0].Reason, OrphanReasonIncompleteLost)
	}
}

func TestComputeOrphanMetas_IncompleteWithLiveSession_StillLiveDeleted(t *testing.T) {
	t.Parallel()
	livePID := os.Getpid()
	metas := []*ServiceMeta{{
		Hostname: "appdev", Mode: topology.PlanModeDev,
		BootstrapSession: "sess-live",
	}}
	alivePIDs := map[int]struct{}{livePID: {}}
	sessionByID := map[string]int{"sess-live": livePID}

	out := computeOrphanMetas(nil, metas, alivePIDs, sessionByID)
	if len(out) != 1 {
		t.Fatalf("orphans = %d, want 1", len(out))
	}
	// Session is alive but the live service is gone — incomplete-lost
	// only fires when the SESSION is dead. Live session means the
	// running process could still resume; classify as live-deleted so
	// the orphan signal surfaces but doesn't block resume.
	if out[0].Reason != OrphanReasonLiveDeleted {
		t.Errorf("reason = %q, want %q (session alive, runtime gone)", out[0].Reason, OrphanReasonLiveDeleted)
	}
}

func TestComputeOrphanMetas_BothPairHalvesLive_NotOrphan(t *testing.T) {
	t.Parallel()
	services := []platform.ServiceStack{
		{Name: "appdev"}, {Name: "appstage"},
	}
	metas := []*ServiceMeta{{
		Hostname: "appdev", StageHostname: "appstage",
		Mode: topology.PlanModeStandard, BootstrappedAt: "2026-04-25",
	}}
	out := computeOrphanMetas(services, metas, nil, nil)
	if len(out) != 0 {
		t.Errorf("orphans = %d, want 0 (both halves live)", len(out))
	}
}

func TestComputeOrphanMetas_OnePairHalfLive_NotOrphan(t *testing.T) {
	t.Parallel()
	services := []platform.ServiceStack{{Name: "appdev"}} // stage gone
	metas := []*ServiceMeta{{
		Hostname: "appdev", StageHostname: "appstage",
		Mode: topology.PlanModeStandard, BootstrappedAt: "2026-04-25",
	}}
	out := computeOrphanMetas(services, metas, nil, nil)
	if len(out) != 0 {
		t.Errorf("orphans = %d, want 0 (dev half still live)", len(out))
	}
}

func TestComputeOrphanMetas_NilAlivePIDs_FallsBackToLiveDeleted(t *testing.T) {
	t.Parallel()
	metas := []*ServiceMeta{{
		Hostname: "appdev", Mode: topology.PlanModeDev,
		BootstrapSession: "sess-x", // incomplete + session, but we have no liveness info
	}}
	out := computeOrphanMetas(nil, metas, nil, nil)
	if len(out) != 1 {
		t.Fatalf("orphans = %d, want 1", len(out))
	}
	if out[0].Reason != OrphanReasonLiveDeleted {
		t.Errorf("reason = %q, want %q (liveness unknown → don't claim incomplete-lost)",
			out[0].Reason, OrphanReasonLiveDeleted)
	}
}

func TestComputeOrphanMetas_MixedOrphanAndLive(t *testing.T) {
	t.Parallel()
	services := []platform.ServiceStack{{Name: "alivedev"}}
	metas := []*ServiceMeta{
		{Hostname: "alivedev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-04-25"},
		{Hostname: "ghostdev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-04-25"},
	}
	out := computeOrphanMetas(services, metas, nil, nil)
	if len(out) != 1 {
		t.Fatalf("orphans = %d, want 1", len(out))
	}
	if out[0].Hostname != "ghostdev" {
		t.Errorf("orphan hostname = %q, want ghostdev", out[0].Hostname)
	}
}

func TestComputeOrphanMetas_NilMeta_Skipped(t *testing.T) {
	t.Parallel()
	metas := []*ServiceMeta{
		nil,
		{Hostname: "ghostdev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-04-25"},
		nil,
	}
	out := computeOrphanMetas(nil, metas, nil, nil)
	if len(out) != 1 {
		t.Fatalf("orphans = %d, want 1 (nils skipped)", len(out))
	}
}

func TestComputeOrphanMetas_SortedByHostname(t *testing.T) {
	t.Parallel()
	metas := []*ServiceMeta{
		{Hostname: "zorpdev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-04-25"},
		{Hostname: "alphadev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-04-25"},
		{Hostname: "midevdev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-04-25"},
	}
	out := computeOrphanMetas(nil, metas, nil, nil)
	if len(out) != 3 {
		t.Fatalf("orphans = %d, want 3", len(out))
	}
	want := []string{"alphadev", "midevdev", "zorpdev"}
	for i, h := range want {
		if out[i].Hostname != h {
			t.Errorf("orphans[%d] = %q, want %q (sorted)", i, out[i].Hostname, h)
		}
	}
}

// ---------- deriveIdleScenario tests with orphan + live mixed states ----------

func TestDeriveIdleScenario_OrphanOnly_RoutesToIdleOrphan(t *testing.T) {
	t.Parallel()
	orphans := []OrphanMeta{{Hostname: "ghostdev", Reason: OrphanReasonLiveDeleted}}
	got := deriveIdleScenario(PhaseIdle, nil, orphans)
	if got != IdleOrphan {
		t.Errorf("scenario = %q, want %q", got, IdleOrphan)
	}
}

func TestDeriveIdleScenario_OrphanPlusLiveRuntime_NotIdleOrphan(t *testing.T) {
	t.Parallel()
	services := []ServiceSnapshot{
		{Hostname: "alive", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true},
	}
	orphans := []OrphanMeta{{Hostname: "ghost", Reason: OrphanReasonLiveDeleted}}
	got := deriveIdleScenario(PhaseIdle, services, orphans)
	// Live bootstrapped runtime → IdleBootstrapped takes precedence; orphan
	// still appears in env.OrphanMetas for visibility but doesn't drive scenario.
	if got != IdleBootstrapped {
		t.Errorf("scenario = %q, want %q (live runtime suppresses orphan routing)", got, IdleBootstrapped)
	}
}

func TestDeriveIdleScenario_OrphanPlusLiveManaged_NotIdleOrphan(t *testing.T) {
	t.Parallel()
	services := []ServiceSnapshot{
		{Hostname: "pgdev", RuntimeClass: topology.RuntimeManaged},
	}
	orphans := []OrphanMeta{{Hostname: "ghost", Reason: OrphanReasonLiveDeleted}}
	got := deriveIdleScenario(PhaseIdle, services, orphans)
	// Managed deps don't drive runtime buckets, but DO suppress orphan
	// routing — user has live infrastructure even with stale runtime metas.
	if got != IdleEmpty {
		t.Errorf("scenario = %q, want %q (managed live suppresses orphan)", got, IdleEmpty)
	}
}

func TestDeriveIdleScenario_OrphanPlusAdoptableRuntime_RoutesToAdopt(t *testing.T) {
	t.Parallel()
	services := []ServiceSnapshot{
		{Hostname: "legacy", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: false},
	}
	orphans := []OrphanMeta{{Hostname: "ghost", Reason: OrphanReasonLiveDeleted}}
	got := deriveIdleScenario(PhaseIdle, services, orphans)
	if got != IdleAdopt {
		t.Errorf("scenario = %q, want %q (adoptable runtime suppresses orphan)", got, IdleAdopt)
	}
}

func TestDeriveIdleScenario_NoOrphansNoServices_StillEmpty(t *testing.T) {
	t.Parallel()
	got := deriveIdleScenario(PhaseIdle, nil, nil)
	if got != IdleEmpty {
		t.Errorf("scenario = %q, want %q", got, IdleEmpty)
	}
}

func TestDeriveIdleScenario_NonIdlePhase_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	orphans := []OrphanMeta{{Hostname: "ghost", Reason: OrphanReasonLiveDeleted}}
	got := deriveIdleScenario(PhaseDevelopActive, nil, orphans)
	if got != "" {
		t.Errorf("scenario = %q, want empty (non-idle phase)", got)
	}
}

// ---------- ComputeEnvelope integration ----------

func TestComputeEnvelope_LoadsOrphanMetasIntoEnvelope(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Write a complete meta on disk with no live counterpart.
	if err := WriteServiceMeta(dir, &ServiceMeta{
		Hostname: "ghostdev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-04-25",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock().
		WithServices(nil).
		WithProject(&platform.Project{ID: "p1", Name: "demo"})

	env, err := ComputeEnvelope(context.Background(), mock, dir, "p1", runtime.Info{}, fixedTime)
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}
	if len(env.OrphanMetas) != 1 {
		t.Fatalf("env.OrphanMetas = %d, want 1", len(env.OrphanMetas))
	}
	if env.OrphanMetas[0].Hostname != "ghostdev" {
		t.Errorf("hostname = %q, want ghostdev", env.OrphanMetas[0].Hostname)
	}
	if env.OrphanMetas[0].Reason != OrphanReasonLiveDeleted {
		t.Errorf("reason = %q, want %q", env.OrphanMetas[0].Reason, OrphanReasonLiveDeleted)
	}
	if env.IdleScenario != IdleOrphan {
		t.Errorf("idleScenario = %q, want %q", env.IdleScenario, IdleOrphan)
	}
}
