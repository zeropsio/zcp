package platform

import (
	"testing"

	"github.com/zeropsio/zerops-go/dto/output"
	zgotypes "github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/enum"
)

// TestMapIntegrationOutput_GithubConfigured ensures GitHub output → configured
// state with all fields mapped per Phase A B.2 wire shape.
func TestMapIntegrationOutput_GithubConfigured(t *testing.T) {
	t.Parallel()
	gh := &output.GithubIntegration{
		RepositoryFullName: zgotypes.NewString("krls2020/myapp"),
		EventType:          enum.GithubIntegrationEventTypeEnumTag,
		BranchName:         zgotypes.StringNull{},
		TagRegex:           zgotypes.NewStringNull("^v\\d+\\.\\d+\\.\\d+$"),
		IsActive:           zgotypes.NewBool(true),
		ZeropsYamlSetup:    zgotypes.NewStringNull("prod"),
	}
	got := mapIntegrationOutput(gh, nil)
	wantStatus := IntegrationStatus{
		State:              IntegrationConfigured,
		Provider:           IntegrationProviderGitHub,
		RepositoryFullName: "krls2020/myapp",
		EventType:          IntegrationEventTag,
		BranchName:         "",
		TagRegex:           "^v\\d+\\.\\d+\\.\\d+$",
		ZeropsYamlSetup:    "prod",
		IsActive:           true,
	}
	if got != wantStatus {
		t.Errorf("got %+v\n want %+v", got, wantStatus)
	}
}

// TestMapIntegrationOutput_GithubBranchConfigured covers BRANCH event type
// path — branchName filled, tagRegex empty.
func TestMapIntegrationOutput_GithubBranchConfigured(t *testing.T) {
	t.Parallel()
	gh := &output.GithubIntegration{
		RepositoryFullName: zgotypes.NewString("krls2020/myapp"),
		EventType:          enum.GithubIntegrationEventTypeEnumBranch,
		BranchName:         zgotypes.NewStringNull("main"),
		TagRegex:           zgotypes.StringNull{},
		IsActive:           zgotypes.NewBool(true),
		ZeropsYamlSetup:    zgotypes.NewStringNull("prod"),
	}
	got := mapIntegrationOutput(gh, nil)
	if got.State != IntegrationConfigured {
		t.Errorf("state: got %q want %q", got.State, IntegrationConfigured)
	}
	if got.EventType != IntegrationEventBranch {
		t.Errorf("eventType: got %q want %q", got.EventType, IntegrationEventBranch)
	}
	if got.BranchName != "main" {
		t.Errorf("branchName: got %q want main", got.BranchName)
	}
	if got.TagRegex != "" {
		t.Errorf("tagRegex: got %q want empty", got.TagRegex)
	}
}

// TestMapIntegrationOutput_GitlabConfigured ensures GitLab output → configured
// with provider field flipped.
func TestMapIntegrationOutput_GitlabConfigured(t *testing.T) {
	t.Parallel()
	gl := &output.GitlabIntegration{
		RepositoryFullName: zgotypes.NewString("krls2020/myapp"),
		EventType:          enum.GitlabIntegrationEventTypeEnumTag,
		BranchName:         zgotypes.StringNull{},
		TagRegex:           zgotypes.NewStringNull("^v\\d+\\.\\d+\\.\\d+$"),
		IsActive:           zgotypes.NewBool(true),
		ZeropsYamlSetup:    zgotypes.NewStringNull("prod"),
	}
	got := mapIntegrationOutput(nil, gl)
	if got.Provider != IntegrationProviderGitLab {
		t.Errorf("provider: got %q want %q", got.Provider, IntegrationProviderGitLab)
	}
	if got.State != IntegrationConfigured {
		t.Errorf("state: got %q want %q", got.State, IntegrationConfigured)
	}
	if got.RepositoryFullName != "krls2020/myapp" {
		t.Errorf("repositoryFullName: got %q", got.RepositoryFullName)
	}
}

// TestMapIntegrationOutput_BothNil_DefaultsNotConfigured guards the defensive
// branch — both fields nil should produce NotConfigured, never panic.
func TestMapIntegrationOutput_BothNil_DefaultsNotConfigured(t *testing.T) {
	t.Parallel()
	got := mapIntegrationOutput(nil, nil)
	if got.State != IntegrationNotConfigured {
		t.Errorf("state: got %q want %q", got.State, IntegrationNotConfigured)
	}
	if got.Provider != "" {
		t.Errorf("provider should be empty; got %q", got.Provider)
	}
}

// TestApiCodeNoExternalRepositoryIntegration_Constant pins the wire-error
// code value — the platform contract this wrapper depends on for state
// mapping (Phase A B.1). Changes to this constant must update the spec doc.
func TestApiCodeNoExternalRepositoryIntegration_Constant(t *testing.T) {
	t.Parallel()
	if apiCodeNoExternalRepositoryIntegration != "noExternalRepositoryIntegration" {
		t.Errorf("apiCodeNoExternalRepositoryIntegration drifted from spec: got %q", apiCodeNoExternalRepositoryIntegration)
	}
}
