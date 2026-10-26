// Tests for: tools/hq_recipe_parts.go — a change to the app's set of parts
// reaching the tiers on main as a targeted "add/remove service X" proposal
// (spec-mate §2.8a.5), run against a fake HQ that serves real git
// (hq_lab_test.go).
package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/recipe"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// workerStage is the stage of the part a Mate adds to the lab's app.
var workerStage = platform.ServiceStack{ID: "svc-workerstage", Name: "workerstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}}

// newPartsLab is the recipe lab whose one-part app's recipe landed on main,
// and which the group then edited by hand: Stage's runtime renamed to shop
// with a comment of its own, and a project variable added.
func newPartsLab(t *testing.T) (*hqLab, map[string]string) {
	t.Helper()
	lab := newRecipeLab(t)
	lab.hq.seed(hq.RecipeRepo)
	if outcome := lab.proposeRecipe(lab.mock); outcome.Change != 1 {
		t.Fatalf("the first recipe was not proposed: %+v", outcome)
	}
	lab.hq.mergeChange(hq.RecipeRepo, 1)
	stagePath := hq.RecipeTierPaths[hq.RecipeTierStage]
	stage := lab.hq.recipeFiles("main")[stagePath]
	edited := strings.Replace(stage, "  - hostname: app\n", "  # the storefront, renamed by hand\n  - hostname: shop\n", 1)
	edited = strings.Replace(edited, "project:\n  name: ", "project:\n  # edited by the group\n  name: ", 1)
	if edited == stage || !strings.Contains(edited, "hostname: shop") {
		t.Fatalf("the Stage tier does not read as the composer writes it:\n%s", stage)
	}
	lab.hq.landOnMain(hq.RecipeRepo, map[string]string{stagePath: edited})
	return lab, lab.hq.recipeFiles("main")
}

// addWorker makes the lab's pair two parts: a worker stage, its setup
// recorded, running in the project.
func addWorker(t *testing.T, lab *hqLab) platform.Client {
	t.Helper()
	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.Stages = append(m.Stages, topology.StageRef{Hostname: "workerstage", SetupName: "worker"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return recipeReconcileClient(workerStage)
}

// proposeService is the agent's group-recipe service=host over the lab.
func (l *hqLab) proposeService(client platform.Client, host string) (*hqServiceAnswer, string) {
	l.t.Helper()
	result, _, _ := handleGroupRecipeService(l.t.Context(), client, l.hq.srv.Client(), l.rt, l.stateDir, host)
	text := getTextContent(l.t, result)
	if result.IsError {
		return nil, text
	}
	var answer hqServiceAnswer
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		l.t.Fatalf("parse: %v\n%s", err, text)
	}
	return &answer, text
}

type hqServiceAnswer struct {
	Change  int      `json:"change"`
	Branch  string   `json:"branch"`
	Tiers   []string `json:"tiers"`
	Message string   `json:"message"`
}

// A one-part app's first change proposes no tier edit: the recipe on main
// carries its one part, so the reconcile says nothing and a targeted call
// has nothing to propose.
func TestGroupRecipeService_OnePart_ProposesNoTierEdit(t *testing.T) {
	t.Parallel()
	lab, main := newPartsLab(t)

	if outcome := lab.proposeRecipe(lab.mock); outcome.Line != "" || outcome.Change != 0 {
		t.Errorf("reconcile over a one-part app = %+v, want nothing said or proposed", outcome)
	}
	answer, text := lab.proposeService(lab.mock, "appstage")
	if answer == nil || answer.Change != 0 || len(answer.Tiers) != 0 {
		t.Errorf("service=appstage = %s, want nothing to propose", text)
	}
	if lab.hq.changeIn(hq.RecipeRepo, 2) != nil || !equalFiles(lab.hq.recipeFiles("main"), main) {
		t.Error("a one-part app touched the recipe repository")
	}
}

// Adding or removing a part proposes exactly "add/remove service X" to
// existing tiers and leaves every other line, hand edits included,
// unchanged.
func TestGroupRecipeService_AddThenRemovePart_OnlyThatService(t *testing.T) {
	t.Parallel()
	lab, main := newPartsLab(t)
	client := addWorker(t, lab)

	// The reconcile adds no whole tier and steers to the targeted call.
	outcome := lab.proposeRecipe(client)
	if outcome.Change != 0 || !strings.Contains(outcome.Line, `action="group-recipe" service="workerstage"`) {
		t.Fatalf("reconcile after adding a part = %+v, want the steer to service=workerstage", outcome)
	}

	answer, text := lab.proposeService(client, "workerstage")
	if answer == nil || answer.Change != 2 || answer.Branch != recipeBranch(2) {
		t.Fatalf("service=workerstage = %s", text)
	}
	if change := lab.hq.changeIn(hq.RecipeRepo, 2); change == nil || change.Title != "Mate: workerstage in the group recipe" {
		t.Fatalf("change #2 = %+v", change)
	}
	added := lab.hq.recipeFiles(recipeBranch(2))
	wantAdded := map[string]string{
		hq.RecipeTierPaths[hq.RecipeTierMate]:       "workerstage",
		hq.RecipeTierPaths[hq.RecipeTierStage]:      "worker",
		hq.RecipeTierPaths[hq.RecipeTierProduction]: "worker",
	}
	for path, body := range added {
		host, targeted := wantAdded[path]
		if !targeted {
			if body != main[path] {
				t.Errorf("%s changed, which no tier edit names", path)
			}
			continue
		}
		var setup string
		for _, held := range bundle.HeldRuntimes(body) {
			if held.Hostname == host {
				setup = held.SetupName
			}
		}
		if setup != "worker" {
			t.Errorf("%s: %s builds setup %q, want worker:\n%s", path, host, setup, body)
		}
		// Every other line is main's: taking the service out gives main back.
		if back, err := bundle.RemoveTierService(body, host); err != nil || back != main[path] {
			t.Errorf("%s: the proposal changes more than service %s (%v):\n%s", path, host, err, body)
		}
	}
	if stage := added[hq.RecipeTierPaths[hq.RecipeTierStage]]; !strings.Contains(stage, "hostname: shop") || strings.Contains(stage, "hostname: app\n") {
		t.Errorf("the hostname the tier holds was recomputed:\n%s", stage)
	}

	// The person merges it; the Mate deletes the worker's stage and the
	// tiers lose exactly that service again.
	lab.hq.mergeChange(hq.RecipeRepo, 2)
	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.Stages = m.Stages[:1]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if outcome := lab.proposeRecipe(lab.mock); !strings.Contains(outcome.Line, `action="group-recipe" service="workerstage"`) {
		t.Fatalf("reconcile after removing a part = %+v, want the steer to service=workerstage", outcome)
	}
	answer, text = lab.proposeService(lab.mock, "workerstage")
	if answer == nil || answer.Change != 3 {
		t.Fatalf("service=workerstage after removal = %s", text)
	}
	if removed := lab.hq.recipeFiles(recipeBranch(3)); !equalFiles(removed, main) {
		t.Errorf("removing the part does not bring the tiers back to what main held before it: %v", diffPaths(main, removed))
	}
}

// recipePreviewYAML is the lab app's zerops.yaml once its stages deploy Mate's
// preview variants: each variant extends the setup production builds and
// changes only the framing (docs/spec-mate.md, the preview pane).
const recipePreviewYAML = `zerops:
  - setup: api
    run:
      base: nodejs@22
      start: zcp-devsup run
  - setup: prod
    run:
      base: nodejs@22
      start: node server.js
  - setup: preview
    extends: prod
    run:
      routing:
        headers:
          - for: "/"
            values:
              Content-Security-Policy: "frame-ancestors 'self' https://mate.zerops.io https://*.zerops.app"
  - setup: worker
    run:
      base: nodejs@22
      start: node worker.js
  - setup: workerpreview
    extends: worker
    run:
      routing:
        headers:
          - for: "/"
            values:
              Content-Security-Policy: "frame-ancestors 'self' https://mate.zerops.io https://*.zerops.app"
`

// composeLabWithYAML composes the lab's recipe as composeGroupRecipe does, the
// pair's zerops.yaml read as yaml — the lab mounts no checkout.
func composeLabWithYAML(t *testing.T, lab *hqLab, client platform.Client, yaml string) groupRecipeComposition {
	t.Helper()
	comp, outcome, ok := composeGroupRecipe(t.Context(), client, lab.hq.srv.Client(), lab.rt, lab.stateDir)
	if !ok {
		t.Fatalf("compose: %+v", outcome)
	}
	t.Cleanup(comp.close)
	comp.inputs.Runtimes[0].ZeropsYAMLBody = yaml
	layout, _, err := bundle.BuildGroupRecipe(comp.inputs)
	if err != nil {
		t.Fatalf("compose with the zerops.yaml: %v", err)
	}
	if comp.files, err = recipe.Build(layout); err != nil {
		t.Fatal(err)
	}
	return comp
}

// A stage that deploys Mate's preview variant is the same part as the setup
// the variant extends: the AI Agent tier carries the stage building the
// variant, the group environments the part building the setup it extends
// (bundle.groupEnvironmentSetup). So a stage that starts deploying a variant
// changes no part — the reconcile proposes nothing over main — and a part the
// Mate gains with a variant is ONE proposal, adding the stage to the AI Agent
// tier and the extended setup's runtime to Stage and Small Production.
func TestGroupRecipeService_PreviewVariant_IsThePartOfTheSetupItExtends(t *testing.T) {
	t.Run("a stage that starts deploying the variant changes no part", func(t *testing.T) {
		lab, _ := newPartsLab(t)
		if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
			m.SetStageSetup("appstage", "preview")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		comp := composeLabWithYAML(t, lab, lab.mock, recipePreviewYAML)
		steer, err := recipePartSteer(t.Context(), comp)
		if err != nil {
			t.Fatal(err)
		}
		if steer != "" {
			t.Errorf("steer = %q, want nothing: the stage builds the variant of the part main carries", steer)
		}
	})
	t.Run("a part gained with a variant is one proposal over every tier", func(t *testing.T) {
		lab, _ := newPartsLab(t)
		if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
			m.Stages = append(m.Stages, topology.StageRef{Hostname: "workerstage", SetupName: "workerpreview"})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		comp := composeLabWithYAML(t, lab, recipeReconcileClient(workerStage), recipePreviewYAML)
		part, found, err := recipePartOf(t.Context(), comp, "workerstage")
		if err != nil || !found {
			t.Fatalf("part of workerstage = %+v, %v, %v", part, found, err)
		}
		tiers, err := planRecipeService(t.Context(), comp, part)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"0 — AI Agent: adds workerstage", "3 — Stage: adds worker", "4 — Small Production: adds worker"}
		if got := serviceTierEdits(tiers); strings.Join(got, "; ") != strings.Join(want, "; ") {
			t.Errorf("service=workerstage edits = %q, want %q", got, want)
		}
		steer, err := recipePartSteer(t.Context(), comp)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(steer, `service="workerstage"`) || strings.Contains(steer, `service="worker"`) {
			t.Errorf("steer = %q, want the one call service=\"workerstage\", never a second one for the extended setup's runtime", steer)
		}
	})
}

func equalFiles(a, b map[string]string) bool { return len(diffPaths(a, b)) == 0 }

// diffPaths names the paths a and b hold differently.
func diffPaths(a, b map[string]string) []string {
	var out []string
	for path, body := range a {
		if b[path] != body {
			out = append(out, path)
		}
	}
	for path := range b {
		if _, ok := a[path]; !ok {
			out = append(out, path)
		}
	}
	return out
}
