// Tests for: v8.90 Fix A — handleStart rejects nested workflow starts.
//
// A sub-agent spawned by the main agent inside an active recipe session
// must not be able to start a second workflow. Before v8.90, calling
// `action=start workflow=develop` inside a running recipe returned
// `PREREQUISITE_MISSING: Run bootstrap first` from the develop-workflow
// prereq check — misleading because the real state is "a workflow is
// already active". v8.90 adds a top-of-handleStart check that rejects
// action=start with SUBAGENT_MISUSE when any non-immediate workflow is
// already active and the caller is asking for a different one.

package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// startRecipeSession starts a recipe session on the given engine so
// subsequent action=start calls see an active recipe.
func startRecipeSession(t *testing.T, engine *workflow.Engine) {
	t.Helper()
	if _, err := engine.RecipeStart("proj1", "test recipe for active-session", "minimal"); err != nil {
		t.Fatalf("RecipeStart: %v", err)
	}
	if !engine.HasActiveSession() {
		t.Fatal("expected active session after RecipeStart")
	}
}

// startBootstrapSession starts a bootstrap session so subsequent
// action=start calls see an active bootstrap.
func startBootstrapSession(t *testing.T, engine *workflow.Engine) {
	t.Helper()
	if _, err := engine.BootstrapStart("proj1", "test bootstrap for active-session"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	if !engine.HasActiveSession() {
		t.Fatal("expected active session after BootstrapStart")
	}
}

func TestHandleStart_SubagentMisuse_RecipeActive_DevelopStartRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	startRecipeSession(t, engine)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, dir, "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "develop",
		"intent":   "Implement features",
	})
	if !result.IsError {
		t.Fatalf("expected IsError, got success: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "SUBAGENT_MISUSE") {
		t.Errorf("expected SUBAGENT_MISUSE error code, got: %s", text)
	}
	if !strings.Contains(text, "recipe") || !strings.Contains(text, "develop") {
		t.Errorf("expected error to name both workflows (recipe, develop), got: %s", text)
	}
}

func TestHandleStart_SubagentMisuse_RecipeActive_BootstrapStartRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	startRecipeSession(t, engine)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, dir, "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "Scaffold infrastructure",
	})
	if !result.IsError {
		t.Fatalf("expected IsError, got success: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "SUBAGENT_MISUSE") {
		t.Errorf("expected SUBAGENT_MISUSE error code, got: %s", text)
	}
	if !strings.Contains(text, "recipe") || !strings.Contains(text, "bootstrap") {
		t.Errorf("expected error to name both workflows (recipe, bootstrap), got: %s", text)
	}
}

func TestHandleStart_SubagentMisuse_BootstrapActive_RecipeStartRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	startBootstrapSession(t, engine)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, dir, "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "start",
		"workflow":    "recipe",
		"intent":      "Create a Laravel recipe",
		"tier":        "minimal",
		"clientModel": "claude-opus-4-6[1m]",
	})
	if !result.IsError {
		t.Fatalf("expected IsError, got success: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "SUBAGENT_MISUSE") {
		t.Errorf("expected SUBAGENT_MISUSE error code, got: %s", text)
	}
	if !strings.Contains(text, "bootstrap") || !strings.Contains(text, "recipe") {
		t.Errorf("expected error to name both workflows, got: %s", text)
	}
}

// TestHandleStart_ImmediateWorkflow_NotRejected — cicd is stateless,
// not session-creating, so the active-session check must not apply.
func TestHandleStart_ImmediateWorkflow_NotRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	startRecipeSession(t, engine)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, dir, "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "cicd",
	})
	if result.IsError {
		t.Fatalf("cicd start should not be rejected inside an active recipe, got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if strings.Contains(text, "SUBAGENT_MISUSE") {
		t.Errorf("cicd start must not emit SUBAGENT_MISUSE, got: %s", text)
	}
}

// TestHandleStart_FreshSession_NoSubagentMisuse — a fresh session must not
// emit SUBAGENT_MISUSE for any workflow name. (The workflow's own prereq
// checks may still fire — we only assert the top-level check is silent.)
func TestHandleStart_FreshSession_NoSubagentMisuse(t *testing.T) {
	t.Parallel()
	names := []string{"bootstrap", "recipe", "develop", "cicd"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)
			if engine.HasActiveSession() {
				t.Fatal("fresh engine should have no active session")
			}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, dir, "", nil, runtime.Info{})

			result := callTool(t, srv, "zerops_workflow", map[string]any{
				"action":      "start",
				"workflow":    name,
				"intent":      "fresh session probe",
				"tier":        "minimal",
				"clientModel": "claude-opus-4-6[1m]",
			})
			// We don't assert non-error: bootstrap requires real services, develop
			// requires service metas, etc. We only assert the top-level
			// SUBAGENT_MISUSE check does NOT fire on a fresh session.
			text := getTextContent(t, result)
			if strings.Contains(text, "SUBAGENT_MISUSE") {
				t.Errorf("fresh session must not emit SUBAGENT_MISUSE for %q, got: %s", name, text)
			}
		})
	}
}

// TestHandleStart_SameWorkflowReStart_FallsThroughToSpecificHandler — when
// the active workflow and the requested workflow match, the top-level check
// allows the call; the workflow-specific handler owns idempotency.
func TestHandleStart_SameWorkflowReStart_FallsThroughToSpecificHandler(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	startRecipeSession(t, engine)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, dir, "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "start",
		"workflow":    "recipe",
		"intent":      "Create a Laravel recipe",
		"tier":        "minimal",
		"clientModel": "claude-opus-4-6[1m]",
	})
	// Whatever the recipe handler returns (error or success for idempotent
	// re-start), it must NOT be SUBAGENT_MISUSE — that code is reserved
	// for the OTHER-workflow case.
	text := getTextContent(t, result)
	if strings.Contains(text, "SUBAGENT_MISUSE") {
		t.Errorf("re-starting the same workflow must not emit SUBAGENT_MISUSE, got: %s", text)
	}
}

// TestSubagentMisuseError_MessageShape — the suggestion text must point
// sub-agents at their scoped dispatch brief, not at `workflow=bootstrap`.
// This is the precise copy a v25 sub-agent would have benefited from.
func TestSubagentMisuseError_MessageShape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	startRecipeSession(t, engine)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, dir, "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "develop",
		"intent":   "Implement features",
	})
	if !result.IsError {
		t.Fatalf("expected IsError, got success")
	}
	text := getTextContent(t, result)

	wants := []string{
		"SUBAGENT_MISUSE",
		"do NOT call zerops_workflow",
		"scoped task",
	}
	for _, w := range wants {
		if !strings.Contains(text, w) {
			t.Errorf("error message missing %q\nGot: %s", w, text)
		}
	}
}
