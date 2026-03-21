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

	checker := checkProvision(mock, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
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

	checker := checkProvision(mock, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
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

	checker := checkProvision(mock, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
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

	checker := checkProvision(mock, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
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
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "simple"},
			Dependencies: []workflow.Dependency{
				{Hostname: "files", Type: "shared-storage@1", Resolution: "CREATE"},
			},
		}},
	}

	checker := checkProvision(mock, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
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
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "simple"},
			Dependencies: []workflow.Dependency{
				{Hostname: "storage", Type: "object-storage@1", Resolution: "CREATE"},
			},
		}},
	}

	checker := checkProvision(mock, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass (object-storage should skip env check): %s", result.Summary)
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

	checker := checkDeploy(mock, nil, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
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
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "simple"},
		}},
	}

	checker := checkDeploy(mock, nil, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
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
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "simple"},
		}},
	}

	checker := checkDeploy(mock, nil, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for subdomain not enabled")
	}
}




func TestCheckProvision_NilPlan_ReturnsNil(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	checker := checkProvision(mock, "proj-1", nil)
	result, err := checker(context.Background(), nil, nil)
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
	checker := checkProvision(mock, "proj-1", nil)
	_, err := checker(context.Background(), plan, nil)
	if err == nil {
		t.Fatal("expected error from API failure")
	}
}

func TestBuildStepChecker_UnknownStep_ReturnsNil(t *testing.T) {
	t.Parallel()
	checker := buildStepChecker("discover", nil, nil, "", nil, nil, "")
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
		{"generate", false},
		{"deploy", false},
		{"close", true},
		{"discover", true},
		{"unknown", true},
	}
	for _, tt := range tests {
		t.Run(tt.step, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock()
			checker := buildStepChecker(tt.step, mock, nil, "proj-1", nil, nil, t.TempDir())
			if tt.wantNil && checker != nil {
				t.Errorf("expected nil checker for step %q", tt.step)
			}
			if !tt.wantNil && checker == nil {
				t.Errorf("expected non-nil checker for step %q", tt.step)
			}
		})
	}
}

func TestCheckProvision_StoresDiscoveredEnvVars(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvLocal, nil)

	// Start a bootstrap session.
	_, err := eng.BootstrapStart("proj-1", "test intent")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Submit plan to complete discover step.
	_, err = eng.BootstrapCompletePlan([]workflow.BootstrapTarget{{
		Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
		Dependencies: []workflow.Dependency{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
		},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING"},
		{ID: "s2", Name: "appstage", Status: "NEW"},
		{ID: "s3", Name: "db", Status: "RUNNING"},
	}).WithServiceEnv("s3", []platform.EnvVar{
		{Key: "connectionString", Content: "pg://..."},
		{Key: "port", Content: "5432"},
		{Key: "user", Content: "zerops"},
	})

	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}

	checker := checkProvision(mock, "proj-1", eng)
	result, err := checker(context.Background(), state.Bootstrap.Plan, state.Bootstrap)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass: %s", result.Summary)
	}

	// Verify env vars were stored.
	state, err = eng.GetState()
	if err != nil {
		t.Fatalf("GetState after check: %v", err)
	}
	if state.Bootstrap.DiscoveredEnvVars == nil {
		t.Fatal("DiscoveredEnvVars should not be nil after provision check")
	}
	dbVars := state.Bootstrap.DiscoveredEnvVars["db"]
	if len(dbVars) != 3 {
		t.Errorf("db env vars: want 3, got %d", len(dbVars))
	}
}

func TestCheckProvision_ExistingRuntime_StageActive_Pass(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING"},
		{ID: "s2", Name: "appstage", Status: "ACTIVE"},
		{ID: "s3", Name: "cache", Status: "RUNNING"},
	}).WithServiceEnv("s3", []platform.EnvVar{{Key: "port", Content: "6379"}})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "python@3.12", IsExisting: true},
			Dependencies: []workflow.Dependency{
				{Hostname: "cache", Type: "valkey@7.2", Mode: "NON_HA", Resolution: "CREATE"},
			},
		}},
	}

	checker := checkProvision(mock, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass for existing runtime with ACTIVE stage, got fail: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckProvision_ExistingRuntime_StageRunning_Pass(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "ACTIVE"},
		{ID: "s2", Name: "appstage", Status: "RUNNING"},
		{ID: "s3", Name: "queue", Status: "RUNNING"},
	}).WithServiceEnv("s3", []platform.EnvVar{{Key: "connectionString", Content: "nats://..."}})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "python@3.12", IsExisting: true},
			Dependencies: []workflow.Dependency{
				{Hostname: "queue", Type: "nats@2.10", Mode: "NON_HA", Resolution: "CREATE"},
			},
		}},
	}

	checker := checkProvision(mock, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass for existing runtime with RUNNING stage, got fail: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckProvision_NewRuntime_StageActive_Fail(t *testing.T) {
	t.Parallel()
	// New runtime (IsExisting=false) should still require stage to be NEW/READY_TO_DEPLOY.
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING"},
		{ID: "s2", Name: "appstage", Status: "ACTIVE"},
	})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: false},
		}},
	}

	checker := checkProvision(mock, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for new runtime with ACTIVE stage — should require NEW or READY_TO_DEPLOY")
	}
}

func TestCheckProvision_ExistsDep_StoresEnvVars(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvLocal, nil)

	_, err := eng.BootstrapStart("proj-1", "exists dep env var test")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	_, err = eng.BootstrapCompletePlan([]workflow.BootstrapTarget{{
		Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: true},
		Dependencies: []workflow.Dependency{
			{Hostname: "db", Type: "postgresql@16", Resolution: "EXISTS"},
		},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING"},
		{ID: "s2", Name: "appstage", Status: "RUNNING"},
		{ID: "s3", Name: "db", Status: "RUNNING"},
	}).WithServiceEnv("s3", []platform.EnvVar{
		{Key: "connectionString", Content: "pg://..."},
		{Key: "port", Content: "5432"},
		{Key: "user", Content: "zerops"},
	})

	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}

	checker := checkProvision(mock, "proj-1", eng)
	result, err := checker(context.Background(), state.Bootstrap.Plan, state.Bootstrap)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}

	state, err = eng.GetState()
	if err != nil {
		t.Fatalf("GetState after check: %v", err)
	}
	if state.Bootstrap.DiscoveredEnvVars == nil {
		t.Fatal("DiscoveredEnvVars should not be nil after provision check")
	}
	dbVars := state.Bootstrap.DiscoveredEnvVars["db"]
	if len(dbVars) != 3 {
		t.Errorf("db env vars: want 3, got %d", len(dbVars))
	}
}

func TestCheckProvision_ExistsDep_NoEnvVars_Fail(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING"},
		{ID: "s2", Name: "db", Status: "RUNNING"},
	})
	// db has no env vars — EXISTS dep should still require them.

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: true, BootstrapMode: "simple"},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Resolution: "EXISTS"},
			},
		}},
	}

	checker := checkProvision(mock, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for missing env vars on EXISTS dep")
	}
	hasEnvFail := false
	for _, c := range result.Checks {
		if c.Name == "db_env_vars" && c.Status == statusFail {
			hasEnvFail = true
		}
	}
	if !hasEnvFail {
		t.Error("expected db_env_vars fail check")
	}
}

func TestCheckProvision_MixedResolution_StoresBoth(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvLocal, nil)

	_, err := eng.BootstrapStart("proj-1", "mixed resolution test")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	_, err = eng.BootstrapCompletePlan([]workflow.BootstrapTarget{{
		Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: true},
		Dependencies: []workflow.Dependency{
			{Hostname: "db", Type: "postgresql@16", Resolution: "EXISTS"},
			{Hostname: "cache", Type: "valkey@7.2", Mode: "NON_HA", Resolution: "CREATE"},
		},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING"},
		{ID: "s2", Name: "appstage", Status: "RUNNING"},
		{ID: "s3", Name: "db", Status: "RUNNING"},
		{ID: "s4", Name: "cache", Status: "RUNNING"},
	}).WithServiceEnv("s3", []platform.EnvVar{
		{Key: "connectionString", Content: "pg://..."},
		{Key: "port", Content: "5432"},
	}).WithServiceEnv("s4", []platform.EnvVar{
		{Key: "connectionString", Content: "redis://..."},
		{Key: "port", Content: "6379"},
	})

	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}

	checker := checkProvision(mock, "proj-1", eng)
	result, err := checker(context.Background(), state.Bootstrap.Plan, state.Bootstrap)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}

	state, err = eng.GetState()
	if err != nil {
		t.Fatalf("GetState after check: %v", err)
	}
	if state.Bootstrap.DiscoveredEnvVars == nil {
		t.Fatal("DiscoveredEnvVars should not be nil")
	}
	if len(state.Bootstrap.DiscoveredEnvVars["db"]) != 2 {
		t.Errorf("db env vars: want 2, got %d", len(state.Bootstrap.DiscoveredEnvVars["db"]))
	}
	if len(state.Bootstrap.DiscoveredEnvVars["cache"]) != 2 {
		t.Errorf("cache env vars: want 2, got %d", len(state.Bootstrap.DiscoveredEnvVars["cache"]))
	}
}

func TestCheckProvision_SimpleMode_NoStage_Pass(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING"},
		{ID: "s2", Name: "db", Status: "RUNNING"},
	}).WithServiceEnv("s2", []platform.EnvVar{{Key: "connectionString", Content: "pg://..."}})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "php-nginx@8.4", BootstrapMode: "simple"},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
			},
		}},
	}

	checker := checkProvision(mock, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass for simple mode (no stage): %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckProvision_DevMode_NoStage_Pass(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING"},
		{ID: "s2", Name: "db", Status: "RUNNING"},
	}).WithServiceEnv("s2", []platform.EnvVar{{Key: "connectionString", Content: "pg://..."}})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "python@3.12", BootstrapMode: "dev"},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
			},
		}},
	}

	checker := checkProvision(mock, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass for dev mode (no stage): %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckDeploy_SimpleMode_NoStage_Pass(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING", Ports: []platform.Port{{Port: 8080}}, SubdomainAccess: true},
	})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "php-nginx@8.4", BootstrapMode: "simple"},
		}},
	}

	checker := checkDeploy(mock, nil, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass for simple mode deploy (no stage): %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckDeploy_ExistingRuntime_StageRunning_Pass(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING", Ports: []platform.Port{{Port: 3000}}, SubdomainAccess: true},
		{ID: "s2", Name: "appstage", Status: "RUNNING", Ports: []platform.Port{{Port: 3000}}, SubdomainAccess: true},
	})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: true},
		}},
	}

	checker := checkDeploy(mock, nil, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass for existing runtime deploy with stage RUNNING: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckProvision_StoreEnvVarsError_Fail(t *testing.T) {
	t.Parallel()
	// Engine without bootstrap session — StoreDiscoveredEnvVars will fail.
	eng := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
			},
		}},
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "RUNNING"},
		{ID: "s2", Name: "appstage", Status: "NEW"},
		{ID: "s3", Name: "db", Status: "RUNNING"},
	}).WithServiceEnv("s3", []platform.EnvVar{
		{Key: "connectionString", Content: "pg://..."},
	})

	checker := checkProvision(mock, "proj-1", eng)
	result, err := checker(context.Background(), plan, &workflow.BootstrapState{})
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail when StoreDiscoveredEnvVars errors")
	}
	// Verify the env_store check is present.
	found := false
	for _, c := range result.Checks {
		if c.Name == "db_env_store" && c.Status == statusFail {
			found = true
		}
	}
	if !found {
		t.Error("expected db_env_store fail check in results")
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
}

func TestCheckProvision_TypeMismatch_Fail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		planType    string
		apiType     string
		isRuntime   bool // true = runtime mismatch, false = dependency mismatch
		depHostname string
		depType     string
		apiDepType  string
	}{
		{
			name:      "runtime_type_mismatch",
			planType:  "nodejs@22",
			apiType:   "postgresql@16",
			isRuntime: true,
		},
		{
			name:        "dependency_type_mismatch",
			planType:    "nodejs@22",
			apiType:     "nodejs@22",
			isRuntime:   false,
			depHostname: "db",
			depType:     "postgresql@16",
			apiDepType:  "mariadb@10.11",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			services := []platform.ServiceStack{
				{
					ID: "s1", Name: "appdev", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName: tt.apiType,
					},
				},
			}
			var deps []workflow.Dependency
			if !tt.isRuntime {
				services = append(services, platform.ServiceStack{
					ID: "s2", Name: tt.depHostname, Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName: tt.apiDepType,
					},
				})
				deps = append(deps, workflow.Dependency{
					Hostname:   tt.depHostname,
					Type:       tt.depType,
					Resolution: "SHARED",
				})
			}
			mock := platform.NewMock().WithServices(services)
			if !tt.isRuntime {
				mock = mock.WithServiceEnv("s2", []platform.EnvVar{{Key: "port", Content: "5432"}})
			}

			plan := &workflow.ServicePlan{
				Targets: []workflow.BootstrapTarget{{
					Runtime:      workflow.RuntimeTarget{DevHostname: "appdev", Type: tt.planType, BootstrapMode: "simple"},
					Dependencies: deps,
				}},
			}

			checker := checkProvision(mock, "proj-1", nil)
			result, err := checker(context.Background(), plan, nil)
			if err != nil {
				t.Fatalf("checker error: %v", err)
			}
			if result.Passed {
				t.Error("expected fail for type mismatch")
			}
			hasTypeFail := false
			for _, c := range result.Checks {
				if c.Status == statusFail && (c.Name == "appdev_type" || c.Name == tt.depHostname+"_type") {
					hasTypeFail = true
				}
			}
			if !hasTypeFail {
				t.Error("expected a _type fail check")
				for _, c := range result.Checks {
					t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
				}
			}
		})
	}
}

func TestCheckProvision_TypeMatch_Pass(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			ID: "s1", Name: "appdev", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName: "nodejs@22",
			},
		},
		{
			ID: "s2", Name: "appstage", Status: "NEW",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName: "nodejs@22",
			},
		},
		{
			ID: "s3", Name: "db", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName: "postgresql@16",
			},
		},
	}).WithServiceEnv("s3", []platform.EnvVar{{Key: "connectionString", Content: "pg://..."}})

	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
			},
		}},
	}

	checker := checkProvision(mock, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("checker error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass when types match, got fail: %s", result.Summary)
		for _, c := range result.Checks {
			t.Logf("  %s: %s %s", c.Name, c.Status, c.Detail)
		}
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
