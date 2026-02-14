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
		"zerops://foundation/core": {
			URI:     "zerops://foundation/core",
			Title:   "Zerops Fundamentals",
			Content: "# Zerops Fundamentals\n\nUniversal rules here.\n\n## 3. Build Pipeline\n\nStructure rules.\n\n## 5. Networking\n\nPorts 10-65435.",
		},
		"zerops://foundation/runtimes": {
			URI:     "zerops://foundation/runtimes",
			Title:   "Runtime Exceptions",
			Content: "## PHP\n\nBuild php@X, run php-nginx@X. Port 80.\n\n## Node.js\n\nnode_modules in deployFiles. SSR patterns.",
		},
		"zerops://foundation/services": {
			URI:     "zerops://foundation/services",
			Title:   "Managed Service Reference",
			Content: "## PostgreSQL\n\nPort 5432. Env: hostname, password, connectionString.\n\n## Valkey\n\nPort 6379. Connection: redis://cache:6379.\n\n## Wiring Patterns\n\nUse ${hostname_var} for cross-refs.\n\nenvSecrets for sensitive data.",
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
	if !strings.Contains(content, "## 5. Networking") {
		t.Error("expected section headers")
	}
}

func TestStore_GetFoundation_NotFound(t *testing.T) {
	t.Parallel()
	// Store without foundation docs
	store, _ := NewStore(map[string]*Document{})

	_, err := store.GetFoundation()
	if err == nil {
		t.Error("expected error when foundation core not found")
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
			want:    []string{"Zerops Fundamentals", "PHP", "Build php@X", "Port 80"},
		},
		{
			name:    "Node.js runtime",
			runtime: "nodejs@22",
			want:    []string{"Zerops Fundamentals", "Node.js", "node_modules"},
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
			want:     []string{"Zerops Fundamentals", "PostgreSQL", "Port 5432", "${hostname_var}"},
		},
		{
			name:     "Multiple services",
			services: []string{"postgresql@16", "valkey@7.2"},
			want:     []string{"Zerops Fundamentals", "PostgreSQL", "Valkey", "Port 6379"},
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

	// Should contain all components
	required := []string{
		"Zerops Fundamentals",
		"Node.js",
		"node_modules",
		"PostgreSQL",
		"Port 5432",
		"Valkey",
		"Port 6379",
		"${hostname_var}",
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

	// Should at least contain foundation
	if !strings.Contains(briefing, "Zerops Fundamentals") {
		t.Error("empty briefing should still contain foundation")
	}
}

func TestStore_GetBriefing_UnknownRuntime(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("unknown@1.0", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should contain foundation, no exception section (graceful)
	if !strings.Contains(briefing, "Zerops Fundamentals") {
		t.Error("briefing should contain foundation")
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

	// Should contain foundation + wiring, no service card (graceful)
	if !strings.Contains(briefing, "Zerops Fundamentals") {
		t.Error("briefing should contain foundation")
	}
	if !strings.Contains(briefing, "${hostname_var}") {
		t.Error("briefing should contain wiring patterns when services provided")
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
