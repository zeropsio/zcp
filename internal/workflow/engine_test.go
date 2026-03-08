// Tests for: workflow engine — orchestration, project state detection, transitions.
package workflow

import (
	"context"
	"fmt"
	"os"
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
	state, gateResult, err := eng.Transition(PhaseDiscover)
	if err != nil {
		t.Fatalf("Transition to DISCOVER: %v", err)
	}
	if gateResult != nil {
		t.Fatalf("Transition gate failed: %v", gateResult.Missing)
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
	_, _, err := eng.Transition(PhaseDeploy)
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
	_, gateResult, err := eng.Transition(PhaseDiscover)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gateResult == nil {
		t.Fatal("expected gate failure (no recipe_review evidence)")
	}
	if gateResult.Passed {
		t.Fatal("expected gate to fail")
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
	if eng.HasActiveSession() {
		t.Error("expected no active session after reset")
	}
}

func TestEngine_Reset_ClearsSessionID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.Start("proj-1", "deploy", "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if eng.SessionID() == "" {
		t.Fatal("expected non-empty session ID after Start")
	}

	if err := eng.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if eng.SessionID() != "" {
		t.Error("expected empty session ID after Reset")
	}
}

func TestEngine_Reset_Unregisters(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.Start("proj-1", "deploy", "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := eng.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions after reset, got %d", len(sessions))
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

func TestEngine_Start_StoresSessionID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	state, err := eng.Start("proj-1", "deploy", "test")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if eng.SessionID() != state.SessionID {
		t.Errorf("engine SessionID mismatch: want %s, got %s", state.SessionID, eng.SessionID())
	}
}

func TestEngine_Start_RegistersInRegistry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	state, err := eng.Start("proj-1", "deploy", "test")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	sessions, err := eng.ListActiveSessions()
	if err != nil {
		t.Fatalf("ListActiveSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionID != state.SessionID {
		t.Errorf("registry session mismatch")
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
	if err := saveSessionState(dir, eng.SessionID(), state); err != nil {
		t.Fatalf("saveSessionState: %v", err)
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

// --- Bootstrap exclusivity tests ---

func TestEngine_BootstrapExclusivity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng1 := NewEngine(dir)
	eng2 := NewEngine(dir)

	if _, err := eng1.BootstrapStart("proj-1", "first bootstrap"); err != nil {
		t.Fatalf("first BootstrapStart: %v", err)
	}

	// Second bootstrap on different engine should fail.
	_, err := eng2.BootstrapStart("proj-1", "second bootstrap")
	if err == nil {
		t.Fatal("expected error for second bootstrap (exclusivity)")
	}
}

func TestEngine_BootstrapExclusivity_DeadPID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Register a bootstrap session with a dead PID directly.
	entry := SessionEntry{
		SessionID: "dead-bootstrap",
		PID:       9999999,
		Workflow:  "bootstrap",
		ProjectID: "proj-1",
		Phase:     PhaseInit,
	}
	if err := RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	// New bootstrap should succeed because the dead PID will be pruned.
	eng := NewEngine(dir)
	_, err := eng.BootstrapStart("proj-1", "test")
	if err != nil {
		t.Fatalf("BootstrapStart should succeed after dead PID pruned: %v", err)
	}
}

// --- Multiple engines coexist tests ---

func TestEngine_MultipleEngines_Coexist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng1 := NewEngine(dir)
	eng2 := NewEngine(dir)

	// Both can start deploy sessions (different from bootstrap exclusivity).
	if _, err := eng1.Start("proj-1", "deploy", "first"); err != nil {
		t.Fatalf("eng1.Start: %v", err)
	}
	if _, err := eng2.Start("proj-1", "deploy", "second"); err != nil {
		t.Fatalf("eng2.Start: %v", err)
	}

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("want 2 sessions, got %d", len(sessions))
	}
}

func TestEngine_Transition_UpdatesRegistry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	state, err := eng.Start("proj-1", "deploy", "test")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	ev := &Evidence{
		SessionID: state.SessionID, Type: "recipe_review", VerificationType: "attestation",
		Attestation: "recipe reviewed", Passed: 1,
	}
	if err := eng.RecordEvidence(ev); err != nil {
		t.Fatalf("RecordEvidence: %v", err)
	}

	if _, gr, err := eng.Transition(PhaseDiscover); err != nil || gr != nil {
		t.Fatalf("Transition: %v", err)
	}

	sessions, err := eng.ListActiveSessions()
	if err != nil {
		t.Fatalf("ListActiveSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	if sessions[0].Phase != PhaseDiscover {
		t.Errorf("registry phase: want DISCOVER, got %s", sessions[0].Phase)
	}
}

func TestEngine_SameEngine_ActiveSessionBlocks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	state, err := eng.Start("proj-1", "deploy", "original")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	sessionID := state.SessionID

	// Second start on same engine should fail (active non-DONE session).
	_, err = eng.Start("proj-1", "deploy", "retry")
	if err == nil {
		t.Fatal("expected error: active session should block")
	}

	// Original session still accessible.
	state, err = eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.SessionID != sessionID {
		t.Errorf("session ID changed unexpectedly")
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
	if resp.Progress.Total != 6 {
		t.Errorf("Total: want 6, got %d", resp.Progress.Total)
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

	steps := []string{"discover", "provision", "generate", "deploy", "verify", "strategy"}
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
	if resp.Progress.Completed != 6 {
		t.Errorf("Completed: want 6, got %d", resp.Progress.Completed)
	}

	// Session should be unregistered (DONE sessions are immediately cleaned up).
	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions after bootstrap done, got %d", len(sessions))
	}
}

func TestEngine_BootstrapComplete_AutoEvidence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	steps := []string{"discover", "provision", "generate", "deploy", "verify", "strategy"}
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

	steps := []string{"discover", "provision", "generate", "deploy", "verify", "strategy"}
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

	if len(resp.Progress.Steps) != 6 {
		t.Fatalf("Steps count: want 6, got %d", len(resp.Progress.Steps))
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

	steps := []string{"discover", "provision", "generate", "deploy", "verify", "strategy"}
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
}

func TestBootstrapComplete_AutoComplete_FailedEvidence_Blocked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Complete steps 0-4 normally.
	preSteps := []string{"discover", "provision", "generate", "deploy", "verify"}
	for _, step := range preSteps {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// Overwrite stage_verify evidence with Failed: 1.
	sessionID := eng.SessionID()
	badEv := &Evidence{
		SessionID: sessionID, Type: "stage_verify", VerificationType: "attestation",
		Timestamp: "2026-02-23T12:00:00Z", Attestation: "stage failed",
		Passed: 1, Failed: 1,
	}
	if err := SaveEvidence(eng.evidenceDir, sessionID, badEv); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}

	// Complete the last step — auto-complete will overwrite the evidence.
	_, err := eng.BootstrapComplete(context.Background(), "strategy", "Strategy selection complete", nil)
	if err != nil {
		t.Fatalf("BootstrapComplete(strategy): %v", err)
	}

	state, err := eng.GetState()
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

	steps := []string{"discover", "provision", "generate", "deploy", "verify", "strategy"}
	for _, step := range steps {
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" completed ok", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	state, _ := eng.GetState()
	sessionID := state.SessionID

	// recipe_review maps to step: discover — 1 step completed.
	ev, err := LoadEvidence(eng.evidenceDir, sessionID, "recipe_review")
	if err != nil {
		t.Fatalf("load recipe_review: %v", err)
	}
	if ev.Passed < 1 {
		t.Errorf("recipe_review.Passed: want >=1, got %d", ev.Passed)
	}

	// dev_verify maps to steps: generate, deploy, verify — all 3 completed.
	ev, err = LoadEvidence(eng.evidenceDir, sessionID, "dev_verify")
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
	remaining := []string{"provision", "generate", "deploy", "verify", "strategy"}
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

	err := eng.StoreDiscoveredEnvVars("db", []string{"connectionString", "port", "user"})
	if err != nil {
		t.Fatalf("StoreDiscoveredEnvVars: %v", err)
	}

	err = eng.StoreDiscoveredEnvVars("cache", []string{"connectionString"})
	if err != nil {
		t.Fatalf("StoreDiscoveredEnvVars: %v", err)
	}

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

	checker := func(_ context.Context, _ *ServicePlan, _ *BootstrapState) (*StepCheckResult, error) {
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

	checker := func(_ context.Context, _ *ServicePlan, _ *BootstrapState) (*StepCheckResult, error) {
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

func TestEngine_Iterate_MaxLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.Start("proj-1", "deploy", "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Iterate 10 times (default max) — all should succeed.
	for i := range 10 {
		state, err := eng.Iterate()
		if err != nil {
			t.Fatalf("Iterate %d: %v", i+1, err)
		}
		if state.Iteration != i+1 {
			t.Errorf("Iteration %d: want %d, got %d", i+1, i+1, state.Iteration)
		}
	}

	// 11th iteration should fail with "max iterations" error.
	_, err := eng.Iterate()
	if err == nil {
		t.Fatal("expected error on 11th iteration")
	}
	if got := err.Error(); !contains(got, "max iterations") {
		t.Errorf("error should mention 'max iterations', got: %s", got)
	}
}

func TestEngine_Iterate_EnvOverride(t *testing.T) {
	t.Setenv("ZCP_MAX_ITERATIONS", "2")

	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.Start("proj-1", "deploy", "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Iterate twice — should succeed.
	for i := range 2 {
		if _, err := eng.Iterate(); err != nil {
			t.Fatalf("Iterate %d: %v", i+1, err)
		}
	}

	// Third iteration should fail.
	_, err := eng.Iterate()
	if err == nil {
		t.Fatal("expected error on 3rd iteration with max=2")
	}
	if got := err.Error(); !contains(got, "max iterations") {
		t.Errorf("error should mention 'max iterations', got: %s", got)
	}
}

// contains is a test helper for substring matching.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestEngine_BootstrapComplete_CheckerError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	checker := func(_ context.Context, _ *ServicePlan, _ *BootstrapState) (*StepCheckResult, error) {
		return nil, fmt.Errorf("API unreachable")
	}

	_, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project detected fine", checker)
	if err == nil {
		t.Fatal("expected error from checker")
	}
}

// --- Item 16: TOCTOU fix via InitSessionAtomic ---

func TestInitSessionAtomic_BootstrapExclusivity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		firstWf     string
		secondWf    string
		expectErr   bool
		errContains string
	}{
		{
			"second_bootstrap_blocked",
			"bootstrap", "bootstrap",
			true, "bootstrap already active",
		},
		{
			"deploy_after_bootstrap_ok",
			"bootstrap", "deploy",
			false, "",
		},
		{
			"two_deploys_ok",
			"deploy", "deploy",
			false, "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			// First session via InitSessionAtomic.
			_, err := InitSessionAtomic(dir, "proj-1", tt.firstWf, "first")
			if err != nil {
				t.Fatalf("first InitSessionAtomic: %v", err)
			}

			// Second session.
			_, err = InitSessionAtomic(dir, "proj-1", tt.secondWf, "second")
			if tt.expectErr {
				if err == nil {
					t.Fatal("expected error for second session")
				}
				if !contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// --- Item 17: Two-write consistency via saveStateAndUpdateRegistry ---

func TestSaveStateAndUpdateRegistry_Atomic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		phase Phase
	}{
		{"to_discover", PhaseDiscover},
		{"to_develop", PhaseDevelop},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			state, err := InitSessionAtomic(dir, "proj-1", "deploy", "test")
			if err != nil {
				t.Fatalf("InitSessionAtomic: %v", err)
			}
			state.Phase = tt.phase

			if err := saveStateAndUpdateRegistry(dir, state.SessionID, state, tt.phase); err != nil {
				t.Fatalf("saveStateAndUpdateRegistry: %v", err)
			}

			// Verify state file has correct phase.
			loaded, err := LoadSessionByID(dir, state.SessionID)
			if err != nil {
				t.Fatalf("LoadSessionByID: %v", err)
			}
			if loaded.Phase != tt.phase {
				t.Errorf("state phase: want %s, got %s", tt.phase, loaded.Phase)
			}

			// Verify registry has correct phase.
			sessions, err := ListSessions(dir)
			if err != nil {
				t.Fatalf("ListSessions: %v", err)
			}
			found := false
			for _, s := range sessions {
				if s.SessionID == state.SessionID {
					found = true
					if s.Phase != tt.phase {
						t.Errorf("registry phase: want %s, got %s", tt.phase, s.Phase)
					}
				}
			}
			if !found {
				t.Error("session not found in registry")
			}
		})
	}
}

// --- Item 18: Session Resume ---

func TestEngine_Resume_DeadSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create a session with a dead PID by writing state directly.
	sessionID := "dead-session-001"
	state := &WorkflowState{
		Version:   stateVersion,
		SessionID: sessionID,
		PID:       9999999, // Dead PID.
		ProjectID: "proj-1",
		Workflow:  "deploy",
		Phase:     PhaseDevelop,
		Iteration: 2,
		Intent:    "deploy app",
		CreatedAt: "2026-03-01T00:00:00Z",
		UpdatedAt: "2026-03-01T01:00:00Z",
		History:   []PhaseTransition{},
	}
	if err := saveSessionState(dir, sessionID, state); err != nil {
		t.Fatalf("saveSessionState: %v", err)
	}
	entry := SessionEntry{
		SessionID: sessionID,
		PID:       9999999,
		Workflow:  "deploy",
		ProjectID: "proj-1",
		Phase:     PhaseDevelop,
		CreatedAt: "2026-03-01T00:00:00Z",
		UpdatedAt: "2026-03-01T01:00:00Z",
	}
	if err := RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	eng := NewEngine(dir)
	resumed, err := eng.Resume(sessionID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.SessionID != sessionID {
		t.Errorf("SessionID: want %s, got %s", sessionID, resumed.SessionID)
	}
	if resumed.Phase != PhaseDevelop {
		t.Errorf("Phase: want DEVELOP, got %s", resumed.Phase)
	}
	if resumed.Iteration != 2 {
		t.Errorf("Iteration: want 2, got %d", resumed.Iteration)
	}
	if eng.SessionID() != sessionID {
		t.Errorf("engine SessionID: want %s, got %s", sessionID, eng.SessionID())
	}
	// PID should be updated to current process.
	reloaded, err := LoadSessionByID(dir, sessionID)
	if err != nil {
		t.Fatalf("LoadSessionByID: %v", err)
	}
	if reloaded.PID != currentPID() {
		t.Errorf("PID: want %d, got %d", currentPID(), reloaded.PID)
	}
}

func TestEngine_Resume_AliveSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng1 := NewEngine(dir)

	// Create an active session (current PID = alive).
	state, err := eng1.Start("proj-1", "deploy", "test")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Try to resume from another engine — should fail because PID is alive.
	eng2 := NewEngine(dir)
	_, err = eng2.Resume(state.SessionID)
	if err == nil {
		t.Fatal("expected error resuming alive session")
	}
	if !contains(err.Error(), "still active") {
		t.Errorf("error should mention 'still active', got: %s", err.Error())
	}
}

func TestEngine_Resume_NotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir)

	_, err := eng.Resume("nonexistent-session")
	if err == nil {
		t.Fatal("expected error resuming non-existent session")
	}
}

// currentPID is a test helper.
func currentPID() int {
	return os.Getpid()
}
