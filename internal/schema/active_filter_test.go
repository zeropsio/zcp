package schema

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestFilterToActive_ServiceTypes(t *testing.T) {
	original := schemaWithServiceTypes([]string{
		"ubuntu/deno@1",
		"ubuntu/deno@1.45",
		"ubuntu/deno@2",
		"ubuntu/deno@2.0.0",
		"alpine/bun@1",
		"alpine/bun@1.3",
		"alpine/bun@1.3.9",
		"bun@1",
		"bun@1.3",
		"bun@1.3.9",
		"alpine/bun@canary",
		"bun@canary",
		"alpine/bun@can",
	})

	filtered := FilterToActive(original, []string{
		"ubuntu/deno@2.0.0",
		"alpine/bun@1.3.9",
		"alpine/bun@canary",
	})

	want := []string{
		"ubuntu/deno@2",
		"ubuntu/deno@2.0.0",
		"alpine/bun@1",
		"alpine/bun@1.3",
		"alpine/bun@1.3.9",
		"bun@1",
		"bun@1.3",
		"bun@1.3.9",
		"alpine/bun@canary",
		"bun@canary",
	}
	if !slices.Equal(filtered.ImportYml.ServiceTypes, want) {
		t.Fatalf("filtered service types = %v, want %v", filtered.ImportYml.ServiceTypes, want)
	}
	for _, dropped := range []string{"ubuntu/deno@1", "ubuntu/deno@1.45", "alpine/bun@can"} {
		if filtered.HasServiceType(dropped) {
			t.Fatalf("filtered schema still accepts inactive/non-expanded form %q", dropped)
		}
		if filtered.ImportYml.ServiceTypeSet()[dropped] {
			t.Fatalf("filtered serviceTypeSet still contains %q", dropped)
		}
	}
	if !original.HasServiceType("ubuntu/deno@1") {
		t.Fatal("FilterToActive mutated the input schema")
	}
}

func TestCache_ActiveFilter_DegradesVisiblyOnError(t *testing.T) {
	activeErr := errors.New("active status unavailable")
	cache := NewCache(time.Hour, "", func(context.Context) ([]string, error) {
		return nil, activeErr
	})
	cache.fetchSchemas = func(context.Context, string) (*Schemas, error) {
		return schemaWithServiceTypes([]string{"ubuntu/deno@1", "ubuntu/deno@2", "ubuntu/deno@2.0.0"}), nil
	}

	got := cache.Get(context.Background())
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if !got.HasServiceType("ubuntu/deno@1") {
		t.Fatal("active fetch failure must keep the unfiltered schema instead of panicking or dropping entries")
	}
	if !cache.ActiveStatusUnavailable() {
		t.Fatal("active fetch failure was not exposed via cache degrade flag")
	}
}

func TestCache_ActiveFilter_AppliesOnSuccess(t *testing.T) {
	cache := NewCache(time.Hour, "", func(context.Context) ([]string, error) {
		return []string{"ubuntu/deno@2.0.0"}, nil
	})
	cache.fetchSchemas = func(context.Context, string) (*Schemas, error) {
		return schemaWithServiceTypes([]string{
			"ubuntu/deno@1",
			"ubuntu/deno@1.45",
			"ubuntu/deno@2",
			"ubuntu/deno@2.0.0",
		}), nil
	}

	got := cache.Get(context.Background())
	if got.HasServiceType("ubuntu/deno@1") {
		t.Fatal("cache still accepts inactive Deno 1.x after active filter")
	}
	if !got.HasServiceType("ubuntu/deno@2") || !got.HasServiceType("ubuntu/deno@2.0.0") {
		t.Fatalf("cache dropped active Deno 2 family/concrete forms: %v", got.ImportYml.ServiceTypes)
	}
	if cache.ActiveStatusUnavailable() {
		t.Fatal("cache marked active status unavailable after successful active fetch")
	}
}

func schemaWithServiceTypes(types []string) *Schemas {
	return &Schemas{
		ZeropsYml: &ZeropsYmlSchema{
			BuildBases:          []string{"nodejs@22"},
			RunBases:            []string{"nodejs@22"},
			buildBaseSet:        baseNameSet([]string{"nodejs@22"}),
			runBaseSet:          makeStringSet([]string{"nodejs@22"}),
			buildBaseVersionSet: makeStringSet([]string{"nodejs@22"}),
		},
		ImportYml: &ImportYmlSchema{
			ServiceTypes:    slices.Clone(types),
			Modes:           []string{"NON_HA"},
			CorePackages:    []string{"LIGHT"},
			StoragePolicies: []string{"public-read"},
			serviceTypeSet:  makeStringSet(types),
		},
	}
}
