// Tests for: knowledge engine — GetBriefing integration tests with real embedded docs
package knowledge

import (
	"strings"
	"testing"
)

func TestStore_GetBriefing_RealDocs(t *testing.T) {
	store := newTestStore(t)
	briefing, err := store.GetBriefing("php-nginx@8.4", []string{"postgresql@16"}, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if !strings.Contains(briefing, "Zerops Platform Model") {
		t.Error("briefing missing platform model content")
	}
	if !strings.Contains(briefing, "Zerops Rules") {
		t.Error("briefing missing rules content")
	}
	if !strings.Contains(briefing, "Zerops Grammar") {
		t.Error("briefing missing grammar content")
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

func TestStore_GetBriefing_LayerOrderRealDocs(t *testing.T) {
	store := newTestStore(t)
	briefing, err := store.GetBriefing("bun@1.2", []string{"postgresql@16"}, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	platformIdx := strings.Index(briefing, "Zerops Platform Model")
	rulesIdx := strings.Index(briefing, "Zerops Rules")
	grammarIdx := strings.Index(briefing, "Zerops Grammar")
	runtimeIdx := strings.Index(briefing, "Runtime-Specific: Bun")
	serviceIdx := strings.Index(briefing, "Service Cards")
	if platformIdx < 0 {
		t.Fatal("briefing missing Zerops Platform Model")
	}
	if rulesIdx < 0 {
		t.Fatal("briefing missing Zerops Rules")
	}
	if grammarIdx < 0 {
		t.Fatal("briefing missing Zerops Grammar")
	}
	if runtimeIdx < 0 {
		t.Fatal("briefing missing Runtime-Specific: Bun section")
	}
	if serviceIdx < 0 {
		t.Fatal("briefing missing Service Cards section")
	}
	// L0 platform model -> L1 rules -> L2 grammar -> L3 runtime -> L4 services
	if platformIdx >= rulesIdx {
		t.Errorf("platform model (pos %d) should come before rules (pos %d)", platformIdx, rulesIdx)
	}
	if rulesIdx >= grammarIdx {
		t.Errorf("rules (pos %d) should come before grammar (pos %d)", rulesIdx, grammarIdx)
	}
	if grammarIdx >= runtimeIdx {
		t.Errorf("grammar (pos %d) should come before runtime (pos %d)", grammarIdx, runtimeIdx)
	}
	if runtimeIdx >= serviceIdx {
		t.Errorf("runtime (pos %d) should come before services (pos %d)", runtimeIdx, serviceIdx)
	}
}
