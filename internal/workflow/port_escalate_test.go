package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestDecidePortEscalation_T1OnlyWithBuildStallAndPrebuilt: T1 (source-build →
// prebuilt-binary) fires ONLY when classStallStreak>=3 on FailureClassBuild AND
// a prebuilt URL is available; otherwise T2 bail.
func TestDecidePortEscalation_T1OnlyWithBuildStallAndPrebuilt(t *testing.T) {
	t.Parallel()
	buildStall3 := []PortAttempt{
		fail(topology.FailureClassBuild, FixAddPrepareCommands, "build:command-not-found"),
		fail(topology.FailureClassBuild, FixInspect, "build:missing-header"),
		fail(topology.FailureClassBuild, FixInspect, "build:linker-error"),
	}

	tests := []struct {
		name     string
		plan     PortPlan
		attempts []PortAttempt
		wantTier PortEscalationTier
		wantStr  AcquisitionStrategy
	}{
		{
			name:     "build stall 3 + prebuilt URL present -> T1 escalate to prebuilt-binary",
			plan:     PortPlan{Band: BandEasy, Acquisition: AcquireSourceBuild, PrebuiltURL: "https://example.com/app.tar.gz"},
			attempts: buildStall3,
			wantTier: PortEscalateT1,
			wantStr:  AcquirePrebuiltBinary,
		},
		{
			name:     "build stall 3 + NO prebuilt URL -> T2 bail",
			plan:     PortPlan{Band: BandEasy, Acquisition: AcquireSourceBuild},
			attempts: buildStall3,
			wantTier: PortEscalateT2Bail,
		},
		{
			name: "build stall only 2 -> T0 stay",
			plan: PortPlan{Band: BandEasy, Acquisition: AcquireSourceBuild, PrebuiltURL: "https://example.com/app.tar.gz"},
			attempts: []PortAttempt{
				fail(topology.FailureClassBuild, FixAddPrepareCommands, "build:command-not-found"),
				fail(topology.FailureClassBuild, FixInspect, "build:missing-header"),
			},
			wantTier: PortEscalateT0Stay,
		},
		{
			name: "already on prebuilt-binary -> no further strategy escalation (T2)",
			plan: PortPlan{Band: BandEasy, Acquisition: AcquirePrebuiltBinary, PrebuiltURL: "https://example.com/app.tar.gz"},
			attempts: []PortAttempt{
				fail(topology.FailureClassBuild, FixInspect, "build:x"),
				fail(topology.FailureClassBuild, FixInspect, "build:y"),
				fail(topology.FailureClassBuild, FixInspect, "build:z"),
			},
			wantTier: PortEscalateT2Bail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dec := DecidePortEscalation(tt.plan, tt.attempts)
			if dec.Tier != tt.wantTier {
				t.Fatalf("Tier = %v, want %v (dec=%+v)", dec.Tier, tt.wantTier, dec)
			}
			if tt.wantTier == PortEscalateT1 && dec.NewStrategy != tt.wantStr {
				t.Errorf("NewStrategy = %v, want %v", dec.NewStrategy, tt.wantStr)
			}
		})
	}
}

// TestDecidePortEscalation_EscalateFlagBails: an attempt that already flagged
// Escalate (e.g. build OOM) with no prebuilt fallback bails (T2).
func TestDecidePortEscalation_EscalateFlagBails(t *testing.T) {
	t.Parallel()
	attempts := []PortAttempt{
		{Class: topology.FailureClassBuild, FixKind: FixEscalate, Escalate: true, Signals: []string{"build:oom-killed"}},
	}
	dec := DecidePortEscalation(PortPlan{Band: BandEasy, Acquisition: AcquireSourceBuild}, attempts)
	if dec.Tier != PortEscalateT2Bail {
		t.Errorf("escalate-flagged OOM with no prebuilt should bail, got %v", dec.Tier)
	}
}

// TestDecidePortEscalation_OOMWithPrebuilt_EscalatesFirst: a build-OOM escalate
// flag with a prebuilt URL available should try the prebuilt strategy (T1)
// before bailing — the strategy switch may dodge the build entirely.
func TestDecidePortEscalation_OOMWithPrebuilt_EscalatesFirst(t *testing.T) {
	t.Parallel()
	attempts := []PortAttempt{
		{Class: topology.FailureClassBuild, FixKind: FixEscalate, Escalate: true, Signals: []string{"build:oom-killed"}},
	}
	dec := DecidePortEscalation(PortPlan{Band: BandEasy, Acquisition: AcquireSourceBuild, PrebuiltURL: "https://x/app.tgz"}, attempts)
	if dec.Tier != PortEscalateT1 || dec.NewStrategy != AcquirePrebuiltBinary {
		t.Errorf("OOM + prebuilt should T1-escalate to prebuilt, got tier=%v str=%v", dec.Tier, dec.NewStrategy)
	}
}

// TestDecidePortEscalation_CredentialNeverEscalates: credential/network classes
// never escalate the acquisition strategy (T0 stay).
func TestDecidePortEscalation_CredentialNeverEscalates(t *testing.T) {
	t.Parallel()
	for _, class := range []topology.FailureClass{topology.FailureClassCredential, topology.FailureClassNetwork} {
		attempts := []PortAttempt{
			fail(class, FixGitPushSetup),
			fail(class, FixGitPushSetup),
			fail(class, FixGitPushSetup),
			fail(class, FixGitPushSetup),
		}
		dec := DecidePortEscalation(PortPlan{Band: BandEasy, Acquisition: AcquireSourceBuild, PrebuiltURL: "https://x/app.tgz"}, attempts)
		if dec.Tier != PortEscalateT0Stay {
			t.Errorf("class %s should never escalate strategy, got tier %v", class, dec.Tier)
		}
	}
}

// TestDecidePortEscalation_PostEscalationPhaseStallBails: after a strategy
// switch the phase stalls again -> T2 bail.
func TestDecidePortEscalation_PostEscalationPhaseStallBails(t *testing.T) {
	t.Parallel()
	// Already on prebuilt-binary (escalated earlier), now stuck at serve.
	attempts := []PortAttempt{
		fail(topology.FailureClassConfig, FixGlueYAML),
		fail(topology.FailureClassConfig, FixGlueYAML),
		fail(topology.FailureClassConfig, FixGlueYAML),
	}
	dec := DecidePortEscalation(PortPlan{Band: BandEasy, Acquisition: AcquirePrebuiltBinary, PrebuiltURL: "https://x/app.tgz"}, attempts)
	if dec.Tier != PortEscalateT2Bail {
		t.Errorf("post-escalation phase stall should bail, got %v", dec.Tier)
	}
}

// TestDecidePortEscalation_NewClassWithBudget_Stays: a new failure class with
// budget left -> T0 stay (continue with the next fix).
func TestDecidePortEscalation_NewClassWithBudget_Stays(t *testing.T) {
	t.Parallel()
	attempts := []PortAttempt{
		fail(topology.FailureClassBuild, FixAddPrepareCommands, "build:command-not-found"),
		fail(topology.FailureClassStart, FixWireDepRefs, "init:db-connection-refused"),
	}
	dec := DecidePortEscalation(PortPlan{Band: BandEasy, Acquisition: AcquireSourceBuild}, attempts)
	if dec.Tier != PortEscalateT0Stay {
		t.Errorf("new class with budget should stay, got %v", dec.Tier)
	}
}
