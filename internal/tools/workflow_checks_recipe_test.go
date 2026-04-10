package tools

import (
	"context"
	"os"
	"path/filepath"
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
	writeFile(t, filepath.Join(appDir, "README.md"), validREADME)

	checker := checkRecipeGenerate(stateDir, nil)
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

	checker := checkRecipeGenerate(stateDir, nil)
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

	checker := checkRecipeGenerate(stateDir, nil)
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

func TestCheckRecipeGenerate_MissingFragments(t *testing.T) {
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
	// README without any fragments.
	writeFile(t, filepath.Join(appDir, "README.md"), "# App\nJust a basic readme.")

	checker := checkRecipeGenerate(stateDir, nil)
	result, err := checker(context.Background(), testRecipePlan(), testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected checks to fail when fragments are missing")
	}

	// Verify specific failures.
	failedNames := make(map[string]bool)
	for _, c := range result.Checks {
		if c.Status == "fail" {
			failedNames[c.Name] = true
		}
	}
	for _, name := range []string{"fragment_integration-guide", "fragment_knowledge-base", "fragment_intro"} {
		if !failedNames[name] {
			t.Errorf("expected %q to fail", name)
		}
	}
}

func TestCheckRecipeGenerate_LowCommentRatio(t *testing.T) {
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

	checker := checkRecipeGenerate(stateDir, nil)
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

func TestCheckRecipeGenerate_PlaceholderText(t *testing.T) {
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

	checker := checkRecipeGenerate(stateDir, nil)
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
			{Hostname: "worker", Type: "php-nginx@8.4", IsWorker: true},
			{Hostname: "db", Type: "postgresql@18"},
			{Hostname: "redis", Type: "valkey@7.2"},
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

	checker := checkRecipeGenerate(stateDir, nil)
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

	checker := checkRecipeGenerate(stateDir, nil)
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

			checker := checkRecipeGenerate(stateDir, vf)
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
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true},
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

	checker := checkRecipeGenerate(stateDir, nil)
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
			{Hostname: "worker", Type: "php-nginx@8.4", IsWorker: true},
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

	checker := checkRecipeGenerate(stateDir, nil)
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
