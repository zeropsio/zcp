package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// adoptNowMarker is a phrase unique to the idle adopt-now warning
// (adoptableServicesWarning). Its ABSENCE proves a busy candidate did not get
// the "adopt now" steer.
const adoptNowMarker = "before any service-scoped"

// TestEnrichWithMetaStatus_BusyAdoptable_WaitWarningNotAdoptNow pins the core
// fix: an adoptable runtime with a live build in flight gets the WAIT steer
// (re-discover / watch events / then adopt + the cancelable processId), NOT the
// "adopt now" warning. Its Activity is surfaced.
func TestEnrichWithMetaStatus_BusyAdoptable_WaitWarningNotAdoptNow(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", ServiceID: "id-appdev", Type: "nodejs@22", Status: "READY_TO_DEPLOY"},
		},
	}
	activity := map[string]ops.ServiceActivity{
		"appdev": {Action: "build", Status: platform.BuildStatusBuilding, ProcessID: "proc-1"},
	}

	enrichWithMetaStatus(result, stateDir, activity)

	if got := result.Services[0].AdoptionState; got != ops.AdoptionAdoptable {
		t.Errorf("AdoptionState: got %q, want adoptable (busy does not change the bucket)", got)
	}
	if a := result.Services[0].Activity; a == nil || a.Action != "build" || a.Status != platform.BuildStatusBuilding || a.ProcessID != "proc-1" {
		t.Errorf("Activity not attached/incorrect: %+v", a)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("Warnings: got %d, want 1 (wait only); full=%v", len(result.Warnings), result.Warnings)
	}
	w := result.Warnings[0]
	for _, want := range []string{"appdev", platform.BuildStatusBuilding, "in progress", "zerops_discover", "zerops_events", "then adopt", "proc-1", `action="cancel"`} {
		if !strings.Contains(w, want) {
			t.Errorf("wait warning missing snippet %q; got: %s", want, w)
		}
	}
	if strings.Contains(w, adoptNowMarker) {
		t.Errorf("busy adoptable must NOT get the adopt-now steer; got: %s", w)
	}
}

// TestEnrichWithMetaStatus_MixedBusyIdleAdoptable_TwoDistinctWarnings pins the
// partition: an idle adoptable gets adopt-now, a busy adoptable gets wait —
// each names only its own hostname.
func TestEnrichWithMetaStatus_MixedBusyIdleAdoptable_TwoDistinctWarnings(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", ServiceID: "id-appdev", Type: "nodejs@22", Status: "READY_TO_DEPLOY"},
			{Hostname: "frontend", ServiceID: "id-frontend", Type: "php-nginx@8.4", Status: "READY_TO_DEPLOY"},
		},
	}
	activity := map[string]ops.ServiceActivity{
		"appdev": {Action: "deploy", Status: platform.BuildStatusDeploying, ProcessID: "proc-9"},
	}

	enrichWithMetaStatus(result, stateDir, activity)

	if len(result.Warnings) != 2 {
		t.Fatalf("Warnings: got %d, want 2 (adopt-now + wait); full=%v", len(result.Warnings), result.Warnings)
	}
	var adoptNow, wait string
	for _, w := range result.Warnings {
		if strings.Contains(w, adoptNowMarker) {
			adoptNow = w
		} else {
			wait = w
		}
	}
	if adoptNow == "" || wait == "" {
		t.Fatalf("expected one adopt-now and one wait warning; got: %v", result.Warnings)
	}
	if !strings.Contains(adoptNow, "frontend") || strings.Contains(adoptNow, "appdev") {
		t.Errorf("adopt-now must list only the idle candidate frontend; got: %s", adoptNow)
	}
	if !strings.Contains(wait, "appdev") || strings.Contains(wait, "frontend") {
		t.Errorf("wait must list only the busy candidate appdev; got: %s", wait)
	}
}

// TestEnrichWithMetaStatus_AdoptedButBusy_ActivitySurfacedNoWarning pins §3.4:
// an already-adopted service that is busy gets its Activity surfaced (so the
// agent sees a live deploy before pushing onto it) but NO adopt/wait warning —
// deploy-onto-busy is surfaced, not hard-gated, in v1.
func TestEnrichWithMetaStatus_AdoptedButBusy_ActivitySurfacedNoWarning(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrappedAt:   "2026-06-30",
		BootstrapSession: "sess-1",
	}); err != nil {
		t.Fatal(err)
	}
	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", ServiceID: "id-appdev", Type: "nodejs@22", Status: "READY_TO_DEPLOY"},
			{Hostname: "appstage", ServiceID: "id-appstage", Type: "nodejs@22", Status: "READY_TO_DEPLOY"},
		},
	}
	activity := map[string]ops.ServiceActivity{
		"appdev": {Action: "deploy", Status: platform.BuildStatusDeploying, ProcessID: "proc-2"},
	}

	enrichWithMetaStatus(result, stateDir, activity)

	if result.Services[0].AdoptionState != ops.AdoptionAdopted {
		t.Errorf("appdev should be adopted; got %q", result.Services[0].AdoptionState)
	}
	if a := result.Services[0].Activity; a == nil || a.Status != platform.BuildStatusDeploying {
		t.Errorf("adopted-but-busy service must still surface Activity; got %+v", a)
	}
	for _, w := range result.Warnings {
		if strings.Contains(w, "adoptable") || strings.Contains(w, "in progress") {
			t.Errorf("no adopt/wait warning expected for an adopted service; got: %s", w)
		}
	}
}

// TestEnrichWithMetaStatus_NilActivity_PriorBehavior is the refactor-safety pin:
// a nil activity map degrades to the exact pre-activity behavior (adopt-now
// warning, no Activity attached).
func TestEnrichWithMetaStatus_NilActivity_PriorBehavior(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", ServiceID: "id-appdev", Type: "nodejs@22", Status: "READY_TO_DEPLOY"},
		},
	}

	enrichWithMetaStatus(result, stateDir, nil)

	if result.Services[0].Activity != nil {
		t.Errorf("nil activity must leave Activity nil; got %+v", result.Services[0].Activity)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], adoptNowMarker) {
		t.Errorf("nil activity must yield the adopt-now warning; got: %v", result.Warnings)
	}
}

// TestFetchProjectActivity_LiveBuildShape wires the P1 primitive through the
// discover fetch: a mock seeded with the P0-verified in-flight shape (a
// stack.build process RUNNING referencing the target + the ephemeral build
// container, plus a BUILDING appVersion on the target) yields a per-hostname
// activity map keyed by the discovered hostname. The build-container ref must
// not leak as its own entry.
func TestFetchProjectActivity_LiveBuildShape(t *testing.T) {
	t.Parallel()
	const projectID = "proj-1"
	const targetID = "id-appdev"
	const buildContainerID = "id-build-ephemeral" // distinct id, absent from the service list

	// Shape per internal/ops/testdata/activity/building.json (live eval-zcp).
	procs := []platform.ProcessEvent{{
		ID:        "build-proc-1",
		ProjectID: projectID,
		ServiceStacks: []platform.ServiceStackRef{
			{ID: targetID, Name: "appdev"},
			{ID: buildContainerID, Name: "buildappdevv123"},
		},
		ActionName: "stack.build",
		Status:     platform.ProcessStatusRunning,
		Created:    "2026-06-30T09:01:34Z",
	}}
	avs := []platform.AppVersionEvent{{
		ID:             "av-1",
		ProjectID:      projectID,
		ServiceStackID: targetID,
		Source:         "GIT",
		Status:         platform.BuildStatusBuilding,
		Created:        "2026-06-30T09:01:34Z",
	}}
	mock := platform.NewMock().WithProcessEvents(procs).WithAppVersionEvents(avs)

	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", ServiceID: targetID, Type: "nodejs@22", Status: "READY_TO_DEPLOY"},
		},
	}

	activity := fetchProjectActivity(context.Background(), mock, projectID, result)
	a, ok := activity["appdev"]
	if !ok {
		t.Fatalf("appdev should be busy; got %+v", activity)
	}
	if a.Action != "build" || a.Status != platform.BuildStatusBuilding || a.ProcessID != "build-proc-1" {
		t.Errorf("activity = %+v, want {build BUILDING build-proc-1}", a)
	}
	if _, leaked := activity["buildappdevv123"]; leaked {
		t.Errorf("build container leaked into activity map: %+v", activity)
	}
	if len(activity) != 1 {
		t.Errorf("expected exactly one busy service, got %+v", activity)
	}
}
