package workflow

import (
	"strings"
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

func TestValidateBootstrapRecipeMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		match    *RecipeMatch
		targets  []BootstrapTarget
		wantErr  bool
		errMatch string
	}{
		{
			name:  "nil_match_no_check",
			match: nil,
			targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "app", Type: "nodejs@22", BootstrapMode: "simple"}},
			},
		},
		{
			name:  "empty_mode_no_check",
			match: &RecipeMatch{Slug: "foo", Mode: ""},
			targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "app", Type: "nodejs@22", BootstrapMode: "simple"}},
			},
		},
		{
			name:  "standard_matches_standard",
			match: &RecipeMatch{Slug: "nestjs-minimal", Mode: "standard"},
			targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "standard"}},
			},
		},
		{
			name:  "standard_vs_simple_rejected",
			match: &RecipeMatch{Slug: "nestjs-minimal", Mode: "standard"},
			targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "app", Type: "nodejs@22", BootstrapMode: "simple"}},
			},
			wantErr:  true,
			errMatch: "recipe \"nestjs-minimal\" is standard mode",
		},
		{
			name:  "simple_vs_standard_rejected",
			match: &RecipeMatch{Slug: "nextjs-ssr-hello-world", Mode: "simple"},
			targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "standard"}},
			},
			wantErr:  true,
			errMatch: "recipe \"nextjs-ssr-hello-world\" is simple mode",
		},
		{
			name:  "explicit_standard_matches_recipe",
			match: &RecipeMatch{Slug: "r", Mode: "standard"},
			targets: []BootstrapTarget{
				{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "standard"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateBootstrapRecipeMode(tt.match, tt.targets)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				if tt.errMatch != "" && !strings.Contains(err.Error(), tt.errMatch) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errMatch)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
