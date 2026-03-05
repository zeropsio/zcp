package tools

import (
	"context"
	"fmt"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestCheckProvision_AllServicesExist_Pass(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING"},
		{ID: "s2", Name: "appstage", Status: "NEW"},
		{ID: "s3", Name: "db", Status: "RUNNING"},
	}).WithServiceEnv("s3", []platform.EnvVar{{Key: "connectionString", Content: "pg://..."}})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
			},
		}},
	}

	checker := checkProvision(mock, "proj-1")
	result, err := checker(context.Background(), plan)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass, got fail: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckProvision_ActiveStatus_Pass(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "ACTIVE"},
		{ID: "s2", Name: "appstage", Status: "READY_TO_DEPLOY"},
		{ID: "s3", Name: "db", Status: "ACTIVE"},
	}).WithServiceEnv("s3", []platform.EnvVar{{Key: "connectionString", Content: "pg://..."}})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
			},
		}},
	}

	checker := checkProvision(mock, "proj-1")
	result, err := checker(context.Background(), plan)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass for ACTIVE status, got fail: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckProvision_MissingService_Fail(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING"},
	})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"},
			},
		}},
	}

	checker := checkProvision(mock, "proj-1")
	result, err := checker(context.Background(), plan)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for missing db service")
	}

	hasFail := false
	for _, c := range result.Checks {
		if c.Status == "fail" && c.Name == "db_exists" {
			hasFail = true
		}
	}
	if !hasFail {
		t.Error("expected a fail check for db_exists")
	}
}

func TestCheckProvision_NoEnvVars_Fail(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING"},
		{ID: "s2", Name: "db", Status: "RUNNING"},
	})
	// db has no env vars configured

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"},
			},
		}},
	}

	checker := checkProvision(mock, "proj-1")
	result, err := checker(context.Background(), plan)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for missing env vars on db")
	}
}

func TestCheckProvision_SharedStorage_SkipEnvCheck(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING"},
		{ID: "s2", Name: "files", Status: "RUNNING"},
	})
	// shared-storage has no env vars — should not be checked

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", Simple: true},
			Dependencies: []workflow.Dependency{
				{Hostname: "files", Type: "shared-storage@1", Resolution: "CREATE"},
			},
		}},
	}

	checker := checkProvision(mock, "proj-1")
	result, err := checker(context.Background(), plan)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass (storage should skip env check): %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckProvision_ObjectStorage_SkipEnvCheck(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING"},
		{ID: "s2", Name: "storage", Status: "RUNNING"},
	})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", Simple: true},
			Dependencies: []workflow.Dependency{
				{Hostname: "storage", Type: "object-storage@1", Resolution: "CREATE"},
			},
		}},
	}

	checker := checkProvision(mock, "proj-1")
	result, err := checker(context.Background(), plan)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass (object-storage should skip env check): %s", result.Summary)
	}
}

func TestCheckGenerate_ReturnsNil(t *testing.T) {
	t.Parallel()
	checker := checkGenerate()
	if checker != nil {
		t.Error("checkGenerate should return nil (skip)")
	}
}

func TestCheckDeploy_AllActive_Pass(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING", Ports: []platform.Port{{Port: 3000}}, SubdomainAccess: true},
		{ID: "s2", Name: "appstage", Status: "RUNNING", Ports: []platform.Port{{Port: 3000}}, SubdomainAccess: true},
	})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
		}},
	}

	checker := checkDeploy(mock, "proj-1")
	result, err := checker(context.Background(), plan)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckDeploy_BuildFailed_Fail(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "BUILD_FAILED"},
	})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", Simple: true},
		}},
	}

	checker := checkDeploy(mock, "proj-1")
	result, err := checker(context.Background(), plan)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for BUILD_FAILED status")
	}
}

func TestCheckDeploy_SubdomainNotEnabled_Fail(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING", Ports: []platform.Port{{Port: 3000}}, SubdomainAccess: false},
	})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", Simple: true},
		}},
	}

	checker := checkDeploy(mock, "proj-1")
	result, err := checker(context.Background(), plan)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for subdomain not enabled")
	}
}

func TestCheckVerify_AllHealthy_Pass(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			ID: "s1", Name: "appdev", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName:  "nodejs@22",
				ServiceStackTypeCategoryName: "USER",
			},
		},
	})
	logFetcher := platform.NewMockLogFetcher()

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", Simple: true},
		}},
	}

	checker := checkVerify(mock, logFetcher, "proj-1", nil)
	result, err := checker(context.Background(), plan)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	// Verify we at least get a result. Without httpClient and subdomain, some
	// checks will be skipped, resulting in degraded status.
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestCheckVerify_PreExistingUnhealthy_Ignored(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			ID: "s1", Name: "appdev", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName:  "nodejs@22",
				ServiceStackTypeCategoryName: "USER",
			},
		},
		{
			ID: "s2", Name: "oldapp", Status: "STOPPED",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName:  "nodejs@20",
				ServiceStackTypeCategoryName: "USER",
			},
		},
	})
	logFetcher := platform.NewMockLogFetcher()

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", Simple: true},
		}},
	}

	checker := checkVerify(mock, logFetcher, "proj-1", nil)
	result, err := checker(context.Background(), plan)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}

	// The pre-existing unhealthy "oldapp" should not cause failure.
	for _, c := range result.Checks {
		if c.Name == "oldapp_health" {
			t.Error("oldapp should not appear in checks (not in plan)")
		}
	}
}

func TestCheckVerify_TargetUnhealthy_Fail(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			ID: "s1", Name: "appdev", Status: "STOPPED",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName:  "nodejs@22",
				ServiceStackTypeCategoryName: "USER",
			},
		},
	})
	logFetcher := platform.NewMockLogFetcher()

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", Simple: true},
		}},
	}

	checker := checkVerify(mock, logFetcher, "proj-1", nil)
	result, err := checker(context.Background(), plan)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for stopped/unhealthy target service")
	}
}

func TestCheckProvision_NilPlan_ReturnsNil(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	checker := checkProvision(mock, "proj-1")
	result, err := checker(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for nil plan")
	}
}

func TestCheckProvision_APIError_ReturnsError(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithError("ListServices", fmt.Errorf("API down"))
	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
		}},
	}
	checker := checkProvision(mock, "proj-1")
	_, err := checker(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error from API failure")
	}
}

func TestBuildStepChecker_UnknownStep_ReturnsNil(t *testing.T) {
	t.Parallel()
	checker := buildStepChecker("discover", nil, nil, "", nil)
	if checker != nil {
		t.Error("expected nil checker for unknown step 'discover'")
	}
}

func TestBuildStepChecker_KnownSteps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		step    string
		wantNil bool
	}{
		{"provision", false},
		{"generate", true},
		{"deploy", false},
		{"verify", false},
		{"discover", true},
		{"unknown", true},
	}
	for _, tt := range tests {
		t.Run(tt.step, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock()
			checker := buildStepChecker(tt.step, mock, nil, "proj-1", nil)
			if tt.wantNil && checker != nil {
				t.Errorf("expected nil checker for step %q", tt.step)
			}
			if !tt.wantNil && checker == nil {
				t.Errorf("expected non-nil checker for step %q", tt.step)
			}
		})
	}
}

func TestIsManagedNonStorage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		serviceType string
		want        bool
	}{
		{"postgresql", "postgresql@16", true},
		{"valkey", "valkey@7.2", true},
		{"shared_storage", "shared-storage@1", false},
		{"object_storage", "object-storage@1", false},
		{"nodejs", "nodejs@22", false},
		{"nats", "nats@2.10", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isManagedNonStorage(tt.serviceType); got != tt.want {
				t.Errorf("isManagedNonStorage(%q) = %v, want %v", tt.serviceType, got, tt.want)
			}
		})
	}
}
