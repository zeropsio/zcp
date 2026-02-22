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
		{Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
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

// --- Bootstrap conductor engine tests ---

func TestEngine_BootstrapStart_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	resp, err := eng.BootstrapStart("proj-1", ModeFull, "bun + postgres")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	if resp.SessionID == "" {
		t.Error("expected non-empty SessionID")
	}
	if resp.Mode != ModeFull {
		t.Errorf("Mode: want full, got %s", resp.Mode)
	}
	if resp.Intent != "bun + postgres" {
		t.Errorf("Intent mismatch")
	}
	if resp.Progress.Total != 10 {
		t.Errorf("Total: want 10, got %d", resp.Progress.Total)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.Name != "detect" {
		t.Errorf("Current.Name: want detect, got %s", resp.Current.Name)
	}

	// Verify bootstrap state is persisted.
	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap == nil {
		t.Fatal("Bootstrap state should be set")
	}
	if !state.Bootstrap.Active {
		t.Error("Bootstrap should be active")
	}
}

func TestEngine_BootstrapStart_ExistingSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", ModeFull, "first"); err != nil {
		t.Fatalf("first BootstrapStart: %v", err)
	}

	_, err := eng.BootstrapStart("proj-1", ModeFull, "second")
	if err == nil {
		t.Fatal("expected error for second BootstrapStart")
	}
}

func TestEngine_BootstrapComplete_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", ModeFull, "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	resp, err := eng.BootstrapComplete("detect", "FRESH project, no services found")
	if err != nil {
		t.Fatalf("BootstrapComplete: %v", err)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil after first complete")
	}
	if resp.Current.Name != "plan" {
		t.Errorf("Current.Name: want plan, got %s", resp.Current.Name)
	}
	if resp.Progress.Completed != 1 {
		t.Errorf("Completed: want 1, got %d", resp.Progress.Completed)
	}
}

func TestEngine_BootstrapComplete_FullSequence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", ModeFull, "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	steps := []string{
		"detect", "plan", "load-knowledge", "generate-import",
		"import-services", "mount-dev", "discover-envs",
		"deploy", "verify", "report",
	}
	var resp *BootstrapResponse
	for _, step := range steps {
		var err error
		resp, err = eng.BootstrapComplete(step, "Attestation for "+step+" completed ok")
		if err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// After all steps: bootstrap done.
	if resp.Current != nil {
		t.Error("Current should be nil after all steps")
	}
	if resp.Progress.Completed != 10 {
		t.Errorf("Completed: want 10, got %d", resp.Progress.Completed)
	}

	// Phase should be DONE.
	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Phase != PhaseDone {
		t.Errorf("Phase: want DONE, got %s", state.Phase)
	}
	if state.Bootstrap.Active {
		t.Error("Bootstrap should be inactive after completion")
	}
}

func TestEngine_BootstrapComplete_AutoEvidence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", ModeFull, "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	steps := []string{
		"detect", "plan", "load-knowledge", "generate-import",
		"import-services", "mount-dev", "discover-envs",
		"deploy", "verify", "report",
	}
	for _, step := range steps {
		if _, err := eng.BootstrapComplete(step, "Attestation for "+step+" completed ok"); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// Verify evidence files exist for all 5 types.
	state, _ := eng.GetState()
	evidenceTypes := []string{"recipe_review", "discovery", "dev_verify", "deploy_evidence", "stage_verify"}
	for _, et := range evidenceTypes {
		ev, err := LoadEvidence(eng.evidenceDir, state.SessionID, et)
		if err != nil {
			t.Errorf("missing evidence %q: %v", et, err)
			continue
		}
		if ev.Attestation == "" {
			t.Errorf("evidence %q has empty attestation", et)
		}
	}
}

func TestEngine_BootstrapComplete_AutoTransition(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", ModeFull, "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	steps := []string{
		"detect", "plan", "load-knowledge", "generate-import",
		"import-services", "mount-dev", "discover-envs",
		"deploy", "verify", "report",
	}
	for _, step := range steps {
		if _, err := eng.BootstrapComplete(step, "Attestation for "+step+" completed ok"); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Phase != PhaseDone {
		t.Errorf("Phase: want DONE, got %s", state.Phase)
	}
	// Full mode: INIT→DISCOVER→DEVELOP→DEPLOY→VERIFY→DONE = 5 transitions.
	if len(state.History) != 5 {
		t.Errorf("History length: want 5, got %d", len(state.History))
	}
}

func TestEngine_BootstrapSkip_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", ModeFull, "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Complete up to mount-dev (steps 0-4).
	preSteps := []string{"detect", "plan", "load-knowledge", "generate-import", "import-services"}
	for _, step := range preSteps {
		if _, err := eng.BootstrapComplete(step, "Attestation for "+step+" completed ok"); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// Skip mount-dev.
	resp, err := eng.BootstrapSkip("mount-dev", "no runtime services")
	if err != nil {
		t.Fatalf("BootstrapSkip: %v", err)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.Name != "discover-envs" {
		t.Errorf("Current.Name: want discover-envs, got %s", resp.Current.Name)
	}
}

func TestEngine_BootstrapSkip_Mandatory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", ModeFull, "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Skip plan (mandatory) — should fail.
	// First complete detect so plan is current.
	if _, err := eng.BootstrapComplete("detect", "FRESH project detected ok"); err != nil {
		t.Fatalf("BootstrapComplete: %v", err)
	}

	_, err := eng.BootstrapSkip("plan", "skip reason")
	if err == nil {
		t.Fatal("expected error skipping mandatory step 'plan'")
	}
}

func TestEngine_BootstrapStatus_Active(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", ModeFull, "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Complete 3 steps.
	steps := []string{"detect", "plan", "load-knowledge"}
	for _, step := range steps {
		if _, err := eng.BootstrapComplete(step, "Attestation for "+step+" completed ok"); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	resp, err := eng.BootstrapStatus()
	if err != nil {
		t.Fatalf("BootstrapStatus: %v", err)
	}
	if resp.Progress.Completed != 3 {
		t.Errorf("Completed: want 3, got %d", resp.Progress.Completed)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.Name != "generate-import" {
		t.Errorf("Current.Name: want generate-import, got %s", resp.Current.Name)
	}
}

func TestEngine_BootstrapStatus_WithAttestations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", ModeFull, "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	if _, err := eng.BootstrapComplete("detect", "FRESH project, no services"); err != nil {
		t.Fatalf("BootstrapComplete: %v", err)
	}

	resp, err := eng.BootstrapStatus()
	if err != nil {
		t.Fatalf("BootstrapStatus: %v", err)
	}

	// Verify first step shows as complete in summary.
	if len(resp.Progress.Steps) != 10 {
		t.Fatalf("Steps count: want 10, got %d", len(resp.Progress.Steps))
	}
	if resp.Progress.Steps[0].Status != "complete" {
		t.Errorf("step[0].Status: want complete, got %s", resp.Progress.Steps[0].Status)
	}
}

func TestEngine_BootstrapStatus_NoSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	_, err := eng.BootstrapStatus()
	if err == nil {
		t.Fatal("expected error for status without session")
	}
}
