package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/knowledge"
)

// v8.83 §response-size-fix — substep guide coverage.
//
// Context from the v22 session log audit:
//
//   - `complete substep=snapshot-dev`  → detailedGuide 40,820 bytes
//   - `complete substep=verify-stage`  → detailedGuide ~40 KB
//   - `complete substep=readmes`       → detailedGuide ~40 KB
//   - Meanwhile: `complete substep=start-processes` → detailedGuide 1,632 bytes
//
// Root cause: subStepToTopic's switch has no case for SubStepFeatureSweepDev /
// SubStepFeatureSweepStage, and buildGuide has no terminal-substep branch for
// "all substeps complete, agent ready for full-step check". In both cases the
// function falls through to resolveRecipeGuidance which emits the full
// ~40 KB step guide — pure redundancy because the agent already has that
// content in context from the step-transition response.
//
// These tests guard against recurrence: every substep in initSubSteps MUST
// have a non-empty subStepToTopic mapping, AND the all-substeps-complete
// state must produce a compact message instead of the full step guide.

// TestSubStepToTopic_EveryInitSubStepHasMapping is the meta-test that prevents
// silent fall-through recurrence. It enumerates every SubStep* constant that
// appears in a real initSubSteps output (across all plan shapes) and asserts
// subStepToTopic returns a non-empty topic for each one.
//
// If a future substep is added to initSubSteps without a corresponding entry
// in subStepToTopic, this test fails — forcing the developer to wire the
// mapping before ship.
func TestSubStepToTopic_EveryInitSubStepHasMapping(t *testing.T) {
	t.Parallel()

	type stepFixture struct {
		name  string
		step  string
		plans []*RecipePlan // plan shapes that produce this step's substeps
	}

	// Exercise initSubSteps across all plan shapes so we catch predicate-
	// gated substeps (e.g., showcase-only substeps).
	fixtures := []stepFixture{
		{
			name: "generate", step: RecipeStepGenerate,
			plans: []*RecipePlan{
				fixtureForShape(ShapeHelloWorld),
				fixtureForShape(ShapeBackendMinimal),
				fixtureForShape(ShapeFullStackShowcase),
				fixtureForShape(ShapeDualRuntimeShowcase),
			},
		},
		{
			name: "deploy", step: RecipeStepDeploy,
			plans: []*RecipePlan{
				fixtureForShape(ShapeHelloWorld),
				fixtureForShape(ShapeBackendMinimal),
				fixtureForShape(ShapeFullStackShowcase),
				fixtureForShape(ShapeDualRuntimeShowcase),
			},
		},
	}

	for _, fx := range fixtures {
		for _, plan := range fx.plans {
			substeps := initSubSteps(fx.step, plan)
			for _, ss := range substeps {
				topic := subStepToTopic(fx.step, ss.Name, plan)
				if topic == "" {
					t.Errorf("step=%q substep=%q (plan shape %s) has no subStepToTopic mapping — will fall through to the full ~40 KB step guide on completion response. Add a case in subStepToTopic's switch.",
						fx.step, ss.Name, planShape(plan))
				}
			}
		}
	}
}

// TestBuildSubStepGuide_FeatureSweepDev_ReturnsFocusedGuidance asserts the
// specific v22 regression: after completing snapshot-dev, the NEXT substep is
// feature-sweep-dev, and its guide was 40 KB because no mapping existed.
// With the mapping in place, the guide should be the feature-sweep-dev block
// (a few KB), not the full step monolith.
func TestBuildSubStepGuide_FeatureSweepDev_ReturnsFocusedGuidance(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	rs := &RecipeState{Plan: plan}
	guide := rs.buildSubStepGuide(RecipeStepDeploy, SubStepFeatureSweepDev)
	if guide == "" {
		t.Fatal("expected non-empty focused guidance for feature-sweep-dev substep; got empty (fall-through to full step guide)")
	}
	// The focused guide must actually contain feature-sweep content, not
	// the entire deploy section. Check for content-type contract mentions
	// that are feature-sweep-specific.
	for _, needle := range []string{"feature", "application/json"} {
		if !stringsContains(strings.ToLower(guide), strings.ToLower(needle)) {
			t.Errorf("focused guidance missing %q: first 200 chars = %q", needle, guide[:min(200, len(guide))])
		}
	}
}

// TestBuildSubStepGuide_FeatureSweepStage_ReturnsFocusedGuidance — parity
// for the stage sweep, which has the same fall-through bug.
func TestBuildSubStepGuide_FeatureSweepStage_ReturnsFocusedGuidance(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	rs := &RecipeState{Plan: plan}
	guide := rs.buildSubStepGuide(RecipeStepDeploy, SubStepFeatureSweepStage)
	if guide == "" {
		t.Fatal("expected non-empty focused guidance for feature-sweep-stage substep; got empty")
	}
	for _, needle := range []string{"feature", "application/json"} {
		if !stringsContains(strings.ToLower(guide), strings.ToLower(needle)) {
			t.Errorf("focused guidance missing %q", needle)
		}
	}
}

// TestBuildGuide_AllSubStepsComplete_ReturnsCompactMessage asserts the
// terminal-substep branch: when every deploy substep has been marked complete
// but the agent hasn't yet called `complete step=deploy`, the guide must NOT
// be the full ~40 KB step monolith. v22's `complete substep=readmes` response
// served that monolith because currentSubStepName() returned "" and the fall-
// through kicked in.
//
// Expected shape: a compact message telling the agent to run the full-step
// check — a few hundred bytes, NOT tens of thousands.
func TestBuildGuide_AllSubStepsComplete_ReturnsCompactMessage(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)

	// Construct a RecipeState with deploy step in progress and all its
	// substeps marked complete (CurrentSubStep past the end).
	rs := &RecipeState{
		Plan: plan,
		Steps: []RecipeStep{
			{Name: RecipeStepResearch, Status: stepComplete},
			{Name: RecipeStepProvision, Status: stepComplete},
			{Name: RecipeStepGenerate, Status: stepComplete},
			{
				Name:   RecipeStepDeploy,
				Status: stepInProgress,
				SubSteps: func() []RecipeSubStep {
					ss := deploySubSteps(plan)
					for i := range ss {
						ss[i].Status = stepComplete
					}
					return ss
				}(),
			},
			{Name: RecipeStepFinalize, Status: stepPending},
			{Name: RecipeStepClose, Status: stepPending},
		},
		CurrentStep: 3,
	}
	// Mark the CurrentSubStep index past the last substep (all complete).
	rs.Steps[3].CurrentSubStep = len(rs.Steps[3].SubSteps)

	guide := rs.buildGuide(RecipeStepDeploy, 0, nil)

	// The compact terminal message should be well under 4 KB. The full
	// deploy step monolith is ~40 KB — anything in between suggests
	// fall-through happened.
	const maxAcceptableBytes = 4096
	if len(guide) > maxAcceptableBytes {
		t.Errorf("all-substeps-complete guide is %d bytes — expected compact (<%d bytes). Fall-through to full step guide detected. Preview: %q",
			len(guide), maxAcceptableBytes, guide[:min(300, len(guide))])
	}

	// And it should explicitly tell the agent what's next: call complete
	// with step=deploy (no substep).
	for _, needle := range []string{"complete", "deploy"} {
		if !stringsContains(strings.ToLower(guide), strings.ToLower(needle)) {
			t.Errorf("compact terminal message missing %q: %q", needle, guide)
		}
	}
}

// TestBuildGuide_DeployStep_AcrossAllSubsteps_NoFallThroughToMonolith is
// the end-to-end size-regression guard. It walks every deploy substep in
// turn, marks it in-progress, and captures the size of the guide buildGuide
// serves. The goal is to detect FALL-THROUGH to the full step monolith —
// not to enforce an absolute size ceiling on focused topics (some topics
// are legitimately 10-18 KB because they carry feature-rich content like
// the readme-fragments template or the feature-subagent-brief).
//
// The heuristic: a substep guide must be substantially smaller than the
// full-step monolith. If substep_size >= 50% of full_step_size, the switch
// fell through. v22 observed 3 substep responses at 40+ KB where the full
// step guide was ~40 KB — classic 100% fall-through.
//
// Secondary absolute ceiling at 25 KB catches the case where both substep
// and full-step grow together and the relative test would pass trivially.
func TestBuildGuide_DeployStep_AcrossAllSubsteps_NoFallThroughToMonolith(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)

	// Measure the full-step monolith size for reference.
	fullStepGuide := resolveRecipeGuidance(RecipeStepDeploy, RecipeTierShowcase, plan)
	fullStepSize := len(fullStepGuide)
	if fullStepSize < 20000 {
		t.Fatalf("expected full-step deploy guide >= 20 KB (for the relative test to be meaningful), got %d bytes", fullStepSize)
	}
	t.Logf("full-step deploy guide: %d bytes (reference baseline)", fullStepSize)

	// Build a state in deploy step with all substeps present.
	rs := &RecipeState{
		Plan: plan,
		Steps: []RecipeStep{
			{Name: RecipeStepResearch, Status: stepComplete},
			{Name: RecipeStepProvision, Status: stepComplete},
			{Name: RecipeStepGenerate, Status: stepComplete},
			{
				Name:     RecipeStepDeploy,
				Status:   stepInProgress,
				SubSteps: deploySubSteps(plan),
			},
			{Name: RecipeStepFinalize, Status: stepPending},
			{Name: RecipeStepClose, Status: stepPending},
		},
		CurrentStep: 3,
	}

	// Fall-through detection threshold: if a substep guide is ≥ 50% the
	// size of the full-step monolith, it's almost certainly the full
	// monolith being served (possibly with minor additions/deletions).
	fallThroughThreshold := fullStepSize / 2
	// Absolute ceiling: no substep's focused guide should exceed 25 KB.
	const absoluteCeilingBytes = 25 * 1024

	var offenders []string
	for i := range rs.Steps[3].SubSteps {
		rs.Steps[3].CurrentSubStep = i
		for j := range rs.Steps[3].SubSteps {
			switch {
			case j < i:
				rs.Steps[3].SubSteps[j].Status = stepComplete
			case j == i:
				rs.Steps[3].SubSteps[j].Status = stepInProgress
			default:
				rs.Steps[3].SubSteps[j].Status = stepPending
			}
		}
		guide := rs.buildGuide(RecipeStepDeploy, 0, nil)
		size := len(guide)
		subStepName := rs.Steps[3].SubSteps[i].Name
		t.Logf("substep %-22s → %5d bytes (%.0f%% of full-step)",
			subStepName, size, 100*float64(size)/float64(fullStepSize))
		if size >= fallThroughThreshold {
			offenders = append(offenders, fmt.Sprintf(
				"%s: %d bytes (%.0f%% of full-step — FALL-THROUGH)",
				subStepName, size, 100*float64(size)/float64(fullStepSize),
			))
		} else if size > absoluteCeilingBytes {
			offenders = append(offenders, fmt.Sprintf(
				"%s: %d bytes (exceeds %d-byte absolute ceiling)",
				subStepName, size, absoluteCeilingBytes,
			))
		}
	}
	if len(offenders) > 0 {
		t.Errorf("substep guides failed fall-through detection (full-step reference: %d bytes, threshold: %d bytes):\n  %s",
			fullStepSize, fallThroughThreshold, strings.Join(offenders, "\n  "))
	}
}

// TestBuildGuide_V22Regression_SpecificSubsteps targets the three specific
// substeps that v22 served as 40 KB monoliths: snapshot-dev, verify-stage,
// and readmes-when-complete. This is a direct regression test — if any of
// these three re-surfaces the old fall-through behavior, this test catches
// it immediately with a specific error.
func TestBuildGuide_V22Regression_SpecificSubsteps(t *testing.T) {
	// Not t.Parallel(): subtests share a mutable RecipeState via closure
	// and would race under parallel execution.
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	fullStep := len(resolveRecipeGuidance(RecipeStepDeploy, RecipeTierShowcase, plan))

	type fixture struct {
		name          string
		currentSub    int // index into deploySubSteps after completing substep at index-1
		allComplete   bool
		maxBytesRel   float64 // max acceptable size as fraction of full-step
		expectCompact bool    // true if we expect the compact terminal message
	}

	substeps := deploySubSteps(plan)
	findIndex := func(name string) int {
		for i, ss := range substeps {
			if ss.Name == name {
				return i
			}
		}
		t.Fatalf("substep %q not found in deploy substeps", name)
		return -1
	}

	tests := []fixture{
		{
			// After snapshot-dev completes, current = feature-sweep-dev.
			// v22 served 40 KB here (~100% of full-step). Must now be
			// < 50% and ideally just the feature-sweep-dev topic.
			name:        "after-snapshot-dev (current=feature-sweep-dev)",
			currentSub:  findIndex(SubStepFeatureSweepDev),
			maxBytesRel: 0.5,
		},
		{
			// After verify-stage completes, current = feature-sweep-stage.
			// Same 40 KB monolith in v22.
			name:        "after-verify-stage (current=feature-sweep-stage)",
			currentSub:  findIndex(SubStepFeatureSweepStage),
			maxBytesRel: 0.5,
		},
		{
			// After readmes completes, all substeps done. v22 served
			// 40 KB (the full monolith re-delivered). Must now be the
			// compact terminal message, < 4 KB.
			name:          "after-readmes (all complete)",
			allComplete:   true,
			maxBytesRel:   0.1,
			expectCompact: true,
		},
	}

	rs := &RecipeState{
		Plan: plan,
		Steps: []RecipeStep{
			{Name: RecipeStepResearch, Status: stepComplete},
			{Name: RecipeStepProvision, Status: stepComplete},
			{Name: RecipeStepGenerate, Status: stepComplete},
			{
				Name:     RecipeStepDeploy,
				Status:   stepInProgress,
				SubSteps: append([]RecipeSubStep(nil), substeps...),
			},
			{Name: RecipeStepFinalize, Status: stepPending},
			{Name: RecipeStepClose, Status: stepPending},
		},
		CurrentStep: 3,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No t.Parallel() — these subtests share rs.Steps[3].SubSteps
			// via the enclosing scope and mutate CurrentSubStep per case.
			if tt.allComplete {
				rs.Steps[3].CurrentSubStep = len(rs.Steps[3].SubSteps)
				for i := range rs.Steps[3].SubSteps {
					rs.Steps[3].SubSteps[i].Status = stepComplete
				}
			} else {
				rs.Steps[3].CurrentSubStep = tt.currentSub
				for i := range rs.Steps[3].SubSteps {
					switch {
					case i < tt.currentSub:
						rs.Steps[3].SubSteps[i].Status = stepComplete
					case i == tt.currentSub:
						rs.Steps[3].SubSteps[i].Status = stepInProgress
					default:
						rs.Steps[3].SubSteps[i].Status = stepPending
					}
				}
			}

			guide := rs.buildGuide(RecipeStepDeploy, 0, nil)
			size := len(guide)
			maxBytes := int(tt.maxBytesRel * float64(fullStep))
			t.Logf("%s → %d bytes (full-step baseline %d, ceiling %d)", tt.name, size, fullStep, maxBytes)
			if size > maxBytes {
				t.Errorf("%s: %d bytes exceeds ceiling %d (%.0f%% of full-step)",
					tt.name, size, maxBytes, 100*float64(size)/float64(fullStep))
			}
			if tt.expectCompact {
				// Must name the step and next-action clearly.
				for _, needle := range []string{"complete", "deploy"} {
					if !strings.Contains(strings.ToLower(guide), needle) {
						t.Errorf("compact message missing %q: %q", needle, guide)
					}
				}
			}
		})
	}
}

// --- helpers ---

// planShape returns a short label for diagnostics.
func planShape(p *RecipePlan) string {
	if p == nil {
		return "<nil>"
	}
	return p.Framework + "/" + p.Tier
}

// Ensure unused knowledge.Provider import doesn't break lint if helpers
// evolve to need it. Current tests pass nil.
var _ knowledge.Provider = nil
