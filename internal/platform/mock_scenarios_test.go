package platform

import (
	"context"
	"sync"
	"testing"
)

func TestMockProcessScenario_TransitionsAsConfigured(t *testing.T) {
	t.Parallel()

	mock := NewMock().
		WithProcess(&Process{ID: "p1", ActionName: "start.service.stack", Status: "ignored"}).
		WithProcessScenario("p1", ProcessScenario{
			InitialStatus: "PENDING",
			Transitions: []ProcessTransition{
				{AtCall: 2, Status: "RUNNING"},
				{AtCall: 4, Status: "FINISHED"},
			},
		})

	want := []string{"PENDING", "RUNNING", "RUNNING", "FINISHED", "FINISHED"}
	for i, expected := range want {
		got, err := mock.GetProcess(context.Background(), "p1")
		if err != nil {
			t.Fatalf("call %d: GetProcess: %v", i+1, err)
		}
		if got.Status != expected {
			t.Errorf("call %d: status=%q, want %q", i+1, got.Status, expected)
		}
		// The base record fields stay stable across calls.
		if got.ID != "p1" || got.ActionName != "start.service.stack" {
			t.Errorf("call %d: base record drifted: %+v", i+1, got)
		}
	}
}

func TestMockProcessScenario_NoScenarioReturnsStoredStatus(t *testing.T) {
	t.Parallel()

	// Without a scenario, GetProcess returns the stored Status verbatim
	// (same as pre-Phase-3.1 behavior, just defensively copied).
	mock := NewMock().
		WithProcess(&Process{ID: "p1", ActionName: "start", Status: "RUNNING"})

	for i := range 3 {
		got, err := mock.GetProcess(context.Background(), "p1")
		if err != nil {
			t.Fatalf("GetProcess: %v", err)
		}
		if got.Status != "RUNNING" {
			t.Errorf("call %d: status=%q, want RUNNING", i+1, got.Status)
		}
	}
}

// TestMockProcessScenario_NoCallerAliasing proves GetProcess returns a
// COPY of the Process struct, not a pointer to the live mock state.
// Pre-Phase-3.1 the method returned the stored *Process directly; if a
// caller mutated any field, the next call would observe that mutation
// (and worse, concurrent callers could see torn writes).
//
// The test mutates a returned Process and asserts the next GetProcess
// is unaffected. Run with -race to also confirm no data race fires
// under concurrent reads while transitions advance.
func TestMockProcessScenario_NoCallerAliasing(t *testing.T) {
	t.Parallel()

	mock := NewMock().
		WithProcess(&Process{ID: "p1", ActionName: "start", Status: "PENDING"}).
		WithProcessScenario("p1", ProcessScenario{
			InitialStatus: "PENDING",
			Transitions:   []ProcessTransition{{AtCall: 50, Status: "FINISHED"}},
		})

	first, err := mock.GetProcess(context.Background(), "p1")
	if err != nil {
		t.Fatalf("first GetProcess: %v", err)
	}
	// Caller mutates the returned struct; this MUST NOT propagate
	// into the mock's live state.
	first.Status = "MUTATED_BY_CALLER"
	first.ActionName = "MUTATED_BY_CALLER"

	second, err := mock.GetProcess(context.Background(), "p1")
	if err != nil {
		t.Fatalf("second GetProcess: %v", err)
	}
	if second.Status == "MUTATED_BY_CALLER" {
		t.Error("Status leaked from caller mutation — GetProcess returned an aliased pointer")
	}
	if second.ActionName != "start" {
		t.Errorf("ActionName drifted: %q (caller mutation leaked)", second.ActionName)
	}

	// Run a concurrent storm to flush out any data race the new locking
	// strategy might introduce. With -race this fails loudly on any
	// torn access.
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			_, _ = mock.GetProcess(context.Background(), "p1")
		})
	}
	wg.Wait()
}

// TestMockProcessScenario_OverwriteResetsTimeline proves a second
// WithProcessScenario call for the same processID resets callCount —
// the new timeline starts fresh from call 1, regardless of how many
// GetProcess calls hit the previous scenario.
func TestMockProcessScenario_OverwriteResetsTimeline(t *testing.T) {
	t.Parallel()

	mock := NewMock().
		WithProcess(&Process{ID: "p1"}).
		WithProcessScenario("p1", ProcessScenario{
			InitialStatus: "OLD-INITIAL",
			Transitions:   []ProcessTransition{{AtCall: 1, Status: "OLD-FIRED"}},
		})

	// Burn a few calls on the first scenario.
	for range 3 {
		if _, err := mock.GetProcess(context.Background(), "p1"); err != nil {
			t.Fatalf("burn call: %v", err)
		}
	}

	// Install fresh scenario; callCount must reset.
	mock.WithProcessScenario("p1", ProcessScenario{
		InitialStatus: "NEW-INITIAL",
		Transitions:   []ProcessTransition{{AtCall: 2, Status: "NEW-FIRED"}},
	})

	got1, err := mock.GetProcess(context.Background(), "p1")
	if err != nil {
		t.Fatalf("after-overwrite call 1: %v", err)
	}
	if got1.Status != "NEW-INITIAL" {
		t.Errorf("after-overwrite call 1: status=%q, want NEW-INITIAL", got1.Status)
	}

	got2, err := mock.GetProcess(context.Background(), "p1")
	if err != nil {
		t.Fatalf("after-overwrite call 2: %v", err)
	}
	if got2.Status != "NEW-FIRED" {
		t.Errorf("after-overwrite call 2: status=%q, want NEW-FIRED", got2.Status)
	}
}

func TestScenarioStatusAt(t *testing.T) {
	t.Parallel()

	scenario := ProcessScenario{
		InitialStatus: "PENDING",
		Transitions: []ProcessTransition{
			{AtCall: 3, Status: "RUNNING"},
			{AtCall: 7, Status: "FINISHED"},
		},
	}

	cases := []struct {
		callNum int
		want    string
	}{
		{1, "PENDING"},
		{2, "PENDING"},
		{3, "RUNNING"},
		{4, "RUNNING"},
		{6, "RUNNING"},
		{7, "FINISHED"},
		{100, "FINISHED"},
	}
	for _, tc := range cases {
		got := scenarioStatusAt(scenario, tc.callNum)
		if got != tc.want {
			t.Errorf("scenarioStatusAt(scenario, %d) = %q, want %q", tc.callNum, got, tc.want)
		}
	}
}
