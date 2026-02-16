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
			if strings.Contains(result, "Service Stacks (live)") {
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
	if strings.Contains(result, "Service Stacks (live)") {
		t.Error("should not contain dynamic section when API fails with no cache")
	}
}

func TestGetContext_EmptyStacks(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceStackTypes(nil)
	cache := NewStackTypeCache(0)

	result := GetContext(context.Background(), mock, cache)

	if strings.Contains(result, "Service Stacks (live)") {
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
	if strings.Contains(result, "Service Stacks (live)") {
		t.Error("should not show dynamic section when all types are hidden")
	}
}
