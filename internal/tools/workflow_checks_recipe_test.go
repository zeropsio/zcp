package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/workflow"
)

// Wrappers around schema functions for test use (tools_test can't access schema internals).
func parseZeropsYmlSchemaForTest(data []byte) (*schema.ZeropsYmlSchema, error) {
	return schema.ParseZeropsYmlSchema(data)
}

func extractValidFieldsForTest(s *schema.ZeropsYmlSchema) *schema.ValidFields {
	return schema.ExtractValidFields(s)
}

func testRecipePlan() *workflow.RecipePlan {
	return &workflow.RecipePlan{
		Framework:   "laravel",
		Tier:        workflow.RecipeTierMinimal,
		Slug:        "laravel-hello-world",
		RuntimeType: "php-nginx@8.4",
		Research: workflow.ResearchData{
			ServiceType:    "php-nginx",
			PackageManager: "composer",
			HTTPPort:       80,
			BuildCommands:  []string{"composer install"},
			DeployFiles:    []string{"."},
			StartCommand:   "php artisan serve",
		},
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "php-nginx@8.4", Environments: []string{"0", "1", "2", "3", "4", "5"}},
		},
	}
}

func testRecipeState() *workflow.RecipeState {
	return &workflow.RecipeState{
		Active:      true,
		CurrentStep: 2, // generate
	}
}

const validREADME = `# Laravel Hello World

<!-- #ZEROPS_EXTRACT_START:intro# -->

A minimal Laravel application demonstrating Zerops deployment.

<!-- #ZEROPS_EXTRACT_END:intro# -->

## Integration Guide

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->

### zerops.yaml

` + "```yaml" + `
# Laravel zerops.yaml configuration
# Base setup shared between environments
zerops:
  # Service configuration
  - setup: app
    # Build phase configuration
    build:
      # Use PHP 8.4 with Node for Vite
      base: php-nginx@8.4
      # Install all PHP dependencies
      buildCommands:
        - composer install
        # Build frontend assets
        - npm run build
      # Deploy the entire project
      deployFiles:
        - .
    # Runtime configuration
    run:
      # Start the application
      start: php artisan serve --host=0.0.0.0 --port=80
      # Configure ports
      ports:
        - port: 80
          httpSupport: true
` + "```" + `

<!-- #ZEROPS_EXTRACT_END:integration-guide# -->

## Knowledge Base

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Getting Started
Laravel on Zerops uses PHP-FPM behind nginx.

### Gotchas
- Always set APP_KEY via envSecrets, not environment variables
- Use stderr logging driver, not file-based logging

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`

// TestCheckCommentSpecificity exercises the v13 comment-specificity floor,
// the companion to the existing comment_ratio check. comment_ratio says
// "comments are PRESENT"; specificity says "comments are LOAD-BEARING".
// Generic lines like "npm ci for reproducible builds" pass ratio but
// read as boilerplate that teaches nothing Zerops-specific.
func TestCheckCommentSpecificity(t *testing.T) {
	t.Parallel()
	showcase := &workflow.RecipePlan{Tier: workflow.RecipeTierShowcase}
	minimal := &workflow.RecipePlan{Tier: workflow.RecipeTierMinimal}

	// Boilerplate comments — clear the ratio check, fail specificity.
	boilerplate := `zerops:
  # npm ci for reproducible builds
  - setup: prod
    # cache node_modules between builds
    build:
      # Node 22 base
      base: nodejs@22
      # install dependencies
      buildCommands:
        - npm ci
`
	// Real v12 API-style comments — specific, load-bearing.
	loadBearing := `zerops:
  - setup: prod
    build:
      # Node 22 provides native fetch and stable ESM support.
      base: nodejs@22
      buildCommands:
        # Deterministic install — respects package-lock.json exactly,
        # preventing phantom dependency drift between builds.
        - npm ci
    run:
      initCommands:
        # execOnce guarantees exactly-one execution across horizontal
        # containers — migrations acquire advisory locks and must not race.
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/migrate.js
      envVariables:
        # Credentials injected by Zerops at runtime from the managed
        # PostgreSQL service — never hardcode connection strings.
        DB_HOST: ${db_hostname}
`

	tests := []struct {
		name       string
		yaml       string
		plan       *workflow.RecipePlan
		wantLen    int
		wantStatus string
	}{
		{"boilerplate fails specificity", boilerplate, showcase, 1, "fail"},
		{"load-bearing passes", loadBearing, showcase, 1, "pass"},
		{"minimal tier skipped", boilerplate, minimal, 0, ""},
		{"nil plan skipped", boilerplate, nil, 0, ""},
		{"no comments skipped", "zerops:\n  - setup: prod\n", showcase, 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := checkCommentSpecificity(tt.yaml, tt.plan)
			if len(got) != tt.wantLen {
				t.Fatalf("checks len = %d, want %d: %+v", len(got), tt.wantLen, got)
			}
			if tt.wantLen == 0 {
				return
			}
			if got[0].Name != "comment_specificity" {
				t.Errorf("check name = %q", got[0].Name)
			}
			if got[0].Status != tt.wantStatus {
				t.Errorf("status = %q, want %q; detail: %s", got[0].Status, tt.wantStatus, got[0].Detail)
			}
		})
	}
}

// TestCheckIntegrationGuideCodeBlocks verifies the v13 integration-guide
// code-block floor: showcase READMEs must contain at least one non-YAML
// code block showing an actual application-code change a user makes to
// their own code (trust proxy, bind 0.0.0.0, allowedHosts, etc.). The v12
// audit found most READMEs were 95% zerops.yaml comments with only one
// real code-adjustment section; this check forces every showcase
// integration guide to document at least one concrete code change.
func TestCheckIntegrationGuideCodeBlocks(t *testing.T) {
	t.Parallel()
	showcase := &workflow.RecipePlan{Tier: workflow.RecipeTierShowcase}
	minimal := &workflow.RecipePlan{Tier: workflow.RecipeTierMinimal}

	yamlOnly := "<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n\n" +
		"### zerops.yaml\n\n```yaml\nzerops:\n  - setup: prod\n```\n" +
		"\n<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n"
	withTypescript := "<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n\n" +
		"### zerops.yaml\n\n```yaml\nzerops:\n  - setup: prod\n```\n\n" +
		"### Trust Proxy\n\n```typescript\napp.set('trust proxy', true);\n```\n" +
		"\n<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n"
	withBash := "<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n\n" +
		"### zerops.yaml\n\n```yaml\nzerops:\n  - setup: prod\n```\n\n" +
		"### First deploy\n\n```bash\nnpm install && npm run build\n```\n" +
		"\n<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n"

	tests := []struct {
		name       string
		content    string
		plan       *workflow.RecipePlan
		wantLen    int
		wantStatus string
	}{
		{"showcase yaml-only fails", yamlOnly, showcase, 1, "fail"},
		{"showcase with typescript passes", withTypescript, showcase, 1, "pass"},
		{"showcase with bash passes", withBash, showcase, 1, "pass"},
		{"minimal tier skipped (no check)", yamlOnly, minimal, 0, ""},
		{"nil plan skipped", yamlOnly, nil, 0, ""},
		{"no integration guide fragment skipped", "no fragment here", showcase, 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := checkIntegrationGuideCodeBlocks(t.Context(), tt.content, tt.plan)
			if len(got) != tt.wantLen {
				t.Fatalf("checks len = %d, want %d: %+v", len(got), tt.wantLen, got)
			}
			if tt.wantLen == 0 {
				return
			}
			if got[0].Name != "integration_guide_code_adjustment" {
				t.Errorf("check name = %q", got[0].Name)
			}
			if got[0].Status != tt.wantStatus {
				t.Errorf("status = %q, want %q; detail: %s", got[0].Status, tt.wantStatus, got[0].Detail)
			}
		})
	}
}

// TestCheckIntegrationGuidePerItemCodeBlock verifies the v18 per-item
// code-block floor: every H3 heading inside the integration-guide
// fragment must carry at least one fenced code block in its section.
// Catches the v18 appdev regression where IG step 3 ("Place VITE_API_URL
// in build.envVariables for prod, run.envVariables for dev") was
// prose-only while v7 had a code block.
//
// The existing `integration_guide_code_adjustment` check only enforces
// ≥1 non-YAML block in the whole IG fragment — passes even if half the
// H3 items are prose-only. This per-item floor makes the bar: recipe.md
// already tells the agent to write one IG item per real code adjustment
// with the diff. The check now enforces it.
func TestCheckIntegrationGuidePerItemCodeBlock(t *testing.T) {
	t.Parallel()
	showcase := &workflow.RecipePlan{Tier: workflow.RecipeTierShowcase}
	minimal := &workflow.RecipePlan{Tier: workflow.RecipeTierMinimal}

	// v18 appdev-style: H3 #3 is prose-only. Should fail.
	v18AppdevRegression := "<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n\n" +
		"### 1. Adding `zerops.yaml`\n\n```yaml\nzerops:\n  - setup: prod\n```\n\n" +
		"### 2. Add `.zerops.app` to Vite's `allowedHosts`\n\n" +
		"```typescript\nexport default defineConfig({ server: { allowedHosts: ['.zerops.app'] } });\n```\n\n" +
		"### 3. Place `VITE_API_URL` in `build.envVariables` for prod, `run.envVariables` for dev\n\n" +
		"Vite substitutes `import.meta.env.VITE_*` at build time but reads `process.env.VITE_*` at startup. " +
		"Placing the URL in the wrong section causes `undefined` in the browser.\n\n" +
		"<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n"

	// v7 apidev-style: every H3 carries a code block. Should pass.
	v7AllItemsCoded := "<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n\n" +
		"### 1. Adding `zerops.yaml`\n\n```yaml\nzerops:\n  - setup: prod\n```\n\n" +
		"### 2. Trust proxy and bind 0.0.0.0\n\n" +
		"```typescript\napp.set('trust proxy', true); await app.listen(3000, '0.0.0.0');\n```\n\n" +
		"### 3. Enable CORS for the SPA\n\n" +
		"```typescript\napp.enableCors({ origin: process.env.FRONTEND_URL });\n```\n\n" +
		"### 4. TypeORM via env vars\n\n" +
		"```typescript\nhost: process.env.DB_HOST, port: parseInt(process.env.DB_PORT ?? '5432', 10)\n```\n\n" +
		"<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n"

	// Single H3 with yaml only — legacy minimal recipe shape. Should
	// pass (only one item, nothing to enforce per-item).
	singleH3 := "<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n\n" +
		"### 1. Adding `zerops.yaml`\n\n```yaml\nzerops:\n  - setup: prod\n```\n\n" +
		"<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n"

	// Two H3s, second has a bash code block — passes (any language).
	twoH3sBash := "<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n\n" +
		"### 1. Adding `zerops.yaml`\n\n```yaml\nzerops:\n  - setup: prod\n```\n\n" +
		"### 2. First deploy\n\n```bash\nzcli push\n```\n\n" +
		"<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n"

	tests := []struct {
		name       string
		content    string
		plan       *workflow.RecipePlan
		wantStatus string // "fail", "pass", or "" for skipped
		wantItem   string // substring that must appear in the fail detail
	}{
		{"v18 appdev regression fails", v18AppdevRegression, showcase, "fail", "VITE_API_URL"},
		{"v7 all-items-coded passes", v7AllItemsCoded, showcase, "pass", ""},
		{"single H3 passes", singleH3, showcase, "pass", ""},
		{"two H3s second has bash passes", twoH3sBash, showcase, "pass", ""},
		{"minimal tier skipped", v18AppdevRegression, minimal, "", ""},
		{"nil plan skipped", v18AppdevRegression, nil, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := checkIntegrationGuidePerItemCodeBlock(tt.content, tt.plan)
			if tt.wantStatus == "" {
				if len(got) != 0 {
					t.Fatalf("expected no checks (skipped), got: %+v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("checks len = %d, want 1: %+v", len(got), got)
			}
			if got[0].Name != "integration_guide_per_item_code" {
				t.Errorf("check name = %q, want integration_guide_per_item_code", got[0].Name)
			}
			if got[0].Status != tt.wantStatus {
				t.Errorf("status = %q, want %q; detail: %s", got[0].Status, tt.wantStatus, got[0].Detail)
			}
			if tt.wantItem != "" && !strings.Contains(got[0].Detail, tt.wantItem) {
				t.Errorf("detail %q missing expected substring %q", got[0].Detail, tt.wantItem)
			}
		})
	}
}

func TestCheckRecipeGenerate_ValidMinimal(t *testing.T) {
	t.Parallel()

	// Set up fixture directory.
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create app mount dir with zerops.yaml and README.
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), `zerops:
  - setup: dev
    build:
      base: php-nginx@8.4
      buildCommands:
        - composer install
      deployFiles:
        - .
    run:
      envVariables:
        APP_ENV: local
        APP_DEBUG: "true"
      ports:
        - port: 80
          httpSupport: true
  - setup: prod
    build:
      base: php-nginx@8.4
      buildCommands:
        - composer install --no-dev --optimize-autoloader
      deployFiles:
        - app
        - vendor
    run:
      envVariables:
        APP_ENV: production
        APP_DEBUG: "false"
      ports:
        - port: 80
          httpSupport: true
`)
	// v14: README must NOT exist at generate-complete time. Writing
	// READMEs is the post-deploy `readmes` sub-step's job so the
	// gotchas section can narrate real debug experience. The
	// `no_premature_readme` check fails this fixture if README exists.
	// See TestCheckRecipeDeployReadmes_ValidFragments below for the
	// equivalent test of the README content check at the deploy step.

	checker := checkRecipeGenerate(stateDir, nil, nil)
	result, err := checker(context.Background(), testRecipePlan(), testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Passed {
		t.Errorf("expected all checks to pass, got failures:")
		for _, c := range result.Checks {
			if c.Status == "fail" {
				t.Errorf("  %s: %s", c.Name, c.Detail)
			}
		}
	}
}

// TestCheckRecipeGenerate_PrematureReadmeFails locks in the v14 rule:
// README.md must not exist on the mount when generate completes. The
// `no_premature_readme` check fails when a README is present because
// generate-time READMEs forced agents to speculate about gotchas before
// any debugging had happened, producing the synthetic-narrative failures
// the authenticity floor kept catching.
func TestCheckRecipeGenerate_PrematureReadmeFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), `zerops:
  - setup: dev
    build:
      base: php-nginx@8.4
      buildCommands:
        - composer install
      deployFiles:
        - .
    run:
      envVariables:
        APP_ENV: local
      ports:
        - port: 80
          httpSupport: true
  - setup: prod
    build:
      base: php-nginx@8.4
      buildCommands:
        - composer install --no-dev
      deployFiles:
        - app
        - vendor
    run:
      envVariables:
        APP_ENV: production
      ports:
        - port: 80
          httpSupport: true
`)
	writeFile(t, filepath.Join(appDir, "README.md"), validREADME)

	checker := checkRecipeGenerate(stateDir, nil, nil)
	result, err := checker(context.Background(), testRecipePlan(), testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Passed {
		t.Errorf("expected no_premature_readme to fail — README written at generate time")
	}
	found := false
	for _, c := range result.Checks {
		if c.Name == "app_no_premature_readme" && c.Status == "fail" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected app_no_premature_readme check to fail, checks: %+v", result.Checks)
	}
}

func TestCheckRecipeGenerate_MissingProdSetup(t *testing.T) {
	t.Parallel()

	// Agent wrote dev only — the consolidated generate step now requires BOTH
	// dev and prod so the file matches the README integration-guide fragment.
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), `zerops:
  - setup: dev
    build:
      base: php-nginx@8.4
      deployFiles: [.]
    run:
      ports:
        - port: 80
`)
	writeFile(t, filepath.Join(appDir, "README.md"), validREADME)

	checker := checkRecipeGenerate(stateDir, nil, nil)
	result, err := checker(context.Background(), testRecipePlan(), testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected checks to fail when prod setup missing")
	}
	var sawProdFail bool
	for _, c := range result.Checks {
		if c.Name == "app_prod_setup" && c.Status == "fail" {
			sawProdFail = true
		}
	}
	if !sawProdFail {
		t.Error("expected app_prod_setup fail check")
	}
}

func TestCheckRecipeGenerate_DevProdBitIdentical(t *testing.T) {
	t.Parallel()

	// Both setups present but run.envVariables are byte-equal — the dev
	// container would behave exactly like prod during iteration.
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), `zerops:
  - setup: dev
    build:
      base: php-nginx@8.4
      deployFiles: [.]
    run:
      envVariables:
        APP_ENV: production
        APP_DEBUG: "false"
      ports:
        - port: 80
  - setup: prod
    build:
      base: php-nginx@8.4
      deployFiles:
        - app
    run:
      envVariables:
        APP_ENV: production
        APP_DEBUG: "false"
      ports:
        - port: 80
`)
	writeFile(t, filepath.Join(appDir, "README.md"), validREADME)

	checker := checkRecipeGenerate(stateDir, nil, nil)
	result, err := checker(context.Background(), testRecipePlan(), testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected checks to fail on identical dev/prod env maps")
	}
	var sawDivergenceFail bool
	for _, c := range result.Checks {
		if c.Name == "dev_prod_env_divergence" && c.Status == "fail" {
			sawDivergenceFail = true
		}
	}
	if !sawDivergenceFail {
		t.Error("expected dev_prod_env_divergence fail check")
	}
}

func TestCheckRecipeDeployReadmes_MissingFragments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), `zerops:
  - setup: app
    build:
      base: php-nginx@8.4
      deployFiles: [.]
    run:
      start: php artisan serve
      ports:
        - port: 80
`)
	// README without any fragments — v14 runs content checks at the
	// deploy step, not generate.
	writeFile(t, filepath.Join(appDir, "README.md"), "# App\nJust a basic readme.")

	checker := checkRecipeDeployReadmes(stateDir, nil, nil)
	result, err := checker(context.Background(), testRecipePlan(), testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected checks to fail when fragments are missing")
	}

	// Verify specific failures and that the new error message shows
	// the literal marker format (the v14 fix that burned a run where
	// the agent invented `<!-- FRAGMENT:intro:start -->`).
	failedNames := make(map[string]bool)
	var igDetail string
	for _, c := range result.Checks {
		if c.Status == "fail" {
			failedNames[c.Name] = true
			if c.Name == "fragment_integration-guide" {
				igDetail = c.Detail
			}
		}
	}
	for _, name := range []string{"fragment_integration-guide", "fragment_knowledge-base", "fragment_intro"} {
		if !failedNames[name] {
			t.Errorf("expected %q to fail", name)
		}
	}
	if !strings.Contains(igDetail, "#ZEROPS_EXTRACT_START:integration-guide#") {
		t.Errorf("fragment error detail should show the literal marker format, got: %q", igDetail)
	}
}

func TestCheckRecipeDeployReadmes_LowCommentRatio(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), `zerops:
  - setup: app
    build:
      base: php-nginx@8.4
      deployFiles: [.]
    run:
      start: php artisan serve
      ports:
        - port: 80
`)

	// README with fragments but very low comment ratio in YAML.
	readme := `# App
<!-- #ZEROPS_EXTRACT_START:intro# -->
A Laravel app.
<!-- #ZEROPS_EXTRACT_END:intro# -->
<!-- #ZEROPS_EXTRACT_START:integration-guide# -->
` + "```yaml" + `
zerops:
  - setup: app
    build:
      base: php-nginx@8.4
      buildCommands:
        - composer install
        - npm run build
      deployFiles:
        - .
    run:
      start: php artisan serve
      ports:
        - port: 80
          httpSupport: true
` + "```" + `
<!-- #ZEROPS_EXTRACT_END:integration-guide# -->
<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas
- Use stderr logging
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	writeFile(t, filepath.Join(appDir, "README.md"), readme)

	checker := checkRecipeDeployReadmes(stateDir, nil, nil)
	result, err := checker(context.Background(), testRecipePlan(), testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find comment_ratio check.
	for _, c := range result.Checks {
		if c.Name == "comment_ratio" {
			if c.Status != "fail" {
				t.Errorf("expected comment_ratio to fail (no comments in yaml), got %q", c.Status)
			}
			return
		}
	}
	t.Error("comment_ratio check not found")
}

func TestCheckRecipeDeployReadmes_PlaceholderText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), `zerops:
  - setup: app
    build:
      base: php-nginx@8.4
      deployFiles: [.]
    run:
      start: php artisan serve
      ports:
        - port: 80
`)

	readme := `# App
<!-- #ZEROPS_EXTRACT_START:intro# -->
A test app.
<!-- #ZEROPS_EXTRACT_END:intro# -->
<!-- #ZEROPS_EXTRACT_START:integration-guide# -->
Use PLACEHOLDER_DB_HOST for your database. TODO fix this.
` + "```yaml" + `
# Config
zerops:
  # Setup
  - setup: app
    # Build
    build:
      # Base runtime
      base: php-nginx@8.4
` + "```" + `
<!-- #ZEROPS_EXTRACT_END:integration-guide# -->
<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas
- Don't use <your-api-key> directly
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	writeFile(t, filepath.Join(appDir, "README.md"), readme)

	checker := checkRecipeDeployReadmes(stateDir, nil, nil)
	result, err := checker(context.Background(), testRecipePlan(), testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, c := range result.Checks {
		if c.Name == "no_placeholders" {
			if c.Status != "fail" {
				t.Errorf("expected no_placeholders to fail, got %q", c.Status)
			}
			return
		}
	}
	t.Error("no_placeholders check not found")
}

func TestCommentRatio(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		yaml    string
		wantMin float64
		wantMax float64
	}{
		{"all comments", "# comment1\n# comment2\n# comment3", 1.0, 1.0},
		{"no comments", "key: value\nother: value", 0, 0},
		{"50%", "# comment\nkey: value", 0.5, 0.5},
		{"empty", "", 0, 0},
		{"with blank lines", "# comment\n\nkey: value\n\n", 0.5, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := commentRatio(tt.yaml)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("commentRatio() = %f, want [%f, %f]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestExtractFragmentContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		frag    string
		want    string
	}{
		{
			"extracts content",
			"before\n<!-- #ZEROPS_EXTRACT_START:intro# -->\nHello world\n<!-- #ZEROPS_EXTRACT_END:intro# -->\nafter",
			"intro",
			"Hello world",
		},
		{
			"missing fragment",
			"no fragments here",
			"intro",
			"",
		},
		{
			"multiline content",
			"<!-- #ZEROPS_EXTRACT_START:guide# -->\nLine 1\nLine 2\nLine 3\n<!-- #ZEROPS_EXTRACT_END:guide# -->",
			"guide",
			"Line 1\nLine 2\nLine 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractFragmentContent(tt.content, tt.frag)
			if got != tt.want {
				t.Errorf("extractFragmentContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractYAMLBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			"extracts yaml",
			"text\n```yaml\nkey: value\n```\nmore",
			"key: value\n",
		},
		{
			"extracts yml",
			"```yml\nfoo: bar\n```",
			"foo: bar\n",
		},
		{
			"no yaml block",
			"just text",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractYAMLBlock(tt.content)
			if got != tt.want {
				t.Errorf("extractYAMLBlock() = %q, want %q", got, tt.want)
			}
		})
	}
}

func testShowcasePlan() *workflow.RecipePlan {
	return &workflow.RecipePlan{
		Framework:   "laravel",
		Tier:        workflow.RecipeTierShowcase,
		Slug:        "laravel-showcase",
		RuntimeType: "php-nginx@8.4",
		Research: workflow.ResearchData{
			ServiceType:    "php-nginx",
			PackageManager: "composer",
			HTTPPort:       80,
			BuildCommands:  []string{"composer install"},
			DeployFiles:    []string{"."},
			NeedsAppSecret: true,
		},
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "php-nginx@8.4"},
			// Shared-codebase worker — idiomatic for Laravel (Horizon).
			{Hostname: "worker", Type: "php-nginx@8.4", IsWorker: true, SharesCodebaseWith: "app"},
			{Hostname: "db", Type: "postgresql@18"},
			{Hostname: "redis", Type: "valkey@7.2"},
			// Dedicated NATS broker — required kindMessaging service.
			{Hostname: "queue", Type: "nats@2.12"},
			{Hostname: "storage", Type: "object-storage"},
			{Hostname: "search", Type: "meilisearch@1.20"},
		},
	}
}

func TestCheckRecipeGenerate_ShowcaseMissingWorkerSetup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Showcase zerops.yaml with dev+prod but NO worker setup.
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), `zerops:
  - setup: dev
    build:
      base: php-nginx@8.4
      buildCommands:
        - composer install
      deployFiles:
        - .
    run:
      envVariables:
        APP_ENV: local
        APP_DEBUG: "true"
      ports:
        - port: 80
          httpSupport: true
  - setup: prod
    build:
      base: php-nginx@8.4
      buildCommands:
        - composer install --no-dev --optimize-autoloader
      deployFiles:
        - app
        - vendor
    run:
      envVariables:
        APP_ENV: production
        APP_DEBUG: "false"
      ports:
        - port: 80
          httpSupport: true
`)
	writeFile(t, filepath.Join(appDir, "README.md"), validREADME)

	checker := checkRecipeGenerate(stateDir, nil, nil)
	result, err := checker(context.Background(), testShowcasePlan(), testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected checks to fail when showcase has no worker setup")
	}
	var sawWorkerFail bool
	for _, c := range result.Checks {
		if c.Name == "app_worker_setup" && c.Status == "fail" {
			sawWorkerFail = true
		}
	}
	if !sawWorkerFail {
		t.Error("expected app_worker_setup fail check")
	}
}

func TestCheckRecipeGenerate_ShowcaseWithWorkerSetup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Showcase zerops.yaml with all 3 setups: dev + prod + worker.
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), `zerops:
  - setup: dev
    build:
      base: php-nginx@8.4
      buildCommands:
        - composer install
      deployFiles:
        - .
    run:
      envVariables:
        APP_ENV: local
        APP_DEBUG: "true"
      ports:
        - port: 80
          httpSupport: true
  - setup: prod
    build:
      base: php-nginx@8.4
      buildCommands:
        - composer install --no-dev --optimize-autoloader
      deployFiles:
        - app
        - vendor
    run:
      envVariables:
        APP_ENV: production
        APP_DEBUG: "false"
      ports:
        - port: 80
          httpSupport: true
  - setup: worker
    build:
      base: php-nginx@8.4
      buildCommands:
        - composer install --no-dev --optimize-autoloader
      deployFiles:
        - app
        - vendor
    run:
      envVariables:
        APP_ENV: production
        APP_DEBUG: "false"
      start: php artisan queue:work
`)
	writeFile(t, filepath.Join(appDir, "README.md"), validREADME)

	checker := checkRecipeGenerate(stateDir, nil, nil)
	result, err := checker(context.Background(), testShowcasePlan(), testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sawWorkerPass bool
	for _, c := range result.Checks {
		if c.Name == "app_worker_setup" {
			if c.Status != "pass" {
				t.Errorf("expected app_worker_setup to pass, got %q: %s", c.Status, c.Detail)
			}
			sawWorkerPass = true
		}
	}
	if !sawWorkerPass {
		t.Error("expected app_worker_setup check to be present")
	}
}

func TestCheckRecipeGenerate_SchemaFieldValidation(t *testing.T) {
	t.Parallel()

	// Build valid fields from the test schema.
	schemaData, err := os.ReadFile("../../internal/schema/testdata/zerops_yml_schema.json")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	s, err := parseZeropsYmlSchemaForTest(schemaData)
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	vf := extractValidFieldsForTest(s)

	tests := []struct {
		name       string
		yaml       string
		wantStatus string
	}{
		{
			name: "valid zerops.yaml passes",
			yaml: `zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
        - npm install
      deployFiles: ./
    run:
      base: nodejs@22
      envVariables:
        NODE_ENV: development
      ports:
        - port: 3000
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - ./dist
    run:
      base: nodejs@22
      start: node dist/main.js
      envVariables:
        NODE_ENV: production
      ports:
        - port: 3000
`,
			wantStatus: statusPass,
		},
		{
			name: "verticalAutoscaling under run fails",
			yaml: `zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
        - npm install
      deployFiles: ./
    run:
      base: nodejs@22
      envVariables:
        NODE_ENV: development
      ports:
        - port: 3000
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
      deployFiles:
        - ./dist
    run:
      base: nodejs@22
      start: node dist/main.js
      envVariables:
        NODE_ENV: production
      ports:
        - port: 3000
      verticalAutoscaling:
        minRam: 0.25
`,
			wantStatus: statusFail,
		},
		{
			name: "unknown top-level field fails",
			yaml: `zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
        - npm install
      deployFiles: ./
    run:
      base: nodejs@22
      envVariables:
        NODE_ENV: development
      ports:
        - port: 3000
    scaling:
      min: 1
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
      deployFiles:
        - ./dist
    run:
      base: nodejs@22
      start: node dist/main.js
      envVariables:
        NODE_ENV: production
      ports:
        - port: 3000
`,
			wantStatus: statusFail,
		},
	}

	plan := &workflow.RecipePlan{
		Framework:   "nestjs",
		Tier:        workflow.RecipeTierMinimal,
		Slug:        "nestjs-minimal",
		RuntimeType: "nodejs@22",
		Research: workflow.ResearchData{
			ServiceType:    "nodejs",
			PackageManager: "npm",
			HTTPPort:       3000,
		},
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "nodejs@22"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			stateDir := filepath.Join(dir, ".zcp", "state")
			if err := os.MkdirAll(stateDir, 0o755); err != nil {
				t.Fatal(err)
			}
			appDir := filepath.Join(dir, "app")
			if err := os.MkdirAll(appDir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(appDir, "zerops.yaml"), tt.yaml)
			writeFile(t, filepath.Join(appDir, "README.md"), validREADME)

			checker := checkRecipeGenerate(stateDir, vf, nil)
			result, err := checker(context.Background(), plan, testRecipeState())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var found bool
			for _, c := range result.Checks {
				if c.Name == "zerops_yml_schema_fields" {
					found = true
					if c.Status != tt.wantStatus {
						t.Errorf("zerops_yml_schema_fields status = %q, want %q (detail: %s)", c.Status, tt.wantStatus, c.Detail)
					}
				}
			}
			if !found {
				t.Error("zerops_yml_schema_fields check not found in results")
			}
		})
	}
}

// frontendZeropsYaml is a static-runtime dev+prod zerops.yaml — the kind a
// dual-runtime frontend writes. It has NO worker setup, and must not be
// required to have one.
const frontendZeropsYaml = `zerops:
  - setup: dev
    build:
      base: nodejs@22
      envVariables:
        VITE_API_URL: https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app
      buildCommands:
        - npm install
        - npm run build
      deployFiles:
        - dist/~
    run:
      base: static
  - setup: prod
    build:
      base: nodejs@22
      envVariables:
        VITE_API_URL: https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - dist/~
    run:
      base: static
`

// workerZeropsYaml is a nodejs dev+prod zerops.yaml — the kind a separate-
// codebase worker (no shared host) writes. Used by the predecessor-floor
// integration test that exercises the per-worker README check loop.
const workerZeropsYaml = `zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
        - npm install
      deployFiles:
        - .
    run:
      envVariables:
        NODE_ENV: development
      start: node dist/worker.js
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - dist
    run:
      envVariables:
        NODE_ENV: production
      start: node dist/worker.js
`

// apiZeropsYamlWithWorker is a nodejs dev+prod+worker zerops.yaml — the kind a
// dual-runtime API (with shared-codebase BullMQ worker) writes.
const apiZeropsYamlWithWorker = `zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
        - npm install
      deployFiles:
        - .
    run:
      envVariables:
        NODE_ENV: development
      ports:
        - port: 3000
          httpSupport: true
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - dist
    run:
      envVariables:
        NODE_ENV: production
      ports:
        - port: 3000
          httpSupport: true
  - setup: worker
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - dist
    run:
      envVariables:
        NODE_ENV: production
      start: node dist/worker.js
`

func TestCheckRecipeGenerate_DualRuntime(t *testing.T) {
	t.Parallel()

	plan := &workflow.RecipePlan{
		Framework:   "nestjs",
		Tier:        workflow.RecipeTierShowcase,
		Slug:        "nestjs-showcase",
		RuntimeType: "nodejs@22",
		Research: workflow.ResearchData{
			ServiceType:    "nodejs",
			PackageManager: "npm",
			HTTPPort:       3000,
			BuildCommands:  []string{"npm ci"},
			DeployFiles:    []string{"dist"},
			StartCommand:   "node dist/main.js",
		},
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "static", Role: "app"},
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			// Shared-codebase worker: explicitly names the API as host.
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true, SharesCodebaseWith: "api"},
			{Hostname: "db", Type: "postgresql@17"},
		},
	}

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Each codebase gets the zerops.yaml it actually needs:
	// frontend has no worker setup, API has one.
	appDir := filepath.Join(dir, "appdev")
	apiDir := filepath.Join(dir, "apidev")
	for _, d := range []string{appDir, apiDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), frontendZeropsYaml)
	writeFile(t, filepath.Join(appDir, "README.md"), validREADME)
	writeFile(t, filepath.Join(apiDir, "zerops.yaml"), apiZeropsYamlWithWorker)
	writeFile(t, filepath.Join(apiDir, "README.md"), validREADME)

	checker := checkRecipeGenerate(stateDir, nil, nil)
	result, err := checker(context.Background(), plan, testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both zerops.yaml files exist and both pass — the frontend must NOT be
	// required to have a worker setup (regression guard for dual-runtime).
	byName := map[string]workflow.StepCheck{}
	for _, c := range result.Checks {
		byName[c.Name] = c
	}

	// app does NOT host the worker — no app_worker_setup check should exist.
	if _, exists := byName["app_worker_setup"]; exists {
		t.Error("frontend (static) must not be required to have setup: worker — worker shares API codebase")
	}

	// api hosts the worker — api_worker_setup must exist and pass.
	if c, exists := byName["api_worker_setup"]; !exists {
		t.Error("api_worker_setup check missing — API hosts the shared-codebase worker")
	} else if c.Status != statusPass {
		t.Errorf("api_worker_setup should pass, got %s: %s", c.Status, c.Detail)
	}

	// Both zerops.yaml existence checks must pass.
	for _, name := range []string{"app_zerops_yml_exists", "api_zerops_yml_exists"} {
		c, exists := byName[name]
		if !exists {
			t.Errorf("missing check %s", name)
			continue
		}
		if c.Status != statusPass {
			t.Errorf("%s should pass, got %s: %s", name, c.Status, c.Detail)
		}
	}
}

// TestCheckRecipeGenerate_SingleAppShowcase_WorkerRequired guards the
// backward-compatible path: single-app showcase (e.g. Laravel) where the
// worker shares the app's codebase — the app's zerops.yaml must have a worker
// setup.
func TestCheckRecipeGenerate_SingleAppShowcase_WorkerRequired(t *testing.T) {
	t.Parallel()

	plan := &workflow.RecipePlan{
		Framework:   "laravel",
		Tier:        workflow.RecipeTierShowcase,
		Slug:        "laravel-showcase",
		RuntimeType: "php-nginx@8.4",
		Research: workflow.ResearchData{
			ServiceType:    "php-nginx",
			PackageManager: "composer",
			HTTPPort:       80,
			BuildCommands:  []string{"composer install"},
			DeployFiles:    []string{"."},
			StartCommand:   "php artisan serve",
		},
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "php-nginx@8.4"},
			// Explicit shared-codebase declaration: worker shares app's codebase.
			{Hostname: "worker", Type: "php-nginx@8.4", IsWorker: true, SharesCodebaseWith: "app"},
			{Hostname: "db", Type: "mariadb@10.11"},
		},
	}

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// app's zerops.yaml has NO worker setup — the check must fail loudly so
	// the agent knows to add one. This is the inverse of the dual-runtime
	// case: here the app IS the worker's codebase host.
	appDir := filepath.Join(dir, "appdev")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), `zerops:
  - setup: dev
    build:
      base: php-nginx@8.4
      deployFiles: [.]
    run:
      envVariables:
        APP_ENV: local
      ports:
        - port: 80
  - setup: prod
    build:
      base: php-nginx@8.4
      deployFiles: [.]
    run:
      envVariables:
        APP_ENV: production
      ports:
        - port: 80
`)
	writeFile(t, filepath.Join(appDir, "README.md"), validREADME)

	checker := checkRecipeGenerate(stateDir, nil, nil)
	result, err := checker(context.Background(), plan, testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var workerCheck *workflow.StepCheck
	for i := range result.Checks {
		if result.Checks[i].Name == "app_worker_setup" {
			workerCheck = &result.Checks[i]
			break
		}
	}
	if workerCheck == nil {
		t.Fatal("expected app_worker_setup check — app hosts the shared-codebase worker")
	}
	if workerCheck.Status != statusFail {
		t.Errorf("app_worker_setup should fail when setup: worker is missing, got %s", workerCheck.Status)
	}
}

// writeFile is a test helper to write a file.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestCheckRecipeGenerate_EnvSelfShadow_SeparateWorker — v8.94. v28 shipped a
// workerdev zerops.yaml containing nine `key: ${key}` self-shadow lines
// (db_hostname, queue_user, ...) in run.envVariables. complete step=generate
// returned zero worker-prefixed checks — the recipe generate checker filtered
// worker targets out before checking anything. The lesson: separate-codebase
// workers have their OWN zerops.yaml and need the same env_self_shadow floor
// the app/api targets already get.
func TestCheckRecipeGenerate_EnvSelfShadow_SeparateWorker(t *testing.T) {
	t.Parallel()

	plan := &workflow.RecipePlan{
		Framework:   "nestjs",
		Tier:        workflow.RecipeTierShowcase,
		Slug:        "nestjs-showcase",
		RuntimeType: "nodejs@22",
		Research: workflow.ResearchData{
			ServiceType:    "nodejs",
			PackageManager: "npm",
			HTTPPort:       3000,
			BuildCommands:  []string{"npm ci"},
			DeployFiles:    []string{"dist"},
			StartCommand:   "node dist/main.js",
		},
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "static", Role: "app"},
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			// Separate-codebase worker — SharesCodebaseWith is empty.
			// Its own zerops.yaml lives at workerdev/zerops.yaml.
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true},
			{Hostname: "db", Type: "postgresql@17"},
			{Hostname: "queue", Type: "nats@2.12"},
		},
	}

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Three codebase mounts: appdev (static), apidev (clean), workerdev (self-shadow).
	for _, sub := range []string{"appdev", "apidev", "workerdev"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	writeFile(t, filepath.Join(dir, "appdev", "zerops.yaml"), frontendZeropsYaml)
	writeFile(t, filepath.Join(dir, "apidev", "zerops.yaml"), `zerops:
  - setup: dev
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      envVariables:
        NODE_ENV: development
      start: zsc noop --silent
      ports:
        - port: 3000
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
      deployFiles: [./dist]
    run:
      envVariables:
        NODE_ENV: production
      start: node dist/main.js
      ports:
        - port: 3000
`)
	// Workerdev ships nine self-shadow lines — exact v28 shape.
	writeFile(t, filepath.Join(dir, "workerdev", "zerops.yaml"), `zerops:
  - setup: dev
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      envVariables:
        NODE_ENV: development
        db_hostname: ${db_hostname}
        db_port: ${db_port}
        db_user: ${db_user}
        db_password: ${db_password}
        queue_hostname: ${queue_hostname}
        queue_port: ${queue_port}
        queue_user: ${queue_user}
        queue_password: ${queue_password}
      start: zsc noop --silent
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
      deployFiles: [./dist]
    run:
      envVariables:
        NODE_ENV: production
      start: node dist/worker.js
`)

	checker := checkRecipeGenerate(stateDir, nil, nil)
	result, err := checker(context.Background(), plan, testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	byName := map[string]workflow.StepCheck{}
	for _, c := range result.Checks {
		byName[c.Name] = c
	}

	// Worker must be enumerated — zerops_yml_exists check present.
	if _, ok := byName["worker_zerops_yml_exists"]; !ok {
		t.Error("worker_zerops_yml_exists check missing — separate-codebase worker was not enumerated")
	}

	// Worker self-shadow check MUST fire and MUST fail.
	workerShadow, ok := byName["worker_env_self_shadow"]
	if !ok {
		t.Fatal("worker_env_self_shadow check missing — self-shadow enumeration does not cover workers")
	}
	if workerShadow.Status != statusFail {
		t.Errorf("worker_env_self_shadow status = %q, want fail; detail: %s", workerShadow.Status, workerShadow.Detail)
	}
	// Detail should name the offending keys so the agent can locate them.
	if !strings.Contains(workerShadow.Detail, "db_hostname") {
		t.Errorf("worker_env_self_shadow detail should mention db_hostname: %s", workerShadow.Detail)
	}

	// Overall result must fail — self-shadow is a generate-blocking defect.
	if result.Passed {
		t.Error("result.Passed = true, want false — worker self-shadow should block generate")
	}
}

// TestCheckRecipeGenerate_EnvSelfShadow_CleanWorker — companion to the fail
// case. Same plan shape, workerdev zerops.yaml has NO self-shadow — the
// worker_env_self_shadow check must fire and pass. Regression guard against a
// fix that suppresses the check entirely (which would also look like "no
// failures" in the first test but silently regresses v8.94's coverage).
func TestCheckRecipeGenerate_EnvSelfShadow_CleanWorker(t *testing.T) {
	t.Parallel()

	plan := &workflow.RecipePlan{
		Framework:   "nestjs",
		Tier:        workflow.RecipeTierShowcase,
		Slug:        "nestjs-showcase",
		RuntimeType: "nodejs@22",
		Research: workflow.ResearchData{
			ServiceType: "nodejs", PackageManager: "npm", HTTPPort: 3000,
		},
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "static", Role: "app"},
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true},
			{Hostname: "db", Type: "postgresql@17"},
		},
	}

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"appdev", "apidev", "workerdev"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(dir, "appdev", "zerops.yaml"), frontendZeropsYaml)
	writeFile(t, filepath.Join(dir, "apidev", "zerops.yaml"), `zerops:
  - setup: dev
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      envVariables:
        NODE_ENV: development
      start: zsc noop --silent
      ports:
        - port: 3000
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
      deployFiles: [./dist]
    run:
      envVariables:
        NODE_ENV: production
      start: node dist/main.js
      ports:
        - port: 3000
`)
	// Worker yaml with properly renamed keys — no self-shadow.
	writeFile(t, filepath.Join(dir, "workerdev", "zerops.yaml"), `zerops:
  - setup: dev
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      envVariables:
        NODE_ENV: development
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        NATS_HOST: ${queue_hostname}
      start: zsc noop --silent
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
      deployFiles: [./dist]
    run:
      envVariables:
        NODE_ENV: production
        DB_HOST: ${db_hostname}
        NATS_HOST: ${queue_hostname}
      start: node dist/worker.js
`)

	checker := checkRecipeGenerate(stateDir, nil, nil)
	result, err := checker(context.Background(), plan, testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	byName := map[string]workflow.StepCheck{}
	for _, c := range result.Checks {
		byName[c.Name] = c
	}
	workerShadow, ok := byName["worker_env_self_shadow"]
	if !ok {
		t.Fatal("worker_env_self_shadow check missing when worker yaml is clean")
	}
	if workerShadow.Status != statusPass {
		t.Errorf("worker_env_self_shadow status = %q, want pass; detail: %s", workerShadow.Status, workerShadow.Detail)
	}
}

// TestCheckRecipeGenerate_EnvSelfShadow_AppTargetCovered ensures the
// self-shadow check fires on non-worker runtime targets as well. v28 was
// written after an app-target self-shadow would also slip through the recipe
// path (bootstrap checkGenerate had the check; recipe checkRecipeGenerate did
// not). Parity means every enumerated hostname gets the floor.
func TestCheckRecipeGenerate_EnvSelfShadow_AppTargetCovered(t *testing.T) {
	t.Parallel()

	plan := testRecipePlan() // single-target Laravel minimal plan
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// App-target zerops.yaml with a self-shadow line.
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), `zerops:
  - setup: dev
    build:
      base: php-nginx@8.4
      deployFiles: [.]
    run:
      envVariables:
        APP_ENV: local
        db_hostname: ${db_hostname}
      ports:
        - port: 80
  - setup: prod
    build:
      base: php-nginx@8.4
      deployFiles: [.]
    run:
      envVariables:
        APP_ENV: production
      ports:
        - port: 80
`)

	checker := checkRecipeGenerate(stateDir, nil, nil)
	result, err := checker(context.Background(), plan, testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var appShadow *workflow.StepCheck
	for i := range result.Checks {
		if result.Checks[i].Name == "app_env_self_shadow" {
			appShadow = &result.Checks[i]
			break
		}
	}
	if appShadow == nil {
		t.Fatal("app_env_self_shadow check missing — app targets also need self-shadow enumeration")
	}
	if appShadow.Status != statusFail {
		t.Errorf("app_env_self_shadow status = %q, want fail", appShadow.Status)
	}
}
