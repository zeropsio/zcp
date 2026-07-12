// Tests for: integration — delegated launch-token minting, MCP-tool tier
// (plans/archive/token-delegation-implementation-spec-2026-07-10.md §4.6).
//
// Scope deliberately narrow: the published confirmLaunch schema, the
// ready-to-launch delegatedLaunch availability decoration, and the
// no-delegation manual fallback. The full mint → admin → stage → create
// mutation path is NOT exercised here — projectAdminClientFactory is
// private to internal/tools (the cross-project admin-client seam) and
// this package intentionally has no test-only setter for it. That path
// is covered by internal/tools/launch_delegation_test.go, where the
// factory can be injected.
package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/tools"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// launchDelegationSSHStub returns canned responses for the launch
// source-state reader's SSH commands — a source with a valid `setup:
// prod` block, mirroring workflow_launch_production_sequence_test.go's
// launchSSHStub (internal/tools, not importable here).
type launchDelegationSSHStub struct {
	responses map[string]string
}

func (s *launchDelegationSSHStub) ExecSSH(_ context.Context, _ string, command string) ([]byte, error) {
	for k, v := range s.responses {
		if strings.Contains(command, k) {
			return []byte(v), nil
		}
	}
	return nil, nil
}

func (s *launchDelegationSSHStub) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return nil, nil
}

const launchDelegationSourceYAML = `zerops:
  - setup: dev
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      start: node dist/server.js
  - setup: prod
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      start: node dist/server.js
`

// setupLaunchDelegationServer wires a full MCP server exposing
// zerops_workflow, backed by mock so the ready-to-launch read path
// (scope → classify → ready) can run end-to-end through the tool layer.
func setupLaunchDelegationServer(t *testing.T, mock *platform.Mock, ssh ops.SSHDeployer) (*mcp.ClientSession, func()) {
	t.Helper()

	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}

	mcpSrv := mcp.NewServer(&mcp.Implementation{Name: "zcp-test", Version: "0.1"}, nil)
	stateDir := t.TempDir()
	engine := workflow.NewEngine(stateDir, workflow.EnvLocal, nil)

	// Seed a complete, gate-ready ServiceMeta via the exported writer —
	// the source-control gate (checks 1-6) requires it before scope
	// narrows into classify/ready. Mirrors internal/tools's
	// seedLaunchGateReadyMeta fixture (unexported, not importable here).
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:                   "app",
		Mode:                       topology.PlanModeSimple,
		GitPushState:               topology.GitPushConfigured,
		RemoteURL:                  launchDelegationRemoteURL,
		BuildIntegration:           topology.BuildIntegrationActions,
		BuildIntegrationVerifiedAt: "2026-06-10T00:00:00Z",
		CloseDeployMode:            topology.CloseModeAuto,
		CloseDeployModeConfirmed:   true,
		BootstrappedAt:             "2026-01-01",
	}); err != nil {
		t.Fatalf("seed service meta: %v", err)
	}

	rt := runtime.Info{InContainer: true, ServiceName: "zcp"}
	tools.RegisterWorkflow(mcpSrv, mock, nil, "source-id", nil, engine, nil, stateDir, "", nil, ssh, rt, "")
	tools.RegisterKnowledge(mcpSrv, store, mock, nil, nil, nil)

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := mcpSrv.Connect(ctx, st, nil)
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

func launchDelegationMockClient() *platform.Mock {
	return platform.NewMock().
		WithProject(&platform.Project{ID: "source-id", Name: "myapp-dev", Status: "ACTIVE"}).
		WithServices([]platform.ServiceStack{
			{
				ID:   "svc-app-src",
				Name: "app",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
				Status: "ACTIVE",
				Mode:   "NON_HA",
			},
		}).
		WithProjectEnv([]platform.ProjectEnvVar{{Key: "LOG_LEVEL", Content: "info"}})
}

// launchDelegationRemoteURL is the single canonical remote used across
// every SSH-stub response AND the seeded ServiceMeta.RemoteURL — the
// source-control gate's check 3 compares them, so they must agree.
const launchDelegationRemoteURL = "https://github.com/example/myapp.git"

func launchDelegationSSH() *launchDelegationSSHStub {
	return &launchDelegationSSHStub{responses: map[string]string{
		"cat /var/www/zerops.yaml": launchDelegationSourceYAML,
		"git remote get-url":       launchDelegationRemoteURL,
		"git rev-parse HEAD":       "abc123def456",
		"git status --porcelain":   "", // clean tree
		"ls-remote":                "abc123def456",
	}}
}

// callReadyToLaunch drives the two prerequisite calls (scope-complete,
// then classify-complete) and returns the decoded third call's response
// body — the ready-to-launch response carrying delegatedLaunch.
func callReadyToLaunch(t *testing.T, session *mcp.ClientSession) map[string]any {
	t.Helper()
	ctx := context.Background()

	// Call 1 seeds ProductionProjectName + TargetService — surfaces
	// classify-prompt (the single project env is unclassified).
	if _, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "zerops_workflow",
		Arguments: map[string]any{
			"action":                "start",
			"workflow":              "launch-production",
			"productionProjectName": "myapp-prod",
			"targetService":         "app",
			"region":                "eu-central",
		},
	}); err != nil {
		t.Fatalf("call 1 (classify-prompt): %v", err)
	}

	// Call 2 supplies the classification — surfaces ready-to-launch.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "zerops_workflow",
		Arguments: map[string]any{
			"action":                "start",
			"workflow":              "launch-production",
			"productionProjectName": "myapp-prod",
			"targetService":         "app",
			"region":                "eu-central",
			"envClassifications":    map[string]string{"LOG_LEVEL": "plain-config"},
		},
	})
	if err != nil {
		t.Fatalf("call 2 (ready-to-launch): %v", err)
	}
	if result.IsError {
		t.Fatalf("ready-to-launch call returned an error result: %v", result.Content)
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &body); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, tc.Text)
	}
	return body
}

// TestIntegration_LaunchWorkflow_PublishesConfirmLaunchSchema pins §4.1
// at the MCP-tool boundary: confirmLaunch must be a known input-schema
// property so an agent sending it is not rejected with "unexpected
// additional properties".
func TestIntegration_LaunchWorkflow_PublishesConfirmLaunchSchema(t *testing.T) {
	t.Parallel()
	session, cleanup := setupLaunchDelegationServer(t, launchDelegationMockClient(), launchDelegationSSH())
	defer cleanup()

	result, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	var schema []byte
	for _, tl := range result.Tools {
		if tl.Name == "zerops_workflow" {
			b, marshalErr := json.Marshal(tl.InputSchema)
			if marshalErr != nil {
				t.Fatalf("marshal schema: %v", marshalErr)
			}
			schema = b
		}
	}
	if schema == nil {
		t.Fatal("zerops_workflow not registered")
	}
	if !strings.Contains(string(schema), `"confirmLaunch"`) {
		t.Errorf("schema must declare confirmLaunch property:\n%s", schema)
	}
}

// TestIntegration_LaunchWorkflow_ReadyToLaunch_DelegationAvailable pins
// §4.3 at the MCP-tool boundary: a mock with a usable delegation
// decorates the ready-to-launch response with delegatedLaunch.available
// = true.
func TestIntegration_LaunchWorkflow_ReadyToLaunch_DelegationAvailable(t *testing.T) {
	t.Parallel()
	mock := launchDelegationMockClient().WithTokenDelegations(platform.TokenDelegation{
		ID: "d1", CanCreateProjects: true,
	})
	session, cleanup := setupLaunchDelegationServer(t, mock, launchDelegationSSH())
	defer cleanup()

	body := callReadyToLaunch(t, session)
	if body["status"] != "ready-to-launch" {
		t.Fatalf("status: got %v want ready-to-launch\nbody: %+v", body["status"], body)
	}
	delegated, ok := body["delegatedLaunch"].(map[string]any)
	if !ok {
		t.Fatalf("delegatedLaunch block missing or wrong shape: %+v", body)
	}
	if delegated["available"] != true {
		t.Errorf("delegatedLaunch.available: got %v want true", delegated["available"])
	}
}

// TestIntegration_LaunchWorkflow_ReadyToLaunch_NoDelegation_ManualFallback
// pins D-6 at the MCP-tool boundary: a mock with no delegation
// decorates delegatedLaunch.available=false and keeps the manual
// launchKey guidance.
func TestIntegration_LaunchWorkflow_ReadyToLaunch_NoDelegation_ManualFallback(t *testing.T) {
	t.Parallel()
	mock := launchDelegationMockClient() // no delegation seeded
	session, cleanup := setupLaunchDelegationServer(t, mock, launchDelegationSSH())
	defer cleanup()

	body := callReadyToLaunch(t, session)
	if body["status"] != "ready-to-launch" {
		t.Fatalf("status: got %v want ready-to-launch\nbody: %+v", body["status"], body)
	}
	delegated, ok := body["delegatedLaunch"].(map[string]any)
	if !ok {
		t.Fatalf("delegatedLaunch block missing or wrong shape: %+v", body)
	}
	if delegated["available"] != false {
		t.Errorf("delegatedLaunch.available: got %v want false", delegated["available"])
	}
	guidance, _ := body["guidance"].(string)
	if !strings.Contains(guidance, "launchKey") {
		t.Errorf("guidance must still carry the manual launchKey walkthrough; got %q", guidance)
	}
}
