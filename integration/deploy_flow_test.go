// Tests for: integration — deploy tool flow with mock backend.
//
// Verifies the full zerops_deploy MCP tool path: discover → deploy → verify
// result shape, using a mock SSH deployer and mock API backend.

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
	"github.com/zeropsio/zcp/internal/workflow"
)

// mockSSHDeployer implements ops.SSHDeployer for integration tests.
// Records the hostname/command pairs from each call so tests can assert
// on which arguments reached the deployer (e.g. that workingDir
// propagated into a `cd` segment).
type mockSSHDeployer struct {
	output []byte
	err    error
	calls  []sshCall
}

type sshCall struct {
	hostname string
	command  string
}

func (m *mockSSHDeployer) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	m.calls = append(m.calls, sshCall{hostname: hostname, command: command})
	return m.output, m.err
}

func (m *mockSSHDeployer) ExecSSHBackground(_ context.Context, hostname, command string, _ time.Duration) ([]byte, error) {
	m.calls = append(m.calls, sshCall{hostname: hostname, command: command})
	return m.output, m.err
}

// startDevelopWorkflow writes a service meta, zerops.yaml, and starts a develop workflow via MCP.
// Pre-flight validation in zerops_deploy requires both the meta and zerops.yaml to exist.
func startDevelopWorkflow(t *testing.T, session *mcp.ClientSession) {
	t.Helper()

	stateDir := ".zcp/state"
	meta := &workflow.ServiceMeta{
		Hostname:       "app",
		Mode:           "simple",
		BootstrappedAt: "2026-01-01",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write test meta: %v", err)
	}

	// Write minimal zerops.yaml for pre-flight validation.
	zeropsYaml := "zerops:\n  - setup: prod\n    build:\n      base: nodejs@22\n    run:\n      start: node index.js\n"
	if err := os.WriteFile("zerops.yaml", []byte(zeropsYaml), 0o600); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}

	t.Cleanup(func() {
		os.RemoveAll(".zcp")
		os.Remove("zerops.yaml")
	})

	// Start develop workflow via MCP.
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "zerops_workflow",
		Arguments: map[string]any{
			"action":   "start",
			"workflow": "develop",
			"intent":   "integration test deploy",
			"scope":    []string{"app"},
		},
	})
	if err != nil {
		t.Fatalf("start develop workflow: %v", err)
	}
	if result.IsError {
		t.Fatalf("develop workflow start failed: %v", result.Content)
	}
}

// setupTestServerWithDeploy creates a full MCP server with a mock SSH deployer.
func setupTestServerWithDeploy(t *testing.T, mock *platform.Mock, logFetcher platform.LogFetcher, sshDeployer ops.SSHDeployer) (*mcp.ClientSession, func()) {
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

	srv := server.New(context.Background(), mock, authInfo, store, logFetcher, sshDeployer, nil, runtime.Info{})

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

func TestIntegration_DeploySSHSelfDeploy(t *testing.T) {
	mock := defaultMock()
	deployer := &mockSSHDeployer{output: []byte("push ok")}
	session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), deployer)
	defer cleanup()

	startDevelopWorkflow(t, session)

	// Step 1: Discover to find the service.
	discoverText := callAndGetText(t, session, "zerops_discover", map[string]any{
		"service": "app",
	})

	var dr ops.DiscoverResult
	if err := json.Unmarshal([]byte(discoverText), &dr); err != nil {
		t.Fatalf("parse discover result: %v", err)
	}
	if len(dr.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(dr.Services))
	}
	if dr.Services[0].Hostname != "app" {
		t.Errorf("hostname = %q, want %q", dr.Services[0].Hostname, "app")
	}

	// Step 2: Deploy to the service (self-deploy: targetService only).
	deployText := callAndGetText(t, session, "zerops_deploy", map[string]any{
		"targetService": "app",
	})

	var deployResult ops.DeployResult
	if err := json.Unmarshal([]byte(deployText), &deployResult); err != nil {
		t.Fatalf("parse deploy result: %v", err)
	}
	if deployResult.Status != "DEPLOYED" {
		t.Errorf("status = %q, want %q", deployResult.Status, "DEPLOYED")
	}
	if deployResult.Mode != "ssh" {
		t.Errorf("mode = %q, want %q", deployResult.Mode, "ssh")
	}
	if deployResult.SourceService != "app" {
		t.Errorf("sourceService = %q, want %q (auto-inferred)", deployResult.SourceService, "app")
	}
	if deployResult.BuildStatus != "ACTIVE" {
		t.Errorf("buildStatus = %q, want %q", deployResult.BuildStatus, "ACTIVE")
	}
	if deployResult.TargetService != "app" {
		t.Errorf("targetService = %q, want %q", deployResult.TargetService, "app")
	}
	if !deployResult.SSHReady {
		t.Error("expected SSHReady=true after successful deploy with SSH deployer")
	}

	// Step 3: Verify service still RUNNING after deploy.
	postDeployText := callAndGetText(t, session, "zerops_discover", map[string]any{
		"service": "app",
	})
	var postDR ops.DiscoverResult
	if err := json.Unmarshal([]byte(postDeployText), &postDR); err != nil {
		t.Fatalf("parse post-deploy discover: %v", err)
	}
	if len(postDR.Services) != 1 || postDR.Services[0].Status != "RUNNING" {
		t.Errorf("expected RUNNING after deploy, got: %+v", postDR.Services)
	}
}

func TestIntegration_DeploySSHWithWorkingDir(t *testing.T) {
	mock := defaultMock()
	deployer := &mockSSHDeployer{output: []byte("push ok")}
	session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), deployer)
	defer cleanup()

	startDevelopWorkflow(t, session)

	// Deploy with explicit workingDir.
	deployText := callAndGetText(t, session, "zerops_deploy", map[string]any{
		"targetService": "app",
		"workingDir":    "/tmp/myapp",
	})

	var deployResult ops.DeployResult
	if err := json.Unmarshal([]byte(deployText), &deployResult); err != nil {
		t.Fatalf("parse deploy result: %v", err)
	}
	if deployResult.Status != "DEPLOYED" {
		t.Errorf("status = %q, want %q", deployResult.Status, "DEPLOYED")
	}
	if deployResult.Mode != "ssh" {
		t.Errorf("mode = %q, want %q", deployResult.Mode, "ssh")
	}
	// Phase 6.1: the test name implies workingDir is exercised; assert it
	// actually propagated into the SSH command. Pre-Phase-6.1 the mock
	// deployer dropped the command string entirely, so workingDir could
	// have been any value (or nothing at all) and the test would still
	// pass. The recorded command must `cd` into the requested path.
	if len(deployer.calls) == 0 {
		t.Fatal("expected SSH deployer to be called at least once")
	}
	cmd := deployer.calls[0].command
	if !strings.Contains(cmd, "cd /tmp/myapp") {
		t.Errorf("SSH command does not cd into the workingDir; got: %s", cmd)
	}
}

func TestIntegration_DeployNotRegisteredWithoutDeployer(t *testing.T) {
	t.Parallel()

	mock := defaultMock()
	// Server created WITHOUT deployer — zerops_deploy should not be registered.
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	// Calling zerops_deploy should fail — tool not found.
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "zerops_deploy",
		Arguments: map[string]any{"targetService": "app"},
	})
	// Either the call returns an error or the result has IsError.
	if err == nil && result != nil && !result.IsError {
		t.Error("expected error calling zerops_deploy without deployer registered")
	}
}

func TestIntegration_DeployError(t *testing.T) {
	mock := defaultMock()
	deployer := &mockSSHDeployer{
		output: []byte("error: push failed"),
		err:    fmt.Errorf("ssh app: exit status 1"),
	}
	session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), deployer)
	defer cleanup()

	startDevelopWorkflow(t, session)

	result := callAndGetResult(t, session, "zerops_deploy", map[string]any{
		"targetService": "app",
	})
	if !result.IsError {
		t.Error("expected IsError for failed deploy")
	}
}
