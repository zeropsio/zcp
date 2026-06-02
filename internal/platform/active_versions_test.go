package platform

import (
	"context"
	"slices"
	"testing"
)

func TestActiveServiceTypeVersions_FiltersActiveOnly(t *testing.T) {
	mock := NewMock().WithActiveServiceTypeVersions([]mockServiceTypeVersion{
		{Name: "alpine/bun@1.3.9", Status: "ACTIVE"},
		{Name: "ubuntu/deno@1.45.5", Status: "DISABLED"},
		{Name: "ubuntu/deno@2.0.0", Status: "ACTIVE"},
	})

	got, err := mock.ActiveServiceTypeVersions(context.Background())
	if err != nil {
		t.Fatalf("ActiveServiceTypeVersions: %v", err)
	}

	want := []string{"alpine/bun@1.3.9", "ubuntu/deno@2.0.0"}
	if !slices.Equal(got, want) {
		t.Fatalf("ActiveServiceTypeVersions = %v, want %v", got, want)
	}
	if slices.Contains(got, "ubuntu/deno@1.45.5") {
		t.Fatal("disabled Deno 1.x version leaked into active set")
	}
}
