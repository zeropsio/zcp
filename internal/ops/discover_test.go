// Tests for: plans/analysis/ops.md § ops/discover.go
package ops

import (
	"context"
	"fmt"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestDiscover_AllServices(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-2", Name: "db", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
		{ID: "svc-3", Name: "cache", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "valkey@7.2"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services)

	result, err := Discover(context.Background(), mock, "proj-1", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Project.ID != "proj-1" {
		t.Errorf("expected project ID proj-1, got %s", result.Project.ID)
	}
	if len(result.Services) != 3 {
		t.Fatalf("expected 3 services, got %d", len(result.Services))
	}
}

func TestDiscover_SingleService_Found(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			Ports:                []platform.Port{{Port: 3000, Protocol: "TCP", Public: true}},
			CustomAutoscaling: &platform.CustomAutoscaling{
				CPUMode: "SHARED", MinCPU: 1, MaxCPU: 4,
				MinRAM: 0.25, MaxRAM: 4,
				MinDisk: 1, MaxDisk: 10,
				HorizontalMinCount: 1, HorizontalMaxCount: 3,
			}},
		{ID: "svc-2", Name: "db", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services)

	result, err := Discover(context.Background(), mock, "proj-1", "api", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(result.Services))
	}
	svc := result.Services[0]
	if svc.Hostname != "api" {
		t.Errorf("expected hostname=api, got %s", svc.Hostname)
	}
	if svc.ServiceID != "svc-1" {
		t.Errorf("expected serviceId=svc-1, got %s", svc.ServiceID)
	}
}

func TestDiscover_SingleService_NotFound(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING"},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services)

	_, err := Discover(context.Background(), mock, "proj-1", "missing", false)
	if err == nil {
		t.Fatal("expected error for missing service")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrServiceNotFound {
		t.Errorf("expected code %s, got %s", platform.ErrServiceNotFound, pe.Code)
	}
}

func TestDiscover_WithEnvs(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-2", Name: "db", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithServiceEnv("svc-1", []platform.EnvVar{
			{ID: "e1", Key: "PORT", Content: "3000"},
		}).
		WithServiceEnv("svc-2", []platform.EnvVar{
			{ID: "e2", Key: "DB_HOST", Content: "localhost"},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(result.Services))
	}
	for _, svc := range result.Services {
		if svc.Envs == nil {
			t.Errorf("expected envs for service %s, got nil", svc.Hostname)
		}
	}
}

func TestDiscover_EnvFetchError_Graceful(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithError("GetServiceEnv", fmt.Errorf("env fetch error"))

	result, err := Discover(context.Background(), mock, "proj-1", "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(result.Services))
	}
	// Env fetch error should not fail the whole discover
	if result.Services[0].Envs != nil {
		t.Error("expected nil envs when fetch fails")
	}
}

func TestDiscover_ProjectEnvs_NoFilter(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithProjectEnv([]platform.EnvVar{
			{ID: "pe1", Key: "GLOBAL_KEY", Content: "global_val"},
			{ID: "pe2", Key: "APP_ENV", Content: "production"},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Project.Envs == nil {
		t.Fatal("expected project envs, got nil")
	}
	if len(result.Project.Envs) != 2 {
		t.Fatalf("expected 2 project envs, got %d", len(result.Project.Envs))
	}
	if result.Project.Envs[0]["key"] != "GLOBAL_KEY" {
		t.Errorf("expected first env key=GLOBAL_KEY, got %v", result.Project.Envs[0]["key"])
	}
}

func TestDiscover_ProjectEnvs_WithServiceFilter(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithProjectEnv([]platform.EnvVar{
			{ID: "pe1", Key: "GLOBAL_KEY", Content: "global_val"},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "api", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Project envs should NOT be included when filtering by service hostname.
	if result.Project.Envs != nil {
		t.Errorf("expected nil project envs when hostname filter is set, got %d", len(result.Project.Envs))
	}
}

func TestDiscover_ProjectEnvFetchError_Graceful(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithError("GetProjectEnv", fmt.Errorf("project env fetch error"))

	result, err := Discover(context.Background(), mock, "proj-1", "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Project env fetch error should not fail the whole discover.
	if result.Project.Envs != nil {
		t.Error("expected nil project envs when fetch fails")
	}
	if len(result.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(result.Services))
	}
}

func TestDiscover_FiltersSystemServices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		services        []platform.ServiceStack
		wantCount       int
		wantNoHostnames []string
	}{
		{
			name: "filters CORE category",
			services: []platform.ServiceStack{
				{ID: "svc-0", Name: "core", ProjectID: "proj-1", Status: statusActive,
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName:  "core",
						ServiceStackTypeCategoryName: "CORE",
					}},
				{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName:  "nodejs@22",
						ServiceStackTypeCategoryName: "USER",
					}},
			},
			wantCount:       1,
			wantNoHostnames: []string{"core"},
		},
		{
			name: "filters BUILD category",
			services: []platform.ServiceStack{
				{ID: "svc-0", Name: "buildappdevv1771328058", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName:  "ubuntu-build@1",
						ServiceStackTypeCategoryName: "BUILD",
					}},
				{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName:  "nodejs@22",
						ServiceStackTypeCategoryName: "USER",
					}},
			},
			wantCount:       1,
			wantNoHostnames: []string{"buildappdevv1771328058"},
		},
		{
			name: "filters all system categories at once",
			services: []platform.ServiceStack{
				{ID: "s1", Name: "core", ProjectID: "proj-1", Status: statusActive,
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "CORE"}},
				{ID: "s2", Name: "builder", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "BUILD"}},
				{ID: "s3", Name: "internal-svc", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "INTERNAL"}},
				{ID: "s4", Name: "prep-runtime", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "PREPARE_RUNTIME"}},
				{ID: "s5", Name: "balancer", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "HTTP_L7_BALANCER"}},
				{ID: "s6", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName:  "nodejs@22",
						ServiceStackTypeCategoryName: "USER",
					}},
				{ID: "s7", Name: "db", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName:  "postgresql@16",
						ServiceStackTypeCategoryName: "STANDARD",
					}},
			},
			wantCount:       2,
			wantNoHostnames: []string{"core", "builder", "internal-svc", "prep-runtime", "balancer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := platform.NewMock().
				WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
				WithServices(tt.services)

			result, err := Discover(context.Background(), mock, "proj-1", "", false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result.Services) != tt.wantCount {
				t.Fatalf("expected %d services, got %d", tt.wantCount, len(result.Services))
			}
			hostnames := make(map[string]bool)
			for _, svc := range result.Services {
				hostnames[svc.Hostname] = true
			}
			for _, forbidden := range tt.wantNoHostnames {
				if hostnames[forbidden] {
					t.Errorf("system service %q should be filtered", forbidden)
				}
			}
		})
	}
}

func TestDiscover_ProjectNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithError("GetProject", fmt.Errorf("project not found"))

	_, err := Discover(context.Background(), mock, "proj-1", "", false)
	if err == nil {
		t.Fatal("expected error when project not found")
	}
}
