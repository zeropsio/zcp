// Tests for: plans/analysis/ops.md § ops/context.go
package ops

import (
	"strings"
	"testing"
)

func TestGetContext_NonEmpty(t *testing.T) {
	t.Parallel()

	result := GetContext()
	if result == "" {
		t.Fatal("GetContext() returned empty string")
	}
}

func TestGetContext_ContainsCriticalSections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		section string
	}{
		{"critical_rules", "Critical Rules"},
		{"service_types", "Service Types"},
		{"overview", "Overview"},
		{"how_zerops_works", "How Zerops Works"},
		{"configuration", "Configuration"},
		{"defaults", "Defaults"},
	}

	result := GetContext()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(result, tt.section) {
				t.Errorf("GetContext() does not contain section %q", tt.section)
			}
		})
	}
}

func TestGetContext_TokenSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		min  int
		max  int
	}{
		// ~800-1200 tokens maps to roughly 3000-6000 chars
		{"char_range", 3000, 6000},
	}

	result := GetContext()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			length := len(result)
			if length < tt.min || length > tt.max {
				t.Errorf("GetContext() length = %d, want between %d and %d", length, tt.min, tt.max)
			}
		})
	}
}
