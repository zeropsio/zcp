// Tests for: knowledge engine — GetBriefing integration tests with real embedded docs
package knowledge

import (
	"regexp"
	"strings"
	"testing"
)

func TestStore_GetBriefing_RealDocs(t *testing.T) {
	store := newTestStore(t)
	briefing, err := store.GetBriefing("php-nginx@8.4", []string{"postgresql@16"}, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	// Core reference is no longer included in briefing (use scope="infrastructure")
	if strings.Contains(briefing, "Zerops Core Reference") {
		t.Error("briefing should NOT contain core reference")
	}
	if !strings.Contains(briefing, "PHP") {
		t.Error("briefing missing PHP runtime delta")
	}
	if !strings.Contains(briefing, "PostgreSQL") {
		t.Error("briefing missing PostgreSQL service card")
	}
	if !strings.Contains(briefing, "Wiring") {
		t.Error("briefing missing wiring section")
	}
}

// --- GetBriefing Version Integration Tests ---

func TestGetBriefing_IncludesVersionCheck(t *testing.T) {
	store := newTestStore(t)
	types := testStackTypes()

	briefing, err := store.GetBriefing("nodejs@22", []string{"postgresql@16"}, types)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if !strings.Contains(briefing, "Version Check") {
		t.Error("briefing missing Version Check section")
	}
	if !strings.Contains(briefing, "\u2713") {
		t.Error("briefing missing checkmarks for valid types")
	}
}

func TestGetBriefing_VersionWarning(t *testing.T) {
	store := newTestStore(t)
	types := testStackTypes()

	briefing, err := store.GetBriefing("bun@1", []string{"postgresql@16"}, types)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if !strings.Contains(briefing, "\u26a0") {
		t.Error("briefing missing warning for invalid bun@1")
	}
}

func TestGetBriefing_NilTypes_NoVersionSection(t *testing.T) {
	store := newTestStore(t)

	briefing, err := store.GetBriefing("nodejs@22", []string{"postgresql@16"}, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if strings.Contains(briefing, "Version Check") {
		t.Error("briefing should NOT contain Version Check when types is nil")
	}
}

// --- Knowledge Content & Briefing Order Tests ---

func TestGetBriefing_BunRuntime_ContainsBindingRule(t *testing.T) {
	store := newTestStore(t)
	briefing, err := store.GetBriefing("bun@1.2", []string{"postgresql@16"}, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if !strings.Contains(briefing, "0.0.0.0") {
		t.Error("Bun briefing missing 0.0.0.0 binding rule")
	}
	if !strings.Contains(briefing, "Bun.serve") {
		t.Error("Bun briefing missing Bun.serve reference")
	}
}

func TestStore_GetRecipe_Bun(t *testing.T) {
	store := newTestStore(t)
	content, err := store.GetRecipe("bun")
	if err != nil {
		t.Fatalf("GetRecipe(bun): %v", err)
	}
	if !strings.Contains(content, "0.0.0.0") {
		t.Error("bun recipe missing 0.0.0.0 binding rule")
	}
	if !strings.Contains(content, "zerops.yml") {
		t.Error("bun recipe missing zerops.yml example")
	}
}

func TestStore_GetBriefing_SurfacesMatchingRecipes(t *testing.T) {
	store := newTestStore(t)
	briefing, err := store.GetBriefing("bun@1.2", nil, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if !strings.Contains(briefing, "Matching Recipes") {
		t.Error("Bun briefing missing Matching Recipes section")
	}
	if !strings.Contains(briefing, "bun-hono") {
		t.Error("Bun briefing missing bun-hono recipe hint")
	}
}

func TestStore_GetBriefing_NuxtRecipeForNodejs(t *testing.T) {
	store := newTestStore(t)
	briefing, err := store.GetBriefing("nodejs@22", nil, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if !strings.Contains(briefing, "nuxt") {
		t.Error("Node.js briefing missing nuxt recipe hint")
	}
}

func TestStore_GetBriefing_StaticRecipes(t *testing.T) {
	store := newTestStore(t)
	briefing, err := store.GetBriefing("static", nil, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if !strings.Contains(briefing, "Matching Recipes") {
		t.Error("static briefing missing Matching Recipes section")
	}
	if !strings.Contains(briefing, "svelte-static") {
		t.Error("static briefing missing svelte-static recipe hint")
	}
}

func TestStore_GetBriefing_RustRecipe(t *testing.T) {
	store := newTestStore(t)
	briefing, err := store.GetBriefing("rust@1", nil, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if !strings.Contains(briefing, "rust") {
		t.Error("Rust briefing missing rust recipe hint")
	}
}

// --- Runtime auto-promotion tests ---
// When runtime is empty but a known runtime name appears in services,
// GetBriefing should auto-promote it to runtime and load the runtime delta.

func TestStore_GetBriefing_AutoPromotesRuntimeFromServices(t *testing.T) {
	store := newTestStore(t)

	tests := []struct {
		name          string
		services      []string
		wantRuntime   string // expected runtime delta section
		wantService   string // expected service card (should NOT include promoted runtime)
		wantNoService string // should NOT appear as service card
	}{
		{
			name:          "python in services gets promoted",
			services:      []string{"python@3.12", "valkey@7.2"},
			wantRuntime:   "Runtime-Specific: Python",
			wantService:   "Valkey",
			wantNoService: "", // Python may appear in runtime section, not as service card
		},
		{
			name:          "java in services gets promoted",
			services:      []string{"java@21", "mariadb@10.6"},
			wantRuntime:   "Runtime-Specific: Java",
			wantService:   "MariaDB",
			wantNoService: "",
		},
		{
			name:          "nodejs in services gets promoted",
			services:      []string{"nodejs@22", "postgresql@16"},
			wantRuntime:   "Runtime-Specific: Node.js",
			wantService:   "PostgreSQL",
			wantNoService: "",
		},
		{
			name:        "pure services — no promotion",
			services:    []string{"postgresql@16", "valkey@7.2"},
			wantRuntime: "", // no runtime delta
			wantService: "PostgreSQL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			briefing, err := store.GetBriefing("", tt.services, nil)
			if err != nil {
				t.Fatalf("GetBriefing: %v", err)
			}
			if tt.wantRuntime != "" {
				if !strings.Contains(briefing, tt.wantRuntime) {
					t.Errorf("briefing missing auto-promoted runtime section %q", tt.wantRuntime)
				}
			} else {
				if strings.Contains(briefing, "Runtime-Specific:") {
					t.Error("briefing should not contain runtime delta when only managed services are passed")
				}
			}
			if tt.wantService != "" && !strings.Contains(briefing, tt.wantService) {
				t.Errorf("briefing missing expected service card %q", tt.wantService)
			}
		})
	}
}

func TestBriefing_ValkeyNoCredentials(t *testing.T) {
	store := newTestStore(t)
	briefing, err := store.GetBriefing("nodejs@22", []string{"valkey@7.2"}, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	// Should contain the correct no-auth wiring pattern
	if !strings.Contains(briefing, "redis://cache:${cache_port}") {
		t.Error("Valkey wiring should contain redis://cache:${cache_port}")
	}
	// Valkey wiring URL should NOT contain user:password@ pattern
	if strings.Contains(briefing, "${cache_user}:${cache_password}@cache") {
		t.Error("Valkey wiring URL should NOT contain credentials (${cache_user}:${cache_password}@)")
	}
	// Verify the "No authentication" guidance is present
	if !strings.Contains(briefing, "No authentication") {
		t.Error("Valkey card should mention 'No authentication'")
	}
}

func TestStore_GetBriefing_NoPromotionWhenRuntimeSet(t *testing.T) {
	store := newTestStore(t)

	// When runtime is already set, services should stay as services even if they're runtime names
	briefing, err := store.GetBriefing("php-nginx@8.4", []string{"nodejs@22", "postgresql@16"}, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	// PHP should be the runtime delta (from explicit runtime param)
	if !strings.Contains(briefing, "Runtime-Specific: PHP") {
		t.Error("briefing missing explicitly-set PHP runtime delta")
	}
	// Node.js should NOT get its own runtime delta section (it's in services, runtime is already set)
	if strings.Contains(briefing, "Runtime-Specific: Node.js") {
		t.Error("briefing should not have second runtime delta when runtime is already set")
	}
}

func TestStore_GetBriefing_LayerOrderRealDocs(t *testing.T) {
	store := newTestStore(t)
	briefing, err := store.GetBriefing("bun@1.2", []string{"postgresql@16"}, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	runtimeIdx := strings.Index(briefing, "Runtime-Specific: Bun")
	serviceIdx := strings.Index(briefing, "Service Cards")
	if runtimeIdx < 0 {
		t.Fatal("briefing missing Runtime-Specific: Bun section")
	}
	if serviceIdx < 0 {
		t.Fatal("briefing missing Service Cards section")
	}
	// Runtime -> services (no core — core is separate via scope="infrastructure")
	if runtimeIdx >= serviceIdx {
		t.Errorf("runtime (pos %d) should come before services (pos %d)", runtimeIdx, serviceIdx)
	}
}

// --- Phase 1: No static version numbers in briefing output ---

// TestGetBriefing_NoStaticVersionLines verifies that runtime delta sections returned by
// GetBriefing do NOT contain **Versions**: or **Version**: lines. These are redundant
// with FormatServiceStacks() live injection.
func TestGetBriefing_NoStaticVersionLines(t *testing.T) {
	store := newTestStore(t)

	// versionsPattern matches lines like "**Versions**: ..." or "**Version**: ..."
	versionsPattern := regexp.MustCompile(`(?m)^\*\*Versions?\*\*:`)

	runtimes := []string{
		"php-nginx@8.4", "nodejs@22", "bun@1.2", "python@3.12",
		"go@1", "java@21", "dotnet@9", "ruby@3.4", "alpine@3.23",
	}

	for _, rt := range runtimes {
		t.Run(rt, func(t *testing.T) {
			briefing, err := store.GetBriefing(rt, nil, nil)
			if err != nil {
				t.Fatalf("GetBriefing(%s): %v", rt, err)
			}
			if versionsPattern.MatchString(briefing) {
				t.Errorf("briefing for %s contains static **Versions**: line — should be removed (live stacks provide version info)", rt)
			}
		})
	}
}

// TestGetBriefing_NoStaticServiceTypeVersions verifies that service cards returned by
// GetBriefing do NOT contain hardcoded version numbers in **Type**: lines.
func TestGetBriefing_NoStaticServiceTypeVersions(t *testing.T) {
	store := newTestStore(t)

	// Match **Type**: lines that contain @ (version pinning)
	typeWithVersion := regexp.MustCompile(`(?m)^\*\*Type\*\*:.*@`)

	services := []string{
		"postgresql@16", "mariadb@10.6", "valkey@7.2",
		"elasticsearch@8.16", "kafka@3.8", "nats@2.12",
	}

	briefing, err := store.GetBriefing("", services, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if typeWithVersion.MatchString(briefing) {
		t.Errorf("briefing contains **Type**: lines with hardcoded versions — should use base name only")
	}
}
