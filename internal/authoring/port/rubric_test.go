package port

import (
	"strings"
	"testing"
)

// TestC1Builds_GradesFromTerminalAndWarnings pins the build gate grading.
func TestC1Builds_GradesFromTerminalAndWarnings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		succeeded bool
		warnings  bool
		want      int
	}{
		{"terminal success no warnings", true, false, 2},
		{"terminal success with warnings", true, true, 1},
		{"build failed", false, false, 0},
		{"build failed with warnings still 0", false, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := C1Builds(tt.succeeded, tt.warnings).Grade; got != tt.want {
				t.Errorf("C1Builds = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestC2BootsStable_ActiveThenExitGrades1 is the §7 stability-hold false-positive
// guard: ACTIVE-then-exit / crash-loop grades 1, stable past the hold grades 2.
func TestC2BootsStable_ActiveThenExitGrades1(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		reachedActive   bool
		stableAfterHold bool
		want            int
	}{
		{"stable past hold", true, true, 2},
		{"active then exit / crash-loop", true, false, 1},
		{"never booted", false, false, 0},
		{"never booted but flagged stable (impossible) still 0", false, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := C2BootsStable(tt.reachedActive, tt.stableAfterHold).Grade; got != tt.want {
				t.Errorf("C2BootsStable = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestC3ServesHTTP_FromVerify pins the HTTP-serve grade.
func TestC3ServesHTTP_FromVerify(t *testing.T) {
	t.Parallel()
	if got := C3ServesHTTP(true).Grade; got != 1 {
		t.Errorf("C3 pass = %d, want 1", got)
	}
	if got := C3ServesHTTP(false).Grade; got != 0 {
		t.Errorf("C3 fail = %d, want 0", got)
	}
}

// TestC4CoreFlow_FromProbe pins the agent-authored core-flow probe grade.
func TestC4CoreFlow_FromProbe(t *testing.T) {
	t.Parallel()
	if got := C4CoreFlow(true).Grade; got != 1 {
		t.Errorf("C4 pass = %d, want 1", got)
	}
	if got := C4CoreFlow(false).Grade; got != 0 {
		t.Errorf("C4 fail = %d, want 0", got)
	}
}

// TestC5Persists_DurableVsEphemeral pins that ephemeral-only survival grades 1.
func TestC5Persists_DurableVsEphemeral(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		survived bool
		durable  bool
		want     int
	}{
		{"survived on durable surface", true, true, 2},
		{"survived only on ephemeral FS", true, false, 1},
		{"did not survive", false, false, 0},
		{"did not survive even if durable", false, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := C5Persists(tt.survived, tt.durable).Grade; got != tt.want {
				t.Errorf("C5Persists = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestC6HACapable_ThroughputVsHA is the MEMORY invariant: scaling for throughput
// (grade 1) is DISTINCT from HA replication (grade 2 — managed-HA + verify).
func TestC6HACapable_ThroughputVsHA(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		appContainers int
		managedDepsHA bool
		haVerify      bool
		want          int
	}{
		{"HA replication", 2, true, true, 2},
		{"throughput only — no managed HA", 2, false, true, 1},
		{"throughput only — managed HA but no verify", 2, true, false, 1},
		{"single container", 1, true, true, 0},
		{"zero containers", 0, false, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := C6HACapable(tt.appContainers, tt.managedDepsHA, tt.haVerify).Grade; got != tt.want {
				t.Errorf("C6HACapable = %d, want %d", got, tt.want)
			}
		})
	}
}

// rubricFrom builds a PortRubric from C-grade ints (0 = absent/zero).
func rubricFrom(c1, c2, c3, c4, c5, c6 int) PortRubric {
	return PortRubric{Grades: []PortGrade{
		{Check: PortCheckBuilds, Grade: c1},
		{Check: PortCheckBoots, Grade: c2},
		{Check: PortCheckServes, Grade: c3},
		{Check: PortCheckCoreFlow, Grade: c4},
		{Check: PortCheckPersists, Grade: c5},
		{Check: PortCheckHA, Grade: c6},
	}}
}

func honoredMap(tiers []HonoredTier) map[PortTierLevel]HonoredTier {
	m := make(map[PortTierLevel]HonoredTier, len(tiers))
	for _, t := range tiers {
		m[t.Level] = t
	}
	return m
}

// TestRollUpHonoredTiers_Infeasible: C1=C2=C3=0 honors NO tier.
func TestRollUpHonoredTiers_Infeasible(t *testing.T) {
	t.Parallel()
	tiers := RollUpHonoredTiers(rubricFrom(0, 0, 0, 0, 0, 0))
	for _, ht := range tiers {
		if ht.Honored {
			t.Errorf("tier %d honored on infeasible rubric", ht.Level)
		}
		if ht.Reason == "" {
			t.Errorf("excluded tier %d missing a reason", ht.Level)
		}
	}
	if _, ok := HighestHonoredTier(rubricFrom(0, 0, 0, 0, 0, 0)); ok {
		t.Errorf("HighestHonoredTier ok=true on infeasible rubric")
	}
}

// TestRollUpHonoredTiers_GateOnly: C1/C2/C3≥1 honors tiers 0/1 only.
func TestRollUpHonoredTiers_GateOnly(t *testing.T) {
	t.Parallel()
	m := honoredMap(RollUpHonoredTiers(rubricFrom(2, 2, 1, 0, 0, 0)))
	for _, lvl := range []PortTierLevel{PortTierAIAgent, PortTierRemote} {
		if !m[lvl].Honored {
			t.Errorf("tier %d should be honored (gate met)", lvl)
		}
	}
	for _, lvl := range []PortTierLevel{PortTierLocal, PortTierStage, PortTierSmallProd, PortTierHAProd} {
		if m[lvl].Honored {
			t.Errorf("tier %d should NOT be honored without C4", lvl)
		}
		if m[lvl].Reason == "" {
			t.Errorf("excluded tier %d missing reason", lvl)
		}
	}
	if lvl, ok := HighestHonoredTier(rubricFrom(2, 2, 1, 0, 0, 0)); !ok || lvl != PortTierRemote {
		t.Errorf("highest = %d ok=%v, want %d true", lvl, ok, PortTierRemote)
	}
}

// TestRollUpHonoredTiers_CoreFlowHonorsLocalStage: +C4 honors tiers 2/3.
func TestRollUpHonoredTiers_CoreFlowHonorsLocalStage(t *testing.T) {
	t.Parallel()
	m := honoredMap(RollUpHonoredTiers(rubricFrom(2, 2, 1, 1, 0, 0)))
	for _, lvl := range []PortTierLevel{PortTierLocal, PortTierStage} {
		if !m[lvl].Honored {
			t.Errorf("tier %d should be honored with C4", lvl)
		}
	}
	if m[PortTierSmallProd].Honored {
		t.Errorf("small-prod must not be honored without C5=2")
	}
}

// TestRollUpHonoredTiers_C5C6Contract is the §7 PLACEMENT-break contract case:
// C5=2, C6=1 ⇒ tiers 0-4 honored, tier 5 NOT honored with the throughput-vs-HA
// reason. A higher C5 grade does NOT honor tier 5 whose own prerequisite (C6=2)
// is unmet.
func TestRollUpHonoredTiers_C5C6Contract(t *testing.T) {
	t.Parallel()
	m := honoredMap(RollUpHonoredTiers(rubricFrom(2, 2, 1, 1, 2, 1)))
	for _, lvl := range []PortTierLevel{
		PortTierAIAgent, PortTierRemote, PortTierLocal, PortTierStage, PortTierSmallProd,
	} {
		if !m[lvl].Honored {
			t.Errorf("tier %d should be honored (C5=2)", lvl)
		}
	}
	ha := m[PortTierHAProd]
	if ha.Honored {
		t.Errorf("tier 5 must NOT be honored with C6=1 (throughput only)")
	}
	if ha.Reason == "" {
		t.Errorf("tier 5 exclusion must carry a reason")
	}
	if lvl, ok := HighestHonoredTier(rubricFrom(2, 2, 1, 1, 2, 1)); !ok || lvl != PortTierSmallProd {
		t.Errorf("highest = %d ok=%v, want small-prod", lvl, ok)
	}
}

// TestRollUpHonoredTiers_FullHA: C5=2,C6=2 honors all six tiers.
func TestRollUpHonoredTiers_FullHA(t *testing.T) {
	t.Parallel()
	m := honoredMap(RollUpHonoredTiers(rubricFrom(2, 2, 1, 1, 2, 2)))
	for _, ht := range m {
		if !ht.Honored {
			t.Errorf("tier %d should be honored on a full-HA rubric", ht.Level)
		}
	}
	if lvl, ok := HighestHonoredTier(rubricFrom(2, 2, 1, 1, 2, 2)); !ok || lvl != PortTierHAProd {
		t.Errorf("highest = %d ok=%v, want ha-prod", lvl, ok)
	}
}

// TestRollUpHonoredTiers_C2CrashLoopFailsGate — a C2=1 grade is the §7 false-
// positive (ACTIVE-then-exit / crash-loop, booted but NOT stable). A crash-
// looping deploy must honor NO tier: gating it would ship a guide for a process
// that does not stay up. Only C2==2 (stable past the hold) clears the gate.
func TestRollUpHonoredTiers_C2CrashLoopFailsGate(t *testing.T) {
	t.Parallel()
	// C2=1 (crash-loop) must NOT clear the gate — distinct failure scenario from
	// the old (wrong) "it booted, good enough" behavior.
	m1 := honoredMap(RollUpHonoredTiers(rubricFrom(2, 1, 1, 0, 0, 0)))
	if m1[PortTierAIAgent].Honored {
		t.Errorf("C2=1 (crash-loop) must NOT clear the boots-STABLE gate")
	}
	if !strings.Contains(m1[PortTierAIAgent].Reason, "crash-looped") {
		t.Errorf("C2=1 gate-unmet reason must name the crash-loop, got %q", m1[PortTierAIAgent].Reason)
	}
	// C2=0 (never booted) also fails the gate, with its own reason.
	m0 := honoredMap(RollUpHonoredTiers(rubricFrom(2, 0, 1, 0, 0, 0)))
	if m0[PortTierAIAgent].Honored {
		t.Errorf("C2=0 must fail the gate")
	}
	if !strings.Contains(m0[PortTierAIAgent].Reason, "never reached ACTIVE") {
		t.Errorf("C2=0 gate-unmet reason must name never-ACTIVE, got %q", m0[PortTierAIAgent].Reason)
	}
	// C2=2 (stable) DOES clear the gate.
	m2 := honoredMap(RollUpHonoredTiers(rubricFrom(2, 2, 1, 0, 0, 0)))
	if !m2[PortTierAIAgent].Honored {
		t.Errorf("C2=2 (boots stable) must clear the gate")
	}
}
