package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestInferRecipeShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		yaml         string
		wantMode     topology.Mode
		wantRuntimes int
	}{
		{
			name: "standard_dev_plus_prod",
			yaml: `services:
  - hostname: appdev
    type: nodejs@22
    zeropsSetup: dev
  - hostname: appstage
    type: nodejs@22
    zeropsSetup: prod
  - hostname: db
    type: postgresql@18
`,
			wantMode:     "standard",
			wantRuntimes: 2,
		},
		{
			name: "simple_single_prod",
			yaml: `services:
  - hostname: app
    type: nodejs@22
    zeropsSetup: prod
  - hostname: db
    type: postgresql@18
`,
			wantMode:     "simple",
			wantRuntimes: 1,
		},
		{
			name: "dev_single_dev",
			yaml: `services:
  - hostname: app
    type: nodejs@22
    zeropsSetup: dev
`,
			wantMode:     "dev",
			wantRuntimes: 1,
		},
		{
			name: "managed_only_no_runtime",
			yaml: `services:
  - hostname: db
    type: postgresql@18
`,
			wantMode:     "",
			wantRuntimes: 0,
		},
		{
			name:         "invalid_yaml",
			yaml:         "::: not yaml",
			wantMode:     "",
			wantRuntimes: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mode, count := InferRecipeShape(tt.yaml)
			if mode != tt.wantMode {
				t.Errorf("mode: got %q, want %q", mode, tt.wantMode)
			}
			if count != tt.wantRuntimes {
				t.Errorf("runtimes: got %d, want %d", count, tt.wantRuntimes)
			}
		})
	}
}

// TestParseRecipeShape pins the R3 owner type: ParseRecipeShape captures every
// runtime (incl. a zeropsSetup:worker as a first-class Worker runtime with
// ServesHTTP=false) + every managed dep from the recipe import YAML — the
// single source the derived plan + the YAML rewrite both key off. Mode()
// ignores worker extras so a 3-runtime worker recipe (laravel-showcase) is
// still "standard", not the old lossy ("",3) unrecognized.
func TestParseRecipeImportShape(t *testing.T) {
	t.Parallel()

	const showcase = `services:
  - hostname: appdev
    type: php-nginx@8.4
    zeropsSetup: dev
    buildFromGit: https://github.com/zerops-recipe-apps/laravel-showcase-app
  - hostname: appstage
    type: php-nginx@8.4
    zeropsSetup: prod
    buildFromGit: https://github.com/zerops-recipe-apps/laravel-showcase-app
  - hostname: workerstage
    type: php-nginx@8.4
    zeropsSetup: worker
    buildFromGit: https://github.com/zerops-recipe-apps/laravel-showcase-app
  - hostname: db
    type: postgresql@18
  - hostname: redis
    type: valkey@7.2
`
	shape, err := ParseRecipeImportShape(showcase)
	if err != nil {
		t.Fatalf("ParseRecipeImportShape: %v", err)
	}
	if len(shape.Runtimes) != 3 {
		t.Fatalf("runtimes: got %d, want 3 (appdev, appstage, workerstage)", len(shape.Runtimes))
	}
	if len(shape.ManagedDeps) != 2 {
		t.Fatalf("managed deps: got %d, want 2 (db, redis)", len(shape.ManagedDeps))
	}
	// The worker is captured as a first-class Worker runtime, not folded to stage.
	worker := shape.Runtimes[2]
	if worker.Hostname != "workerstage" || worker.RoleKind != RecipeRuntimeRoleWorker || !worker.IsWorker {
		t.Errorf("workerstage: got hostname=%q role=%q isWorker=%v, want workerstage/worker/true", worker.Hostname, worker.RoleKind, worker.IsWorker)
	}
	if worker.ServesHTTP {
		t.Errorf("worker.ServesHTTP = true, want false (a queue worker serves no HTTP)")
	}
	if worker.Type != "php-nginx@8.4" {
		t.Errorf("worker.Type = %q, want php-nginx@8.4", worker.Type)
	}
	// dev/stage halves keep their roles + types.
	if shape.Runtimes[0].RoleKind != RecipeRuntimeRoleDev || shape.Runtimes[1].RoleKind != RecipeRuntimeRoleStage {
		t.Errorf("dev/stage roles: got %q/%q, want dev/stage", shape.Runtimes[0].RoleKind, shape.Runtimes[1].RoleKind)
	}
	// Mode() ignores the worker extra → still standard (not the old ("",3)).
	if shape.Mode() != topology.PlanModeStandard {
		t.Errorf("Mode() = %q, want standard (worker ignored for primary shape)", shape.Mode())
	}
	if shape.RuntimeCount() != 3 {
		t.Errorf("RuntimeCount() = %d, want 3", shape.RuntimeCount())
	}
	// The buildFromGit is preserved (the derived plan needs it).
	if shape.Runtimes[0].BuildFromGit == "" {
		t.Errorf("dev runtime BuildFromGit dropped")
	}
}

// TestDeriveRecipePlan pins the R3 derive-from-owner core: the bootstrap plan
// is a pure function of the recipe import shape — every runtime (incl. the
// worker + cross-type halves) becomes a declared target, managed deps are
// CREATE on the primary app target, and the worker is a standalone simple
// target so it earns its own ServiceMeta (the fix for provisioned-but-untracked).
func TestDeriveRecipePlan(t *testing.T) {
	t.Parallel()

	parse := func(t *testing.T, y string) RecipeImportShape {
		t.Helper()
		s, err := ParseRecipeImportShape(y)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return s
	}

	t.Run("showcase_worker_gets_own_target", func(t *testing.T) {
		t.Parallel()
		// laravel-showcase shape: app pair + worker, all sharing one repo.
		shape := parse(t, `services:
  - hostname: appdev
    type: php-nginx@8.4
    zeropsSetup: dev
    buildFromGit: https://github.com/zerops-recipe-apps/laravel-showcase-app
  - hostname: appstage
    type: php-nginx@8.4
    zeropsSetup: prod
    buildFromGit: https://github.com/zerops-recipe-apps/laravel-showcase-app
  - hostname: workerstage
    type: php-nginx@8.4
    zeropsSetup: worker
    buildFromGit: https://github.com/zerops-recipe-apps/laravel-showcase-app
  - hostname: db
    type: postgresql@18
  - hostname: redis
    type: valkey@7.2
`)
		targets, err := DeriveRecipePlan(shape, RecipeShapeOverrides{})
		if err != nil {
			t.Fatalf("derive: %v", err)
		}
		if len(targets) != 2 {
			t.Fatalf("targets: got %d, want 2 (app standard pair + worker)", len(targets))
		}
		app := targets[0]
		if app.Runtime.DevHostname != "appdev" || app.Runtime.ExplicitStage != "appstage" || app.Runtime.BootstrapMode != topology.PlanModeStandard {
			t.Errorf("app target = %+v, want appdev/appstage/standard", app.Runtime)
		}
		if app.Runtime.StageType != "" {
			t.Errorf("same-type pair must leave StageType empty, got %q", app.Runtime.StageType)
		}
		if len(app.Dependencies) != 2 {
			t.Errorf("app deps: got %d, want 2 (db, redis CREATE)", len(app.Dependencies))
		}
		for _, d := range app.Dependencies {
			if d.Resolution != ResolutionCreate {
				t.Errorf("dep %q resolution = %q, want CREATE", d.Hostname, d.Resolution)
			}
		}
		worker := targets[1]
		if worker.Runtime.DevHostname != "workerstage" || worker.Runtime.BootstrapMode != topology.PlanModeSimple {
			t.Errorf("worker target = %+v, want workerstage/simple", worker.Runtime)
		}
		if len(worker.Dependencies) != 0 {
			t.Errorf("worker must carry no deps (reaches managed via env refs), got %d", len(worker.Dependencies))
		}
	})

	t.Run("multi_repo_two_pairs_both_tracked", func(t *testing.T) {
		t.Parallel()
		// zerops-showcase: two dev/stage pairs across two buildFromGit repos
		// (a bun app pair + a python worker pair). The derived plan MUST emit
		// one standard target per repo so every runtime earns a ServiceMeta —
		// the old single-pair derive silently dropped the second pair
		// (provisioned-but-untracked).
		shape := parse(t, `services:
  - hostname: appdev
    type: bun@1.2
    zeropsSetup: dev
    buildFromGit: https://github.com/zerops-recipe-apps/showcase-recipe-app
  - hostname: appstage
    type: bun@1.2
    zeropsSetup: prod
    buildFromGit: https://github.com/zerops-recipe-apps/showcase-recipe-app
  - hostname: workerdev
    type: python@3.12
    zeropsSetup: dev
    buildFromGit: https://github.com/zerops-recipe-apps/showcase-recipe-worker
  - hostname: workerstage
    type: python@3.12
    zeropsSetup: prod
    buildFromGit: https://github.com/zerops-recipe-apps/showcase-recipe-worker
  - hostname: db
    type: postgresql@17
  - hostname: redis
    type: valkey@7.2
  - hostname: queue
    type: nats@2.12
  - hostname: storage
    type: object-storage
`)
		targets, err := DeriveRecipePlan(shape, RecipeShapeOverrides{})
		if err != nil {
			t.Fatalf("derive: %v", err)
		}
		if len(targets) != 2 {
			t.Fatalf("targets: got %d, want 2 (bun app pair + python worker pair)", len(targets))
		}
		app := targets[0]
		if app.Runtime.DevHostname != "appdev" || app.Runtime.ExplicitStage != "appstage" || app.Runtime.BootstrapMode != topology.PlanModeStandard {
			t.Errorf("app target = %+v, want appdev/appstage/standard", app.Runtime)
		}
		// Managed deps land on the PRIMARY (first repo) app target only.
		if len(app.Dependencies) != 4 {
			t.Errorf("app deps: got %d, want 4 (db, redis, queue, storage CREATE on primary)", len(app.Dependencies))
		}
		worker := targets[1]
		if worker.Runtime.DevHostname != "workerdev" || worker.Runtime.ExplicitStage != "workerstage" || worker.Runtime.BootstrapMode != topology.PlanModeStandard {
			t.Errorf("worker pair target = %+v, want workerdev/workerstage/standard", worker.Runtime)
		}
		if worker.Runtime.Type != "python@3.12" {
			t.Errorf("worker pair type = %q, want python@3.12", worker.Runtime.Type)
		}
		// The second repo's pair carries NO deps — it reaches managed services
		// via ${host_*} env refs, not its own plan dependency.
		if len(worker.Dependencies) != 0 {
			t.Errorf("second-repo pair must carry no deps, got %d", len(worker.Dependencies))
		}
	})

	t.Run("cross_type_pair_sets_stage_type", func(t *testing.T) {
		t.Parallel()
		// vue-static shape: nodejs dev + static stage, one shared repo.
		shape := parse(t, `services:
  - hostname: appdev
    type: nodejs@22
    zeropsSetup: dev
    buildFromGit: https://github.com/zerops-recipe-apps/vue-static-hello-world-app
  - hostname: appstage
    type: static@1.0
    zeropsSetup: prod
    buildFromGit: https://github.com/zerops-recipe-apps/vue-static-hello-world-app
`)
		targets, err := DeriveRecipePlan(shape, RecipeShapeOverrides{})
		if err != nil {
			t.Fatalf("derive: %v", err)
		}
		if len(targets) != 1 {
			t.Fatalf("targets: got %d, want 1", len(targets))
		}
		rt := targets[0].Runtime
		if rt.Type != "nodejs@22" || rt.StageType != "static@1.0" {
			t.Errorf("cross-type: got Type=%q StageType=%q, want nodejs@22 / static@1.0", rt.Type, rt.StageType)
		}
	})

	t.Run("simple_single_prod", func(t *testing.T) {
		t.Parallel()
		shape := parse(t, "services:\n  - hostname: app\n    type: nodejs@22\n    zeropsSetup: prod\n")
		targets, _ := DeriveRecipePlan(shape, RecipeShapeOverrides{})
		if len(targets) != 1 || targets[0].Runtime.BootstrapMode != topology.PlanModeSimple {
			t.Errorf("simple: got %+v, want 1 simple target", targets)
		}
	})

	t.Run("dev_single_dev", func(t *testing.T) {
		t.Parallel()
		shape := parse(t, "services:\n  - hostname: app\n    type: nodejs@22\n    zeropsSetup: dev\n")
		targets, _ := DeriveRecipePlan(shape, RecipeShapeOverrides{})
		if len(targets) != 1 || targets[0].Runtime.BootstrapMode != topology.PlanModeDev {
			t.Errorf("dev: got %+v, want 1 dev target", targets)
		}
	})

	t.Run("managed_only_errors", func(t *testing.T) {
		t.Parallel()
		shape := parse(t, "services:\n  - hostname: db\n    type: postgresql@18\n")
		if _, err := DeriveRecipePlan(shape, RecipeShapeOverrides{}); err == nil {
			t.Error("managed-only shape should error (no runtime to derive)")
		}
	})

	t.Run("hostname_override_renames", func(t *testing.T) {
		t.Parallel()
		shape := parse(t, "services:\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n")
		targets, _ := DeriveRecipePlan(shape, RecipeShapeOverrides{
			RuntimeHostnameByOriginal: map[string]string{"appdev": "myapp"},
		})
		if len(targets) != 1 || targets[0].Runtime.DevHostname != "myapp" {
			t.Errorf("override: got %+v, want DevHostname=myapp", targets)
		}
	})
}

// TestDeriveRecipePlan_DevOnly pins the opt-in dev-only narrowing of a standard
// recipe: only the dev half survives, as a PlanModeDev target (no promote);
// stage + worker are dropped; managed deps stay on the surviving dev target.
func TestDeriveRecipePlan_DevOnly(t *testing.T) {
	t.Parallel()
	parse := func(t *testing.T, y string) RecipeImportShape {
		t.Helper()
		s, err := ParseRecipeImportShape(y)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return s
	}

	t.Run("keeps_dev_drops_stage_and_worker", func(t *testing.T) {
		t.Parallel()
		// laravel-showcase shape: php pair + php worker + managed db, one repo.
		shape := parse(t, `services:
  - hostname: appdev
    type: php-nginx@8.4
    zeropsSetup: dev
    buildFromGit: https://example.com/app
  - hostname: appstage
    type: php-nginx@8.4
    zeropsSetup: prod
    buildFromGit: https://example.com/app
  - hostname: workerstage
    type: php-nginx@8.4
    zeropsSetup: worker
    buildFromGit: https://example.com/app
  - hostname: db
    type: postgresql@18
`)
		targets, err := DeriveRecipePlan(shape, RecipeShapeOverrides{DevOnly: true})
		if err != nil {
			t.Fatalf("derive dev-only: %v", err)
		}
		// Only the dev half survives — no stage, no worker (the paid stage skipped).
		if len(targets) != 1 {
			t.Fatalf("dev-only targets: got %d, want 1 (dev half only)", len(targets))
		}
		rt := targets[0].Runtime
		if rt.DevHostname != "appdev" || rt.ExplicitStage != "" {
			t.Errorf("dev-only target = %+v, want appdev with no stage half", rt)
		}
		if rt.BootstrapMode != topology.PlanModeDev {
			t.Errorf("dev-only mode = %q, want PlanModeDev (no promote target)", rt.BootstrapMode)
		}
		// Managed deps preserved on the surviving dev target.
		if len(targets[0].Dependencies) != 1 || targets[0].Dependencies[0].Hostname != "db" {
			t.Errorf("dev-only deps = %+v, want db preserved", targets[0].Dependencies)
		}
	})

	t.Run("with_rename", func(t *testing.T) {
		t.Parallel()
		shape := parse(t, `services:
  - hostname: appdev
    type: nodejs@22
    zeropsSetup: dev
    buildFromGit: https://example.com/app
  - hostname: appstage
    type: nodejs@22
    zeropsSetup: prod
    buildFromGit: https://example.com/app
`)
		targets, _ := DeriveRecipePlan(shape, RecipeShapeOverrides{
			DevOnly:                   true,
			RuntimeHostnameByOriginal: map[string]string{"appdev": "myapp"},
		})
		if len(targets) != 1 || targets[0].Runtime.DevHostname != "myapp" || targets[0].Runtime.BootstrapMode != topology.PlanModeDev {
			t.Errorf("dev-only + rename: got %+v, want single myapp dev target", targets)
		}
	})
}

// TestCanNarrowRecipeDevOnly pins the opt-in narrowing predicate: legal ONLY for
// a standard recipe (a dev/stage pair to narrow); simple/dev/managed-only all
// return a descriptive error so the agent can relay why the user's "dev only"
// can't apply.
func TestCanNarrowRecipeDevOnly(t *testing.T) {
	t.Parallel()
	parse := func(t *testing.T, y string) RecipeImportShape {
		t.Helper()
		s, err := ParseRecipeImportShape(y)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return s
	}
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name:    "standard_narrowable",
			yaml:    "services:\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n  - hostname: appstage\n    type: nodejs@22\n    zeropsSetup: prod\n",
			wantErr: false,
		},
		{
			name:    "simple_not_narrowable",
			yaml:    "services:\n  - hostname: app\n    type: nodejs@22\n    zeropsSetup: prod\n",
			wantErr: true,
		},
		{
			name:    "dev_already_dev_only",
			yaml:    "services:\n  - hostname: app\n    type: nodejs@22\n    zeropsSetup: dev\n",
			wantErr: true,
		},
		{
			name:    "managed_only_not_narrowable",
			yaml:    "services:\n  - hostname: db\n    type: postgresql@18\n",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := CanNarrowRecipeDevOnly(parse(t, tt.yaml))
			if (err != nil) != tt.wantErr {
				t.Errorf("CanNarrowRecipeDevOnly err = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

// TestDeriveRecipePlan_ServesHTTPAndSetupNames pins R3-P4's derive-from-shape
// stamps: ServesHTTP (false for a worker, true for HTTP runtimes) and the
// LITERAL zeropsSetup as the setup name (a worker's is "worker", not the
// mode-convention "prod"), plus StageEffectiveType for cross-type pairs.
func TestDeriveRecipePlan_ServesHTTPAndSetupNames(t *testing.T) {
	t.Parallel()
	parse := func(t *testing.T, y string) RecipeImportShape {
		t.Helper()
		s, err := ParseRecipeImportShape(y)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return s
	}

	t.Run("worker_app_servesHttp_and_setup_names", func(t *testing.T) {
		t.Parallel()
		shape := parse(t, `services:
  - hostname: appdev
    type: php-nginx@8.4
    zeropsSetup: dev
    buildFromGit: https://example.com/app
  - hostname: appstage
    type: php-nginx@8.4
    zeropsSetup: prod
    buildFromGit: https://example.com/app
  - hostname: workerstage
    type: php-nginx@8.4
    zeropsSetup: worker
    buildFromGit: https://example.com/app
`)
		targets, _ := DeriveRecipePlan(shape, RecipeShapeOverrides{})
		app, worker := targets[0].Runtime, targets[1].Runtime
		// HTTP runtimes leave ServesHTTP nil — verify uses the universal port
		// signal; only the worker carries the curated false.
		if app.ServesHTTP != nil {
			t.Errorf("app ServesHTTP = %v, want nil (defer to the live port signal)", *app.ServesHTTP)
		}
		if app.PrimarySetupName != "dev" || app.StageSetupName != "prod" {
			t.Errorf("app setup names = %q/%q, want dev/prod", app.PrimarySetupName, app.StageSetupName)
		}
		if worker.ServesHTTP == nil || *worker.ServesHTTP {
			t.Errorf("worker ServesHTTP = %v, want non-nil false", worker.ServesHTTP)
		}
		if worker.PrimarySetupName != "worker" {
			t.Errorf("worker PrimarySetupName = %q, want \"worker\" (literal zeropsSetup, NOT mode-convention \"prod\")", worker.PrimarySetupName)
		}
	})

	t.Run("simple_prod_setup_name", func(t *testing.T) {
		t.Parallel()
		simple := parse(t, "services:\n  - hostname: app\n    type: nodejs@22\n    zeropsSetup: prod\n    buildFromGit: https://example.com/app\n")
		st, _ := DeriveRecipePlan(simple, RecipeShapeOverrides{})
		if st[0].Runtime.PrimarySetupName != "prod" || st[0].Runtime.StageSetupName != "" {
			t.Errorf("simple setup names = %q/%q, want prod/\"\"", st[0].Runtime.PrimarySetupName, st[0].Runtime.StageSetupName)
		}
		if st[0].Runtime.ServesHTTP != nil {
			t.Errorf("simple ServesHTTP = %v, want nil (HTTP runtime defers to the port signal)", *st[0].Runtime.ServesHTTP)
		}
	})

	t.Run("cross_type_stage_effective_type", func(t *testing.T) {
		t.Parallel()
		cross := parse(t, "services:\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n    buildFromGit: https://example.com/x\n  - hostname: appstage\n    type: static@1.0\n    zeropsSetup: prod\n    buildFromGit: https://example.com/x\n")
		ct, _ := DeriveRecipePlan(cross, RecipeShapeOverrides{})
		if ct[0].Runtime.StageEffectiveType() != "static@1.0" {
			t.Errorf("StageEffectiveType() = %q, want static@1.0", ct[0].Runtime.StageEffectiveType())
		}
		// same-type pair: StageEffectiveType falls back to Type.
		same := parse(t, "services:\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n    buildFromGit: https://example.com/y\n  - hostname: appstage\n    type: nodejs@22\n    zeropsSetup: prod\n    buildFromGit: https://example.com/y\n")
		mt, _ := DeriveRecipePlan(same, RecipeShapeOverrides{})
		if mt[0].Runtime.StageEffectiveType() != "nodejs@22" {
			t.Errorf("same-type StageEffectiveType() = %q, want nodejs@22 (fallback to Type)", mt[0].Runtime.StageEffectiveType())
		}
	})
}

// TestPlanTargetSnapshots_CrossTypeStage pins R3-P4.0b: a cross-type standard
// target (nodejs dev → static stage) emits a stage snapshot carrying the STAGE
// type + its own runtime class, not the dev type — so cross-type stage atoms
// match correctly. Reading Runtime.Type for the stage half mis-classified it.
func TestPlanTargetSnapshots_CrossTypeStage(t *testing.T) {
	t.Parallel()
	target := BootstrapTarget{Runtime: RuntimeTarget{
		DevHostname:   "appdev",
		ExplicitStage: "appstage",
		Type:          "nodejs@22",
		StageType:     "static",
		BootstrapMode: topology.PlanModeStandard,
	}}
	snaps := planTargetSnapshots(target, nil)
	if len(snaps) != 2 {
		t.Fatalf("snapshots: got %d, want 2 (dev + stage)", len(snaps))
	}
	dev, stage := snaps[0], snaps[1]
	if dev.TypeVersion != "nodejs@22" {
		t.Errorf("dev snapshot TypeVersion = %q, want nodejs@22", dev.TypeVersion)
	}
	if stage.TypeVersion != "static" {
		t.Errorf("stage snapshot TypeVersion = %q, want static (cross-type stage), NOT the dev type", stage.TypeVersion)
	}
	if stage.RuntimeClass != classifyEnvelopeRuntime("static") {
		t.Errorf("stage snapshot RuntimeClass = %q, want the static class (classified from the stage type)", stage.RuntimeClass)
	}
}

// TestReconcileRecipeOverrides pins R3-P4.1: a submitted plan is reconciled
// into overrides by SIGNATURE (type+mode), reorder-safe; empty → empty; renames
// extracted by original-hostname identity; managed EXISTS by hostname; managed
// rename + count/signature mismatch rejected. The recipe owns the shape.
func TestReconcileRecipeOverrides(t *testing.T) {
	t.Parallel()
	parse := func(t *testing.T, y string) RecipeImportShape {
		t.Helper()
		s, err := ParseRecipeImportShape(y)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return s
	}
	// laravel-showcase-like: php standard pair + php worker + db managed.
	shape := parse(t, `services:
  - hostname: appdev
    type: php-nginx@8.4
    zeropsSetup: dev
    buildFromGit: https://example.com/app
  - hostname: appstage
    type: php-nginx@8.4
    zeropsSetup: prod
    buildFromGit: https://example.com/app
  - hostname: workerstage
    type: php-nginx@8.4
    zeropsSetup: worker
    buildFromGit: https://example.com/app
  - hostname: db
    type: postgresql@18
`)
	derived, _ := DeriveRecipePlan(shape, RecipeShapeOverrides{})

	t.Run("empty_submission_empty_overrides", func(t *testing.T) {
		t.Parallel()
		ov, err := reconcileRecipeOverrides(shape, nil, false)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if ov.RuntimeHostnameByOriginal != nil || ov.ManagedResolutionByHost != nil {
			t.Errorf("empty submission must yield empty overrides, got %+v", ov)
		}
	})

	t.Run("verbatim_submission_no_renames", func(t *testing.T) {
		t.Parallel()
		ov, err := reconcileRecipeOverrides(shape, derived, false)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if len(ov.RuntimeHostnameByOriginal) != 0 {
			t.Errorf("submitting the derived shape verbatim must yield no renames, got %+v", ov.RuntimeHostnameByOriginal)
		}
	})

	t.Run("reorder_is_safe", func(t *testing.T) {
		t.Parallel()
		// Reverse the derived target order — signature match must still pair
		// each correctly (no ordinal swap).
		reordered := []BootstrapTarget{derived[1], derived[0]}
		ov, err := reconcileRecipeOverrides(shape, reordered, false)
		if err != nil {
			t.Fatalf("reorder must reconcile cleanly: %v", err)
		}
		if len(ov.RuntimeHostnameByOriginal) != 0 {
			t.Errorf("reordered verbatim shape must yield no renames, got %+v", ov.RuntimeHostnameByOriginal)
		}
	})

	t.Run("rename_extracted_by_identity", func(t *testing.T) {
		t.Parallel()
		// Rename the standard pair's dev+stage; keep the worker.
		renamed := []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "myappdev", ExplicitStage: "myappstage", Type: "php-nginx@8.4", BootstrapMode: topology.PlanModeStandard}},
			{Runtime: RuntimeTarget{DevHostname: "workerstage", Type: "php-nginx@8.4", BootstrapMode: topology.PlanModeSimple}},
		}
		ov, err := reconcileRecipeOverrides(shape, renamed, false)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if ov.RuntimeHostnameByOriginal["appdev"] != "myappdev" || ov.RuntimeHostnameByOriginal["appstage"] != "myappstage" {
			t.Errorf("renames = %+v, want appdev→myappdev, appstage→myappstage", ov.RuntimeHostnameByOriginal)
		}
	})

	t.Run("managed_exists_flip", func(t *testing.T) {
		t.Parallel()
		withExists := []BootstrapTarget{
			{Runtime: derived[0].Runtime, Dependencies: []Dependency{{Hostname: "db", Type: "postgresql@18", Resolution: ResolutionExists}}},
			{Runtime: derived[1].Runtime},
		}
		ov, err := reconcileRecipeOverrides(shape, withExists, false)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if ov.ManagedResolutionByHost["db"] != ResolutionExists {
			t.Errorf("EXISTS flip = %+v, want db→EXISTS", ov.ManagedResolutionByHost)
		}
	})

	t.Run("partial_standard_rename_without_stage_hits_app_not_worker", func(t *testing.T) {
		t.Parallel()
		// laravel-showcase has a php app PAIR + a php WORKER (same type). Rename
		// only the app's dev half, declaring bootstrapMode=standard but NO
		// stageHostname. The mode marks it "pair" → matches the app, NOT the
		// same-type worker bucket (BUG-2 regression guard).
		partial := []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "renamedapp", Type: "php-nginx@8.4", BootstrapMode: topology.PlanModeStandard}},
		}
		ov, err := reconcileRecipeOverrides(shape, partial, false)
		if err != nil {
			t.Fatalf("partial standard rename must reconcile: %v", err)
		}
		if ov.RuntimeHostnameByOriginal["appdev"] != "renamedapp" {
			t.Errorf("rename should map appdev→renamedapp, got %+v", ov.RuntimeHostnameByOriginal)
		}
		if _, touched := ov.RuntimeHostnameByOriginal["workerstage"]; touched {
			t.Errorf("the worker must NOT be touched by an app-pair rename, got %+v", ov.RuntimeHostnameByOriginal)
		}
	})

	t.Run("lowercase_exists_flips", func(t *testing.T) {
		t.Parallel()
		// resolution normalization runs in validation, AFTER reconcile — accept
		// a lowercased "exists" here too (BUG-3 regression guard).
		withExists := []BootstrapTarget{
			{Runtime: derived[0].Runtime, Dependencies: []Dependency{{Hostname: "db", Type: "postgresql@18", Resolution: "exists"}}},
		}
		ov, err := reconcileRecipeOverrides(shape, withExists, false)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if ov.ManagedResolutionByHost["db"] != ResolutionExists {
			t.Errorf("lowercase \"exists\" should flip db→EXISTS, got %+v", ov.ManagedResolutionByHost)
		}
	})

	t.Run("managed_rename_rejected", func(t *testing.T) {
		t.Parallel()
		bad := []BootstrapTarget{
			{Runtime: derived[0].Runtime, Dependencies: []Dependency{{Hostname: "mydb", Type: "postgresql@18", Resolution: ResolutionCreate}}},
			{Runtime: derived[1].Runtime},
		}
		if _, err := reconcileRecipeOverrides(shape, bad, false); err == nil {
			t.Error("renaming a managed dep must be rejected (backs ${host_*} refs)")
		}
	})

	t.Run("partial_submission_allowed", func(t *testing.T) {
		t.Parallel()
		// Submit ONLY the app pair (rename it); omit the worker. The worker
		// still derives unchanged — a partial plan renames what's listed and
		// derives the rest (no need to re-state targets you don't touch).
		partial := []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "myappdev", ExplicitStage: "myappstage", Type: "php-nginx@8.4"}},
		}
		ov, err := reconcileRecipeOverrides(shape, partial, false)
		if err != nil {
			t.Fatalf("partial submission must reconcile: %v", err)
		}
		if ov.RuntimeHostnameByOriginal["appdev"] != "myappdev" {
			t.Errorf("partial rename = %+v, want appdev→myappdev", ov.RuntimeHostnameByOriginal)
		}
	})

	t.Run("unknown_signature_rejected", func(t *testing.T) {
		t.Parallel()
		bad := []BootstrapTarget{
			derived[0], derived[1],
			{Runtime: RuntimeTarget{DevHostname: "extra", Type: "golang@1", BootstrapMode: topology.PlanModeSimple}},
		}
		if _, err := reconcileRecipeOverrides(shape, bad, false); err == nil {
			t.Error("a target whose type the recipe doesn't have must be rejected")
		}
	})
}

// TestReconcileRecipeOverrides_DevOnly pins that the dev-only opt-in survives
// reconcile (empty and renamed submissions) and that a renamed lone dev target
// matches the NARROWED shape rather than mis-bucketing against the full pair.
func TestReconcileRecipeOverrides_DevOnly(t *testing.T) {
	t.Parallel()
	shape, err := ParseRecipeImportShape(`services:
  - hostname: appdev
    type: php-nginx@8.4
    zeropsSetup: dev
    buildFromGit: https://example.com/app
  - hostname: appstage
    type: php-nginx@8.4
    zeropsSetup: prod
    buildFromGit: https://example.com/app
  - hostname: db
    type: postgresql@18
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	t.Run("carries_through_empty_submission", func(t *testing.T) {
		t.Parallel()
		ov, err := reconcileRecipeOverrides(shape, nil, true)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if !ov.DevOnly {
			t.Error("devOnly must survive an empty submission (the narrow opt-in is not a rename)")
		}
	})

	t.Run("rename_matches_narrowed_derive", func(t *testing.T) {
		t.Parallel()
		// Under dev-only the submission is matched against the NARROWED shape (a
		// single dev target), so a renamed lone dev target reconciles cleanly
		// instead of mis-bucketing against the full pair's signatures.
		submitted := []BootstrapTarget{
			{Runtime: RuntimeTarget{DevHostname: "myapp", Type: "php-nginx@8.4", BootstrapMode: topology.PlanModeDev}},
		}
		ov, err := reconcileRecipeOverrides(shape, submitted, true)
		if err != nil {
			t.Fatalf("dev-only rename must reconcile against the narrowed shape: %v", err)
		}
		if !ov.DevOnly || ov.RuntimeHostnameByOriginal["appdev"] != "myapp" {
			t.Errorf("dev-only rename = %+v, want DevOnly + appdev→myapp", ov)
		}
	})
}

// (TestValidateBootstrapRecipeMode deleted in R3-P4.2 with the function it
// pinned — the recipe plan is derived, so its mode cannot deviate.)
