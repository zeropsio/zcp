//go:build api

package platform_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// Live API tests for ProjectAdminClient — opt-in via:
//   - go test ./internal/platform/ -tags api -run TestProjectAdminClient_
//   - env ZCP_E2E_PROD_LAUNCH=1
//   - env ZCP_LAUNCH_KEY=<admin-token>  (NOT the project-scoped ZCP_API_KEY;
//     this MUST be a Zerops API token with canCreateProjects=true —
//     Custom access per project + Allow creating projects toggle ON)
//
// These tests provision throwaway projects in the authenticated org,
// observe real API behavior, and tear down. Each test is responsible for
// its own cleanup via t.Cleanup.
//
// Phase A spike findings are the predicted contracts these tests verify.
// See docs/spec-launch-production-platform-spike.md §"What still needs
// admin-token verification".

const (
	throwawayProjectPrefix = "zcp-launch-spike-"
	apiHostEnv             = "ZCP_API_HOST"
	defaultAPIHostForSpike = "api.app-prg1.zerops.io"
)

func requireLaunchKey(t *testing.T) string {
	t.Helper()
	if os.Getenv("ZCP_E2E_PROD_LAUNCH") != "1" {
		t.Skip("ZCP_E2E_PROD_LAUNCH != 1 (opt-in)")
	}
	key := os.Getenv("ZCP_LAUNCH_KEY")
	if key == "" {
		t.Skip("ZCP_LAUNCH_KEY not set (admin one-shot token required)")
	}
	return key
}

func apiHost() string {
	if h := os.Getenv(apiHostEnv); h != "" {
		return h
	}
	return defaultAPIHostForSpike
}

// newAdminClient constructs a ProjectAdminClient and registers its Close
// via t.Cleanup so admin survives ALL test-body t.Cleanup hooks (Delete
// + verify). t.Cleanup hooks run in LIFO order: tests register DeleteProject
// AFTER this constructor, so Delete fires first, then Close zeros the
// SDK handler last.
func newAdminClient(t *testing.T) platform.ProjectAdminClient {
	t.Helper()
	key := requireLaunchKey(t)
	admin, err := platform.NewProjectAdminClient(key, apiHost())
	if err != nil {
		t.Fatalf("NewProjectAdminClient: %v", err)
	}
	t.Cleanup(admin.Close)
	return admin
}

// throwawayName returns a unique project name for this test run.
func throwawayName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%s%s-%d", throwawayProjectPrefix, sanitize(t.Name()), time.Now().UnixNano())
}

func sanitize(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}

// minimalImportYAML returns a minimal valid import yaml for spike testing.
// One probe service that starts without code so import + delete cycle is fast.
func minimalImportYAML(projectName string) string {
	return fmt.Sprintf(`project:
  name: %s
  tags:
    - zcp-launch-spike
services:
  - hostname: probe
    type: nodejs@22
    startWithoutCode: true
    minContainers: 1
`, projectName)
}

// TestProjectAdminClient_CreateAndImport_Live verifies the create+import
// happy path against real Zerops API. Confirms Phase A.1 SDK-derived
// contracts: synchronous response with projectId + per-service IDs +
// per-service async processes.
func TestProjectAdminClient_CreateAndImport_Live(t *testing.T) {
	admin := newAdminClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	name := throwawayName(t)
	yaml := minimalImportYAML(name)

	result, err := admin.CreateAndImportProject(ctx, yaml, platform.CreateOpts{
		Location: "eu-central",
		Tags:     []string{"zcp-spike"},
	})
	if err != nil {
		t.Fatalf("CreateAndImportProject: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if result.ProjectID == "" {
		t.Error("expected non-empty ProjectID")
	}
	if result.ProjectName != name {
		t.Errorf("ProjectName: got %q want %q", result.ProjectName, name)
	}
	if len(result.ServiceStacks) == 0 {
		t.Error("expected at least one service stack")
	}
	for i, s := range result.ServiceStacks {
		if s.ID == "" {
			t.Errorf("service[%d].ID empty", i)
		}
		if s.Name == "" {
			t.Errorf("service[%d].Name empty", i)
		}
	}

	// Cleanup: delete the throwaway. Don't poll completion — test is fast,
	// platform handles eventual delete asynchronously.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.DeleteProject(ctx, result.ProjectID); err != nil {
			t.Logf("cleanup DeleteProject %s: %v", result.ProjectID, err)
		}
	})
}

// TestProjectAdminClient_CreateAndImport_RejectsInvalidYaml verifies
// server-side schema validation rejects malformed input.
func TestProjectAdminClient_CreateAndImport_RejectsInvalidYaml(t *testing.T) {
	admin := newAdminClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Yaml with required project.name missing — platform should reject.
	bad := `project:
  description: missing name
services: []
`
	_, err := admin.CreateAndImportProject(ctx, bad, platform.CreateOpts{})
	if err == nil {
		t.Fatal("expected schema validation error for yaml missing project.name")
	}
	// Don't pin error message text — we just need failure.
}

// TestProjectAdminClient_DeleteProject_LiveCycle verifies the async-process
// delete pattern. Phase A.2 predicted output.Process; verify mapping.
func TestProjectAdminClient_DeleteProject_LiveCycle(t *testing.T) {
	admin := newAdminClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create throwaway
	name := throwawayName(t)
	result, err := admin.CreateAndImportProject(ctx, minimalImportYAML(name), platform.CreateOpts{Location: "eu-central"})
	if err != nil {
		t.Fatalf("create throwaway: %v", err)
	}

	// Delete and observe Process
	proc, err := admin.DeleteProject(ctx, result.ProjectID)
	if err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if proc == nil {
		t.Fatal("nil process")
	}
	if proc.ID == "" {
		t.Error("expected non-empty Process.ID")
	}
	if proc.Status == "" {
		t.Error("expected non-empty Process.Status")
	}
}

// TestProjectAdminClient_LaunchKeyRejectedAtConstruction verifies that an
// invalid key fails at NewProjectAdminClient, not on the first method call.
func TestProjectAdminClient_LaunchKeyRejectedAtConstruction(t *testing.T) {
	if os.Getenv("ZCP_E2E_PROD_LAUNCH") != "1" {
		t.Skip("ZCP_E2E_PROD_LAUNCH != 1 (opt-in)")
	}
	_, err := platform.NewProjectAdminClient("not-a-real-key-just-text", apiHost())
	if err == nil {
		t.Fatal("expected construction-time error for invalid key")
	}
}

// TestProjectAdminClient_GetServiceEnvKeys_OmitsValues verifies P-LP-5
// against the live API. EnvKey having no Value field is a compile-time
// guarantee pinned at the type-definition level + via the mock unit test
// (TestMockProjectAdmin_GetServiceEnvKeys_ReturnsNoValues); this live
// test additionally pins that GetProjectEnvKeys against a freshly
// created project returns a usable response shape.
//
// A.10 fix: project.userRoles in import yaml is silently dropped by
// PostClientProjectImport. Role assignment happens via GrantSelfRole
// AFTER create — separate PutClientUserRoles API call. This test
// exercises that path end-to-end.
func TestProjectAdminClient_GetServiceEnvKeys_OmitsValues(t *testing.T) {
	admin := newAdminClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Create throwaway with project-level user envs. Role assignment
	// for env-read happens via GrantSelfRole AFTER create (A.10).
	name := throwawayName(t)
	yaml := fmt.Sprintf(`project:
  name: %s
  tags:
    - zcp-launch-spike
  envVariables:
    PROBE_VAR: spike-value-not-real
services:
  - hostname: probe
    type: nodejs@22
    startWithoutCode: true
    minContainers: 1
`, name)
	result, err := admin.CreateAndImportProject(ctx, yaml, platform.CreateOpts{Location: "eu-central"})
	if err != nil {
		t.Fatalf("create throwaway: %v", err)
	}

	// A.10: grant self ADMIN on the new project.
	if err := admin.GrantSelfRole(ctx, result.ProjectID, "ADMIN"); err != nil {
		t.Fatalf("GrantSelfRole: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = admin.DeleteProject(ctx, result.ProjectID)
	})

	// A.10: grant launching clientUser ADMIN on the new project. Note:
	// the platform auto-assigns OWNER to the creating clientUser, so
	// this is informational/audit for the launch workflow; the API call
	// still succeeds.
	if err := admin.GrantSelfRole(ctx, result.ProjectID, "ADMIN"); err != nil {
		t.Fatalf("GrantSelfRole: %v", err)
	}

	// Poll until PROBE_VAR shows up in env-read. Project creation is
	// async — env-list is empty briefly even after the project itself
	// becomes readable. Up to 60s wait.
	var keys []platform.EnvKey
	var probeFound bool
	for range 60 {
		var listErr error
		keys, listErr = admin.GetProjectEnvKeys(ctx, result.ProjectID)
		if listErr == nil {
			for _, k := range keys {
				if k.Key == "PROBE_VAR" {
					probeFound = true
					break
				}
			}
			if probeFound {
				break
			}
		}
		time.Sleep(1 * time.Second)
	}
	if !probeFound {
		t.Errorf("expected PROBE_VAR in env-read after 60s, got %d entries: %v", len(keys), keys)
	}
	// Compile-time pin: EnvKey has no Value field — would not compile
	// to reference k.Value anywhere in user code.
	t.Logf("env-read returned %d entries; PROBE_VAR=%v; EnvKey carries no Value field", len(keys), probeFound)
}

// TestProjectAdminClient_AfterClose_ReturnsErrClientClosed verifies the
// real client (not mock) honors Close() semantics.
func TestProjectAdminClient_AfterClose_ReturnsErrClientClosed(t *testing.T) {
	admin := newAdminClient(t)
	admin.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := admin.CreateAndImportProject(ctx, "irrelevant", platform.CreateOpts{})
	if !errors.Is(err, platform.ErrClientClosed) {
		t.Fatalf("expected ErrClientClosed, got %v", err)
	}
}

// TestProjectAdminClient_GetServiceStackIntegrationStatus_NotConfiguredLive
// verifies Phase A B.1 finding against real Zerops API: a freshly imported
// service-stack returns IntegrationNotConfigured (the HTTP 400
// noExternalRepositoryIntegration code is mapped, not propagated).
//
// Uses buildFromGit to ensure the service has an associated repo URL but
// no integration PUT yet. Path B v1: ZCP never PUTs; this is the only
// integration call the real client makes.
func TestProjectAdminClient_GetServiceStackIntegrationStatus_NotConfiguredLive(t *testing.T) {
	admin := newAdminClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	name := throwawayName(t)
	yaml := fmt.Sprintf(`project:
  name: %s
  tags:
    - zcp-launch-spike
services:
  - hostname: app
    type: nodejs@22
    mode: NON_HA
    enableSubdomainAccess: false
    minContainers: 1
    maxContainers: 1
    buildFromGit: https://github.com/krls2020/zcp-pipeline-probe
    zeropsSetup: prod
`, name)
	result, err := admin.CreateAndImportProject(ctx, yaml, platform.CreateOpts{Location: "eu-central"})
	if err != nil {
		t.Fatalf("create throwaway: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = admin.DeleteProject(ctx, result.ProjectID)
	})

	if len(result.ServiceStacks) == 0 {
		t.Fatal("expected at least one service stack in import result")
	}
	svcID := result.ServiceStacks[0].ID

	status, err := admin.GetServiceStackIntegrationStatus(ctx, svcID)
	if err != nil {
		t.Fatalf("GetServiceStackIntegrationStatus: %v", err)
	}
	if status.State != platform.IntegrationNotConfigured {
		t.Errorf("State: got %q want %q (Phase A B.1 expects NotConfigured for fresh service)", status.State, platform.IntegrationNotConfigured)
	}
	if status.Provider != "" {
		t.Errorf("Provider: got %q want empty (NotConfigured state has no provider)", status.Provider)
	}
}
