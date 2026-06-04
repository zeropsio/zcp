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
		// HTTP runtimes leave ServesHTTP nil — verify uses the live port signal.
		if app.ServesHTTP != nil {
			t.Errorf("app.ServesHTTP = %v, want nil (HTTP runtime defers to the port signal)", *app.ServesHTTP)
		}
		if app.PrimarySetupName != "dev" || app.StageSetupName != "prod" {
			t.Errorf("app setup names = %q/%q, want dev/prod", app.PrimarySetupName, app.StageSetupName)
		}
	})

	t.Run("local_standard_pair_is_localstage_keyed_on_dev_identity", func(t *testing.T) {
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

		// local-stage shape (matching adopt-local): the meta is keyed on the
		// DEV hostname (the local/CWD identity, not a Zerops service), with
		// StageHostname = the deployed Zerops stage. Keying on the stage would
		// collide Hostname==StageHostname and break ModeFor(stage).
		meta, err := ReadServiceMeta(dir, "appdev")
		if err != nil || meta == nil {
			t.Fatalf("local-stage meta should be keyed on the dev identity (appdev): %v", err)
		}
		if meta.Mode != topology.PlanModeLocalStage {
			t.Errorf("meta.Mode = %q, want local-stage", meta.Mode)
		}
		if meta.StageHostname != "appstage" {
			t.Errorf("meta.StageHostname = %q, want appstage (the Zerops stage)", meta.StageHostname)
		}
		if meta.PrimarySetupName != "" || meta.StageSetupName != "prod" {
			t.Errorf("setup names = %q/%q, want \"\"/prod (local-stage: no local-dev setup)", meta.PrimarySetupName, meta.StageSetupName)
		}
		// ModeFor projects the dev side to "" (local, no Zerops mode) and the
		// stage to local-stage — the BUG-1 regression guard.
		if got := meta.ModeFor("appdev"); got != "" {
			t.Errorf("ModeFor(appdev) = %q, want \"\" (local dev side)", got)
		}
		if got := meta.ModeFor("appstage"); got != topology.ModeLocalStage {
			t.Errorf("ModeFor(appstage) = %q, want %q", got, topology.ModeLocalStage)
		}
	})
}
