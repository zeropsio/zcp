// Tests for: Store methods — contextual assembly methods on *Store

package knowledge

import (
	"strings"
	"testing"
)

// testStoreWithCore creates a Store with mock foundation documents for testing.
func testStoreWithCore(t *testing.T) *Store {
	t.Helper()
	docs := map[string]*Document{
		"zerops://foundation/grammar": {
			URI:     "zerops://foundation/grammar",
			Title:   "Zerops Grammar",
			Content: "# Zerops Grammar\n\nUniversal rules here.\n\n## zerops.yml Schema\n\nStructure rules.\n\n## Platform Rules\n\nPorts 10-65435.",
		},
		"zerops://foundation/runtimes": {
			URI:     "zerops://foundation/runtimes",
			Title:   "Runtime Deltas",
			Content: "## PHP\n\nBuild php@X, run php-nginx@X. Port 80.\n\n## Node.js\n\nnode_modules in deployFiles. SSR patterns.",
		},
		"zerops://foundation/services": {
			URI:     "zerops://foundation/services",
			Title:   "Managed Service Reference",
			Content: "## PostgreSQL\n\nPort 5432. Env: hostname, password, connectionString.\n\n## Valkey\n\nPort 6379. Connection: redis://cache:6379.",
		},
		"zerops://foundation/wiring": {
			URI:     "zerops://foundation/wiring",
			Title:   "Wiring Patterns",
			Content: "## Syntax Rules\n\nUse ${hostname_var} for cross-refs.\n\nenvSecrets for sensitive data.\n\n## PostgreSQL\n\nDATABASE_URL:postgresql://${h_user}:${h_password}@{h}:5432\n\n## Valkey\n\nREDIS_URL:redis://${h_user}:${h_password}@{h}:6379",
		},
		"zerops://decisions/choose-database": {
			URI:     "zerops://decisions/choose-database",
			Title:   "Choose Database",
			TLDR:    "Use PostgreSQL for everything unless you have a specific reason not to.",
			Content: "# Choose Database\n\n## TL;DR\nUse PostgreSQL for everything unless you have a specific reason not to.",
		},
		"zerops://decisions/choose-cache": {
			URI:     "zerops://decisions/choose-cache",
			Title:   "Choose Cache",
			TLDR:    "Use Valkey (default) — KeyDB is deprecated.",
			Content: "# Choose Cache\n\n## TL;DR\nUse Valkey (default) — KeyDB is deprecated.",
		},
		"zerops://recipes/ghost": {
			URI:     "zerops://recipes/ghost",
			Title:   "Ghost CMS Recipe",
			Content: "maxContainers: 1\n\nUse MariaDB with wsrep.",
		},
		"zerops://recipes/laravel-jetstream": {
			URI:     "zerops://recipes/laravel-jetstream",
			Title:   "Laravel Jetstream Recipe",
			Content: "Multi-base build. S3 + Redis + Mailpit.",
		},
	}
	store, err := NewStore(docs)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

// --- GetFoundation Tests ---

func TestStore_GetFoundation_Success(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	content, err := store.GetFoundation()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(content, "Universal rules") {
		t.Error("expected foundation content")
	}
	if !strings.Contains(content, "## Platform Rules") {
		t.Error("expected section headers")
	}
}

func TestStore_GetFoundation_NotFound(t *testing.T) {
	t.Parallel()
	// Store without foundation docs
	store, _ := NewStore(map[string]*Document{})

	_, err := store.GetFoundation()
	if err == nil {
		t.Error("expected error when foundation grammar not found")
	}
}

// --- GetCorePrinciples backward compat ---

func TestStore_GetCorePrinciples_BackwardCompat(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	content, err := store.GetCorePrinciples()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(content, "Universal rules") {
		t.Error("expected foundation content via backward compat")
	}
}

// --- GetBriefing Tests ---

func TestStore_GetBriefing_RuntimeOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		runtime string
		want    []string // substrings that must be present
	}{
		{
			name:    "PHP runtime",
			runtime: "php-nginx@8.4",
			want:    []string{"Zerops Grammar", "PHP", "Build php@X", "Port 80"},
		},
		{
			name:    "Node.js runtime",
			runtime: "nodejs@22",
			want:    []string{"Zerops Grammar", "Node.js", "node_modules"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := testStoreWithCore(t)

			briefing, err := store.GetBriefing(tt.runtime, nil, nil)
			if err != nil {
				t.Fatal(err)
			}

			for _, substr := range tt.want {
				if !strings.Contains(briefing, substr) {
					t.Errorf("briefing missing %q", substr)
				}
			}
		})
	}
}

func TestStore_GetBriefing_ServicesOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		services []string
		want     []string
	}{
		{
			name:     "PostgreSQL only",
			services: []string{"postgresql@16"},
			want:     []string{"Zerops Grammar", "PostgreSQL", "Port 5432", "${hostname_var}", "DATABASE_URL"},
		},
		{
			name:     "Multiple services",
			services: []string{"postgresql@16", "valkey@7.2"},
			want:     []string{"Zerops Grammar", "PostgreSQL", "Valkey", "Port 6379", "REDIS_URL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := testStoreWithCore(t)

			briefing, err := store.GetBriefing("", tt.services, nil)
			if err != nil {
				t.Fatal(err)
			}

			for _, substr := range tt.want {
				if !strings.Contains(briefing, substr) {
					t.Errorf("briefing missing %q", substr)
				}
			}
		})
	}
}

func TestStore_GetBriefing_RuntimeAndServices(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("nodejs@22", []string{"postgresql@16", "valkey@7.2"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should contain all layers
	required := []string{
		"Zerops Grammar",
		"Node.js",
		"node_modules",
		"PostgreSQL",
		"Port 5432",
		"Valkey",
		"Port 6379",
		"${hostname_var}",
		"DATABASE_URL",
		"REDIS_URL",
		"Choose Database",
		"Choose Cache",
	}

	for _, substr := range required {
		if !strings.Contains(briefing, substr) {
			t.Errorf("briefing missing %q", substr)
		}
	}
}

func TestStore_GetBriefing_EmptyInputs(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should at least contain grammar
	if !strings.Contains(briefing, "Zerops Grammar") {
		t.Error("empty briefing should still contain grammar")
	}
}

func TestStore_GetBriefing_UnknownRuntime(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("unknown@1.0", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should contain grammar, no exception section (graceful)
	if !strings.Contains(briefing, "Zerops Grammar") {
		t.Error("briefing should contain grammar")
	}
	// Should NOT contain PHP/Node.js specific content
	if strings.Contains(briefing, "Build php@X") {
		t.Error("briefing should not contain PHP exceptions for unknown runtime")
	}
}

func TestStore_GetBriefing_UnknownService(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("", []string{"unknown-service@1"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should contain grammar + wiring syntax (services were requested)
	if !strings.Contains(briefing, "Zerops Grammar") {
		t.Error("briefing should contain grammar")
	}
	if !strings.Contains(briefing, "${hostname_var}") {
		t.Error("briefing should contain wiring syntax when services provided")
	}
}

func TestStore_GetBriefing_CoreMissing(t *testing.T) {
	t.Parallel()
	// Store without foundation
	store, _ := NewStore(map[string]*Document{})

	_, err := store.GetBriefing("php@8", nil, nil)
	if err == nil {
		t.Error("expected error when foundation missing")
	}
}

// --- GetBriefing Layered Composition Tests ---

func TestStore_GetBriefing_GrammarFirst(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("bun@1.2", []string{"postgresql@16"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	grammarIdx := strings.Index(briefing, "Zerops Grammar")
	runtimeIdx := strings.Index(briefing, "Runtime-Specific:")
	serviceIdx := strings.Index(briefing, "Service Cards")

	if grammarIdx < 0 {
		t.Fatal("briefing missing grammar")
	}
	if runtimeIdx >= 0 && grammarIdx >= runtimeIdx {
		t.Error("grammar should come before runtime delta")
	}
	if serviceIdx >= 0 && grammarIdx >= serviceIdx {
		t.Error("grammar should come before service cards")
	}
}

func TestStore_GetBriefing_WiringIncluded(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("", []string{"postgresql@16"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(briefing, "Wiring Patterns") {
		t.Error("briefing with services should include wiring patterns")
	}
	if !strings.Contains(briefing, "Wiring: PostgreSQL") {
		t.Error("briefing should include per-service wiring template")
	}
}

func TestStore_GetBriefing_NoWiringWithoutServices(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("nodejs@22", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(briefing, "Wiring Patterns") {
		t.Error("briefing without services should NOT include wiring patterns")
	}
}

func TestStore_GetBriefing_DecisionsIncluded(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("", []string{"postgresql@16", "valkey@7.2"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(briefing, "Decision Hints") {
		t.Error("briefing with managed services should include decision hints")
	}
	if !strings.Contains(briefing, "Choose Database") {
		t.Error("briefing with postgresql should include database decision")
	}
	if !strings.Contains(briefing, "Choose Cache") {
		t.Error("briefing with valkey should include cache decision")
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
			name:       "laravel-jetstream recipe",
			recipeName: "laravel-jetstream",
			want:       "Multi-base build",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := testStoreWithCore(t)

			recipe, err := store.GetRecipe(tt.recipeName)
			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(recipe, tt.want) {
				t.Errorf("recipe missing expected content %q", tt.want)
			}
		})
	}
}

func TestStore_GetRecipe_NotFound(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	_, err := store.GetRecipe("nonexistent")
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

	if len(recipes) != 2 {
		t.Errorf("expected 2 recipes, got %d", len(recipes))
	}

	// Should be sorted
	if recipes[0] != "ghost" {
		t.Errorf("first recipe should be 'ghost', got %q", recipes[0])
	}
	if recipes[1] != "laravel-jetstream" {
		t.Errorf("second recipe should be 'laravel-jetstream', got %q", recipes[1])
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
