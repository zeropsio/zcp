package ops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// activityFixture mirrors the JSON shape of the P0 live-capture fixtures under
// testdata/activity/ (eval-zcp 2026-06-30, frozen from a real first
// buildFromGit deploy + the .git-suffix fast-fail). The Processes/AppVersions
// arrays are the exact platform-mapped shapes ProjectActivity consumes.
type activityFixture struct {
	Note          string                     `json:"note"`
	ServiceStatus string                     `json:"serviceStatus"`
	Processes     []platform.ProcessEvent    `json:"processes"`
	AppVersions   []platform.AppVersionEvent `json:"appVersions"`
}

func loadActivityFixture(t *testing.T, name string) activityFixture {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "activity", name+".json"))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var f activityFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", name, err)
	}
	return f
}

// targetStackID returns the live-captured target service ID for a fixture. The
// appVersion's top-level serviceStackId is ALWAYS the target runtime (the build
// container lives only in build.serviceStackId + the process serviceStacks[]),
// so it is the unambiguous identity even when the process refs both.
func (f activityFixture) targetStackID(t *testing.T) string {
	t.Helper()
	if len(f.AppVersions) == 0 {
		t.Fatal("fixture has no appVersions to derive the target stack id")
	}
	return f.AppVersions[0].ServiceStackID
}

// TestProjectActivity exercises the loop-safe predicate against the P0 live
// fixtures. The process is the SOLE busy-truth (PENDING/RUNNING); the appVersion
// only refines the build/deploy LABEL of an already-busy service. The two
// negative cases (FAILED build + WAITING_TO_BUILD; stuck BUILDING with NO live
// process) are the loop-safety invariant: recovery is never gated.
func TestProjectActivity(t *testing.T) {
	const projectID = "0LwKmRBGRv21qk1P5QlDBw"
	const host = "svc"

	tests := []struct {
		name       string
		fixture    string
		mutate     func(*activityFixture, string) // optional synthetic tweak; arg2 = target stack id
		wantBusy   bool
		wantAction string
		wantStatus string
	}{
		{
			name:       "running build + BUILDING appversion => busy action=build",
			fixture:    "building",
			wantBusy:   true,
			wantAction: "build",
			wantStatus: "BUILDING",
		},
		{
			name:       "running build + DEPLOYING appversion => busy action=deploy",
			fixture:    "deploying",
			wantBusy:   true,
			wantAction: "deploy",
			wantStatus: "DEPLOYING",
		},
		{
			name:       "running build + WAITING_TO_BUILD appversion (pre-build window) => busy action=build status RUNNING",
			fixture:    "waiting_to_build_running",
			wantBusy:   true,
			wantAction: "build",
			wantStatus: "RUNNING",
		},
		{
			name:     "settled (FINISHED build + ACTIVE appversion + ACTIVE service) => not busy",
			fixture:  "terminal_active",
			wantBusy: false,
		},
		{
			// THE LOOP TRAP (CLAUDE.md): a <1s config/clone fast-fail leaves
			// stack.build FAILED with the appVersion frozen at WAITING_TO_BUILD.
			// Process terminal => NOT busy => adopt + corrective deploy stay open.
			name:     "FAILED build + WAITING_TO_BUILD frozen => not busy (recovery never gated)",
			fixture:  "failed_fastfail",
			wantBusy: false,
		},
		{
			// Codex deadlock guard: an appVersion stuck at BUILDING whose build
			// container died has NO live process to cancel. The appVersion must
			// NEVER mark a service busy on its own, or the gate never opens.
			name:    "stuck BUILDING appversion with NO live process => not busy",
			fixture: "building",
			mutate: func(f *activityFixture, _ string) {
				f.Processes = nil
			},
			wantBusy: false,
		},
		{
			// Lifecycle op (restart/scale/...) is process-only: no in-progress
			// appVersion, so the label comes straight from the process action.
			name:    "lifecycle restart (process-only, no in-progress appversion) => busy action=restart",
			fixture: "building",
			mutate: func(f *activityFixture, targetID string) {
				f.Processes = []platform.ProcessEvent{{
					ID:            "restart-proc-1",
					ProjectID:     projectID,
					ServiceStacks: []platform.ServiceStackRef{{ID: targetID, Name: host}},
					ActionName:    "stack.restart",
					Status:        platform.ProcessStatusRunning,
					Created:       "2026-06-30T10:00:00Z",
				}}
				f.AppVersions = nil // no in-progress appVersion -> label from process
			},
			wantBusy:   true,
			wantAction: "restart",
			wantStatus: "RUNNING",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := loadActivityFixture(t, tc.fixture)
			targetID := f.targetStackID(t)
			if tc.mutate != nil {
				tc.mutate(&f, targetID)
			}
			mock := platform.NewMock().
				WithProcessEvents(f.Processes).
				WithAppVersionEvents(f.AppVersions)
			idToHost := map[string]string{targetID: host}

			got, err := ProjectActivity(context.Background(), mock, projectID, idToHost, 100)
			if err != nil {
				t.Fatalf("ProjectActivity: %v", err)
			}

			act, busy := got[host]
			if busy != tc.wantBusy {
				t.Fatalf("busy(%s) = %v, want %v (activity=%+v)", host, busy, tc.wantBusy, got)
			}
			if !tc.wantBusy {
				if len(got) != 0 {
					t.Errorf("expected empty activity map for idle/terminal, got %+v", got)
				}
				return
			}
			if act.Action != tc.wantAction {
				t.Errorf("Action = %q, want %q", act.Action, tc.wantAction)
			}
			if act.Status != tc.wantStatus {
				t.Errorf("Status = %q, want %q", act.Status, tc.wantStatus)
			}
			if act.ProcessID == "" {
				t.Error("busy verdict must always carry a ProcessID (the cancel-escape / loop-safety invariant)")
			}
			if len(got) != 1 {
				t.Errorf("expected exactly one busy service, got %d: %+v", len(got), got)
			}
		})
	}
}

// TestProjectActivity_LiveStatesBeyondRunning pins the full non-terminal set
// (codex finding 6): CANCELING and ROLLBACKING are live in-flight states, so a
// service mid-cancel/mid-rollback is busy — not "idle" the way a {PENDING,
// RUNNING}-only predicate would wrongly report.
func TestProjectActivity_LiveStatesBeyondRunning(t *testing.T) {
	const projectID = "p"
	const targetID = "id-x"
	for _, status := range []string{
		platform.ProcessStatusPending,
		platform.ProcessStatusRunning,
		platform.ProcessStatusRollbacking,
		platform.ProcessStatusCanceling,
	} {
		t.Run(status, func(t *testing.T) {
			mock := platform.NewMock().WithProcessEvents([]platform.ProcessEvent{{
				ID:            "proc-" + status,
				ProjectID:     projectID,
				ServiceStacks: []platform.ServiceStackRef{{ID: targetID, Name: "svc"}},
				ActionName:    "stack.restart",
				Status:        status,
				Created:       "2026-06-30T10:00:00Z",
			}})
			got, err := ProjectActivity(context.Background(), mock, projectID, map[string]string{targetID: "svc"}, 100)
			if err != nil {
				t.Fatal(err)
			}
			if _, busy := got["svc"]; !busy {
				t.Errorf("status %s must be busy (live, non-terminal); got %+v", status, got)
			}
		})
	}
	// Terminal states are never busy.
	for _, status := range []string{
		platform.ProcessStatusFinished,
		platform.ProcessStatusFailed,
		platform.ProcessStatusCanceled,
		"SOME_FUTURE_UNKNOWN",
	} {
		t.Run("terminal/"+status, func(t *testing.T) {
			mock := platform.NewMock().WithProcessEvents([]platform.ProcessEvent{{
				ID:            "proc",
				ProjectID:     projectID,
				ServiceStacks: []platform.ServiceStackRef{{ID: targetID, Name: "svc"}},
				ActionName:    "stack.restart",
				Status:        status,
				Created:       "2026-06-30T10:00:00Z",
			}})
			got, _ := ProjectActivity(context.Background(), mock, projectID, map[string]string{targetID: "svc"}, 100)
			if _, busy := got["svc"]; busy {
				t.Errorf("status %s must NOT be busy; got %+v", status, got)
			}
		})
	}
}

// TestProjectActivity_LifecycleNotMislabeledByStaleAppVersion pins codex finding
// 7: a running lifecycle process (restart) must NOT be relabeled "build" off a
// stale real BUILDING appVersion lingering from a prior build. The appVersion
// phase only refines the build process; a restart is self-describing.
func TestProjectActivity_LifecycleNotMislabeledByStaleAppVersion(t *testing.T) {
	const projectID = "p"
	const targetID = "id-x"
	mock := platform.NewMock().
		WithProcessEvents([]platform.ProcessEvent{{
			ID:            "restart-1",
			ProjectID:     projectID,
			ServiceStacks: []platform.ServiceStackRef{{ID: targetID, Name: "svc"}},
			ActionName:    "stack.restart",
			Status:        platform.ProcessStatusRunning,
			Created:       "2026-06-30T11:00:00Z",
		}}).
		WithAppVersionEvents([]platform.AppVersionEvent{{
			ID:             "av-stale",
			ProjectID:      projectID,
			ServiceStackID: targetID,
			Source:         "GIT",
			Status:         platform.BuildStatusBuilding, // stale, lingering
			Created:        "2026-06-30T10:00:00Z",
		}})
	got, err := ProjectActivity(context.Background(), mock, projectID, map[string]string{targetID: "svc"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	a := got["svc"]
	if a.Action != "restart" {
		t.Errorf("Action = %q, want restart (stale BUILDING must not relabel a lifecycle op)", a.Action)
	}
	if a.Status != platform.ProcessStatusRunning {
		t.Errorf("Status = %q, want RUNNING", a.Status)
	}
}

// TestProjectActivity_IgnoresBuildContainerAndOtherServices proves the
// target-id match is unambiguous: the ephemeral build container ref inside the
// stack.build process (a distinct id, absent from idToHost) must not surface as
// its own busy service, and a sibling service's process must not bleed onto the
// target.
func TestProjectActivity_IgnoresBuildContainerAndOtherServices(t *testing.T) {
	const projectID = "0LwKmRBGRv21qk1P5QlDBw"
	f := loadActivityFixture(t, "building")
	targetID := f.targetStackID(t)

	// Build-container id is the OTHER ref in the stack.build process.
	var buildContainerID string
	for _, p := range f.Processes {
		for _, ref := range p.ServiceStacks {
			if ref.ID != targetID {
				buildContainerID = ref.ID
			}
		}
	}
	if buildContainerID == "" {
		t.Fatal("building fixture should carry a build-container ref distinct from the target")
	}

	mock := platform.NewMock().
		WithProcessEvents(f.Processes).
		WithAppVersionEvents(f.AppVersions)

	// idToHost maps ONLY the target (the build container is ephemeral and never
	// in the service list).
	idToHost := map[string]string{targetID: "appdev"}
	got, err := ProjectActivity(context.Background(), mock, projectID, idToHost, 100)
	if err != nil {
		t.Fatalf("ProjectActivity: %v", err)
	}
	if _, ok := got[buildContainerID]; ok {
		t.Errorf("build-container id leaked as a busy service key: %+v", got)
	}
	if len(got) != 1 {
		t.Errorf("expected only the target busy, got %+v", got)
	}
	if _, ok := got["appdev"]; !ok {
		t.Errorf("target appdev should be busy, got %+v", got)
	}
}

// TestIsAppVersionInProgress pins the shared phase-label predicate: only
// BUILDING / DEPLOYING are in-progress; every terminal/queued/failed state is
// not (so an appVersion can never independently mark a service busy).
func TestIsAppVersionInProgress(t *testing.T) {
	inProgress := []string{platform.BuildStatusBuilding, platform.BuildStatusDeploying}
	for _, s := range inProgress {
		if !isAppVersionInProgress(s) {
			t.Errorf("isAppVersionInProgress(%q) = false, want true", s)
		}
	}
	notInProgress := []string{
		platform.ServiceStatusActive,
		platform.BuildStatusBuildFailed,
		platform.BuildStatusDeployFailed,
		"WAITING_TO_BUILD",
		platform.ProcessStatusFailed,
		"",
	}
	for _, s := range notInProgress {
		if isAppVersionInProgress(s) {
			t.Errorf("isAppVersionInProgress(%q) = true, want false", s)
		}
	}
}
