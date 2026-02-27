// Tests for: bootstrap conductor — 10-step state machine with attestations.
package workflow

import (
	"slices"
	"strings"
	"testing"
)

func TestStepDetails_AllStepsCovered(t *testing.T) {
	t.Parallel()
	expectedNames := []string{
		"detect", "plan", "load-knowledge", "generate-import",
		"import-services", "mount-dev", "discover-envs",
		"deploy", "verify", "report",
	}
	for _, name := range expectedNames {
		detail := lookupDetail(name)
		if detail.Name == "" {
			t.Errorf("missing StepDetail for %q", name)
			continue
		}
		if detail.Guidance == "" {
			t.Errorf("step %q has empty Guidance", name)
		}
		if len(detail.Tools) == 0 {
			t.Errorf("step %q has no Tools", name)
		}
		if detail.Verification == "" {
			t.Errorf("step %q has empty Verification", name)
		}
	}
}

func TestStepDetails_ToolLists(t *testing.T) {
	t.Parallel()
	tests := []struct {
		step     string
		wantTool string
	}{
		{"deploy", "zerops_verify"},
		{"verify", "zerops_verify"},
	}
	for _, tt := range tests {
		t.Run(tt.step+"_has_"+tt.wantTool, func(t *testing.T) {
			t.Parallel()
			detail := lookupDetail(tt.step)
			if !slices.Contains(detail.Tools, tt.wantTool) {
				t.Errorf("step %q Tools %v should contain %q", tt.step, detail.Tools, tt.wantTool)
			}
		})
	}
}

func TestStepDetails_DetectGuidance_ThreeStates(t *testing.T) {
	t.Parallel()
	detail := lookupDetail("detect")

	// Guidance must mention the 3 actual code states.
	for _, state := range []string{"FRESH", "CONFORMANT", "NON_CONFORMANT"} {
		if !strings.Contains(detail.Guidance, state) {
			t.Errorf("detect guidance missing state %q", state)
		}
	}
	// Guidance must NOT mention dropped states.
	for _, dropped := range []string{"PARTIAL", "EXISTING"} {
		if strings.Contains(detail.Guidance, dropped) {
			t.Errorf("detect guidance still mentions dropped state %q", dropped)
		}
	}
}

func TestStepDetails_Categories(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		category StepCategory
	}{
		{"detect", CategoryFixed},
		{"plan", CategoryCreative},
		{"load-knowledge", CategoryFixed},
		{"generate-import", CategoryCreative},
		{"import-services", CategoryFixed},
		{"mount-dev", CategoryFixed},
		{"discover-envs", CategoryFixed},
		{"deploy", CategoryBranching},
		{"verify", CategoryFixed},
		{"report", CategoryFixed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			detail := lookupDetail(tt.name)
			if detail.Category != tt.category {
				t.Errorf("step %q: want category %q, got %q", tt.name, tt.category, detail.Category)
			}
		})
	}
}

func TestNewBootstrapState_10Steps(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()

	if !bs.Active {
		t.Error("expected Active to be true")
	}
	if bs.CurrentStep != 0 {
		t.Errorf("CurrentStep: want 0, got %d", bs.CurrentStep)
	}
	if len(bs.Steps) != 10 {
		t.Fatalf("Steps count: want 10, got %d", len(bs.Steps))
	}

	expectedNames := []string{
		"detect", "plan", "load-knowledge", "generate-import",
		"import-services", "mount-dev", "discover-envs",
		"deploy", "verify", "report",
	}
	for i, name := range expectedNames {
		if bs.Steps[i].Name != name {
			t.Errorf("step[%d].Name: want %s, got %s", i, name, bs.Steps[i].Name)
		}
		if bs.Steps[i].Status != "pending" {
			t.Errorf("step[%d].Status: want pending, got %s", i, bs.Steps[i].Status)
		}
	}
}

func TestCompleteStep_Success(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = "in_progress"

	err := bs.CompleteStep("detect", "FRESH project, no existing services found")
	if err != nil {
		t.Fatalf("CompleteStep: %v", err)
	}

	if bs.Steps[0].Status != "complete" {
		t.Errorf("step[0].Status: want complete, got %s", bs.Steps[0].Status)
	}
	if bs.Steps[0].Attestation != "FRESH project, no existing services found" {
		t.Errorf("attestation not stored")
	}
	if bs.Steps[0].CompletedAt == "" {
		t.Error("CompletedAt not set")
	}
	if bs.CurrentStep != 1 {
		t.Errorf("CurrentStep: want 1, got %d", bs.CurrentStep)
	}
}

func TestCompleteStep_WrongStep(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = "in_progress"

	err := bs.CompleteStep("plan", "something")
	if err == nil {
		t.Fatal("expected error for completing wrong step")
	}
	if !strings.Contains(err.Error(), "detect") {
		t.Errorf("error should mention current step 'detect', got: %s", err.Error())
	}
}

func TestCompleteStep_EmptyAttestation(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = "in_progress"

	// Empty attestation.
	err := bs.CompleteStep("detect", "")
	if err == nil {
		t.Fatal("expected error for empty attestation")
	}

	// Too short attestation.
	err = bs.CompleteStep("detect", "short")
	if err == nil {
		t.Fatal("expected error for short attestation (<10 chars)")
	}
}

func TestCompleteStep_NotActive(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Active = false

	err := bs.CompleteStep("detect", "some attestation text here")
	if err == nil {
		t.Fatal("expected error when bootstrap not active")
	}
}

func TestCompleteStep_AllDone(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()

	stepNames := []string{
		"detect", "plan", "load-knowledge", "generate-import",
		"import-services", "mount-dev", "discover-envs",
		"deploy", "verify", "report",
	}
	for _, name := range stepNames {
		bs.Steps[bs.CurrentStep].Status = "in_progress"
		err := bs.CompleteStep(name, "Attestation for "+name+" step completed successfully")
		if err != nil {
			t.Fatalf("CompleteStep(%s): %v", name, err)
		}
	}

	if bs.Active {
		t.Error("expected Active=false after all steps complete")
	}
	if bs.CurrentStep != 10 {
		t.Errorf("CurrentStep: want 10, got %d", bs.CurrentStep)
	}
}

func TestSkipStep_Success(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	// Advance to mount-dev (index 5).
	for i := range 5 {
		bs.Steps[i].Status = "complete"
	}
	bs.CurrentStep = 5
	bs.Steps[5].Status = "in_progress"

	err := bs.SkipStep("mount-dev", "no runtime services to mount")
	if err != nil {
		t.Fatalf("SkipStep: %v", err)
	}

	if bs.Steps[5].Status != "skipped" {
		t.Errorf("step[5].Status: want skipped, got %s", bs.Steps[5].Status)
	}
	if bs.Steps[5].SkipReason != "no runtime services to mount" {
		t.Error("SkipReason not stored")
	}
	if bs.CurrentStep != 6 {
		t.Errorf("CurrentStep: want 6, got %d", bs.CurrentStep)
	}
}

func TestSkipStep_MandatoryStep(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		step string
		idx  int
	}{
		{"detect", "detect", 0},
		{"plan", "plan", 1},
		{"load-knowledge", "load-knowledge", 2},
		{"generate-import", "generate-import", 3},
		{"import-services", "import-services", 4},
		{"verify", "verify", 8},
		{"report", "report", 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bs := NewBootstrapState()
			for i := 0; i < tt.idx; i++ {
				bs.Steps[i].Status = "complete"
			}
			bs.CurrentStep = tt.idx
			bs.Steps[tt.idx].Status = "in_progress"

			err := bs.SkipStep(tt.step, "some reason")
			if err == nil {
				t.Fatalf("expected error skipping mandatory step %q", tt.step)
			}
		})
	}
}

func TestSkipStep_WrongStep(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = "in_progress"

	err := bs.SkipStep("mount-dev", "reason")
	if err == nil {
		t.Fatal("expected error for skipping wrong step")
	}
	if !strings.Contains(err.Error(), "detect") {
		t.Errorf("error should mention current step 'detect', got: %s", err.Error())
	}
}

func TestBuildResponse_FirstStep(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = "in_progress"

	resp := bs.BuildResponse("sess-1", ModeFull, "bun + postgres")
	if resp.SessionID != "sess-1" {
		t.Errorf("SessionID: want sess-1, got %s", resp.SessionID)
	}
	if resp.Mode != ModeFull {
		t.Errorf("Mode: want full, got %s", resp.Mode)
	}
	if resp.Intent != "bun + postgres" {
		t.Errorf("Intent mismatch")
	}
	if resp.Progress.Total != 10 {
		t.Errorf("Progress.Total: want 10, got %d", resp.Progress.Total)
	}
	if resp.Progress.Completed != 0 {
		t.Errorf("Progress.Completed: want 0, got %d", resp.Progress.Completed)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.Name != "detect" {
		t.Errorf("Current.Name: want detect, got %s", resp.Current.Name)
	}
	if resp.Current.Index != 0 {
		t.Errorf("Current.Index: want 0, got %d", resp.Current.Index)
	}
	if resp.Current.Guidance == "" {
		t.Error("Current.Guidance should not be empty")
	}
}

func TestBuildResponse_MiddleStep(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	// Complete first 5 steps.
	for i := range 5 {
		bs.Steps[i].Status = "complete"
		bs.Steps[i].Attestation = "done"
	}
	bs.CurrentStep = 5
	bs.Steps[5].Status = "in_progress"

	resp := bs.BuildResponse("sess-2", ModeFull, "test")
	if resp.Progress.Completed != 5 {
		t.Errorf("Progress.Completed: want 5, got %d", resp.Progress.Completed)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.Name != "mount-dev" {
		t.Errorf("Current.Name: want mount-dev, got %s", resp.Current.Name)
	}
	if resp.Current.Index != 5 {
		t.Errorf("Current.Index: want 5, got %d", resp.Current.Index)
	}
}

func TestBuildResponse_AllDone(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	for i := range bs.Steps {
		bs.Steps[i].Status = "complete"
	}
	bs.CurrentStep = 10
	bs.Active = false

	resp := bs.BuildResponse("sess-3", ModeFull, "test")
	if resp.Current != nil {
		t.Error("Current should be nil when all done")
	}
	if resp.Progress.Completed != 10 {
		t.Errorf("Progress.Completed: want 10, got %d", resp.Progress.Completed)
	}
	if !strings.Contains(strings.ToLower(resp.Message), "complete") {
		t.Errorf("Message should contain 'complete', got: %s", resp.Message)
	}
}

func TestBuildResponse_WithSkipped(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	// Complete 5, skip 1, in_progress 1.
	for i := range 5 {
		bs.Steps[i].Status = "complete"
	}
	bs.Steps[5].Status = "skipped"
	bs.Steps[5].SkipReason = "no runtime services"
	bs.CurrentStep = 6
	bs.Steps[6].Status = "in_progress"

	resp := bs.BuildResponse("sess-4", ModeFull, "test")
	// Skipped counts as completed for progress.
	if resp.Progress.Completed != 6 {
		t.Errorf("Progress.Completed: want 6 (5 complete + 1 skipped), got %d", resp.Progress.Completed)
	}

	// Verify the skipped step shows as "skipped" in summary.
	found := false
	for _, s := range resp.Progress.Steps {
		if s.Name == "mount-dev" && s.Status == "skipped" {
			found = true
			break
		}
	}
	if !found {
		t.Error("mount-dev should appear as 'skipped' in progress steps")
	}
}

func TestBuildResponse_PriorContext_Attestations(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	// Complete first 3 steps with attestations.
	attestations := map[string]string{
		"detect":         "FRESH project detected, no runtime services",
		"plan":           "Planned: appdev (bun@1.2), db (postgresql@16)",
		"load-knowledge": "Loaded bun runtime briefing and infrastructure scope",
	}
	for i, name := range []string{"detect", "plan", "load-knowledge"} {
		bs.Steps[i].Status = stepComplete
		bs.Steps[i].Attestation = attestations[name]
	}
	bs.CurrentStep = 3
	bs.Steps[3].Status = stepInProgress

	resp := bs.BuildResponse("sess-ctx", ModeFull, "bun + postgres")
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.PriorContext == nil {
		t.Fatal("PriorContext should not be nil when prior steps have attestations")
	}
	if len(resp.Current.PriorContext.Attestations) != 3 {
		t.Errorf("PriorContext.Attestations: want 3 entries, got %d", len(resp.Current.PriorContext.Attestations))
	}
	if resp.Current.PriorContext.Attestations["detect"] != attestations["detect"] {
		t.Errorf("PriorContext.Attestations[detect] mismatch")
	}
}

func TestBuildResponse_PriorContext_WithPlan(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	// Complete first 2 steps.
	bs.Steps[0].Status = stepComplete
	bs.Steps[0].Attestation = "FRESH project"
	bs.Steps[1].Status = stepComplete
	bs.Steps[1].Attestation = "Planned services"
	bs.CurrentStep = 2
	bs.Steps[2].Status = stepInProgress
	bs.Plan = &ServicePlan{
		Services: []PlannedService{
			{Hostname: "appdev", Type: "bun@1.2"},
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
		},
		CreatedAt: "2026-02-27T00:00:00Z",
	}

	resp := bs.BuildResponse("sess-plan", ModeFull, "test")
	if resp.Current.PriorContext == nil {
		t.Fatal("PriorContext should not be nil")
	}
	if resp.Current.PriorContext.Plan == nil {
		t.Fatal("PriorContext.Plan should not be nil when plan exists")
	}
	if len(resp.Current.PriorContext.Plan.Services) != 2 {
		t.Errorf("PriorContext.Plan.Services: want 2, got %d", len(resp.Current.PriorContext.Plan.Services))
	}
}

func TestBuildResponse_DetailedGuide_Populated(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = stepInProgress

	resp := bs.BuildResponse("sess-guide", ModeFull, "test")
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.DetailedGuide == "" {
		t.Error("DetailedGuide should be populated from bootstrap.md for detect step")
	}
}

func TestBuildResponse_PriorContext_FirstStep_Empty(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Steps[0].Status = stepInProgress

	resp := bs.BuildResponse("sess-first", ModeFull, "test")
	if resp.Current.PriorContext != nil {
		t.Error("PriorContext should be nil for first step (no prior attestations)")
	}
}

func TestBootstrapState_CurrentStepName_10Steps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		step     int
		expected string
	}{
		{"first", 0, "detect"},
		{"middle", 5, "mount-dev"},
		{"last", 9, "report"},
		{"out_of_bounds", 10, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bs := NewBootstrapState()
			bs.CurrentStep = tt.step
			if got := bs.CurrentStepName(); got != tt.expected {
				t.Errorf("CurrentStepName: want %s, got %s", tt.expected, got)
			}
		})
	}
}
