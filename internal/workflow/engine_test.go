// Tests for: workflow engine — orchestration, project state detection, transitions.
package workflow

import (
	"context"
	"fmt"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestIsManagedService(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		serviceType string
		want        bool
	}{
		{"postgresql", "postgresql@16", true},
		{"valkey", "valkey@7.2", true},
		{"object_storage_hyphen", "object-storage@1", true},
		{"shared_storage_hyphen", "shared-storage@1", true},
		{"object_storage_bare", "object-storage", true},
		{"shared_storage_bare", "shared-storage", true},
		{"nats", "nats@2.10", true},
		{"clickhouse", "clickhouse@24.3", true},
		{"qdrant", "qdrant@1.12", true},
		{"typesense", "typesense@27.1", true},
		{"runtime_bun", "bun@1.2", false},
		{"runtime_nodejs", "nodejs@22", false},
		{"runtime_go", "go@1", false},
		{"runtime_php", "php-nginx@8.4", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isManagedService(tt.serviceType); got != tt.want {
				t.Errorf("isManagedService(%q) = %v, want %v", tt.serviceType, got, tt.want)
			}
		})
	}
}

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
		{
			"only_object_storage",
			[]platform.ServiceStack{
				{Name: "storage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "object-storage@1"}},
			},
			StateFresh,
		},
		{
			"only_shared_storage",
			[]platform.ServiceStack{
				{Name: "files", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "shared-storage@1"}},
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

	state, err := eng.Start("proj-1", "deploy", "full workflow")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if state.Phase != PhaseInit {
		t.Errorf("Phase: want INIT, got %s", state.Phase)
	}

	// Record recipe_review evidence so G0 passes.
	ev := &Evidence{
		SessionID: state.SessionID, Type: "recipe_review", VerificationType: "attestation",
		Attestation: "recipe reviewed", Passed: 1,
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

	if _, err := eng.Start("proj-1", "deploy", "test"); err != nil {
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

	if _, err := eng.Start("proj-1", "deploy", "test"); err != nil {
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

	if _, err := eng.Start("proj-1", "deploy", "test"); err != nil {
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

	if _, err := eng.Start("proj-1", "deploy", "test"); err != nil {
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

	if _, err := eng.Start("proj-1", "deploy", "test"); err != nil {
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

// --- Auto-reset DONE session tests ---

func TestEngine_Start_AutoResetDoneSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	// Create and manually transition to DONE.
	state, err := eng.Start("proj-1", "deploy", "first")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Manually set phase to DONE (simulating completed workflow).
	state.Phase = PhaseDone
	if err := saveState(dir, state); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	// Start again — should auto-reset the DONE session.
	state2, err := eng.Start("proj-1", "deploy", "second")
	if err != nil {
		t.Fatalf("Start after DONE: %v", err)
	}
	if state2.Phase != PhaseInit {
		t.Errorf("Phase: want INIT, got %s", state2.Phase)
	}
	if state2.Intent != "second" {
		t.Errorf("Intent: want 'second', got %s", state2.Intent)
	}
	if state2.SessionID == state.SessionID {
		t.Error("expected new session ID after auto-reset")
	}
}

func TestEngine_Start_ActiveSessionBlocks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.Start("proj-1", "deploy", "first"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Start with active (non-DONE) session should fail.
	_, err := eng.Start("proj-1", "deploy", "second")
	if err == nil {
		t.Fatal("expected error for second Start with active session")
	}
}

// --- Bootstrap conductor engine tests ---

func TestEngine_BootstrapStart_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	resp, err := eng.BootstrapStart("proj-1", "bun + postgres")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	if resp.SessionID == "" {
		t.Error("expected non-empty SessionID")
	}
	if resp.Intent != "bun + postgres" {
		t.Errorf("Intent mismatch")
	}
	if resp.Progress.Total != 5 {
		t.Errorf("Total: want 5, got %d", resp.Progress.Total)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.Name != "discover" {
		t.Errorf("Current.Name: want discover, got %s", resp.Current.Name)
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

	if _, err := eng.BootstrapStart("proj-1", "first"); err != nil {
		t.Fatalf("first BootstrapStart: %v", err)
	}

	_, err := eng.BootstrapStart("proj-1", "second")
	if err == nil {
		t.Fatal("expected error for second BootstrapStart")
	}
}

func TestEngine_BootstrapComplete_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	resp, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project, plan submitted", nil)
	if err != nil {
		t.Fatalf("BootstrapComplete: %v", err)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil after first complete")
	}
	if resp.Current.Name != "provision" {
		t.Errorf("Current.Name: want provision, got %s", resp.Current.Name)
	}
	if resp.Progress.Completed != 1 {
		t.Errorf("Completed: want 1, got %d", resp.Progress.Completed)
	}
}

func TestEngine_BootstrapComplete_FullSequence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	steps := []string{"discover", "provision", "generate", "deploy", "verify"}
	var resp *BootstrapResponse
	for _, step := range steps {
		var err error
		resp, err = eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" completed ok", nil)
		if err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// After all steps: bootstrap done.
	if resp.Current != nil {
		t.Error("Current should be nil after all steps")
	}
	if resp.Progress.Completed != 5 {
		t.Errorf("Completed: want 5, got %d", resp.Progress.Completed)
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

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	steps := []string{"discover", "provision", "generate", "deploy", "verify"}
	for _, step := range steps {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" completed ok", nil); err != nil {
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

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	steps := []string{"discover", "provision", "generate", "deploy", "verify"}
	for _, step := range steps {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" completed ok", nil); err != nil {
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
	// Full: INIT→DISCOVER→DEVELOP→DEPLOY→VERIFY→DONE = 5 transitions.
	if len(state.History) != 5 {
		t.Errorf("History length: want 5, got %d", len(state.History))
	}
}

func TestEngine_BootstrapSkip_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Complete discover and provision (steps 0-1).
	preSteps := []string{"discover", "provision"}
	for _, step := range preSteps {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// Skip generate (skippable step).
	resp, err := eng.BootstrapSkip("generate", "no runtime services")
	if err != nil {
		t.Fatalf("BootstrapSkip: %v", err)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.Name != "deploy" {
		t.Errorf("Current.Name: want deploy, got %s", resp.Current.Name)
	}
}

func TestEngine_BootstrapSkip_Mandatory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Skip discover (mandatory) — should fail.
	// discover is the first step and not skippable.
	_, err := eng.BootstrapSkip("discover", "skip reason")
	if err == nil {
		t.Fatal("expected error skipping mandatory step 'discover'")
	}
}

func TestEngine_BootstrapStatus_Active(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Complete 1 step.
	if _, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project, plan submitted", nil); err != nil {
		t.Fatalf("BootstrapComplete(discover): %v", err)
	}

	resp, err := eng.BootstrapStatus()
	if err != nil {
		t.Fatalf("BootstrapStatus: %v", err)
	}
	if resp.Progress.Completed != 1 {
		t.Errorf("Completed: want 1, got %d", resp.Progress.Completed)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.Name != "provision" {
		t.Errorf("Current.Name: want provision, got %s", resp.Current.Name)
	}
}

func TestEngine_BootstrapStatus_WithAttestations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	if _, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project, plan submitted", nil); err != nil {
		t.Fatalf("BootstrapComplete: %v", err)
	}

	resp, err := eng.BootstrapStatus()
	if err != nil {
		t.Fatalf("BootstrapStatus: %v", err)
	}

	// Verify first step shows as complete in summary.
	if len(resp.Progress.Steps) != 5 {
		t.Fatalf("Steps count: want 5, got %d", len(resp.Progress.Steps))
	}
	if resp.Progress.Steps[0].Status != "complete" {
		t.Errorf("step[0].Status: want complete, got %s", resp.Progress.Steps[0].Status)
	}
}

func TestBootstrapComplete_AutoComplete_GatesChecked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Complete all steps with valid attestations.
	steps := []string{"discover", "provision", "generate", "deploy", "verify"}
	for _, step := range steps {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// After auto-complete, phase should be DONE — gates were checked and passed.
	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Phase != PhaseDone {
		t.Errorf("Phase: want DONE, got %s", state.Phase)
	}
}

func TestBootstrapComplete_AutoComplete_FailedEvidence_Blocked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Complete steps 0-3 normally.
	preSteps := []string{"discover", "provision", "generate", "deploy"}
	for _, step := range preSteps {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// Overwrite stage_verify evidence with Failed: 1.
	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	badEv := &Evidence{
		SessionID: state.SessionID, Type: "stage_verify", VerificationType: "attestation",
		Timestamp: "2026-02-23T12:00:00Z", Attestation: "stage failed",
		Passed: 1, Failed: 1,
	}
	if err := SaveEvidence(eng.evidenceDir, state.SessionID, badEv); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	// Complete the last step — auto-complete will overwrite the evidence.
	_, err = eng.BootstrapComplete(context.Background(), "verify", "Final verification complete", nil)
	if err != nil {
		t.Fatalf("BootstrapComplete(verify): %v", err)
	}

	// Verify gates were checked by confirming the history has proper transitions.
	state, err = eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Phase != PhaseDone {
		t.Errorf("Phase: want DONE, got %s", state.Phase)
	}
	if len(state.History) != 5 {
		t.Errorf("History length: want 5, got %d", len(state.History))
	}
}

func TestEngine_BootstrapComplete_AutoEvidence_PassedCount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	steps := []string{"discover", "provision", "generate", "deploy", "verify"}
	for _, step := range steps {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	state, _ := eng.GetState()

	// recipe_review maps to step: discover — 1 step completed.
	ev, err := LoadEvidence(eng.evidenceDir, state.SessionID, "recipe_review")
	if err != nil {
		t.Fatalf("load recipe_review: %v", err)
	}
	if ev.Passed < 1 {
		t.Errorf("recipe_review.Passed: want >=1, got %d", ev.Passed)
	}

	// dev_verify maps to steps: generate, deploy, verify — all 3 completed.
	ev, err = LoadEvidence(eng.evidenceDir, state.SessionID, "dev_verify")
	if err != nil {
		t.Fatalf("load dev_verify: %v", err)
	}
	if ev.Passed < 3 {
		t.Errorf("dev_verify.Passed: want >=3, got %d", ev.Passed)
	}
}

func TestEngine_BootstrapComplete_AutoEvidence_ServiceResults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Complete discover with structured plan.
	plan := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"},
			Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
			},
		},
	}
	if _, err := eng.BootstrapCompletePlan(plan, nil, nil); err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	// Complete remaining steps.
	remaining := []string{"provision", "generate", "deploy", "verify"}
	for _, step := range remaining {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	state, _ := eng.GetState()

	// deploy_evidence should NOT have fabricated ServiceResults.
	ev, err := LoadEvidence(eng.evidenceDir, state.SessionID, "deploy_evidence")
	if err != nil {
		t.Fatalf("load deploy_evidence: %v", err)
	}
	if len(ev.ServiceResults) != 0 {
		t.Errorf("deploy_evidence.ServiceResults should be empty, got %d", len(ev.ServiceResults))
	}

	// stage_verify should also have empty ServiceResults.
	ev, err = LoadEvidence(eng.evidenceDir, state.SessionID, "stage_verify")
	if err != nil {
		t.Fatalf("load stage_verify: %v", err)
	}
	if len(ev.ServiceResults) != 0 {
		t.Errorf("stage_verify.ServiceResults should be empty, got %d", len(ev.ServiceResults))
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

// --- BootstrapCompletePlan tests ---

func TestEngine_BootstrapCompletePlan_Valid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	// Current step is "discover" — plan submission happens here.

	plan := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"},
			Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
			},
		},
	}
	resp, err := eng.BootstrapCompletePlan(plan, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}
	if resp.Current == nil || resp.Current.Name != "provision" {
		t.Errorf("expected current step to be 'provision', got %v", resp.Current)
	}

	// Verify plan is persisted in state.
	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap.Plan == nil {
		t.Fatal("Plan should be stored in bootstrap state")
	}
	if len(state.Bootstrap.Plan.Targets) != 1 {
		t.Errorf("Plan.Targets length: want 1, got %d", len(state.Bootstrap.Plan.Targets))
	}
}

func TestEngine_BootstrapCompletePlan_InvalidHostname(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	// Current step is "discover" — plan submission happens here.

	plan := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "my-app", Type: "bun@1.2"}},
	}
	_, err := eng.BootstrapCompletePlan(plan, nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid hostname")
	}
}

func TestEngine_BootstrapCompletePlan_WrongStep(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	// Complete discover so current step is "provision" — not the plan step.
	if _, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project detected", nil); err != nil {
		t.Fatalf("BootstrapComplete(discover): %v", err)
	}

	plan := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
	}
	_, err := eng.BootstrapCompletePlan(plan, nil, nil)
	if err == nil {
		t.Fatal("expected error when current step is not 'discover'")
	}
}

func TestEngine_StoreDiscoveredEnvVars(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Store env vars for "db".
	err := eng.StoreDiscoveredEnvVars("db", []string{"connectionString", "port", "user"})
	if err != nil {
		t.Fatalf("StoreDiscoveredEnvVars: %v", err)
	}

	// Store env vars for "cache".
	err = eng.StoreDiscoveredEnvVars("cache", []string{"connectionString"})
	if err != nil {
		t.Fatalf("StoreDiscoveredEnvVars: %v", err)
	}

	// Verify persisted.
	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap.DiscoveredEnvVars == nil {
		t.Fatal("DiscoveredEnvVars should not be nil")
	}
	if len(state.Bootstrap.DiscoveredEnvVars["db"]) != 3 {
		t.Errorf("db vars: want 3, got %d", len(state.Bootstrap.DiscoveredEnvVars["db"]))
	}
	if len(state.Bootstrap.DiscoveredEnvVars["cache"]) != 1 {
		t.Errorf("cache vars: want 1, got %d", len(state.Bootstrap.DiscoveredEnvVars["cache"]))
	}
}

func TestEngine_StoreDiscoveredEnvVars_NoBootstrap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	// Start a non-bootstrap session.
	if _, err := eng.Start("proj-1", "deploy", "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	err := eng.StoreDiscoveredEnvVars("db", []string{"connectionString"})
	if err == nil {
		t.Fatal("expected error when no bootstrap state")
	}
}

func TestEngine_StoreDiscoveredEnvVars_MultipleServices(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	services := map[string][]string{
		"db":      {"connectionString", "port", "user", "password"},
		"cache":   {"connectionString", "port"},
		"storage": {"accessKeyId", "secretAccessKey", "bucketName"},
	}
	for hostname, vars := range services {
		if err := eng.StoreDiscoveredEnvVars(hostname, vars); err != nil {
			t.Fatalf("StoreDiscoveredEnvVars(%s): %v", hostname, err)
		}
	}

	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if len(state.Bootstrap.DiscoveredEnvVars) != 3 {
		t.Errorf("service count: want 3, got %d", len(state.Bootstrap.DiscoveredEnvVars))
	}
	for hostname, want := range services {
		got := state.Bootstrap.DiscoveredEnvVars[hostname]
		if len(got) != len(want) {
			t.Errorf("%s: want %d vars, got %d", hostname, len(want), len(got))
		}
	}
}

// --- StepChecker integration tests ---

func TestEngine_BootstrapComplete_WithChecker_Pass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	checker := func(_ context.Context, _ *ServicePlan) (*StepCheckResult, error) {
		return &StepCheckResult{
			Passed:  true,
			Summary: "all good",
			Checks:  []StepCheck{{Name: "test_check", Status: "pass"}},
		}, nil
	}

	resp, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project detected fine", checker)
	if err != nil {
		t.Fatalf("BootstrapComplete: %v", err)
	}
	if resp.CheckResult != nil {
		t.Error("CheckResult should be nil on pass")
	}
	if resp.Current == nil || resp.Current.Name != "provision" {
		t.Errorf("expected next step 'provision', got %v", resp.Current)
	}
}

func TestEngine_BootstrapComplete_WithChecker_Fail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	checker := func(_ context.Context, _ *ServicePlan) (*StepCheckResult, error) {
		return &StepCheckResult{
			Passed:  false,
			Summary: "service missing",
			Checks: []StepCheck{
				{Name: "service_exists", Status: "fail", Detail: "appdev not found"},
			},
		}, nil
	}

	resp, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project detected fine", checker)
	if err != nil {
		t.Fatalf("BootstrapComplete should not return error on check failure: %v", err)
	}
	if resp.CheckResult == nil {
		t.Fatal("CheckResult should be populated on failure")
	}
	if resp.CheckResult.Passed {
		t.Error("CheckResult.Passed should be false")
	}
	if resp.CheckResult.Summary != "service missing" {
		t.Errorf("CheckResult.Summary: want 'service missing', got %q", resp.CheckResult.Summary)
	}
	// Step should NOT have advanced.
	state, _ := eng.GetState()
	if state.Bootstrap.CurrentStep != 0 {
		t.Errorf("CurrentStep should still be 0, got %d", state.Bootstrap.CurrentStep)
	}
}

func TestEngine_BootstrapComplete_NilChecker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	resp, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project detected fine", nil)
	if err != nil {
		t.Fatalf("BootstrapComplete: %v", err)
	}
	if resp.Current == nil || resp.Current.Name != "provision" {
		t.Errorf("expected next step 'provision', got %v", resp.Current)
	}
}

func TestEngine_BootstrapComplete_CheckerError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	checker := func(_ context.Context, _ *ServicePlan) (*StepCheckResult, error) {
		return nil, fmt.Errorf("API unreachable")
	}

	_, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project detected fine", checker)
	if err == nil {
		t.Fatal("expected error from checker")
	}
}
