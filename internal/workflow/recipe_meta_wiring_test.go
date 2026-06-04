package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestBootstrapRecipe_MetaWiring pins R3-P4.3 + P4.5: the derived recipe plan
// stamps each runtime's ServiceMeta with ServesHTTP + the LITERAL setup name
// from the recipe shape, and local mode projects a standard pair to a single
// local-stage meta keyed on the stage (dev half = the user's CWD, no meta).
func TestBootstrapRecipe_MetaWiring(t *testing.T) {
	t.Parallel()

	t.Run("container_worker_servesHttp_false_and_setup_worker", func(t *testing.T) {
		t.Parallel()
		docs := map[string]*knowledge.Document{
			"zerops://recipes/laravel-showcase": {
				URI:        "zerops://recipes/laravel-showcase",
				Title:      "Laravel Showcase",
				Languages:  []string{"php"},
				Frameworks: []string{"laravel"},
				ImportYAML: "services:\n  - hostname: appdev\n    type: php-nginx@8.4\n    zeropsSetup: dev\n    buildFromGit: https://example.com/ls\n  - hostname: appstage\n    type: php-nginx@8.4\n    zeropsSetup: prod\n    buildFromGit: https://example.com/ls\n  - hostname: workerstage\n    type: php-nginx@8.4\n    zeropsSetup: worker\n    buildFromGit: https://example.com/ls\n",
			},
		}
		store, err := knowledge.NewStore(docs)
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		dir := t.TempDir()
		eng := NewEngine(dir, EnvContainer, store)
		if _, err := eng.BootstrapStartWithRoute("proj-1", "laravel showcase", BootstrapRouteRecipe, "laravel-showcase"); err != nil {
			t.Fatalf("start: %v", err)
		}
		if _, err := eng.BootstrapCompleteRecipePlan(nil, nil, nil); err != nil {
			t.Fatalf("recipe plan: %v", err)
		}
		state, _ := eng.GetState()
		eng.writeProvisionMetas(state)

		worker, err := FindServiceMeta(dir, "workerstage")
		if err != nil || worker == nil {
			t.Fatalf("worker meta not written: %v", err)
		}
		if worker.ServesHTTP == nil || *worker.ServesHTTP {
			t.Errorf("worker.ServesHTTP = %v, want non-nil false (a queue worker serves no HTTP)", worker.ServesHTTP)
		}
		if worker.PrimarySetupName != "worker" {
			t.Errorf("worker.PrimarySetupName = %q, want \"worker\" (literal zeropsSetup, NOT the mode-convention \"prod\")", worker.PrimarySetupName)
		}

		app, err := FindServiceMeta(dir, "appdev")
		if err != nil || app == nil {
			t.Fatalf("app meta not written: %v", err)
		}
		if app.ServesHTTP == nil || !*app.ServesHTTP {
			t.Errorf("app.ServesHTTP = %v, want non-nil true", app.ServesHTTP)
		}
		if app.PrimarySetupName != "dev" || app.StageSetupName != "prod" {
			t.Errorf("app setup names = %q/%q, want dev/prod", app.PrimarySetupName, app.StageSetupName)
		}
	})

	t.Run("local_standard_pair_is_localstage_keyed_on_stage", func(t *testing.T) {
		t.Parallel()
		docs := map[string]*knowledge.Document{
			"zerops://recipes/nodejs-hello-world": {
				URI:        "zerops://recipes/nodejs-hello-world",
				Title:      "Node Hello",
				Languages:  []string{"javascript"},
				ImportYAML: "services:\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n    buildFromGit: https://example.com/n\n  - hostname: appstage\n    type: nodejs@22\n    zeropsSetup: prod\n    buildFromGit: https://example.com/n\n",
			},
		}
		store, err := knowledge.NewStore(docs)
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		dir := t.TempDir()
		eng := NewEngine(dir, EnvLocal, store)
		if _, err := eng.BootstrapStartWithRoute("proj-1", "node app", BootstrapRouteRecipe, "nodejs-hello-world"); err != nil {
			t.Fatalf("start: %v", err)
		}
		if _, err := eng.BootstrapCompleteRecipePlan(nil, nil, nil); err != nil {
			t.Fatalf("recipe plan: %v", err)
		}
		state, _ := eng.GetState()
		eng.writeProvisionMetas(state)

		// Dev half is the user's CWD in local mode → no meta keyed on it.
		if m, _ := ReadServiceMeta(dir, "appdev"); m != nil {
			t.Errorf("appdev must have NO meta in local mode (it is the CWD), got %+v", m)
		}
		stage, err := FindServiceMeta(dir, "appstage")
		if err != nil || stage == nil {
			t.Fatalf("appstage meta not written: %v", err)
		}
		if stage.Mode != topology.PlanModeLocalStage {
			t.Errorf("appstage.Mode = %q, want local-stage", stage.Mode)
		}
		if stage.PrimarySetupName != "" || stage.StageSetupName != "prod" {
			t.Errorf("appstage setup names = %q/%q, want \"\"/prod (local-stage: no local-dev setup)", stage.PrimarySetupName, stage.StageSetupName)
		}
	})
}
