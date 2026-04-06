package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
)

// mockRecipeProvider implements knowledge.Provider for testing recipe chain logic.
type mockRecipeProvider struct {
	recipes map[string]string // name → content
}

func (m *mockRecipeProvider) Get(string) (*knowledge.Document, error)     { return nil, nil } //nolint:nilnil // test mock
func (m *mockRecipeProvider) Search(string, int) []knowledge.SearchResult { return nil }
func (m *mockRecipeProvider) GetCore() (string, error)                    { return "", nil }
func (m *mockRecipeProvider) GetUniversals() (string, error)              { return "", nil }
func (m *mockRecipeProvider) GetModel() (string, error)                   { return "", nil }
func (m *mockRecipeProvider) GetBriefing(string, []string, string, []platform.ServiceStackType) (string, error) {
	return "", nil
}
func (m *mockRecipeProvider) GetRecipe(name, _ string) (string, error) {
	if content, ok := m.recipes[name]; ok {
		return content, nil
	}
	return "", nil
}
func (m *mockRecipeProvider) ListRecipes() []string {
	names := make([]string, 0, len(m.recipes))
	for name := range m.recipes {
		names = append(names, name)
	}
	return names
}

func TestRecipeTierRank(t *testing.T) {
	t.Parallel()
	tests := []struct {
		slug string
		want int
	}{
		{"php-hello-world", 0},
		{"bun-hello-world", 0},
		{"laravel-minimal", 1},
		{"django-minimal", 1},
		{"laravel-showcase", 2},
		{"rails-showcase", 2},
		{"unknown-recipe", -1},
		{"", -1},
	}
	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			t.Parallel()
			if got := recipeTierRank(tt.slug); got != tt.want {
				t.Errorf("recipeTierRank(%q) = %d, want %d", tt.slug, got, tt.want)
			}
		})
	}
}

func TestExtractKnowledgeSections(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    string
		absent  string
	}{
		{
			name: "extracts_gotchas_before_zeropsyaml",
			content: `# Bun Hello World on Zerops

## Base Image

Includes: Bun, npm, yarn, git, bunx.

## Gotchas

- **BUN_INSTALL** — use ./.bun for caching.
- **bunx** — use instead of npx.

## 1. Adding zerops.yaml

` + "```yaml\nzerops:\n  - setup: prod\n```",
			want:   "## Gotchas",
			absent: "zerops.yaml",
		},
		{
			name:    "no_knowledge_sections",
			content: "# PHP Hello World\n\n## 1. Adding `zerops.yaml`\n\n```yaml\nzerops:\n```",
			want:    "",
		},
		{
			name: "strips_title_keeps_sections",
			content: `# Some Recipe

## Base Image

Has stuff.

## Gotchas

- Gotcha 1

## 1. Adding config
`,
			want:   "## Base Image",
			absent: "# Some Recipe",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractKnowledgeSections(tt.content)
			if tt.want == "" {
				if got != "" {
					t.Errorf("expected empty, got: %q", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("expected to contain %q, got: %q", tt.want, got)
			}
			if tt.absent != "" && strings.Contains(got, tt.absent) {
				t.Errorf("should not contain %q, got: %q", tt.absent, got)
			}
		})
	}
}

func TestFindRelatedRecipes(t *testing.T) {
	t.Parallel()
	allRecipes := []string{
		"bun-hello-world",
		"laravel-minimal",
		"laravel-showcase",
		"php-hello-world",
		"nodejs-hello-world",
		"django-minimal",
	}

	tests := []struct {
		name        string
		framework   string
		runtimeBase string
		currentSlug string
		wantNames   []string
	}{
		{
			name:        "showcase_finds_minimal_and_hello",
			framework:   "laravel",
			runtimeBase: "php",
			currentSlug: "laravel-showcase",
			wantNames:   []string{"php-hello-world", "laravel-minimal"},
		},
		{
			name:        "minimal_finds_hello",
			framework:   "laravel",
			runtimeBase: "php",
			currentSlug: "laravel-minimal",
			wantNames:   []string{"php-hello-world"},
		},
		{
			name:        "hello_finds_nothing",
			framework:   "php",
			runtimeBase: "php",
			currentSlug: "php-hello-world",
			wantNames:   nil,
		},
		{
			name:        "excludes_self",
			framework:   "laravel",
			runtimeBase: "php",
			currentSlug: "laravel-minimal",
			wantNames:   []string{"php-hello-world"},
		},
		{
			name:        "no_cross_framework",
			framework:   "django",
			runtimeBase: "python",
			currentSlug: "django-minimal",
			wantNames:   nil, // no python-hello-world in store
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := findRelatedRecipes(allRecipes, tt.framework, tt.runtimeBase, tt.currentSlug)
			if len(got) != len(tt.wantNames) {
				names := make([]string, 0, len(got))
				for _, c := range got {
					names = append(names, c.name)
				}
				t.Fatalf("got %d candidates %v, want %d %v", len(got), names, len(tt.wantNames), tt.wantNames)
			}
			for i, want := range tt.wantNames {
				if got[i].name != want {
					t.Errorf("candidate[%d] = %q, want %q", i, got[i].name, want)
				}
			}
		})
	}
}

func TestRecipeKnowledgeChain_ShowcaseGetsBoth(t *testing.T) {
	t.Parallel()
	kp := &mockRecipeProvider{
		recipes: map[string]string{
			"php-hello-world": `# PHP Hello World

## Gotchas

- **PDO extension** — included in base image.

## 1. Adding zerops.yaml

` + "```yaml\nzerops:\n  - setup: prod\n```",
			"laravel-minimal": `# Laravel Minimal

## Gotchas

- **No .env file** — Zerops injects env vars as OS vars.

## 1. Adding zerops.yaml

` + "```yaml\nzerops:\n  - setup: prod\n    build:\n      base: php@8.4\n```",
		},
	}
	plan := &RecipePlan{
		Framework:   "laravel",
		Tier:        RecipeTierShowcase,
		Slug:        "laravel-showcase",
		RuntimeType: "php-nginx@8.4",
	}

	result := recipeKnowledgeChain(plan, kp)

	// Should contain full minimal content.
	if !strings.Contains(result, "laravel-minimal") {
		t.Error("expected laravel-minimal in chain")
	}
	if !strings.Contains(result, "No .env file") {
		t.Error("expected minimal gotcha in full content")
	}
	if !strings.Contains(result, "(full)") {
		t.Error("expected '(full)' label for direct predecessor")
	}

	// Should contain hello-world gotchas only.
	if !strings.Contains(result, "php-hello-world") {
		t.Error("expected php-hello-world in chain")
	}
	if !strings.Contains(result, "PDO extension") {
		t.Error("expected hello-world gotcha")
	}
	if !strings.Contains(result, "(gotchas only)") {
		t.Error("expected '(gotchas only)' label for earlier ancestor")
	}

	// Minimal's full content includes its own zerops.yaml — that's expected.
	// Hello-world's zerops.yaml should be stripped (only gotchas injected).
	// Count: minimal has "setup: prod" in its full content, but hello-world's should be absent.
	if strings.Count(result, "setup: prod") > 1 {
		t.Error("hello-world zerops.yaml config should be stripped, but found multiple 'setup: prod' occurrences")
	}
}

func TestRecipeKnowledgeChain_MinimalGetsHelloFull(t *testing.T) {
	t.Parallel()
	kp := &mockRecipeProvider{
		recipes: map[string]string{
			"php-hello-world": `# PHP Hello World

## Gotchas

- **PDO extension** — included.

## 1. Adding zerops.yaml

` + "```yaml\nzerops:\n  - setup: prod\n```",
		},
	}
	plan := &RecipePlan{
		Framework:   "laravel",
		Tier:        RecipeTierMinimal,
		Slug:        "laravel-minimal",
		RuntimeType: "php-nginx@8.4",
	}

	result := recipeKnowledgeChain(plan, kp)

	if !strings.Contains(result, "php-hello-world") {
		t.Error("expected php-hello-world in chain")
	}
	if !strings.Contains(result, "(full)") {
		t.Error("expected full content for direct predecessor")
	}
	// Full content should include the zerops.yaml section.
	if !strings.Contains(result, "setup: prod") {
		t.Error("expected zerops.yaml in full hello-world content")
	}
}

func TestRecipeKnowledgeChain_HelloWorldGetsNothing(t *testing.T) {
	t.Parallel()
	kp := &mockRecipeProvider{
		recipes: map[string]string{
			"php-hello-world": "# PHP Hello World\n\n## Gotchas\n\n- gotcha",
		},
	}
	plan := &RecipePlan{
		Framework:   "php",
		Tier:        "hello-world", // not a real tier constant but rank 0
		Slug:        "php-hello-world",
		RuntimeType: "php@8.4",
	}

	result := recipeKnowledgeChain(plan, kp)
	if result != "" {
		t.Errorf("expected empty for hello-world tier, got: %q", result)
	}
}

func TestRecipeKnowledgeChain_NilPlan(t *testing.T) {
	t.Parallel()
	kp := &mockRecipeProvider{recipes: map[string]string{}}
	if result := recipeKnowledgeChain(nil, kp); result != "" {
		t.Errorf("expected empty for nil plan, got: %q", result)
	}
}

func TestNormalizeRuntimeBase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"php-nginx", "php"},
		{"php-apache", "php"},
		{"nodejs", "nodejs"},
		{"bun", "bun"},
		{"go", "go"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			if got := normalizeRuntimeBase(tt.input); got != tt.want {
				t.Errorf("normalizeRuntimeBase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
