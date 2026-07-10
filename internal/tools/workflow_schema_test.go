// Tests for: WorkflowInput schema generation — pin F1 fix
// (launchKey accepted on input schema; mcp-go no longer rejects
// the agent's call with "unexpected additional properties").
//
// P-LP-1 is enforced at the OUTPUT boundary — response/state/audit
// struct shape. Those pins live in workflow_launch_production_test.go
// (TestHandleLaunchProduction_LaunchKeyNeverInResponse,
// TestLaunchState_NoLaunchKeyFieldExists,
// TestAuditLog_NeverContainsLaunchKey). This file pins the INPUT
// side: schema must expose the property AND CallTool must accept it.
package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
	"github.com/zeropsio/zcp/internal/tools"
)

// schemaTestSession bootstraps an MCP session against a fresh ZCP server
// and returns the registered zerops_workflow tool's schema + a live
// client session for CallTool regressions. Used by F1 schema tests.
func schemaTestSession(t *testing.T) (*mcp.Tool, *mcp.ClientSession) {
	t.Helper()

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test"}).
		WithServices(nil)
	authInfo := &auth.Info{ProjectID: "p1", Token: "test", APIHost: "localhost"}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	logFetcher := platform.NewMockLogFetcher()

	srv := server.New(context.Background(), mock, authInfo, store, logFetcher, &nopSSH{}, &nopMounter{}, runtime.Info{})

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.MCPServer().Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	for _, tool := range result.Tools {
		if tool.Name == "zerops_workflow" {
			return tool, session
		}
	}
	t.Fatal("zerops_workflow not registered")
	return nil, nil
}

// TestWorkflowToolSchema_AcceptsLaunchKey reproduces the F1 critical
// bug: before the fix, WorkflowInput.LaunchKey was tagged json:"-"
// which made encoding/json strip it from BOTH directions AND made the
// mcp-go schema-generator omit it as a property. Agents got
// "unexpected additional properties [launchKey]" on every publish
// call, blocking the entire launch-production workflow.
//
// After F1: launchKey is a known property in the input schema with
// a non-empty description so the agent gets schema-driven guidance.
func TestWorkflowToolSchema_AcceptsLaunchKey(t *testing.T) {
	t.Parallel()

	tool, _ := schemaTestSession(t)

	if tool.InputSchema == nil {
		t.Fatal("zerops_workflow has nil input schema")
	}
	schemaJSON, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	schemaStr := string(schemaJSON)

	if !strings.Contains(schemaStr, `"launchKey"`) {
		t.Errorf("schema must declare launchKey property after F1 fix:\n%s", schemaStr)
	}
	// Description must mention the field's launch-production binding so
	// the agent recognizes when to populate it. The exact wording is
	// allowed to drift; the substring "Launch-production" anchors it.
	if !strings.Contains(schemaStr, "Launch-production") {
		t.Errorf("launchKey property must have a description mentioning launch-production; got:\n%s", schemaStr)
	}
}

// TestWorkflowTool_CallToolWithLaunchKey_NoAdditionalPropertiesError
// is the regression that reproduces the exact failure agents hit in
// the wild before F1. Pre-fix: the SDK validated input against the
// generated schema, saw launchKey was not a known property, and
// returned the call with an error stating "unexpected additional
// properties [launchKey]". The handler never ran; the agent had no
// path to publish.
//
// Post-fix: the call passes schema validation and reaches the
// handler. The handler in this minimal harness has nil source-project
// envs etc., so it will likely return a scope-prompt or auth-failure
// response — the assertion is ONLY that no "additional properties"
// SDK-side validation error fires.
func TestWorkflowTool_CallToolWithLaunchKey_NoAdditionalPropertiesError(t *testing.T) {
	t.Parallel()

	_, session := schemaTestSession(t)

	ctx := context.Background()
	args := map[string]any{
		"workflow":              "launch-production",
		"productionProjectName": "myapp-prod",
		"targetService":         "appdev",
		"launchKey":             "test-launch-key-arg",
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "zerops_workflow",
		Arguments: args,
	})
	if err != nil {
		// Schema-validation failure shows up here as an error. Pre-F1
		// reproduction: "Invalid arguments for tool ... unexpected
		// additional properties [launchKey]".
		if strings.Contains(err.Error(), "additional properties") &&
			strings.Contains(err.Error(), "launchKey") {
			t.Fatalf("F1 regression: SDK rejected launchKey as additional property:\n%v", err)
		}
		// Other errors (transport, etc.) are unexpected setup issues.
		t.Fatalf("unexpected CallTool error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil CallTool result")
	}
	// The handler may return an error response (IsError=true) for
	// in-memory mock context — that's fine. We only verify the call
	// reached the handler at all (schema validation passed).
}

// TestWorkflowInput_UnmarshalsLaunchKeyArg is a compile-time-adjacent
// pin: json.Unmarshal of an arg map containing launchKey must populate
// WorkflowInput.LaunchKey. Drift alarm if someone re-applies json:"-"
// or renames the field.
func TestWorkflowInput_UnmarshalsLaunchKeyArg(t *testing.T) {
	t.Parallel()

	const sentinel = "test-launch-key-value-XYZ"
	body := `{"workflow":"launch-production","launchKey":"` + sentinel + `"}`

	var input tools.WorkflowInput
	if err := json.Unmarshal([]byte(body), &input); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if input.LaunchKey != sentinel {
		t.Errorf("LaunchKey: got %q want %q", input.LaunchKey, sentinel)
	}
	if input.Workflow != "launch-production" {
		t.Errorf("Workflow: got %q want launch-production", input.Workflow)
	}
}

// TestWorkflowInputSchema_PublishesConfirmLaunch pins §4.1 of the
// token-delegation spec (plans/token-delegation-implementation-spec-2026-07-10.md):
// confirmLaunch must be a known schema property (like launchKey before
// it) so an agent sending it is not rejected with "unexpected additional
// properties [confirmLaunch]".
func TestWorkflowInputSchema_PublishesConfirmLaunch(t *testing.T) {
	t.Parallel()

	tool, _ := schemaTestSession(t)

	if tool.InputSchema == nil {
		t.Fatal("zerops_workflow has nil input schema")
	}
	schemaJSON, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	schemaStr := string(schemaJSON)

	if !strings.Contains(schemaStr, `"confirmLaunch"`) {
		t.Errorf("schema must declare confirmLaunch property:\n%s", schemaStr)
	}
	if !strings.Contains(schemaStr, "delegated") {
		t.Errorf("confirmLaunch property must have a description mentioning the delegated path; got:\n%s", schemaStr)
	}
}

// TestWorkflowInput_UnmarshalsConfirmLaunch_DirectBool pins the FlexBool
// direct-boolean unmarshal path for confirmLaunch.
func TestWorkflowInput_UnmarshalsConfirmLaunch_DirectBool(t *testing.T) {
	t.Parallel()

	body := `{"workflow":"launch-production","confirmLaunch":true}`
	var input tools.WorkflowInput
	if err := json.Unmarshal([]byte(body), &input); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !input.ConfirmLaunch.Bool() {
		t.Error("ConfirmLaunch: got false want true (direct JSON boolean)")
	}
}

// TestWorkflowInput_UnmarshalsConfirmLaunch_StringTrue pins the FlexBool
// stringified-boolean unmarshal path for confirmLaunch — the class of
// agent behavior FlexBool exists to absorb (see flexbool.go doc-comment).
func TestWorkflowInput_UnmarshalsConfirmLaunch_StringTrue(t *testing.T) {
	t.Parallel()

	body := `{"workflow":"launch-production","confirmLaunch":"true"}`
	var input tools.WorkflowInput
	if err := json.Unmarshal([]byte(body), &input); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !input.ConfirmLaunch.Bool() {
		t.Error(`ConfirmLaunch: got false want true (stringified "true")`)
	}
}
