package bundle

import (
	"maps"
	"testing"
)

// TestBuildGroupRecipe_CarriesProfileOverrides: a managed service's profile
// overrides are how it behaves — Valkey's eviction policy decides whether it
// is a cache or a queue — so each tier carries them wherever the profile it
// writes takes them: every Valkey profile takes maxmemory-policy, while a
// PostgreSQL's overrides belong to its custom profile only, which Small
// Production replaces with the production default.
func TestBuildGroupRecipe_CarriesProfileOverrides(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		typ       string
		profile   string
		overrides map[string]any
		want      map[string]map[string]string // tier → profileOverrides
	}{
		{
			name: "valkey: every tier", typ: "valkey@7.2", profile: "hobby",
			overrides: map[string]any{"maxmemory-policy": "noeviction"},
			want: map[string]map[string]string{
				"AI Agent": {"maxmemory-policy": "noeviction"}, "Stage": {"maxmemory-policy": "noeviction"},
				"Small Production": {"maxmemory-policy": "noeviction"},
			},
		},
		{
			name: "postgresql custom: the tiers that keep its profile", typ: "postgresql@17", profile: "custom",
			overrides: map[string]any{"max_connections": "200"},
			want: map[string]map[string]string{
				"AI Agent": {"max_connections": "200"}, "Stage": {"max_connections": "200"}, "Small Production": nil,
			},
		},
		{
			name: "no overrides: none written", typ: "valkey@7.2", profile: "hobby",
			want: map[string]map[string]string{"AI Agent": nil, "Stage": nil, "Small Production": nil},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := groupInputsFixture()
			in.ManagedServices = append(in.ManagedServices, ManagedServiceEntry{
				Hostname: "svc", Type: tt.typ, Profile: tt.profile, ProfileOverrides: tt.overrides,
			})
			layout, _, err := BuildGroupRecipe(in)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			for tier, want := range tt.want {
				got := scalarMap(mappingValue(serviceNode(t, tierBody(t, layout.Tiers, tier), "svc"), "profileOverrides"))
				if len(got) == 0 {
					got = nil
				}
				if !maps.Equal(got, want) {
					t.Errorf("%s: profileOverrides = %v, want %v", tier, got, want)
				}
			}
		})
	}
}
