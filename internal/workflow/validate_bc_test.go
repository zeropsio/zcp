// Tests for: BC tolerance for legacy `type` + `os` / `mode` split shape.
//
// Sunday-release 2026-05-18 moved Zerops upstream identifiers to OS-prefixed
// composite form for runtimes (`alpine/nodejs@22`) and mode-encoded composite
// form for managed deps (`postgresql:single@18`). The legacy split shape
// (`type: nodejs@22 + os: alpine`, `type: postgresql@18 + mode: NON_HA`) is
// still accepted by the Zerops API for BC; ZCP's plan validator normalizes
// both shapes to the composite form so downstream consumers see one canonical
// identifier.
package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// compositeLiveTypes mirrors the post-Sunday-release live catalog: composite
// runtime identifiers (`alpine/nodejs@22`) and mode-encoded managed deps
// (`postgresql:single@18`). Multi-OS variants exist to exercise the
// disambiguation paths.
var compositeLiveTypes = []platform.ServiceStackType{
	{Name: "Node.js", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
		{Name: "alpine/nodejs@22", Status: "ACTIVE"},
		{Name: "ubuntu/nodejs@22", Status: "ACTIVE"},
	}},
	{Name: "Bun", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
		{Name: "alpine/bun@1.2", Status: "ACTIVE"},
	}},
	{Name: "PostgreSQL", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
		{Name: "postgresql:single@18", Status: "ACTIVE"},
		{Name: "postgresql:ha@18", Status: "ACTIVE"},
	}},
	{Name: "Valkey", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
		{Name: "valkey:single@7.2", Status: "ACTIVE"},
	}},
}

func TestResolveRuntimeType_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		rt        RuntimeTarget
		wantType  string
		wantFound bool
	}{
		{
			name:      "composite passes through",
			rt:        RuntimeTarget{Type: "alpine/nodejs@22"},
			wantType:  "alpine/nodejs@22",
			wantFound: true,
		},
		{
			name:      "legacy bare + os composes",
			rt:        RuntimeTarget{Type: "nodejs@22", Os: "alpine"},
			wantType:  "alpine/nodejs@22",
			wantFound: true,
		},
		{
			name:      "legacy bare + os case-sensitive miss",
			rt:        RuntimeTarget{Type: "nodejs@22", Os: "Alpine"},
			wantType:  "nodejs@22",
			wantFound: false,
		},
		{
			name:      "bare with unique OS variant resolves",
			rt:        RuntimeTarget{Type: "bun@1.2"},
			wantType:  "alpine/bun@1.2",
			wantFound: true,
		},
		{
			name:      "bare with multiple OS variants ambiguous — fails strict",
			rt:        RuntimeTarget{Type: "nodejs@22"},
			wantType:  "nodejs@22",
			wantFound: false,
		},
		{
			name:      "composite that isn't in catalog fails",
			rt:        RuntimeTarget{Type: "alpine/nodejs@99"},
			wantType:  "alpine/nodejs@99",
			wantFound: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotType, gotFound := resolveRuntimeType(tt.rt, compositeLiveTypes)
			if gotType != tt.wantType || gotFound != tt.wantFound {
				t.Errorf("resolveRuntimeType(%+v) = (%q, %v); want (%q, %v)",
					tt.rt, gotType, gotFound, tt.wantType, tt.wantFound)
			}
		})
	}
}

func TestResolveDepType_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		dep       Dependency
		wantType  string
		wantFound bool
	}{
		{
			name:      "composite passes through",
			dep:       Dependency{Type: "postgresql:single@18"},
			wantType:  "postgresql:single@18",
			wantFound: true,
		},
		{
			name:      "legacy bare + NON_HA composes to :single",
			dep:       Dependency{Type: "postgresql@18", Mode: ModeNonHA},
			wantType:  "postgresql:single@18",
			wantFound: true,
		},
		{
			name:      "legacy bare + HA composes to :ha",
			dep:       Dependency{Type: "postgresql@18", Mode: ModeHA},
			wantType:  "postgresql:ha@18",
			wantFound: true,
		},
		{
			name:      "legacy bare + lowercase mode normalizes via ToUpper",
			dep:       Dependency{Type: "postgresql@18", Mode: "non_ha"},
			wantType:  "postgresql:single@18",
			wantFound: true,
		},
		{
			name:      "bare + unknown mode falls through",
			dep:       Dependency{Type: "postgresql@18", Mode: "BOGUS"},
			wantType:  "postgresql@18",
			wantFound: false,
		},
		{
			name:      "bare with unique mode variant resolves",
			dep:       Dependency{Type: "valkey@7.2"},
			wantType:  "valkey:single@7.2",
			wantFound: true,
		},
		{
			name:      "bare with multiple mode variants ambiguous — fails strict",
			dep:       Dependency{Type: "postgresql@18"},
			wantType:  "postgresql@18",
			wantFound: false,
		},
		{
			name:      "bare without version fails",
			dep:       Dependency{Type: "postgresql", Mode: ModeNonHA},
			wantType:  "postgresql",
			wantFound: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotType, gotFound := resolveDepType(tt.dep, compositeLiveTypes)
			if gotType != tt.wantType || gotFound != tt.wantFound {
				t.Errorf("resolveDepType(%+v) = (%q, %v); want (%q, %v)",
					tt.dep, gotType, gotFound, tt.wantType, tt.wantFound)
			}
		})
	}
}

// TestValidateBootstrapTargets_LegacyShapeNormalizes confirms end-to-end that
// a plan submitted with the legacy split shape (bare type + os + mode)
// validates against a composite-form catalog and that the targets slice is
// rewritten in place to the canonical composite identifiers.
func TestValidateBootstrapTargets_LegacyShapeNormalizes(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{
				DevHostname: "appdev", Type: "nodejs@22", Os: "alpine",
				BootstrapMode: "standard", ExplicitStage: "appstage",
			},
			Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@18", Mode: ModeNonHA, Resolution: "CREATE"},
			},
		},
	}
	_, err := ValidateBootstrapTargets(targets, compositeLiveTypes, nil)
	if err != nil {
		t.Fatalf("legacy shape rejected: %v", err)
	}
	if targets[0].Runtime.Type != "alpine/nodejs@22" {
		t.Errorf("Runtime.Type not normalized: got %q, want %q", targets[0].Runtime.Type, "alpine/nodejs@22")
	}
	if targets[0].Runtime.Os != "" {
		t.Errorf("Runtime.Os not stripped after normalization: got %q", targets[0].Runtime.Os)
	}
	if targets[0].Dependencies[0].Type != "postgresql:single@18" {
		t.Errorf("Dependency.Type not normalized: got %q, want %q",
			targets[0].Dependencies[0].Type, "postgresql:single@18")
	}
	// Mode is preserved — still used by downstream managed-service gating.
	if targets[0].Dependencies[0].Mode != ModeNonHA {
		t.Errorf("Dependency.Mode unexpectedly cleared: got %q", targets[0].Dependencies[0].Mode)
	}
}

// TestValidateBootstrapTargets_CompositeShapePassesThrough confirms the
// post-Sunday-release canonical shape works without normalization side-effects.
func TestValidateBootstrapTargets_CompositeShapePassesThrough(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{
				DevHostname: "appdev", Type: "alpine/nodejs@22",
				BootstrapMode: "standard", ExplicitStage: "appstage",
			},
			Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql:single@18", Mode: ModeNonHA, Resolution: "CREATE"},
			},
		},
	}
	_, err := ValidateBootstrapTargets(targets, compositeLiveTypes, nil)
	if err != nil {
		t.Fatalf("composite shape rejected: %v", err)
	}
	if targets[0].Runtime.Type != "alpine/nodejs@22" {
		t.Errorf("Runtime.Type changed unexpectedly: got %q", targets[0].Runtime.Type)
	}
}
