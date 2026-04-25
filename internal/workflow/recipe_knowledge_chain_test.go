package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
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
func (m *mockRecipeProvider) GetBriefing(string, []string, topology.Mode, []platform.ServiceStackType) (string, error) {
	return "", nil
}
func (m *mockRecipeProvider) GetRecipe(name string, _ topology.Mode) (string, error) {
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

// TestExtractForPredecessor covers the direct-predecessor extractor: it must
// return the Gotchas H2 body plus the zerops.yaml code fence from ## 1. —
// and nothing else (integration-step prose is dropped).
func TestExtractForPredecessor(t *testing.T) {
	t.Parallel()
	const content = `# NestJS Minimal

## Gotchas

- **Trust proxy** — must be enabled at runtime.
- **Vite dev host** — add .zerops.app to server.host config.

## 1. Adding ` + "`zerops.yaml`" + `

Open ` + "`zerops.yaml`" + ` in your editor and paste the template below, then adapt the build/run commands to your project.

` + "```yaml\nzerops:\n  - setup: api\n    build:\n      base: nodejs@22\n    run:\n      base: nodejs@22\n```" + `

## 2. Trust proxy and bind 0.0.0.0

Open ` + "`main.ts`" + ` and add the following to your bootstrap function:

` + "```ts\napp.set('trust proxy', true)\n```"

	got := extractForPredecessor(content)

	// Must contain the Gotchas H2 and both items.
	if !strings.Contains(got, "## Gotchas") {
		t.Error("expected Gotchas H2 in predecessor extract")
	}
	if !strings.Contains(got, "Trust proxy") {
		t.Error("expected Gotchas bullet in extract")
	}
	// Must contain the zerops.yaml code fence.
	if !strings.Contains(got, "```yaml") {
		t.Error("expected yaml fence in extract")
	}
	if !strings.Contains(got, "setup: api") {
		t.Error("expected yaml fence body in extract")
	}
	// Must NOT contain the trailing integration prose.
	if strings.Contains(got, "## 2. Trust proxy") {
		t.Error("integration step H2 should be dropped")
	}
	if strings.Contains(got, "app.set('trust proxy'") {
		t.Error("integration TS fence should be dropped")
	}
}

// TestExtractForAncestor_NoGotchas_ReturnsEmpty is the regression guard
// against the old extractor's behavior of emitting "This recipe
// demonstrates..." title-intro filler as if it were knowledge content.
// Hello-world recipes commonly lack a ## Gotchas H2; those must yield
// empty, not filler.
func TestExtractForAncestor_NoGotchas_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	const content = `# Node.js Hello World

A minimal Node.js app deployed to Zerops.

## 1. Adding zerops.yaml

` + "```yaml\nzerops:\n  - setup: app\n```"

	got := extractForAncestor(content)
	if got != "" {
		t.Errorf("expected empty ancestor extract for recipe with no Gotchas H2, got %q", got)
	}
}

// TestExtractForAncestor_WithGotchas_ReturnsBody ensures a recipe that DOES
// have a Gotchas H2 is surfaced to callers — the empty-on-missing behaviour
// must not swallow valid ancestor knowledge.
func TestExtractForAncestor_WithGotchas_ReturnsBody(t *testing.T) {
	t.Parallel()
	const content = `# PHP Hello World

## Gotchas

- **PDO extension** — included in base image.

## 1. Adding zerops.yaml

` + "```yaml\nzerops:\n```"

	got := extractForAncestor(content)
	if !strings.Contains(got, "PDO extension") {
		t.Errorf("expected Gotchas body in ancestor extract, got %q", got)
	}
	if strings.Contains(got, "## 1.") {
		t.Error("zerops.yaml H2 should be dropped from ancestor extract")
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

func TestRecipeKnowledgeChain_ShowcaseGetsPredecessorAndAncestor(t *testing.T) {
	t.Parallel()
	kp := &mockRecipeProvider{
		recipes: map[string]string{
			"php-hello-world": `# PHP Hello World

## Gotchas

- **PDO extension** — included in base image.

## 1. Adding zerops.yaml

` + "```yaml\nzerops:\n  - setup: helloprod\n```",
			"laravel-minimal": `# Laravel Minimal

## Gotchas

- **No .env file** — Zerops injects env vars as OS vars.

## 1. Adding zerops.yaml

Open ` + "`zerops.yaml`" + ` and paste:

` + "```yaml\nzerops:\n  - setup: prod\n    build:\n      base: php@8.4\n```" + `

## 2. Configure app

Do stuff.
`,
		},
	}
	plan := &RecipePlan{
		Framework:   "laravel",
		Tier:        RecipeTierShowcase,
		Slug:        "laravel-showcase",
		RuntimeType: "php-nginx@8.4",
	}

	result := recipeKnowledgeChain(plan, kp)

	// Predecessor (laravel-minimal): Gotchas + YAML template.
	if !strings.Contains(result, "laravel-minimal") {
		t.Error("expected laravel-minimal in chain")
	}
	if !strings.Contains(result, "(predecessor)") {
		t.Error("expected '(predecessor)' label for direct predecessor")
	}
	if !strings.Contains(result, "No .env file") {
		t.Error("expected predecessor gotcha body in chain")
	}
	if !strings.Contains(result, "setup: prod") {
		t.Error("expected predecessor YAML template fence in chain")
	}
	if strings.Contains(result, "## 2. Configure app") {
		t.Error("predecessor integration-step H2 should be dropped")
	}

	// Ancestor (php-hello-world): Gotchas only, no YAML.
	if !strings.Contains(result, "php-hello-world") {
		t.Error("expected php-hello-world in chain")
	}
	if !strings.Contains(result, "(ancestor gotchas)") {
		t.Error("expected '(ancestor gotchas)' label for earlier ancestor")
	}
	if !strings.Contains(result, "PDO extension") {
		t.Error("expected ancestor gotcha body")
	}
	if strings.Contains(result, "setup: helloprod") {
		t.Error("hello-world ancestor YAML should NOT be injected")
	}
}

func TestRecipeKnowledgeChain_MinimalGetsHelloPredecessor(t *testing.T) {
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
	if !strings.Contains(result, "(predecessor)") {
		t.Error("expected predecessor label for tier-delta-1 recipe")
	}
	// Predecessor extract pulls the YAML fence — the template content lands.
	if !strings.Contains(result, "setup: prod") {
		t.Error("expected predecessor YAML fence in chain")
	}
	if !strings.Contains(result, "PDO extension") {
		t.Error("expected predecessor Gotchas in chain")
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
