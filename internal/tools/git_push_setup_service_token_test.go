package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// F5 — GIT_TOKEN moves from the project-singleton env to a SERVICE-scope
// secret on the push source. One token per push-source/repo pair; the
// legacy project key migrates away lazily on the next confirm.

func seedGpsMeta(t *testing.T, stateDir string, gitPushState topology.GitPushState, remoteURL string) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     gitPushState,
		RemoteURL:        remoteURL,
		BootstrapSession: "t",
		BootstrappedAt:   "2026-06-10",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// TestGitPushSetupContainer_WritesServiceScopeSecret pins the F5 write
// site: confirm stores GIT_TOKEN via CreateServiceEnvVar on the push
// SOURCE service — never via CreateProjectEnv — and deletes a legacy
// project-scope GIT_TOKEN when present (lazy one-way migration).
func TestGitPushSetupContainer_WritesServiceScopeSecret(t *testing.T) {
	stateDir := t.TempDir()
	seedGpsMeta(t, stateDir, topology.GitPushUnconfigured, "")

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE"}}).
		WithProjectEnv([]platform.ProjectEnvVar{{ID: "env-legacy", Key: "GIT_TOKEN", Content: "old-project-singleton"}}).
		WithProcess(&platform.Process{ID: "proc-restart-svc-appdev", ActionName: "restart", Status: "FINISHED"})

	ssh := &containerSSHStub{}
	result, _, _ := handleGitPushSetup(
		context.Background(), mock, nil, ssh, "proj1",
		WorkflowInput{
			Service:   "appdev",
			RemoteURL: "https://github.com/me/app.git",
			GitToken:  "github_pat_new",
		}, stateDir, runtime.Info{InContainer: true},
	)
	body := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("confirm failed: %s", body)
	}

	// Service-scope write happened on the push source.
	svcEnvs, _ := mock.GetServiceEnv(context.Background(), "svc-appdev")
	found := false
	for _, e := range svcEnvs {
		if e.Key == "GIT_TOKEN" && e.Content == "github_pat_new" {
			found = true
		}
	}
	if !found {
		t.Errorf("GIT_TOKEN must land as a service-scope env on svc-appdev; got %+v", svcEnvs)
	}
	// Project scope is no longer written.
	if n := len(mock.CapturedProjectEnvCreations); n != 0 {
		t.Errorf("GIT_TOKEN must NOT be written at project scope anymore; CreateProjectEnv captures: %d", n)
	}
	// Legacy project singleton migrated away.
	if n := mock.CallCounts["DeleteProjectEnv"]; n != 1 {
		t.Errorf("legacy project-scope GIT_TOKEN must be deleted (lazy migration); DeleteProjectEnv calls: %d", n)
	}
	if strings.Contains(body, "github_pat_new") {
		t.Error("token value leaked into the response body")
	}
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta.GitPushState != topology.GitPushConfigured {
		t.Errorf("confirm must stamp configured; got %q", meta.GitPushState)
	}
}

// TestGitPushSetup_Confirm_GitTokenIsSensitive pins the platform's 2026-08
// userData model requirement (spec-zerops-env-lifecycle.md §7): the
// service-scope GIT_TOKEN record confirm writes must carry sensitive:true
// like every other ZCP-written service-scope secret.
func TestGitPushSetup_Confirm_GitTokenIsSensitive(t *testing.T) {
	stateDir := t.TempDir()
	seedGpsMeta(t, stateDir, topology.GitPushUnconfigured, "")

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE"}}).
		WithProcess(&platform.Process{ID: "proc-restart-svc-appdev", ActionName: "restart", Status: "FINISHED"})

	ssh := &containerSSHStub{}
	result, _, _ := handleGitPushSetup(
		context.Background(), mock, nil, ssh, "proj1",
		WorkflowInput{
			Service:   "appdev",
			RemoteURL: "https://github.com/me/app.git",
			GitToken:  "github_pat_new",
		}, stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("confirm failed: %s", getTextContent(t, result))
	}

	svcEnvs, _ := mock.GetServiceEnv(context.Background(), "svc-appdev")
	found := false
	for _, e := range svcEnvs {
		if e.Key != "GIT_TOKEN" {
			continue
		}
		found = true
		if !e.Sensitive {
			t.Errorf("GIT_TOKEN Sensitive = false, want true")
		}
	}
	if !found {
		t.Fatal("GIT_TOKEN not found in service env after confirm")
	}
}

// TestGitPushSetupWalkthrough_StateAware pins F5e: the walkthrough on an
// already-configured pair reflects the recorded state and says so,
// instead of hardcoding unconfigured + a full PAT collection.
func TestGitPushSetupWalkthrough_StateAware(t *testing.T) {
	stateDir := t.TempDir()
	seedGpsMeta(t, stateDir, topology.GitPushConfigured, "https://github.com/me/app.git")

	result, _, _ := handleGitPushSetup(
		context.Background(), nil, nil, nil, "proj1",
		WorkflowInput{Service: "appdev"}, stateDir, runtime.Info{InContainer: true},
	)
	body := getTextContent(t, result)
	if !strings.Contains(body, `"alreadyConfigured"`) {
		t.Errorf("configured pair's walkthrough must carry alreadyConfigured note: %s", body)
	}
	if !strings.Contains(body, `"gitPushState":"configured"`) {
		t.Errorf("walkthrough must reflect the real recorded GitPushState: %s", body)
	}
}
