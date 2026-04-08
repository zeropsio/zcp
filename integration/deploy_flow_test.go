// Tests for: integration — deploy tool flow with mock backend.
//
// Verifies the full zerops_deploy MCP tool path: discover → deploy → verify
// result shape, using a mock SSH deployer and mock API backend.

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

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
type mockSSHDeployer struct {
	output []byte
	err    error
}

func (m *mockSSHDeployer) ExecSSH(_ context.Context, _, _ string) ([]byte, error) {
	return m.output, m.err
}

// startDevelopWorkflow writes a service meta and starts a develop workflow via MCP.
// Deploy requires an active workflow session (requireWorkflow guard).
func startDevelopWorkflow(t *testing.T, session *mcp.ClientSession) {
	t.Helper()

	// Write service meta so develop workflow can start.
	stateDir := ".zcp/state"
	meta := &workflow.ServiceMeta{
		Hostname:       "app",
		Mode:           "simple",
		BootstrappedAt: "2026-01-01",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write test meta: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(".zcp")
	})

	// Start develop workflow via MCP.
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "zerops_workflow",
		Arguments: map[string]any{"action": "start", "workflow": "develop", "intent": "integration test deploy"},
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
	t.Parallel()

	mock := defaultMock()
	deployer := &mockSSHDeployer{
		output: []byte("error: push failed"),
		err:    fmt.Errorf("ssh app: exit status 1"),
	}
	session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), deployer)
	defer cleanup()

	// Start workflow session (required by deploy guard).
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "develop",
		"intent": "integration test",
	})

	result := callAndGetResult(t, session, "zerops_deploy", map[string]any{
		"targetService": "app",
	})
	if !result.IsError {
		t.Error("expected IsError for failed deploy")
	}
}
