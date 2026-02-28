// Tests for: integration — deploy tool flow with mock backend.
//
// Verifies the full zerops_deploy MCP tool path: discover → deploy → verify
// result shape, using a mock local deployer and mock API backend.

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// mockLocalDeployer implements ops.LocalDeployer for integration tests.
type mockLocalDeployer struct {
	output []byte
	err    error
}

func (m *mockLocalDeployer) ExecZcli(_ context.Context, _ ...string) ([]byte, error) {
	return m.output, m.err
}

// setupTestServerWithDeploy creates a full MCP server with a mock local deployer.
func setupTestServerWithDeploy(t *testing.T, mock *platform.Mock, logFetcher platform.LogFetcher, localDeployer ops.LocalDeployer) (*mcp.ClientSession, func()) {
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

	srv := server.New(context.Background(), mock, authInfo, store, logFetcher, nil, localDeployer, nil, nil, runtime.Info{})

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

func TestIntegration_DeployLocalFlow(t *testing.T) {
	t.Parallel()

	mock := defaultMock()
	deployer := &mockLocalDeployer{output: []byte("push ok")}
	session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), deployer)
	defer cleanup()

	// Start workflow session (required by deploy guard).
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "deploy",
		"intent": "integration test",
	})

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

	// Step 2: Deploy to the service.
	deployText := callAndGetText(t, session, "zerops_deploy", map[string]any{
		"targetService": "app",
	})

	var deployResult ops.DeployResult
	if err := json.Unmarshal([]byte(deployText), &deployResult); err != nil {
		t.Fatalf("parse deploy result: %v", err)
	}
	// Deploy now blocks until build completes — status should be DEPLOYED.
	if deployResult.Status != "DEPLOYED" {
		t.Errorf("status = %q, want %q", deployResult.Status, "DEPLOYED")
	}
	if deployResult.Mode != "local" {
		t.Errorf("mode = %q, want %q", deployResult.Mode, "local")
	}
	if deployResult.BuildStatus != "ACTIVE" {
		t.Errorf("buildStatus = %q, want %q", deployResult.BuildStatus, "ACTIVE")
	}
	if deployResult.TargetService != "app" {
		t.Errorf("targetService = %q, want %q", deployResult.TargetService, "app")
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

func TestIntegration_DeployLocalWithWorkingDir(t *testing.T) {
	t.Parallel()

	mock := defaultMock()
	deployer := &mockLocalDeployer{output: []byte("push ok")}
	session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), deployer)
	defer cleanup()

	// Start workflow session (required by deploy guard).
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "deploy",
		"intent": "integration test",
	})

	// Deploy with explicit workingDir.
	deployText := callAndGetText(t, session, "zerops_deploy", map[string]any{
		"targetService": "app",
		"workingDir":    "/tmp/myapp",
	})

	var deployResult ops.DeployResult
	if err := json.Unmarshal([]byte(deployText), &deployResult); err != nil {
		t.Fatalf("parse deploy result: %v", err)
	}
	// Deploy now blocks until build completes — status should be DEPLOYED.
	if deployResult.Status != "DEPLOYED" {
		t.Errorf("status = %q, want %q", deployResult.Status, "DEPLOYED")
	}
	if deployResult.Mode != "local" {
		t.Errorf("mode = %q, want %q", deployResult.Mode, "local")
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
	deployer := &mockLocalDeployer{
		output: []byte("error: push failed"),
		err:    fmt.Errorf("zcli push: exit status 1"),
	}
	session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), deployer)
	defer cleanup()

	// Start workflow session (required by deploy guard).
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "deploy",
		"intent": "integration test",
	})

	result := callAndGetResult(t, session, "zerops_deploy", map[string]any{
		"targetService": "app",
	})
	if !result.IsError {
		t.Error("expected IsError for failed deploy")
	}
}
