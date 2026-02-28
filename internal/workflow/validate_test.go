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
		{"leading_digit", "3test", "must start with a letter"},
		{"all_digits", "123", "must start with a letter"},
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
		{Name: "Object Storage", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "object-storage", Status: "ACTIVE"},
		}},
	}

	tests := []struct {
		name        string
		services    []PlannedService
		liveTypes   []platform.ServiceStackType
		wantErr     string
		wantDefault []string          // hostnames that should have mode defaulted to NON_HA
		wantMode    map[string]string // hostname -> expected mode after validation
	}{
		{
			name: "valid_plan",
			services: []PlannedService{
				{Hostname: "appdev", Type: "bun@1.2"},
				{Hostname: "appstage", Type: "bun@1.2"},
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
			},
			liveTypes: liveTypes,
		},
		{
			name:      "empty_services",
			services:  []PlannedService{},
			liveTypes: liveTypes,
			wantErr:   "at least one service",
		},
		{
			name: "duplicate_hostname",
			services: []PlannedService{
				{Hostname: "app", Type: "bun@1.2"},
				{Hostname: "app", Type: "nodejs@22"},
			},
			liveTypes: liveTypes,
			wantErr:   "duplicate hostname",
		},
		{
			name: "invalid_hostname",
			services: []PlannedService{
				{Hostname: "my-app", Type: "bun@1.2"},
			},
			liveTypes: liveTypes,
			wantErr:   "invalid characters",
		},
		{
			name: "empty_type",
			services: []PlannedService{
				{Hostname: "app", Type: ""},
			},
			liveTypes: liveTypes,
			wantErr:   "empty type",
		},
		{
			name: "unknown_type_with_live",
			services: []PlannedService{
				{Hostname: "app", Type: "python@3.12"},
			},
			liveTypes: liveTypes,
			wantErr:   "not found in available",
		},
		{
			name: "managed_missing_mode",
			services: []PlannedService{
				{Hostname: "db", Type: "postgresql@16"},
			},
			liveTypes:   liveTypes,
			wantDefault: []string{"db"},
			wantMode:    map[string]string{"db": "NON_HA"},
		},
		{
			name: "managed_with_mode",
			services: []PlannedService{
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
			},
			liveTypes: liveTypes,
		},
		{
			name: "managed_ha_mode",
			services: []PlannedService{
				{Hostname: "db", Type: "postgresql@16", Mode: "HA"},
			},
			liveTypes: liveTypes,
		},
		{
			name: "managed_invalid_mode",
			services: []PlannedService{
				{Hostname: "db", Type: "postgresql@16", Mode: "INVALID"},
			},
			liveTypes: liveTypes,
			wantErr:   "must be HA or NON_HA",
		},
		{
			name: "nil_live_types_skips_type_check",
			services: []PlannedService{
				{Hostname: "app", Type: "unknown@99"},
			},
			liveTypes: nil,
		},
		{
			name: "runtime_service_no_mode_required",
			services: []PlannedService{
				{Hostname: "app", Type: "bun@1.2"},
			},
			liveTypes: liveTypes,
		},
		{
			name: "valkey_missing_mode_defaults",
			services: []PlannedService{
				{Hostname: "cache", Type: "valkey@7.2"},
			},
			liveTypes:   liveTypes,
			wantDefault: []string{"cache"},
			wantMode:    map[string]string{"cache": "NON_HA"},
		},
		{
			name: "shared_storage_with_mode",
			services: []PlannedService{
				{Hostname: "storage", Type: "shared-storage", Mode: "NON_HA"},
			},
			liveTypes: liveTypes,
		},
		{
			name: "object_storage_no_mode",
			services: []PlannedService{
				{Hostname: "storage", Type: "object-storage"},
			},
			liveTypes:   liveTypes,
			wantDefault: []string{"storage"},
			wantMode:    map[string]string{"storage": "NON_HA"},
		},
		{
			name: "object_storage_with_mode",
			services: []PlannedService{
				{Hostname: "storage", Type: "object-storage", Mode: "NON_HA"},
			},
			liveTypes: liveTypes,
		},
		{
			name: "all_defaulted",
			services: []PlannedService{
				{Hostname: "db", Type: "postgresql@16"},
				{Hostname: "cache", Type: "valkey@7.2"},
				{Hostname: "storage", Type: "object-storage"},
			},
			liveTypes:   liveTypes,
			wantDefault: []string{"db", "cache", "storage"},
			wantMode: map[string]string{
				"db":      "NON_HA",
				"cache":   "NON_HA",
				"storage": "NON_HA",
			},
		},
		{
			name: "batch_multiple_errors",
			services: []PlannedService{
				{Hostname: "my-app", Type: "bun@1.2"},
				{Hostname: "db", Type: ""},
				{Hostname: "cache", Type: "postgresql@16", Mode: "INVALID"},
			},
			liveTypes: liveTypes,
			wantErr:   "3 validation errors",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defaulted, err := ValidateServicePlan(tt.services, tt.liveTypes)
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
				return
			}

			// Check defaulted hostnames.
			if len(tt.wantDefault) != len(defaulted) {
				t.Errorf("defaulted: want %v, got %v", tt.wantDefault, defaulted)
			} else {
				for i, want := range tt.wantDefault {
					if defaulted[i] != want {
						t.Errorf("defaulted[%d]: want %q, got %q", i, want, defaulted[i])
					}
				}
			}

			// Check modes were actually set on services.
			for hostname, wantMode := range tt.wantMode {
				for _, svc := range tt.services {
					if svc.Hostname == hostname {
						if svc.Mode != wantMode {
							t.Errorf("service %q mode: want %q, got %q", hostname, wantMode, svc.Mode)
						}
					}
				}
			}
		})
	}
}
