// Tests for: tools/hq_recipe_required.go — a Mate's group recipe is the
// project a second Mate, the group's stage and its production are built from,
// so a stand-up is not done while it cannot be proposed: the pass says so as
// a required step, in the places the agent reads what it has to do, and
// tries again on every pass until it composes.
package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestGroupRecipe_TheMateWebShape_RequiredUntilItComposes is the Mate Web
// Mate (2026-10-27): a one-part pair whose bootstrap closed before develop
// put the code in, so its stage's setup was not recorded and no zerops.yaml
// was there to tell it — the pass composed nothing, said so inside a long
// result, and the group's recipe repository stayed empty for good.
//
// Now the pass that cannot compose makes it a required step — on the
// bootstrap response, beside a develop start's result and in status —
// naming what is missing and the call that fixes it. Once the code lands on
// main of the pair's repository in HQ the next pass reads its zerops.yaml
// there and proposes the recipe, and nothing is required any more; once the
// pair moves onto its dev pool, the tiers zcp wrote are proposed anew with
// the pool.
func TestGroupRecipe_TheMateWebShape_RequiredUntilItComposes(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.mock = recipeReconcileClient()
	lab.ssh.mock = lab.mock
	lab.wire()
	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.PrimarySetupName = "dev"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The bootstrap's close: no zerops.yaml anywhere yet.
	report := reconcileMateRepositories(t.Context(), lab.mock, lab.hq.srv.Client(), lab.ssh, lab.rt, lab.stateDir, hqPassBackedOff)
	if len(report.required) != 1 {
		t.Fatalf("required = %v, want the group recipe's step (lines %v)", report.required, report.lines)
	}
	step := report.required[0]
	for _, want := range []string{"group recipe", `"appstage"`, `action="set-default-setup"`, `targetService="appstage"`, `setup="dev"`, "zerops.yaml"} {
		if !strings.Contains(step, want) {
			t.Errorf("the required step does not say %q: %s", want, step)
		}
	}
	if n := lab.hq.openCount(hq.RecipeRepo); n != 0 {
		t.Errorf("%d recipe change(s) opened for a recipe that does not compose", n)
	}
	resp := &workflow.BootstrapResponse{Message: "Bootstrap complete."}
	appendRepositoryReport(resp, report)
	if len(resp.Required) != 1 || resp.Required[0] != step || !strings.Contains(resp.Message, "REQUIRED") {
		t.Errorf("bootstrap response = required %v, message %q; want the step required and said", resp.Required, resp.Message)
	}
	developed := withRepositoryReport(&mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "develop started"}}}, report)
	var texts []string
	for _, content := range developed.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			texts = append(texts, text.Text)
		}
	}
	if text := strings.Join(texts, "\n"); !strings.Contains(text, "REQUIRED") || !strings.Contains(text, step) {
		t.Errorf("a develop start does not carry the step:\n%s", text)
	}
	if got := readRecipeRequired(lab.stateDir); len(got) != 1 || got[0] != step {
		t.Errorf("the step status reads = %v, want %q", got, step)
	}

	// The code lands on main of the pair's repository: the next pass reads
	// its zerops.yaml there and proposes the recipe.
	lab.hq.landOnMain("appdev", map[string]string{"zerops.yaml": "zerops:\n  - setup: dev\n    run:\n      base: nodejs@22\n  - setup: prod\n    run:\n      base: nodejs@22\n"})
	report = reconcileMateRepositories(t.Context(), lab.mock, lab.hq.srv.Client(), lab.ssh, lab.rt, lab.stateDir, hqPassBackedOff)
	if len(report.required) != 0 {
		t.Errorf("required = %v after the code landed, want none", report.required)
	}
	if got := readRecipeRequired(lab.stateDir); len(got) != 0 {
		t.Errorf("status still reads a required step: %v", got)
	}
	opened := lab.hq.findOf(labMate, labApp, hq.RecipeRepo)
	if opened == nil || opened.Title != hq.RecipeProposalTitle {
		t.Fatalf("no recipe proposed once the code landed (lines %v)", report.lines)
	}
	branch := lab.hq.recipeFiles(recipeBranch(opened.Number))
	if agent := branch[recipeAgentTier]; !strings.Contains(agent, "hostname: appdev") || !strings.Contains(agent, "zeropsSetup: prod") {
		t.Errorf("the AI Agent tier proposed does not carry the pair, its stage building prod:\n%s", agent)
	}

	// Core lands it, the stage's deploy records its setup, and the pair
	// moves onto its dev pool: the tiers zcp wrote are proposed anew.
	lab.hq.mergeChange(hq.RecipeRepo, opened.Number)
	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.SetStageSetup("appstage", "prod")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lab.moveLabPairOntoPool()
	outcome := lab.proposeRecipe(lab.mock)
	if !outcome.Created || len(outcome.Replaced) != 1 || outcome.Replaced[0] != "0 — AI Agent" {
		t.Fatalf("outcome = %+v, want the AI Agent tier proposed anew", outcome)
	}
	agent := lab.hq.recipeFiles(recipeBranch(outcome.Change))[recipeAgentTier]
	if !strings.Contains(agent, "hostname: "+testPool) || strings.Contains(agent, "hostname: appdev") {
		t.Errorf("the AI Agent tier proposed anew does not carry the pool in the dev service's place:\n%s", agent)
	}
	if mate, intact, _ := bundle.TierStamp(agent); mate != labMate || !intact {
		t.Errorf("the tier proposed anew is not marked as this Mate's: %q intact %v", mate, intact)
	}
}

// TestRecipeRequiredStep_NamesTheWayOut: the step names the stage, the setup
// zcp needs, and the calls that give it — the setups to choose from when the
// zerops.yaml declares several, or where zcp reads the zerops.yaml, a stage
// deploy and set-default-setup when none was read — for a pool-keyed pair the
// repository's main in HQ, since a pool holds no checkout.
func TestRecipeRequiredStep_NamesTheWayOut(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		blocker bundle.GroupRecipeBlocker
		pooled  bool
		want    []string
	}{
		{
			name:    "no zerops.yaml read",
			blocker: bundle.GroupRecipeBlocker{Runtime: "webdev", Stage: "webstage", DevSetup: "dev", Why: "no zerops.yaml was read"},
			want:    []string{`"webstage"`, "no zerops.yaml was read", "the dev checkout", "zerops_deploy", `targetService="webstage" setup="dev" stageSetup=`},
		},
		{
			name:    "a pool's part",
			blocker: bundle.GroupRecipeBlocker{Runtime: "webpool", Stage: "webstage", DevSetup: "webpool", Why: "no zerops.yaml was read"},
			pooled:  true,
			want:    []string{"main of its repository in HQ", `setup="webpool"`},
		},
		{
			name:    "several setups to choose from",
			blocker: bundle.GroupRecipeBlocker{Runtime: "webdev", Stage: "webstage", DevSetup: "dev", Why: "its zerops.yaml declares preview, prod beside the dev setup \"dev\"", Declared: []string{"preview", "prod"}},
			want:    []string{`stageSetup="<one of preview, prod>"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			step := recipeRequiredStep([]bundle.GroupRecipeBlocker{tt.blocker}, map[string]bool{tt.blocker.Runtime: tt.pooled}, []string{"0 — AI Agent", "3 — Stage"})
			for _, want := range tt.want {
				if !strings.Contains(step, want) {
					t.Errorf("step does not say %q:\n%s", want, step)
				}
			}
			if tt.pooled && strings.Contains(step, "dev checkout") {
				t.Errorf("a pool's step points at a dev checkout:\n%s", step)
			}
		})
	}
}
