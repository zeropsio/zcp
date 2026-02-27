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
	RegisterWorkflow(srv, nil, "", nil, nil, nil)

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
	RegisterWorkflow(srv, nil, "", nil, nil, nil)

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
	RegisterWorkflow(srv, nil, "", nil, nil, nil)

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
	RegisterWorkflow(srv, mock, "proj1", cache, nil, nil)

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
	RegisterWorkflow(srv, nil, "", nil, nil, nil)

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
	RegisterWorkflow(srv, mock, "proj1", cache, nil, nil)

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
	RegisterWorkflow(srv, nil, "", nil, nil, nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "start"})

	if !result.IsError {
		t.Error("expected IsError when engine is nil")
	}
}

func TestWorkflowTool_Action_UnknownAction(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "invalid"})

	if !result.IsError {
		t.Error("expected IsError for unknown action")
	}
}

func TestWorkflowTool_Action_Start(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start",
		"mode":   "full",
		"intent": "Deploy bun app",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp startResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse startResponse: %v", err)
	}
	if resp.Mode != "full" {
		t.Errorf("mode = %q, want full", resp.Mode)
	}
	if resp.Phase != "INIT" {
		t.Errorf("phase = %q, want INIT", resp.Phase)
	}
	if resp.SessionID == "" {
		t.Error("expected non-empty sessionId")
	}
}

func TestWorkflowTool_Action_Start_Deploy_ReturnsGuidance(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "deploy",
		"mode":     "full",
		"intent":   "Deploy to production",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp startResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse startResponse: %v", err)
	}
	if resp.Guidance == "" {
		t.Error("expected non-empty guidance for deploy workflow")
	}
	if resp.Mode != "full" {
		t.Errorf("mode = %q, want full", resp.Mode)
	}
}

func TestWorkflowTool_Action_Start_NoWorkflow_NoGuidance(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start",
		"mode":   "full",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp startResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse startResponse: %v", err)
	}
	if resp.Guidance != "" {
		t.Errorf("expected empty guidance without workflow, got: %s", resp.Guidance[:50])
	}
}

func TestWorkflowTool_Action_Start_MissingMode(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "start"})

	if !result.IsError {
		t.Error("expected IsError for missing mode")
	}
}

func TestWorkflowTool_Action_Evidence(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

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
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

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
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

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
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

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
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "transition"})

	if !result.IsError {
		t.Error("expected IsError for missing phase")
	}
}

func TestWorkflowTool_Action_ShowRemoved(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

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
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

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
	if resp.Progress.Total != 11 {
		t.Errorf("Total: want 11, got %d", resp.Progress.Total)
	}
	if resp.Current == nil || resp.Current.Name != "detect" {
		t.Error("expected current step to be 'detect'")
	}
}

func TestWorkflowTool_Action_BootstrapComplete(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

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
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

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
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

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
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

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
	if resp.Progress.Total != 11 {
		t.Errorf("Total: want 11, got %d", resp.Progress.Total)
	}
}

func TestWorkflowTool_Action_BootstrapComplete_PlanStep_Structured(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

	// Start bootstrap and complete detect.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "mode": "full",
	})
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "detect",
		"attestation": "FRESH project, no existing services",
	})

	// Complete plan step with structured plan.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "plan",
		"plan": []any{
			map[string]any{"hostname": "appdev", "type": "bun@1.2"},
			map[string]any{"hostname": "db", "type": "postgresql@16", "mode": "NON_HA"},
		},
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Current == nil || resp.Current.Name != "load-knowledge" {
		t.Error("expected current step to be 'load-knowledge'")
	}
}

func TestWorkflowTool_Action_BootstrapComplete_PlanStep_InvalidPlan(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

	// Start bootstrap and complete detect.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "mode": "full",
	})
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "detect",
		"attestation": "FRESH project, no existing services",
	})

	// Complete plan step with invalid hostname.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "plan",
		"plan": []any{
			map[string]any{"hostname": "my-app", "type": "bun@1.2"},
		},
	})

	if !result.IsError {
		t.Error("expected error for invalid hostname in plan")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "invalid characters") {
		t.Errorf("expected 'invalid characters' error, got: %s", text)
	}
}

func TestWorkflowTool_Action_BootstrapComplete_PlanStep_FallbackAttestation(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

	// Start bootstrap and complete detect.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "mode": "full",
	})
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "detect",
		"attestation": "FRESH project, no existing services",
	})

	// Complete plan step with attestation only (no structured plan) — should still work via JSON parse fallback or plain attestation.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "plan",
		"attestation": "Services: appdev (bun@1.2), db (postgresql@16 NON_HA) — validated manually",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
}

func TestWorkflowTool_Action_QuickMode_Bootstrap(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil)

	// Quick mode bootstrap now uses the conductor (returns BootstrapResponse).
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"mode":     "quick",
		"intent":   "quick test",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse BootstrapResponse: %v", err)
	}
	if resp.Progress.Total != 11 {
		t.Errorf("Total: want 11, got %d", resp.Progress.Total)
	}
	if resp.Mode != "quick" {
		t.Errorf("Mode: want quick, got %s", resp.Mode)
	}
}
