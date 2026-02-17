// Tests for: plans/analysis/platform.md §8 — auth.Resolve behavioral contracts
//
// OMIT t.Parallel(): tests use t.Setenv to modify global env vars (ZCP_API_KEY etc.)
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// testUserInfo returns a standard UserInfo for tests.
func testUserInfo() *platform.UserInfo {
	return &platform.UserInfo{
		ID:       "client-123",
		FullName: "Test User",
		Email:    "test@example.com",
	}
}

// testProject returns a standard Project for tests.
func testProject() platform.Project {
	return platform.Project{
		ID:     "proj-456",
		Name:   "my-project",
		Status: "ACTIVE",
	}
}

// writeCliData creates a temporary cli.data file and returns its directory path.
func writeCliData(t *testing.T, data cliData) string {
	t.Helper()
	dir := t.TempDir()
	zeropsDir := filepath.Join(dir, "zerops")
	if err := os.MkdirAll(zeropsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(zeropsDir, "cli.data"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResolve_EnvVar_SingleProject(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		wantProject string
		wantRegion  string
		wantAPIHost string
	}{
		{
			name:        "happy path with single project",
			token:       "test-token-abc",
			wantProject: "proj-456",
			wantRegion:  "prg1",
			wantAPIHost: "api.app-prg1.zerops.io",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ZCP_API_KEY", tt.token)
			t.Setenv("ZCP_API_HOST", "")
			t.Setenv("ZCP_REGION", "")

			mock := platform.NewMock().
				WithUserInfo(testUserInfo()).
				WithProjects([]platform.Project{testProject()})

			info, err := Resolve(context.Background(), mock)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.Token != tt.token {
				t.Errorf("Token = %q, want %q", info.Token, tt.token)
			}
			if info.ProjectID != tt.wantProject {
				t.Errorf("ProjectID = %q, want %q", info.ProjectID, tt.wantProject)
			}
			if info.ProjectName != "my-project" {
				t.Errorf("ProjectName = %q, want %q", info.ProjectName, "my-project")
			}
			if info.ClientID != "client-123" {
				t.Errorf("ClientID = %q, want %q", info.ClientID, "client-123")
			}
			if info.Email != "test@example.com" {
				t.Errorf("Email = %q, want %q", info.Email, "test@example.com")
			}
			if info.FullName != "Test User" {
				t.Errorf("FullName = %q, want %q", info.FullName, "Test User")
			}
			if info.Region != tt.wantRegion {
				t.Errorf("Region = %q, want %q", info.Region, tt.wantRegion)
			}
			if info.APIHost != tt.wantAPIHost {
				t.Errorf("APIHost = %q, want %q", info.APIHost, tt.wantAPIHost)
			}
		})
	}
}

func TestResolve_EnvVar_NoProject(t *testing.T) {
	tests := []struct {
		name     string
		projects []platform.Project
		wantCode string
	}{
		{
			name:     "zero projects returns TOKEN_NO_PROJECT",
			projects: []platform.Project{},
			wantCode: platform.ErrTokenNoProject,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ZCP_API_KEY", "some-token")

			mock := platform.NewMock().
				WithUserInfo(testUserInfo()).
				WithProjects(tt.projects)

			_, err := Resolve(context.Background(), mock)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("expected PlatformError, got %T: %v", err, err)
			}
			if pe.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q", pe.Code, tt.wantCode)
			}
		})
	}
}

func TestResolve_EnvVar_MultiProject(t *testing.T) {
	tests := []struct {
		name     string
		projects []platform.Project
		wantCode string
	}{
		{
			name: "two projects returns TOKEN_MULTI_PROJECT",
			projects: []platform.Project{
				{ID: "p1", Name: "first", Status: "ACTIVE"},
				{ID: "p2", Name: "second", Status: "ACTIVE"},
			},
			wantCode: platform.ErrTokenMultiProject,
		},
		{
			name: "three projects returns TOKEN_MULTI_PROJECT",
			projects: []platform.Project{
				{ID: "p1", Name: "first", Status: "ACTIVE"},
				{ID: "p2", Name: "second", Status: "ACTIVE"},
				{ID: "p3", Name: "third", Status: "ACTIVE"},
			},
			wantCode: platform.ErrTokenMultiProject,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ZCP_API_KEY", "some-token")

			mock := platform.NewMock().
				WithUserInfo(testUserInfo()).
				WithProjects(tt.projects)

			_, err := Resolve(context.Background(), mock)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("expected PlatformError, got %T: %v", err, err)
			}
			if pe.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q", pe.Code, tt.wantCode)
			}
		})
	}
}

func TestResolve_EnvVar_InvalidToken(t *testing.T) {
	tests := []struct {
		name    string
		mockErr error
	}{
		{
			name:    "GetUserInfo fails with auth error",
			mockErr: platform.NewPlatformError(platform.ErrAuthTokenExpired, "token expired", "Check token validity"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ZCP_API_KEY", "bad-token")

			mock := platform.NewMock().
				WithError("GetUserInfo", tt.mockErr)

			_, err := Resolve(context.Background(), mock)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("expected PlatformError, got %T: %v", err, err)
			}
			if pe.Code != platform.ErrAuthTokenExpired {
				t.Errorf("error code = %q, want %q", pe.Code, platform.ErrAuthTokenExpired)
			}
		})
	}
}

func TestResolve_EnvVar_CustomAPIHost(t *testing.T) {
	tests := []struct {
		name    string
		apiHost string
		want    string
	}{
		{
			name:    "custom API host from env",
			apiHost: "api.custom.zerops.io",
			want:    "api.custom.zerops.io",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ZCP_API_KEY", "some-token")
			t.Setenv("ZCP_API_HOST", tt.apiHost)

			mock := platform.NewMock().
				WithUserInfo(testUserInfo()).
				WithProjects([]platform.Project{testProject()})

			info, err := Resolve(context.Background(), mock)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.APIHost != tt.want {
				t.Errorf("APIHost = %q, want %q", info.APIHost, tt.want)
			}
		})
	}
}

func TestResolve_EnvVar_CustomRegion(t *testing.T) {
	tests := []struct {
		name   string
		region string
		want   string
	}{
		{
			name:   "custom region from env",
			region: "fra1",
			want:   "fra1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ZCP_API_KEY", "some-token")
			t.Setenv("ZCP_REGION", tt.region)

			mock := platform.NewMock().
				WithUserInfo(testUserInfo()).
				WithProjects([]platform.Project{testProject()})

			info, err := Resolve(context.Background(), mock)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.Region != tt.want {
				t.Errorf("Region = %q, want %q", info.Region, tt.want)
			}
		})
	}
}
