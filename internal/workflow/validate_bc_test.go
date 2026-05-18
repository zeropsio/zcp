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
	{Name: "PHP-Nginx", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
		{Name: "alpine/php-nginx@8.4", Status: "ACTIVE"},
		{Name: "ubuntu/php-nginx@8.4", Status: "ACTIVE"},
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

func TestTypeAcceptedByCatalog_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		planType string
		want     bool
	}{
		// Composite — exact catalog match.
		{"composite_runtime_match", "alpine/nodejs@22", true},
		{"composite_runtime_ubuntu_match", "ubuntu/nodejs@22", true},
		{"composite_managed_match", "postgresql:single@18", true},
		{"composite_managed_ha_match", "postgresql:ha@18", true},
		// Legacy bare — accepted via equivalence even when catalog has only
		// composite forms. Multi-OS variants in the catalog (alpine + ubuntu)
		// both bare-canonicalize to `nodejs@22`, so the bare lookup matches.
		{"legacy_bare_runtime_multi_os_variants", "nodejs@22", true},
		{"legacy_bare_runtime_unique_os_variant", "bun@1.2", true},
		{"legacy_bare_php_nginx", "php-nginx@8.4", true},
		// Legacy bare managed — accepted via mode-suffix equivalence.
		{"legacy_bare_managed_with_both_modes", "postgresql@18", true},
		{"legacy_bare_managed_with_single_mode", "valkey@7.2", true},
		// Misses.
		{"unknown_runtime", "nodejs@99", false},
		{"unknown_base", "rust@1.0", false},
		{"unknown_managed", "postgresql@99", false},
		{"empty_type", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := typeAcceptedByCatalog(tt.planType, compositeLiveTypes)
			if got != tt.want {
				t.Errorf("typeAcceptedByCatalog(%q) = %v, want %v", tt.planType, got, tt.want)
			}
		})
	}
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
	_, err := ValidateBootstrapTargets(targets, compositeLiveTypes, nil)
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
	_, err := ValidateBootstrapTargets(targets, compositeLiveTypes, nil)
	if err != nil {
		t.Fatalf("bare shape rejected: %v", err)
	}
	if targets[0].Runtime.Type != "php-nginx@8.4" {
		t.Errorf("Runtime.Type rewritten: got %q, want bare %q preserved",
			targets[0].Runtime.Type, "php-nginx@8.4")
	}
}

// TestIsManagedTypeWithLive_CompositeCatalog locks in the paired BC fix:
// when the live API returns composite mode-encoded managed names
// (`postgresql:single@18`) the live-managed map (built by
// knowledge.ManagedBaseNames) stores the canonical bare base (`postgresql`).
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
	_, err := ValidateBootstrapTargets(targets, compositeLiveTypes, nil)
	if err != nil {
		t.Fatalf("composite shape rejected: %v", err)
	}
	if targets[0].Runtime.Type != "alpine/nodejs@22" {
		t.Errorf("Runtime.Type changed unexpectedly: got %q", targets[0].Runtime.Type)
	}
}
