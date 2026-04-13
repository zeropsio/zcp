package workflow

import (
	"testing"
)

// TestGenerateSubSteps_V14Order locks the v14 generate sub-step sequence:
// scaffold → app-code → smoke-test → zerops-yaml. The zerops.yaml is written
// LAST so buildCommands / cache / deployFiles derive from an install flow the
// agent has already validated under smoke-test rather than research-time
// assumptions. README is deliberately absent from generate; it moves to the
// post-deploy readmes sub-step where gotchas can narrate lived experience.
func TestGenerateSubSteps_V14Order(t *testing.T) {
	t.Parallel()
	got := generateSubSteps()
	want := []string{
		SubStepScaffold,
		SubStepAppCode,
		SubStepSmokeTest,
		SubStepZeropsYAML,
	}
	if len(got) != len(want) {
		t.Fatalf("generate substep count = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("substep[%d] = %q, want %q (full sequence: %+v)", i, got[i].Name, name, got)
		}
	}
	for _, ss := range got {
		if ss.Name == SubStepReadmes || ss.Name == "readme" {
			t.Errorf("generate must not include a readme sub-step (found %q); readmes move to post-deploy", ss.Name)
		}
	}
}

// TestDeploySubSteps_V14ShowcaseOrder locks the v14 showcase deploy sequence:
// dev-deploy → start-procs → verify-dev → init-commands → subagent →
// snapshot-dev → browser-walk → cross-deploy → verify-stage → readmes.
// Three critical invariants:
//   - snapshot-dev sits IMMEDIATELY after subagent (durability: persist the
//     feature sub-agent's output to the deployed artifact before any
//     subsequent step that could end the dev container)
//   - readmes is LAST (narrate gotchas from the debug rounds just experienced)
//   - cross-deploy happens AFTER snapshot-dev and browser-walk so stage never
//     runs without a verified dev snapshot behind it
func TestDeploySubSteps_V14ShowcaseOrder(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{Tier: RecipeTierShowcase, Targets: []RecipeTarget{
		{Hostname: "api", Type: "nodejs@22", Role: RecipeRoleAPI},
		{Hostname: "app", Type: "static", DevBase: "nodejs@22", Role: RecipeRoleApp},
	}}
	got := deploySubSteps(plan)
	want := []string{
		SubStepDeployDev,
		SubStepStartProcs,
		SubStepVerifyDev,
		SubStepInitCommands,
		SubStepSubagent,
		SubStepSnapshotDev,
		SubStepBrowserWalk,
		SubStepCrossDeploy,
		SubStepVerifyStage,
		SubStepReadmes,
	}
	if len(got) != len(want) {
		t.Fatalf("deploy substep count = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("substep[%d] = %q, want %q (full sequence: %+v)", i, got[i].Name, name, got)
		}
	}

	// Structural invariants that matter beyond exact ordering.
	idxSubagent := indexOfSubStep(got, SubStepSubagent)
	idxSnapshot := indexOfSubStep(got, SubStepSnapshotDev)
	idxCrossDeploy := indexOfSubStep(got, SubStepCrossDeploy)
	idxReadmes := indexOfSubStep(got, SubStepReadmes)
	if idxSnapshot != idxSubagent+1 {
		t.Errorf("snapshot-dev must sit immediately after subagent (durability): subagent=%d snapshot=%d", idxSubagent, idxSnapshot)
	}
	if idxCrossDeploy <= idxSnapshot {
		t.Errorf("cross-deploy must happen after snapshot-dev: snapshot=%d cross-deploy=%d", idxSnapshot, idxCrossDeploy)
	}
	if idxReadmes != len(got)-1 {
		t.Errorf("readmes must be the final sub-step: idx=%d, last=%d", idxReadmes, len(got)-1)
	}
}

// TestDeploySubSteps_V14MinimalOrder — minimal tier keeps no feature sub-agent
// or snapshot step. readmes still runs at the end so the narrate-from-
// experience invariant is tier-independent.
func TestDeploySubSteps_V14MinimalOrder(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{Tier: RecipeTierMinimal, Targets: []RecipeTarget{
		{Hostname: "app", Type: "nodejs@22"},
	}}
	got := deploySubSteps(plan)
	want := []string{
		SubStepDeployDev,
		SubStepStartProcs,
		SubStepVerifyDev,
		SubStepInitCommands,
		SubStepCrossDeploy,
		SubStepVerifyStage,
		SubStepReadmes,
	}
	if len(got) != len(want) {
		t.Fatalf("minimal deploy substep count = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("substep[%d] = %q, want %q", i, got[i].Name, name)
		}
	}
	for _, ss := range got {
		if ss.Name == SubStepSubagent || ss.Name == SubStepSnapshotDev || ss.Name == SubStepBrowserWalk {
			t.Errorf("minimal tier must not include showcase-only substep %q", ss.Name)
		}
	}
}

// TestSubStepReadmes_ValidatorWired ensures the deploy-phase readmes
// sub-step still resolves through the validator dispatch after the
// v8.64.0 cleanup that removed the legacy SubStepReadme alias.
func TestSubStepReadmes_ValidatorWired(t *testing.T) {
	t.Parallel()
	if getSubStepValidator(SubStepReadmes) == nil {
		t.Errorf("SubStepReadmes must route to validateReadme for the post-deploy narration sub-step")
	}
}

func indexOfSubStep(steps []RecipeSubStep, name string) int {
	for i, s := range steps {
		if s.Name == name {
			return i
		}
	}
	return -1
}
