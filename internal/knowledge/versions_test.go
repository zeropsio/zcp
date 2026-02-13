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
