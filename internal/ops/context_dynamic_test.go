// Tests for: context.go — GetContext dynamic stacks and version helpers.

package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestGetContext_WithDynamicStacks(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{
			Name:     "Node.js",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "nodejs@22", IsBuild: false, Status: "ACTIVE"},
				{Name: "nodejs@20", IsBuild: false, Status: "ACTIVE"},
			},
		},
		{
			Name:     "zbuild Node.js",
			Category: "BUILD",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "nodejs@20", IsBuild: true, Status: "ACTIVE"},
			},
		},
		{
			Name:     "PostgreSQL",
			Category: "STANDARD",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "postgresql@16", IsBuild: false, Status: "ACTIVE"},
			},
		},
	})
	cache := NewStackTypeCache(0)

	result := GetContext(context.Background(), mock, cache)

	tests := []struct {
		name     string
		contains string
	}{
		{"has_dynamic_header", "Service Stacks (live)"},
		{"has_runtime_category", "Runtime:"},
		{"has_managed_category", "Managed:"},
		{"has_nodejs_version", "nodejs@"},
		{"has_pg_version", "postgresql@16"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(result, tt.contains) {
				t.Errorf("GetContext() does not contain %q", tt.contains)
			}
		})
	}
}

func TestGetContext_BuildRunCrossReference(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{
			Name:     "Golang",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "go@1", IsBuild: false, Status: "ACTIVE"},
			},
		},
		{
			Name:     "zbuild Golang",
			Category: "BUILD",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "go@1", IsBuild: true, Status: "ACTIVE"},
			},
		},
		{
			Name:     "Nginx static",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "nginx@1.22", IsBuild: false, Status: "ACTIVE"},
			},
		},
		{
			Name:     "PostgreSQL",
			Category: "STANDARD",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "postgresql@16", IsBuild: false, Status: "ACTIVE"},
			},
		},
	})
	cache := NewStackTypeCache(0)

	result := GetContext(context.Background(), mock, cache)

	// Go has a BUILD counterpart → marked [B]
	if !strings.Contains(result, "go@1 [B]") {
		t.Error("Golang should show [B] (has BUILD counterpart)")
	}
	// Nginx has no BUILD counterpart → no [B]
	if strings.Contains(result, "nginx@1.22 [B]") {
		t.Error("Nginx should not show [B] (no BUILD counterpart)")
	}
	if !strings.Contains(result, "nginx@1.22") {
		t.Error("Nginx should be present")
	}
	// PostgreSQL has no BUILD counterpart → no [B]
	if strings.Contains(result, "postgresql@16 [B]") {
		t.Error("PostgreSQL should not show [B] (managed service)")
	}
	if !strings.Contains(result, "postgresql@16") {
		t.Error("PostgreSQL should be present")
	}
}

func TestGetContext_UnmatchedBuildVersions(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{
			Name:     "PHP+Nginx",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "php-nginx@8.4", IsBuild: false, Status: "ACTIVE"},
			},
		},
		{
			Name:     "zbuild PHP",
			Category: "BUILD",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "php@8.1", IsBuild: true, Status: "ACTIVE"},
				{Name: "php@8.3", IsBuild: true, Status: "ACTIVE"},
			},
		},
		{
			Name:     "Golang",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "go@1", IsBuild: false, Status: "ACTIVE"},
			},
		},
		{
			Name:     "zbuild Golang",
			Category: "BUILD",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "go@1", IsBuild: true, Status: "ACTIVE"},
			},
		},
	})
	cache := NewStackTypeCache(0)

	result := GetContext(context.Background(), mock, cache)

	// PHP build bases don't match run bases → shown in "Build-only"
	if !strings.Contains(result, "Build-only:") {
		t.Error("should have Build-only section for unmatched PHP build versions")
	}
	if !strings.Contains(result, "php@{8.1,8.3}") {
		t.Error("should show php build versions in compact brace notation")
	}
	// Go build version matches run version → marked [B] in main section
	if !strings.Contains(result, "go@1 [B]") {
		t.Error("Golang should show [B]")
	}
	// "zbuild" prefix should be stripped in build-only section
	if strings.Contains(result, "zbuild") {
		t.Error("should strip 'zbuild ' prefix from build-only names")
	}
}

func TestGetContext_FiltersInternalCategories(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{Name: "Node.js", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
			{Name: "nodejs@22", IsBuild: false, Status: "ACTIVE"},
		}},
		{Name: "Internal Tool", Category: "INTERNAL", Versions: []platform.ServiceStackTypeVersion{
			{Name: "internal@1", IsBuild: false, Status: "ACTIVE"},
		}},
		{Name: "zbuild Node.js", Category: "BUILD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "nodejs@20", IsBuild: true, Status: "ACTIVE"},
		}},
		{Name: "Prepare Runtime", Category: "PREPARE_RUNTIME", Versions: []platform.ServiceStackTypeVersion{
			{Name: "prep@1", IsBuild: false, Status: "ACTIVE"},
		}},
		{Name: "HTTP Balancer", Category: "HTTP_L7_BALANCER", Versions: []platform.ServiceStackTypeVersion{
			{Name: "lb@1", IsBuild: false, Status: "ACTIVE"},
		}},
		{Name: "Core", Category: "CORE", Versions: []platform.ServiceStackTypeVersion{
			{Name: "core@1", IsBuild: false, Status: "ACTIVE"},
		}},
	})
	cache := NewStackTypeCache(0)

	result := GetContext(context.Background(), mock, cache)

	if !strings.Contains(result, "nodejs@22") {
		t.Error("should contain USER category types")
	}
	if strings.Contains(result, "internal@1") {
		t.Error("should filter out INTERNAL category")
	}
	if strings.Contains(result, "zbuild") {
		t.Error("should filter out BUILD category from display")
	}
	if strings.Contains(result, "prep@1") {
		t.Error("should filter out PREPARE_RUNTIME category")
	}
	if strings.Contains(result, "lb@1") {
		t.Error("should filter out HTTP_L7_BALANCER category")
	}
	if strings.Contains(result, "core@1") {
		t.Error("should filter out CORE category")
	}
}

func TestGetContext_FiltersDisabledVersions(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{Name: "Node.js", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
			{Name: "nodejs@22", IsBuild: false, Status: "ACTIVE"},
			{Name: "nodejs@18", IsBuild: false, Status: "DISABLED"},
		}},
	})
	cache := NewStackTypeCache(0)

	result := GetContext(context.Background(), mock, cache)

	if !strings.Contains(result, "nodejs@22") {
		t.Error("should contain ACTIVE version nodejs@22")
	}
	if strings.Contains(result, "nodejs@18") {
		t.Error("should filter out DISABLED version nodejs@18")
	}
}

func TestGetContext_AllVersionsDisabled(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{Name: "Node.js", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
			{Name: "nodejs@18", IsBuild: false, Status: "DISABLED"},
			{Name: "nodejs@16", IsBuild: false, Status: "DISABLED"},
		}},
		{Name: "PostgreSQL", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "postgresql@16", IsBuild: false, Status: "ACTIVE"},
		}},
	})
	cache := NewStackTypeCache(0)

	result := GetContext(context.Background(), mock, cache)

	// Node.js has all disabled versions → should not appear
	if strings.Contains(result, "nodejs") {
		t.Error("type with all DISABLED versions should not appear")
	}
	// PostgreSQL has ACTIVE version → should appear
	if !strings.Contains(result, "postgresql@16") {
		t.Error("type with ACTIVE versions should appear")
	}
}

func TestGetContext_CategoryOrdering(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		// Intentionally in reverse order to test sorting.
		{Name: "S3", Category: "OBJECT_STORAGE", Versions: []platform.ServiceStackTypeVersion{
			{Name: "s3@1", IsBuild: false, Status: "ACTIVE"},
		}},
		{Name: "PostgreSQL", Category: "STANDARD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "postgresql@16", IsBuild: false, Status: "ACTIVE"},
		}},
		{Name: "Shared NFS", Category: "SHARED_STORAGE", Versions: []platform.ServiceStackTypeVersion{
			{Name: "nfs@1", IsBuild: false, Status: "ACTIVE"},
		}},
		{Name: "Node.js", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
			{Name: "nodejs@22", IsBuild: false, Status: "ACTIVE"},
		}},
	})
	cache := NewStackTypeCache(0)

	result := GetContext(context.Background(), mock, cache)

	// Categories must appear in defined order: USER, STANDARD, SHARED_STORAGE, OBJECT_STORAGE.
	runtimeIdx := strings.Index(result, "Runtime:")
	managedIdx := strings.Index(result, "Managed:")
	sharedIdx := strings.Index(result, "Shared storage:")
	objectIdx := strings.Index(result, "Object storage:")

	if runtimeIdx < 0 || managedIdx < 0 || sharedIdx < 0 || objectIdx < 0 {
		t.Fatalf("missing category sections: runtime=%d, managed=%d, shared=%d, object=%d",
			runtimeIdx, managedIdx, sharedIdx, objectIdx)
	}
	if runtimeIdx >= managedIdx {
		t.Errorf("Runtime (%d) should appear before Managed (%d)", runtimeIdx, managedIdx)
	}
	if managedIdx >= sharedIdx {
		t.Errorf("Managed (%d) should appear before Shared storage (%d)", managedIdx, sharedIdx)
	}
	if sharedIdx >= objectIdx {
		t.Errorf("Shared storage (%d) should appear before Object storage (%d)", sharedIdx, objectIdx)
	}
}

func TestGetContext_UnknownCategoriesSorted(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{Name: "Node.js", Category: "USER", Versions: []platform.ServiceStackTypeVersion{
			{Name: "nodejs@22", IsBuild: false, Status: "ACTIVE"},
		}},
		{Name: "Zeta Service", Category: "ZETA", Versions: []platform.ServiceStackTypeVersion{
			{Name: "zeta@1", IsBuild: false, Status: "ACTIVE"},
		}},
		{Name: "Alpha Service", Category: "ALPHA", Versions: []platform.ServiceStackTypeVersion{
			{Name: "alpha@1", IsBuild: false, Status: "ACTIVE"},
		}},
	})
	cache := NewStackTypeCache(0)

	result := GetContext(context.Background(), mock, cache)

	// Known category (USER) comes first.
	runtimeIdx := strings.Index(result, "Runtime:")
	// Unknown categories should be sorted alphabetically: ALPHA before ZETA.
	alphaIdx := strings.Index(result, "ALPHA:")
	zetaIdx := strings.Index(result, "ZETA:")

	if runtimeIdx < 0 || alphaIdx < 0 || zetaIdx < 0 {
		t.Fatalf("missing sections: runtime=%d, alpha=%d, zeta=%d", runtimeIdx, alphaIdx, zetaIdx)
	}
	if runtimeIdx >= alphaIdx {
		t.Errorf("Runtime (%d) should appear before ALPHA (%d)", runtimeIdx, alphaIdx)
	}
	if alphaIdx >= zetaIdx {
		t.Errorf("ALPHA (%d) should appear before ZETA (%d)", alphaIdx, zetaIdx)
	}
}

func TestCompactVersions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		versions []string
		want     string
	}{
		{"single", []string{"nodejs@22"}, "nodejs@22"},
		{"single_bare", []string{"static"}, "static"},
		{"multi_same_prefix", []string{"nodejs@18", "nodejs@20", "nodejs@22"}, "nodejs@{18,20,22}"},
		{"multi_bare", []string{"static", "runtime"}, "static, runtime"},
		{"multi_mixed_prefix", []string{"nodejs@22", "python@3.14"}, "nodejs@22, python@3.14"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := compactVersions(tt.versions)
			if got != tt.want {
				t.Errorf("compactVersions(%v) = %q, want %q", tt.versions, got, tt.want)
			}
		})
	}
}
