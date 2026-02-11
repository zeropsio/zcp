//go:build api

package platform_test

import (
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/platform/apitest"
)

// Tests for: design/zcp-prd.md section 4.3 (ZeropsClient API Contract)

func TestAPI_GetUserInfo(t *testing.T) {
	h := apitest.New(t)
	info, err := h.Client().GetUserInfo(h.Ctx())
	if err != nil {
		t.Fatalf("GetUserInfo failed: %v", err)
	}
	if info.ID == "" {
		t.Error("ID is empty")
	}
	if info.Email == "" {
		t.Error("Email is empty")
	}
}

func TestAPI_ListProjects(t *testing.T) {
	h := apitest.New(t)
	info, err := h.Client().GetUserInfo(h.Ctx())
	if err != nil {
		t.Fatalf("GetUserInfo failed: %v", err)
	}
	projects, err := h.Client().ListProjects(h.Ctx(), info.ID)
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	if len(projects) == 0 {
		t.Fatal("no projects returned")
	}
	for _, p := range projects {
		if p.ID == "" {
			t.Error("project ID is empty")
		}
		if p.Name == "" {
			t.Error("project Name is empty")
		}
		if p.Status == "" {
			t.Error("project Status is empty")
		}
	}
}

func TestAPI_GetProject(t *testing.T) {
	h := apitest.New(t)
	proj, err := h.Client().GetProject(h.Ctx(), h.ProjectID())
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}
	if proj.ID != h.ProjectID() {
		t.Errorf("ID = %q, want %q", proj.ID, h.ProjectID())
	}
	if proj.Name == "" {
		t.Error("Name is empty")
	}
}

func TestAPI_ListServices(t *testing.T) {
	h := apitest.New(t)
	services, err := h.Client().ListServices(h.Ctx(), h.ProjectID())
	if err != nil {
		t.Fatalf("ListServices failed: %v", err)
	}
	for _, s := range services {
		if s.ID == "" {
			t.Error("service ID is empty")
		}
		if s.Name == "" {
			t.Error("service Name is empty")
		}
		if s.Status == "" {
			t.Error("service Status is empty")
		}
		if s.ProjectID != h.ProjectID() {
			t.Errorf("ProjectID = %q, want %q", s.ProjectID, h.ProjectID())
		}
		if s.ServiceStackTypeInfo.ServiceStackTypeVersionName == "" {
			t.Error("ServiceStackTypeVersionName is empty")
		}
	}
}

func TestAPI_GetService(t *testing.T) {
	h := apitest.New(t)
	services, err := h.Client().ListServices(h.Ctx(), h.ProjectID())
	if err != nil {
		t.Fatalf("ListServices failed: %v", err)
	}
	if len(services) == 0 {
		t.Skip("no services to test GetService")
	}
	svc, err := h.Client().GetService(h.Ctx(), services[0].ID)
	if err != nil {
		t.Fatalf("GetService failed: %v", err)
	}
	if svc.ID != services[0].ID {
		t.Errorf("ID = %q, want %q", svc.ID, services[0].ID)
	}
	if svc.Name == "" {
		t.Error("Name is empty")
	}
}

func TestAPI_GetServiceEnv(t *testing.T) {
	h := apitest.New(t)
	services, err := h.Client().ListServices(h.Ctx(), h.ProjectID())
	if err != nil {
		t.Fatalf("ListServices failed: %v", err)
	}
	if len(services) == 0 {
		t.Skip("no services to test GetServiceEnv")
	}
	envs, err := h.Client().GetServiceEnv(h.Ctx(), services[0].ID)
	if err != nil {
		t.Fatalf("GetServiceEnv failed: %v", err)
	}
	// May be empty, but must not be nil
	if envs == nil {
		t.Error("envs is nil, want empty slice")
	}
}

func TestAPI_GetProjectEnv(t *testing.T) {
	h := apitest.New(t)
	envs, err := h.Client().GetProjectEnv(h.Ctx(), h.ProjectID())
	if err != nil {
		t.Fatalf("GetProjectEnv failed: %v", err)
	}
	if envs == nil {
		t.Error("envs is nil, want empty slice")
	}
}

func TestAPI_GetProjectLog(t *testing.T) {
	h := apitest.New(t)
	access, err := h.Client().GetProjectLog(h.Ctx(), h.ProjectID())
	if err != nil {
		t.Fatalf("GetProjectLog failed: %v", err)
	}
	if access.URL == "" {
		t.Error("URL is empty")
	}
	if access.AccessToken == "" {
		t.Error("AccessToken is empty")
	}
}

func TestAPI_SearchProcesses(t *testing.T) {
	h := apitest.New(t)
	events, err := h.Client().SearchProcesses(h.Ctx(), h.ProjectID(), 10)
	if err != nil {
		t.Fatalf("SearchProcesses failed: %v", err)
	}
	for _, e := range events {
		if e.ID == "" {
			t.Error("event ID is empty")
		}
		if e.Status == "" {
			t.Error("event Status is empty")
		}
		if e.ActionName == "" {
			t.Error("event ActionName is empty")
		}
		// Verify status normalization
		if e.Status == "DONE" {
			t.Errorf("raw DONE status not normalized to FINISHED")
		}
		if e.Status == "CANCELLED" {
			t.Errorf("raw CANCELLED status not normalized to CANCELED")
		}
	}
}

func TestAPI_SearchAppVersions(t *testing.T) {
	h := apitest.New(t)
	events, err := h.Client().SearchAppVersions(h.Ctx(), h.ProjectID(), 10)
	if err != nil {
		t.Fatalf("SearchAppVersions failed: %v", err)
	}
	for _, e := range events {
		if e.ID == "" {
			t.Error("event ID is empty")
		}
		if e.Status == "" {
			t.Error("event Status is empty")
		}
		if e.Source == "" {
			t.Error("event Source is empty")
		}
	}
}

func TestAPI_GetProcess_NotFound(t *testing.T) {
	h := apitest.New(t)
	_, err := h.Client().GetProcess(h.Ctx(), "00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrProcessNotFound {
		t.Errorf("code = %q, want %q", pe.Code, platform.ErrProcessNotFound)
	}
}

func TestAPI_InvalidToken(t *testing.T) {
	client, err := platform.NewZeropsClient("invalid-token", "api.app-prg1.zerops.io")
	if err != nil {
		t.Fatalf("NewZeropsClient failed: %v", err)
	}
	h := apitest.New(t)
	_, err = client.GetUserInfo(h.Ctx())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrAuthTokenExpired {
		t.Errorf("code = %q, want %q", pe.Code, platform.ErrAuthTokenExpired)
	}
}
