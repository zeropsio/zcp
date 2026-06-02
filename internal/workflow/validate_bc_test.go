// Tests for: BC acceptance of legacy bare-type shape against post-Sunday-release
// composite catalog.
//
// Sunday-release 2026-05-18 moved Zerops upstream identifiers to OS-prefixed
// composite form for runtimes (`alpine/nodejs@22`) and mode-encoded composite
// form for managed deps (`postgresql:single@18`). The legacy split shape
// (`type: nodejs@22 + os: alpine`, `type: postgresql@18 + mode: NON_HA`) is
// still accepted by the Zerops API for BC; ZCP's plan validator accepts both
// shapes via type-equivalence (topology.TypesAreEquivalent), without
// rewriting plan.Type in place — recipe match comparisons rely on the
// agent-supplied shape staying intact.
package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
)

// compositeSchemas mirrors a post-Sunday-release composite-ONLY catalog:
// OS-prefixed runtimes (`alpine/nodejs@22`) and mode-encoded managed deps
// (`postgresql:single@18`), with NO bare forms — so the bootstrap validator's
// equivalence-aware HasServiceType must accept a legacy bare plan type against
// it. (The HasServiceType composite↔bare equivalence itself is unit-pinned in
// internal/schema/catalog_test.go; here we pin that bootstrap VALIDATION
// accepts both shapes without rewriting the agent's submitted type.)
var compositeSchemas = &schema.Schemas{
	ImportYml: &schema.ImportYmlSchema{
		ServiceTypes: []string{
			"alpine/nodejs@22", "ubuntu/nodejs@22",
			"alpine/php-nginx@8.4", "ubuntu/php-nginx@8.4",
			"alpine/bun@1.2",
			"postgresql:single@18", "postgresql:ha@18",
			"valkey:single@7.2",
		},
	},
}

// TestValidateBootstrapTargets_LegacyShapePassesValidation asserts a plan
// submitted with the legacy split shape (bare type + os + mode) validates
// against a composite-form catalog WITHOUT mutating the agent's submitted
// shape. Mutation would break the recipe-match check (recipe_override.go's
// findRuntimeSlot / findDepByType) since recipe import.yaml carries bare
// forms.
func TestValidateBootstrapTargets_LegacyShapePassesValidation(t *testing.T) {
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
	_, err := ValidateBootstrapTargets(targets, compositeSchemas, nil)
	if err != nil {
		t.Fatalf("legacy shape rejected: %v", err)
	}
	// Plan must NOT be rewritten — recipe-match downstream compares plan.Type
	// against recipe service's type (also bare).
	if targets[0].Runtime.Type != "nodejs@22" {
		t.Errorf("Runtime.Type unexpectedly rewritten: got %q, want %q (agent's submitted shape preserved)",
			targets[0].Runtime.Type, "nodejs@22")
	}
	if targets[0].Runtime.Os != "alpine" {
		t.Errorf("Runtime.Os unexpectedly cleared: got %q, want %q", targets[0].Runtime.Os, "alpine")
	}
	if targets[0].Dependencies[0].Type != "postgresql@18" {
		t.Errorf("Dependency.Type unexpectedly rewritten: got %q, want %q",
			targets[0].Dependencies[0].Type, "postgresql@18")
	}
}

// TestValidateBootstrapTargets_BareShapeAcceptedWithoutOsField confirms that
// a plan with bare types and NO `os:` sibling field (the shape recipe atom
// guidance teaches the agent) still validates against a composite catalog —
// the equivalence predicate matches regardless of OS-ambiguity.
func TestValidateBootstrapTargets_BareShapeAcceptedWithoutOsField(t *testing.T) {
	t.Parallel()
	targets := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{
				DevHostname: "appdev", Type: "php-nginx@8.4",
				BootstrapMode: "standard", ExplicitStage: "appstage",
			},
			Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@18", Mode: ModeNonHA, Resolution: "CREATE"},
			},
		},
	}
	_, err := ValidateBootstrapTargets(targets, compositeSchemas, nil)
	if err != nil {
		t.Fatalf("bare shape rejected: %v", err)
	}
	if targets[0].Runtime.Type != "php-nginx@8.4" {
		t.Errorf("Runtime.Type rewritten: got %q, want bare %q preserved",
			targets[0].Runtime.Type, "php-nginx@8.4")
	}
}

// TestIsManagedTypeWithLive_CompositeCatalog locks in the paired BC fix:
// when the schema carries composite mode-encoded managed names
// (`postgresql:single@18`) the live-managed map (built by
// schema.Schemas.ManagedBaseNames) stores the canonical bare base (`postgresql`).
// isManagedTypeWithLive then must also canonicalize the plan-side service
// type before consulting the map, regardless of which shape the agent
// submitted.
func TestIsManagedTypeWithLive_CompositeCatalog(t *testing.T) {
	t.Parallel()
	liveManaged := map[string]bool{
		"postgresql":     true,
		"valkey":         true,
		"object-storage": true,
	}
	tests := []struct {
		name    string
		svcType string
		want    bool
	}{
		// Bare — legacy clients.
		{"bare_managed", "postgresql@18", true},
		{"bare_managed_valkey", "valkey@7.2", true},
		// Composite mode-encoded — post-release shape.
		{"composite_single", "postgresql:single@18", true},
		{"composite_ha", "postgresql:ha@18", true},
		{"composite_valkey_single", "valkey:single@7.2", true},
		// Runtimes — not managed.
		{"composite_runtime", "alpine/nodejs@22", false},
		{"bare_runtime", "nodejs@22", false},
		// Unknown base — not managed.
		{"unknown", "rust@1.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isManagedTypeWithLive(tt.svcType, liveManaged)
			if got != tt.want {
				t.Errorf("isManagedTypeWithLive(%q) = %v, want %v",
					tt.svcType, got, tt.want)
			}
		})
	}
}

// TestIsManagedTypeWithLive_EmptyLiveFallsBackToStatic confirms the
// fallback path (when live map empty, e.g. catalog fetch failed)
// uses topology.IsManagedService which has its own canonicalization.
func TestIsManagedTypeWithLive_EmptyLiveFallsBackToStatic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		svcType string
		want    bool
	}{
		{"composite_managed_static", "postgresql:single@18", true},
		{"bare_managed_static", "postgresql@18", true},
		{"composite_runtime_static", "alpine/nodejs@22", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isManagedTypeWithLive(tt.svcType, nil)
			if got != tt.want {
				t.Errorf("isManagedTypeWithLive(%q, nil) = %v, want %v",
					tt.svcType, got, tt.want)
			}
		})
	}
}

// TestValidateBootstrapTargets_CompositeShapePassesThrough confirms the
// post-Sunday-release canonical shape also works (in case agent or recipe
// upgrades to composite ahead of the rest of the repo).
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
	_, err := ValidateBootstrapTargets(targets, compositeSchemas, nil)
	if err != nil {
		t.Fatalf("composite shape rejected: %v", err)
	}
	if targets[0].Runtime.Type != "alpine/nodejs@22" {
		t.Errorf("Runtime.Type changed unexpectedly: got %q", targets[0].Runtime.Type)
	}
}
