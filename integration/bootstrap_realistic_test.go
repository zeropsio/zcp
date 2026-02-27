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
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/tools"
	"github.com/zeropsio/zcp/internal/workflow"
)

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

// nopLocal satisfies ops.LocalDeployer for realistic integration tests.
type nopLocal struct{}

func (*nopLocal) ExecZcli(_ context.Context, _ ...string) ([]byte, error) {
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
	engine := workflow.NewEngine(t.TempDir())
	logFetcher := defaultLogFetcher()

	tools.RegisterWorkflow(mcpSrv, mock, projectID, nil, engine)
	tools.RegisterDiscover(mcpSrv, mock, projectID)
	tools.RegisterKnowledge(mcpSrv, store, mock, nil)
	tools.RegisterImport(mcpSrv, mock, projectID, nil, engine)
	tools.RegisterProcess(mcpSrv, mock)
	tools.RegisterMount(mcpSrv, mock, projectID, &nopMounter{})
	tools.RegisterDeploy(mcpSrv, mock, projectID, nil, &nopLocal{}, authInfo, engine)
	tools.RegisterSubdomain(mcpSrv, mock, projectID)
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
func TestIntegration_BootstrapRealistic_FullAgentFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("realistic E2E test, skipping in short mode")
	}
	t.Parallel()

	session, cleanup := setupRealisticServer(t, bootstrapMock())
	defer cleanup()

	agentStartAndDetect(t, session)
	agentPlanAndKnowledge(t, session)
	agentGenerateAndImport(t, session)
	agentMountAndDiscoverEnvs(t, session)
	agentGenerateCode(t, session)
	agentDeployWithSubdomain(t, session)
	agentVerifyAndReport(t, session)
}

func agentStartAndDetect(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	startText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "mode": "full",
		"intent": "bun + hono app with postgresql",
	})
	var resp workflow.BootstrapResponse
	mustUnmarshal(t, startText, &resp)
	assertStep(t, &resp, "detect", 0)

	discoverText := callAndGetText(t, session, "zerops_discover", nil)
	var dr ops.DiscoverResult
	mustUnmarshal(t, discoverText, &dr)
	if dr.Project.Name != "myapp" {
		t.Errorf("project name: want 'myapp', got %q", dr.Project.Name)
	}

	completeStep(t, session, "detect",
		"FRESH project detected: myapp has 3 services (bundev, bunstage, db). "+
			"Classified as PARTIAL — services exist from prior import.")
}

func agentPlanAndKnowledge(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	knowledgeText := callAndGetText(t, session, "zerops_knowledge", map[string]any{"scope": "infrastructure"})
	if len(knowledgeText) < 50 {
		t.Errorf("infrastructure knowledge too short: %d chars", len(knowledgeText))
	}
	completeStep(t, session, "plan",
		"Plan: bundev+bunstage (bun@1, Hono), db (postgresql@16). Hostnames validated.")

	runtimeText := callAndGetText(t, session, "zerops_knowledge", map[string]any{
		"runtime": "bun", "services": []any{"postgresql"},
	})
	if runtimeText == "" {
		t.Fatal("zerops_knowledge returned empty for runtime briefing")
	}
	callAndGetText(t, session, "zerops_knowledge", map[string]any{"scope": "infrastructure"})
	completeStep(t, session, "load-knowledge",
		"Loaded bun runtime briefing + infrastructure rules. Both mandatory calls done.")
}

func agentGenerateAndImport(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	completeStep(t, session, "generate-import",
		"Generated import.yml: bundev, bunstage (bun@1), db (postgresql@16). All validated.")

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

	postText := callAndGetText(t, session, "zerops_discover", nil)
	var postDR ops.DiscoverResult
	mustUnmarshal(t, postText, &postDR)
	if len(postDR.Services) != 3 {
		t.Errorf("post-import services: want 3, got %d", len(postDR.Services))
	}
	completeStep(t, session, "import-services",
		"All 3 services imported. bundev=RUNNING, bunstage=RUNNING, db=RUNNING. All FINISHED.")
}

func agentMountAndDiscoverEnvs(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	mountText := callAndGetText(t, session, "zerops_mount", map[string]any{
		"action": "mount", "serviceHostname": "bundev",
	})
	var mr ops.MountResult
	mustUnmarshal(t, mountText, &mr)
	if mr.Status != "MOUNTED" {
		t.Errorf("mount status: want MOUNTED, got %s", mr.Status)
	}
	completeStep(t, session, "mount-dev",
		"Mounted bundev at /var/www/bundev/. Writable. Stage and managed skipped.")

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
	completeStep(t, session, "discover-envs",
		"Discovered db envs: connectionString, host, port, user, password, dbName. 6 vars.")
}

func agentGenerateCode(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	completeStep(t, session, "generate-code",
		"Generated zerops.yml + app code for bundev and bunstage. /status endpoint with DB SELECT 1 proof. deployFiles: [.].")
}

func agentDeployWithSubdomain(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	for _, svc := range []string{"bundev", "bunstage"} {
		deployText := callAndGetText(t, session, "zerops_deploy", map[string]any{"targetService": svc})
		var dr ops.DeployResult
		mustUnmarshal(t, deployText, &dr)
		if dr.TargetService != svc {
			t.Errorf("deploy target: want %s, got %s", svc, dr.TargetService)
		}

		subText := callAndGetText(t, session, "zerops_subdomain", map[string]any{
			"serviceHostname": svc, "action": "enable",
		})
		var sr ops.SubdomainResult
		mustUnmarshal(t, subText, &sr)
		if sr.Hostname != svc {
			t.Errorf("subdomain hostname: want %s, got %s", svc, sr.Hostname)
		}
	}
	completeStep(t, session, "deploy",
		"Deployed bundev + bunstage: /status 200, SELECT 1 proof. Both subdomains enabled.")
}

func agentVerifyAndReport(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	for _, hostname := range []string{"bundev", "bunstage", "db"} {
		vText := callAndGetText(t, session, "zerops_discover", map[string]any{"service": hostname})
		var vDR ops.DiscoverResult
		mustUnmarshal(t, vText, &vDR)
		if len(vDR.Services) != 1 || vDR.Services[0].Status != "RUNNING" {
			t.Errorf("verify %s: expected 1 RUNNING service", hostname)
		}
	}
	completeStep(t, session, "verify",
		"Independent verification: bundev, bunstage, db all RUNNING. 3/3 healthy.")

	callAndGetText(t, session, "zerops_discover", nil) // final summary discover
	reportText := completeStep(t, session, "report",
		"All 3 services operational. Dev: bundev. Stage: bunstage. DB: db. Complete.")

	var finalResp workflow.BootstrapResponse
	mustUnmarshal(t, reportText, &finalResp)
	if finalResp.Current != nil {
		t.Errorf("expected nil current after completion, got: %s", finalResp.Current.Name)
	}
	if finalResp.Progress.Completed != 11 {
		t.Errorf("completed: want 11, got %d", finalResp.Progress.Completed)
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
// project (just a database) where mount-dev, discover-envs, and deploy are skipped.
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

	managedOnlyDetectAndPlan(t, session)
	managedOnlyImportAndSkip(t, session)
	managedOnlyVerifyAndReport(t, session)
}

func managedOnlyDetectAndPlan(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "mode": "full",
		"intent": "postgresql database only",
	})

	discoverText := callAndGetText(t, session, "zerops_discover", nil)
	var dr ops.DiscoverResult
	mustUnmarshal(t, discoverText, &dr)
	if len(dr.Services) != 1 || dr.Services[0].Hostname != "db" {
		t.Errorf("expected only db service, got: %+v", dr.Services)
	}
	completeStep(t, session, "detect", "Managed-only project: 1 service (db). Classified as PARTIAL.")
	completeStep(t, session, "plan", "Plan: db (postgresql@16) only. Managed-only fast path.")
	callAndGetText(t, session, "zerops_knowledge", map[string]any{"scope": "infrastructure"})
	completeStep(t, session, "load-knowledge", "Loaded infrastructure rules. No runtime briefing needed.")
	completeStep(t, session, "generate-import", "Generated import.yml: db (postgresql@16, NON_HA).")
}

func managedOnlyImportAndSkip(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	importText := callAndGetText(t, session, "zerops_import", map[string]any{
		"content": "services:\n  - hostname: db\n    type: postgresql@16\n    mode: NON_HA\n",
	})
	var ir ops.ImportResult
	mustUnmarshal(t, importText, &ir)
	if len(ir.Processes) != 1 {
		t.Errorf("expected 1 import process, got %d", len(ir.Processes))
	}
	completeStep(t, session, "import-services", "db imported. Process FINISHED. RUNNING.")

	skipText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "skip", "step": "mount-dev", "reason": "no runtime services",
	})
	var skipResp workflow.BootstrapResponse
	mustUnmarshal(t, skipText, &skipResp)
	assertStep(t, &skipResp, "discover-envs", 6)

	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "skip", "step": "discover-envs", "reason": "no runtime services need envs",
	})
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "skip", "step": "generate-code", "reason": "no runtime services to generate code for",
	})
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "skip", "step": "deploy", "reason": "no runtime services to deploy",
	})
}

func managedOnlyVerifyAndReport(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	verifyText := callAndGetText(t, session, "zerops_discover", map[string]any{"service": "db"})
	var vDR ops.DiscoverResult
	mustUnmarshal(t, verifyText, &vDR)
	if len(vDR.Services) != 1 || vDR.Services[0].Status != "RUNNING" {
		t.Errorf("verify db: expected RUNNING, got: %+v", vDR.Services)
	}
	completeStep(t, session, "verify", "db RUNNING. 1/1 managed service healthy.")

	reportText := completeStep(t, session, "report", "Managed-only project complete. db RUNNING.")
	var finalResp workflow.BootstrapResponse
	mustUnmarshal(t, reportText, &finalResp)
	if finalResp.Progress.Completed != 11 {
		t.Errorf("completed: want 11, got %d", finalResp.Progress.Completed)
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
	if skipped != 4 {
		t.Errorf("skipped: want 4, got %d", skipped)
	}
	if completed != 7 {
		t.Errorf("completed: want 7, got %d", completed)
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
