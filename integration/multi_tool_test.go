// Tests for: integration — multi-tool flow tests using full MCP server with mock backend.

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
	"github.com/zeropsio/zcp/internal/server"
)

// setupTestServer creates a full MCP server with mock backend and returns a
// connected client session. The cleanup function must be called when done.
func setupTestServer(t *testing.T, mock *platform.Mock, logFetcher platform.LogFetcher) (*mcp.ClientSession, func()) {
	t.Helper()

	authInfo := &auth.Info{
		ProjectID:   "proj-1",
		Token:       "test-token",
		APIHost:     "localhost",
		Region:      "prg1",
		ClientID:    "client-1",
		ProjectName: "test-project",
	}

	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}

	srv := server.New(mock, authInfo, store, logFetcher, nil, nil, nil, nil)

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.MCPServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}

	cleanup := func() {
		session.Close()
		ss.Close()
	}
	return session, cleanup
}

// callAndGetText calls a tool and returns the text content of the first content item.
func callAndGetText(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if len(result.Content) == 0 {
		t.Fatalf("no content in %s result", name)
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

// callAndGetResult calls a tool and returns the full CallToolResult.
func callAndGetResult(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return result
}

// defaultMock creates a standard mock with project, services, env vars, and events.
func defaultMock() *platform.Mock {
	return platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test-project", Status: "ACTIVE"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			{ID: "svc-2", Name: "db", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
		}).
		WithServiceEnv("svc-1", []platform.EnvVar{
			{ID: "env-1", Key: "DB_HOST", Content: "db"},
		}).
		WithProcessEvents([]platform.ProcessEvent{
			{ID: "pe-1", ActionName: "start", Status: "FINISHED"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{})
}

func defaultLogFetcher() *platform.MockLogFetcher {
	return platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
		{Timestamp: "2024-01-01T00:00:00Z", Severity: "INFO", Message: "test log"},
	})
}

func TestIntegration_DiscoverThenManage(t *testing.T) {
	t.Parallel()

	mock := defaultMock()
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	// Step 1: Discover services.
	discoverText := callAndGetText(t, session, "zerops_discover", nil)

	var dr ops.DiscoverResult
	if err := json.Unmarshal([]byte(discoverText), &dr); err != nil {
		t.Fatalf("parse discover result: %v", err)
	}
	if len(dr.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(dr.Services))
	}

	// Step 2: Use discovered hostname to restart a service.
	hostname := dr.Services[0].Hostname
	manageText := callAndGetText(t, session, "zerops_manage", map[string]any{
		"action":          "restart",
		"serviceHostname": hostname,
	})

	var proc platform.Process
	if err := json.Unmarshal([]byte(manageText), &proc); err != nil {
		t.Fatalf("parse manage result: %v", err)
	}
	if proc.ActionName != "restart" {
		t.Errorf("action = %q, want %q", proc.ActionName, "restart")
	}
	if proc.Status != "PENDING" {
		t.Errorf("status = %q, want %q", proc.Status, "PENDING")
	}
}

func TestIntegration_DiscoverThenScale(t *testing.T) {
	t.Parallel()

	mock := defaultMock()
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	// Step 1: Discover services.
	discoverText := callAndGetText(t, session, "zerops_discover", nil)

	var dr ops.DiscoverResult
	if err := json.Unmarshal([]byte(discoverText), &dr); err != nil {
		t.Fatalf("parse discover result: %v", err)
	}
	if len(dr.Services) == 0 {
		t.Fatal("expected at least 1 service")
	}

	// Step 2: Use discovered hostname to scale a service.
	hostname := dr.Services[0].Hostname
	scaleText := callAndGetText(t, session, "zerops_scale", map[string]any{
		"serviceHostname": hostname,
		"cpuMode":         "SHARED",
		"minCpu":          1,
		"maxCpu":          4,
	})

	var sr ops.ScaleResult
	if err := json.Unmarshal([]byte(scaleText), &sr); err != nil {
		t.Fatalf("parse scale result: %v", err)
	}
	if sr.Hostname != hostname {
		t.Errorf("serviceHostname = %q, want %q", sr.Hostname, hostname)
	}
	if sr.ServiceID == "" {
		t.Error("expected non-empty serviceId")
	}
}

func TestIntegration_ImportThenDiscover(t *testing.T) {
	t.Parallel()

	mock := defaultMock().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "test-project",
			ServiceStacks: []platform.ImportedServiceStack{
				{
					ID:   "svc-web",
					Name: "web",
					Processes: []platform.Process{
						{ID: "proc-web", ActionName: "serviceStackImport", Status: "PENDING"},
					},
				},
			},
		})
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	validYAML := `services:
  - hostname: web
    type: nodejs@22
    minContainers: 1
`

	// Step 1: Import — validates inline and calls API.
	importText := callAndGetText(t, session, "zerops_import", map[string]any{
		"content": validYAML,
	})

	var importResult ops.ImportResult
	if err := json.Unmarshal([]byte(importText), &importResult); err != nil {
		t.Fatalf("parse import result: %v", err)
	}
	if importResult.ProjectID != "proj-1" {
		t.Errorf("expected projectId=proj-1, got %s", importResult.ProjectID)
	}
	if len(importResult.Processes) != 1 {
		t.Fatalf("expected 1 process, got %d", len(importResult.Processes))
	}

	// Step 2: Discover shows services.
	discoverText := callAndGetText(t, session, "zerops_discover", nil)

	var dr ops.DiscoverResult
	if err := json.Unmarshal([]byte(discoverText), &dr); err != nil {
		t.Fatalf("parse discover result: %v", err)
	}
	if len(dr.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(dr.Services))
	}
}

func TestIntegration_EnvSetThenDiscover(t *testing.T) {
	t.Parallel()

	mock := defaultMock()
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	// Step 1: Set an env var.
	setResult := callAndGetResult(t, session, "zerops_env", map[string]any{
		"action":          "set",
		"serviceHostname": "app",
		"variables":       []any{"NEW_VAR=hello"},
	})
	if setResult.IsError {
		t.Fatalf("env set returned error: %s", getTextContent(t, setResult))
	}

	// Step 2: Read env vars via zerops_discover with includeEnvs=true.
	discoverText := callAndGetText(t, session, "zerops_discover", map[string]any{
		"service":     "app",
		"includeEnvs": true,
	})

	var dr ops.DiscoverResult
	if err := json.Unmarshal([]byte(discoverText), &dr); err != nil {
		t.Fatalf("parse discover result: %v", err)
	}
	if len(dr.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(dr.Services))
	}
	if dr.Services[0].Envs == nil {
		t.Fatal("expected envs in discover result")
	}

	foundDBHost := false
	for _, env := range dr.Services[0].Envs {
		if env["key"] == "DB_HOST" {
			foundDBHost = true
			if env["value"] != "db" {
				t.Errorf("DB_HOST value = %q, want %q", env["value"], "db")
			}
		}
	}
	if !foundDBHost {
		t.Error("DB_HOST not found in discover env vars")
	}
}

func TestIntegration_DiscoverProjectEnvs(t *testing.T) {
	t.Parallel()

	mock := defaultMock().
		WithProjectEnv([]platform.EnvVar{
			{ID: "pe1", Key: "GLOBAL_KEY", Content: "global_val"},
		})
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	// Discover without service filter — should include project envs.
	discoverText := callAndGetText(t, session, "zerops_discover", map[string]any{
		"includeEnvs": true,
	})

	var dr ops.DiscoverResult
	if err := json.Unmarshal([]byte(discoverText), &dr); err != nil {
		t.Fatalf("parse discover result: %v", err)
	}
	if dr.Project.Envs == nil {
		t.Fatal("expected project envs in discover result")
	}
	if len(dr.Project.Envs) != 1 {
		t.Fatalf("expected 1 project env, got %d", len(dr.Project.Envs))
	}
	if dr.Project.Envs[0]["key"] != "GLOBAL_KEY" {
		t.Errorf("expected key=GLOBAL_KEY, got %v", dr.Project.Envs[0]["key"])
	}
}

func TestIntegration_DeleteWithConfirmGate(t *testing.T) {
	t.Parallel()

	mock := defaultMock()
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	// Step 1: Delete without confirm — must return error.
	noConfirmResult := callAndGetResult(t, session, "zerops_delete", map[string]any{
		"serviceHostname": "app",
		"confirm":         false,
	})
	if !noConfirmResult.IsError {
		t.Fatal("expected IsError when confirm=false")
	}
	errText := getTextContent(t, noConfirmResult)
	if !strings.Contains(errText, "CONFIRM_REQUIRED") {
		t.Errorf("error should contain CONFIRM_REQUIRED, got: %s", errText)
	}

	// Step 2: Delete with confirm — must succeed.
	confirmResult := callAndGetResult(t, session, "zerops_delete", map[string]any{
		"serviceHostname": "app",
		"confirm":         true,
	})
	if confirmResult.IsError {
		t.Fatalf("delete with confirm returned error: %s", getTextContent(t, confirmResult))
	}

	deleteText := getTextContent(t, confirmResult)
	var proc platform.Process
	if err := json.Unmarshal([]byte(deleteText), &proc); err != nil {
		t.Fatalf("parse delete result: %v", err)
	}
	if proc.ActionName != "delete" {
		t.Errorf("action = %q, want %q", proc.ActionName, "delete")
	}
}

func TestIntegration_ProcessPolling(t *testing.T) {
	t.Parallel()

	mock := defaultMock().
		WithProcess(&platform.Process{
			ID:         "p1",
			ActionName: "restart",
			Status:     "FINISHED",
			Created:    "2024-01-01T00:00:00Z",
		})

	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	// Call zerops_process to check status.
	processText := callAndGetText(t, session, "zerops_process", map[string]any{
		"processId": "p1",
	})

	var status ops.ProcessStatusResult
	if err := json.Unmarshal([]byte(processText), &status); err != nil {
		t.Fatalf("parse process result: %v", err)
	}
	if status.ProcessID != "p1" {
		t.Errorf("processId = %q, want %q", status.ProcessID, "p1")
	}
	if status.Status != "FINISHED" {
		t.Errorf("status = %q, want %q", status.Status, "FINISHED")
	}
	if status.Action != "restart" {
		t.Errorf("actionName = %q, want %q", status.Action, "restart")
	}
}

func TestIntegration_ErrorPropagation(t *testing.T) {
	t.Parallel()

	mock := defaultMock().
		WithError("ListServices", platform.NewPlatformError(
			platform.ErrAPIError, "simulated API failure", "retry later",
		))

	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	// Discover calls GetProject (succeeds) then ListServices (fails).
	result := callAndGetResult(t, session, "zerops_discover", nil)
	if !result.IsError {
		t.Fatal("expected IsError for injected API error")
	}

	errText := getTextContent(t, result)

	var errBody map[string]string
	if err := json.Unmarshal([]byte(errText), &errBody); err != nil {
		t.Fatalf("expected JSON error body, got: %s", errText)
	}
	if errBody["code"] != platform.ErrAPIError {
		t.Errorf("code = %q, want %q", errBody["code"], platform.ErrAPIError)
	}
	if !strings.Contains(errBody["error"], "simulated API failure") {
		t.Errorf("error = %q, should contain 'simulated API failure'", errBody["error"])
	}
	if errBody["suggestion"] != "retry later" {
		t.Errorf("suggestion = %q, want %q", errBody["suggestion"], "retry later")
	}
}

func TestIntegration_ContextThenWorkflow(t *testing.T) {
	t.Parallel()

	mock := defaultMock()
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	// Step 1: Call zerops_context — should return non-empty platform context.
	contextText := callAndGetText(t, session, "zerops_context", nil)
	if contextText == "" {
		t.Fatal("expected non-empty context text")
	}
	if !strings.Contains(contextText, "Zerops") {
		t.Error("context should mention Zerops")
	}

	// Step 2: Call zerops_workflow without params — should return catalog.
	catalogText := callAndGetText(t, session, "zerops_workflow", nil)
	if catalogText == "" {
		t.Fatal("expected non-empty workflow catalog")
	}
	if !strings.Contains(catalogText, "bootstrap") {
		t.Error("catalog should list bootstrap workflow")
	}

	// Step 3: Call zerops_workflow with specific workflow.
	bootstrapText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"workflow": "bootstrap",
	})
	if bootstrapText == "" {
		t.Fatal("expected non-empty bootstrap workflow content")
	}
	if len(bootstrapText) < 50 {
		t.Errorf("bootstrap workflow content too short (%d chars)", len(bootstrapText))
	}
}

// getTextContent extracts the text string from the first content item.
func getTextContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("no content in result")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", result.Content[0])
	}
	return tc.Text
}
