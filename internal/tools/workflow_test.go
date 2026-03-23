// Tests for: workflow.go — zerops_workflow MCP tool handler.

package tools

import (
	"context"
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
	RegisterWorkflow(srv, nil, "", nil, nil, nil, "", "")

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
	RegisterWorkflow(srv, nil, "", nil, nil, nil, "", "")

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
	RegisterWorkflow(srv, nil, "", nil, nil, nil, "", "")

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
	RegisterWorkflow(srv, mock, "proj1", cache, nil, nil, "", "")

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
	RegisterWorkflow(srv, nil, "", nil, nil, nil, "", "")

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

// --- New Action-Based Workflow Tests ---

func TestWorkflowTool_Action_NoEngine(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "", nil, nil, nil, "", "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "start"})

	if !result.IsError {
		t.Error("expected IsError when engine is nil")
	}
}

func TestWorkflowTool_Action_UnknownAction(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "invalid"})

	if !result.IsError {
		t.Error("expected IsError for unknown action")
	}
}

func TestWorkflowTool_Action_Start_Deploy_Stateful(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)

	// Write a complete service meta so deploy start finds targets.
	meta := &workflow.ServiceMeta{
		Hostname:       "appdev",
		Mode:           "standard",
		StageHostname:  "appstage",
		DeployStrategy: workflow.StrategyPushDev,
		BootstrappedAt: "2026-03-04T12:00:00Z",
	}
	if err := workflow.WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, dir, "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "deploy",
		"intent":   "Deploy bun app",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}
	// Deploy is stateful — should create a session.
	if !engine.HasActiveSession() {
		t.Error("deploy should create a session")
	}
}

func TestWorkflowTool_Action_Start_Deploy_NoMetas(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "deploy",
		"intent":   "Deploy app",
	})

	if !result.IsError {
		t.Error("expected error when no service metas exist")
	}
}

func TestWorkflowTool_Action_Start_Deploy_IncompleteMetas(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)

	// Write an incomplete meta (no BootstrappedAt — bootstrap didn't finish).
	meta := &workflow.ServiceMeta{
		Hostname: "appdev",
		Mode:     "dev",
	}
	if err := workflow.WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, dir, "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "deploy",
		"intent":   "Deploy app",
	})

	if !result.IsError {
		t.Error("expected error when service metas are incomplete (bootstrap not finished)")
	}
}

func TestWorkflowTool_Action_Start_Immediate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		workflow string
	}{
		{"debug", "debug"},
		{"configure", "configure"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

			result := callTool(t, srv, "zerops_workflow", map[string]any{
				"action":   "start",
				"workflow": tt.workflow,
			})

			if result.IsError {
				t.Errorf("unexpected error: %s", getTextContent(t, result))
			}
			text := getTextContent(t, result)
			var resp immediateResponse
			if err := json.Unmarshal([]byte(text), &resp); err != nil {
				t.Fatalf("failed to parse immediateResponse: %v", err)
			}
			if resp.Workflow != tt.workflow {
				t.Errorf("workflow = %q, want %q", resp.Workflow, tt.workflow)
			}
			if resp.Guidance == "" {
				t.Error("expected non-empty guidance")
			}
		})
	}
}

func TestWorkflowTool_Action_Start_ImmediateNoSession(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	// Start an immediate workflow — should NOT create a session.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "debug",
	})
	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	// Verify no session was created.
	if engine.HasActiveSession() {
		t.Error("immediate workflow should not create a session")
	}
}

func TestWorkflowTool_Action_Start_AutoResetDone(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	// Start and complete a bootstrap to get to DONE.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap",
		"intent": "first bootstrap",
	})
	// Submit empty plan (managed-only) to satisfy mode gate.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []map[string]any{},
	})
	for _, step := range []string{"provision", "generate", "deploy", "close"} {
		callTool(t, srv, "zerops_workflow", map[string]any{
			"action":      "complete",
			"step":        step,
			"attestation": "Attestation for " + step + " completed ok",
		})
	}

	// Now start a new bootstrap — should auto-reset the completed session.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "second bootstrap",
	})
	if result.IsError {
		t.Errorf("expected auto-reset of completed session, got error: %s", getTextContent(t, result))
	}
}

func TestWorkflowTool_Action_Reset(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	// Start bootstrap and reset.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
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

func TestWorkflowTool_Action_ShowRemoved(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "show"})

	if !result.IsError {
		t.Error("expected IsError for removed show action")
	}
}

// --- Bootstrap Conductor Tests ---

func TestWorkflowTool_Action_BootstrapStart(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
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
	if resp.Progress.Total != 5 {
		t.Errorf("Total: want 6, got %d", resp.Progress.Total)
	}
	if resp.Current == nil || resp.Current.Name != "discover" {
		t.Error("expected current step to be 'discover'")
	}
}

func TestWorkflowTool_Action_BootstrapComplete(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap",
	})

	// Complete discover step.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "discover",
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
	if resp.Current == nil || resp.Current.Name != "provision" {
		t.Error("expected current step to be 'provision'")
	}
}

func TestWorkflowTool_Action_BootstrapComplete_MissingFields(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

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
		"action": "complete", "step": "discover",
	})
	if !result.IsError {
		t.Error("expected IsError for missing attestation")
	}
}

func TestWorkflowTool_Action_BootstrapSkip(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	// Start and advance to generate (managed-only plan for skip test).
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap",
	})
	// Submit empty plan (managed-only) so generate can be skipped.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []map[string]any{},
	})
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "provision",
		"attestation": "Attestation for provision completed ok",
	})

	// Skip generate (allowed for managed-only plan).
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "skip",
		"step":   "generate",
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
	if resp.Current == nil || resp.Current.Name != "deploy" {
		t.Error("expected current step to be 'deploy'")
	}
}

func TestWorkflowTool_Action_BootstrapStatus(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap",
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
	if resp.Progress.Total != 5 {
		t.Errorf("Total: want 6, got %d", resp.Progress.Total)
	}
}

func TestWorkflowTool_Action_BootstrapComplete_DiscoverStep_Structured(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap",
	})

	// Complete discover step with structured plan.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan": []any{
			map[string]any{
				"runtime": map[string]any{"devHostname": "appdev", "type": "bun@1.2"},
				"dependencies": []any{
					map[string]any{"hostname": "db", "type": "postgresql@16", "mode": "NON_HA", "resolution": "CREATE"},
				},
			},
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
	if resp.Current == nil || resp.Current.Name != "provision" {
		t.Error("expected current step to be 'provision'")
	}
}

func TestWorkflowTool_Action_BootstrapComplete_DiscoverStep_InvalidPlan(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap",
	})

	// Complete discover step with invalid hostname.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan": []any{
			map[string]any{
				"runtime": map[string]any{"devHostname": "my-app", "type": "bun@1.2"},
			},
		},
	})

	if !result.IsError {
		t.Error("expected error for invalid hostname in plan")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "invalid hostname") {
		t.Errorf("expected 'invalid hostname' error, got: %s", text)
	}
}

func TestWorkflow_BootstrapStart_IncludesStacks(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{
			Name:     "Go",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "go@1", Status: statusActive},
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
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, "proj1", cache, engine, nil, "", "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "go + postgres",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.AvailableStacks == "" {
		t.Error("expected availableStacks to be populated")
	}
	if !strings.Contains(resp.AvailableStacks, "go@1") {
		t.Errorf("availableStacks missing go@1: %s", resp.AvailableStacks)
	}
	if !strings.Contains(resp.AvailableStacks, "postgresql@16") {
		t.Errorf("availableStacks missing postgresql@16: %s", resp.AvailableStacks)
	}
}

func TestWorkflow_BootstrapStart_NoCache_OmitsStacks(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "bun app",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.AvailableStacks != "" {
		t.Errorf("expected empty availableStacks without cache, got: %s", resp.AvailableStacks)
	}
}

func TestWorkflow_BootstrapComplete_IncludesStacks_OnDiscoverStep(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{
			Name:     "Bun",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "bun@1.2", Status: statusActive},
			},
		},
	})
	cache := ops.NewStackTypeCache(1 * time.Hour)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, "proj1", cache, engine, nil, "", "")

	// Start bootstrap — current step is discover, should include stacks.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap",
	})
	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.AvailableStacks == "" {
		t.Error("expected availableStacks at discover step")
	}
	if !strings.Contains(resp.AvailableStacks, "bun@1.2") {
		t.Errorf("availableStacks missing bun@1.2: %s", resp.AvailableStacks)
	}

	// Complete discover — moves to provision, which should NOT include stacks.
	result = callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "discover",
		"attestation": "FRESH project, no existing services",
	})
	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text = getTextContent(t, result)
	var resp2 workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp2); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp2.AvailableStacks != "" {
		t.Errorf("expected empty availableStacks at provision step, got: %s", resp2.AvailableStacks)
	}
}

func TestWorkflow_BootstrapStatus_IncludesStacks(t *testing.T) {
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
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, "proj1", cache, engine, nil, "", "")

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap",
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
	if resp.AvailableStacks == "" {
		t.Error("expected availableStacks in status response")
	}
	if !strings.Contains(resp.AvailableStacks, "nodejs@22") {
		t.Errorf("availableStacks missing nodejs@22: %s", resp.AvailableStacks)
	}
}

func TestWorkflowTool_Action_Resume_MissingSessionID(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "resume",
	})
	if !result.IsError {
		t.Error("expected error for resume without sessionId")
	}
}

// --- Item 26: populateStacks gated to discover+generate ---

func TestWorkflowTool_BootstrapStatus_NoStacks_DeployStep(t *testing.T) {
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
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, "proj1", cache, engine, nil, "", "")

	// Start bootstrap and advance to deploy step.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap",
	})
	for _, step := range []string{"discover", "provision", "generate"} {
		callTool(t, srv, "zerops_workflow", map[string]any{
			"action": "complete", "step": step,
			"attestation": "Attestation for " + step + " completed ok",
		})
	}

	// At deploy step, status should NOT include stacks.
	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "status"})
	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.AvailableStacks != "" {
		t.Errorf("expected empty availableStacks at deploy step, got: %s", resp.AvailableStacks)
	}
}

func TestWorkflowTool_Action_BootstrapComplete_DiscoverStep_FallbackAttestation(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap",
	})

	// Complete discover step with attestation only (no structured plan).
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "discover",
		"attestation": "Services: appdev (bun@1.2), db (postgresql@16 NON_HA) — validated manually",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
}

func TestWorkflowTool_Resume_Bootstrap_ReturnsBootstrapResponse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)

	// Start bootstrap and advance to provision.
	resp, err := engine.BootstrapStart("proj1", "bun + postgres")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	sessionID := resp.SessionID

	// Complete discover to advance to provision.
	if _, err := engine.BootstrapComplete(context.Background(), "discover", "FRESH project, plan submitted", nil); err != nil {
		t.Fatalf("BootstrapComplete: %v", err)
	}

	// Overwrite session PID to a dead value.
	state, err := workflow.LoadSessionByID(dir, sessionID)
	if err != nil {
		t.Fatalf("LoadSessionByID: %v", err)
	}
	state.PID = 9999999
	if err := workflow.SaveSessionState(dir, sessionID, state); err != nil {
		t.Fatalf("SaveSessionState: %v", err)
	}

	// Create new engine (fresh PID) and resume.
	engine2 := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine2, nil, "", "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":    "resume",
		"sessionId": sessionID,
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var bootstrapResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &bootstrapResp); err != nil {
		t.Fatalf("failed to parse BootstrapResponse: %v", err)
	}
	if bootstrapResp.Current == nil {
		t.Fatal("expected non-nil current step")
	}
	if bootstrapResp.Progress.Total != 5 {
		t.Errorf("Progress.Total: want 5, got %d", bootstrapResp.Progress.Total)
	}
	if bootstrapResp.Current.DetailedGuide == "" {
		t.Error("expected non-empty detailedGuide in resume response")
	}
}

func TestWorkflowTool_Iterate_Bootstrap_ReturnsBootstrapResponse(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, engine, nil, "", "")

	// Start bootstrap and complete all steps through verify (reach iteration zone).
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "intent": "test",
	})
	for _, step := range []string{"discover", "provision", "generate", "deploy", "verify"} {
		callTool(t, srv, "zerops_workflow", map[string]any{
			"action": "complete", "step": step,
			"attestation": "Attestation for " + step + " completed ok",
		})
	}

	// Iterate — should reset to generate step and return BootstrapResponse.
	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "iterate"})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var bootstrapResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &bootstrapResp); err != nil {
		t.Fatalf("failed to parse BootstrapResponse: %v", err)
	}
	if bootstrapResp.Current == nil {
		t.Fatal("expected non-nil current step after iterate")
	}
	if bootstrapResp.Current.Name != "generate" {
		t.Errorf("Current.Name: want generate, got %s", bootstrapResp.Current.Name)
	}
	if bootstrapResp.Progress.Total != 5 {
		t.Errorf("Progress.Total: want 5, got %d", bootstrapResp.Progress.Total)
	}
}
