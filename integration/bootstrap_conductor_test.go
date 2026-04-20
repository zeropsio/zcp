// Tests for: integration — bootstrap conductor full E2E flow through MCP server.
//
// Exercises the complete bootstrap conductor lifecycle:
// start → complete all 5 steps → auto-evidence → auto-transition → DONE
// Also tests: skip flow, status recovery, error cases.

package integration_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/tools"
	"github.com/zeropsio/zcp/internal/workflow"
)

// setupWorkflowServer creates a full MCP server with a workflow engine
// backed by a temp directory — required for bootstrap conductor tests.
func setupWorkflowServer(t *testing.T, mock *platform.Mock) (*mcp.ClientSession, func()) {
	t.Helper()

	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}

	mcpSrv := mcp.NewServer(
		&mcp.Implementation{Name: "zcp-test", Version: "0.1"},
		nil,
	)

	stateDir := t.TempDir()
	engine := workflow.NewEngine(stateDir, workflow.EnvLocal, nil)

	tools.RegisterWorkflow(mcpSrv, mock, "proj-1", nil, nil, engine, nil, "", "", nil, runtime.Info{})
	tools.RegisterDiscover(mcpSrv, mock, "proj-1", "")
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

func TestIntegration_BootstrapConductor_FullFlow(t *testing.T) {
	t.Parallel()

	// Use engine directly to bypass MCP tool layer and step checkers.
	// (Checkers validate service existence against mock, which doesn't support dynamic service creation.)
	stateDir := t.TempDir()
	engine := workflow.NewEngine(stateDir, workflow.EnvLocal, nil)

	// Start bootstrap.
	startResp, err := engine.BootstrapStart("proj-1", "bun + postgres app")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	if startResp.Progress.Total != 5 {
		t.Fatalf("total steps: want 5, got %d", startResp.Progress.Total)
	}
	if startResp.Current == nil || startResp.Current.Name != "discover" {
		t.Fatal("expected first step to be 'discover'")
	}

	// Submit standard mode plan (bundev/bunstage + db).
	planResp, err := engine.BootstrapCompletePlan([]workflow.BootstrapTarget{
		{
			Runtime: workflow.RuntimeTarget{DevHostname: "bundev", Type: "bun@1.2"},
			Dependencies: []workflow.Dependency{
				{Hostname: "bunstage", Type: "bun@1.2", Mode: "NON_HA", Resolution: "CREATE"},
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
			},
		},
	}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}
	if planResp.Progress.Completed != 1 {
		t.Errorf("after discover: completed want 1, got %d", planResp.Progress.Completed)
	}
	if planResp.Current == nil || planResp.Current.Name != "provision" {
		t.Errorf("after discover: current want provision, got %v", planResp.Current)
	}

	// Complete remaining steps.
	var lastResp *workflow.BootstrapResponse
	for i, step := range []string{"provision", "generate", "deploy", "close"} {
		resp, err := engine.BootstrapComplete(context.Background(), step, "Completed "+step, nil)
		if err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
		lastResp = resp

		// Verify progress.
		expectedCompleted := 2 + i // discover(1) + plan(1) + current step
		if resp.Progress.Completed != expectedCompleted {
			t.Errorf("step %s: completed want %d, got %d", step, expectedCompleted, resp.Progress.Completed)
		}

		// Verify next step or completion.
		if step != "close" {
			if resp.Current == nil {
				t.Fatalf("step %s: current should not be nil", step)
			}
		}
	}

	// After all steps: verify completion.
	if lastResp.Current != nil {
		t.Errorf("current should be nil after all steps complete, got: name=%q", lastResp.Current.Name)
	}
	if lastResp.Progress.Completed != 5 {
		t.Errorf("final completed: want 5, got %d", lastResp.Progress.Completed)
	}
}

func TestIntegration_BootstrapConductor_SkipFlow(t *testing.T) {
	t.Parallel()

	mock := defaultMock()
	session, cleanup := setupWorkflowServer(t, mock)
	defer cleanup()

	// Start bootstrap.
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "intent": "managed-only project",
	})

	// Complete discover with empty plan (managed-only project).
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []map[string]any{}, // Empty plan for managed-only.
	})

	// Complete provision.
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "provision",
		"attestation": "Completed provision for managed-only project",
	})

	// Skip generate (no runtime services).
	skipText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "skip", "step": "generate", "reason": "managed-only project, no code to generate",
	})
	var skipResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(skipText), &skipResp); err != nil {
		t.Fatalf("parse skip response: %v", err)
	}
	if skipResp.Current == nil || skipResp.Current.Name != "deploy" {
		t.Fatal("after skipping generate, expected deploy")
	}

	// Skip deploy (no runtime services).
	skipText = callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "skip", "step": "deploy", "reason": "managed-only project, no deploy needed",
	})
	if err := json.Unmarshal([]byte(skipText), &skipResp); err != nil {
		t.Fatalf("parse skip response: %v", err)
	}
	if skipResp.Current == nil || skipResp.Current.Name != "close" {
		t.Fatal("after skipping deploy, expected close")
	}

	// Skip close (managed-only, no runtime services).
	finalText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "skip", "step": "close", "reason": "managed-only project, no close needed",
	})

	var finalResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(finalText), &finalResp); err != nil {
		t.Fatalf("parse final response: %v", err)
	}

	// 2 completed + 3 skipped = 5 total.
	if finalResp.Progress.Completed != 5 {
		t.Errorf("completed: want 5, got %d", finalResp.Progress.Completed)
	}
	if finalResp.Current != nil {
		t.Error("current should be nil after completion")
	}

	// Skip-close path must surface the transition message — not the raw
	// "Bootstrap complete. All steps finished." from the engine. Regression
	// guard: prior to the appendTransitionMessage extraction, skip-close
	// returned a barebones message with no develop-workflow guidance, so
	// agents silently moved on to code changes without opening a session.
	if !strings.Contains(finalResp.Message, "develop") {
		t.Errorf("final message missing develop-workflow transition guidance: %q", finalResp.Message)
	}

	// Verify step statuses in summary.
	skippedCount := 0
	completedCount := 0
	for _, s := range finalResp.Progress.Steps {
		t.Logf("Step %s: %s", s.Name, s.Status)
		switch s.Status {
		case "skipped":
			skippedCount++
		case "complete":
			completedCount++
		}
	}
	if skippedCount != 3 {
		t.Errorf("skipped count: want 3, got %d", skippedCount)
	}
	if completedCount != 2 {
		t.Errorf("completed count: want 2, got %d", completedCount)
	}
}

func TestIntegration_BootstrapConductor_StatusRecovery(t *testing.T) {
	t.Parallel()

	// Use engine directly to test status recovery without MCP tool layer checkers.
	stateDir := t.TempDir()
	engine := workflow.NewEngine(stateDir, workflow.EnvLocal, nil)

	// Start and complete discover step.
	startResp, err := engine.BootstrapStart("proj-1", "recovery test")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	if startResp.SessionID == "" {
		t.Fatal("expected session ID from start")
	}

	// Complete discover with a plan.
	_, err = engine.BootstrapCompletePlan([]workflow.BootstrapTarget{
		{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			Dependencies: []workflow.Dependency{
				{Hostname: "appstage", Type: "nodejs@22", Mode: "NON_HA", Resolution: "CREATE"},
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
			},
		},
	}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	// Simulate context recovery: call status on the same engine to verify state persistence.
	statusResp, err := engine.BootstrapStatus()
	if err != nil {
		t.Fatalf("BootstrapStatus: %v", err)
	}

	if statusResp.Progress.Completed != 1 {
		t.Errorf("completed after recovery: want 1, got %d", statusResp.Progress.Completed)
	}
	if statusResp.Current == nil || statusResp.Current.Name != "provision" {
		t.Errorf("current after recovery: want provision, got %v", statusResp.Current)
	}
	if statusResp.SessionID == "" {
		t.Error("session ID should be preserved after recovery")
	}

	// Verify step statuses are preserved.
	if statusResp.Progress.Steps[0].Status != "complete" {
		t.Errorf("discover status: want complete, got %s", statusResp.Progress.Steps[0].Status)
	}
	if statusResp.Progress.Steps[1].Status != "in_progress" {
		t.Errorf("provision status: want in_progress, got %s", statusResp.Progress.Steps[1].Status)
	}

	// Continue from state: complete provision and check next step.
	provisionResp, err := engine.BootstrapComplete(context.Background(), "provision", "Services imported, dev mounted, env vars discovered successfully", nil)
	if err != nil {
		t.Fatalf("BootstrapComplete(provision): %v", err)
	}
	if provisionResp.Progress.Completed != 2 {
		t.Errorf("completed after provision: want 2, got %d", provisionResp.Progress.Completed)
	}
	if provisionResp.Current == nil || provisionResp.Current.Name != "generate" {
		t.Errorf("current after provision: want generate, got %v", provisionResp.Current)
	}
}

func TestIntegration_BootstrapConductor_ErrorCases(t *testing.T) {
	t.Parallel()

	mock := defaultMock()
	session, cleanup := setupWorkflowServer(t, mock)
	defer cleanup()

	// Error: complete without starting.
	result := callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover", "attestation": "test attestation here",
	})
	if !result.IsError {
		t.Error("expected error completing step without active bootstrap")
	}

	// Status without an active workflow returns the idle-phase orientation.
	text := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "status",
	})
	if !strings.Contains(text, "Phase: idle") {
		t.Errorf("expected idle-phase orientation, got: %s", text)
	}

	// Start bootstrap.
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "intent": "error test",
	})

	// Error: double start.
	result = callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap",
	})
	if !result.IsError {
		t.Error("expected error for double start")
	}

	// Error: complete wrong step.
	result = callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "provision", "attestation": "wrong step attestation here",
	})
	if !result.IsError {
		t.Error("expected error completing out-of-order step")
	}

	// Error: skip mandatory step (discover).
	result = callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "skip", "step": "discover", "reason": "want to skip",
	})
	if !result.IsError {
		t.Error("expected error skipping mandatory step 'discover'")
	}

	// Error: short attestation.
	result = callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover", "attestation": "short",
	})
	if !result.IsError {
		t.Error("expected error for short attestation")
	}

	// Error: missing step field.
	result = callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "attestation": "valid attestation text here",
	})
	if !result.IsError {
		t.Error("expected error for missing step field")
	}
	errText := getTextContent(t, result)
	if !strings.Contains(errText, "INVALID_PARAMETER") {
		t.Errorf("expected INVALID_PARAMETER, got: %s", errText)
	}
}

func TestIntegration_BootstrapConductor_ResetAndRestart(t *testing.T) {
	t.Parallel()

	mock := defaultMock()
	session, cleanup := setupWorkflowServer(t, mock)
	defer cleanup()

	// Start, complete a step, then reset.
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "intent": "will reset",
	})
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover", "attestation": "FRESH project detected successfully",
	})

	// Reset.
	resetResult := callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "reset",
	})
	if resetResult.IsError {
		t.Fatalf("reset error: %s", getTextContent(t, resetResult))
	}

	// Restart — should work after reset.
	restartText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "intent": "restarted",
	})
	var restartResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(restartText), &restartResp); err != nil {
		t.Fatalf("parse restart response: %v", err)
	}
	if restartResp.Intent != "restarted" {
		t.Errorf("intent: want 'restarted', got %q", restartResp.Intent)
	}
	if restartResp.Current == nil || restartResp.Current.Name != "discover" {
		t.Error("expected fresh start at discover step")
	}
	if restartResp.Progress.Completed != 0 {
		t.Errorf("completed: want 0 after restart, got %d", restartResp.Progress.Completed)
	}
}

func TestIntegration_BootstrapConductor_StepGuidanceQuality(t *testing.T) {
	t.Parallel()

	// Use engine directly to bypass MCP tool layer and step checkers.
	stateDir := t.TempDir()
	engine := workflow.NewEngine(stateDir, workflow.EnvLocal, nil)

	// Start bootstrap.
	startResp, err := engine.BootstrapStart("proj-1", "guidance quality check")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	expectedTools := map[string]string{
		"discover":  "zerops_discover",
		"provision": "zerops_import",
		"generate":  "zerops_knowledge",
		"deploy":    "zerops_deploy",
		"close":     "zerops_workflow",
	}

	steps := []string{"discover", "provision", "generate", "deploy", "close"}

	// Check first step.
	if startResp.Current == nil {
		t.Fatal("start response: current is nil")
	}
	if startResp.Current.Name != "discover" {
		t.Fatalf("first step: want discover, got %q", startResp.Current.Name)
	}

	// Submit plan for discover.
	planResp, err := engine.BootstrapCompletePlan([]workflow.BootstrapTarget{
		{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			Dependencies: []workflow.Dependency{
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
			},
		},
	}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	// Check each step's guidance quality as we complete them.
	stepsToCheck := steps[1:] // Skip discover, start with provision
	responses := make([]*workflow.BootstrapResponse, 0, 2+len(stepsToCheck))
	responses = append(responses, startResp, planResp)

	for i, step := range stepsToCheck {
		resp := responses[len(responses)-1]

		if resp.Current == nil {
			t.Fatalf("step %d (%s): current is nil", i+1, step)
		}
		if resp.Current.Name != step {
			t.Fatalf("step %d: want %q, got %q", i+1, step, resp.Current.Name)
		}

		// Check detailedGuide has real content (close step may have empty guidance).
		if step != "close" && len(resp.Current.DetailedGuide) < 30 {
			t.Errorf("step %q: detailedGuide too short (%d chars)", step, len(resp.Current.DetailedGuide))
		}

		// Check tools include expected tool.
		expectedTool := expectedTools[step]
		if !slices.Contains(resp.Current.Tools, expectedTool) {
			t.Errorf("step %q: expected tool %q in %v", step, expectedTool, resp.Current.Tools)
		}

		// Check verification string exists.
		if resp.Current.Verification == "" {
			t.Errorf("step %q: empty verification", step)
		}

		// Complete step.
		completedResp, err := engine.BootstrapComplete(context.Background(), step, "Quality check attestation for "+step, nil)
		if err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
		responses = append(responses, completedResp)
	}

	// Verify all steps are completed.
	final := responses[len(responses)-1]
	if final.Current != nil {
		t.Errorf("after all steps: current should be nil, got %s", final.Current.Name)
	}
}

// TestIntegration_BootstrapConductor_DevMode_G4Skipped verifies that dev mode plans
// complete auto-transition without stage_verify evidence (G4 is skipped).
// Uses engine directly to set the plan (integration tests through MCP tool have
// step checkers that validate service existence against the mock).
func TestIntegration_BootstrapConductor_DevMode_G4Skipped(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	engine := workflow.NewEngine(stateDir, workflow.EnvLocal, nil)

	_, err := engine.BootstrapStart("proj-1", "dev mode test")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Submit dev mode plan directly.
	_, err = engine.BootstrapCompletePlan([]workflow.BootstrapTarget{{
		Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", BootstrapMode: "dev"},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	// Complete remaining steps.
	var lastDevResp *workflow.BootstrapResponse
	for _, step := range []string{"provision", "generate", "deploy", "close"} {
		resp, err := engine.BootstrapComplete(context.Background(), step, "Completed "+step+" for dev mode", nil)
		if err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
		lastDevResp = resp
	}

	// Verify bootstrap completed via last response (session cleaned up on completion).
	if lastDevResp == nil || lastDevResp.Current != nil {
		t.Error("Bootstrap should be completed (no current step in final response)")
	}
}

func TestIntegration_BootstrapConductor_SimpleMode_Completes(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	engine := workflow.NewEngine(stateDir, workflow.EnvLocal, nil)

	_, err := engine.BootstrapStart("proj-1", "simple mode test")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	_, err = engine.BootstrapCompletePlan([]workflow.BootstrapTarget{{
		Runtime: workflow.RuntimeTarget{DevHostname: "myapp", Type: "bun@1.2", BootstrapMode: "simple"},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	var lastSimpleResp *workflow.BootstrapResponse
	for _, step := range []string{"provision", "generate", "deploy", "close"} {
		resp, err := engine.BootstrapComplete(context.Background(), step, "Completed "+step+" for simple mode", nil)
		if err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
		lastSimpleResp = resp
	}

	// Verify bootstrap completed via last response (session cleaned up on completion).
	if lastSimpleResp == nil || lastSimpleResp.Current != nil {
		t.Error("Bootstrap should be completed (no current step in final response)")
	}
}

func TestIntegration_BootstrapConductor_MixedModes_StandardRequired(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	engine := workflow.NewEngine(stateDir, workflow.EnvLocal, nil)

	_, err := engine.BootstrapStart("proj-1", "mixed mode test")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Mixed: standard + simple. PlanMode should be "standard".
	resp, err := engine.BootstrapCompletePlan([]workflow.BootstrapTarget{
		{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
		{Runtime: workflow.RuntimeTarget{DevHostname: "frontend", Type: "bun@1.2", BootstrapMode: "simple"}},
	}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}
	if resp.Current.PlanMode != "standard" {
		t.Errorf("PlanMode: want 'standard', got %q", resp.Current.PlanMode)
	}

	// Complete all steps.
	var lastMixedResp *workflow.BootstrapResponse
	for _, step := range []string{"provision", "generate", "deploy", "close"} {
		resp, err := engine.BootstrapComplete(context.Background(), step, "Completed "+step+" for mixed mode", nil)
		if err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
		lastMixedResp = resp
	}

	// Verify bootstrap completed via last response (session cleaned up on completion).
	if lastMixedResp == nil || lastMixedResp.Current != nil {
		t.Error("Bootstrap should be completed (no current step in final response)")
	}
}
