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
// (block until done with the wait action + the cancelable processId), NOT the
// "adopt now" warning. Its Activity list is surfaced.
func TestEnrichWithMetaStatus_BusyAdoptable_WaitWarningNotAdoptNow(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", ServiceID: "id-appdev", Type: "nodejs@22", Status: "READY_TO_DEPLOY"},
		},
	}
	activity := map[string][]ops.LiveOp{
		"appdev": {{Action: "build", Status: platform.BuildStatusBuilding, ProcessID: "proc-1"}},
	}

	enrichWithMetaStatus(result, stateDir, activity)

	if got := result.Services[0].AdoptionState; got != ops.AdoptionAdoptable {
		t.Errorf("AdoptionState: got %q, want adoptable (busy does not change the bucket)", got)
	}
	a := result.Services[0].Activity
	if len(a) != 1 || a[0].Action != "build" || a[0].Status != platform.BuildStatusBuilding || a[0].ProcessID != "proc-1" {
		t.Errorf("Activity not attached/incorrect: %+v", a)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("Warnings: got %d, want 1 (live-activity only); full=%v", len(result.Warnings), result.Warnings)
	}
	w := result.Warnings[0]
	for _, want := range []string{"Live activity", "appdev", "build", platform.BuildStatusBuilding, "in flight", `action="wait"`, "service=", "zerops_discover", "proc-1", `action="cancel"`} {
		if !strings.Contains(w, want) {
			t.Errorf("live-activity warning missing snippet %q; got: %s", want, w)
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
	activity := map[string][]ops.LiveOp{
		"appdev": {{Action: "deploy", Status: platform.BuildStatusDeploying, ProcessID: "proc-9"}},
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

// TestEnrichWithMetaStatus_MultipleConcurrentOps pins that a service with TWO
// live ops surfaces BOTH in its attached Activity and in the warning (the list
// model — no single-representative collapse).
func TestEnrichWithMetaStatus_MultipleConcurrentOps(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", ServiceID: "id-appdev", Type: "nodejs@22", Status: "NEW"},
		},
	}
	activity := map[string][]ops.LiveOp{
		"appdev": {
			{Action: "subdomain-enable", Status: platform.ProcessStatusPending, ProcessID: "proc-sub"},
			{Action: "build", Status: platform.BuildStatusBuilding, ProcessID: "proc-build"},
		},
	}

	enrichWithMetaStatus(result, stateDir, activity)

	if len(result.Services[0].Activity) != 2 {
		t.Fatalf("both live ops must be surfaced; got %+v", result.Services[0].Activity)
	}
	w := result.Warnings[0]
	for _, want := range []string{"build", platform.BuildStatusBuilding, "proc-build", "subdomain-enable", "proc-sub"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning must name both concurrent ops; missing %q; got: %s", want, w)
		}
	}
}

// TestEnrichWithMetaStatus_AdoptedButBusy_GetsLiveActivityNote pins that an
// already-adopted service that is busy gets its Activity surfaced AND the
// project-level live-activity note (so the agent doesn't deploy onto it
// mid-operation) — but NO adopt warning (it's already tracked).
func TestEnrichWithMetaStatus_AdoptedButBusy_GetsLiveActivityNote(t *testing.T) {
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
	activity := map[string][]ops.LiveOp{
		"appdev": {{Action: "deploy", Status: platform.BuildStatusDeploying, ProcessID: "proc-2"}},
	}

	enrichWithMetaStatus(result, stateDir, activity)

	if result.Services[0].AdoptionState != ops.AdoptionAdopted {
		t.Errorf("appdev should be adopted; got %q", result.Services[0].AdoptionState)
	}
	a := result.Services[0].Activity
	if len(a) != 1 || a[0].Status != platform.BuildStatusDeploying {
		t.Errorf("adopted-but-busy service must still surface Activity; got %+v", a)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected exactly the live-activity note; got %v", result.Warnings)
	}
	w := result.Warnings[0]
	if !strings.Contains(w, "Live activity") || !strings.Contains(w, "appdev") || !strings.Contains(w, "proc-2") {
		t.Errorf("busy adopted service must get the live-activity note naming it + its processId; got: %s", w)
	}
	if strings.Contains(w, adoptNowMarker) {
		t.Errorf("adopted service must NOT get an adopt steer; got: %s", w)
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

	procs := []platform.Process{{
		ID: "build-proc-1",
		ServiceStacks: []platform.ServiceStackRef{
			{ID: targetID, Name: "appdev"},
			{ID: buildContainerID, Name: "buildappdevv123"},
		},
		ActionName: "stack.build",
		Status:     platform.ProcessStatusRunning,
		Created:    "2026-06-30T09:01:34Z",
		AppVersion: &platform.ProcessAppVersion{Status: platform.BuildStatusBuilding},
	}}
	mock := platform.NewMock().WithProjectProcesses(procs)

	result := &ops.DiscoverResult{
		Services: []ops.ServiceInfo{
			{Hostname: "appdev", ServiceID: targetID, Type: "nodejs@22", Status: "READY_TO_DEPLOY"},
		},
	}

	activity := fetchProjectActivity(context.Background(), mock, projectID, result)
	live := activity["appdev"]
	if len(live) != 1 {
		t.Fatalf("appdev should have one live op; got %+v", activity)
	}
	a := live[0]
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
