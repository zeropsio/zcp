// Tests for: workflow_checks.go::checkServiceType type-equivalence BC.
//
// Sunday-release 2026-05-18 moved Zerops upstream identifiers to composite
// form (`alpine/php-nginx@8.4`, `postgresql:single@18`). Provision checks
// compare the plan-side `expectedType` (which may be the legacy bare form
// the recipe atom teaches the agent to use) against the live-side
// `ServiceStackTypeVersionName` (now composite). Strict byte-equality
// rejects every legacy-shape plan against a composite live state —
// `topology.TypesAreEquivalent` accepts both shapes.
package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestCheckServiceType_TypesAreEquivalent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		expectedType string
		actualType   string
		wantFail     bool
	}{
		// Identity — passes byte-equality (and equivalence).
		{
			name:         "exact_byte_match",
			expectedType: "alpine/php-nginx@8.4",
			actualType:   "alpine/php-nginx@8.4",
			wantFail:     false,
		},
		// Legacy bare plan vs composite live — BC acceptance.
		{
			name:         "bare_plan_composite_live_runtime",
			expectedType: "php-nginx@8.4",
			actualType:   "alpine/php-nginx@8.4",
			wantFail:     false,
		},
		{
			name:         "bare_plan_composite_live_managed",
			expectedType: "postgresql@18",
			actualType:   "postgresql:single@18",
			wantFail:     false,
		},
		// Composite plan vs bare live — reverse direction also accepts.
		{
			name:         "composite_plan_bare_live",
			expectedType: "alpine/nodejs@22",
			actualType:   "nodejs@22",
			wantFail:     false,
		},
		// Empty actual → skip (older API responses with no type info).
		{
			name:         "empty_actual_skips",
			expectedType: "alpine/php-nginx@8.4",
			actualType:   "",
			wantFail:     false,
		},
		// Genuine type mismatch — must still fail.
		{
			name:         "different_runtime_fails",
			expectedType: "nodejs@22",
			actualType:   "alpine/bun@1.2",
			wantFail:     true,
		},
		{
			name:         "different_version_fails",
			expectedType: "nodejs@22",
			actualType:   "alpine/nodejs@24",
			wantFail:     true,
		},
		{
			name:         "different_mode_managed_does_not_match_bare",
			expectedType: "postgresql:ha@18",
			actualType:   "postgresql:single@18",
			wantFail:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svcMap := map[string]platform.ServiceStack{
				"svc": {
					Name: "svc",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName: tt.actualType,
					},
				},
			}
			got := checkServiceType(svcMap, "svc", tt.expectedType)
			if tt.wantFail {
				if len(got) != 1 || got[0].Status != statusFail {
					t.Errorf("expected failed check, got %+v", got)
				}
			} else {
				if len(got) != 0 {
					t.Errorf("expected no checks (equivalence accepts), got %+v", got)
				}
			}
		})
	}
}

// TestCheckServiceType_PlatformResolution pins P1: a planned version-family
// SELECTOR that the platform resolved to a concrete patch is ACCEPTED (and
// reported), while a genuine same-base concrete mismatch still fails.
func TestCheckServiceType_PlatformResolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		expectedType     string
		actualType       string
		wantResolvedPass bool // accepted as a resolution → pass-check with "resolved" detail
		wantFail         bool
	}{
		// Version-family selector → platform-resolved concrete (the F1 cases).
		{"go_major_family", "go@1", "ubuntu/go@1.22", true, false},
		{"bun_minor_family", "bun@1.3", "alpine/bun@1.3.9", true, false},
		{"elixir_minor_family", "elixir@1.16", "ubuntu/elixir@1.16.2", true, false},
		{"latest_rolling", "nodejs@latest", "ubuntu/nodejs@24", true, false},
		// Genuine mismatch — must STILL fail (not turned into a pass).
		{"same_base_concrete_mismatch", "nodejs@22", "ubuntu/nodejs@24", false, true},
		{"different_base", "nodejs@22", "alpine/bun@1.3.9", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svcMap := map[string]platform.ServiceStack{
				"svc": {Name: "svc", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: tt.actualType}},
			}
			got := checkServiceType(svcMap, "svc", tt.expectedType)
			switch {
			case tt.wantFail:
				if len(got) != 1 || got[0].Status != statusFail {
					t.Errorf("expected fail, got %+v", got)
				}
			case tt.wantResolvedPass:
				if len(got) != 1 || got[0].Status != statusPass || !strings.Contains(got[0].Detail, "resolved to") {
					t.Errorf("expected resolution pass-check with detail, got %+v", got)
				}
			}
		})
	}
}
