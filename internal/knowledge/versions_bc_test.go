// Tests for: ManagedBaseNames BC tolerance against post-Sunday-release
// composite mode-encoded managed type identifiers.
//
// Pre-fix the live API name `postgresql:single@18` was Cut on `@` into
// `postgresql:single`, so plan-side lookup of bare `postgresql@18` (after
// its own Cut → `postgresql`) missed and `validate.go::isManagedTypeWithLive`
// rejected a valid managed service. Both sides canonicalize via
// `topology.CanonicalBareForm` so the stored key is bare regardless of API
// shape.
package knowledge

import (
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestManagedBaseNames_CompositeShapes(t *testing.T) {
	t.Parallel()

	types := []platform.ServiceStackType{
		// Sunday-release: managed types come back as mode-encoded composite.
		{Name: "PostgreSQL", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "postgresql:single@18", Status: "ACTIVE"},
			{Name: "postgresql:ha@18", Status: "ACTIVE"},
			{Name: "postgresql:single@17", Status: "ACTIVE"},
		}},
		{Name: "Valkey", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "valkey:single@7.2", Status: "ACTIVE"},
			{Name: "valkey:ha@7.2", Status: "ACTIVE"},
		}},
		{Name: "MariaDB", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "mariadb:single@11", Status: "ACTIVE"},
		}},
		// Mixed: legacy bare-form managed still present for BC during rollout.
		{Name: "KeyDB", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "keydb@6", Status: "ACTIVE"},
		}},
	}

	result := ManagedBaseNames(types)

	// All composite mode-encoded names must canonicalize to bare base.
	wantBare := []string{"postgresql", "valkey", "mariadb", "keydb"}
	for _, base := range wantBare {
		if !result[base] {
			t.Errorf("expected %q in managed base names, got map: %v", base, result)
		}
	}

	// Critically: the mode-suffixed key MUST NOT appear — that was the bug.
	forbidden := []string{
		"postgresql:single",
		"postgresql:ha",
		"valkey:single",
		"valkey:ha",
		"mariadb:single",
	}
	for _, k := range forbidden {
		if result[k] {
			t.Errorf("mode-suffixed key %q must not appear in managed base names (bug regression)", k)
		}
	}
}

func TestManagedBaseNames_MixedBareAndComposite(t *testing.T) {
	t.Parallel()

	// During the BC window both shapes can land in the same response.
	types := []platform.ServiceStackType{
		{Name: "PostgreSQL", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "postgresql@16", Status: "ACTIVE"},        // legacy bare
			{Name: "postgresql:single@18", Status: "ACTIVE"}, // new composite
		}},
	}

	result := ManagedBaseNames(types)

	if !result["postgresql"] {
		t.Errorf("expected postgresql in managed base names regardless of shape, got: %v", result)
	}
	if result["postgresql:single"] {
		t.Error("mode-suffixed key must not appear when canonicalization is correct")
	}
}
