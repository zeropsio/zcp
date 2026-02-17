// Tests for: workflow.go — zerops_workflow MCP tool handler.

package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// --- Legacy Workflow Tests ---

func TestWorkflowTool_NoParams_ReturnsError(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "", nil, nil)

	result := callTool(t, srv, "zerops_workflow", nil)

	if !result.IsError {
		t.Error("expected IsError for empty call")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "No workflow specified") {
		t.Errorf("expected 'No workflow specified' error, got: %s", text)
	}
}

func TestWorkflowTool_Specific(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "", nil, nil)

	// "bootstrap" is one of the known workflows.
	result := callTool(t, srv, "zerops_workflow", map[string]any{"workflow": "bootstrap"})

	if result.IsError {
		t.Error("unexpected IsError")
	}
	text := getTextContent(t, result)
	if text == "" {
		t.Error("expected non-empty workflow content")
	}
}

func TestWorkflowTool_NotFound(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "", nil, nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{"workflow": "nonexistent_workflow"})

	if !result.IsError {
		t.Error("expected IsError for unknown workflow")
	}
}

func TestWorkflowTool_Bootstrap_IncludesStacks(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{
			Name:     "Node.js",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "nodejs@22", Status: statusActive},
			},
		},
		{
			Name:     "PostgreSQL",
			Category: "STANDARD",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "postgresql@16", Status: statusActive},
			},
		},
	})
	cache := ops.NewStackTypeCache(1 * time.Hour)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, "proj1", cache, nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{"workflow": "bootstrap"})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "Available Service Stacks") {
		t.Error("bootstrap workflow missing injected stacks")
	}
}

func TestWorkflowTool_Bootstrap_NoCache(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "", nil, nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{"workflow": "bootstrap"})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	// Should not have live stacks section injected (no cache/client)
	if strings.Contains(text, "## Available Service Stacks (live)") {
		t.Error("bootstrap without cache should not contain injected stacks header")
	}
}

func TestWorkflowTool_Scale_NoStacks(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{
			Name:     "Node.js",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "nodejs@22", Status: statusActive},
			},
		},
	})
	cache := ops.NewStackTypeCache(1 * time.Hour)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, "proj1", cache, nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{"workflow": "scale"})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if strings.Contains(text, "Available Service Stacks") {
		t.Error("scale workflow should not contain stacks")
	}
}

// --- New Action-Based Workflow Tests ---

func TestWorkflowTool_Action_NoEngine(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "", nil, nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "start"})

	if !result.IsError {
		t.Error("expected IsError when engine is nil")
	}
}

func TestWorkflowTool_Action_UnknownAction(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine)

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "invalid"})

	if !result.IsError {
		t.Error("expected IsError for unknown action")
	}
}

func TestWorkflowTool_Action_Start(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine)

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start",
		"mode":   "full",
		"intent": "Deploy bun app",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var state workflow.WorkflowState
	if err := json.Unmarshal([]byte(text), &state); err != nil {
		t.Fatalf("failed to parse state: %v", err)
	}
	if state.Mode != "full" {
		t.Errorf("mode = %q, want full", state.Mode)
	}
	if state.Phase != "INIT" {
		t.Errorf("phase = %q, want INIT", state.Phase)
	}
}

func TestWorkflowTool_Action_Start_MissingMode(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine)

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "start"})

	if !result.IsError {
		t.Error("expected IsError for missing mode")
	}
}

func TestWorkflowTool_Action_Evidence(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine)

	// Start session first.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "mode": "full",
	})

	// Record evidence.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "evidence",
		"type":        "recipe_review",
		"attestation": "loaded bun+pg knowledge",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "recorded") {
		t.Errorf("expected recorded status, got: %s", text)
	}
}

func TestWorkflowTool_Action_Evidence_MissingFields(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine)

	// Missing type.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "evidence", "attestation": "test",
	})
	if !result.IsError {
		t.Error("expected IsError for missing type")
	}

	// Missing attestation.
	result = callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "evidence", "type": "recipe_review",
	})
	if !result.IsError {
		t.Error("expected IsError for missing attestation")
	}
}

func TestWorkflowTool_Action_TransitionWithGate(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine)

	// Start session.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "mode": "full",
	})

	// Try transition without evidence — should fail.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "transition", "phase": "DISCOVER",
	})
	if !result.IsError {
		t.Error("expected gate failure without evidence")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "GATE_FAILED") {
		t.Errorf("expected GATE_FAILED code, got: %s", text)
	}

	// Record evidence.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "evidence", "type": "recipe_review", "attestation": "ok",
	})

	// Transition should now succeed.
	result = callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "transition", "phase": "DISCOVER",
	})
	if result.IsError {
		t.Errorf("unexpected error after evidence: %s", getTextContent(t, result))
	}
}

func TestWorkflowTool_Action_Reset(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine)

	// Start and reset.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "mode": "full",
	})
	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "reset"})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "reset") {
		t.Errorf("expected reset confirmation, got: %s", text)
	}
}

func TestWorkflowTool_Action_Transition_MissingPhase(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine)

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "transition"})

	if !result.IsError {
		t.Error("expected IsError for missing phase")
	}
}

func TestWorkflowTool_Action_ShowRemoved(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine)

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "show"})

	if !result.IsError {
		t.Error("expected IsError for removed show action")
	}
}
