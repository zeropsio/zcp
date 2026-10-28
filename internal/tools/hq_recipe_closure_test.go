// Tests for: tools/hq_recipe_inputs.go and the recipe reconcile — a tier
// never ships a runtime whose setup reads a service the tier does not carry.
package tools

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/workflow"
)

// The composer can tell a service the Mate's project runs from a name no
// service has only when the reader hands it every service the project runs
// — the ones the recipe carries and the ones it leaves out alike: a runtime
// nothing wired, a Swarm pool, a Box, zcp itself.
func TestComposeGroupRecipeInputs_ProjectHostnames_EveryServiceTheProjectRuns(t *testing.T) {
	stateDir := t.TempDir()
	writeHQWiredPairMeta(t, stateDir)
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		t.Fatalf("ListServiceMetas: %v", err)
	}
	pool := liveRuntime("svc-pool", "devpool", "swarm@1")
	inputs, _, err := composeGroupRecipeInputs(context.Background(), medusaLikeProject(pool, liveBox("svc-box", "box")),
		"p1", "acme", t.TempDir(), testHQAddress, testHQOrg, labApp, metas, metas, nil)
	if err != nil {
		t.Fatalf("composeGroupRecipeInputs: %v", err)
	}
	want := []string{"appdev", "appstage", "box", "db", "devpool", "mailpit", "orphan", "search", "storage", "zcp"}
	got := slices.Sorted(slices.Values(inputs.ProjectHostnames))
	if !slices.Equal(got, want) {
		t.Errorf("ProjectHostnames = %v, want %v", got, want)
	}
}

// Through the reconcile: a stage setup that reads the Mate's mail catcher,
// which a stage and a production never run, refuses the recipe — nothing is
// proposed, and the pass's line itself names the tier, the setup, the
// variable and the service, rather than a warning among the others.
func TestGroupRecipe_TierClosure_NothingProposedAndTheLineNamesTheRead(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.mock = recipeReconcileClient(publicBuildRuntime("svc-mailpit", "mailpit", "alpine@3.21", "https://github.com/zerops-recipe-apps/mailpit-app"))
	lab.ssh.mock = lab.mock
	lab.wire()
	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.PrimarySetupName = "api"
		m.SetStageSetup(m.FirstStage().Hostname, "prod")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lab.hq.landOnMain("appdev", map[string]string{"zerops.yaml": "zerops:\n" +
		"  - setup: api\n    run:\n      base: nodejs@22\n      envVariables:\n        DB_PORT: ${db_port}\n" +
		"  - setup: prod\n    run:\n      base: nodejs@22\n      envVariables:\n        DB_PORT: ${db_port}\n        MAIL_URL: smtp://mailpit:1025\n"})

	outcome := lab.proposeRecipe(lab.mock)

	if outcome.Change != 0 || lab.hq.findOf(labMate, labApp, hq.RecipeRepo) != nil {
		t.Errorf("outcome %+v: want nothing proposed while a tier lacks what its runtime reads", outcome)
	}
	for _, want := range []string{"does not compose", `tier "Stage"`, `tier "Small Production"`, `setup "prod"`, "run.envVariables.MAIL_URL", `"mailpit"`} {
		if !strings.Contains(outcome.Line, want) {
			t.Errorf("line does not say %s:\n%s", want, outcome.Line)
		}
	}
	if strings.Contains(outcome.Line, `tier "AI Agent"`) {
		t.Errorf("line names the AI Agent tier, which carries the mail catcher:\n%s", outcome.Line)
	}
}
