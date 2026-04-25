package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestExportProject_Success(t *testing.T) {
	t.Parallel()

	exportYAML := `project:
  name: copy of myproject
  corePackage: LIGHT
services:
  - hostname: app
    type: nodejs@22
  - hostname: db
    type: postgresql@16
`
	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "app", ProjectID: "proj-1", Status: "RUNNING", Mode: "HA",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			SubdomainAccess:      true},
		{ID: "svc-2", Name: "db", ProjectID: "proj-1", Status: "RUNNING", Mode: "NON_HA",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithServices(services).
		WithExportYAML(exportYAML)

	result, err := ExportProject(context.Background(), mock, "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ProjectName != "myproject" {
		t.Errorf("expected projectName=myproject, got %s", result.ProjectName)
	}
	if result.ExportYAML != exportYAML {
		t.Errorf("expected export YAML to match input")
	}
	if len(result.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(result.Services))
	}

	// Check runtime service. Mode is suppressed for runtime services
	// (non-load-bearing — replica count is governed by minContainers,
	// not by HA/NON_HA). See discover.go::buildSummaryServiceInfo.
	app := result.Services[0]
	if app.Hostname != "app" {
		t.Errorf("expected hostname=app, got %s", app.Hostname)
	}
	if app.Mode != "" {
		t.Errorf("expected empty mode for runtime service, got %q", app.Mode)
	}
	if app.IsInfrastructure {
		t.Error("expected app to not be infrastructure")
	}
	if !app.SubdomainEnabled {
		t.Error("expected app subdomain enabled")
	}

	// Check managed service.
	db := result.Services[1]
	if db.Hostname != "db" {
		t.Errorf("expected hostname=db, got %s", db.Hostname)
	}
	if db.Mode != "NON_HA" {
		t.Errorf("expected mode=NON_HA, got %s", db.Mode)
	}
	if !db.IsInfrastructure {
		t.Error("expected db to be infrastructure")
	}
}

func TestExportProject_ExportAPIError(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: statusActive}).
		WithError("GetProjectExport", platform.NewPlatformError(
			platform.ErrServiceNotFound, "project not found", ""))

	_, err := ExportProject(context.Background(), mock, "proj-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "export project") {
		t.Errorf("expected 'export project' in error, got: %s", err.Error())
	}
}

func TestExportResult_RuntimeAndManagedServices(t *testing.T) {
	t.Parallel()

	result := &ExportResult{
		Services: []ExportedService{
			{Hostname: "app", Type: "nodejs@22", IsInfrastructure: false},
			{Hostname: "worker", Type: "nodejs@22", IsInfrastructure: false},
			{Hostname: "db", Type: "postgresql@16", IsInfrastructure: true},
			{Hostname: "cache", Type: "valkey@7.2", IsInfrastructure: true},
		},
	}

	runtimes := result.RuntimeServices()
	if len(runtimes) != 2 {
		t.Errorf("expected 2 runtime services, got %d", len(runtimes))
	}

	managed := result.ManagedServices()
	if len(managed) != 2 {
		t.Errorf("expected 2 managed services, got %d", len(managed))
	}

	hostnames := result.ServiceHostnames()
	if hostnames != "app, worker, db, cache" {
		t.Errorf("expected 'app, worker, db, cache', got '%s'", hostnames)
	}
}

func TestExportService_Success(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "app", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}
	serviceYAML := "services:\n  - hostname: app\n    type: nodejs@22\n"

	mock := platform.NewMock().
		WithServices(services).
		WithServiceExportYAML(serviceYAML)

	yaml, err := ExportService(context.Background(), mock, "proj-1", "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if yaml != serviceYAML {
		t.Errorf("expected service YAML to match")
	}
}

func TestExportService_NotFound(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "app", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}

	mock := platform.NewMock().WithServices(services)

	_, err := ExportService(context.Background(), mock, "proj-1", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent service")
	}
}
