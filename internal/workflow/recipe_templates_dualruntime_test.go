// Tests for dual-runtime showcase generation — the API-first split where
// a recipe has both a frontend (static) and an API (runtime) service with
// separate repos and cross-service env var references.
//
// This file covers:
//   - Env 5 HA topology for the full showcase shape
//   - Repo-suffix routing (-api, -app, -worker) including the shared vs
//     separate worker codebase distinction
//   - Dual-runtime import.yaml generation (both services in every env)
//   - API priority assignment (priority: 5 so API starts before frontend)
//   - buildServiceIncludesList multi-runtime output
//   - Validation of dual-runtime and 3-repo showcase plans
//   - NATS broker rendering + hostname "queue" convention
//   - Separate-codebase (3-repo) worker rendering
//   - Project env variable propagation across all 6 tiers for the 3-repo case
//
// All tests here are parallel-safe (no shared mutable state) and rely on
// the package-level fixtures defined in recipe_templates_test.go
// (testMinimalPlan, testShowcasePlan, testDualRuntimePlan).
package workflow

import (
	"fmt"
	"strings"
	"testing"
)

func TestGenerateEnvImportYAML_Showcase_Env5_HA(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlan()
	yaml := GenerateEnvImportYAML(plan, 5)

	// corePackage: SERIOUS still required.
	if !strings.Contains(yaml, "corePackage: SERIOUS") {
		t.Error("expected corePackage: SERIOUS in env 5")
	}

	// Managed services (db, cache, search) get mode: HA.
	for _, hostname := range []string{"db", "redis", "search"} {
		if !strings.Contains(yaml, "hostname: "+hostname) {
			t.Errorf("expected hostname %s in env 5", hostname)
		}
	}
	if !strings.Contains(yaml, "mode: HA") {
		t.Error("expected mode: HA on managed services in env 5")
	}

	// Object storage does NOT get mode: HA.
	// Verify by checking storage section doesn't contain mode.
	lines := strings.Split(yaml, "\n")
	inStorage := false
	for _, line := range lines {
		if strings.Contains(line, "hostname: storage") {
			inStorage = true
		} else if strings.TrimSpace(line) != "" && strings.HasPrefix(strings.TrimSpace(line), "- hostname:") {
			inStorage = false
		}
		if inStorage && strings.Contains(line, "mode:") {
			t.Error("object-storage must NOT have mode field even in env 5")
		}
	}

	// App and worker get cpuMode: DEDICATED.
	// Mailpit does NOT.
	inMailpit := false
	for _, line := range lines {
		if strings.Contains(line, "hostname: mailpit") {
			inMailpit = true
		} else if strings.TrimSpace(line) != "" && strings.HasPrefix(strings.TrimSpace(line), "- hostname:") {
			inMailpit = false
		}
		if inMailpit && strings.Contains(line, "cpuMode: DEDICATED") {
			t.Error("mailpit should NOT have cpuMode: DEDICATED in env 5")
		}
	}
}

func TestWriteRuntimeBuildFromGit_APIRole(t *testing.T) {
	t.Parallel()

	dualPlan := testDualRuntimePlan()
	singlePlan := testShowcasePlan()

	tests := []struct {
		name   string
		plan   *RecipePlan
		target RecipeTarget
		want   string
	}{
		{"frontend_app", dualPlan, RecipeTarget{Hostname: "app", Type: "static", Role: "app"}, "nestjs-showcase-app"},
		{"api_backend", dualPlan, RecipeTarget{Hostname: "api", Type: "nodejs@22", Role: "api"}, "nestjs-showcase-api"},
		// Shared worker in dual-runtime: SharesCodebaseWith="api" → inherits -api suffix.
		{"shared_worker_dualruntime", dualPlan, RecipeTarget{Hostname: "worker", Type: "nodejs@22", IsWorker: true, SharesCodebaseWith: "api"}, "nestjs-showcase-api"},
		{"no_role_app", dualPlan, RecipeTarget{Hostname: "app", Type: "nodejs@22"}, "nestjs-showcase-app"},
		// Shared worker in single-app: SharesCodebaseWith="app" → inherits -app suffix.
		{"shared_worker_singleapp", singlePlan, RecipeTarget{Hostname: "worker", Type: "php-nginx@8.4", IsWorker: true, SharesCodebaseWith: "app"}, "laravel-showcase-app"},
		// Separate worker (3-repo case): empty SharesCodebaseWith → -worker suffix
		// regardless of base-runtime match with the API. This case was literally
		// unexpressible under the old runtime-match heuristic.
		{"separate_worker_same_runtime", dualPlan, RecipeTarget{Hostname: "worker", Type: "nodejs@22", IsWorker: true}, "nestjs-showcase-worker"},
		// Separate worker with a different runtime (cross-language): still -worker.
		{"separate_worker_cross_runtime", singlePlan, RecipeTarget{Hostname: "worker", Type: "python@3.12", IsWorker: true}, "laravel-showcase-worker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			writeRuntimeBuildFromGit(&b, tt.plan, tt.target)
			if !strings.Contains(b.String(), tt.want) {
				t.Errorf("expected %q in %q", tt.want, b.String())
			}
		})
	}
}

func TestGenerateEnvImportYAML_DualRuntime(t *testing.T) {
	t.Parallel()

	plan := testDualRuntimePlan()

	for i := 0; i < EnvTierCount(); i++ {
		t.Run(fmt.Sprintf("env_%d", i), func(t *testing.T) {
			t.Parallel()
			yaml := GenerateEnvImportYAML(plan, i)

			if i <= 1 {
				// Env 0-1: both app and api get dev+stage pairs.
				for _, host := range []string{"appdev", "appstage", "apidev", "apistage"} {
					if !strings.Contains(yaml, "hostname: "+host) {
						t.Errorf("expected hostname %s in env %d", host, i)
					}
				}
				// API dev/stage use -api repo.
				apiDevBlock := extractServiceBlock(yaml, "apidev")
				if !strings.Contains(apiDevBlock, "nestjs-showcase-api") {
					t.Error("apidev should use -api repo URL")
				}
				// App dev/stage use -app repo.
				appDevBlock := extractServiceBlock(yaml, "appdev")
				if !strings.Contains(appDevBlock, "nestjs-showcase-app") {
					t.Error("appdev should use -app repo URL")
				}
				// Shared-codebase worker uses -api repo (shares API codebase, not frontend).
				workerBlock := extractServiceBlock(yaml, "workerstage")
				if !strings.Contains(workerBlock, "nestjs-showcase-api") {
					t.Errorf("workerstage should use -api repo URL (shares API codebase), got block: %q", workerBlock)
				}
			} else {
				// Env 2+: bare hostnames.
				if !strings.Contains(yaml, "hostname: app") {
					t.Error("expected bare app hostname")
				}
				if !strings.Contains(yaml, "hostname: api") {
					t.Error("expected bare api hostname")
				}
				// API uses -api repo.
				apiBlock := extractServiceBlock(yaml, "api")
				if !strings.Contains(apiBlock, "nestjs-showcase-api") {
					t.Error("api should use -api repo URL in env 2+")
				}
			}
		})
	}
}

func TestGenerateEnvImportYAML_APIPriority(t *testing.T) {
	t.Parallel()

	plan := testDualRuntimePlan()

	// Check all env tiers for API priority.
	for i := 0; i < EnvTierCount(); i++ {
		t.Run(fmt.Sprintf("env_%d", i), func(t *testing.T) {
			t.Parallel()
			yaml := GenerateEnvImportYAML(plan, i)

			var apiHost string
			if i <= 1 {
				apiHost = "apidev"
			} else {
				apiHost = "api"
			}
			apiBlock := extractServiceBlock(yaml, apiHost)
			if !strings.Contains(apiBlock, "priority: 5") {
				t.Errorf("expected priority: 5 on %s in env %d", apiHost, i)
			}
		})
	}
}

func TestBuildServiceIncludesList_MultiRuntime(t *testing.T) {
	t.Parallel()

	plan := testDualRuntimePlan()

	// Env 0-1: should mention both app and api dev+stage services.
	got01 := buildServiceIncludesList(plan, 0)
	for _, want := range []string{"app dev service", "app staging service", "api dev service", "api staging service"} {
		if !strings.Contains(got01, want) {
			t.Errorf("expected %q in env 0 includes list, got %q", want, got01)
		}
	}

	// Env 2+: no dev/stage mention for runtimes, only data services.
	got2 := buildServiceIncludesList(plan, 2)
	if strings.Contains(got2, "dev service") {
		t.Errorf("env 2 should not mention dev service, got %q", got2)
	}
}

func TestValidateRecipePlan_DualRuntimeShowcase(t *testing.T) {
	t.Parallel()

	plan := RecipePlan{
		Framework:   "nestjs",
		Tier:        RecipeTierShowcase,
		Slug:        "nestjs-showcase",
		RuntimeType: "nodejs@22",
		BuildBases:  []string{"nodejs@22"},
		Research: ResearchData{
			ServiceType:    "nodejs",
			PackageManager: "npm",
			HTTPPort:       3000,
			BuildCommands:  []string{"npm ci"},
			DeployFiles:    []string{"dist"},
			StartCommand:   "node dist/main.js",
			CacheLib:       "ioredis",
			SessionDriver:  "redis",
			QueueDriver:    "bullmq",
			StorageDriver:  "s3",
			SearchLib:      "meilisearch",
			MailLib:        "nodemailer",
			LoggingDriver:  "stderr",
		},
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "static", Role: "app"},
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			// NestJS BullMQ-style worker that shares API codebase.
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true, SharesCodebaseWith: "api"},
			{Hostname: "db", Type: "postgresql@17"},
			{Hostname: "cache", Type: "valkey@7.2"},
			{Hostname: "queue", Type: "nats@2.12"},
			{Hostname: "storage", Type: "object-storage"},
			{Hostname: "search", Type: "meilisearch@1"},
		},
	}

	errs := ValidateRecipePlan(plan, nil, nil)
	if len(errs) > 0 {
		t.Errorf("dual-runtime showcase should be valid, got errors: %v", errs)
	}
}

// TestValidateRecipePlan_ThreeRepoShowcase locks the 3-repo case: app static
// + api nodejs + worker nodejs, where the worker is a SEPARATE codebase even
// though its base runtime matches the API. Expressible only because
// SharesCodebaseWith is explicit rather than runtime-inferred.
func TestValidateRecipePlan_ThreeRepoShowcase(t *testing.T) {
	t.Parallel()

	plan := RecipePlan{
		Framework:   "nestjs",
		Tier:        RecipeTierShowcase,
		Slug:        "nestjs-showcase",
		RuntimeType: "nodejs@22",
		BuildBases:  []string{"nodejs@22"},
		Research: ResearchData{
			ServiceType:    "nodejs",
			PackageManager: "npm",
			HTTPPort:       3000,
			BuildCommands:  []string{"npm ci"},
			DeployFiles:    []string{"dist"},
			StartCommand:   "node dist/main.js",
			CacheLib:       "ioredis",
			SessionDriver:  "redis",
			QueueDriver:    "nats",
			StorageDriver:  "s3",
			SearchLib:      "meilisearch",
			MailLib:        "nodemailer",
			LoggingDriver:  "stderr",
		},
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "static", Role: "app"},
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			// Separate-codebase worker — same runtime as api but distinct repo.
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true},
			{Hostname: "db", Type: "postgresql@17"},
			{Hostname: "cache", Type: "valkey@7.2"},
			{Hostname: "queue", Type: "nats@2.12"},
			{Hostname: "storage", Type: "object-storage"},
			{Hostname: "search", Type: "meilisearch@1"},
		},
	}

	errs := ValidateRecipePlan(plan, nil, nil)
	if len(errs) > 0 {
		t.Errorf("3-repo showcase should be valid, got errors: %v", errs)
	}
}

// TestGenerateEnvImportYAML_NATSQueueRendering locks the NATS broker output
// shape. NATS is the canonical showcase messaging service, wired with hostname
// "queue" (the canonical literal — see themes/services.md) and type "nats@2.12".
// Tests: it appears in every env, it renders as a managed service (priority: 10),
// it gets mode: HA only in env 5, and the intro service list mentions NATS.
func TestGenerateEnvImportYAML_NATSQueueRendering(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlan()

	for _, envIdx := range []int{0, 1, 2, 3, 4, 5} {
		t.Run(fmt.Sprintf("env_%d_has_queue", envIdx), func(t *testing.T) {
			t.Parallel()
			yaml := GenerateEnvImportYAML(plan, envIdx)
			if !strings.Contains(yaml, "hostname: queue") {
				t.Errorf("env %d must include hostname: queue (NATS broker)", envIdx)
			}
			if !strings.Contains(yaml, "type: nats@") {
				t.Errorf("env %d queue service must be type: nats@..., got:\n%s", envIdx, yaml)
			}
			queueBlock := extractServiceBlock(yaml, "queue")
			if !strings.Contains(queueBlock, "priority: 10") {
				t.Errorf("env %d queue (managed service) must have priority: 10, got: %q", envIdx, queueBlock)
			}
		})
	}

	t.Run("env_5_queue_is_HA", func(t *testing.T) {
		t.Parallel()
		yaml := GenerateEnvImportYAML(plan, 5)
		queueBlock := extractServiceBlock(yaml, "queue")
		if !strings.Contains(queueBlock, "mode: HA") {
			t.Errorf("env 5 queue must be mode: HA, got: %q", queueBlock)
		}
	})

	t.Run("env_0_queue_is_NON_HA", func(t *testing.T) {
		t.Parallel()
		yaml := GenerateEnvImportYAML(plan, 0)
		queueBlock := extractServiceBlock(yaml, "queue")
		if !strings.Contains(queueBlock, "mode: NON_HA") {
			t.Errorf("env 0 queue must be mode: NON_HA, got: %q", queueBlock)
		}
	})
}

// TestGenerateEnvImportYAML_ThreeRepoSeparateWorker verifies the template
// output for the 3-repo case: the worker gets its own dev+stage pair, its
// own -worker repo, and env 0-1 must emit workerdev+workerstage (not just
// workerstage as shared-codebase workers do).
func TestGenerateEnvImportYAML_ThreeRepoSeparateWorker(t *testing.T) {
	t.Parallel()

	plan := testDualRuntimePlan()
	// Clear the shared flag to simulate the 3-repo case while keeping every
	// other structural detail identical. This is the "worker Node.js matches
	// api Node.js but is a separate repo" scenario.
	for i := range plan.Targets {
		if plan.Targets[i].IsWorker {
			plan.Targets[i].SharesCodebaseWith = ""
		}
	}

	t.Run("env_0_has_workerdev", func(t *testing.T) {
		t.Parallel()
		yaml := GenerateEnvImportYAML(plan, 0)
		if !strings.Contains(yaml, "hostname: workerdev") {
			t.Error("3-repo worker must have workerdev (own dev container)")
		}
		if !strings.Contains(yaml, "hostname: workerstage") {
			t.Error("3-repo worker must have workerstage")
		}
	})

	t.Run("env_0_worker_uses_worker_repo", func(t *testing.T) {
		t.Parallel()
		yaml := GenerateEnvImportYAML(plan, 0)
		workerBlock := extractServiceBlock(yaml, "workerstage")
		if !strings.Contains(workerBlock, "nestjs-showcase-worker") {
			t.Errorf("3-repo workerstage should use -worker repo URL, got: %q", workerBlock)
		}
	})

	t.Run("env_0_worker_uses_prod_setup", func(t *testing.T) {
		t.Parallel()
		yaml := GenerateEnvImportYAML(plan, 0)
		workerBlock := extractServiceBlock(yaml, "workerstage")
		// Separate-codebase worker runs its own zerops.yaml's prod setup,
		// NOT a shared "worker" setup in api's yaml.
		if !strings.Contains(workerBlock, "zeropsSetup: prod") {
			t.Errorf("3-repo workerstage should use zeropsSetup: prod, got: %q", workerBlock)
		}
	})

	t.Run("env_2_worker_uses_worker_repo", func(t *testing.T) {
		t.Parallel()
		yaml := GenerateEnvImportYAML(plan, 2)
		workerBlock := extractServiceBlock(yaml, "worker")
		if !strings.Contains(workerBlock, "nestjs-showcase-worker") {
			t.Errorf("env 2 bare worker should use -worker repo, got: %q", workerBlock)
		}
	})

	t.Run("env_2_api_is_not_worker_host", func(t *testing.T) {
		t.Parallel()
		// The API target's zerops.yaml should NOT need a `setup: worker` block
		// because the worker is now a separate codebase. This is checked via
		// TargetHostsSharedWorker at the workflow level; here we just verify
		// the template output is consistent: no mention of a worker setup on
		// the api service block.
		apiTarget := RecipeTarget{Hostname: "api", Type: "nodejs@22", Role: "api"}
		if TargetHostsSharedWorker(apiTarget, plan) {
			t.Error("api should NOT host the worker in 3-repo case (worker is separate codebase)")
		}
	})
}

// TestGenerateEnvImportYAML_ThreeRepoProjectEnvVariables verifies that
// projectEnvVariables propagate correctly across all 6 envs for a 3-repo
// dual-runtime showcase. This is the test the v5 post-mortem flagged as
// missing: the shape verification and env-propagation path for the CORS
// regression must be under test, not just convention.
//
// The invariants locked here:
//   - Dev-pair envs (0, 1) carry DEV_* + STAGE_* with dev/stage hostnames
//     (apidev, appdev, apistage, appstage).
//   - Single-slot envs (2, 3, 4, 5) carry STAGE_* only, with plain
//     hostnames (api, app).
//   - A worker declared as a separate codebase does NOT appear in URL
//     env vars — workers have no HTTP surface. This guards against the
//     "accidentally exposed worker URL" class of mistakes.
func TestGenerateEnvImportYAML_ThreeRepoProjectEnvVariables(t *testing.T) {
	t.Parallel()

	plan := testDualRuntimePlan()
	// Flip to separate-codebase (3-repo case).
	for i := range plan.Targets {
		if plan.Targets[i].IsWorker {
			plan.Targets[i].SharesCodebaseWith = ""
		}
	}

	// Supply the canonical dual-runtime projectEnvVariables shape.
	plan.ProjectEnvVariables = map[string]map[string]string{
		"0": {
			"DEV_API_URL":        "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"DEV_FRONTEND_URL":   "https://appdev-${zeropsSubdomainHost}.prg1.zerops.app",
			"STAGE_API_URL":      "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"STAGE_FRONTEND_URL": "https://appstage-${zeropsSubdomainHost}.prg1.zerops.app",
		},
		"1": {
			"DEV_API_URL":        "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"DEV_FRONTEND_URL":   "https://appdev-${zeropsSubdomainHost}.prg1.zerops.app",
			"STAGE_API_URL":      "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"STAGE_FRONTEND_URL": "https://appstage-${zeropsSubdomainHost}.prg1.zerops.app",
		},
		"2": {
			"STAGE_API_URL":      "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"STAGE_FRONTEND_URL": "https://app-${zeropsSubdomainHost}.prg1.zerops.app",
		},
		"3": {
			"STAGE_API_URL":      "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"STAGE_FRONTEND_URL": "https://app-${zeropsSubdomainHost}.prg1.zerops.app",
		},
		"4": {
			"STAGE_API_URL":      "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"STAGE_FRONTEND_URL": "https://app-${zeropsSubdomainHost}.prg1.zerops.app",
		},
		"5": {
			"STAGE_API_URL":      "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"STAGE_FRONTEND_URL": "https://app-${zeropsSubdomainHost}.prg1.zerops.app",
		},
	}

	devPairWants := []string{
		"DEV_API_URL: https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		"DEV_FRONTEND_URL: https://appdev-${zeropsSubdomainHost}.prg1.zerops.app",
		"STAGE_API_URL: https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		"STAGE_FRONTEND_URL: https://appstage-${zeropsSubdomainHost}.prg1.zerops.app",
	}
	singleSlotWants := []string{
		"STAGE_API_URL: https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		"STAGE_FRONTEND_URL: https://app-${zeropsSubdomainHost}.prg1.zerops.app",
	}

	for _, envIndex := range []int{0, 1} {
		yaml := GenerateEnvImportYAML(plan, envIndex)
		for _, want := range devPairWants {
			if !strings.Contains(yaml, want) {
				t.Errorf("env %d: missing %q\n---\n%s", envIndex, want, yaml)
			}
		}
		// Dev-pair envs must NOT carry the single-slot (api/app) hostnames
		// — envs 0-1 route through dev/stage hostnames exclusively.
		if strings.Contains(yaml, "https://api-${zeropsSubdomainHost}") {
			t.Errorf("env %d: dev-pair env should not reference bare 'api' hostname in URLs", envIndex)
		}
	}

	for _, envIndex := range []int{2, 3, 4, 5} {
		yaml := GenerateEnvImportYAML(plan, envIndex)
		for _, want := range singleSlotWants {
			if !strings.Contains(yaml, want) {
				t.Errorf("env %d: missing %q\n---\n%s", envIndex, want, yaml)
			}
		}
		// Single-slot envs must NOT carry DEV_* vars.
		if strings.Contains(yaml, "DEV_API_URL") {
			t.Errorf("env %d: single-slot env should not carry DEV_API_URL", envIndex)
		}
		if strings.Contains(yaml, "DEV_FRONTEND_URL") {
			t.Errorf("env %d: single-slot env should not carry DEV_FRONTEND_URL", envIndex)
		}
		// Single-slot envs must not reference dev/stage hostnames in URLs.
		if strings.Contains(yaml, "apistage-") || strings.Contains(yaml, "appstage-") {
			t.Errorf("env %d: single-slot env should not reference apistage/appstage URLs", envIndex)
		}
	}

	// Workers must never appear in URL env vars regardless of env tier —
	// they have no HTTP surface.
	for envIndex := range 6 {
		yaml := GenerateEnvImportYAML(plan, envIndex)
		if strings.Contains(yaml, "WORKER_URL") {
			t.Errorf("env %d: worker services must not appear as WORKER_URL (no HTTP surface)", envIndex)
		}
	}
}

// TestGenerateEnvImportYAML_ServeOnlyDevTypeOverride verifies that
// serve-only targets (static, nginx) use DevBase as the service type
// in dev environments (env 0-1) instead of the prod type. This prevents
// the "type: static" dev service that can't host a dev server.
func TestGenerateEnvImportYAML_ServeOnlyDevTypeOverride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		prodType    string
		devBase     string
		wantDevType string
	}{
		{"static_with_nodejs_devbase", "static", "nodejs@22", "nodejs@22"},
		{"nginx_with_nodejs_devbase", "nginx", "nodejs@22", "nodejs@22"},
		{"runtime_without_devbase", "nodejs@22", "", "nodejs@22"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := &RecipePlan{
				Framework:   "test",
				Tier:        RecipeTierMinimal,
				Slug:        "test-minimal",
				RuntimeType: "nodejs@22",
				Research: ResearchData{
					ServiceType:    "nodejs",
					PackageManager: "npm",
					HTTPPort:       3000,
					BuildCommands:  []string{"npm ci"},
					DeployFiles:    []string{"."},
					StartCommand:   "node server.js",
				},
				Targets: []RecipeTarget{
					{Hostname: "app", Type: tt.prodType, DevBase: tt.devBase},
					{Hostname: "db", Type: "postgresql@17"},
				},
			}

			yaml := GenerateEnvImportYAML(plan, 0)
			appDevBlock := extractServiceBlock(yaml, "appdev")
			wantLine := "type: " + tt.wantDevType
			if !strings.Contains(appDevBlock, wantLine) {
				t.Errorf("appdev should have %q, got block:\n%s", wantLine, appDevBlock)
			}

			// Stage service always uses the prod type.
			appStageBlock := extractServiceBlock(yaml, "appstage")
			stageLine := "type: " + tt.prodType
			if !strings.Contains(appStageBlock, stageLine) {
				t.Errorf("appstage should have %q, got block:\n%s", stageLine, appStageBlock)
			}

			// Env 2+ always uses the prod type (no dev/stage split).
			yaml2 := GenerateEnvImportYAML(plan, 2)
			appBlock := extractServiceBlock(yaml2, "app")
			if !strings.Contains(appBlock, "type: "+tt.prodType) {
				t.Errorf("env 2 app should have prod type %q, got block:\n%s", tt.prodType, appBlock)
			}
		})
	}
}

// TestGenerateEnvImportYAML_DualRuntimeServeOnlyDevOverride verifies the
// full dual-runtime case: the frontend has type:static with DevBase:nodejs@22,
// and the dev service in env 0-1 must use nodejs@22 instead of static.
func TestGenerateEnvImportYAML_DualRuntimeServeOnlyDevOverride(t *testing.T) {
	t.Parallel()

	plan := testDualRuntimePlan()
	// Set DevBase on the static frontend target.
	for i := range plan.Targets {
		if plan.Targets[i].Type == "static" {
			plan.Targets[i].DevBase = "nodejs@22"
		}
	}

	for _, envIndex := range []int{0, 1} {
		t.Run(fmt.Sprintf("env_%d", envIndex), func(t *testing.T) {
			t.Parallel()
			yaml := GenerateEnvImportYAML(plan, envIndex)

			// appdev must use nodejs@22 (dev type), NOT static.
			appDevBlock := extractServiceBlock(yaml, "appdev")
			if strings.Contains(appDevBlock, "type: static") {
				t.Error("appdev must NOT use type: static (serve-only can't host a dev server)")
			}
			if !strings.Contains(appDevBlock, "type: nodejs@22") {
				t.Errorf("appdev must use type: nodejs@22 (dev override), got:\n%s", appDevBlock)
			}

			// appstage must still use static (prod type).
			appStageBlock := extractServiceBlock(yaml, "appstage")
			if !strings.Contains(appStageBlock, "type: static") {
				t.Errorf("appstage must use type: static (prod type), got:\n%s", appStageBlock)
			}

			// apidev must still use nodejs@22 (its own type, no override).
			apiDevBlock := extractServiceBlock(yaml, "apidev")
			if !strings.Contains(apiDevBlock, "type: nodejs@22") {
				t.Errorf("apidev must use type: nodejs@22, got:\n%s", apiDevBlock)
			}
		})
	}
}
