// OMIT t.Parallel(): tests use t.Setenv to modify global env vars (ZCP_API_KEY, projectId).
package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// severalOrgs is a token in two orgs whose project lives in the second.
func severalOrgs() *platform.UserInfo {
	return &platform.UserInfo{
		Memberships: []platform.ClientMembership{
			{ClientID: "org-first", ClientUserID: "cu-first"},
			{ClientID: "org-owner", ClientUserID: "cu-owner"},
		},
		Email: "test@example.com",
	}
}

func TestResolve_BindsTheOrgThatOwnsTheProject(t *testing.T) {
	owned := platform.Project{ID: "proj-b", Name: "b", Status: "ACTIVE", ClientID: "org-owner"}
	tests := []struct {
		name     string
		scoped   bool
		injected bool
	}{
		{name: "a zcli-scoped project binds its own org", scoped: true},
		{name: "an injected container project binds its own org", injected: true},
		{name: "the one project found across every org binds its own org"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ZCP_API_KEY", "")
			t.Setenv("ZCP_API_HOST", "")
			t.Setenv("ZCP_REGION", "")
			t.Setenv("serviceId", "")
			t.Setenv("projectId", "")
			switch {
			case tt.scoped:
				scope := owned.ID
				t.Setenv("ZCP_ZCLI_DATA_DIR", writeCliData(t, cliData{
					Token:          "zcli-token",
					RegionData:     cliRegion{Name: "prg1", Address: "api.app-prg1.zerops.io"},
					ScopeProjectID: &scope,
				}))
			case tt.injected:
				t.Setenv("ZCP_API_KEY", "some-token")
				t.Setenv("serviceId", "svc-1")
				t.Setenv("projectId", owned.ID)
			default:
				t.Setenv("ZCP_API_KEY", "some-token")
			}

			mock := platform.NewMock().
				WithUserInfo(severalOrgs()).
				WithProjects([]platform.Project{owned}).
				WithProject(&owned)

			info, err := Resolve(context.Background(), mock)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if info.ClientID != "org-owner" {
				t.Errorf("Info.ClientID = %q, want org-owner (the project's org, not the first membership)", info.ClientID)
			}
			if got, _ := mock.OrgID(context.Background()); got != "org-owner" {
				t.Errorf("bound org = %q, want org-owner", got)
			}
		})
	}
}

func TestResolve_UnscopedStartup_CountsProjectsAcrossEveryOrganization(t *testing.T) {
	inFirst := platform.Project{ID: "proj-a", Name: "a", Status: "ACTIVE", ClientID: "org-first"}
	inOwner := platform.Project{ID: "proj-b", Name: "b", Status: "ACTIVE", ClientID: "org-owner"}
	tests := []struct {
		name        string
		projects    []platform.Project
		wantID      string
		wantCode    string
		wantMessage string
	}{
		{name: "no project in any org is the no-project error", wantCode: platform.ErrTokenNoProject},
		{name: "one project outside the first org is that project", projects: []platform.Project{inOwner}, wantID: "proj-b"},
		{
			name:        "a project in each org is too many",
			projects:    []platform.Project{inFirst, inOwner},
			wantCode:    platform.ErrTokenMultiProject,
			wantMessage: "Token accesses 2 projects; use project-scoped token",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ZCP_API_KEY", "some-token")
			t.Setenv("serviceId", "")
			t.Setenv("projectId", "")

			mock := platform.NewMock().WithUserInfo(severalOrgs()).WithProjects(tt.projects)

			info, err := Resolve(context.Background(), mock)
			if tt.wantCode != "" {
				var pe *platform.PlatformError
				if !errors.As(err, &pe) || pe.Code != tt.wantCode {
					t.Fatalf("Resolve error = %v, want code %s", err, tt.wantCode)
				}
				if tt.wantMessage != "" && pe.Message != tt.wantMessage {
					t.Errorf("message = %q, want %q", pe.Message, tt.wantMessage)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if info.ProjectID != tt.wantID {
				t.Errorf("ProjectID = %q, want %q", info.ProjectID, tt.wantID)
			}
		})
	}
}
