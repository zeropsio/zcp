// Tests for: plans/analysis/platform.md §8 — auth.Resolve behavioral contracts
//
// OMIT t.Parallel(): tests use t.Setenv to modify global env vars (ZCP_API_KEY etc.)
package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestResolve_ZcliFallback_Success(t *testing.T) {
	tests := []struct {
		name        string
		cliToken    string
		cliRegion   string
		cliAddress  string
		wantRegion  string
		wantAPIHost string
	}{
		{
			name:        "reads token and region from cli.data",
			cliToken:    "zcli-token-xyz",
			cliRegion:   "prg1",
			cliAddress:  "api.app-prg1.zerops.io",
			wantRegion:  "prg1",
			wantAPIHost: "api.app-prg1.zerops.io",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ZCP_API_KEY", "")
			t.Setenv("ZCP_API_HOST", "")
			t.Setenv("ZCP_REGION", "")

			dir := writeCliData(t, cliData{
				Token: tt.cliToken,
				RegionData: cliRegion{
					Name:    tt.cliRegion,
					Address: tt.cliAddress,
				},
				ScopeProjectID: nil,
			})
			t.Setenv("ZCP_ZCLI_DATA_DIR", dir)

			mock := platform.NewMock().
				WithUserInfo(testUserInfo()).
				WithProjects([]platform.Project{testProject()})

			info, err := Resolve(context.Background(), mock)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.Token != tt.cliToken {
				t.Errorf("Token = %q, want %q", info.Token, tt.cliToken)
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

func TestResolve_ZcliFallback_ScopeProject(t *testing.T) {
	tests := []struct {
		name          string
		scopeProject  string
		wantProjectID string
		wantName      string
	}{
		{
			name:          "ScopeProjectID set uses GetProject",
			scopeProject:  "scoped-proj-789",
			wantProjectID: "scoped-proj-789",
			wantName:      "scoped-project",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ZCP_API_KEY", "")
			t.Setenv("ZCP_API_HOST", "")
			t.Setenv("ZCP_REGION", "")

			scopeID := tt.scopeProject
			dir := writeCliData(t, cliData{
				Token: "zcli-token-scoped",
				RegionData: cliRegion{
					Name:    "prg1",
					Address: "api.app-prg1.zerops.io",
				},
				ScopeProjectID: &scopeID,
			})
			t.Setenv("ZCP_ZCLI_DATA_DIR", dir)

			mock := platform.NewMock().
				WithUserInfo(testUserInfo()).
				WithProject(&platform.Project{
					ID:     tt.wantProjectID,
					Name:   tt.wantName,
					Status: "ACTIVE",
				})

			info, err := Resolve(context.Background(), mock)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.ProjectID != tt.wantProjectID {
				t.Errorf("ProjectID = %q, want %q", info.ProjectID, tt.wantProjectID)
			}
			if info.ProjectName != tt.wantName {
				t.Errorf("ProjectName = %q, want %q", info.ProjectName, tt.wantName)
			}
		})
	}
}

func TestResolve_ZcliFallback_NullScope(t *testing.T) {
	tests := []struct {
		name        string
		projects    []platform.Project
		wantProject string
	}{
		{
			name:        "null ScopeProjectID falls back to ListProjects",
			projects:    []platform.Project{testProject()},
			wantProject: "proj-456",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ZCP_API_KEY", "")
			t.Setenv("ZCP_API_HOST", "")
			t.Setenv("ZCP_REGION", "")

			dir := writeCliData(t, cliData{
				Token: "zcli-token-null-scope",
				RegionData: cliRegion{
					Name:    "prg1",
					Address: "api.app-prg1.zerops.io",
				},
				ScopeProjectID: nil,
			})
			t.Setenv("ZCP_ZCLI_DATA_DIR", dir)

			mock := platform.NewMock().
				WithUserInfo(testUserInfo()).
				WithProjects(tt.projects)

			info, err := Resolve(context.Background(), mock)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.ProjectID != tt.wantProject {
				t.Errorf("ProjectID = %q, want %q", info.ProjectID, tt.wantProject)
			}
		})
	}
}

func TestResolve_NoAuth(t *testing.T) {
	tests := []struct {
		name     string
		wantCode string
	}{
		{
			name:     "no env var and no cli.data returns AUTH_REQUIRED",
			wantCode: platform.ErrAuthRequired,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ZCP_API_KEY", "")
			t.Setenv("ZCP_API_HOST", "")
			t.Setenv("ZCP_REGION", "")
			// Point to a nonexistent directory so cli.data won't be found
			t.Setenv("ZCP_ZCLI_DATA_DIR", "/nonexistent/path/that/does/not/exist")

			mock := platform.NewMock()

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
