// Tests for: context.go — GetContext returns platform knowledge for AI agents.

package ops

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestGetContext_StaticOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		client platform.Client
		cache  *StackTypeCache
	}{
		{"nil_client_nil_cache", nil, nil},
		{"nil_client", nil, NewStackTypeCache(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := GetContext(context.Background(), tt.client, tt.cache)
			if result == "" {
				t.Fatal("GetContext() returned empty string")
			}
			if !strings.Contains(result, "Critical Rules") {
				t.Error("missing Critical Rules section")
			}
			if strings.Contains(result, "Available Service Stacks") {
				t.Error("static-only should not contain dynamic section")
			}
		})
	}
}

func TestGetContext_ContainsCriticalSections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		section string
	}{
		{"critical_rules", "Critical Rules"},
		{"overview", "Overview"},
		{"how_zerops_works", "How Zerops Works"},
		{"configuration", "Configuration"},
		{"defaults", "Defaults"},
	}

	result := GetContext(context.Background(), nil, nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(result, tt.section) {
				t.Errorf("GetContext() does not contain section %q", tt.section)
			}
		})
	}
}

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
		{"has_dynamic_header", "Available Service Stacks"},
		{"has_runtime_category", "Runtime & Container"},
		{"has_managed_category", "Managed Services"},
		{"has_nodejs", "Node.js"},
		{"has_postgresql", "PostgreSQL"},
		{"has_nodejs_version", "nodejs@22"},
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

	// Go has a BUILD counterpart → "build+run"
	if !strings.Contains(result, "**Golang** — build+run: go@1") {
		t.Error("Golang should show build+run (has BUILD counterpart)")
	}
	// Nginx has no BUILD counterpart → "run"
	if !strings.Contains(result, "**Nginx static** — run: nginx@1.22") {
		t.Error("Nginx should show run (no BUILD counterpart)")
	}
	// PostgreSQL has no BUILD counterpart → "run"
	if !strings.Contains(result, "**PostgreSQL** — run: postgresql@16") {
		t.Error("PostgreSQL should show run (managed service)")
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

	// PHP build bases don't match run bases → shown in "Build-only Bases"
	if !strings.Contains(result, "Build-only Bases") {
		t.Error("should have Build-only Bases section for unmatched PHP build versions")
	}
	if !strings.Contains(result, "php@8.1") {
		t.Error("should show php@8.1 in build-only section")
	}
	if !strings.Contains(result, "php@8.3") {
		t.Error("should show php@8.3 in build-only section")
	}
	// Go build version matches run version → NOT in build-only section
	// (go@1 should be matched and shown as build+run in the main section)
	if !strings.Contains(result, "**Golang** — build+run: go@1") {
		t.Error("Golang should show build+run")
	}
	// "zbuild" prefix should be stripped in build-only section
	if strings.Contains(result, "zbuild") {
		t.Error("should strip 'zbuild ' prefix from build-only names")
	}
}

func TestGetContext_APIError(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithError("ListServiceStackTypes", fmt.Errorf("api down"))
	cache := NewStackTypeCache(0)

	result := GetContext(context.Background(), mock, cache)

	if result == "" {
		t.Fatal("GetContext() returned empty on API error")
	}
	if !strings.Contains(result, "Critical Rules") {
		t.Error("should contain static content on API error")
	}
	if strings.Contains(result, "Available Service Stacks") {
		t.Error("should not contain dynamic section when API fails with no cache")
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
			{Name: "core", IsBuild: false, Status: "ACTIVE"},
		}},
	})
	cache := NewStackTypeCache(0)

	result := GetContext(context.Background(), mock, cache)

	if !strings.Contains(result, "Node.js") {
		t.Error("should contain USER category types")
	}
	if strings.Contains(result, "Internal Tool") {
		t.Error("should filter out INTERNAL category")
	}
	if strings.Contains(result, "zbuild") {
		t.Error("should filter out BUILD category from display")
	}
	if strings.Contains(result, "Prepare Runtime") {
		t.Error("should filter out PREPARE_RUNTIME category")
	}
	if strings.Contains(result, "HTTP Balancer") {
		t.Error("should filter out HTTP_L7_BALANCER category")
	}
	if strings.Contains(result, "**Core**") {
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

func TestGetContext_EmptyStacks(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes(nil)
	cache := NewStackTypeCache(0)

	result := GetContext(context.Background(), mock, cache)

	if strings.Contains(result, "Available Service Stacks") {
		t.Error("should not contain dynamic section with empty stacks")
	}
}

func TestGetContext_NoHardcodedServiceTypes(t *testing.T) {
	t.Parallel()

	result := GetContext(context.Background(), nil, nil)
	if strings.Contains(result, "| Runtime |") {
		t.Error("static context should not contain hardcoded Service Types table")
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
	if strings.Contains(result, "Node.js") {
		t.Error("type with all DISABLED versions should not appear")
	}
	// PostgreSQL has ACTIVE version → should appear
	if !strings.Contains(result, "PostgreSQL") {
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
	runtimeIdx := strings.Index(result, "Runtime & Container")
	managedIdx := strings.Index(result, "Managed Services")
	sharedIdx := strings.Index(result, "Shared Storage")
	objectIdx := strings.Index(result, "Object Storage")

	if runtimeIdx < 0 || managedIdx < 0 || sharedIdx < 0 || objectIdx < 0 {
		t.Fatalf("missing category sections: runtime=%d, managed=%d, shared=%d, object=%d",
			runtimeIdx, managedIdx, sharedIdx, objectIdx)
	}
	if runtimeIdx >= managedIdx {
		t.Errorf("Runtime & Container (%d) should appear before Managed Services (%d)", runtimeIdx, managedIdx)
	}
	if managedIdx >= sharedIdx {
		t.Errorf("Managed Services (%d) should appear before Shared Storage (%d)", managedIdx, sharedIdx)
	}
	if sharedIdx >= objectIdx {
		t.Errorf("Shared Storage (%d) should appear before Object Storage (%d)", sharedIdx, objectIdx)
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
	runtimeIdx := strings.Index(result, "Runtime & Container")
	// Unknown categories should be sorted alphabetically: ALPHA before ZETA.
	alphaIdx := strings.Index(result, "ALPHA")
	zetaIdx := strings.Index(result, "ZETA")

	if runtimeIdx < 0 || alphaIdx < 0 || zetaIdx < 0 {
		t.Fatalf("missing sections: runtime=%d, alpha=%d, zeta=%d", runtimeIdx, alphaIdx, zetaIdx)
	}
	if runtimeIdx >= alphaIdx {
		t.Errorf("Runtime & Container (%d) should appear before ALPHA (%d)", runtimeIdx, alphaIdx)
	}
	if alphaIdx >= zetaIdx {
		t.Errorf("ALPHA (%d) should appear before ZETA (%d)", alphaIdx, zetaIdx)
	}
}

func TestGetContext_OnlyHiddenCategories(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{Name: "Core", Category: "CORE", Versions: []platform.ServiceStackTypeVersion{
			{Name: "core", IsBuild: false, Status: "ACTIVE"},
		}},
		{Name: "zbuild Go", Category: "BUILD", Versions: []platform.ServiceStackTypeVersion{
			{Name: "go@1", IsBuild: true, Status: "ACTIVE"},
		}},
	})
	cache := NewStackTypeCache(0)

	result := GetContext(context.Background(), mock, cache)

	// All types are in hidden categories → no dynamic section
	if strings.Contains(result, "Available Service Stacks") {
		t.Error("should not show dynamic section when all types are hidden")
	}
}
