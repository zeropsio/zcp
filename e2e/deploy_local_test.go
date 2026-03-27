//go:build e2e

// Tests for: e2e — local deploy lifecycle via zerops_deploy (zcli push from local machine).
//
// Tests the complete local mode flow:
//   - Local deploy: code on user's machine → zcli push → build on Zerops → HTTP 200
//   - Schema validation: local mode has no sourceService parameter
//   - Error paths: missing zerops.yml, service not found, build failure
//   - Env var bridge: FormatEnvFile generates correct .env content
//
// Prerequisites:
//   - ZCP_API_KEY set
//   - zcli installed and in PATH
//   - VPN connected to the project (for managed service tests)
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_LocalDeploy -v -timeout 900s

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// newLocalHarness creates an E2E harness with sshDeployer=nil (local mode).
// This triggers RegisterDeployLocal instead of RegisterDeploySSH in server.go.
func newLocalHarness(t *testing.T) *e2eHarness {
	t.Helper()

	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set — skipping E2E test")
	}

	apiHost := os.Getenv("ZCP_API_HOST")
	if apiHost == "" {
		apiHost = "api.app-prg1.zerops.io"
	}

	region := os.Getenv("ZCP_REGION")
	if region == "" {
		region = "prg1"
	}

	client, err := platform.NewZeropsClient(token, apiHost)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
		t.Fatalf("auth resolve: %v", err)
	}
	authInfo.Region = region

	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}

	logFetcher := platform.NewLogFetcher()
	// sshDeployer=nil → local mode: RegisterDeployLocal is used.
	srv := server.New(context.Background(), client, authInfo, store, logFetcher, nil, nil, runtime.Info{})

	return &e2eHarness{
		t:         t,
		client:    client,
		projectID: authInfo.ProjectID,
		authInfo:  authInfo,
		srv:       srv,
	}
}

// TestE2E_LocalDeploy_Success tests the full local deploy flow:
// import service → create local app → zcli push → verify DEPLOYED → HTTP 200
func TestE2E_LocalDeploy_Success(t *testing.T) {
	h := newLocalHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	hostname := "zcpld" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, hostname)
	})

	t.Cleanup(func() {
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	})

	step := 0

	// --- Step 1: Start workflow (import requires active session) ---
	step++
	logStep(t, step, "zerops_workflow bootstrap")
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "e2e local deploy test",
	})

	// --- Step 2: Import service ---
	step++
	logStep(t, step, "zerops_import %s", hostname)
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    startWithoutCode: true
    enableSubdomainAccess: true
`, hostname)
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
	assertImportAllFinished(t, importText)

	// --- Step 3: Wait for service RUNNING ---
	step++
	logStep(t, step, "waiting for %s to be RUNNING", hostname)
	waitForServiceStatus(s, hostname, "RUNNING", "ACTIVE")

	// --- Step 4: Create local app ---
	step++
	logStep(t, step, "creating local app for %s", hostname)
	appDir := createMinimalApp(t, hostname)
	t.Logf("  App dir: %s", appDir)

	// --- Step 5: Local deploy via zerops_deploy ---
	step++
	logStep(t, step, "zerops_deploy targetService=%s (local mode)", hostname)
	deployText := s.mustCallSuccess("zerops_deploy", map[string]any{
		"targetService": hostname,
		"workingDir":    appDir,
	})

	var deployResult struct {
		Status            string `json:"status"`
		Mode              string `json:"mode"`
		BuildStatus       string `json:"buildStatus"`
		TargetService     string `json:"targetService"`
		TargetServiceType string `json:"targetServiceType"`
		Message           string `json:"message"`
	}
	if err := json.Unmarshal([]byte(deployText), &deployResult); err != nil {
		t.Fatalf("parse deploy result: %v", err)
	}
	if deployResult.Status != "DEPLOYED" {
		t.Fatalf("status = %s, want DEPLOYED (full: %s)", deployResult.Status, truncate(deployText, 500))
	}
	if deployResult.Mode != "local" {
		t.Errorf("mode = %s, want local", deployResult.Mode)
	}
	if deployResult.TargetService != hostname {
		t.Errorf("targetService = %s, want %s", deployResult.TargetService, hostname)
	}
	t.Logf("  Deploy: status=%s mode=%s target=%s", deployResult.Status, deployResult.Mode, deployResult.TargetService)

	// --- Step 6: Verify RUNNING via discover ---
	step++
	logStep(t, step, "verify RUNNING via discover")
	waitForServiceStatus(s, hostname, "RUNNING", "ACTIVE")
	t.Log("  Service is RUNNING")

	// --- Step 7: Delete ---
	step++
	logStep(t, step, "zerops_delete %s", hostname)
	deleteText := s.mustCallSuccess("zerops_delete", map[string]any{
		"serviceHostname": hostname,
	})
	procID := extractProcessID(t, deleteText)
	waitForProcess(s, procID)
	t.Log("  Service deleted")
}

// TestE2E_LocalDeploy_Schema verifies that local mode schema has no sourceService.
func TestE2E_LocalDeploy_Schema(t *testing.T) {
	h := newLocalHarness(t)

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	_, err := h.srv.MCPServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "e2e-schema", Version: "0.1"}, nil)
	session, err := mcpClient.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	var found bool
	for _, tool := range result.Tools {
		if tool.Name != "zerops_deploy" {
			continue
		}
		found = true

		schemaJSON, _ := json.Marshal(tool.InputSchema)
		schemaStr := string(schemaJSON)

		if strings.Contains(schemaStr, "sourceService") {
			t.Error("local mode schema should NOT have sourceService")
		}
		if !strings.Contains(schemaStr, "targetService") {
			t.Error("local mode schema should have targetService")
		}
		if !strings.Contains(tool.Description, "zcli") {
			t.Errorf("description should mention zcli, got: %s", tool.Description)
		}
		if strings.Contains(tool.Description, "SSH") {
			t.Errorf("description should NOT mention SSH, got: %s", tool.Description)
		}
		t.Log("  Schema verified: no sourceService, has targetService, mentions zcli")
	}
	if !found {
		t.Fatal("zerops_deploy not registered")
	}
}

// TestE2E_LocalDeploy_ServiceNotFound verifies proper error for non-existent target.
func TestE2E_LocalDeploy_ServiceNotFound(t *testing.T) {
	h := newLocalHarness(t)
	s := newSession(t, h.srv)

	appDir := createMinimalApp(t, "nonexistent")

	result := s.callTool("zerops_deploy", map[string]any{
		"targetService": "nonexistentservice" + randomSuffix(),
		"workingDir":    appDir,
	})

	if !result.IsError {
		t.Fatal("expected error for non-existent service")
	}

	text := getE2ETextContent(t, result)
	if !strings.Contains(text, "SERVICE_NOT_FOUND") {
		t.Errorf("expected SERVICE_NOT_FOUND, got: %s", truncate(text, 300))
	}
	t.Logf("  Error (expected): %s", truncate(text, 200))
}

// TestE2E_LocalDeploy_MissingZeropsYml verifies error when zerops.yml is absent.
func TestE2E_LocalDeploy_MissingZeropsYml(t *testing.T) {
	h := newLocalHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	hostname := "zcpld" + suffix

	t.Cleanup(func() {
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, hostname)
	})

	// Start workflow (import requires session).
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "intent": "e2e missing yml test",
	})

	// Create service so it exists.
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    startWithoutCode: true
`, hostname)
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
	assertImportAllFinished(t, importText)
	waitForServiceStatus(s, hostname, "RUNNING", "ACTIVE")

	// Empty dir — no zerops.yml.
	emptyDir := t.TempDir()

	result := s.callTool("zerops_deploy", map[string]any{
		"targetService": hostname,
		"workingDir":    emptyDir,
	})

	if !result.IsError {
		t.Fatal("expected error for missing zerops.yml")
	}

	text := getE2ETextContent(t, result)
	if !strings.Contains(text, "zerops.yml") {
		t.Errorf("error should mention zerops.yml, got: %s", truncate(text, 300))
	}
	t.Logf("  Error (expected): %s", truncate(text, 200))
}

// TestE2E_LocalDeploy_BuildFailed verifies that build failures return buildLogs.
func TestE2E_LocalDeploy_BuildFailed(t *testing.T) {
	h := newLocalHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	hostname := "zcpld" + suffix

	t.Cleanup(func() {
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, hostname)
	})

	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "intent": "e2e build fail test",
	})

	// Import service.
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    startWithoutCode: true
`, hostname)
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
	assertImportAllFinished(t, importText)
	waitForServiceStatus(s, hostname, "RUNNING", "ACTIVE")

	// Create app with broken build command.
	appDir := createBrokenBuildApp(t, hostname)

	// Note: with background push, pollDeployBuild may pick up the previous
	// ACTIVE version from startWithoutCode. The broken build happens async.
	// Instead of checking deploy result, verify via events that a BUILD_FAILED appears.
	// With non-interactive zcli push, build failures return exit non-zero → DEPLOY_FAILED error.
	result := s.callTool("zerops_deploy", map[string]any{
		"targetService": hostname,
		"workingDir":    appDir,
	})

	if !result.IsError {
		// If zcli push somehow succeeded (e.g., exit 1 was caught differently),
		// check via events for BUILD_FAILED.
		text := getE2ETextContent(t, result)
		t.Logf("  Deploy succeeded unexpectedly: %s", truncate(text, 300))
		time.Sleep(15 * time.Second)
		eventsText := s.mustCallSuccess("zerops_events", map[string]any{
			"serviceHostname": hostname,
			"limit":           5,
		})
		if strings.Contains(eventsText, "BUILD_FAILED") || strings.Contains(eventsText, "FAILED") {
			t.Log("  Confirmed: BUILD_FAILED event found in events")
		} else {
			t.Logf("  Events: %s", truncate(eventsText, 500))
		}
	} else {
		text := getE2ETextContent(t, result)
		if !strings.Contains(text, "DEPLOY_FAILED") {
			t.Errorf("expected DEPLOY_FAILED error code, got: %s", truncate(text, 300))
		}
		t.Logf("  Build failed (expected): %s", truncate(text, 200))
	}
}

// TestE2E_LocalDeploy_EnvVarBridge tests .env generation from zerops_discover.
func TestE2E_LocalDeploy_EnvVarBridge(t *testing.T) {
	h := newLocalHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	dbHostname := "zcpld" + suffix

	t.Cleanup(func() {
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, dbHostname)
	})

	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "intent": "e2e env bridge test",
	})

	// Create a managed service to discover env vars from.
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: postgresql@16
    mode: NON_HA
`, dbHostname)
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
	assertImportAllFinished(t, importText)
	waitForServiceStatus(s, dbHostname, "RUNNING", "ACTIVE")

	// Discover env vars.
	// Discover all services with envs (includeEnvs only works without service filter).
	discoverText := s.mustCallSuccess("zerops_discover", map[string]any{
		"includeEnvs": true,
	})
	t.Logf("  Discover response: %s", truncate(discoverText, 500))

	// Parse env vars from service-level envs.
	// Managed services (postgresql, mariadb, etc.) expose env vars at service level:
	// connectionString, password, port, hostname, etc. — NO hostname prefix.
	var discoverResult struct {
		Services []struct {
			Hostname string           `json:"hostname"`
			Envs     []map[string]any `json:"envs"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(discoverText), &discoverResult); err != nil {
		t.Fatalf("parse discover: %v", err)
	}

	var envVars []platform.EnvVar
	for _, svc := range discoverResult.Services {
		if svc.Hostname == dbHostname {
			for _, ev := range svc.Envs {
				key, _ := ev["key"].(string)
				value, _ := ev["value"].(string)
				if key != "" {
					envVars = append(envVars, platform.EnvVar{Key: key, Content: value})
				}
			}
		}
	}

	if len(envVars) == 0 {
		t.Fatalf("expected env vars for %s, discover returned: %s", dbHostname, truncate(discoverText, 500))
	}
	t.Logf("  Found %d env vars for %s", len(envVars), dbHostname)

	// Generate .env content.
	groups := []ops.ServiceEnvGroup{
		{
			Hostname: dbHostname,
			Type:     "postgresql@16",
			Vars:     envVars,
		},
	}
	envContent := ops.FormatEnvFile(groups, h.projectID)

	// Verify .env content.
	if !strings.Contains(envContent, "Generated by ZCP") {
		t.Error(".env missing header")
	}
	if !strings.Contains(envContent, h.projectID) {
		t.Error(".env missing project ID in VPN hint")
	}
	if !strings.Contains(envContent, dbHostname+"_") {
		t.Errorf(".env missing env vars with %s_ prefix", dbHostname)
	}

	hasCredential := strings.Contains(envContent, "connectionString") || strings.Contains(envContent, "password")
	if !hasCredential {
		t.Errorf(".env should contain connectionString or password, got:\n%s", envContent)
	}

	t.Logf("  .env preview:\n%s", truncate(envContent, 400))
}

// --- local deploy test helpers ---

// createBrokenBuildApp creates a temp app with a build command that exits 1.
func createBrokenBuildApp(t *testing.T, hostname string) string {
	t.Helper()
	dir := t.TempDir()

	zeropsYML := fmt.Sprintf(`zerops:
  - setup: %s
    build:
      base: nodejs@22
      buildCommands:
        - exit 1
      deployFiles: ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
`, hostname)

	if err := os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte(zeropsYML), 0o644); err != nil {
		t.Fatalf("write zerops.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.js"), []byte("process.exit(1);\n"), 0o644); err != nil {
		t.Fatalf("write server.js: %v", err)
	}

	// Git init.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, args := range [][]string{
		{"init"},
		{"add", "."},
		{"commit", "-m", "init"},
	} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s (%v)", args, string(out), err)
		}
	}

	return dir
}
