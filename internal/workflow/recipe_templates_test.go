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

func TestGenerateEnvImportYAML_PlatformComments(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()

	// Env 0: data service gets platform comments.
	yaml0 := GenerateEnvImportYAML(plan, 0)
	for _, want := range []string{
		"zerops-recipe-apps", // correct buildFromGit org
		"MariaDB",            // DB type name in comment
		"NON_HA",             // mode explanation
		"Priority 10",        // priority explanation
	} {
		if !strings.Contains(yaml0, want) {
			t.Errorf("env 0: expected %q in output", want)
		}
	}

	// Env 5: HA + DEDICATED + SERIOUS in platform comments.
	yaml5 := GenerateEnvImportYAML(plan, 5)
	for _, want := range []string{
		"SERIOUS",   // corePackage comment
		"HA",        // mode
		"DEDICATED", // cpuMode
	} {
		if !strings.Contains(yaml5, want) {
			t.Errorf("env 5: expected %q in output", want)
		}
	}
}

func TestGenerateEnvImportYAML_TemplateBaseline_BelowCommentThreshold(t *testing.T) {
	t.Parallel()

	plan := testMinimalPlan()

	// Template generates platform comments only — baseline is BELOW the 30%
	// finalize threshold. This is intentional: the agent MUST add framework
	// comments to pass the checker. Verify the template isn't accidentally
	// meeting the threshold on its own.
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
		// Platform comments should provide ~10-25% baseline, NOT >= 30%.
		if ratio >= 0.30 {
			t.Errorf("env %d: template ratio %.0f%% meets 30%% threshold — agent won't be forced to add comments",
				i, ratio*100)
		}
		// But should have SOME platform comments (not zero).
		if commentLines == 0 {
			t.Errorf("env %d: expected at least some platform comments", i)
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
