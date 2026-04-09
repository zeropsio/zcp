package workflow

import (
	"fmt"
	"strings"
	"testing"
)

// repoURL is the expected buildFromGit URL pattern.
const testRepoBase = "https://github.com/zerops-recipe-apps/"

func testMinimalPlan() *RecipePlan {
	return &RecipePlan{
		Framework:   "laravel",
		Tier:        RecipeTierMinimal,
		Slug:        "laravel-minimal",
		RuntimeType: "php-nginx@8.4",
		Research: ResearchData{
			ServiceType:    "php-nginx",
			PackageManager: "composer",
			HTTPPort:       80,
			BuildCommands:  []string{"composer install"},
			DeployFiles:    []string{"."},
			StartCommand:   "php artisan serve",
			NeedsAppSecret: true,
			AppSecretKey:   "APP_KEY",
			DBDriver:       "mysql",
			MigrationCmd:   "php artisan migrate",
		},
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "php-nginx@8.4", Environments: []string{"0", "1", "2", "3", "4", "5"}},
			{Hostname: "db", Type: "mariadb@10.11", Environments: []string{"0", "1", "2", "3", "4", "5"}},
		},
	}
}

func testShowcasePlan() *RecipePlan {
	plan := testMinimalPlan()
	plan.Tier = RecipeTierShowcase
	plan.Slug = "laravel-showcase"
	plan.Targets = append(plan.Targets,
		RecipeTarget{Hostname: "redis", Type: "valkey@7.2"},
		RecipeTarget{Hostname: "worker", Type: "php-nginx@8.4", IsWorker: true},
		RecipeTarget{Hostname: "storage", Type: "object-storage"},
		RecipeTarget{Hostname: "mailpit", Type: "mailpit"},
		RecipeTarget{Hostname: "search", Type: "meilisearch@1"},
	)
	return plan
}

// testDualRuntimePlan returns a showcase plan with dual-runtime architecture:
// a Svelte frontend (static) + NestJS API backend + shared-codebase worker.
func testDualRuntimePlan() *RecipePlan {
	return &RecipePlan{
		Framework:   "nestjs",
		Tier:        RecipeTierShowcase,
		Slug:        "nestjs-showcase",
		RuntimeType: "nodejs@22",
		Research: ResearchData{
			ServiceType:    "nodejs",
			PackageManager: "npm",
			HTTPPort:       3000,
			BuildCommands:  []string{"npm ci", "npm run build"},
			DeployFiles:    []string{"dist"},
			StartCommand:   "node dist/main.js",
			CacheLib:       "ioredis",
			SessionDriver:  "redis",
			QueueDriver:    "bullmq",
			StorageDriver:  "s3",
			SearchLib:      "meilisearch",
			MailLib:        "nodemailer",
		},
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "static", Role: "app"},
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true},
			{Hostname: "db", Type: "postgresql@17"},
			{Hostname: "cache", Type: "valkey@7.2"},
			{Hostname: "storage", Type: "object-storage"},
			{Hostname: "search", Type: "meilisearch@1"},
		},
	}
}

func TestGenerateRecipeREADME_Minimal(t *testing.T) {
	t.Parallel()

	readme := GenerateRecipeREADME(testMinimalPlan())

	tests := []struct {
		name   string
		want   string
		negate bool
	}{
		{"framework name", "Laravel", false},
		{"pretty name from slug", "Minimal", false},
		{"no Hello World for minimal", "Hello World", true},
		{"intro extract markers", "ZEROPS_EXTRACT_START:intro", false},
		{"deploy links", "deploy with one click", false},
		{"deploy button", "deploy-button.svg", false},
		{"cover image", "cover-laravel.svg", false},
		{"opencode mention", "opencode.ai", false},
		{"more recipes line", "Laravel recipes", false},
		{"discord link", "discord.gg/zeropsio", false},
	}
	for _, tt := range tests {
		if tt.negate {
			if strings.Contains(readme, tt.want) {
				t.Errorf("%s: should NOT contain %q", tt.name, tt.want)
			}
		} else {
			if !strings.Contains(readme, tt.want) {
				t.Errorf("%s: expected to contain %q", tt.name, tt.want)
			}
		}
	}
	// Should list all 6 environments with links.
	for _, env := range envTiers {
		if !strings.Contains(readme, env.Label) {
			t.Errorf("expected README to list environment %q", env.Label)
		}
	}
}

func TestGenerateRecipeREADME_Showcase(t *testing.T) {
	t.Parallel()

	readme := GenerateRecipeREADME(testShowcasePlan())

	if !strings.Contains(readme, "Showcase") {
		t.Error("expected README to mention showcase")
	}
	// Showcase intro must list ALL services, not just the DB.
	for _, want := range []string{"MariaDB", "Valkey", "Meilisearch", "object storage"} {
		if !strings.Contains(readme, want) {
			t.Errorf("showcase intro missing service %q", want)
		}
	}
}

func TestRecipeIntroServiceList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		plan     *RecipePlan
		wantSubs []string
		wantNot  []string
	}{
		{
			name:     "minimal DB only",
			plan:     testMinimalPlan(),
			wantSubs: []string{"connected to", "MariaDB"},
			wantNot:  []string{"Valkey", "and"},
		},
		{
			name:     "showcase lists all services",
			plan:     testShowcasePlan(),
			wantSubs: []string{"connected to", "MariaDB", "Valkey", "Meilisearch", "object storage"},
		},
		{
			name: "no DB no services",
			plan: &RecipePlan{
				Research: ResearchData{DBDriver: recipeDBNone},
				Targets:  []RecipeTarget{{Hostname: "app", Type: "nodejs@22"}},
			},
			wantSubs: []string{""}, // empty string = no service list
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := recipeIntroServiceList(tt.plan)
			for _, want := range tt.wantSubs {
				if !strings.Contains(got, want) {
					t.Errorf("expected %q in %q", want, got)
				}
			}
			for _, notWant := range tt.wantNot {
				if strings.Contains(got, notWant) {
					t.Errorf("unexpected %q in %q", notWant, got)
				}
			}
		})
	}
}

func TestGenerateRecipeREADME_HelloWorld(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	plan.Slug = "bun-hello-world"
	plan.Framework = "bun"
	readme := GenerateRecipeREADME(plan)

	if !strings.Contains(readme, "# Bun Hello World Recipe") {
		t.Error("expected 'Bun Hello World Recipe' title derived from slug")
	}
}

func TestGenerateEnvImportYAML_AllEnvs(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()

	for i := 0; i < EnvTierCount(); i++ {
		t.Run(fmt.Sprintf("env_%d", i), func(t *testing.T) {
			t.Parallel()
			yaml := GenerateEnvImportYAML(plan, i)

			if yaml == "" {
				t.Fatal("expected non-empty YAML")
			}
			if !strings.Contains(yaml, "project:") {
				t.Error("expected project: key")
			}
			if !strings.Contains(yaml, "services:") {
				t.Error("expected services: key")
			}

			// Project name should contain slug + suffix.
			suffix := envTiers[i].Suffix
			expectedName := plan.Slug + "-" + suffix
			if !strings.Contains(yaml, expectedName) {
				t.Errorf("expected project name %q in YAML", expectedName)
			}

			// Shared secret at project level via envVariables.
			if plan.Research.NeedsAppSecret {
				if !strings.Contains(yaml, "zeropsPreprocessor=on") {
					t.Error("expected zeropsPreprocessor=on for generateRandomString")
				}
				// Preprocessor directive MUST be on the very first line — Zerops
				// parser rejects it anywhere else.
				if !strings.HasPrefix(yaml, "#zeropsPreprocessor=on\n") {
					t.Errorf("zeropsPreprocessor=on must be the first line, got: %q", strings.SplitN(yaml, "\n", 2)[0])
				}
				if !strings.Contains(yaml, "<@generateRandomString(<32>)>") {
					t.Error("expected <@generateRandomString(<32>)> with inner angle brackets")
				}
				// Must be under project.envVariables, not per-service envSecrets.
				if strings.Contains(yaml, "envSecrets") {
					t.Error("shared secret should be project-level envVariables, not per-service envSecrets")
				}
				if !strings.Contains(yaml, "  envVariables:") {
					t.Error("expected project-level envVariables")
				}
			}

			// Data service priority.
			if strings.Contains(yaml, "hostname: db") {
				if !strings.Contains(yaml, "priority: 10") {
					t.Error("expected priority: 10 for data service")
				}
			}
		})
	}
}

func TestGenerateEnvImportYAML_SharedSecretProjectLevel(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	yaml := GenerateEnvImportYAML(plan, 0)

	// Shared secret must be at project level (envVariables), not per-service envSecrets.
	if strings.Contains(yaml, "envSecrets") {
		t.Error("should not have per-service envSecrets — shared secret belongs at project level")
	}
	if !strings.Contains(yaml, "  envVariables:") {
		t.Error("expected project-level envVariables for shared secret")
	}
}

func TestGenerateEnvImportYAML_Env5_HA(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	yaml := GenerateEnvImportYAML(plan, 5)

	if !strings.Contains(yaml, "mode: HA") {
		t.Error("expected HA mode for env 5 data services")
	}
	if !strings.Contains(yaml, "cpuMode: DEDICATED") {
		t.Error("expected DEDICATED cpuMode for env 5")
	}

	// corePackage must be at project level, NOT under verticalAutoscaling.
	if !strings.Contains(yaml, "corePackage: SERIOUS") {
		t.Error("expected SERIOUS corePackage for env 5")
	}
	// Verify it's at project level (indented 2 spaces under project:).
	for line := range strings.SplitSeq(yaml, "\n") {
		if strings.Contains(line, "corePackage: SERIOUS") {
			indent := len(line) - len(strings.TrimLeft(line, " "))
			if indent > 4 {
				t.Errorf("corePackage should be at project level (indent<=4), got indent=%d", indent)
			}
		}
	}
}

func TestGenerateEnvImportYAML_Env4_MinContainers(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	yaml := GenerateEnvImportYAML(plan, 4)

	if !strings.Contains(yaml, "minContainers: 2") {
		t.Error("expected minContainers: 2 for env 4 app services")
	}
}

func TestGenerateEnvImportYAML_Env01_DevStageServices(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()

	for _, envIdx := range []int{0, 1} {
		t.Run(fmt.Sprintf("env_%d", envIdx), func(t *testing.T) {
			t.Parallel()
			yaml := GenerateEnvImportYAML(plan, envIdx)

			// Dev service: appdev with zeropsSetup: dev + buildFromGit.
			if !strings.Contains(yaml, "hostname: appdev") {
				t.Error("expected appdev hostname")
			}
			if !strings.Contains(yaml, "zeropsSetup: dev") {
				t.Error("expected zeropsSetup: dev on dev service")
			}
			expectedDevRepo := testRepoBase + plan.Slug + "-app"
			if !strings.Contains(yaml, "buildFromGit: "+expectedDevRepo) {
				t.Errorf("expected buildFromGit on dev service")
			}
			// Recipe deliverable must NOT have startWithoutCode.
			if strings.Contains(yaml, "startWithoutCode") {
				t.Error("recipe deliverable must not have startWithoutCode (that's workspace only)")
			}

			// Stage service: appstage with zeropsSetup: prod, buildFromGit.
			if !strings.Contains(yaml, "hostname: appstage") {
				t.Error("expected appstage hostname for stage service")
			}
			if !strings.Contains(yaml, "zeropsSetup: prod") {
				t.Error("expected zeropsSetup: prod on stage service")
			}
			expectedRepo := testRepoBase + plan.Slug + "-app"
			if !strings.Contains(yaml, "buildFromGit: "+expectedRepo) {
				t.Errorf("expected buildFromGit: %s", expectedRepo)
			}
		})
	}
}

func TestGenerateEnvImportYAML_Env2Plus_ProdService(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()

	for envIdx := 2; envIdx < EnvTierCount(); envIdx++ {
		t.Run(fmt.Sprintf("env_%d", envIdx), func(t *testing.T) {
			t.Parallel()
			yaml := GenerateEnvImportYAML(plan, envIdx)

			// Bare hostname — no appdev/appstage.
			if strings.Contains(yaml, "hostname: appdev") {
				t.Error("env 2+ should use bare hostname, not appdev")
			}
			if !strings.Contains(yaml, "hostname: app") {
				t.Error("expected bare hostname: app")
			}

			// zeropsSetup: prod (maps bare hostname to setup: prod in zerops.yaml).
			if !strings.Contains(yaml, "zeropsSetup: prod") {
				t.Error("expected zeropsSetup: prod on app service")
			}
			expectedRepo := testRepoBase + plan.Slug + "-app"
			if !strings.Contains(yaml, "buildFromGit: "+expectedRepo) {
				t.Errorf("expected buildFromGit: %s", expectedRepo)
			}

			// Must NOT have startWithoutCode.
			if strings.Contains(yaml, "startWithoutCode") {
				t.Error("env 2+ should not have startWithoutCode")
			}
		})
	}
}

// TestGenerateEnvImportYAML_SharedCodebaseWorker verifies that when a worker uses
// the same runtime as the app (one app, two processes — e.g. web + queue:work),
// env 0-1 get workerstage ONLY. No workerdev — appdev runs both processes via SSH.
// This applies to ALL recipe tiers (showcase, minimal, etc.).
func TestGenerateEnvImportYAML_SharedCodebaseWorker(t *testing.T) {
	t.Parallel()

	tiers := []struct {
		name string
		tier string
		slug string
	}{
		{"showcase", RecipeTierShowcase, "laravel-showcase"},
		{"minimal", RecipeTierMinimal, "laravel-minimal"},
	}

	for _, tier := range tiers {
		t.Run(tier.name+"_env_0_no_workerdev", func(t *testing.T) {
			t.Parallel()
			plan := testShowcasePlan()
			plan.Tier = tier.tier
			plan.Slug = tier.slug
			yaml := GenerateEnvImportYAML(plan, 0)

			// Shared codebase: NO workerdev — appdev is the shared workspace.
			if strings.Contains(yaml, "hostname: workerdev") {
				t.Error("shared-codebase worker must NOT have workerdev — appdev runs both processes")
			}
			if !strings.Contains(yaml, "hostname: workerstage") {
				t.Error("expected workerstage hostname")
			}
			// workerstage uses zeropsSetup: worker (shared zerops.yaml's worker setup).
			workerstageBlock := extractServiceBlock(yaml, "workerstage")
			if !strings.Contains(workerstageBlock, "zeropsSetup: worker") {
				t.Error("expected zeropsSetup: worker on workerstage")
			}
			// Workers must NOT have enableSubdomainAccess.
			if strings.Contains(workerstageBlock, "enableSubdomainAccess") {
				t.Error("workerstage must NOT have enableSubdomainAccess")
			}
			// Shared codebase: same app repo.
			if !strings.Contains(workerstageBlock, tier.slug+"-app") {
				t.Error("shared-codebase workerstage should use app repo URL")
			}
		})

		t.Run(tier.name+"_env_2_worker", func(t *testing.T) {
			t.Parallel()
			plan := testShowcasePlan()
			plan.Tier = tier.tier
			plan.Slug = tier.slug
			yaml := GenerateEnvImportYAML(plan, 2)

			if !strings.Contains(yaml, "hostname: worker") {
				t.Error("expected bare worker hostname in env 2")
			}
			// Shared-codebase worker in env 2+: zeropsSetup: worker (shared yaml).
			if !strings.Contains(yaml, "zeropsSetup: worker") {
				t.Error("expected zeropsSetup: worker")
			}
			if !strings.Contains(yaml, tier.slug+"-app") {
				t.Error("shared-codebase worker should use app repo URL")
			}
		})
	}
}

// TestGenerateEnvImportYAML_SeparateCodebaseWorker verifies that when a worker uses
// a different runtime than the app (separate codebase — e.g. bun app + python worker),
// it gets its own dev+stage pair in env 0-1 because it needs its own container and mount.
func TestGenerateEnvImportYAML_SeparateCodebaseWorker(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlan()
	// Override worker to different type = separate codebase.
	for i, t := range plan.Targets {
		if t.IsWorker {
			plan.Targets[i].Type = "python@3.12"
		}
	}

	t.Run("env_0_has_workerdev", func(t *testing.T) {
		t.Parallel()
		yaml := GenerateEnvImportYAML(plan, 0)

		// Separate codebase: worker gets its own dev+stage pair.
		if !strings.Contains(yaml, "hostname: workerdev") {
			t.Error("separate-codebase worker should have workerdev")
		}
		if !strings.Contains(yaml, "hostname: workerstage") {
			t.Error("separate-codebase worker should have workerstage")
		}
		// Separate-codebase worker stage uses zeropsSetup: prod (its own zerops.yaml).
		if !strings.Contains(yaml, "zeropsSetup: prod") {
			t.Error("expected zeropsSetup: prod on separate-codebase workerstage")
		}
	})

	t.Run("env_2_worker", func(t *testing.T) {
		t.Parallel()
		yaml := GenerateEnvImportYAML(plan, 2)

		if !strings.Contains(yaml, "hostname: worker") {
			t.Error("expected bare worker hostname in env 2")
		}
		// Separate-codebase worker: zeropsSetup: prod (own zerops.yaml).
		lines := strings.Split(yaml, "\n")
		inWorker := false
		for _, line := range lines {
			if strings.Contains(line, "hostname: worker") && !strings.Contains(line, "hostname: workerstage") {
				inWorker = true
			} else if strings.Contains(line, "hostname:") {
				inWorker = false
			}
			if inWorker && strings.Contains(line, "zeropsSetup: prod") {
				// Good — separate-codebase worker uses prod setup from its own zerops.yaml.
				return
			}
		}
		t.Error("expected zeropsSetup: prod for separate-codebase worker in env 2")
	})

	t.Run("env_2_separate_codebase_buildFromGit", func(t *testing.T) {
		t.Parallel()
		yaml := GenerateEnvImportYAML(plan, 2)

		// Separate-codebase worker should use {slug}-worker repo.
		if !strings.Contains(yaml, "laravel-showcase-worker") {
			t.Error("separate-codebase worker should use -worker repo URL")
		}
	})
}

func TestGenerateEnvImportYAML_Showcase_ObjectStorage(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlan()

	for _, envIdx := range []int{0, 2, 5} {
		t.Run(fmt.Sprintf("env_%d", envIdx), func(t *testing.T) {
			t.Parallel()
			yaml := GenerateEnvImportYAML(plan, envIdx)

			if !strings.Contains(yaml, "hostname: storage") {
				t.Error("expected storage hostname")
			}
			if !strings.Contains(yaml, "objectStorageSize:") {
				t.Error("expected objectStorageSize for object-storage")
			}
			if !strings.Contains(yaml, "objectStoragePolicy:") {
				t.Error("expected objectStoragePolicy for object-storage")
			}
			// Object storage must NOT have mode or verticalAutoscaling.
			lines := strings.Split(yaml, "\n")
			inStorage := false
			for _, line := range lines {
				if strings.Contains(line, "hostname: storage") {
					inStorage = true
				} else if strings.TrimSpace(line) != "" && strings.HasPrefix(strings.TrimSpace(line), "- hostname:") {
					inStorage = false
				}
				if inStorage {
					if strings.Contains(line, "mode:") {
						t.Error("object-storage must NOT have mode field")
					}
					if strings.Contains(line, "verticalAutoscaling:") {
						t.Error("object-storage must NOT have verticalAutoscaling")
					}
				}
			}
		})
	}
}

func TestGenerateEnvImportYAML_Showcase_UtilityService(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlan()

	t.Run("env_0_mailpit", func(t *testing.T) {
		t.Parallel()
		yaml := GenerateEnvImportYAML(plan, 0)

		// Mailpit is a single service (no dev/stage pair).
		if !strings.Contains(yaml, "hostname: mailpit") {
			t.Error("expected mailpit hostname")
		}
		if strings.Contains(yaml, "hostname: mailpitdev") {
			t.Error("mailpit should NOT have dev/stage pair")
		}
		// Should have buildFromGit from utility repo.
		if !strings.Contains(yaml, "buildFromGit: "+testRepoBase+"mailpit-app") {
			t.Error("expected buildFromGit pointing to mailpit-app utility repo")
		}
		// Should have enableSubdomainAccess (web UI).
		if !strings.Contains(yaml, "enableSubdomainAccess: true") {
			t.Error("expected enableSubdomainAccess for mailpit web UI")
		}
	})

	t.Run("env_5_mailpit_no_dedicated", func(t *testing.T) {
		t.Parallel()
		yaml := GenerateEnvImportYAML(plan, 5)

		// Mailpit in env 5 should NOT have cpuMode: DEDICATED (it's a utility).
		lines := strings.Split(yaml, "\n")
		inMailpit := false
		for _, line := range lines {
			if strings.Contains(line, "hostname: mailpit") {
				inMailpit = true
			} else if strings.TrimSpace(line) != "" && strings.HasPrefix(strings.TrimSpace(line), "- hostname:") {
				inMailpit = false
			}
			if inMailpit && strings.Contains(line, "cpuMode: DEDICATED") {
				t.Error("utility service (mailpit) should NOT have cpuMode: DEDICATED")
			}
		}
	})
}

func TestGenerateEnvImportYAML_NoPlatformKnowledgeComments(t *testing.T) {
	t.Parallel()

	// Template MUST NOT emit platform-knowledge comments (naming specific
	// managed service types or their characteristics). Default structural
	// comments are allowed for uncommented runtime services (they describe
	// the service role from its properties, not platform knowledge).
	plan := testShowcasePlan()
	for i := 0; i < EnvTierCount(); i++ {
		yaml := GenerateEnvImportYAML(plan, i)
		for _, forbidden := range []string{"Valkey single-node", "PostgreSQL single-node", "Meilisearch single-node"} {
			if strings.Contains(yaml, forbidden) {
				t.Errorf("env %d: template emitted platform prose %q — should be agent-owned", i, forbidden)
			}
		}
	}
}

func TestGenerateEnvImportYAML_DefaultCommentsForUncommentedServices(t *testing.T) {
	t.Parallel()

	// When the agent omits comments for a dev/stage service, the template
	// generates a structural default so the import.yaml is never bare.
	plan := testShowcasePlan() // no EnvComments
	yaml := GenerateEnvImportYAML(plan, 0)

	// Shared-codebase worker: no workerdev, only workerstage in env 0.
	if strings.Contains(yaml, "hostname: workerdev") {
		t.Error("shared-codebase worker must NOT have workerdev in env 0")
	}
	if !strings.Contains(yaml, "# Stage worker") {
		t.Error("expected default comment for uncommented workerstage in env 0")
	}
}

func TestGenerateEnvImportYAML_AgentCommentsOverrideDefaults(t *testing.T) {
	t.Parallel()

	// When the agent provides a comment, the default is NOT emitted.
	plan := testShowcasePlan()
	plan.EnvComments = map[string]EnvComments{
		"0": {
			Service: map[string]string{
				"appdev":      "Agent-written appdev comment.",
				"workerstage": "Agent-written workerstage comment.",
			},
		},
	}
	yaml := GenerateEnvImportYAML(plan, 0)

	if strings.Contains(yaml, "Dev workspace — zeropsSetup:dev") {
		t.Error("default comment should not appear when agent provides one")
	}
	if !strings.Contains(yaml, "Agent-written appdev comment") {
		t.Error("expected agent comment for appdev")
	}
	if !strings.Contains(yaml, "Agent-written workerstage comment") {
		t.Error("expected agent comment for workerstage")
	}
}

func TestGenerateEnvREADME_FixedContent(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()

	for i := 0; i < EnvTierCount(); i++ {
		t.Run(fmt.Sprintf("env_%d", i), func(t *testing.T) {
			t.Parallel()
			readme := GenerateEnvREADME(plan, i)

			if readme == "" {
				t.Fatal("expected non-empty README")
			}
			if !strings.Contains(readme, "Laravel") {
				t.Error("expected framework name in README")
			}
			if !strings.Contains(readme, "Minimal") {
				t.Error("expected pretty name 'Minimal' from slug")
			}
			if strings.Contains(readme, "Hello World") {
				t.Error("should NOT contain 'Hello World' for minimal tier")
			}
			if !strings.Contains(readme, "ZEROPS_EXTRACT_START:intro") {
				t.Error("expected intro extract markers")
			}
			if !strings.Contains(readme, "ZEROPS_EXTRACT_END:intro") {
				t.Error("expected intro extract end marker")
			}
			if !strings.Contains(readme, "app.zerops.io/recipes") {
				t.Error("expected deploy link")
			}

			// Env type label must appear in intro paragraph.
			env := envTiers[i]
			lowerLabel := strings.ToLower(env.Label)
			if !strings.Contains(readme, lowerLabel+" environment for") {
				t.Errorf("expected '%s environment for' in intro paragraph", lowerLabel)
			}

			// Extract intro should use sentence-cased IntroLabel.
			if !strings.Contains(readme, "**"+env.IntroLabel+"**") {
				t.Errorf("expected sentence-cased label **%s** in extract", env.IntroLabel)
			}
		})
	}
}

func TestGenerateEnvREADME_DynamicDescription(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()

	// Env 0-1 should include "dev service" and "database" from targets.
	for _, envIdx := range []int{0, 1} {
		readme := GenerateEnvREADME(plan, envIdx)
		if !strings.Contains(readme, "dev service") {
			t.Errorf("env %d: expected 'dev service' in description", envIdx)
		}
		if !strings.Contains(readme, "staging service") {
			t.Errorf("env %d: expected 'staging service' in description", envIdx)
		}
		if !strings.Contains(readme, "database") {
			t.Errorf("env %d: expected 'database' in description", envIdx)
		}
	}
}

func TestRecipePrettyName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		slug      string
		framework string
		want      string
	}{
		{"laravel-minimal", "laravel", "Minimal"},
		{"bun-hello-world", "bun", "Hello World"},
		{"django-showcase", "django", "Showcase"},
		{"nextjs-hello-world", "nextjs", "Hello World"},
		{"react-static-starter", "react", "Static Starter"},
	}
	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			t.Parallel()
			got := recipePrettyName(tt.slug, tt.framework)
			if got != tt.want {
				t.Errorf("recipePrettyName(%q, %q) = %q, want %q", tt.slug, tt.framework, got, tt.want)
			}
		})
	}
}

func TestNaturalJoin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		parts []string
		want  string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a and b"},
		{[]string{"a", "b", "c"}, "a, b, and c"},
	}
	for _, tt := range tests {
		got := naturalJoin(tt.parts)
		if got != tt.want {
			t.Errorf("naturalJoin(%v) = %q, want %q", tt.parts, got, tt.want)
		}
	}
}

func TestBuildFinalizeOutput_FileCount(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	files := BuildFinalizeOutput(plan)

	// 1 main README + 6 * (import.yaml + README.md) + 1 app README = 14 files.
	expectedCount := 1 + 6*2 + 1
	if len(files) != expectedCount {
		t.Errorf("expected %d files, got %d", expectedCount, len(files))
	}

	// Check main README exists.
	if _, ok := files["README.md"]; !ok {
		t.Error("missing main README.md")
	}
	// Check app README scaffold exists.
	if _, ok := files["appdev/README.md"]; !ok {
		t.Error("missing appdev/README.md")
	}

	// Check all env folders.
	for i := 0; i < EnvTierCount(); i++ {
		folder := EnvFolder(i)
		if _, ok := files[folder+"/import.yaml"]; !ok {
			t.Errorf("missing %s/import.yaml", folder)
		}
		if _, ok := files[folder+"/README.md"]; !ok {
			t.Errorf("missing %s/README.md", folder)
		}
	}
}

func TestGenerateEnvImportYAML_AgentCommentsRendered_Env01(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	plan.EnvComments = map[string]EnvComments{
		"0": {
			Service: map[string]string{
				"appdev":   "agent-written appdev comment for env 0",
				"appstage": "agent-written appstage comment for env 0",
				"db":       "agent-written db comment for env 0",
			},
			Project: "agent-written project comment for env 0",
		},
		"1": {
			Service: map[string]string{
				"appdev":   "agent-written appdev comment for env 1",
				"appstage": "agent-written appstage comment for env 1",
				"db":       "agent-written db comment for env 1",
			},
		},
	}

	// Env 0 carries all four agent comments.
	yaml0 := GenerateEnvImportYAML(plan, 0)
	for _, want := range []string{
		"agent-written appdev comment for env 0",
		"agent-written appstage comment for env 0",
		"agent-written db comment for env 0",
		"agent-written project comment for env 0",
	} {
		if !strings.Contains(yaml0, want) {
			t.Errorf("env 0: expected %q in output", want)
		}
	}
	// And env 0 MUST NOT leak env 1's comments (per-env isolation).
	for _, forbidden := range []string{
		"comment for env 1",
	} {
		if strings.Contains(yaml0, forbidden) {
			t.Errorf("env 0: leaked env 1 content %q", forbidden)
		}
	}

	// Env 1 carries its own comments and no project comment (not provided).
	yaml1 := GenerateEnvImportYAML(plan, 1)
	for _, want := range []string{
		"agent-written appdev comment for env 1",
		"agent-written appstage comment for env 1",
		"agent-written db comment for env 1",
	} {
		if !strings.Contains(yaml1, want) {
			t.Errorf("env 1: expected %q in output", want)
		}
	}
	if strings.Contains(yaml1, "project comment for env") {
		t.Error("env 1: rendered a project comment when none was provided")
	}
}

func TestGenerateEnvImportYAML_AgentCommentsRendered_Env2Plus(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	plan.EnvComments = map[string]EnvComments{
		"2": {
			Service: map[string]string{
				"app": "local-env app comment",
				"db":  "local-env db comment",
			},
			Project: "local-env project comment",
		},
		"4": {
			Service: map[string]string{
				"app": "small-prod app comment",
				"db":  "small-prod db comment",
			},
			Project: "small-prod project comment",
		},
		"5": {
			Service: map[string]string{
				"app": "HA-prod app comment — explains DEDICATED CPU",
				"db":  "HA-prod db comment — explains HA replication",
			},
			Project: "HA-prod project comment — explains corePackage SERIOUS",
		},
	}

	cases := []struct {
		env    int
		wants  []string
		avoids []string
	}{
		{
			env: 2,
			wants: []string{
				"local-env app comment", "local-env db comment", "local-env project comment",
			},
			avoids: []string{"small-prod", "HA-prod"},
		},
		{
			env: 4,
			wants: []string{
				"small-prod app comment", "small-prod db comment", "small-prod project comment",
			},
			avoids: []string{"local-env", "HA-prod"},
		},
		{
			env: 5,
			wants: []string{
				"HA-prod app comment", "HA-prod db comment", "HA-prod project comment",
				"DEDICATED CPU", "HA replication", "corePackage SERIOUS",
			},
			avoids: []string{"local-env", "small-prod"},
		},
	}
	for _, tc := range cases {
		yaml := GenerateEnvImportYAML(plan, tc.env)
		for _, want := range tc.wants {
			if !strings.Contains(yaml, want) {
				t.Errorf("env %d: expected %q in output", tc.env, want)
			}
		}
		for _, avoid := range tc.avoids {
			if strings.Contains(yaml, avoid) {
				t.Errorf("env %d: leaked cross-env content %q", tc.env, avoid)
			}
		}
	}
}

func TestGenerateEnvImportYAML_AgentComments_HostnameKeyMustMatchEnvShape(t *testing.T) {
	t.Parallel()

	// In env 0-1 the runtime pair takes keys "appdev" + "appstage".
	// If the agent provides only the base "app" key, nothing renders for
	// the runtime pair — this asserts the strict key semantics so agents
	// discover the mistake via the comment-ratio check.
	plan := testMinimalPlan()
	plan.EnvComments = map[string]EnvComments{
		"0": {Service: map[string]string{"app": "wrong-key comment"}},
	}
	yaml0 := GenerateEnvImportYAML(plan, 0)
	if strings.Contains(yaml0, "wrong-key comment") {
		t.Error("env 0: base-hostname key 'app' should NOT match appdev/appstage entries")
	}
}

func TestGenerateEnvImportYAML_AgentComments_CommentRatioReaches30Percent(t *testing.T) {
	t.Parallel()

	// Realistic-sized per-env comments (2 sentences per service + project)
	// must push the comment ratio past the 30% finalize threshold for every
	// env. This proves the check is achievable with the new per-env API.
	longComment := "This is a realistic-length comment explaining the service's role in this environment. It covers what zeropsSetup does, why the scaling fields have their values, and what the developer should understand about this specific tier."

	plan := testMinimalPlan()
	plan.EnvComments = map[string]EnvComments{}
	for i := 0; i < EnvTierCount(); i++ {
		svc := map[string]string{"db": longComment}
		if i <= 1 {
			svc["appdev"] = longComment
			svc["appstage"] = longComment
		} else {
			svc["app"] = longComment
		}
		plan.EnvComments[fmt.Sprintf("%d", i)] = EnvComments{
			Service: svc,
			Project: longComment,
		}
	}

	for i := 0; i < EnvTierCount(); i++ {
		yaml := GenerateEnvImportYAML(plan, i)
		var commentLines, totalLines int
		for line := range strings.SplitSeq(yaml, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || trimmed == "#zeropsPreprocessor=on" {
				continue
			}
			totalLines++
			if strings.HasPrefix(trimmed, "#") {
				commentLines++
			}
		}
		ratio := float64(commentLines) / float64(totalLines)
		if ratio < 0.30 {
			t.Errorf("env %d: ratio %.0f%% below 30%% threshold with realistic agent comments — API can't reach the bar", i, ratio*100)
		}
	}
}

func TestGenerateEnvImportYAML_StructuralFields(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()

	// Env 0: buildFromGit repo, NON_HA + priority field values.
	yaml0 := GenerateEnvImportYAML(plan, 0)
	for _, want := range []string{
		"zerops-recipe-apps", // buildFromGit org in URL
		"mode: NON_HA",       // field value
		"priority: 10",       // field value
	} {
		if !strings.Contains(yaml0, want) {
			t.Errorf("env 0: expected %q in output", want)
		}
	}

	// Env 5: HA + DEDICATED + SERIOUS field values.
	yaml5 := GenerateEnvImportYAML(plan, 5)
	for _, want := range []string{
		"corePackage: SERIOUS",
		"mode: HA",
		"cpuMode: DEDICATED",
	} {
		if !strings.Contains(yaml5, want) {
			t.Errorf("env 5: expected %q in output", want)
		}
	}
}

func TestGenerateEnvImportYAML_BaselineBelowCommentThreshold(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()

	// With no agent comments, the only comments the template emits are the
	// env-header paragraph at the top of each file. The resulting comment
	// ratio MUST stay below the 30% finalize threshold so the checker still
	// forces the agent to contribute per-env service + project prose.
	for i := 0; i < EnvTierCount(); i++ {
		yaml := GenerateEnvImportYAML(plan, i)
		var commentLines, totalLines int
		for line := range strings.SplitSeq(yaml, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || trimmed == "#zeropsPreprocessor=on" {
				continue
			}
			totalLines++
			if strings.HasPrefix(trimmed, "#") {
				commentLines++
			}
		}
		if totalLines == 0 {
			t.Fatalf("env %d: no content", i)
		}
		ratio := float64(commentLines) / float64(totalLines)
		if ratio >= 0.30 {
			t.Errorf("env %d: template baseline ratio %.0f%% meets 30%% threshold — agent won't be forced to add comments",
				i, ratio*100)
		}
	}
}

func TestGenerateEnvImportYAML_ProjectNameSuffixes(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()

	wantSuffixes := map[int]string{
		0: "agent", 1: "remote", 2: "local",
		3: "stage", 4: "small-prod", 5: "ha-prod",
	}
	for envIdx, suffix := range wantSuffixes {
		yaml := GenerateEnvImportYAML(plan, envIdx)
		expected := plan.Slug + "-" + suffix
		if !strings.Contains(yaml, expected) {
			t.Errorf("env %d: expected project name %q", envIdx, expected)
		}
	}
}

func TestGenerateAppREADME(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	readme := GenerateAppREADME(plan)

	checks := []struct {
		name string
		want string
	}{
		{"title", "# Laravel Minimal Recipe App"},
		{"intro extract", "ZEROPS_EXTRACT_START:intro"},
		{"integration-guide extract", "ZEROPS_EXTRACT_START:integration-guide"},
		{"knowledge-base extract", "ZEROPS_EXTRACT_START:knowledge-base"},
		{"deploy button", "deploy-button.svg"},
		{"cover image", "cover-laravel.svg"},
		{"recipe link", "app.zerops.io/recipes/laravel-minimal"},
		{"H2 outside marker", "## Integration Guide"},
		{"step 1 heading", "### 1. Adding `zerops.yaml`"},
	}
	for _, tt := range checks {
		if !strings.Contains(readme, tt.want) {
			t.Errorf("%s: expected to contain %q", tt.name, tt.want)
		}
	}
}

func TestEnvFolder_AllTiers(t *testing.T) {
	t.Parallel()

	for i := 0; i < EnvTierCount(); i++ {
		folder := EnvFolder(i)
		if folder == "" {
			t.Errorf("env %d returned empty folder", i)
		}
		// Verify em-dash character.
		if !strings.Contains(folder, "\u2014") {
			t.Errorf("env %d folder %q missing em-dash", i, folder)
		}
	}
}

func TestEnvFolder_OutOfRange(t *testing.T) {
	t.Parallel()

	if EnvFolder(-1) != "" {
		t.Error("expected empty for negative index")
	}
	if EnvFolder(6) != "" {
		t.Error("expected empty for out-of-range index")
	}
}

func TestIsRuntimeType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		svcType string
		want    bool
	}{
		{"php-nginx@8.4", true},
		{"nodejs@22", true},
		{"go@1", true},
		{"bun@1.2", true},
		{"python@3.12", true},
		{"nginx@1.22", true},
		{"static", true},
		{"rust@stable", true},
		{"docker@26.1", true},
		{"ubuntu@24.04", true},
		// managed and utility are NOT runtime
		{"postgresql@17", false},
		{"mariadb@10.6", false},
		{"valkey@7.2", false},
		{"meilisearch@1.20", false},
		{"object-storage", false},
		{"shared-storage", false},
		{"nats@2.12", false},
		{"kafka@3.9", false},
		{"mailpit", false},
	}

	for _, tt := range tests {
		t.Run(tt.svcType, func(t *testing.T) {
			t.Parallel()
			got := IsRuntimeType(tt.svcType)
			if got != tt.want {
				t.Errorf("IsRuntimeType(%q) = %v, want %v", tt.svcType, got, tt.want)
			}
		})
	}
}

func TestServiceTypeCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		serviceType   string
		supportsMode  bool
		supportsScale bool
		isObjStorage  bool
		isUtility     bool
	}{
		{"runtime", "php-nginx@8.4", false, true, false, false},
		{"postgresql", "postgresql@16", true, true, false, false},
		{"valkey", "valkey@7.2", true, true, false, false},
		{"meilisearch", "meilisearch@1", true, true, false, false},
		{"object_storage", "object-storage", false, false, true, false},
		{"shared_storage", "shared-storage", true, false, false, false},
		{"mailpit", "mailpit", false, true, false, true},
		{"nodejs", "nodejs@22", false, true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ServiceSupportsMode(tt.serviceType); got != tt.supportsMode {
				t.Errorf("ServiceSupportsMode(%q) = %v, want %v", tt.serviceType, got, tt.supportsMode)
			}
			if got := ServiceSupportsAutoscaling(tt.serviceType); got != tt.supportsScale {
				t.Errorf("ServiceSupportsAutoscaling(%q) = %v, want %v", tt.serviceType, got, tt.supportsScale)
			}
			if got := IsObjectStorageType(tt.serviceType); got != tt.isObjStorage {
				t.Errorf("IsObjectStorageType(%q) = %v, want %v", tt.serviceType, got, tt.isObjStorage)
			}
			if got := IsUtilityType(tt.serviceType); got != tt.isUtility {
				t.Errorf("IsUtilityType(%q) = %v, want %v", tt.serviceType, got, tt.isUtility)
			}
		})
	}
}

func TestRecipeSetupName(t *testing.T) {
	t.Parallel()

	appTarget := RecipeTarget{Hostname: "app", Type: "php-nginx@8.4"}
	monoWorker := RecipeTarget{Hostname: "worker", Type: "php-nginx@8.4", IsWorker: true}
	polyWorker := RecipeTarget{Hostname: "worker", Type: "python@3.12", IsWorker: true}
	plan := &RecipePlan{RuntimeType: "php-nginx@8.4"}

	tests := []struct {
		name   string
		target RecipeTarget
		isDev  bool
		want   string
	}{
		{"app_dev", appTarget, true, "dev"},
		{"app_prod", appTarget, false, "prod"},
		{"shared_codebase_worker_dev", monoWorker, true, "dev"},
		{"shared_codebase_worker_stage", monoWorker, false, "worker"},
		{"separate_codebase_worker_dev", polyWorker, true, "dev"},
		{"separate_codebase_worker_stage", polyWorker, false, "prod"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := recipeSetupName(tt.target, tt.isDev, plan)
			if got != tt.want {
				t.Errorf("recipeSetupName(%s, isDev=%v) = %q, want %q", tt.target.Hostname, tt.isDev, got, tt.want)
			}
		})
	}
}

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
		{"shared_worker_dualruntime", dualPlan, RecipeTarget{Hostname: "worker", Type: "nodejs@22", IsWorker: true}, "nestjs-showcase-api"},
		{"no_role_app", dualPlan, RecipeTarget{Hostname: "app", Type: "nodejs@22"}, "nestjs-showcase-app"},
		{"shared_worker_singleapp", singlePlan, RecipeTarget{Hostname: "worker", Type: "php-nginx@8.4", IsWorker: true}, "laravel-showcase-app"},
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
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true},
			{Hostname: "db", Type: "postgresql@17"},
			{Hostname: "cache", Type: "valkey@7.2"},
			{Hostname: "storage", Type: "object-storage"},
			{Hostname: "search", Type: "meilisearch@1"},
		},
	}

	errs := ValidateRecipePlan(plan, nil, nil)
	if len(errs) > 0 {
		t.Errorf("dual-runtime showcase should be valid, got errors: %v", errs)
	}
}
