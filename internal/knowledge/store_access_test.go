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
				"YAML schema reference.\n\n## zerops.yml Schema\n\nStructure rules.\n\n" +
				"## Schema Rules\n\nPorts 10-65435.\n\n" +
				"## Build/Deploy Lifecycle\n\nBuild and Run are SEPARATE containers.",
		},
		"zerops://themes/runtimes": {
			URI:     "zerops://themes/runtimes",
			Title:   "Runtime Deltas",
			Content: "## PHP\n\nBuild php@X, run php-nginx@X. Port 80.\n\n## Node.js\n\nnode_modules in deployFiles. SSR patterns.",
		},
		"zerops://themes/services": {
			URI:     "zerops://themes/services",
			Title:   "Managed Service Reference",
			Content: "## PostgreSQL\n\nPort 5432. Env: hostname, password, connectionString.\n\n## Valkey\n\nPort 6379. Connection: redis://cache:6379.",
		},
		"zerops://themes/wiring": {
			URI:     "zerops://themes/wiring",
			Title:   "Wiring Patterns",
			Content: "## Syntax Rules\n\nUse ${hostname_var} for cross-refs.\n\nenvSecrets for sensitive data.\n\n## PostgreSQL\n\nDATABASE_URL:postgresql://${h_user}:${h_password}@{h}:5432\n\n## Valkey\n\nREDIS_URL:redis://${h_user}:${h_password}@{h}:6379",
		},
		"zerops://themes/operations": {
			URI:     "zerops://themes/operations",
			Title:   "Zerops Operations & Decisions",
			Content: "# Zerops Operations & Decisions\n\n## Choose Database\n\nUse PostgreSQL for everything unless you have a specific reason not to.\n\n## Choose Cache\n\nUse Valkey (default) — KeyDB is deprecated.\n\n## Choose Runtime Base\n\nGo, Rust, .NET build natively — use alpine base for smaller images.",
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

// --- GetBriefing Tests ---

func TestStore_GetBriefing_CoreMissing(t *testing.T) {
	t.Parallel()
	// Store without core — GetBriefing should fail
	store, _ := NewStore(map[string]*Document{})

	_, err := store.GetBriefing("php@8", nil, nil)
	if err == nil {
		t.Error("expected error when core missing")
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
