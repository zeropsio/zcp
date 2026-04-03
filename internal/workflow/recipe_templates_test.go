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
		Slug:        "laravel-hello-world",
		RuntimeType: "php-nginx@8.4",
		Research: ResearchData{
			ServiceType:    "php-nginx",
			PackageManager: "composer",
			HTTPPort:       80,
			BuildCommands:  []string{"composer install"},
			DeployFiles:    []string{"."},
			StartCommand:   "php artisan serve",
			NeedsAppSecret: true,
			DBDriver:       "mysql",
			MigrationCmd:   "php artisan migrate",
		},
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "php-nginx@8.4", Role: "app", Environments: []string{"0", "1", "2", "3", "4", "5"}},
			{Hostname: "db", Type: "mariadb@10.11", Role: "db", Environments: []string{"0", "1", "2", "3", "4", "5"}},
		},
	}
}

func testShowcasePlan() *RecipePlan {
	plan := testMinimalPlan()
	plan.Tier = RecipeTierShowcase
	plan.Slug = "laravel-showcase"
	plan.Targets = append(plan.Targets,
		RecipeTarget{Hostname: "redis", Type: "keydb@6", Role: "cache", Environments: []string{"0", "1", "3", "4", "5"}},
		RecipeTarget{Hostname: "worker", Type: "php-nginx@8.4", Role: "worker", Environments: []string{"0", "1", "3", "4", "5"}},
	)
	return plan
}

func TestGenerateRecipeREADME_Minimal(t *testing.T) {
	t.Parallel()

	readme := GenerateRecipeREADME(testMinimalPlan())

	if !strings.Contains(readme, "Laravel") {
		t.Error("expected README to contain framework name")
	}
	if !strings.Contains(readme, "minimal") {
		t.Error("expected README to mention minimal")
	}
	if !strings.Contains(readme, "## Environments") {
		t.Error("expected README to have Environments section")
	}
	// Should list all 6 environments.
	for i := range 6 {
		if !strings.Contains(readme, fmt.Sprintf("| %d |", i)) {
			t.Errorf("expected README to list environment %d", i)
		}
	}
}

func TestGenerateRecipeREADME_Showcase(t *testing.T) {
	t.Parallel()

	readme := GenerateRecipeREADME(testShowcasePlan())

	if !strings.Contains(readme, "Showcase") {
		t.Error("expected README to mention showcase")
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

			// App secret check — per-service, not project level.
			if plan.Research.NeedsAppSecret {
				if !strings.Contains(yaml, "envSecrets") {
					t.Error("expected envSecrets for NeedsAppSecret=true")
				}
				if !strings.Contains(yaml, "zeropsPreprocessor=on") {
					t.Error("expected zeropsPreprocessor=on for generateRandomString")
				}
				// Must use correct angle bracket syntax.
				if !strings.Contains(yaml, "<@generateRandomString(<32>)>") {
					t.Error("expected <@generateRandomString(<32>)> with inner angle brackets")
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

func TestGenerateEnvImportYAML_EnvSecretsPerService(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	yaml := GenerateEnvImportYAML(plan, 0)

	// envSecrets must NOT be at project level.
	lines := strings.Split(yaml, "\n") //nolint:stringsseq // need index access
	for i, line := range lines {
		if strings.TrimSpace(line) == "envSecrets:" {
			// Check indentation — project-level would be 2 spaces, service-level is 4+.
			indent := len(line) - len(strings.TrimLeft(line, " "))
			if indent <= 2 && i > 0 && strings.Contains(lines[i-1], "project:") || strings.Contains(lines[max(0, i-2)], "project:") {
				t.Error("envSecrets must be per-service, not at project level")
			}
		}
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

			// Dev service: appdev with startWithoutCode, maxContainers, zeropsSetup: dev.
			if !strings.Contains(yaml, "hostname: appdev") {
				t.Error("expected appdev hostname")
			}
			if !strings.Contains(yaml, "startWithoutCode: true") {
				t.Error("expected startWithoutCode: true on dev service")
			}
			if !strings.Contains(yaml, "maxContainers: 1") {
				t.Error("expected maxContainers: 1 on dev service")
			}
			if !strings.Contains(yaml, "zeropsSetup: dev") {
				t.Error("expected zeropsSetup: dev on dev service")
			}
			// Dev service must NOT have buildFromGit.
			// (startWithoutCode services don't build from git)

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

			// zeropsSetup: prod + buildFromGit.
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

func TestGenerateEnvImportYAML_Showcase_WorkerServices(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlan()
	yaml := GenerateEnvImportYAML(plan, 0)

	// Worker in env 0 should have zeropsSetup and buildFromGit.
	if !strings.Contains(yaml, "hostname: workerdev") {
		t.Error("expected workerdev hostname for worker in env 0")
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
			if !strings.Contains(readme, plan.Slug) {
				t.Error("expected slug in README")
			}
			if !strings.Contains(readme, "## Import") {
				t.Error("expected Import section")
			}
			if !strings.Contains(readme, "## Services") {
				t.Error("expected Services section")
			}
		})
	}
}

func TestBuildFinalizeOutput_FileCount(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	files := BuildFinalizeOutput(plan)

	// 1 main README + 6 * (import.yaml + README.md) = 13 files.
	expectedCount := 1 + 6*2
	if len(files) != expectedCount {
		t.Errorf("expected %d files, got %d", expectedCount, len(files))
	}

	// Check main README exists.
	if _, ok := files["README.md"]; !ok {
		t.Error("missing main README.md")
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

func TestTargetInEnv(t *testing.T) {
	t.Parallel()

	target := RecipeTarget{
		Hostname:     "app",
		Type:         "php@8.4",
		Role:         "app",
		Environments: []string{"0", "1", "2"},
	}

	tests := []struct {
		envIndex int
		want     bool
	}{
		{0, true}, {1, true}, {2, true},
		{3, false}, {4, false}, {5, false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("env_%d", tt.envIndex), func(t *testing.T) {
			t.Parallel()
			got := TargetInEnv(target, tt.envIndex)
			if got != tt.want {
				t.Errorf("TargetInEnv(env=%d) = %v, want %v", tt.envIndex, got, tt.want)
			}
		})
	}
}

func TestIsDataService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role string
		want bool
	}{
		{"app", false},
		{"worker", false},
		{"db", true},
		{"cache", true},
		{"storage", true},
		{"search", true},
		{"mail", true},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			t.Parallel()
			got := IsDataService(tt.role)
			if got != tt.want {
				t.Errorf("IsDataService(%q) = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}
