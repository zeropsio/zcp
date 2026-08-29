package schema

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestCache_WithoutActiveProvider_PublicSchemaServiceTypesRemainUnfiltered(t *testing.T) {
	t.Parallel()
	cache := NewCache(time.Hour, "", nil)
	want := []string{"ubuntu/deno@1", "ubuntu/deno@2", "local-storage:single@1"}
	cache.fetchSchemas = func(context.Context, string) (*Schemas, error) {
		return &Schemas{
			ZeropsYml: &ZeropsYmlSchema{
				BuildBases: []string{"nodejs@22"},
				RunBases:   []string{"nodejs@22"},
			},
			ImportYml: &ImportYmlSchema{
				ServiceTypes:    slices.Clone(want),
				Modes:           []string{"NON_HA"},
				CorePackages:    []string{"LIGHT"},
				StoragePolicies: []string{"public-read"},
			},
		}, nil
	}

	got := cache.Get(context.Background())
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if !slices.Equal(got.ImportYml.ServiceTypes, want) {
		t.Fatalf("cached service types = %v, want exact public enum %v", got.ImportYml.ServiceTypes, want)
	}
	if !cache.ActiveStatusUnavailable() {
		t.Fatal("cache without runtime ACTIVE provider did not expose degraded availability status")
	}
}
