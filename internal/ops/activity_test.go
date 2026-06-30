package ops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// directActivityFixture mirrors the testdata/activity/direct_*.json fixtures —
// frozen from the live DIRECT GET /project/{id}/process endpoint (eval
// 2026-06-30): platform.Process values WITH the embedded appVersion phase, the
// exact shape ProjectActivity now consumes via GetProjectProcessesDirect.
type directActivityFixture struct {
	Note          string             `json:"note"`
	ServiceStatus string             `json:"serviceStatus"`
	Processes     []platform.Process `json:"processes"`
}

func loadDirectFixture(t *testing.T, name string) directActivityFixture {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "activity", name+".json"))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var f directActivityFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}
	return f
}

// targetStackID returns the target runtime's id — the first process serviceStack
// ref whose name is NOT an ephemeral build container ("build…").
func (f directActivityFixture) targetStackID(t *testing.T) string {
	t.Helper()
	for _, p := range f.Processes {
		for _, ref := range p.ServiceStacks {
			if !strings.HasPrefix(ref.Name, "build") {
				return ref.ID
			}
		}
	}
	t.Fatal("no non-build-container serviceStack ref in fixture")
	return ""
}

// TestProjectActivity exercises the loop-safe predicate against the P-direct live
// fixtures. The process is the SOLE busy-truth; the embedded appVersion only
// refines the build/deploy LABEL of an already-busy build process. The two
// negatives (FAILED build + frozen WAITING_TO_BUILD; settled) are the
// loop-safety invariant: recovery is never gated.
func TestProjectActivity(t *testing.T) {
	const projectID = "proj-1"
	const host = "svc"

	tests := []struct {
		name       string
		fixture    string
		wantBusy   bool
		wantAction string
		wantStatus string
	}{
		{"running build + BUILDING appversion => busy action=build", "direct_building", true, "build", "BUILDING"},
		{"running build + DEPLOYING appversion => busy action=deploy", "direct_deploying", true, "deploy", "DEPLOYING"},
		{"PENDING build + WAITING_TO_BUILD (pre-build) => busy action=build status=PENDING", "direct_waiting", true, "build", "PENDING"},
		{"settled (FINISHED build + ACTIVE) => not busy", "direct_terminal", false, "", ""},
		{"FAILED build + frozen WAITING_TO_BUILD => not busy (recovery never gated)", "direct_failfast", false, "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := loadDirectFixture(t, tc.fixture)
			targetID := f.targetStackID(t)
			mock := platform.NewMock().WithProjectProcesses(f.Processes)
			idToHost := map[string]string{targetID: host}

			got, err := ProjectActivity(context.Background(), mock, projectID, idToHost)
			if err != nil {
				t.Fatalf("ProjectActivity: %v", err)
			}
			act, busy := got[host]
			if busy != tc.wantBusy {
				t.Fatalf("busy(%s) = %v, want %v (activity=%+v)", host, busy, tc.wantBusy, got)
			}
			if !tc.wantBusy {
				if len(got) != 0 {
					t.Errorf("expected empty activity for idle/terminal, got %+v", got)
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
				t.Error("busy verdict must carry a ProcessID (cancel-escape / loop-safety)")
			}
			if len(got) != 1 {
				t.Errorf("expected exactly one busy service (build container must not leak), got %+v", got)
			}
		})
	}
}

// TestProjectActivity_LiveStatesBeyondRunning pins the full non-terminal set:
// CANCELING and ROLLBACKING are live (busy); FINISHED/FAILED/CANCELED + unknown
// are not.
func TestProjectActivity_LiveStatesBeyondRunning(t *testing.T) {
	const projectID, targetID = "p", "id-x"
	for _, status := range []string{
		platform.ProcessStatusPending, platform.ProcessStatusRunning,
		platform.ProcessStatusRollbacking, platform.ProcessStatusCanceling,
	} {
		t.Run("live/"+status, func(t *testing.T) {
			mock := platform.NewMock().WithProjectProcesses([]platform.Process{{
				ID: "proc", ActionName: "stack.restart", Status: status,
				ServiceStacks: []platform.ServiceStackRef{{ID: targetID, Name: "svc"}},
				Created:       "2026-06-30T10:00:00Z",
			}})
			got, _ := ProjectActivity(context.Background(), mock, projectID, map[string]string{targetID: "svc"})
			if _, busy := got["svc"]; !busy {
				t.Errorf("status %s must be busy; got %+v", status, got)
			}
		})
	}
	for _, status := range []string{
		platform.ProcessStatusFinished, platform.ProcessStatusFailed,
		platform.ProcessStatusCanceled, "SOME_FUTURE_UNKNOWN",
	} {
		t.Run("terminal/"+status, func(t *testing.T) {
			mock := platform.NewMock().WithProjectProcesses([]platform.Process{{
				ID: "proc", ActionName: "stack.restart", Status: status,
				ServiceStacks: []platform.ServiceStackRef{{ID: targetID, Name: "svc"}},
				Created:       "2026-06-30T10:00:00Z",
			}})
			got, _ := ProjectActivity(context.Background(), mock, projectID, map[string]string{targetID: "svc"})
			if _, busy := got["svc"]; busy {
				t.Errorf("status %s must NOT be busy; got %+v", status, got)
			}
		})
	}
}

// TestProjectActivity_LifecycleNotMislabeled pins that a lifecycle process
// (restart) is labeled by its own action, never relabeled "build" — the
// appVersion phase only refines a stack.build process. A stray embedded
// BUILDING appVersion on a restart process must not flip the label.
func TestProjectActivity_LifecycleNotMislabeled(t *testing.T) {
	const projectID, targetID = "p", "id-x"
	mock := platform.NewMock().WithProjectProcesses([]platform.Process{{
		ID: "restart-1", ActionName: "stack.restart", Status: platform.ProcessStatusRunning,
		ServiceStacks: []platform.ServiceStackRef{{ID: targetID, Name: "svc"}},
		Created:       "2026-06-30T11:00:00Z",
		AppVersion:    &platform.ProcessAppVersion{Status: platform.BuildStatusBuilding}, // stray, must be ignored
	}})
	got, err := ProjectActivity(context.Background(), mock, projectID, map[string]string{targetID: "svc"})
	if err != nil {
		t.Fatal(err)
	}
	a := got["svc"]
	if a.Action != "restart" || a.Status != platform.ProcessStatusRunning {
		t.Errorf("lifecycle restart mislabeled: action=%q status=%q, want restart/RUNNING", a.Action, a.Status)
	}
}

// TestProjectActivity_IgnoresBuildContainer proves target-id matching is
// unambiguous: the ephemeral build container ref in the stack.build process
// (distinct id, absent from idToHost) never surfaces as its own busy service.
func TestProjectActivity_IgnoresBuildContainer(t *testing.T) {
	const projectID = "proj-1"
	f := loadDirectFixture(t, "direct_building")
	targetID := f.targetStackID(t)
	var buildContainerID string
	for _, p := range f.Processes {
		for _, ref := range p.ServiceStacks {
			if ref.ID != targetID && strings.HasPrefix(ref.Name, "build") {
				buildContainerID = ref.ID
			}
		}
	}
	if buildContainerID == "" {
		t.Fatal("building fixture should carry a build-container ref")
	}
	mock := platform.NewMock().WithProjectProcesses(f.Processes)
	got, err := ProjectActivity(context.Background(), mock, projectID, map[string]string{targetID: "appdev"})
	if err != nil {
		t.Fatal(err)
	}
	if _, leaked := got[buildContainerID]; leaked {
		t.Errorf("build-container id leaked as busy service: %+v", got)
	}
	if len(got) != 1 || got["appdev"].Action == "" {
		t.Errorf("expected only appdev busy, got %+v", got)
	}
}

// TestIsAppVersionInProgress pins the phase-label predicate.
func TestIsAppVersionInProgress(t *testing.T) {
	for _, s := range []string{platform.BuildStatusBuilding, platform.BuildStatusDeploying} {
		if !isAppVersionInProgress(s) {
			t.Errorf("isAppVersionInProgress(%q) = false, want true", s)
		}
	}
	for _, s := range []string{platform.ServiceStatusActive, platform.BuildStatusBuildFailed, "WAITING_TO_BUILD", ""} {
		if isAppVersionInProgress(s) {
			t.Errorf("isAppVersionInProgress(%q) = true, want false", s)
		}
	}
}
