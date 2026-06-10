package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestBuildPortActiveRecovery_FromSession(t *testing.T) {
	t.Parallel()

	ps := &PortSession{
		Version: portSessionVersion,
		PID:     1234,
		Plan: PortPlan{
			Target:      "strapi",
			Acquisition: AcquireSourceBuild,
			Band:        BandEasy,
		},
	}
	ps.RecordPortAttempt(PortAttempt{
		Class:   topology.FailureClassStart,
		Signals: []string{"init:db-connection-refused"},
		FixKind: FixWireDepRefs,
	})

	env := BuildPortActiveRecovery(ps)
	if env.Kind != "port-active" {
		t.Errorf("Kind: got %q", env.Kind)
	}
	if env.Workflow != WorkflowPort {
		t.Errorf("Workflow: got %q", env.Workflow)
	}
	if env.Phase != PhasePortActive {
		t.Errorf("Phase: got %q", env.Phase)
	}
	if env.Target != "strapi" {
		t.Errorf("Target: got %q", env.Target)
	}
	if env.Band != BandEasy {
		t.Errorf("Band: got %q", env.Band)
	}
	if env.Iteration != 1 {
		t.Errorf("Iteration: got %d", env.Iteration)
	}
	if env.LastAttempt == nil || env.LastAttempt.FixKind != FixWireDepRefs {
		t.Fatalf("LastAttempt not surfaced: %+v", env.LastAttempt)
	}
	if env.NextCall == "" || env.Guidance == "" {
		t.Errorf("recovery envelope must carry NextCall + Guidance")
	}
}

func TestBuildPortActiveRecovery_NoAttempts_FreshRecon(t *testing.T) {
	t.Parallel()

	ps := &PortSession{
		Plan: PortPlan{Target: "ghost", Acquisition: AcquireSourceBuild, Band: BandEasy},
	}
	env := BuildPortActiveRecovery(ps)
	if env.LastAttempt != nil {
		t.Errorf("fresh recon must have no LastAttempt")
	}
	if env.Iteration != 0 {
		t.Errorf("fresh recon iteration: got %d", env.Iteration)
	}
	if env.Guidance == "" {
		t.Errorf("fresh recon must still carry guidance")
	}
}

func TestRecordPortAttempt_BumpsIteration(t *testing.T) {
	t.Parallel()

	ps := &PortSession{}
	a1 := ps.RecordPortAttempt(PortAttempt{Class: topology.FailureClassBuild})
	a2 := ps.RecordPortAttempt(PortAttempt{Class: topology.FailureClassStart})
	if a1.Iteration != 1 || a2.Iteration != 2 {
		t.Fatalf("iterations: a1=%d a2=%d want 1,2", a1.Iteration, a2.Iteration)
	}
	if ps.Iteration != 2 || len(ps.Attempts) != 2 {
		t.Fatalf("session iteration=%d attempts=%d", ps.Iteration, len(ps.Attempts))
	}
}
