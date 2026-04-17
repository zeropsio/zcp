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

// TestBuildGuide_DeployStep_AcrossAllSubsteps_SizeCeiling is the end-to-end
// size-regression guard. It walks every deploy substep in turn and asserts
// that its focused guide stays under an absolute ceiling.
//
// v22 / v8.83 history: three substeps (snapshot-dev, verify-stage, readmes-
// when-complete) served 40+ KB because subStepToTopic had no case for them
// and buildGuide fell through to resolveRecipeGuidance. v8.83 fixed those
// cases; v8.84 additionally pruned step-entry eager topics so the step-entry
// guide itself is now ~7 KB (down from ~43 KB).
//
// Absolute ceiling rationale: the heaviest legitimate substep guide is
// `readmes`, which carries readme-fragments (~18 KB primary) plus content-
// quality-overview (~4 KB sub-step eager) = ~23 KB. Everything else is
// single-topic and under 15 KB. 30 KB headroom accommodates growth of
// those feature-rich topics without hiding accidental monolith delivery.
// A regression to the full ~43 KB step monolith would blow well past
// this ceiling and fire the test.
func TestBuildGuide_DeployStep_AcrossAllSubsteps_SizeCeiling(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)

	fullStepGuide := resolveRecipeGuidance(RecipeStepDeploy, RecipeTierShowcase, plan)
	t.Logf("deploy step-entry guide: %d bytes (v8.84 target < 20 KB)", len(fullStepGuide))

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

	// Absolute ceiling: no substep's focused guide should exceed 30 KB.
	// The heaviest legitimate case (readmes) carries ~23 KB of primary +
	// sub-step-eager content; 30 KB leaves headroom for the skeleton
	// wrapping and small growth of the feature-rich topics. A regression
	// to the full ~43 KB monolith would blow past this.
	const absoluteCeilingBytes = 30 * 1024

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
		t.Logf("substep %-22s → %5d bytes", subStepName, size)
		if size > absoluteCeilingBytes {
			offenders = append(offenders, fmt.Sprintf(
				"%s: %d bytes (exceeds %d-byte absolute ceiling — possible fall-through to step monolith)",
				subStepName, size, absoluteCeilingBytes,
			))
		}
	}
	if len(offenders) > 0 {
		t.Errorf("substep guides exceeded size ceiling:\n  %s", strings.Join(offenders, "\n  "))
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

// TestResolveDeployGuidance_StepEntrySize_UnderBudget — v8.84 response-size
// fix. Asserts the deploy step-entry body (skeleton + step-entry eager
// topics) stays well under Claude Code's persist-to-disk threshold (~50 KB).
//
// v22 / v8.82 regression: four topics were flagged Eager and all landed at
// step entry — subagent-brief (~14 KB), readme-fragments (~18 KB), where-
// commands-run (~5 KB), content-quality-overview (~4 KB). Combined with
// the skeleton (~3 KB), the detailedGuide hit ~43 KB; with the JSON
// envelope, the complete response crossed 50.9 KB, triggering persist-to-
// disk. v8.84 moved subagent-brief / readme-fragments to their sub-step
// focus (already served there via subStepToTopic) and content-quality-
// overview to SubStepReadmes. Only where-commands-run stays at step entry
// — the very first deploy sub-step starts dev servers over SSH, so the
// teaching must precede any sub-step work.
//
// Budget: the deploy step-entry guide must be under 20 KB. This leaves
// plenty of headroom for the skeleton + where-commands-run + knowledge
// injection + JSON envelope, and a comfortable gap from the 50 KB wall.
func TestResolveDeployGuidance_StepEntrySize_UnderBudget(t *testing.T) {
	t.Parallel()

	const maxStepEntryBytes = 20 * 1024

	for _, shape := range []RecipeShape{
		ShapeHelloWorld, ShapeBackendMinimal,
		ShapeFullStackShowcase, ShapeDualRuntimeShowcase,
	} {
		plan := fixtureForShape(shape)
		guide := resolveRecipeGuidance(RecipeStepDeploy, plan.Tier, plan)
		size := len(guide)
		label := planShape(plan)
		t.Logf("shape %-24s deploy step-entry → %5d bytes", label, size)
		if size > maxStepEntryBytes {
			t.Errorf("shape %q deploy step-entry is %d bytes (max %d). Check for newly-promoted EagerStepEntry topics — v8.84 policy is sub-step scope for topics whose teaching is sub-step-specific.",
				label, size, maxStepEntryBytes)
		}
	}
}

// TestResolveDeployGuidance_StepEntryOmitsSubStepTopics — v8.84 scope-
// shift guard. The step-entry guide MUST NOT contain the body of topics
// re-scoped to sub-step entry. If a refactor accidentally re-promotes one
// of them (or adds a new Eager: true without picking an EagerAt), this
// test flags it before the envelope grows back past 50 KB.
//
// The anchors chosen are unique tokens from each topic's body that do not
// appear elsewhere in the step-entry guide.
func TestResolveDeployGuidance_StepEntryOmitsSubStepTopics(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	guide := resolveRecipeGuidance(RecipeStepDeploy, plan.Tier, plan)

	type offender struct {
		topic  string
		anchor string
	}
	// Each anchor is taken from the body of an eager-moved topic. A match
	// in the step-entry guide means that topic's body is being inlined at
	// step entry — regression on v8.84's scope shift.
	cases := []offender{
		// subagent-brief body — the "Installed-package verification rule"
		// is unique to the feature-sub-agent dispatch block.
		{topic: "subagent-brief", anchor: "Installed-package verification rule"},
		// readme-fragments body — byte-literal marker template.
		{topic: "readme-fragments", anchor: "#ZEROPS_EXTRACT_START"},
		// content-quality-overview body — "six-surface teaching system"
		// is the distinctive heading.
		{topic: "content-quality-overview", anchor: "six-surface teaching"},
	}
	for _, c := range cases {
		if strings.Contains(strings.ToLower(guide), strings.ToLower(c.anchor)) {
			t.Errorf("step-entry guide still contains body of sub-step-scoped topic %q (anchor %q). v8.84 moved this topic off step entry; a refactor may have re-promoted it.",
				c.topic, c.anchor)
		}
	}
}

// TestBuildSubStepGuide_Readmes_IncludesBothPrimaryAndEager — v8.84 positive
// case. At the `readmes` sub-step, the focused guide must carry BOTH the
// primary topic (readme-fragments — byte-literal marker template) AND the
// sub-step-eager topic (content-quality-overview — six-surface map). The
// two are complementary: the map orients, the template specifies.
func TestBuildSubStepGuide_Readmes_IncludesBothPrimaryAndEager(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	rs := &RecipeState{Plan: plan}
	guide := rs.buildSubStepGuide(RecipeStepDeploy, SubStepReadmes)
	if guide == "" {
		t.Fatal("expected non-empty readmes sub-step guide")
	}
	// Primary topic anchor: fragment marker template.
	if !strings.Contains(guide, "#ZEROPS_EXTRACT_START") {
		t.Error("readmes sub-step guide missing readme-fragments primary body (anchor: #ZEROPS_EXTRACT_START)")
	}
	// Sub-step-eager topic anchor: six-surface teaching system.
	if !strings.Contains(strings.ToLower(guide), "six-surface") {
		t.Error("readmes sub-step guide missing content-quality-overview eager body (anchor: six-surface)")
	}
}

// TestInjectEagerTopicsForSubStep_ExcludeIDDedup — v8.84 dedup guard. If a
// topic is BOTH the sub-step's primary focus AND marked EagerAt=<thisSubStep>,
// passing its ID as excludeID must prevent double-inline. This exercises the
// contract that keeps buildSubStepGuide from serving the same body twice.
func TestInjectEagerTopicsForSubStep_ExcludeIDDedup(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	withDup := InjectEagerTopicsForSubStep(recipeDeployTopics, plan, SubStepReadmes, "")
	withoutDup := InjectEagerTopicsForSubStep(recipeDeployTopics, plan, SubStepReadmes, "content-quality-overview")
	if withDup == withoutDup {
		t.Error("exclude-ID dedup did not remove content-quality-overview; withDup and withoutDup should differ")
	}
	if strings.Contains(withoutDup, "six-surface teaching") {
		t.Error("excludeID did not remove content-quality-overview from injection")
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
