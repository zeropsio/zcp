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

// TestRecipeCompleteStep_ShowcaseDeploySubStepGate verifies that showcase-tier
// recipes cannot complete the deploy step without first going through each
// sub-step (including the feature sub-agent dispatch). v11 and v12 both shipped
// scaffold-quality output because the main agent skipped step 4b entirely —
// the sub-step was a bullet in guidance text, not a forcing function.
// TestRecipeCompleteStep_ShowcaseCloseSubStepGate — v19 regression guard.
// v18 and v19 both shipped with only `deploy.browser` firing; `close.browser`
// was prose-only in recipe.md and the agent skipped it because close-step
// complete didn't enforce it. This test locks in the enforcement shape:
// for showcase recipes, close step complete must fail when the close
// sub-steps (code-review + close-browser-walk) are not attested, and must
// pass when both are. Minimal recipes skip enforcement — they have no
// feature dashboard to walk.
func TestRecipeCompleteStep_ShowcaseCloseSubStepGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		plan         *RecipePlan
		initSubSteps bool
		completeAll  bool
		wantErr      bool
		errContains  string
	}{
		{
			name:         "showcase close without any sub-steps → blocked",
			plan:         &RecipePlan{Tier: RecipeTierShowcase},
			initSubSteps: false,
			completeAll:  false,
			wantErr:      true,
			errContains:  "sub-step",
		},
		{
			name:         "showcase close with sub-steps pending → blocked",
			plan:         &RecipePlan{Tier: RecipeTierShowcase},
			initSubSteps: true,
			completeAll:  false,
			wantErr:      true,
			errContains:  "pending sub-step",
		},
		{
			name:         "showcase close with all sub-steps complete → passes",
			plan:         &RecipePlan{Tier: RecipeTierShowcase},
			initSubSteps: true,
			completeAll:  true,
			wantErr:      false,
		},
		{
			name:         "minimal close without sub-steps → passes (no enforcement)",
			plan:         &RecipePlan{Tier: RecipeTierMinimal},
			initSubSteps: false,
			completeAll:  false,
			wantErr:      false,
		},
		{
			name:         "nil plan close → passes (test shape, no enforcement)",
			plan:         nil,
			initSubSteps: false,
			completeAll:  false,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rs := NewRecipeState()
			rs.Plan = tt.plan
			// Advance to close step (index 5 — the last step).
			for i := range 5 {
				rs.Steps[i].Status = stepComplete
				rs.Steps[i].Attestation = "prior step"
			}
			rs.CurrentStep = 5
			rs.Steps[5].Status = stepInProgress

			if tt.initSubSteps {
				rs.Steps[5].SubSteps = initSubSteps(RecipeStepClose, tt.plan)
				if tt.completeAll {
					for i := range rs.Steps[5].SubSteps {
						rs.Steps[5].SubSteps[i].Status = stepComplete
						rs.Steps[5].SubSteps[i].Attestation = "complete"
					}
					rs.Steps[5].CurrentSubStep = len(rs.Steps[5].SubSteps)
				}
			}

			err := rs.CompleteStep(RecipeStepClose, "attestation for close step")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestRecipeCloseSubSteps_ShowcaseIncludesBrowserWalk — structural assertion
// that closeSubSteps returns exactly the sub-steps the v19 post-mortem said
// should fire: the static code review (1a) and the close-time browser walk
// (1b). Locked in so a future refactor can't silently drop either.
func TestRecipeCloseSubSteps_ShowcaseIncludesBrowserWalk(t *testing.T) {
	t.Parallel()

	plan := &RecipePlan{Tier: RecipeTierShowcase}
	substeps := initSubSteps(RecipeStepClose, plan)
	if len(substeps) == 0 {
		t.Fatal("expected close sub-steps for showcase, got none")
	}
	names := make(map[string]bool, len(substeps))
	for _, ss := range substeps {
		names[ss.Name] = true
	}
	if !names[SubStepCloseReview] {
		t.Errorf("expected close sub-step %q in sequence", SubStepCloseReview)
	}
	if !names[SubStepCloseBrowserWalk] {
		t.Errorf("expected close sub-step %q in sequence — this is the v18/v19 regression guard", SubStepCloseBrowserWalk)
	}
	// Code review must run before the browser walk — the walk re-verifies
	// after any redeploy caused by code-review fixes.
	reviewIdx := -1
	walkIdx := -1
	for i, ss := range substeps {
		if ss.Name == SubStepCloseReview {
			reviewIdx = i
		}
		if ss.Name == SubStepCloseBrowserWalk {
			walkIdx = i
		}
	}
	if reviewIdx >= 0 && walkIdx >= 0 && reviewIdx >= walkIdx {
		t.Errorf("close sub-step order wrong: review (index %d) must come before browser walk (index %d)", reviewIdx, walkIdx)
	}
	// First sub-step should be in-progress after init.
	if substeps[0].Status != stepInProgress {
		t.Errorf("expected first close sub-step in-progress, got %q", substeps[0].Status)
	}
}

// TestRecipeCloseSubSteps_ExactlyThreeAutonomousSubSteps — v8.97 Fix 2
// bar + C-7.5 (2026-04-20 refinement). Close contains editorial-review +
// code-review + close-browser-walk — no publish, no export, no additional
// sub-steps. Publish is a post-workflow CLI operation; export is gated
// server-side on close=complete (Fix 1) but is not itself a workflow
// sub-step. The v32 close-skip failure class depended on the agent
// reading "publish is gated" and interpreting it as "close itself is
// gated" — removing publish from workflow state closes that ambiguity at
// the root. C-7.5 adds editorial-review as the FIRST close substep so
// its reclassification findings + inline fixes land before code-review
// grades the deliverable.
func TestRecipeCloseSubSteps_ExactlyThreeAutonomousSubSteps(t *testing.T) {
	t.Parallel()

	plan := &RecipePlan{Tier: RecipeTierShowcase}
	got := initSubSteps(RecipeStepClose, plan)
	want := []string{SubStepEditorialReview, SubStepCloseReview, SubStepCloseBrowserWalk}
	if len(got) != len(want) {
		t.Fatalf("expected exactly %d close sub-steps (%v), got %d (%+v)", len(want), want, len(got), got)
	}
	for i, ss := range got {
		if ss.Name != want[i] {
			t.Errorf("close sub-step[%d]: expected %q, got %q — Fix 2 bar + C-7.5 ordering", i, want[i], ss.Name)
		}
		// Reject any substep containing publish or export vocabulary — the
		// server-side guard against Fix 2 regressing into a three-substep
		// close again (publish/export were the banned third substep; the
		// allowed third substep is editorial-review).
		for _, banned := range []string{"publish", "export"} {
			if strings.Contains(ss.Name, banned) {
				t.Errorf("close sub-step[%d] %q contains banned vocabulary %q — publish/export are not workflow sub-steps", i, ss.Name, banned)
			}
		}
	}
}

// TestHandleComplete_CloseStepReturnsPostCompletionGuidance — v8.97 Fix 2,
// updated for v8.103. The close-completion response populates
// PostCompletionSummary and exactly two NextSteps entries: export at [0]
// and publish at [1], BOTH strictly user-gated (never auto-run).
func TestHandleComplete_CloseStepReturnsPostCompletionGuidance(t *testing.T) {
	t.Parallel()

	plan := &RecipePlan{Slug: "test-showcase", Tier: RecipeTierShowcase}
	summary, nextSteps := buildClosePostCompletion(plan, "/var/www/zcprecipator/test-showcase")
	if summary == "" {
		t.Fatal("expected PostCompletionSummary populated on close completion")
	}
	if !strings.Contains(summary, "code-review") || !strings.Contains(summary, "close-browser-walk") {
		t.Errorf("summary must name both sub-steps; got %q", summary)
	}
	if len(nextSteps) != 2 {
		t.Fatalf("expected exactly 2 NextSteps entries (export + publish), got %d: %+v", len(nextSteps), nextSteps)
	}
	if !strings.Contains(nextSteps[0], "zcp sync recipe export") {
		t.Errorf("NextSteps[0] must name export CLI command; got %q", nextSteps[0])
	}
	if !strings.Contains(nextSteps[1], "zcp sync recipe publish") {
		t.Errorf("NextSteps[1] must name publish CLI command; got %q", nextSteps[1])
	}
	if !strings.Contains(nextSteps[1], "test-showcase") {
		t.Errorf("NextSteps[1] must embed slug; got %q", nextSteps[1])
	}
}

// TestHandleComplete_CloseStepPostCompletionBothUserGated — v8.103.
// BOTH NextSteps entries (export at [0], publish at [1]) must be flagged
// as user-gated with "ON REQUEST ONLY" wording and the explicit "do NOT
// run unprompted" instruction. v8.98 Fix B originally framed export as
// autonomous; real usage surfaced the user's objection: the workflow is
// done at close, nothing runs after unless the user asks.
func TestHandleComplete_CloseStepPostCompletionBothUserGated(t *testing.T) {
	t.Parallel()

	plan := &RecipePlan{Slug: "test-showcase", Tier: RecipeTierShowcase}
	_, nextSteps := buildClosePostCompletion(plan, "/var/www/zcprecipator/test-showcase")
	if len(nextSteps) != 2 {
		t.Fatalf("expected exactly 2 NextSteps entries, got %d", len(nextSteps))
	}

	// Both entries must name their command AND flag user-gated.
	if !strings.Contains(nextSteps[0], "zcp sync recipe export") {
		t.Errorf("NextSteps[0] must name export; got %q", nextSteps[0])
	}
	if !strings.Contains(nextSteps[1], "zcp sync recipe publish") {
		t.Errorf("NextSteps[1] must name publish; got %q", nextSteps[1])
	}
	for i, step := range nextSteps {
		if !strings.Contains(step, "ON REQUEST ONLY") {
			t.Errorf("NextSteps[%d] must flag ON REQUEST ONLY; got %q", i, step)
		}
		if !strings.Contains(step, "NOT run unprompted") {
			t.Errorf("NextSteps[%d] must say \"NOT run unprompted\"; got %q", i, step)
		}
		// Regression guard against v8.98 Fix B's autonomous framing.
		if strings.Contains(step, "autonomous") {
			t.Errorf("NextSteps[%d] must NOT claim autonomous execution (v8.103 regression guard); got %q", i, step)
		}
	}
}

// TestHandleComplete_CloseStepSummaryHasNoAutomaticClaims — v8.103.
// The summary must not imply that ANYTHING runs automatically after
// close — not export, not publish. Regression guard against both the
// v8.97 "Export runs automatically" wording and v8.98 Fix B's "run
// export autonomously" instruction. Neither should reappear.
func TestHandleComplete_CloseStepSummaryHasNoAutomaticClaims(t *testing.T) {
	t.Parallel()

	plan := &RecipePlan{Slug: "test-showcase", Tier: RecipeTierShowcase}
	summary, _ := buildClosePostCompletion(plan, "/var/www/zcprecipator/test-showcase")
	for _, banned := range []string{"Export runs automatically", "run export autonomously", "autonomously"} {
		if strings.Contains(summary, banned) {
			t.Errorf("summary must NOT contain %q — nothing runs after close without user ask (v8.103)", banned)
		}
	}
	// Must explicitly say the user decides.
	if !strings.Contains(summary, "user explicitly asks") {
		t.Errorf("summary must name \"user explicitly asks\" as the gate for post-workflow commands; got %q", summary)
	}
}

// TestRecipeCloseSubSteps_MinimalEmpty — regression guard that minimal
// recipes skip close sub-step enforcement entirely. Minimal recipes don't
// have a feature dashboard so a browser walk has nothing to observe, and
// their close step is historically lightweight.
func TestRecipeCloseSubSteps_MinimalEmpty(t *testing.T) {
	t.Parallel()

	plan := &RecipePlan{Tier: RecipeTierMinimal}
	if substeps := initSubSteps(RecipeStepClose, plan); len(substeps) != 0 {
		t.Errorf("expected minimal close sub-steps empty, got %+v", substeps)
	}
	plan = &RecipePlan{Tier: RecipeTierHelloWorld}
	if substeps := initSubSteps(RecipeStepClose, plan); len(substeps) != 0 {
		t.Errorf("expected hello-world close sub-steps empty, got %+v", substeps)
	}
}

func TestRecipeCompleteStep_ShowcaseDeploySubStepGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		plan         *RecipePlan
		initSubSteps bool
		completeAll  bool
		wantErr      bool
		errContains  string
	}{
		{
			name:         "showcase deploy without any sub-steps → blocked",
			plan:         &RecipePlan{Tier: RecipeTierShowcase},
			initSubSteps: false,
			completeAll:  false,
			wantErr:      true,
			errContains:  "sub-step",
		},
		{
			name:         "showcase deploy with sub-steps pending → blocked",
			plan:         &RecipePlan{Tier: RecipeTierShowcase},
			initSubSteps: true,
			completeAll:  false,
			wantErr:      true,
			errContains:  "pending sub-step",
		},
		{
			name:         "showcase deploy with all sub-steps complete → passes",
			plan:         &RecipePlan{Tier: RecipeTierShowcase},
			initSubSteps: true,
			completeAll:  true,
			wantErr:      false,
		},
		{
			name:         "minimal deploy without sub-steps → passes (no enforcement)",
			plan:         &RecipePlan{Tier: RecipeTierMinimal},
			initSubSteps: false,
			completeAll:  false,
			wantErr:      false,
		},
		{
			name:         "nil plan deploy → passes (test shape, no enforcement)",
			plan:         nil,
			initSubSteps: false,
			completeAll:  false,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rs := NewRecipeState()
			rs.Plan = tt.plan
			// Advance to deploy step (index 3).
			for i := range 3 {
				rs.Steps[i].Status = stepComplete
				rs.Steps[i].Attestation = "prior step"
			}
			rs.CurrentStep = 3
			rs.Steps[3].Status = stepInProgress

			if tt.initSubSteps {
				rs.Steps[3].SubSteps = initSubSteps(RecipeStepDeploy, tt.plan)
				if tt.completeAll {
					for i := range rs.Steps[3].SubSteps {
						rs.Steps[3].SubSteps[i].Status = stepComplete
						rs.Steps[3].SubSteps[i].Attestation = "complete"
					}
					rs.Steps[3].CurrentSubStep = len(rs.Steps[3].SubSteps)
				}
			}

			err := rs.CompleteStep(RecipeStepDeploy, "attestation for deploy step")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
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
	// specific research guidance that tells the agent NOT to load hello-world
	// and includes the base research section (worker decision, targets).
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
	// The base research-minimal section should also be included (type table,
	// target fields, scaffold preservation). Post-reshuffle the old
	// "Framework Identity" subsection is gone — the field descriptions live
	// on the tool schema directly. Assert the base section's anchors.
	if !strings.Contains(guide, "Worker codebase decision") {
		t.Error("showcase research guidance missing 'Worker codebase decision' block")
	}
	if !strings.Contains(guide, "Scaffold preservation") {
		t.Error("showcase research guidance missing base 'Scaffold preservation' rule from research-minimal")
	}
}

func TestRecipeBuildResponse_MinimalTier_ResearchGuidance(t *testing.T) {
	t.Parallel()

	// Minimal tier should get the compressed base research guidance.
	rs := NewRecipeState()
	rs.Tier = RecipeTierMinimal
	rs.Steps[0].Status = stepInProgress

	resp := rs.BuildResponse("sess-min", "create minimal", 0, EnvLocal, nil)

	guide := resp.Current.DetailedGuide
	// Post-reshuffle: the Reference Loading block and Framework Identity /
	// Build & Deploy Pipeline / etc. subsections are gone. Assert the
	// content that DID survive the trim: the recipe-type table and the
	// scaffold preservation rule.
	if !strings.Contains(guide, "Runtime hello world") {
		t.Error("minimal research guidance missing recipe-type table")
	}
	if !strings.Contains(guide, "Scaffold preservation") {
		t.Error("minimal research guidance missing scaffold preservation rule")
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
