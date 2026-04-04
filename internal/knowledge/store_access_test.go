// Tests for: Store methods — contextual assembly methods on *Store
package knowledge

import (
	"strings"
	"testing"
)

// testStoreWithCore creates a Store with mock theme documents for testing.
func testStoreWithCore(t *testing.T) *Store {
	t.Helper()
	docs := map[string]*Document{
		"zerops://themes/core": {
			URI:   "zerops://themes/core",
			Title: "Zerops Core Reference",
			Content: "# Zerops Core Reference\n\nConceptual model of how Zerops works.\n\n" +
				"ALWAYS bind 0.0.0.0. NEVER use apt-get on Alpine.\n\n" +
				"YAML schema reference.\n\n## zerops.yaml Schema\n\nStructure rules.\n\n" +
				"## Schema Rules\n\nPorts 10-65435.\n\n" +
				"## Build/Deploy Lifecycle\n\nBuild and Run are SEPARATE containers.",
		},
		"zerops://themes/model": {
			URI:     "zerops://themes/model",
			Title:   "Zerops Platform Model",
			Content: "# Zerops Platform Model\n\n## Container Universe\n\nFull Linux containers (Incus). Hierarchy: Project > Service > Container(s).\n\n## Networking\n\nL7 LB terminates SSL. Ports 10-65435.\n\n## Platform Constraints\n\nBind 0.0.0.0. deployFiles mandatory. No .env files. Cross-service: ${hostname_varname}.",
		},
		"zerops://recipes/php-hello-world": {
			URI:     "zerops://recipes/php-hello-world",
			Title:   "PHP Hello World on Zerops",
			Content: "# PHP Hello World on Zerops\n\nBuild php@X, run php-nginx@X. Port 80.\n\n### Build != Run\nBuild php@X, run php-nginx@X. Port 80.",
		},
		"zerops://recipes/nodejs-hello-world": {
			URI:     "zerops://recipes/nodejs-hello-world",
			Title:   "Node.js Hello World on Zerops",
			Content: "# Node.js Hello World on Zerops\n\nnode_modules in deployFiles. SSR patterns.\n\n### Build Procedure\nnode_modules in deployFiles. SSR patterns.",
		},
		"zerops://themes/services": {
			URI:     "zerops://themes/services",
			Title:   "Managed Service Reference",
			Content: "## Wiring Syntax\n\nUse ${hostname_var} for cross-refs.\n\nenvSecrets for sensitive data.\n\n## Service Wiring Templates\n\nVARS = config, SECRETS = credentials.\n\n## PostgreSQL\n\nPort 5432. Env: hostname, password, connectionString.\n**Wiring**: DATABASE_URL:postgresql://${h_user}:${h_password}@{h}:5432\n\n## Valkey\n\nPort 6379. Connection: redis://cache:6379.\n**Wiring**: REDIS_URL:redis://cache:6379",
		},
		"zerops://themes/operations": {
			URI:     "zerops://themes/operations",
			Title:   "Zerops Operations & Production",
			Content: "# Zerops Operations & Production\n\n## Networking\n\nVXLAN private network.\n\n## Production Checklist\n\nHA mode, backups, scaling.",
		},
		"zerops://decisions/choose-database": {
			URI:         "zerops://decisions/choose-database",
			Title:       "Choosing a Database on Zerops",
			Description: "Use PostgreSQL for everything unless you have a specific reason not to.",
			Content:     "# Choosing a Database\n\nUse PostgreSQL for everything unless you have a specific reason not to.\n\n## Decision Matrix\nPostgreSQL (default), MariaDB, ClickHouse.",
		},
		"zerops://decisions/choose-cache": {
			URI:         "zerops://decisions/choose-cache",
			Title:       "Choosing a Cache on Zerops",
			Description: "Use Valkey — KeyDB is deprecated.",
			Content:     "# Choosing a Cache\n\nUse Valkey — KeyDB is deprecated.",
		},
		"zerops://decisions/choose-queue": {
			URI:         "zerops://decisions/choose-queue",
			Title:       "Choosing a Queue on Zerops",
			Description: "Use NATS for most use cases.",
			Content:     "# Choosing a Queue\n\nUse NATS for most use cases.",
		},
		"zerops://decisions/choose-search": {
			URI:         "zerops://decisions/choose-search",
			Title:       "Choosing a Search on Zerops",
			Description: "Use Meilisearch for simple full-text.",
			Content:     "# Choosing a Search\n\nUse Meilisearch for simple full-text.",
		},
		"zerops://decisions/choose-runtime-base": {
			URI:         "zerops://decisions/choose-runtime-base",
			Title:       "Choosing a Runtime Base on Zerops",
			Description: "Use Alpine as default. Ubuntu for glibc. Docker for pre-built images.",
			Content:     "# Choosing a Runtime Base\n\nUse Alpine as default. Ubuntu for glibc. Docker for pre-built images.",
		},
		"zerops://recipes/ghost": {
			URI:     "zerops://recipes/ghost",
			Title:   "Ghost CMS Recipe",
			Content: "maxContainers: 1\n\nUse MariaDB with wsrep.",
		},
		"zerops://recipes/laravel": {
			URI:     "zerops://recipes/laravel",
			Title:   "Laravel on Zerops",
			Content: "Multi-base build. S3 + Redis + PostgreSQL.",
		},
	}
	store, err := NewStore(docs)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

// --- GetCore Tests ---

func TestStore_GetCore_Success(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	content, err := store.GetCore()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(content, "Conceptual model") {
		t.Error("expected platform model content")
	}
	if !strings.Contains(content, "ALWAYS") {
		t.Error("expected ALWAYS rules")
	}
	if !strings.Contains(content, "NEVER") {
		t.Error("expected NEVER rules")
	}
	if !strings.Contains(content, "YAML schema reference") {
		t.Error("expected grammar content")
	}
	if !strings.Contains(content, "## Schema Rules") {
		t.Error("expected section headers")
	}
	if !strings.Contains(content, "Build/Deploy Lifecycle") {
		t.Error("expected lifecycle section")
	}
}

func TestStore_GetCore_NotFound(t *testing.T) {
	t.Parallel()
	store, _ := NewStore(map[string]*Document{})

	_, err := store.GetCore()
	if err == nil {
		t.Error("expected error when core not found")
	}
}

// --- GetUniversals Tests ---

func TestStore_GetUniversals_Success(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	content, err := store.GetUniversals()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(content, "Platform Constraints") {
		t.Error("expected universals title")
	}
	if !strings.Contains(content, "0.0.0.0") {
		t.Error("expected bind address rule")
	}
	if !strings.Contains(content, "deployFiles") {
		t.Error("expected deployFiles rule")
	}
}

func TestStore_GetUniversals_NotFound(t *testing.T) {
	t.Parallel()
	store, _ := NewStore(map[string]*Document{})

	_, err := store.GetUniversals()
	if err == nil {
		t.Error("expected error when universals not found")
	}
}

// --- GetRecipe Tests ---

func TestStore_GetRecipe_Success(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		recipeName string
		want       string
	}{
		{
			name:       "ghost recipe",
			recipeName: "ghost",
			want:       "maxContainers: 1",
		},
		{
			name:       "laravel recipe",
			recipeName: "laravel",
			want:       "Multi-base build",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := testStoreWithCore(t)

			recipe, err := store.GetRecipe(tt.recipeName, "")
			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(recipe, tt.want) {
				t.Errorf("recipe missing expected content %q", tt.want)
			}
		})
	}
}

func TestStore_GetRecipe_PrependsUniversals(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	recipe, err := store.GetRecipe("ghost", "")
	if err != nil {
		t.Fatal(err)
	}

	// Should contain universals before recipe content
	if !strings.Contains(recipe, "Platform Constraints") {
		t.Error("recipe should be prepended with universals")
	}
	if !strings.Contains(recipe, "maxContainers: 1") {
		t.Error("recipe should still contain original content")
	}

	// Universals should appear before recipe content
	uIdx := strings.Index(recipe, "Platform Constraints")
	rIdx := strings.Index(recipe, "maxContainers: 1")
	if uIdx >= rIdx {
		t.Error("universals should appear before recipe content")
	}
}

func TestStore_GetRecipe_WithoutUniversals(t *testing.T) {
	t.Parallel()
	// Store without universals document
	docs := map[string]*Document{
		"zerops://recipes/ghost": {
			URI:     "zerops://recipes/ghost",
			Title:   "Ghost",
			Content: "maxContainers: 1",
		},
	}
	store, _ := NewStore(docs)

	recipe, err := store.GetRecipe("ghost", "")
	if err != nil {
		t.Fatal(err)
	}

	// Should still return recipe content without universals
	if !strings.Contains(recipe, "maxContainers: 1") {
		t.Error("recipe should return content even without universals")
	}
}

func TestStore_GetRecipe_NotFound(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	_, err := store.GetRecipe("nonexistent", "")
	if err == nil {
		t.Error("expected error for nonexistent recipe")
	}

	// Error should mention available recipes
	if !strings.Contains(err.Error(), "available") {
		t.Error("error should list available recipes")
	}
}

// --- ListRecipes Tests ---

func TestStore_ListRecipes_Success(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	recipes := store.ListRecipes()

	if len(recipes) != 4 {
		t.Errorf("expected 4 recipes, got %d: %v", len(recipes), recipes)
	}

	// Should be sorted alphabetically
	expected := []string{"ghost", "laravel", "nodejs-hello-world", "php-hello-world"}
	for i, want := range expected {
		if i < len(recipes) && recipes[i] != want {
			t.Errorf("recipe[%d] should be %q, got %q", i, want, recipes[i])
		}
	}
}

func TestStore_ListRecipes_Empty(t *testing.T) {
	t.Parallel()
	store, _ := NewStore(map[string]*Document{})

	recipes := store.ListRecipes()

	if len(recipes) != 0 {
		t.Errorf("expected empty list, got %d recipes", len(recipes))
	}
}

// --- Fuzzy Recipe Lookup Tests ---

func TestStore_GetRecipe_FuzzyMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		query      string
		docs       map[string]*Document
		wantErr    bool
		wantSubstr string // substring expected in result (content or disambiguation)
	}{
		{
			name:  "PrefixAutoResolve",
			query: "laravel",
			docs: map[string]*Document{
				"zerops://recipes/ghost":   {URI: "zerops://recipes/ghost", Content: "Ghost recipe"},
				"zerops://recipes/laravel": {URI: "zerops://recipes/laravel", Content: "Laravel recipe content"},
			},
			wantSubstr: "Laravel recipe content",
		},
		{
			name:  "PrefixSingleFuzzy",
			query: "next",
			docs: map[string]*Document{
				"zerops://recipes/ghost":      {URI: "zerops://recipes/ghost", Content: "Ghost recipe"},
				"zerops://recipes/nextjs-ssr": {URI: "zerops://recipes/nextjs-ssr", Content: "Next.js SSR recipe", Description: "SSR on Node.js"},
			},
			wantSubstr: "Next.js SSR recipe",
		},
		{
			name:  "PrefixMultipleResults",
			query: "next",
			docs: map[string]*Document{
				"zerops://recipes/ghost":      {URI: "zerops://recipes/ghost", Content: "Ghost recipe"},
				"zerops://recipes/nextjs-ssr": {URI: "zerops://recipes/nextjs-ssr", Content: "SSR recipe", Description: "Next.js SSR on Node.js"},
				"zerops://recipes/nextjs":     {URI: "zerops://recipes/nextjs", Content: "Merged recipe", Description: "Next.js on Zerops"},
			},
			wantSubstr: "Multiple recipes match",
		},
		{
			name:  "ContainsMatch",
			query: "spring",
			docs: map[string]*Document{
				"zerops://recipes/ghost":       {URI: "zerops://recipes/ghost", Content: "Ghost recipe"},
				"zerops://recipes/java-spring": {URI: "zerops://recipes/java-spring", Content: "Java Spring recipe"},
			},
			wantSubstr: "Java Spring recipe",
		},
		{
			name:  "ContentSingleMatch",
			query: "wsgi",
			docs: map[string]*Document{
				"zerops://recipes/ghost":  {URI: "zerops://recipes/ghost", Content: "Ghost recipe"},
				"zerops://recipes/django": {URI: "zerops://recipes/django", Content: "Django recipe with wsgi and gunicorn"},
			},
			wantSubstr: "Django recipe",
		},
		{
			name:  "CaseInsensitive",
			query: "Django",
			docs: map[string]*Document{
				"zerops://recipes/ghost":  {URI: "zerops://recipes/ghost", Content: "Ghost recipe"},
				"zerops://recipes/django": {URI: "zerops://recipes/django", Content: "Django recipe"},
			},
			wantSubstr: "Django recipe",
		},
		{
			name:  "NoMatch",
			query: "foobar",
			docs: map[string]*Document{
				"zerops://recipes/ghost": {URI: "zerops://recipes/ghost", Content: "Ghost recipe"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store, err := NewStore(tt.docs)
			if err != nil {
				t.Fatalf("NewStore: %v", err)
			}

			result, err := store.GetRecipe(tt.query, "")
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if err != nil && !strings.Contains(err.Error(), "not found") {
					t.Errorf("error should contain 'not found', got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(result, tt.wantSubstr) {
				t.Errorf("result missing expected substring %q, got:\n%s", tt.wantSubstr, result)
			}
		})
	}
}

// --- getRuntimeGuide Tests ---

func TestStore_GetRuntimeGuide(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		slug string
		want string // substring expected in guide
	}{
		{
			name: "PHP guide",
			slug: "php",
			want: "PHP",
		},
		{
			name: "Node.js guide",
			slug: "nodejs",
			want: "Node.js",
		},
		{
			name: "unknown returns empty",
			slug: "nonexistent",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := testStoreWithCore(t)
			guide := store.getRuntimeGuide(tt.slug)
			if tt.want == "" {
				if guide != "" {
					t.Errorf("expected empty guide, got %d chars", len(guide))
				}
				return
			}
			if !strings.Contains(guide, tt.want) {
				t.Errorf("guide missing %q", tt.want)
			}
		})
	}
}

// --- detectRecipeRuntime Tests ---

func TestStore_DetectRecipeRuntime(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	tests := []struct {
		recipe string
		want   string
	}{
		{"laravel", "php"},
		{"nextjs-ssr", "nodejs"},
		{"svelte-nodejs", "nodejs"},
		{"django", "python"},
		{"echo-go", "go"},
		{"dotnet", "dotnet"},
		{"ghost", "nodejs"},
		{"bun-hello-world", "bun"},
		// Static-only recipes should return "" (static skipped)
		{"angular", ""},
		{"vue", ""},
		// Unknown recipe
		{"unknown-recipe", ""},
	}

	for _, tt := range tests {
		t.Run(tt.recipe, func(t *testing.T) {
			t.Parallel()
			got := store.detectRecipeRuntime(tt.recipe)
			if got != tt.want {
				t.Errorf("detectRecipeRuntime(%q) = %q, want %q", tt.recipe, got, tt.want)
			}
		})
	}
}

// --- GetRecipe auto-prepend Tests ---

func TestStore_GetRecipe_PrependsUniversalsOnly(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	recipe, err := store.GetRecipe("laravel", "")
	if err != nil {
		t.Fatal(err)
	}

	// Should contain universals
	if !strings.Contains(recipe, "Platform Constraints") {
		t.Error("recipe should be prepended with universals")
	}
	// Should NOT contain runtime guide — framework recipes are standalone
	if strings.Contains(recipe, "PHP on Zerops") {
		t.Error("recipe should NOT be prepended with runtime guide (framework recipes are standalone)")
	}
	// Should contain recipe content
	if !strings.Contains(recipe, "Multi-base build") {
		t.Error("recipe should contain original content")
	}
	// Order: universals -> recipe content
	uIdx := strings.Index(recipe, "Platform Constraints")
	cIdx := strings.Index(recipe, "Multi-base build")
	if uIdx >= cIdx {
		t.Error("universals should appear before recipe content")
	}
}
