package workflow

import (
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// fail builds a failing attempt with the given class + fix kind. RecordedAt is
// left to the caller where wall-budget timing matters.
func fail(class topology.FailureClass, fix FixClassKind, signals ...string) PortAttempt {
	return PortAttempt{Class: class, FixKind: fix, Signals: signals}
}

// TestClassStallStreak_SurvivesSignalVariation is the key Phase 2 case: the
// streak keys on the FailureClass CATEGORY, so it keeps counting across attempts
// whose Signals differ within one root cause.
func TestClassStallStreak_SurvivesSignalVariation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		attempts []PortAttempt
		want     int
	}{
		{
			name: "same class, different signals each turn, streak counts all",
			attempts: []PortAttempt{
				fail(topology.FailureClassBuild, FixAddPrepareCommands, "build:command-not-found"),
				fail(topology.FailureClassBuild, FixInspect, "build:missing-header"),
				fail(topology.FailureClassBuild, FixInspect, "build:linker-error"),
			},
			want: 3,
		},
		{
			name: "class changes on last turn resets streak to 1",
			attempts: []PortAttempt{
				fail(topology.FailureClassBuild, FixAddPrepareCommands, "build:command-not-found"),
				fail(topology.FailureClassBuild, FixInspect, "build:x"),
				fail(topology.FailureClassStart, FixWireDepRefs, "init:db-connection-refused"),
			},
			want: 1,
		},
		{
			name:     "no attempts",
			attempts: nil,
			want:     0,
		},
		{
			name: "trailing success breaks the streak",
			attempts: []PortAttempt{
				fail(topology.FailureClassBuild, FixInspect, "build:x"),
				{Succeeded: true},
			},
			want: 0,
		},
		{
			name: "only the trailing same-class run counts",
			attempts: []PortAttempt{
				fail(topology.FailureClassStart, FixSetEnv, "init:missing-env-var"),
				fail(topology.FailureClassBuild, FixAddPrepareCommands, "build:command-not-found"),
				fail(topology.FailureClassBuild, FixInspect, "build:y"),
			},
			want: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ClassStallStreak(tt.attempts); got != tt.want {
				t.Errorf("ClassStallStreak = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestPhaseStallStreak_TripsOnNonAdvancement: the streak counts trailing
// attempts whose fix-class PHASE does not advance (same-or-backward), and resets
// the moment the phase advances.
func TestPhaseStallStreak_TripsOnNonAdvancement(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		attempts     []PortAttempt
		progressRose bool
		want         int
	}{
		{
			name: "phase stuck at serve (config) across turns trips",
			attempts: []PortAttempt{
				fail(topology.FailureClassConfig, FixGlueYAML),
				fail(topology.FailureClassConfig, FixGlueYAML),
				fail(topology.FailureClassConfig, FixGlueYAML),
			},
			want: 3,
		},
		{
			name: "phase advances build->start->serve, no stall",
			attempts: []PortAttempt{
				fail(topology.FailureClassBuild, FixAddPrepareCommands),
				fail(topology.FailureClassStart, FixWireDepRefs),
				fail(topology.FailureClassConfig, FixGlueYAML),
			},
			want: 1,
		},
		{
			name: "advance then stall — only trailing stall counts",
			attempts: []PortAttempt{
				fail(topology.FailureClassBuild, FixAddPrepareCommands),
				fail(topology.FailureClassStart, FixWireDepRefs),
				fail(topology.FailureClassStart, FixSetEnv),
				fail(topology.FailureClassStart, FixSetEnv),
			},
			want: 3,
		},
		{
			name: "backward phase movement (serve->build) is a stall, not an advance",
			attempts: []PortAttempt{
				fail(topology.FailureClassConfig, FixGlueYAML),
				fail(topology.FailureClassBuild, FixAddPrepareCommands),
			},
			want: 2,
		},
		{
			name:         "progressRose=true breaks an otherwise-stalled streak this turn",
			progressRose: true,
			attempts: []PortAttempt{
				fail(topology.FailureClassConfig, FixGlueYAML),
				fail(topology.FailureClassConfig, FixGlueYAML),
			},
			want: 1,
		},
		{
			name:     "single attempt is streak 1",
			attempts: []PortAttempt{fail(topology.FailureClassBuild, FixAddPrepareCommands)},
			want:     1,
		},
		{
			name:     "no attempts",
			attempts: nil,
			want:     0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := PhaseStallStreak(tt.attempts, tt.progressRose); got != tt.want {
				t.Errorf("PhaseStallStreak = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestIterationCapFor_PerBand pins the per-band caps.
func TestIterationCapFor_PerBand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		band FeasibilityBand
		want int
	}{
		{BandEasy, 4},
		{BandMedium, 8},
		{BandHard, 12},
		{BandBail, 4}, // bail never deploys; conservative floor
		{FeasibilityBand("unknown"), 4},
	}
	for _, tt := range tests {
		if got := IterationCapFor(tt.band); got != tt.want {
			t.Errorf("IterationCapFor(%s) = %d, want %d", tt.band, got, tt.want)
		}
	}
}

// TestWallBudgetFor_PerBand pins the wall budgets (EASY 45m / HARD 90m; MEDIUM
// interpolated).
func TestWallBudgetFor_PerBand(t *testing.T) {
	t.Parallel()
	if got := WallBudgetFor(BandEasy); got != 45*time.Minute {
		t.Errorf("WallBudgetFor(easy) = %v, want 45m", got)
	}
	if got := WallBudgetFor(BandHard); got != 90*time.Minute {
		t.Errorf("WallBudgetFor(hard) = %v, want 90m", got)
	}
	if got := WallBudgetFor(BandMedium); got < 45*time.Minute || got > 90*time.Minute {
		t.Errorf("WallBudgetFor(medium) = %v, want between 45m and 90m", got)
	}
}

// TestEvaluatePortProgress_IterationCap fires when iteration count reaches the
// band cap and reports the cap terminator.
func TestEvaluatePortProgress_IterationCap(t *testing.T) {
	t.Parallel()
	ps := &PortSession{
		Plan:      PortPlan{Band: BandEasy},
		Iteration: 4,
		Attempts: []PortAttempt{
			fail(topology.FailureClassBuild, FixAddPrepareCommands),
			fail(topology.FailureClassStart, FixWireDepRefs),
			fail(topology.FailureClassConfig, FixGlueYAML),
			fail(topology.FailureClassVerify, FixInspect),
		},
	}
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	res := EvaluatePortProgress(ps, now)
	if !res.Stop {
		t.Fatalf("expected Stop at iteration cap, got %+v", res)
	}
	if res.Terminator != PortTermIterationCap {
		t.Errorf("Terminator = %v, want %v", res.Terminator, PortTermIterationCap)
	}
}

// TestEvaluatePortProgress_RebudgetOnEscalation: a fresh T1 escalation resets
// the effective cap so a late strategy switch is not starved.
func TestEvaluatePortProgress_RebudgetOnEscalation(t *testing.T) {
	t.Parallel()
	// EASY cap = 4. Mark the session as re-budgeted from iteration 3 (a T1
	// escalation fired then), so the cap is measured from the re-budget origin:
	// at iteration 4 only 1 iteration has elapsed in the fresh sub-budget.
	ps := &PortSession{
		Plan:           PortPlan{Band: BandEasy},
		Iteration:      4,
		RebudgetOrigin: 3,
		Attempts: []PortAttempt{
			fail(topology.FailureClassBuild, FixAddPrepareCommands),
			fail(topology.FailureClassBuild, FixInspect),
			fail(topology.FailureClassBuild, FixInspect),
			fail(topology.FailureClassStart, FixWireDepRefs),
		},
	}
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	res := EvaluatePortProgress(ps, now)
	if res.Stop && res.Terminator == PortTermIterationCap {
		t.Errorf("re-budgeted session should NOT hit iteration cap at iteration 4 (origin 3): %+v", res)
	}
}

// TestEvaluatePortProgress_WallBudget fires when the latest attempt's RecordedAt
// is past the monotonic deadline from session start.
func TestEvaluatePortProgress_WallBudget(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	// EASY budget = 45m. Latest attempt recorded 50m after start.
	late := start.Add(50 * time.Minute)
	ps := &PortSession{
		Plan:      PortPlan{Band: BandEasy},
		StartTime: start.Format(time.RFC3339),
		Iteration: 2,
		Attempts: []PortAttempt{
			{Class: topology.FailureClassBuild, FixKind: FixAddPrepareCommands, RecordedAt: start.Add(5 * time.Minute).Format(time.RFC3339)},
			{Class: topology.FailureClassStart, FixKind: FixWireDepRefs, RecordedAt: late.Format(time.RFC3339)},
		},
	}
	res := EvaluatePortProgress(ps, late)
	if !res.Stop {
		t.Fatalf("expected Stop at wall budget, got %+v", res)
	}
	if res.Terminator != PortTermWallBudget {
		t.Errorf("Terminator = %v, want %v", res.Terminator, PortTermWallBudget)
	}
}

// TestEvaluatePortProgress_WithinBudget_Continues: a healthy mid-loop session
// with advancing phases and time/iterations left does not stop.
func TestEvaluatePortProgress_WithinBudget_Continues(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	ps := &PortSession{
		Plan:      PortPlan{Band: BandMedium},
		StartTime: start.Format(time.RFC3339),
		Iteration: 2,
		Attempts: []PortAttempt{
			{Class: topology.FailureClassBuild, FixKind: FixAddPrepareCommands, RecordedAt: start.Add(2 * time.Minute).Format(time.RFC3339)},
			{Class: topology.FailureClassStart, FixKind: FixWireDepRefs, RecordedAt: start.Add(6 * time.Minute).Format(time.RFC3339)},
		},
	}
	res := EvaluatePortProgress(ps, start.Add(7*time.Minute))
	if res.Stop {
		t.Errorf("healthy session should continue, got Stop %+v", res)
	}
}
