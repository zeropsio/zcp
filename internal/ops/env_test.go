// Tests for: plans/analysis/ops.md § ops/env.go
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestEnvGet_Service(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		}).
		WithServiceEnv("svc-1", []platform.EnvVar{
			{ID: "e1", Key: "PORT", Content: "3000"},
			{ID: "e2", Key: "NODE_ENV", Content: "production"},
		})

	result, err := EnvGet(context.Background(), mock, "proj-1", "api", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Scope != "service" {
		t.Errorf("expected scope=service, got %s", result.Scope)
	}
	if result.Hostname != "api" {
		t.Errorf("expected hostname=api, got %s", result.Hostname)
	}
	if len(result.Vars) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(result.Vars))
	}
}

func TestEnvGet_Project(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithProjectEnv([]platform.EnvVar{
			{ID: "pe1", Key: "GLOBAL_KEY", Content: "val"},
		})

	result, err := EnvGet(context.Background(), mock, "proj-1", "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Scope != "project" {
		t.Errorf("expected scope=project, got %s", result.Scope)
	}
	if len(result.Vars) != 1 {
		t.Fatalf("expected 1 var, got %d", len(result.Vars))
	}
}

func TestEnvGet_NoScope(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()

	_, err := EnvGet(context.Background(), mock, "proj-1", "", false)
	if err == nil {
		t.Fatal("expected error for no scope")
	}
}

func TestEnvSet_Service(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		})

	result, err := EnvSet(context.Background(), mock, "proj-1", "api", false, []string{"PORT=3000", "HOST=0.0.0.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Process == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestEnvSet_Project(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()

	result, err := EnvSet(context.Background(), mock, "proj-1", "", true, []string{"GLOBAL=val"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Process == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestEnvSet_InvalidFormat(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		})

	_, err := EnvSet(context.Background(), mock, "proj-1", "api", false, []string{"NOEQUALS"})
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidEnvFormat {
		t.Errorf("expected code %s, got %s", platform.ErrInvalidEnvFormat, pe.Code)
	}
}

func TestEnvDelete_Service_Found(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		}).
		WithServiceEnv("svc-1", []platform.EnvVar{
			{ID: "e1", Key: "DB_HOST", Content: "localhost"},
		})

	result, err := EnvDelete(context.Background(), mock, "proj-1", "api", false, []string{"DB_HOST"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Process == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestEnvDelete_Service_NotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		}).
		WithServiceEnv("svc-1", []platform.EnvVar{
			{ID: "e1", Key: "DB_HOST", Content: "localhost"},
		})

	_, err := EnvDelete(context.Background(), mock, "proj-1", "api", false, []string{"MISSING"})
	if err == nil {
		t.Fatal("expected error for missing env key")
	}
}

func TestEnvDelete_Project(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithProjectEnv([]platform.EnvVar{
			{ID: "pe1", Key: "GLOBAL_KEY", Content: "val"},
		})

	result, err := EnvDelete(context.Background(), mock, "proj-1", "", true, []string{"GLOBAL_KEY"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Process == nil {
		t.Fatal("expected non-nil process")
	}
}
