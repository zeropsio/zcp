package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

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
  - setup: app
    build:
      base: php-nginx@8.4
      buildCommands:
        - composer install
      deployFiles:
        - .
    run:
      start: php artisan serve
      ports:
        - port: 80
          httpSupport: true
`)
	writeFile(t, filepath.Join(appDir, "README.md"), validREADME)

	checker := checkRecipeGenerate(stateDir)
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

	checker := checkRecipeGenerate(stateDir)
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

	checker := checkRecipeGenerate(stateDir)
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

	checker := checkRecipeGenerate(stateDir)
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

// writeFile is a test helper to write a file.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
