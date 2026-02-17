// Tests for: design/context-first-delivery.md § Response-Driven Steering
package knowledge

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// testStackTypes returns realistic ServiceStackType fixtures.
func testStackTypes() []platform.ServiceStackType {
	return []platform.ServiceStackType{
		{
			Name:     "Bun",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "bun@1.1.34", Status: "ACTIVE"},
				{Name: "bun@1.2", Status: "ACTIVE"},
			},
		},
		{
			Name:     "Node.js",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "nodejs@18", Status: "ACTIVE"},
				{Name: "nodejs@20", Status: "ACTIVE"},
				{Name: "nodejs@22", Status: "ACTIVE"},
				{Name: "nodejs@16", Status: "DEPRECATED"},
			},
		},
		{
			Name:     "Go",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "go@1", Status: "ACTIVE"},
			},
		},
		{
			Name:     "PostgreSQL",
			Category: "STANDARD",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "postgresql@16", Status: "ACTIVE"},
			},
		},
		{
			Name:     "Valkey",
			Category: "STANDARD",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "valkey@7.2", Status: "ACTIVE"},
			},
		},
		{
			Name:     "MariaDB",
			Category: "STANDARD",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "mariadb@10.6", Status: "ACTIVE"},
				{Name: "mariadb@10.11", Status: "ACTIVE"},
				{Name: "mariadb@11", Status: "ACTIVE"},
			},
		},
		{
			Name:     "Object Storage",
			Category: "OBJECT_STORAGE",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "object-storage", Status: "ACTIVE"},
			},
		},
		// Hidden category — should not appear in output.
		{
			Name:     "Core",
			Category: "CORE",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "core@1", Status: "ACTIVE"},
			},
		},
	}
}

// --- FormatStackList Tests ---

func TestFormatStackList_Groups(t *testing.T) {
	t.Parallel()
	types := testStackTypes()

	result := FormatStackList(types)

	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "Available Service Stacks") {
		t.Error("missing header")
	}
	if !strings.Contains(result, "Runtime:") {
		t.Error("missing Runtime category")
	}
	if !strings.Contains(result, "Managed:") {
		t.Error("missing Managed category")
	}
	// Verify compact notation
	if !strings.Contains(result, "nodejs@{18,20,22}") {
		t.Errorf("expected compact notation for nodejs, got: %s", result)
	}
	// Verify hidden categories excluded
	if strings.Contains(result, "Core") || strings.Contains(result, "core@1") {
		t.Error("hidden CORE category should not appear")
	}
}

func TestFormatStackList_Empty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		types []platform.ServiceStackType
	}{
		{"nil", nil},
		{"empty", []platform.ServiceStackType{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := FormatStackList(tt.types)
			if result != "" {
				t.Errorf("expected empty string, got: %q", result)
			}
		})
	}
}

// --- FormatVersionCheck Tests ---

func TestFormatVersionCheck_AllValid(t *testing.T) {
	t.Parallel()
	types := testStackTypes()

	result := FormatVersionCheck("bun@1.2", []string{"postgresql@16"}, types)

	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "Version Check") {
		t.Error("missing header")
	}
	// All valid — should have checkmarks
	if !strings.Contains(result, "\u2713") {
		t.Error("expected checkmark for valid types")
	}
	// No warnings
	if strings.Contains(result, "\u26a0") {
		t.Error("expected no warnings for valid types")
	}
}

func TestFormatVersionCheck_InvalidVersion(t *testing.T) {
	t.Parallel()
	types := testStackTypes()

	result := FormatVersionCheck("bun@1", []string{"postgresql@16"}, types)

	if result == "" {
		t.Fatal("expected non-empty result")
	}
	// Warning for bun@1 (not a valid version)
	if !strings.Contains(result, "\u26a0") {
		t.Error("expected warning for invalid version bun@1")
	}
	// Should suggest valid versions
	if !strings.Contains(result, "bun@1.2") || !strings.Contains(result, "bun@1.1.34") {
		t.Errorf("expected suggestion of valid bun versions, got: %s", result)
	}
}

func TestFormatVersionCheck_UnknownBase(t *testing.T) {
	t.Parallel()
	types := testStackTypes()

	result := FormatVersionCheck("ruby@3", nil, types)

	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "\u26a0") {
		t.Error("expected warning for unknown base type ruby")
	}
}

func TestFormatVersionCheck_Empty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		types []platform.ServiceStackType
	}{
		{"nil", nil},
		{"empty", []platform.ServiceStackType{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := FormatVersionCheck("bun@1.2", []string{"postgresql@16"}, tt.types)
			if result != "" {
				t.Errorf("expected empty string, got: %q", result)
			}
		})
	}
}

func TestFormatVersionCheck_BareNameNormalized(t *testing.T) {
	t.Parallel()
	types := testStackTypes()

	// "valkey" without version — should normalize to latest available and pass.
	result := FormatVersionCheck("nodejs@22", []string{"valkey"}, types)

	if result == "" {
		t.Fatal("expected non-empty result")
	}
	// Should have checkmark for normalized valkey (not a warning).
	if strings.Contains(result, "\u26a0") && strings.Contains(result, "valkey") {
		t.Errorf("bare 'valkey' should normalize to valkey@7.2 and pass, got: %s", result)
	}
	if !strings.Contains(result, "\u2713") {
		t.Error("expected checkmarks for valid types")
	}
}

func TestFormatVersionCheck_BareRuntimeNormalized(t *testing.T) {
	t.Parallel()
	types := testStackTypes()

	// "go" without version — should normalize to go@1.
	result := FormatVersionCheck("go", nil, types)

	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if strings.Contains(result, "\u26a0") {
		t.Errorf("bare 'go' should normalize to go@1 and pass, got: %s", result)
	}
}

// --- ValidateServiceTypes Tests ---

func TestValidateServiceTypes_Valid(t *testing.T) {
	t.Parallel()
	types := testStackTypes()
	services := []map[string]any{
		{"hostname": "api", "type": "nodejs@22"},
		{"hostname": "db", "type": "postgresql@16", "mode": "NON_HA"},
	}

	warnings := ValidateServiceTypes(services, types)

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got: %v", warnings)
	}
}

func TestValidateServiceTypes_InvalidType(t *testing.T) {
	t.Parallel()
	types := testStackTypes()
	services := []map[string]any{
		{"hostname": "api", "type": "ruby@3.2"},
	}

	warnings := ValidateServiceTypes(services, types)

	if len(warnings) == 0 {
		t.Fatal("expected warnings for invalid type")
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "ruby@3.2") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning mentioning ruby@3.2, got: %v", warnings)
	}
}

func TestValidateServiceTypes_MissingMode(t *testing.T) {
	t.Parallel()
	types := testStackTypes()
	services := []map[string]any{
		{"hostname": "db", "type": "postgresql@16"},
	}

	warnings := ValidateServiceTypes(services, types)

	if len(warnings) == 0 {
		t.Fatal("expected warning for missing mode on postgresql")
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "mode") && strings.Contains(w, "db") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about missing mode for db, got: %v", warnings)
	}
}

func TestValidateServiceTypes_NoTypes(t *testing.T) {
	t.Parallel()

	services := []map[string]any{
		{"hostname": "api", "type": "ruby@3.2"},
	}

	tests := []struct {
		name  string
		types []platform.ServiceStackType
	}{
		{"nil", nil},
		{"empty", []platform.ServiceStackType{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			warnings := ValidateServiceTypes(services, tt.types)
			if warnings != nil {
				t.Errorf("expected nil warnings, got: %v", warnings)
			}
		})
	}
}

// --- FormatServiceStacks Tests ---

func TestFormatServiceStacks_Empty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		types []platform.ServiceStackType
	}{
		{"nil", nil},
		{"empty", []platform.ServiceStackType{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if result := FormatServiceStacks(tt.types); result != "" {
				t.Errorf("expected empty string, got: %q", result)
			}
		})
	}
}

func TestFormatServiceStacks_BuildRunCrossReference(t *testing.T) {
	t.Parallel()

	types := []platform.ServiceStackType{
		{Name: "Golang", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
			{Name: "go@1", Status: "ACTIVE"},
		}},
		{Name: "zbuild Golang", Category: "BUILD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "go@1", Status: "ACTIVE"},
		}},
		{Name: "Nginx static", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
			{Name: "nginx@1.22", Status: "ACTIVE"},
		}},
		{Name: "PostgreSQL", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "postgresql@16", Status: "ACTIVE"},
		}},
	}

	result := FormatServiceStacks(types)

	if !strings.Contains(result, "go@1 [B]") {
		t.Error("Golang should show [B] (has BUILD counterpart)")
	}
	if strings.Contains(result, "nginx@1.22 [B]") {
		t.Error("Nginx should not show [B] (no BUILD counterpart)")
	}
	if strings.Contains(result, "postgresql@16 [B]") {
		t.Error("PostgreSQL should not show [B] (managed service)")
	}
}

func TestFormatServiceStacks_UnmatchedBuildVersions(t *testing.T) {
	t.Parallel()

	types := []platform.ServiceStackType{
		{Name: "PHP+Nginx", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
			{Name: "php-nginx@8.4", Status: "ACTIVE"},
		}},
		{Name: "zbuild PHP", Category: "BUILD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "php@8.1", Status: "ACTIVE"},
			{Name: "php@8.3", Status: "ACTIVE"},
		}},
		{Name: "Golang", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
			{Name: "go@1", Status: "ACTIVE"},
		}},
		{Name: "zbuild Golang", Category: "BUILD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "go@1", Status: "ACTIVE"},
		}},
	}

	result := FormatServiceStacks(types)

	if !strings.Contains(result, "Build-only:") {
		t.Error("should have Build-only section for unmatched PHP build versions")
	}
	if !strings.Contains(result, "php@{8.1,8.3}") {
		t.Error("should show php build versions in compact brace notation")
	}
	if !strings.Contains(result, "go@1 [B]") {
		t.Error("Golang should show [B]")
	}
}

func TestFormatServiceStacks_CategoryOrdering(t *testing.T) {
	t.Parallel()

	types := []platform.ServiceStackType{
		{Name: "S3", Category: "OBJECT_STORAGE", Versions: []platform.ServiceStackTypeVersion{
			{Name: "s3@1", Status: "ACTIVE"},
		}},
		{Name: "PostgreSQL", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "postgresql@16", Status: "ACTIVE"},
		}},
		{Name: "Shared NFS", Category: "SHARED_STORAGE", Versions: []platform.ServiceStackTypeVersion{
			{Name: "nfs@1", Status: "ACTIVE"},
		}},
		{Name: "Node.js", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
			{Name: "nodejs@22", Status: "ACTIVE"},
		}},
	}

	result := FormatServiceStacks(types)

	runtimeIdx := strings.Index(result, "Runtime:")
	managedIdx := strings.Index(result, "Managed:")
	sharedIdx := strings.Index(result, "Shared storage:")
	objectIdx := strings.Index(result, "Object storage:")

	if runtimeIdx < 0 || managedIdx < 0 || sharedIdx < 0 || objectIdx < 0 {
		t.Fatalf("missing category sections: runtime=%d, managed=%d, shared=%d, object=%d",
			runtimeIdx, managedIdx, sharedIdx, objectIdx)
	}
	if runtimeIdx >= managedIdx {
		t.Errorf("Runtime (%d) should appear before Managed (%d)", runtimeIdx, managedIdx)
	}
	if managedIdx >= sharedIdx {
		t.Errorf("Managed (%d) should appear before Shared storage (%d)", managedIdx, sharedIdx)
	}
	if sharedIdx >= objectIdx {
		t.Errorf("Shared storage (%d) should appear before Object storage (%d)", sharedIdx, objectIdx)
	}
}

func TestFormatServiceStacks_FiltersHiddenCategories(t *testing.T) {
	t.Parallel()

	types := []platform.ServiceStackType{
		{Name: "Node.js", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
			{Name: "nodejs@22", Status: "ACTIVE"},
		}},
		{Name: "Internal Tool", Category: "INTERNAL", Versions: []platform.ServiceStackTypeVersion{
			{Name: "internal@1", Status: "ACTIVE"},
		}},
		{Name: "Core", Category: "CORE", Versions: []platform.ServiceStackTypeVersion{
			{Name: "core@1", Status: "ACTIVE"},
		}},
	}

	result := FormatServiceStacks(types)

	if !strings.Contains(result, "nodejs@22") {
		t.Error("should contain USER category types")
	}
	if strings.Contains(result, "internal@1") {
		t.Error("should filter out INTERNAL category")
	}
	if strings.Contains(result, "core@1") {
		t.Error("should filter out CORE category")
	}
}

func TestFormatServiceStacks_OnlyHiddenCategories(t *testing.T) {
	t.Parallel()

	types := []platform.ServiceStackType{
		{Name: "Core", Category: "CORE", Versions: []platform.ServiceStackTypeVersion{
			{Name: "core", Status: "ACTIVE"},
		}},
	}

	if result := FormatServiceStacks(types); result != "" {
		t.Errorf("expected empty string for only hidden categories, got: %q", result)
	}
}

func TestCompactVersionGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		versions []string
		want     string
	}{
		{"single", []string{"nodejs@22"}, "nodejs@22"},
		{"single_bare", []string{"static"}, "static"},
		{"multi_same_prefix", []string{"nodejs@18", "nodejs@20", "nodejs@22"}, "nodejs@{18,20,22}"},
		{"multi_bare", []string{"static", "runtime"}, "static, runtime"},
		{"multi_mixed_prefix", []string{"nodejs@22", "python@3.14"}, "nodejs@22, python@3.14"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := compactVersionGroup(tt.versions)
			if got != tt.want {
				t.Errorf("compactVersionGroup(%v) = %q, want %q", tt.versions, got, tt.want)
			}
		})
	}
}
