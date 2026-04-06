package workflow

import (
	"slices"
	"strings"
	"testing"
)

func TestNewRecipeState_InitializesCorrectly(t *testing.T) {
	t.Parallel()

	rs := NewRecipeState()

	if !rs.Active {
		t.Error("expected Active to be true")
	}
	if rs.CurrentStep != 0 {
		t.Errorf("expected CurrentStep 0, got %d", rs.CurrentStep)
	}
	if len(rs.Steps) != 6 {
		t.Fatalf("expected 6 steps, got %d", len(rs.Steps))
	}

	wantNames := []string{"research", "provision", "generate", "deploy", "finalize", "close"}
	for i, name := range wantNames {
		if rs.Steps[i].Name != name {
			t.Errorf("step %d: expected name %q, got %q", i, name, rs.Steps[i].Name)
		}
		if rs.Steps[i].Status != stepPending {
			t.Errorf("step %d: expected status %q, got %q", i, stepPending, rs.Steps[i].Status)
		}
	}
}

func TestRecipeCompleteStep_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		step     string
		stepIdx  int
		wantNext string
	}{
		{"complete research", RecipeStepResearch, 0, RecipeStepProvision},
		{"complete provision", RecipeStepProvision, 1, RecipeStepGenerate},
		{"complete generate", RecipeStepGenerate, 2, RecipeStepDeploy},
		{"complete deploy", RecipeStepDeploy, 3, RecipeStepFinalize},
		{"complete finalize", RecipeStepFinalize, 4, RecipeStepClose},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rs := NewRecipeState()
			// Advance to the target step.
			for i := 0; i < tt.stepIdx; i++ {
				rs.Steps[i].Status = stepComplete
				rs.Steps[i].Attestation = "step completed for test"
			}
			rs.CurrentStep = tt.stepIdx
			rs.Steps[tt.stepIdx].Status = stepInProgress

			if err := rs.CompleteStep(tt.step, "attesting completion of this step"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if rs.Steps[tt.stepIdx].Status != stepComplete {
				t.Errorf("expected step status %q, got %q", stepComplete, rs.Steps[tt.stepIdx].Status)
			}
			if rs.CurrentStep != tt.stepIdx+1 {
				t.Errorf("expected CurrentStep %d, got %d", tt.stepIdx+1, rs.CurrentStep)
			}
			if rs.CurrentStepName() != tt.wantNext {
				t.Errorf("expected next step %q, got %q", tt.wantNext, rs.CurrentStepName())
			}
			if !rs.Active {
				t.Error("expected Active to remain true")
			}
		})
	}
}

func TestRecipeCompleteStep_AllDone(t *testing.T) {
	t.Parallel()

	rs := NewRecipeState()
	steps := []string{
		RecipeStepResearch, RecipeStepProvision, RecipeStepGenerate,
		RecipeStepDeploy, RecipeStepFinalize, RecipeStepClose,
	}
	for _, step := range steps {
		rs.Steps[rs.CurrentStep].Status = stepInProgress
		if err := rs.CompleteStep(step, "completed: "+step); err != nil {
			t.Fatalf("complete %q: %v", step, err)
		}
	}

	if rs.Active {
		t.Error("expected Active to be false after all steps")
	}
	if rs.CurrentStep != 6 {
		t.Errorf("expected CurrentStep 6, got %d", rs.CurrentStep)
	}
	if rs.CurrentStepName() != "" {
		t.Errorf("expected empty CurrentStepName, got %q", rs.CurrentStepName())
	}
}

func TestRecipeCompleteStep_WrongName(t *testing.T) {
	t.Parallel()

	rs := NewRecipeState()
	rs.Steps[0].Status = stepInProgress

	err := rs.CompleteStep("provision", "attestation text here")
	if err == nil {
		t.Fatal("expected error for wrong step name")
	}
}

func TestRecipeCompleteStep_ShortAttestation(t *testing.T) {
	t.Parallel()

	rs := NewRecipeState()
	rs.Steps[0].Status = stepInProgress

	err := rs.CompleteStep(RecipeStepResearch, "short")
	if err == nil {
		t.Fatal("expected error for short attestation")
	}
}

func TestRecipeCompleteStep_NotActive(t *testing.T) {
	t.Parallel()

	rs := NewRecipeState()
	rs.Active = false

	err := rs.CompleteStep(RecipeStepResearch, "attestation text here")
	if err == nil {
		t.Fatal("expected error when not active")
	}
}

func TestRecipeSkipStep_CloseOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		step    string
		stepIdx int
		wantErr bool
	}{
		{"skip research forbidden", RecipeStepResearch, 0, true},
		{"skip provision forbidden", RecipeStepProvision, 1, true},
		{"skip generate forbidden", RecipeStepGenerate, 2, true},
		{"skip deploy forbidden", RecipeStepDeploy, 3, true},
		{"skip finalize forbidden", RecipeStepFinalize, 4, true},
		{"skip close allowed", RecipeStepClose, 5, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rs := NewRecipeState()
			// Advance to target step.
			for i := 0; i < tt.stepIdx; i++ {
				rs.Steps[i].Status = stepComplete
				rs.Steps[i].Attestation = "completed for test"
			}
			rs.CurrentStep = tt.stepIdx
			rs.Steps[tt.stepIdx].Status = stepInProgress

			err := rs.SkipStep(tt.step, "skip reason for test")
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.wantErr {
				if rs.Steps[tt.stepIdx].Status != stepSkipped {
					t.Errorf("expected status %q, got %q", stepSkipped, rs.Steps[tt.stepIdx].Status)
				}
			}
		})
	}
}

func TestRecipeSkipStep_NotActive(t *testing.T) {
	t.Parallel()

	rs := NewRecipeState()
	rs.Active = false

	err := rs.SkipStep(RecipeStepResearch, "skip reason")
	if err == nil {
		t.Fatal("expected error when not active")
	}
}

func TestRecipeSkipStep_WrongName(t *testing.T) {
	t.Parallel()

	rs := NewRecipeState()
	rs.Steps[0].Status = stepInProgress

	err := rs.SkipStep("provision", "skip reason")
	if err == nil {
		t.Fatal("expected error for wrong step name")
	}
}

func TestRecipeResetForIteration(t *testing.T) {
	t.Parallel()

	rs := NewRecipeState()
	// Complete research and provision, generate is in_progress.
	rs.Steps[0].Status = stepComplete
	rs.Steps[0].Attestation = "research done"
	rs.Steps[1].Status = stepComplete
	rs.Steps[1].Attestation = "provision done"
	rs.Steps[2].Status = stepInProgress
	rs.CurrentStep = 2
	rs.Plan = &RecipePlan{Framework: "laravel", Tier: RecipeTierMinimal}

	rs.ResetForIteration()

	// Research and provision should be preserved.
	if rs.Steps[0].Status != stepComplete {
		t.Errorf("research should remain complete, got %q", rs.Steps[0].Status)
	}
	if rs.Steps[1].Status != stepComplete {
		t.Errorf("provision should remain complete, got %q", rs.Steps[1].Status)
	}

	// Generate, deploy, finalize should reset.
	for _, idx := range []int{2, 3, 4} {
		if idx == 2 {
			if rs.Steps[idx].Status != stepInProgress {
				t.Errorf("step %d should be in_progress (first reset), got %q", idx, rs.Steps[idx].Status)
			}
		} else {
			if rs.Steps[idx].Status != stepPending {
				t.Errorf("step %d should be pending, got %q", idx, rs.Steps[idx].Status)
			}
		}
	}

	// Close should be preserved as pending (not reset).
	if rs.Steps[5].Status != stepPending {
		t.Errorf("close should remain pending, got %q", rs.Steps[5].Status)
	}

	if rs.CurrentStep != 2 {
		t.Errorf("expected CurrentStep 2, got %d", rs.CurrentStep)
	}
	if !rs.Active {
		t.Error("expected Active to be true after reset")
	}
	if rs.Plan == nil {
		t.Error("expected Plan to be preserved")
	}
}

func TestRecipeResetForIteration_Nil(t *testing.T) {
	t.Parallel()
	var rs *RecipeState
	rs.ResetForIteration() // should not panic
}

func TestRecipeBuildResponse_InProgress(t *testing.T) {
	t.Parallel()

	rs := NewRecipeState()
	rs.Steps[0].Status = stepInProgress

	resp := rs.BuildResponse("sess-abc", "create laravel recipe", 0, EnvLocal, nil)

	if resp.SessionID != "sess-abc" {
		t.Errorf("expected sessionId %q, got %q", "sess-abc", resp.SessionID)
	}
	if resp.Progress.Total != 6 {
		t.Errorf("expected total 6, got %d", resp.Progress.Total)
	}
	if resp.Progress.Completed != 0 {
		t.Errorf("expected completed 0, got %d", resp.Progress.Completed)
	}
	if resp.Current == nil {
		t.Fatal("expected Current to be set")
	}
	if resp.Current.Name != RecipeStepResearch {
		t.Errorf("expected current step %q, got %q", RecipeStepResearch, resp.Current.Name)
	}
	if len(resp.Current.Tools) == 0 {
		t.Error("expected tools to be populated")
	}
}

func TestRecipeBuildResponse_ShowcaseTier_ResearchGuidance(t *testing.T) {
	t.Parallel()

	// Showcase tier set at start (before plan exists) must deliver showcase-
	// specific research guidance that tells the agent NOT to load hello-world.
	rs := NewRecipeState()
	rs.Tier = RecipeTierShowcase
	rs.Steps[0].Status = stepInProgress

	resp := rs.BuildResponse("sess-sc", "create showcase", 0, EnvLocal, nil)

	guide := resp.Current.DetailedGuide
	if !strings.Contains(guide, "load ONE recipe only") {
		t.Error("showcase research guidance missing 'load ONE recipe only' instruction")
	}
	if !strings.Contains(guide, "Do NOT load the hello-world recipe") {
		t.Error("showcase research guidance missing hello-world prohibition")
	}
	// The base research section should also be included (framework identity, etc.)
	if !strings.Contains(guide, "Framework Identity") {
		t.Error("showcase research guidance missing base 'Framework Identity' section")
	}
}

func TestRecipeBuildResponse_MinimalTier_ResearchGuidance(t *testing.T) {
	t.Parallel()

	// Minimal tier should get the standard research guidance with hello-world loading.
	rs := NewRecipeState()
	rs.Tier = RecipeTierMinimal
	rs.Steps[0].Status = stepInProgress

	resp := rs.BuildResponse("sess-min", "create minimal", 0, EnvLocal, nil)

	guide := resp.Current.DetailedGuide
	if !strings.Contains(guide, "Hello-world") {
		t.Error("minimal research guidance missing hello-world reference loading")
	}
}

func TestRecipeBuildResponse_Complete(t *testing.T) {
	t.Parallel()

	rs := NewRecipeState()
	for i := range rs.Steps {
		rs.Steps[i].Status = stepComplete
		rs.Steps[i].Attestation = "completed step"
	}
	rs.CurrentStep = 6
	rs.Active = false

	resp := rs.BuildResponse("sess-xyz", "create recipe", 0, EnvLocal, nil)

	if resp.Progress.Completed != 6 {
		t.Errorf("expected completed 6, got %d", resp.Progress.Completed)
	}
	if resp.Current != nil {
		t.Error("expected Current to be nil when complete")
	}
	if resp.Message != "Recipe complete. All steps finished." {
		t.Errorf("unexpected message: %q", resp.Message)
	}
}

func TestRecipeStepDetails_StepCount(t *testing.T) {
	t.Parallel()

	if len(recipeStepDetails) != 6 {
		t.Errorf("expected 6 recipe step details, got %d", len(recipeStepDetails))
	}
}

func TestRecipeLookupDetail_Known(t *testing.T) {
	t.Parallel()

	tests := []struct {
		step     string
		wantTool string
	}{
		{RecipeStepResearch, "zerops_knowledge"},
		{RecipeStepProvision, "zerops_import"},
		{RecipeStepGenerate, "zerops_knowledge"},
		{RecipeStepDeploy, "zerops_deploy"},
		{RecipeStepFinalize, "zerops_workflow"},
		{RecipeStepClose, "zerops_workflow"},
	}

	for _, tt := range tests {
		t.Run(tt.step, func(t *testing.T) {
			t.Parallel()
			detail := lookupRecipeDetail(tt.step)
			if detail.Name != tt.step {
				t.Errorf("expected name %q, got %q", tt.step, detail.Name)
			}
			if !slices.Contains(detail.Tools, tt.wantTool) {
				t.Errorf("expected tool %q in %v", tt.wantTool, detail.Tools)
			}
		})
	}
}

func TestRecipeLookupDetail_Unknown(t *testing.T) {
	t.Parallel()

	detail := lookupRecipeDetail("nonexistent")
	if detail.Name != "" {
		t.Errorf("expected empty detail for unknown step, got %q", detail.Name)
	}
}

func TestRecipeBuildPriorContext_NoContext(t *testing.T) {
	t.Parallel()

	rs := NewRecipeState()
	ctx := rs.buildPriorContext()
	if ctx != nil {
		t.Error("expected nil prior context for first step")
	}
}

func TestRecipeBuildPriorContext_WithAttestations(t *testing.T) {
	t.Parallel()

	rs := NewRecipeState()
	rs.Steps[0].Status = stepComplete
	rs.Steps[0].Attestation = "research completed with all fields"
	rs.Steps[1].Status = stepInProgress
	rs.CurrentStep = 1

	ctx := rs.buildPriorContext()
	if ctx == nil {
		t.Fatal("expected non-nil prior context")
	}
	if ctx.Attestations["research"] != "research completed with all fields" {
		t.Errorf("expected full attestation for N-1 step, got %q", ctx.Attestations["research"])
	}
}
