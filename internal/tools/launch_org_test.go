package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// The prod project is created in, and the per-project role granted with
// the clientUser of, the org that owns the source project: the admin
// client is built for that org, never the launch key's first membership.
func TestHandleLaunchProduction_LaunchAdminTargetsTheOrgOwningTheProject(t *testing.T) {
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	var gotOrg string
	restore := setProjectAdminClientFactory(func(_, _, clientID string) (platform.ProjectAdminClient, error) {
		gotOrg = clientID
		return nil, errors.New("stop after construction")
	})
	defer restore()

	client := newLaunchMockClient().WithProjectEnv([]platform.ProjectEnvVar{
		{Key: "LOG_LEVEL", Content: "info"},
	})
	client.BindOrg("org-owner")

	if _, _, err := handleLaunchProduction(context.Background(), "source-project-id", client, nil, nil, completeLaunchInput(), stateDir, runtime.Info{}, nil, ""); err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	if gotOrg != "org-owner" {
		t.Errorf("admin client org = %q, want org-owner (the org owning the source project)", gotOrg)
	}
}

func TestValidateExistingProdTokenScope_TargetsTheOrgOwningTheTargetProject(t *testing.T) {
	target := platform.NewMock().
		WithUserInfo(&platform.UserInfo{Memberships: []platform.ClientMembership{
			{ClientID: "org-first", ClientUserID: "cu-first"},
			{ClientID: "org-owner", ClientUserID: "cu-owner"},
		}}).
		WithProjects([]platform.Project{{ID: "target-id", Name: "prod", ClientID: "org-owner"}})

	if err := validateExistingProdTokenScope(context.Background(), target, "target-id"); err != nil {
		t.Fatalf("validateExistingProdTokenScope: %v", err)
	}
	if got, _ := target.OrgID(context.Background()); got != "org-owner" {
		t.Errorf("target client org = %q, want org-owner", got)
	}
}
