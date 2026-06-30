package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestWaitProcesses_AllTerminal pins that WaitProcesses blocks until every given
// process reaches a terminal state and returns each final status. Two processes,
// each transitioning RUNNING -> FINISHED, settle.
func TestWaitProcesses_AllTerminal(t *testing.T) {
	mock := platform.NewMock().
		WithProcess(&platform.Process{ID: "p1", ActionName: "stack.build", Status: platform.ProcessStatusRunning}).
		WithProcess(&platform.Process{ID: "p2", ActionName: "stack.enableSubdomainAccess", Status: platform.ProcessStatusRunning}).
		WithProcessScenario("p1", platform.ProcessScenario{InitialStatus: platform.ProcessStatusRunning, Transitions: []platform.ProcessTransition{{AtCall: 1, Status: platform.ProcessStatusFinished}}}).
		WithProcessScenario("p2", platform.ProcessScenario{InitialStatus: platform.ProcessStatusRunning, Transitions: []platform.ProcessTransition{{AtCall: 1, Status: platform.ProcessStatusFinished}}})

	res, err := WaitProcesses(context.Background(), mock, []string{"p1", "p2", "p1"}, nil) // dup p1 dropped
	if err != nil {
		t.Fatalf("WaitProcesses: %v", err)
	}
	if !res.Settled || res.TimedOut {
		t.Errorf("want settled, got %+v", res)
	}
	if len(res.Processes) != 2 {
		t.Fatalf("want 2 results (dedup), got %+v", res.Processes)
	}
	for _, p := range res.Processes {
		if p.Status != platform.ProcessStatusFinished {
			t.Errorf("process %s status = %q, want FINISHED", p.ProcessID, p.Status)
		}
	}
}

// TestWaitProcesses_FailedReported pins that a FAILED process settles (waiting is
// over) but the message flags it, so "done waiting" is never read as "succeeded".
func TestWaitProcesses_FailedReported(t *testing.T) {
	mock := platform.NewMock().
		WithProcess(&platform.Process{ID: "p1", ActionName: "stack.build", Status: platform.ProcessStatusFailed})

	res, err := WaitProcesses(context.Background(), mock, []string{"p1"}, nil)
	if err != nil {
		t.Fatalf("WaitProcesses: %v", err)
	}
	if !res.Settled {
		t.Errorf("a FAILED terminal process still settles the wait; got %+v", res)
	}
	if !strings.Contains(res.Message, "FAILED") {
		t.Errorf("message must flag the failure; got %q", res.Message)
	}
}

// TestWaitProcesses_NoTargets rejects an empty wait set.
func TestWaitProcesses_NoTargets(t *testing.T) {
	mock := platform.NewMock()
	_, err := WaitProcesses(context.Background(), mock, []string{"", ""}, nil)
	if err == nil {
		t.Fatal("want INVALID_PARAMETER for no process to wait on")
	}
}

// TestWaitServiceSettled_DrainsThenSettles is the core service-wait pin: a
// service is busy (build RUNNING) on the first activity read, then the build
// finishes and the next activity read shows it drained — the loop must re-check
// and exit settled, reporting the build's terminal status. This models the
// universal "wait until the service has no live process" contract.
func TestWaitServiceSettled_DrainsThenSettles(t *testing.T) {
	const svcID = "svc1"
	buildRunning := []platform.Process{{
		ID: "build-1", ActionName: "stack.build", Status: platform.ProcessStatusRunning,
		ServiceStacks: []platform.ServiceStackRef{{ID: svcID, Name: "appdev"}},
		Created:       "2026-06-30T10:00:00Z",
		AppVersion:    &platform.ProcessAppVersion{Status: platform.BuildStatusBuilding},
	}}
	mock := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{{ID: svcID, Name: "appdev"}}).
		// Round 0 activity: build live. Round 1+: drained.
		WithProjectProcessesSequence([][]platform.Process{buildRunning, {}}).
		WithProcess(&platform.Process{ID: "build-1", ActionName: "stack.build", Status: platform.ProcessStatusRunning}).
		WithProcessScenario("build-1", platform.ProcessScenario{InitialStatus: platform.ProcessStatusRunning, Transitions: []platform.ProcessTransition{{AtCall: 1, Status: platform.ProcessStatusFinished}}})

	res, err := WaitServiceSettled(context.Background(), mock, "proj-1", "appdev", nil)
	if err != nil {
		t.Fatalf("WaitServiceSettled: %v", err)
	}
	if !res.Settled || res.TimedOut {
		t.Errorf("want settled, got %+v", res)
	}
	if len(res.Processes) != 1 || res.Processes[0].ProcessID != "build-1" || res.Processes[0].Status != platform.ProcessStatusFinished {
		t.Errorf("want the build reported FINISHED, got %+v", res.Processes)
	}
}

// TestWaitServiceSettled_AlreadyIdle settles immediately (no live process) and
// reports zero waited ops.
func TestWaitServiceSettled_AlreadyIdle(t *testing.T) {
	const svcID = "svc1"
	mock := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{{ID: svcID, Name: "appdev"}}).
		WithProjectProcesses([]platform.Process{{
			ID: "build-old", ActionName: "stack.build", Status: platform.ProcessStatusFinished,
			ServiceStacks: []platform.ServiceStackRef{{ID: svcID, Name: "appdev"}},
			Created:       "2026-06-30T10:00:00Z",
		}})

	res, err := WaitServiceSettled(context.Background(), mock, "proj-1", "appdev", nil)
	if err != nil {
		t.Fatalf("WaitServiceSettled: %v", err)
	}
	if !res.Settled || len(res.Processes) != 0 {
		t.Errorf("an idle service settles immediately with no waited ops; got %+v", res)
	}
}

// TestWaitServiceSettled_FailedBeforeFirstRead_Flagged pins the no-mislead
// contract: a build that FAILED before the wait even saw it live (the <1s
// fast-fail) — so ProjectActivity reports no live op on the first read — must
// still surface the failure, not return a clean "settled".
func TestWaitServiceSettled_FailedBeforeFirstRead_Flagged(t *testing.T) {
	const svcID = "svc1"
	mock := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{{ID: svcID, Name: "appdev"}}).
		WithProjectProcesses([]platform.Process{{
			ID: "build-fail", ActionName: "stack.build", Status: platform.ProcessStatusFailed,
			ServiceStacks: []platform.ServiceStackRef{{ID: svcID, Name: "appdev"}},
			Created:       "2026-06-30T10:00:00Z",
		}})

	res, err := WaitServiceSettled(context.Background(), mock, "proj-1", "appdev", nil)
	if err != nil {
		t.Fatalf("WaitServiceSettled: %v", err)
	}
	if !res.Settled {
		t.Errorf("a service with only terminal processes settles; got %+v", res)
	}
	if len(res.Processes) != 1 || res.Processes[0].Status != platform.ProcessStatusFailed {
		t.Fatalf("the freshly-failed build must be surfaced; got %+v", res.Processes)
	}
	if !strings.Contains(res.Message, "FAILED") {
		t.Errorf("settled message must flag the failure; got %q", res.Message)
	}
}

// TestWaitServiceSettled_UnknownHost rejects a hostname absent from the project.
func TestWaitServiceSettled_UnknownHost(t *testing.T) {
	mock := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{{ID: "svc1", Name: "appdev"}})
	_, err := WaitServiceSettled(context.Background(), mock, "proj-1", "ghost", nil)
	if err == nil {
		t.Fatal("want INVALID_PARAMETER for unknown hostname")
	}
}
