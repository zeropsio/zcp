package platform

// ProcessScenario describes a sequence of process status values that
// the mock advances through as GetProcess is called repeatedly. It
// lets tests express realistic platform transitions
// (PENDING → RUNNING → FINISHED) instead of pinning a single static
// state that doesn't resemble what production actually returns.
//
// Usage:
//
//	mock := platform.NewMock().
//	    WithProcess(&platform.Process{ID: "p1", ActionName: "start.service.stack"}).
//	    WithProcessScenario("p1", platform.ProcessScenario{
//	        InitialStatus: "PENDING",
//	        Transitions: []platform.ProcessTransition{
//	            {AtCall: 2, Status: "RUNNING"},
//	            {AtCall: 4, Status: "FINISHED"},
//	        },
//	    })
//
// Call 1 → "PENDING", call 2..3 → "RUNNING", call 4+ → "FINISHED".
//
// AtCall values must be monotonically increasing (the lookup picks the
// last transition whose AtCall ≤ current call number, so out-of-order
// entries silently shadow earlier ones).
//
// The base Process record (ID, ActionName, etc.) still comes from the
// Mock.processes map populated by WithProcess. The scenario only
// overrides Status on each GetProcess call.
type ProcessScenario struct {
	InitialStatus string
	Transitions   []ProcessTransition
}

// ProcessTransition specifies that, starting at the AtCall-th
// GetProcess call (1-indexed), the process Status flips to Status.
type ProcessTransition struct {
	AtCall int
	Status string
}

// processScenarioState tracks how many GetProcess calls have hit a
// given process so the mock can advance through Transitions
// deterministically. The Mock owns the lifetime of these instances
// and protects them under m.mu.
type processScenarioState struct {
	scenario  ProcessScenario
	callCount int
}

// WithProcessScenario installs a transition timeline for the given
// process ID. The base Process must be added separately via
// WithProcess; this method only governs the Status field across
// successive GetProcess calls. Returns m to support builder chaining.
//
// Calling WithProcessScenario twice for the same processID resets the
// timeline (callCount goes back to zero with the new scenario).
func (m *Mock) WithProcessScenario(processID string, scenario ProcessScenario) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processScenarios[processID] = &processScenarioState{scenario: scenario}
	return m
}

// scenarioStatusAt returns the active status after callNum calls have
// hit the scenario. Walks Transitions and returns the latest one whose
// AtCall ≤ callNum, falling back to InitialStatus when no transition
// has fired yet.
func scenarioStatusAt(s ProcessScenario, callNum int) string {
	status := s.InitialStatus
	for _, tr := range s.Transitions {
		if tr.AtCall <= callNum {
			status = tr.Status
		}
	}
	return status
}
