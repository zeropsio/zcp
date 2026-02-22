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

// --- Bootstrap Conductor Tests ---

func TestWorkflowTool_Action_BootstrapStart(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine)

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"mode":     "full",
		"intent":   "bun + postgres",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Progress.Total != 10 {
		t.Errorf("Total: want 10, got %d", resp.Progress.Total)
	}
	if resp.Current == nil || resp.Current.Name != "detect" {
		t.Error("expected current step to be 'detect'")
	}
}

func TestWorkflowTool_Action_BootstrapComplete(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine)

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "mode": "full",
	})

	// Complete detect step.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "detect",
		"attestation": "FRESH project, no existing services",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Current == nil || resp.Current.Name != "plan" {
		t.Error("expected current step to be 'plan'")
	}
}

func TestWorkflowTool_Action_BootstrapComplete_MissingFields(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine)

	// Missing step.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "attestation": "test attestation here",
	})
	if !result.IsError {
		t.Error("expected IsError for missing step")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "INVALID_PARAMETER") {
		t.Errorf("expected INVALID_PARAMETER error, got: %s", text)
	}

	// Missing attestation.
	result = callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "detect",
	})
	if !result.IsError {
		t.Error("expected IsError for missing attestation")
	}
}

func TestWorkflowTool_Action_BootstrapSkip(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine)

	// Start and advance to mount-dev.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "mode": "full",
	})
	preSteps := []string{"detect", "plan", "load-knowledge", "generate-import", "import-services"}
	for _, step := range preSteps {
		callTool(t, srv, "zerops_workflow", map[string]any{
			"action": "complete", "step": step,
			"attestation": "Attestation for " + step + " completed ok",
		})
	}

	// Skip mount-dev.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "skip",
		"step":   "mount-dev",
		"reason": "no runtime services",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Current == nil || resp.Current.Name != "discover-envs" {
		t.Error("expected current step to be 'discover-envs'")
	}
}

func TestWorkflowTool_Action_BootstrapStatus(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine)

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "mode": "full",
	})

	// Get status.
	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "status"})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Progress.Total != 10 {
		t.Errorf("Total: want 10, got %d", resp.Progress.Total)
	}
}

func TestWorkflowTool_Action_QuickMode(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "", nil, nil)

	// Quick mode with legacy workflow=bootstrap should return full markdown.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"workflow": "bootstrap",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	// Should contain markdown content (not JSON).
	if strings.HasPrefix(text, "{") {
		t.Error("quick/legacy mode should return markdown, not JSON")
	}
}
