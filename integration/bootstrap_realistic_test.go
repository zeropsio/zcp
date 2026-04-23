// Tests for: integration — realistic bootstrap conductor E2E flow.
//
// Simulates an agent executing the full bootstrap flow: calling actual MCP tools
// (zerops_discover, zerops_knowledge, zerops_import, zerops_mount, zerops_deploy,
// zerops_subdomain) between conductor step completions. All external dependencies
// (API, SSHFS, zcli) use mocks.

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/tools"
	"github.com/zeropsio/zcp/internal/workflow"
)

// nopHTTPDoer satisfies ops.HTTPDoer for realistic integration tests —
// returns 200 for every request so WaitHTTPReady passes instantly without
// hitting the network.
type nopHTTPDoer struct{}

func (nopHTTPDoer) Do(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
}

// nopMounter satisfies ops.Mounter for realistic integration tests.
type nopMounter struct{}

var _ ops.Mounter = (*nopMounter)(nil)

func (*nopMounter) CheckMount(_ context.Context, _ string) (platform.MountState, error) {
	return platform.MountStateNotMounted, nil
}
func (*nopMounter) Mount(_ context.Context, _, _ string) error           { return nil }
func (*nopMounter) Unmount(_ context.Context, _, _ string) error         { return nil }
func (*nopMounter) ForceUnmount(_ context.Context, _, _ string) error    { return nil }
func (*nopMounter) IsWritable(_ context.Context, _ string) (bool, error) { return true, nil }
func (*nopMounter) ListMountDirs(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (*nopMounter) HasUnit(_ context.Context, _ string) (bool, error) { return false, nil }
func (*nopMounter) CleanupUnit(_ context.Context, _ string) error     { return nil }

// nopSSH satisfies ops.SSHDeployer for realistic integration tests.
type nopSSH struct{}

func (*nopSSH) ExecSSH(_ context.Context, _, _ string) ([]byte, error) {
	return []byte("push ok"), nil
}

func (*nopSSH) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return []byte("push ok"), nil
}

// bootstrapMock creates a mock backend with a realistic bootstrap scenario:
// project with bundev + bunstage + db services.
func bootstrapMock() *platform.Mock {
	return platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myapp", Status: "ACTIVE"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-bundev", Name: "bundev", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1"}},
			{ID: "svc-bunstage", Name: "bunstage", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1"}},
			{ID: "svc-db", Name: "db", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
		}).
		WithServiceEnv("svc-db", []platform.EnvVar{
			{ID: "env-1", Key: "connectionString", Content: "postgresql://zerops:secret@db:5432/db"},
			{ID: "env-2", Key: "host", Content: "db"},
			{ID: "env-3", Key: "port", Content: "5432"},
			{ID: "env-4", Key: "user", Content: "zerops"},
			{ID: "env-5", Key: "password", Content: "secret"},
			{ID: "env-6", Key: "dbName", Content: "db"},
		}).
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myapp",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-bundev", Name: "bundev", Processes: []platform.Process{
					{ID: "proc-import-bundev", ActionName: "serviceStackImport", Status: "PENDING"},
				}},
				{ID: "svc-bunstage", Name: "bunstage", Processes: []platform.Process{
					{ID: "proc-import-bunstage", ActionName: "serviceStackImport", Status: "PENDING"},
				}},
				{ID: "svc-db", Name: "db", Processes: []platform.Process{
					{ID: "proc-import-db", ActionName: "serviceStackImport", Status: "PENDING"},
				}},
			},
		}).
		WithProcess(&platform.Process{ID: "proc-import-bundev", ActionName: "serviceStackImport", Status: "FINISHED"}).
		WithProcess(&platform.Process{ID: "proc-import-bunstage", ActionName: "serviceStackImport", Status: "FINISHED"}).
		WithProcess(&platform.Process{ID: "proc-import-db", ActionName: "serviceStackImport", Status: "FINISHED"}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-bundev", Status: "ACTIVE", Sequence: 1},
			{ID: "av-2", ProjectID: "proj-1", ServiceStackID: "svc-bunstage", Status: "ACTIVE", Sequence: 1},
		}).
		WithProcessEvents([]platform.ProcessEvent{
			{ID: "pe-1", ActionName: "serviceStackImport", Status: "FINISHED"},
		})
}

// setupRealisticServer creates an MCP server with all tools registered individually,
// using a temp dir for the workflow engine (isolates state between parallel tests).
func setupRealisticServer(t *testing.T, mock *platform.Mock) (*mcp.ClientSession, func()) {
	t.Helper()

	const projectID = "proj-1"
	authInfo := &auth.Info{
		ProjectID: projectID, Token: "test-token", APIHost: "localhost",
		Region: "prg1", ClientID: "client-1", ProjectName: "myapp",
	}

	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}

	mcpSrv := mcp.NewServer(&mcp.Implementation{Name: "zcp-realistic-test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	logFetcher := defaultLogFetcher()

	tools.RegisterWorkflow(mcpSrv, mock, projectID, nil, nil, engine, nil, "", "", nil, runtime.Info{})
	tools.RegisterDiscover(mcpSrv, mock, projectID, "")
	tools.RegisterKnowledge(mcpSrv, store, mock, nil, nil, nil)
	tools.RegisterImport(mcpSrv, mock, projectID, engine, "", nil)
	tools.RegisterProcess(mcpSrv, mock)
	tools.RegisterMount(mcpSrv, mock, projectID, &nopMounter{}, runtime.Info{}, "", engine, nil)
	tools.RegisterDeploySSH(mcpSrv, mock, nopHTTPDoer{}, projectID, &nopSSH{}, authInfo, logFetcher, runtime.Info{}, "", engine)
	tools.RegisterSubdomain(mcpSrv, mock, nopHTTPDoer{}, projectID)
	tools.RegisterLogs(mcpSrv, mock, logFetcher, projectID)
	tools.RegisterEvents(mcpSrv, mock, projectID)

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := mcpSrv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "realistic-test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	return session, func() { session.Close(); ss.Close() }
}

// TestIntegration_BootstrapRealistic_FullAgentFlow simulates what a real LLM agent
// does during bootstrap: it calls conductor steps AND the actual MCP tools between them.
//
// NOTE: This test is currently skipped because MCP tool layer tests with step checkers
// cannot be fully validated with mocks. The provision/deploy checkers validate against
// the mock's ListServices, which doesn't support dynamic service creation like the
// real Zerops API does. To fully test this flow, use E2E tests against real Zerops.
func TestIntegration_BootstrapRealistic_FullAgentFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("realistic E2E test, skipping in short mode")
	}
	t.Skip("MCP tool layer tests with checkers require real API or enhanced mock support")
	t.Parallel()

	session, cleanup := setupRealisticServer(t, bootstrapMock())
	defer cleanup()

	agentDiscover(t, session)
	agentProvision(t, session)
	agentVerify(t, session)
}

func agentDiscover(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	startText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic",
		"intent": "bun + hono app with postgresql",
	})
	var resp workflow.BootstrapResponse
	mustUnmarshal(t, startText, &resp)
	assertStep(t, &resp, "discover", 0)

	// Call zerops_discover to inspect services.
	discoverText := callAndGetText(t, session, "zerops_discover", nil)
	var dr ops.DiscoverResult
	mustUnmarshal(t, discoverText, &dr)
	if dr.Project.Name != "myapp" {
		t.Errorf("project name: want 'myapp', got %q", dr.Project.Name)
	}

	// Call zerops_knowledge for infrastructure rules and runtime briefing.
	knowledgeText := callAndGetText(t, session, "zerops_knowledge", map[string]any{"scope": "infrastructure"})
	if len(knowledgeText) < 50 {
		t.Errorf("infrastructure knowledge too short: %d chars", len(knowledgeText))
	}
	runtimeText := callAndGetText(t, session, "zerops_knowledge", map[string]any{
		"runtime": "bun", "services": []any{"postgresql"},
	})
	if runtimeText == "" {
		t.Fatal("zerops_knowledge returned empty for runtime briefing")
	}

	// Complete discover with a plan.
	discoverCompleteText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []map[string]any{
			{
				"runtime": map[string]any{
					"devHostname": "bundev",
					"type":        "bun@1",
				},
				"dependencies": []map[string]any{
					{"hostname": "bunstage", "type": "bun@1", "mode": "NON_HA", "resolution": "CREATE"},
					{"hostname": "db", "type": "postgresql@16", "mode": "NON_HA", "resolution": "CREATE"},
				},
			},
		},
	})
	var discoverCompleteResp workflow.BootstrapResponse
	mustUnmarshal(t, discoverCompleteText, &discoverCompleteResp)
	// Verify discover completed
	if discoverCompleteResp.Current == nil || discoverCompleteResp.Current.Name != "provision" {
		t.Fatalf("discover did not complete: current=%v, message=%q", discoverCompleteResp.Current, discoverCompleteResp.Message)
	}
	if discoverCompleteResp.Progress.Completed != 1 {
		t.Fatalf("discover completion count: want 1, got %d", discoverCompleteResp.Progress.Completed)
	}
}

func agentProvision(t *testing.T, session *mcp.ClientSession) {
	t.Helper()

	// Generate and apply import.yml.
	importYAML := "services:\n  - hostname: bundev\n    type: bun@1\n    minContainers: 1\n  - hostname: bunstage\n    type: bun@1\n    minContainers: 1\n  - hostname: db\n    type: postgresql@16\n    mode: NON_HA\n"
	importText := callAndGetText(t, session, "zerops_import", map[string]any{"content": importYAML})
	var ir ops.ImportResult
	mustUnmarshal(t, importText, &ir)
	if len(ir.Processes) != 3 {
		t.Errorf("import processes: want 3, got %d", len(ir.Processes))
	}
	for _, proc := range ir.Processes {
		procText := callAndGetText(t, session, "zerops_process", map[string]any{"processId": proc.ProcessID})
		var status ops.ProcessStatusResult
		mustUnmarshal(t, procText, &status)
		if status.Status != "FINISHED" {
			t.Errorf("import process %s: want FINISHED, got %s", proc.ProcessID, status.Status)
		}
	}

	// Mount dev service.
	mountText := callAndGetText(t, session, "zerops_mount", map[string]any{
		"action": "mount", "serviceHostname": "bundev",
	})
	var mr ops.MountResult
	mustUnmarshal(t, mountText, &mr)
	if mr.Status != "MOUNTED" {
		t.Errorf("mount status: want MOUNTED, got %s", mr.Status)
	}

	// Discover env vars.
	envText := callAndGetText(t, session, "zerops_discover", map[string]any{
		"service": "db", "includeEnvs": true,
	})
	var envDR ops.DiscoverResult
	mustUnmarshal(t, envText, &envDR)
	if len(envDR.Services) != 1 {
		t.Fatalf("env discover: want 1 service, got %d", len(envDR.Services))
	}
	envNames := make(map[string]bool)
	for _, env := range envDR.Services[0].Envs {
		if key, ok := env["key"].(string); ok {
			envNames[key] = true
		}
	}
	for _, req := range []string{"connectionString", "host", "port", "user", "password", "dbName"} {
		if !envNames[req] {
			t.Errorf("missing required env var: %s", req)
		}
	}

	// Post-import verification.
	postText := callAndGetText(t, session, "zerops_discover", nil)
	var postDR ops.DiscoverResult
	mustUnmarshal(t, postText, &postDR)
	if len(postDR.Services) != 3 {
		t.Errorf("post-import services: want 3, got %d", len(postDR.Services))
	}

	completeStep(t, session, "provision",
		"All 3 services imported: bundev=RUNNING, bunstage=RUNNING, db=RUNNING. "+
			"Dev mounted at /var/www/bundev/. DB envs discovered: connectionString, host, port, user, password, dbName.")
}

func agentVerify(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	for _, hostname := range []string{"bundev", "bunstage", "db"} {
		vText := callAndGetText(t, session, "zerops_discover", map[string]any{"service": hostname})
		var vDR ops.DiscoverResult
		mustUnmarshal(t, vText, &vDR)
		if len(vDR.Services) != 1 || vDR.Services[0].Status != "RUNNING" {
			t.Errorf("verify %s: expected 1 RUNNING service", hostname)
		}
	}

	// Final summary discover.
	callAndGetText(t, session, "zerops_discover", nil)

	closeText := completeStep(t, session, "close",
		"Bootstrap administratively closed. bundev, bunstage ServiceMetas written with BootstrappedAt.")

	var finalResp workflow.BootstrapResponse
	mustUnmarshal(t, closeText, &finalResp)
	if finalResp.Current != nil {
		t.Errorf("expected nil current after completion, got: %s", finalResp.Current.Name)
	}
	if finalResp.Progress.Completed != 3 {
		t.Errorf("completed: want 3, got %d", finalResp.Progress.Completed)
	}
	if !strings.Contains(strings.ToLower(finalResp.Message), "complete") {
		t.Errorf("final message should contain 'complete', got: %q", finalResp.Message)
	}
	for _, step := range finalResp.Progress.Steps {
		if step.Status != "complete" {
			t.Errorf("step %s: want 'complete', got %q", step.Name, step.Status)
		}
	}
}

// TestIntegration_BootstrapRealistic_ManagedOnlySkipPath simulates a managed-only
// project (just a database) where close is skipped (no runtime to register).
func TestIntegration_BootstrapRealistic_ManagedOnlySkipPath(t *testing.T) {
	if testing.Short() {
		t.Skip("realistic E2E test, skipping in short mode")
	}
	t.Parallel()

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "dbonly", Status: "ACTIVE"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-db", Name: "db", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
		}).
		WithServiceEnv("svc-db", []platform.EnvVar{
			{ID: "env-1", Key: "connectionString", Content: "postgresql://zerops:secret@db:5432/db"},
		}).
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "dbonly",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-db", Name: "db", Processes: []platform.Process{
					{ID: "proc-import-db", ActionName: "serviceStackImport", Status: "PENDING"},
				}},
			},
		}).
		WithProcess(&platform.Process{ID: "proc-import-db", ActionName: "serviceStackImport", Status: "FINISHED"}).
		WithProcessEvents(nil).
		WithAppVersionEvents(nil)

	session, cleanup := setupRealisticServer(t, mock)
	defer cleanup()

	managedOnlyDiscover(t, session)
	managedOnlyProvision(t, session)
	managedOnlySkipAndVerify(t, session)
}

func managedOnlyDiscover(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic",
		"intent": "postgresql database only",
	})

	discoverText := callAndGetText(t, session, "zerops_discover", nil)
	var dr ops.DiscoverResult
	mustUnmarshal(t, discoverText, &dr)
	if len(dr.Services) != 1 || dr.Services[0].Hostname != "db" {
		t.Errorf("expected only db service, got: %+v", dr.Services)
	}

	callAndGetText(t, session, "zerops_knowledge", map[string]any{"scope": "infrastructure"})

	// Complete discover with empty plan (managed-only project).
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []map[string]any{}, // Empty plan for managed-only
	})
}

func managedOnlyProvision(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	importText := callAndGetText(t, session, "zerops_import", map[string]any{
		"content": "services:\n  - hostname: db\n    type: postgresql@16\n    mode: NON_HA\n",
	})
	var ir ops.ImportResult
	mustUnmarshal(t, importText, &ir)
	if len(ir.Processes) != 1 {
		t.Errorf("expected 1 import process, got %d", len(ir.Processes))
	}

	completeStep(t, session, "provision",
		"db imported. Process FINISHED. RUNNING. Dev mount skipped (no runtime). Envs discovered.")
}

func managedOnlySkipAndVerify(t *testing.T, session *mcp.ClientSession) {
	t.Helper()

	// Verify db is running.
	verifyText := callAndGetText(t, session, "zerops_discover", map[string]any{"service": "db"})
	var vDR ops.DiscoverResult
	mustUnmarshal(t, verifyText, &vDR)
	if len(vDR.Services) != 1 || vDR.Services[0].Status != "RUNNING" {
		t.Errorf("verify db: expected RUNNING, got: %+v", vDR.Services)
	}

	// Skip close (managed-only plan: no runtime services require registration).
	// Under Option A, close is the only skippable bootstrap step.
	finalText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "skip", "step": "close", "reason": "managed-only project, no runtime registration needed",
	})
	var finalResp workflow.BootstrapResponse
	mustUnmarshal(t, finalText, &finalResp)
	if finalResp.Progress.Completed != 3 {
		t.Errorf("completed: want 3, got %d", finalResp.Progress.Completed)
	}

	skipped, completed := 0, 0
	for _, s := range finalResp.Progress.Steps {
		switch s.Status {
		case "skipped":
			skipped++
		case "complete":
			completed++
		}
	}
	if skipped != 1 {
		t.Errorf("skipped: want 1, got %d", skipped)
	}
	if completed != 2 {
		t.Errorf("completed: want 2, got %d", completed)
	}
}

// TestIntegration_BootstrapRealistic_ToolsAvailable verifies that a realistic
// server has all the tools that the conductor guidance references.
func TestIntegration_BootstrapRealistic_ToolsAvailable(t *testing.T) {
	t.Parallel()

	session, cleanup := setupRealisticServer(t, bootstrapMock())
	defer cleanup()

	result, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}

	required := []string{
		"zerops_workflow", "zerops_discover", "zerops_knowledge",
		"zerops_import", "zerops_process", "zerops_mount",
		"zerops_deploy", "zerops_subdomain", "zerops_logs",
	}
	for _, name := range required {
		if !toolNames[name] {
			t.Errorf("required tool %q not registered", name)
		}
	}
}

// ── Helpers ──

func mustUnmarshal(t *testing.T, text string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(text), v); err != nil {
		t.Fatalf("unmarshal: %v\ntext: %.200s", err, text)
	}
}

func completeStep(t *testing.T, session *mcp.ClientSession, step, attestation string) string {
	t.Helper()
	return callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": step, "attestation": attestation,
	})
}

func assertStep(t *testing.T, resp *workflow.BootstrapResponse, name string, completed int) {
	t.Helper()
	if resp.Current == nil {
		t.Fatalf("expected current step %q, got nil", name)
	}
	if resp.Current.Name != name {
		t.Errorf("current step: want %q, got %q", name, resp.Current.Name)
	}
	if resp.Progress.Completed != completed {
		t.Errorf("completed: want %d, got %d", completed, resp.Progress.Completed)
	}
}
