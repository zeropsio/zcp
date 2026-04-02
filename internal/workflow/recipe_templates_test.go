package workflow

import (
	"fmt"
	"strings"
	"testing"
)

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

			// App secret check.
			if plan.Research.NeedsAppSecret {
				if !strings.Contains(yaml, "envSecrets") {
					t.Error("expected envSecrets for NeedsAppSecret=true")
				}
				if !strings.Contains(yaml, "zeropsPreprocessor=on") {
					t.Error("expected zeropsPreprocessor=on for generateRandomString")
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
	if !strings.Contains(yaml, "corePackage: SERIOUS") {
		t.Error("expected SERIOUS corePackage for env 5")
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

func TestGenerateEnvImportYAML_Env01_DevHostname(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()
	yaml0 := GenerateEnvImportYAML(plan, 0)
	yaml1 := GenerateEnvImportYAML(plan, 1)

	if !strings.Contains(yaml0, "hostname: appdev") {
		t.Error("expected appdev hostname for env 0")
	}
	if !strings.Contains(yaml1, "hostname: appdev") {
		t.Error("expected appdev hostname for env 1")
	}

	// Env 2+ should use bare hostname.
	yaml2 := GenerateEnvImportYAML(plan, 2)
	if strings.Contains(yaml2, "hostname: appdev") {
		t.Error("env 2 should use bare hostname, not appdev")
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
