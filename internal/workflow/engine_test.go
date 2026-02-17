// Tests for: workflow engine — orchestration, project state detection, transitions.
package workflow

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestDetectProjectState_Fresh(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		services []platform.ServiceStack
		want     ProjectState
	}{
		{
			"no_services",
			nil,
			StateFresh,
		},
		{
			"only_managed_services",
			[]platform.ServiceStack{
				{Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
				{Name: "cache", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "valkey@8"}},
			},
			StateFresh,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().WithServices(tt.services)
			got, err := DetectProjectState(context.Background(), client, "proj-1")
			if err != nil {
				t.Fatalf("DetectProjectState: %v", err)
			}
			if got != tt.want {
				t.Errorf("state: want %s, got %s", tt.want, got)
			}
		})
	}
}

func TestDetectProjectState_Conformant(t *testing.T) {
	t.Parallel()
	services := []platform.ServiceStack{
		{Name: "app-dev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "app-stage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
	}
	client := platform.NewMock().WithServices(services)
	got, err := DetectProjectState(context.Background(), client, "proj-2")
	if err != nil {
		t.Fatalf("DetectProjectState: %v", err)
	}
	if got != StateConformant {
		t.Errorf("state: want CONFORMANT, got %s", got)
	}
}

func TestDetectProjectState_NonConformant(t *testing.T) {
	t.Parallel()
	services := []platform.ServiceStack{
		{Name: "myapp", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
	}
	client := platform.NewMock().WithServices(services)
	got, err := DetectProjectState(context.Background(), client, "proj-3")
	if err != nil {
		t.Fatalf("DetectProjectState: %v", err)
	}
	if got != StateNonConformant {
		t.Errorf("state: want NON_CONFORMANT, got %s", got)
	}
}

func TestEngine_StartAndTransition(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	state, err := eng.Start("proj-1", ModeFull, "full workflow")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if state.Phase != PhaseInit {
		t.Errorf("Phase: want INIT, got %s", state.Phase)
	}

	// Record recipe_review evidence so G0 passes.
	ev := &Evidence{
		SessionID: state.SessionID, Type: "recipe_review", VerificationType: "attestation",
	}
	if err := eng.RecordEvidence(ev); err != nil {
		t.Fatalf("RecordEvidence: %v", err)
	}

	// Transition to DISCOVER.
	state, err = eng.Transition(PhaseDiscover)
	if err != nil {
		t.Fatalf("Transition to DISCOVER: %v", err)
	}
	if state.Phase != PhaseDiscover {
		t.Errorf("Phase: want DISCOVER, got %s", state.Phase)
	}
	if len(state.History) != 1 {
		t.Errorf("History length: want 1, got %d", len(state.History))
	}
}

func TestEngine_Transition_InvalidPhase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.Start("proj-1", ModeFull, "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Skip a phase — should fail.
	_, err := eng.Transition(PhaseDeploy)
	if err == nil {
		t.Fatal("expected error for invalid transition INIT → DEPLOY")
	}
}

func TestEngine_Transition_GateFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.Start("proj-1", ModeFull, "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Try to transition without evidence — gate should block.
	_, err := eng.Transition(PhaseDiscover)
	if err == nil {
		t.Fatal("expected error when gate check fails (no recipe_review evidence)")
	}
}

func TestEngine_Reset(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.Start("proj-1", ModeFull, "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := eng.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, err := eng.GetState(); err == nil {
		t.Fatal("expected error getting state after reset")
	}
}

func TestEngine_Iterate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.Start("proj-1", ModeFull, "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	state, err := eng.Iterate()
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if state.Iteration != 1 {
		t.Errorf("Iteration: want 1, got %d", state.Iteration)
	}
	if state.Phase != PhaseDevelop {
		t.Errorf("Phase: want DEVELOP, got %s", state.Phase)
	}
}

func TestEngine_GetState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.Start("proj-1", ModeFull, "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.ProjectID != "proj-1" {
		t.Errorf("ProjectID: want proj-1, got %s", state.ProjectID)
	}
}
