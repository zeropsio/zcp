// Tests for: plans/analysis/ops.md § ops/discover.go
package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestDiscover_AllServices(t *testing.T) {
	t.Parallel()

	// The Mode struct field is populated only for managed services that
	// support HA/NON_HA (DB, cache, search, messaging, shared-storage) —
	// runtime services and object-storage have a Mode value at the API layer
	// but it carries no replica-count semantic. It is NOT serialized to the
	// agent (json:"-", pinned by TestDiscover_ModeNotSerializedToAgent); this
	// test pins the struct-field population that internal consumers
	// (export/launch/autoscaling) read. Authoritative HA-ness for the agent
	// lives in the type variant; runtime replica count in containers.minContainers.
	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING", Mode: "HA",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-2", Name: "db", ProjectID: "proj-1", Status: "RUNNING", Mode: "NON_HA",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
		{ID: "svc-3", Name: "cache", ProjectID: "proj-1", Status: "RUNNING", Mode: "NON_HA",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "valkey@7.2"}},
		{ID: "svc-4", Name: "storage", ProjectID: "proj-1", Status: "RUNNING", Mode: "HA",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "object-storage@1"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services)

	result, err := Discover(context.Background(), mock, "proj-1", "", false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Project.ID != "proj-1" {
		t.Errorf("expected project ID proj-1, got %s", result.Project.ID)
	}
	if len(result.Services) != 4 {
		t.Fatalf("expected 4 services, got %d", len(result.Services))
	}
	if result.Services[0].Mode != "" {
		t.Errorf("api (nodejs runtime): expected empty mode (runtime services don't carry mode semantics), got %q", result.Services[0].Mode)
	}
	if result.Services[1].Mode != "NON_HA" {
		t.Errorf("db (postgresql managed): expected mode=NON_HA, got %q", result.Services[1].Mode)
	}
	if result.Services[2].Mode != "NON_HA" {
		t.Errorf("cache (valkey managed): expected mode=NON_HA, got %q", result.Services[2].Mode)
	}
	if result.Services[3].Mode != "" {
		t.Errorf("storage (object-storage): expected empty mode (object-storage is always internally replicated), got %q", result.Services[3].Mode)
	}
}

// TestDiscover_ReadsDirectNotES pins that Discover sources its service list from
// the DIRECT (lag-free) ListServicesDirect, NOT the Elasticsearch ListServices.
// A just-imported service is visible to the direct read seconds before ES
// indexes it; the mock models that by seeding ONLY the direct list. If Discover
// regressed to ListServices, it would return zero services here.
func TestDiscover_ReadsDirectNotES(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "p", Status: statusActive}).
		WithServices(nil). // ES list empty (not yet indexed)
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: "READY_TO_DEPLOY",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "", false, false, false)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.Services) != 1 || result.Services[0].Hostname != "appdev" {
		t.Fatalf("Discover must read the DIRECT list (appdev), got %+v", result.Services)
	}
}

// TestDiscover_FiltersBuildContainersFromDirectList pins that the BUILD-category
// rows the direct GET /project/{id}/service-stack returns (ephemeral build
// containers, present once a build runs) are filtered out via IsSystem and never
// surface as user services.
func TestDiscover_FiltersBuildContainersFromDirectList(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "p", Status: statusActive}).
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: "READY_TO_DEPLOY",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
			{ID: "svc-build", Name: "buildappdevv123", ProjectID: "proj-1", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "alpine_build_runtime", ServiceStackTypeCategoryName: "BUILD"}},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "", false, false, false)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.Services) != 1 || result.Services[0].Hostname != "appdev" {
		t.Fatalf("BUILD container must be filtered; got %+v", result.Services)
	}
}

// TestDiscover_ModeNotSerializedToAgent pins the variant-migration contract:
// the deprecated HA `mode` field is internal-only (json:"-") and MUST NOT
// appear in the agent-facing discover JSON. The type variant
// (`postgresql:single@18`) is the authoritative HA indicator; a redundant
// `mode: NON_HA` nudged agents to author the legacy form (eval finding,
// develop-add-managed-dep-to-existing). The struct field stays populated for
// internal consumers (export/launch bundle composition, prod autoscaling).
func TestDiscover_ModeNotSerializedToAgent(t *testing.T) {
	t.Parallel()
	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "db", ProjectID: "proj-1", Status: "RUNNING", Mode: "NON_HA",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql:single@18"}},
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services)
	result, err := Discover(context.Background(), mock, "proj-1", "", false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Internal consumers still read the struct field.
	if result.Services[0].Mode != "NON_HA" {
		t.Errorf("struct Mode must stay populated for internal consumers, got %q", result.Services[0].Mode)
	}
	// Agent-facing JSON must NOT carry a mode key.
	blob, err := json.Marshal(result.Services[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(blob), `"mode"`) {
		t.Errorf("discover JSON must not serialize the deprecated mode field; got %s", blob)
	}
}

func TestDiscover_SingleService_Found(t *testing.T) {
	t.Parallel()

	// ListServices returns minimal info (only CustomAutoscaling — often nulls/zeros).
	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			Ports:                []platform.Port{{Port: 3000, Protocol: "TCP", Public: true}},
		},
		{ID: "svc-2", Name: "db", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
	}

	// GetService returns full detail including CurrentAutoscaling (active config).
	detailSvc := &platform.ServiceStack{
		ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
		Mode:                 "HA",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
		Ports:                []platform.Port{{Port: 3000, Protocol: "TCP", Public: true}},
		CurrentAutoscaling: &platform.CustomAutoscaling{
			CPUMode: "DEDICATED", MinCPU: 1, MaxCPU: 8,
			StartCPUCoreCount: 2,
			MinRAM:            0.125, MaxRAM: 48,
			MinDisk: 1, MaxDisk: 250,
			HorizontalMinCount: 1, HorizontalMaxCount: 10,
			MinFreeCPUCores: 0.5, MinFreeCPUPercent: 20,
			MinFreeRAMGB: 0.25, MinFreeRAMPercent: 15,
		},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithService(detailSvc)

	result, err := Discover(context.Background(), mock, "proj-1", "api", false, false, false)
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
	// nodejs is a runtime service — mode field is non-load-bearing for
	// container count (governed by containers.minContainers below) and
	// must NOT appear in discover output. See TestDiscover_AllServices.
	if svc.Mode != "" {
		t.Errorf("expected empty mode for runtime service, got %q", svc.Mode)
	}
	// Resources should come from CurrentAutoscaling (active config).
	if svc.Resources == nil {
		t.Fatal("expected resources, got nil")
	}
	if svc.Resources["cpuMode"] != "DEDICATED" {
		t.Errorf("expected cpuMode=DEDICATED, got %v", svc.Resources["cpuMode"])
	}
	if svc.Resources["maxCpu"] != int32(8) {
		t.Errorf("expected maxCpu=8, got %v", svc.Resources["maxCpu"])
	}
	if svc.Resources["minRam"] != 0.125 {
		t.Errorf("expected minRam=0.125, got %v", svc.Resources["minRam"])
	}
	if svc.Resources["startCpuCoreCount"] != int32(2) {
		t.Errorf("expected startCpuCoreCount=2, got %v", svc.Resources["startCpuCoreCount"])
	}
	if svc.Resources["minFreeCpuCores"] != 0.5 {
		t.Errorf("expected minFreeCpuCores=0.5, got %v", svc.Resources["minFreeCpuCores"])
	}
	if svc.Resources["minFreeCpuPercent"] != float64(20) {
		t.Errorf("expected minFreeCpuPercent=20, got %v", svc.Resources["minFreeCpuPercent"])
	}
	if svc.Resources["minFreeRamGB"] != 0.25 {
		t.Errorf("expected minFreeRamGB=0.25, got %v", svc.Resources["minFreeRamGB"])
	}
	if svc.Resources["minFreeRamPercent"] != float64(15) {
		t.Errorf("expected minFreeRamPercent=15, got %v", svc.Resources["minFreeRamPercent"])
	}
	if svc.Containers == nil {
		t.Fatal("expected containers, got nil")
	}
	if svc.Containers["maxContainers"] != int32(10) {
		t.Errorf("expected maxContainers=10, got %v", svc.Containers["maxContainers"])
	}
}

func TestDiscover_SingleService_OmitsZeroResources(t *testing.T) {
	t.Parallel()

	// Service with nil CurrentAutoscaling and nil CustomAutoscaling.
	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}

	// GetService also returns nil autoscaling.
	detailSvc := &platform.ServiceStack{
		ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithService(detailSvc)

	result, err := Discover(context.Background(), mock, "proj-1", "api", false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(result.Services))
	}
	svc := result.Services[0]
	if svc.Resources != nil {
		t.Errorf("expected nil resources when no autoscaling, got %v", svc.Resources)
	}
	if svc.Containers != nil {
		t.Errorf("expected nil containers when no autoscaling, got %v", svc.Containers)
	}
}

func TestDiscover_SingleService_FallsBackToCustom(t *testing.T) {
	t.Parallel()

	// Service with nil CurrentAutoscaling but valid CustomAutoscaling.
	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}

	detailSvc := &platform.ServiceStack{
		ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
		CustomAutoscaling: &platform.CustomAutoscaling{
			CPUMode: "SHARED", MinCPU: 1, MaxCPU: 4,
			MinRAM: 0.25, MaxRAM: 4,
			MinDisk: 1, MaxDisk: 10,
			HorizontalMinCount: 1, HorizontalMaxCount: 3,
		},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithService(detailSvc)

	result, err := Discover(context.Background(), mock, "proj-1", "api", false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(result.Services))
	}
	svc := result.Services[0]
	if svc.Resources == nil {
		t.Fatal("expected resources from CustomAutoscaling fallback, got nil")
	}
	if svc.Resources["cpuMode"] != "SHARED" {
		t.Errorf("expected cpuMode=SHARED, got %v", svc.Resources["cpuMode"])
	}
	if svc.Resources["maxCpu"] != int32(4) {
		t.Errorf("expected maxCpu=4, got %v", svc.Resources["maxCpu"])
	}
	if svc.Containers == nil {
		t.Fatal("expected containers from CustomAutoscaling fallback, got nil")
	}
	if svc.Containers["maxContainers"] != int32(3) {
		t.Errorf("expected maxContainers=3, got %v", svc.Containers["maxContainers"])
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

	_, err := Discover(context.Background(), mock, "proj-1", "missing", false, false, false)
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

// TestDiscover_ScopedNotFound_SuggestionExcludesSystemHostnames pins
// the regression caught by Karel's live verification 2026-05-27: a
// `Discover service="<nonexistent>"` call was returning a suggestion
// list that included system hostnames (buildappdev*, core, etc.)
// because FindService's not-found path used raw ListHostnames(services)
// without the IsSystem filter. The IsSystem-MATCHED case was filtered
// (via my earlier branch), but the lookup-MISS case wasn't. Now both
// converge: scoped Discover scans filterUserVisible(services) and any
// miss (system or absent) returns the same ErrServiceNotFound with a
// user-visible-only suggestion.
func TestDiscover_ScopedNotFound_SuggestionExcludesSystemHostnames(t *testing.T) {
	t.Parallel()
	services := []platform.ServiceStack{
		{ID: "svc-build", Name: "buildappdevv123", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "BUILD"}},
		{ID: "svc-core", Name: "core", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "CORE"}},
		{ID: "svc-usr", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services)

	_, err := Discover(context.Background(), mock, "proj-1", "nonexistent-host-xyz", false, false, false)
	if err == nil {
		t.Fatal("expected ErrServiceNotFound for absent hostname")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T", err)
	}
	if pe.Code != platform.ErrServiceNotFound {
		t.Errorf("code = %s, want ErrServiceNotFound", pe.Code)
	}
	// Suggestion MUST NOT name system services.
	for _, sysHost := range []string{"buildappdevv123", "core"} {
		if strings.Contains(pe.Suggestion, sysHost) {
			t.Errorf("suggestion leaks system hostname %q; got: %s", sysHost, pe.Suggestion)
		}
	}
	if !strings.Contains(pe.Suggestion, "api") {
		t.Errorf("suggestion missing user-visible service `api`; got: %s", pe.Suggestion)
	}
}

// TestDiscover_ScopedSystemService_NotFound pins the scoped-path
// system-service filter introduced in v9.101.4 (plan
// plans/discover-adoption-state-enum-2026-05-27.md §"System-service
// filter"). Unfiltered Discover already hides system services
// (CORE/BUILD/INTERNAL/PREPARE_RUNTIME/HTTP_L7_BALANCER); the scoped
// path now matches. A user-targeted `zerops_discover service=
// "<system-hostname>"` returns ErrServiceNotFound with an
// "Available services:" suggestion filtered to user-visible
// hostnames (not naming the system hostnames the user can't
// legitimately target).
func TestDiscover_ScopedSystemService_NotFound(t *testing.T) {
	t.Parallel()
	systemCategories := []string{"CORE", "BUILD", "INTERNAL", "PREPARE_RUNTIME", "HTTP_L7_BALANCER"}
	for _, cat := range systemCategories {
		t.Run(cat, func(t *testing.T) {
			t.Parallel()
			services := []platform.ServiceStack{
				{ID: "svc-sys", Name: "internal-build-svc", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: cat}},
				{ID: "svc-usr", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
			}
			mock := platform.NewMock().
				WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
				WithServices(services)

			_, err := Discover(context.Background(), mock, "proj-1", "internal-build-svc", false, false, false)
			if err == nil {
				t.Fatalf("category %s: expected ErrServiceNotFound for system service", cat)
			}
			pe, ok := err.(*platform.PlatformError)
			if !ok {
				t.Fatalf("category %s: expected *PlatformError, got %T", cat, err)
			}
			if pe.Code != platform.ErrServiceNotFound {
				t.Errorf("category %s: code = %s, want ErrServiceNotFound", cat, pe.Code)
			}
			// Suggestion must NOT name the system hostname (user-visible
			// inventory only).
			if strings.Contains(pe.Suggestion, "internal-build-svc") {
				t.Errorf("category %s: ErrServiceNotFound suggestion must not name system hostname; got: %s",
					cat, pe.Suggestion)
			}
			if !strings.Contains(pe.Suggestion, "api") {
				t.Errorf("category %s: suggestion must list user-visible services; got: %s",
					cat, pe.Suggestion)
			}
		})
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
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{ID: "e1", Key: "PORT", Content: "3000"},
		}).
		WithServiceEnv("svc-2", []platform.ServiceEnvVar{
			{ID: "e2", Key: "DB_HOST", Content: "localhost"},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "", true, true, false)
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

// TestDiscover_YamlBakedLayer pins Phase 3: a live runtime service surfaces
// its yaml-baked run.envVariables (GUI "from master", from the app-version
// userDataList) tagged source="zerops.yaml" — the slim /env omits them.
// Lifecycle-gated (spec §1): managed deps + never-deployed runtimes add none.
func TestDiscover_YamlBakedLayer(t *testing.T) {
	t.Parallel()

	findEnv := func(envs []map[string]any, key string) map[string]any {
		for _, e := range envs {
			if e["key"] == key {
				return e
			}
		}
		return nil
	}

	t.Run("live runtime — yaml-baked appended with source", func(t *testing.T) {
		t.Parallel()
		mock := platform.NewMock().
			WithProject(&platform.Project{ID: "p1", Name: "p", Status: statusActive}).
			WithServices([]platform.ServiceStack{
				{ID: "svc-api", Name: "api", Status: statusActive,
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
					ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-api"}},
			}).
			WithServiceEnv("svc-api", []platform.ServiceEnvVar{{Key: "PORT", Content: "3000"}}).
			WithAppVersionUserData("av-api", []platform.ServiceEnvVar{
				{Key: "FOO", Content: "fromyaml"},
				{Key: "DB_HOST", Content: "${db_hostname}"},
				// RC1/E6: intrinsic + ZEROPS_YAML records in the app-version
				// userDataList must NOT surface as yaml-baked run.envVariables.
				{Key: "zeropsSubdomain", Content: "https://x", Type: platform.ServiceEnvSystem},
				{Key: "ZEROPS_YAML", Content: "build:\n  os: ubuntu", Type: platform.ServiceEnvUser}, // excluded by KEY, not type
			})

		result, err := Discover(context.Background(), mock, "p1", "api", true, true, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		envs := result.Services[0].Envs
		if findEnv(envs, "zeropsSubdomain") != nil {
			t.Errorf("intrinsic SYSTEM var must not surface as source=zerops.yaml: %v", envs)
		}
		if findEnv(envs, "ZEROPS_YAML") != nil {
			t.Errorf("ZEROPS_YAML blob must not surface as source=zerops.yaml: %v", envs)
		}
		foo := findEnv(envs, "FOO")
		if foo == nil {
			t.Fatalf("yaml-baked FOO missing from discover envs: %v", envs)
		}
		if foo["source"] != "zerops.yaml" {
			t.Errorf("FOO source = %v, want zerops.yaml", foo["source"])
		}
		if foo["value"] != "fromyaml" {
			t.Errorf("FOO value = %v, want fromyaml", foo["value"])
		}
		if port := findEnv(envs, "PORT"); port == nil {
			t.Error("slim PORT must remain present")
		}
		if dbh := findEnv(envs, "DB_HOST"); dbh == nil || dbh["isReference"] != true {
			t.Errorf("yaml-baked DB_HOST ref not annotated isReference: %v", dbh)
		}
	})

	t.Run("managed dep — no yaml-baked", func(t *testing.T) {
		t.Parallel()
		mock := platform.NewMock().
			WithProject(&platform.Project{ID: "p1", Name: "p", Status: statusActive}).
			WithServices([]platform.ServiceStack{
				{ID: "svc-db", Name: "db", Status: statusActive,
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"},
					ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-db"}},
			}).
			WithServiceEnv("svc-db", []platform.ServiceEnvVar{{Key: "db_hostname", Content: "db"}}).
			WithAppVersionUserData("av-db", []platform.ServiceEnvVar{{Key: "SHOULD_NOT_APPEAR", Content: "x"}})

		result, err := Discover(context.Background(), mock, "p1", "db", true, true, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if findEnv(result.Services[0].Envs, "SHOULD_NOT_APPEAR") != nil {
			t.Error("managed dep must not surface app-version yaml-baked vars")
		}
	})

	t.Run("never-deployed runtime — no yaml-baked", func(t *testing.T) {
		t.Parallel()
		mock := platform.NewMock().
			WithProject(&platform.Project{ID: "p1", Name: "p", Status: statusActive}).
			WithServices([]platform.ServiceStack{
				{ID: "svc-new", Name: "app", Status: statusActive,
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			}).
			WithServiceEnv("svc-new", []platform.ServiceEnvVar{{Key: "PORT", Content: "3000"}})

		result, err := Discover(context.Background(), mock, "p1", "app", true, true, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, e := range result.Services[0].Envs {
			if e["source"] == "zerops.yaml" {
				t.Errorf("never-deployed runtime must not surface yaml-baked, got: %v", e)
			}
		}
	})
}

// TestDiscover_IncludeEnvs_YamlBakedListedOnce pins the S2 dedupe (spec
// docs/spec-zerops-env-lifecycle.md §1/§6): since 2026-08 a yaml-baked
// run.envVariables key is ALSO mirrored read-only on the slim /env (Type
// USER, same as the app-version userDataList entry) — attachEnvs must NOT
// double-list it. A key seeded on BOTH surfaces appears exactly once in
// info.Envs, tagged source="zerops.yaml"; SYSTEM intrinsics and a genuine
// user-set var stay listed untagged.
func TestDiscover_IncludeEnvs_YamlBakedListedOnce(t *testing.T) {
	t.Parallel()

	findEnv := func(envs []map[string]any, key string) map[string]any {
		for _, e := range envs {
			if e["key"] == key {
				return e
			}
		}
		return nil
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "p", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-api", Name: "api", Status: statusActive,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
				ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-api"}},
		}).
		WithServiceEnv("svc-api", []platform.ServiceEnvVar{
			{Key: "hostname", Content: "api", Type: "SYSTEM"},
			{Key: "API_KEY", Content: "secretvalue", Type: "USER"},
			{Key: "FOO", Content: "fromyaml", Type: "USER"}, // yaml-baked mirror, same value both surfaces
		}).
		WithAppVersionUserData("av-api", []platform.ServiceEnvVar{
			{Key: "FOO", Content: "fromyaml", Type: "USER"},
		})

	result, err := Discover(context.Background(), mock, "p1", "api", true, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	envs := result.Services[0].Envs

	count := 0
	for _, e := range envs {
		if e["key"] == "FOO" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("FOO must appear exactly once in discover envs, got %d occurrences: %v", count, envs)
	}
	foo := findEnv(envs, "FOO")
	if foo["source"] != "zerops.yaml" {
		t.Errorf("FOO source = %v, want zerops.yaml", foo["source"])
	}
	if findEnv(envs, "hostname") == nil {
		t.Error("SYSTEM intrinsic hostname must remain listed")
	}
	if findEnv(envs, "API_KEY") == nil {
		t.Error("user-set API_KEY must remain listed")
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

	result, err := Discover(context.Background(), mock, "proj-1", "", true, true, false)
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
	// Should produce a warning
	if len(result.Warnings) == 0 {
		t.Error("expected warnings when env fetch fails, got none")
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
		WithProjectEnv([]platform.ProjectEnvVar{
			{ID: "pe1", Key: "GLOBAL_KEY", Content: "global_val"},
			{ID: "pe2", Key: "APP_ENV", Content: "production"},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "", true, true, false)
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

// TestDiscover_ProjectEnvs_OnServiceScope_DefaultExcluded pins the
// caller-safe default: when a hostname filter is set, project envs are
// only attached if the caller explicitly opts in via includeProjectEnvs.
// This protects zerops_env action="get" from leaking project values via
// its scoped Discover delegation. See plans/archive/env-discover-three-changes-2026-05-20.md
// Phase 1 + Risk R1.
func TestDiscover_ProjectEnvs_OnServiceScope_DefaultExcluded(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithProjectEnv([]platform.ProjectEnvVar{
			{ID: "pe1", Key: "GLOBAL_KEY", Content: "global_val"},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "api", true, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Project.Envs != nil {
		t.Errorf("expected nil project envs when scoped + includeProjectEnvs=false, got %d", len(result.Project.Envs))
	}
}

// TestDiscover_ProjectEnvs_OnServiceScope_WhenIncluded pins the new
// behavior: a scoped Discover with includeProjectEnvs=true returns both
// service envs AND project envs in a single call. This is the friction
// elimination from plan Change 1 — agents calling zerops_discover with
// service= no longer need a second unscoped call to pick up project envs
// like SESSION_SECRET / GIT_TOKEN / ZCP_API_KEY.
func TestDiscover_ProjectEnvs_OnServiceScope_WhenIncluded(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithProjectEnv([]platform.ProjectEnvVar{
			{ID: "pe1", Key: "GLOBAL_KEY", Content: "global_val"},
			{ID: "pe2", Key: "APP_ENV", Content: "production"},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "api", true, true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Project.Envs == nil {
		t.Fatal("expected project envs attached on scoped call with includeProjectEnvs=true, got nil")
	}
	if len(result.Project.Envs) != 2 {
		t.Fatalf("expected 2 project envs, got %d", len(result.Project.Envs))
	}
	if len(result.Services) != 1 || result.Services[0].Hostname != "api" {
		t.Fatalf("expected the scoped service still resolved, got services=%v", result.Services)
	}
}

// TestDiscover_ManagedService_RefsPopulated pins Phase 3 of
// plans/archive/env-discover-three-changes-2026-05-20.md: managed services
// (topology.IsManagedService) surface a `${hostname_key}` ref string
// per exposed env so the agent can copy verbatim instead of composing
// `DATABASE_URL=${db_hostname}:${db_port}/...` by hand. Ref strings
// use the live env keys for canonical fidelity.
func TestDiscover_ManagedService_RefsPopulated(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "db", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@17"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{Key: "hostname", Content: "db"},
			{Key: "port", Content: "5432"},
			{Key: "user", Content: "dbuser"},
			{Key: "password", Content: "p"},
			{Key: "dbName", Content: "appdb"},
			{Key: "connectionString", Content: "postgresql://dbuser:p@db:5432/appdb"},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "db", true, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(result.Services))
	}
	svc := result.Services[0]
	wantRefs := []string{
		"${db_hostname}",
		"${db_port}",
		"${db_user}",
		"${db_password}",
		"${db_dbName}",
		"${db_connectionString}",
	}
	if !slices.Equal(svc.Refs, wantRefs) {
		t.Errorf("refs mismatch:\n got: %v\nwant: %v", svc.Refs, wantRefs)
	}
}

// TestDiscover_DashHostname_UnderscoreCanonicalRefs pins the
// underscore-canonicalization that matches the platform's env
// interpolator: a `my-db` hostname emits `${my_db_*}` refs, not
// `${my-db_*}` (which the interpolator rejects).
func TestDiscover_DashHostname_UnderscoreCanonicalRefs(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "my-db", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@17"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{Key: "hostname", Content: "my-db"},
			{Key: "port", Content: "5432"},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "my-db", true, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantRefs := []string{"${my_db_hostname}", "${my_db_port}"}
	if !slices.Equal(result.Services[0].Refs, wantRefs) {
		t.Errorf("refs mismatch:\n got: %v\nwant: %v", result.Services[0].Refs, wantRefs)
	}
}

// TestDiscover_RuntimeService_NoRefs pins that runtime services
// (non-managed types) omit the refs field — they don't expose envs
// through the platform's `${...}` interpolator the way managed services
// do, so emitting refs there would mislead agents into composing
// references the platform won't resolve.
func TestDiscover_RuntimeService_NoRefs(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{Key: "PORT", Content: "3000"},
			{Key: "NODE_ENV", Content: "production"},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "api", true, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Services[0].Refs != nil {
		t.Errorf("runtime service refs must be nil, got %v", result.Services[0].Refs)
	}
}

// TestDiscover_NoIncludeEnvs_NoRefs pins that refs derive from live
// envs — when includeEnvs=false the envs are not fetched and refs MUST
// also be omitted (cannot fabricate refs without the source keys).
func TestDiscover_NoIncludeEnvs_NoRefs(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "db", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@17"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{Key: "hostname", Content: "db"},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "db", false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Services[0].Refs != nil {
		t.Errorf("refs must be nil when includeEnvs=false (no source keys), got %v", result.Services[0].Refs)
	}
}

// TestDiscover_ManagedService_EmptyEnvs_OmitsRefs pins the empty-envs
// edge: a managed service that has no exposed envs (no source keys)
// must omit refs entirely rather than emitting an empty `[]` array.
func TestDiscover_ManagedService_EmptyEnvs_OmitsRefs(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "storage", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "object-storage@1"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithServiceEnv("svc-1", nil)

	result, err := Discover(context.Background(), mock, "proj-1", "storage", true, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Services[0].Refs != nil {
		t.Errorf("managed service with zero envs must omit refs, got %v", result.Services[0].Refs)
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

	result, err := Discover(context.Background(), mock, "proj-1", "", true, true, false)
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
	// Should produce a warning
	if len(result.Warnings) == 0 {
		t.Error("expected warnings when project env fetch fails, got none")
	}
}

func TestDiscover_SuccessfulEnvs_NoWarnings(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{ID: "e1", Key: "PORT", Content: "3000"},
		}).
		WithProjectEnv([]platform.ProjectEnvVar{
			{ID: "pe1", Key: "APP_ENV", Content: "production"},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "", true, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings on successful env fetch, got %v", result.Warnings)
	}
	if result.Services[0].Envs == nil {
		t.Error("expected envs to be populated")
	}
	if result.Project.Envs == nil {
		t.Error("expected project envs to be populated")
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

			result, err := Discover(context.Background(), mock, "proj-1", "", false, false, false)
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

func TestDiscover_EnvRefAnnotation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		envs      []platform.ServiceEnvVar
		wantIsRef map[string]bool // key -> expected isReference presence
	}{
		{
			name: "plain values have no isReference",
			envs: []platform.ServiceEnvVar{
				{ID: "e1", Key: "PORT", Content: "3000"},
				{ID: "e2", Key: "HOST", Content: "0.0.0.0"},
			},
			wantIsRef: map[string]bool{"PORT": false, "HOST": false},
		},
		{
			name: "cross-service refs get isReference true",
			envs: []platform.ServiceEnvVar{
				{ID: "e1", Key: "DB_HOST", Content: "${db_hostname}"},
				{ID: "e2", Key: "DB_PASS", Content: "${db_password}"},
			},
			wantIsRef: map[string]bool{"DB_HOST": true, "DB_PASS": true},
		},
		{
			name: "mixed plain and ref values",
			envs: []platform.ServiceEnvVar{
				{ID: "e1", Key: "PORT", Content: "3000"},
				{ID: "e2", Key: "DB_URL", Content: "postgresql://${db_hostname}:${db_port}/mydb"},
				{ID: "e3", Key: "NODE_ENV", Content: "production"},
			},
			wantIsRef: map[string]bool{"PORT": false, "DB_URL": true, "NODE_ENV": false},
		},
		{
			name: "dollar without braces is not a reference",
			envs: []platform.ServiceEnvVar{
				{ID: "e1", Key: "PRICE", Content: "$100"},
			},
			wantIsRef: map[string]bool{"PRICE": false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			services := []platform.ServiceStack{
				{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			}

			mock := platform.NewMock().
				WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
				WithServices(services).
				WithServiceEnv("svc-1", tt.envs)

			result, err := Discover(context.Background(), mock, "proj-1", "api", true, true, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result.Services) != 1 {
				t.Fatalf("expected 1 service, got %d", len(result.Services))
			}

			for _, env := range result.Services[0].Envs {
				key := env["key"].(string)
				wantRef, ok := tt.wantIsRef[key]
				if !ok {
					continue
				}
				_, hasRef := env["isReference"]
				if wantRef && !hasRef {
					t.Errorf("env %s: expected isReference=true, not present", key)
				}
				if !wantRef && hasRef {
					t.Errorf("env %s: expected no isReference, but found %v", key, env["isReference"])
				}
			}
		})
	}
}

func TestDiscover_NotesOnReferences(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{ID: "e1", Key: "PORT", Content: "3000"},
			{ID: "e2", Key: "DB_HOST", Content: "${db_hostname}"},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "api", true, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Notes) == 0 {
		t.Fatal("expected Notes to be populated when env refs exist")
	}
	wantNote := "Values showing ${...} are cross-service references — resolved inside the running container, not in the API. Do not restart to resolve them."
	if !slices.Contains(result.Notes, wantNote) {
		t.Errorf("expected cross-reference note, got: %v", result.Notes)
	}
}

func TestDiscover_NoNotesWithoutReferences(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{ID: "e1", Key: "PORT", Content: "3000"},
			{ID: "e2", Key: "HOST", Content: "0.0.0.0"},
		})

	result, err := Discover(context.Background(), mock, "proj-1", "api", true, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Notes) != 0 {
		t.Errorf("expected no Notes when no refs exist, got: %v", result.Notes)
	}
}

func TestDiscover_SubdomainEnabled_Summary(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			SubdomainAccess:      true},
		{ID: "svc-2", Name: "db", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"},
			SubdomainAccess:      false},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services)

	result, err := Discover(context.Background(), mock, "proj-1", "", false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(result.Services))
	}

	// api has subdomain enabled.
	if !result.Services[0].SubdomainEnabled {
		t.Error("api: expected subdomainEnabled=true")
	}
	// db does not.
	if result.Services[1].SubdomainEnabled {
		t.Error("db: expected subdomainEnabled=false")
	}
	// Summary view should NOT have subdomainUrl (no env fetch).
	if result.Services[0].SubdomainURL != "" {
		t.Errorf("summary view should not have subdomainUrl, got %q", result.Services[0].SubdomainURL)
	}
}

func TestDiscover_SubdomainURL_DetailedView(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			SubdomainAccess:      true,
			Ports:                []platform.Port{{Port: 3000, Protocol: "TCP", Public: true}},
		},
	}

	detailSvc := &platform.ServiceStack{
		ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
		SubdomainAccess:      true,
		Ports:                []platform.Port{{Port: 3000, Protocol: "TCP", Public: true}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithService(detailSvc).
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{ID: "e1", Key: "zeropsSubdomain", Content: "https://api-1df2-3000.prg1.zerops.app"},
			{ID: "e2", Key: "hostname", Content: "api"},
		})

	// Detailed view with includeEnvs=true — URL comes from already-fetched envs.
	result, err := Discover(context.Background(), mock, "proj-1", "api", true, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svc := result.Services[0]
	if !svc.SubdomainEnabled {
		t.Error("expected subdomainEnabled=true")
	}
	if svc.SubdomainURL != "https://api-1df2-3000.prg1.zerops.app" {
		t.Errorf("expected subdomainUrl from env var, got %q", svc.SubdomainURL)
	}
}

func TestDiscover_SubdomainURL_DetailedNoEnvs(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			SubdomainAccess:      true,
			Ports:                []platform.Port{{Port: 3000, Protocol: "TCP", Public: true}},
		},
	}

	detailSvc := &platform.ServiceStack{
		ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
		SubdomainAccess:      true,
		Ports:                []platform.Port{{Port: 3000, Protocol: "TCP", Public: true}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithService(detailSvc).
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{ID: "e1", Key: "zeropsSubdomain", Content: "https://api-1df2-3000.prg1.zerops.app"},
		})

	// Detailed view with includeEnvs=false — should still fetch env to get URL.
	result, err := Discover(context.Background(), mock, "proj-1", "api", false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svc := result.Services[0]
	if !svc.SubdomainEnabled {
		t.Error("expected subdomainEnabled=true")
	}
	if svc.SubdomainURL != "https://api-1df2-3000.prg1.zerops.app" {
		t.Errorf("expected subdomainUrl even without includeEnvs, got %q", svc.SubdomainURL)
	}
}

func TestDiscover_SubdomainURL_DisabledNoFetch(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			SubdomainAccess:      false,
		},
	}

	detailSvc := &platform.ServiceStack{
		ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
		SubdomainAccess:      false,
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithService(detailSvc).
		WithError("GetServiceEnv", fmt.Errorf("should not be called"))

	// When subdomain is disabled and includeEnvs=false, should NOT call GetServiceEnv.
	result, err := Discover(context.Background(), mock, "proj-1", "api", false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svc := result.Services[0]
	if svc.SubdomainEnabled {
		t.Error("expected subdomainEnabled=false")
	}
	if svc.SubdomainURL != "" {
		t.Errorf("expected empty subdomainUrl when disabled, got %q", svc.SubdomainURL)
	}
}

// TestExtractSubdomainURL_RawEnvsHit pins the cached path: when the
// caller hands in a non-nil env slice that already contains the key,
// no API call fires and the URL comes straight from rawEnvs.
func TestExtractSubdomainURL_RawEnvsHit(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithError("GetServiceEnv", fmt.Errorf("should not be called"))
	rawEnvs := []platform.ServiceEnvVar{
		{Key: "zeropsSubdomain", Content: "https://app-1df2-3000.prg1.zerops.app"},
	}
	got := ExtractSubdomainURL(context.Background(), mock, "svc-1", rawEnvs)
	if got != "https://app-1df2-3000.prg1.zerops.app" {
		t.Errorf("got %q, want URL from rawEnvs", got)
	}
}

// TestExtractSubdomainURL_RawEnvsMiss_FetchFallback pins the
// asynchronous-injection contract: SubdomainAccess can be true while the
// platform hasn't yet propagated zeropsSubdomain into the service's env
// list (race between Enable and the env injector). When rawEnvs is
// non-nil but lacks the key, the helper MUST refetch — otherwise discover
// surfaces an empty URL during the propagation window.
func TestExtractSubdomainURL_RawEnvsMiss_FetchFallback(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{Key: "zeropsSubdomain", Content: "https://app-late.prg1.zerops.app"},
		})
	rawEnvs := []platform.ServiceEnvVar{{Key: "OTHER_VAR", Content: "x"}}
	got := ExtractSubdomainURL(context.Background(), mock, "svc-1", rawEnvs)
	if got != "https://app-late.prg1.zerops.app" {
		t.Errorf("got %q, want URL from fetch fallback", got)
	}
}

// TestExtractSubdomainURL_NilEnvs_FetchFallback covers the no-cached-envs
// path — caller signals "I haven't fetched envs yet, do it for me".
func TestExtractSubdomainURL_NilEnvs_FetchFallback(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{Key: "zeropsSubdomain", Content: "https://app-fetched.prg1.zerops.app"},
		})
	got := ExtractSubdomainURL(context.Background(), mock, "svc-1", nil)
	if got != "https://app-fetched.prg1.zerops.app" {
		t.Errorf("got %q, want URL from fetch fallback", got)
	}
}

// TestExtractSubdomainURL_FetchError returns empty without panic when
// the API call fails — caller treats empty as "no URL available".
func TestExtractSubdomainURL_FetchError(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithError("GetServiceEnv", fmt.Errorf("transient API error"))
	got := ExtractSubdomainURL(context.Background(), mock, "svc-1", nil)
	if got != "" {
		t.Errorf("got %q, want empty on fetch error", got)
	}
}

func TestDiscover_ProjectNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithError("GetProject", fmt.Errorf("project not found"))

	_, err := Discover(context.Background(), mock, "proj-1", "", false, false, false)
	if err == nil {
		t.Fatal("expected error when project not found")
	}
}
