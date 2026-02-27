// Tests for: workflow plan validation — hostname regex, service plan structure.
package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestValidatePlanHostname(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		hostname string
		wantErr  string // empty = no error expected
	}{
		{"valid_lowercase", "appdev", ""},
		{"valid_with_digits", "app1dev2", ""},
		{"single_char", "a", ""},
		{"max_length_25", strings.Repeat("a", 25), ""},
		{"has_hyphen", "my-app", "invalid characters"},
		{"has_underscore", "my_app", "invalid characters"},
		{"has_uppercase", "AppDev", "invalid characters"},
		{"too_long", strings.Repeat("a", 26), "exceeds 25"},
		{"empty", "", "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePlanHostname(tt.hostname)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestValidateServicePlan(t *testing.T) {
	t.Parallel()

	liveTypes := []platform.ServiceStackType{
		{Name: "Node.js", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
			{Name: "nodejs@22", Status: "ACTIVE"},
		}},
		{Name: "Bun", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
			{Name: "bun@1.2", Status: "ACTIVE"},
		}},
		{Name: "PostgreSQL", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "postgresql@16", Status: "ACTIVE"},
		}},
		{Name: "Valkey", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "valkey@7.2", Status: "ACTIVE"},
		}},
		{Name: "Shared Storage", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "shared-storage", Status: "ACTIVE"},
		}},
	}

	tests := []struct {
		name      string
		services  []PlannedService
		liveTypes []platform.ServiceStackType
		wantErr   string
	}{
		{
			"valid_plan",
			[]PlannedService{
				{Hostname: "appdev", Type: "bun@1.2"},
				{Hostname: "appstage", Type: "bun@1.2"},
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
			},
			liveTypes,
			"",
		},
		{
			"empty_services",
			[]PlannedService{},
			liveTypes,
			"at least one service",
		},
		{
			"duplicate_hostname",
			[]PlannedService{
				{Hostname: "app", Type: "bun@1.2"},
				{Hostname: "app", Type: "nodejs@22"},
			},
			liveTypes,
			"duplicate hostname",
		},
		{
			"invalid_hostname",
			[]PlannedService{
				{Hostname: "my-app", Type: "bun@1.2"},
			},
			liveTypes,
			"invalid characters",
		},
		{
			"empty_type",
			[]PlannedService{
				{Hostname: "app", Type: ""},
			},
			liveTypes,
			"empty type",
		},
		{
			"unknown_type_with_live",
			[]PlannedService{
				{Hostname: "app", Type: "python@3.12"},
			},
			liveTypes,
			"not found in available",
		},
		{
			"managed_missing_mode",
			[]PlannedService{
				{Hostname: "db", Type: "postgresql@16"},
			},
			liveTypes,
			"requires mode",
		},
		{
			"managed_with_mode",
			[]PlannedService{
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
			},
			liveTypes,
			"",
		},
		{
			"managed_ha_mode",
			[]PlannedService{
				{Hostname: "db", Type: "postgresql@16", Mode: "HA"},
			},
			liveTypes,
			"",
		},
		{
			"managed_invalid_mode",
			[]PlannedService{
				{Hostname: "db", Type: "postgresql@16", Mode: "INVALID"},
			},
			liveTypes,
			"must be HA or NON_HA",
		},
		{
			"nil_live_types_skips_type_check",
			[]PlannedService{
				{Hostname: "app", Type: "unknown@99"},
			},
			nil,
			"",
		},
		{
			"runtime_service_no_mode_required",
			[]PlannedService{
				{Hostname: "app", Type: "bun@1.2"},
			},
			liveTypes,
			"",
		},
		{
			"valkey_requires_mode",
			[]PlannedService{
				{Hostname: "cache", Type: "valkey@7.2"},
			},
			liveTypes,
			"requires mode",
		},
		{
			"shared_storage_requires_mode",
			[]PlannedService{
				{Hostname: "storage", Type: "shared-storage", Mode: "NON_HA"},
			},
			liveTypes,
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateServicePlan(tt.services, tt.liveTypes)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}
