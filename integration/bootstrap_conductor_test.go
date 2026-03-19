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

	tools.RegisterWorkflow(mcpSrv, mock, "proj-1", nil, engine, nil, "")
	tools.RegisterDiscover(mcpSrv, mock, "proj-1")
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

	mock := defaultMock()
	session, cleanup := setupWorkflowServer(t, mock)
	defer cleanup()

	// Step 1: Start bootstrap conductor.
	startText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "bun + postgres app",
	})

	var startResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(startText), &startResp); err != nil {
		t.Fatalf("parse start response: %v", err)
	}
	if startResp.Progress.Total != 6 {
		t.Fatalf("total steps: want 6, got %d", startResp.Progress.Total)
	}
	if startResp.Current == nil || startResp.Current.Name != "discover" {
		t.Fatal("expected first step to be 'discover'")
	}
	if startResp.SessionID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if startResp.Intent != "bun + postgres app" {
		t.Errorf("intent: want 'bun + postgres app', got %q", startResp.Intent)
	}

	// Verify detailedGuide is rich (Guidance is excluded from JSON via json:"-").
	if len(startResp.Current.DetailedGuide) < 50 {
		t.Errorf("detailedGuide too short (%d chars), expected rich instructions", len(startResp.Current.DetailedGuide))
	}
	if len(startResp.Current.Tools) == 0 {
		t.Error("expected non-empty tools list for discover step")
	}

	// Step 2: Complete all 6 steps sequentially.
	steps := []struct {
		name        string
		attestation string
	}{
		{"discover", "FRESH project detected. Planned: bundev/bunstage (bun@1.2), db (postgresql@16). All hostnames validated."},
		{"provision", "import.yml generated, services imported. bundev=RUNNING, bunstage=NEW, db=RUNNING. Dev mounted, envs discovered."},
		{"generate", "zerops.yml and app code written to /var/www/bundev/ with correct env var mappings and /status endpoint."},
		{"deploy", "Deployed bundev: /status returns 200 with SELECT 1 proof. Deployed bunstage: /status 200. Both subdomains enabled."},
		{"verify", "Independent verification: bundev RUNNING + HTTP 200, bunstage RUNNING + HTTP 200, db RUNNING. 3/3 healthy. Report presented."},
		{"strategy", "User chose push-dev strategy for bundev. Strategy recorded via zerops_workflow action=complete step=strategy."},
	}

	var lastResp workflow.BootstrapResponse
	for i, step := range steps {
		text := callAndGetText(t, session, "zerops_workflow", map[string]any{
			"action":      "complete",
			"step":        step.name,
			"attestation": step.attestation,
		})

		if err := json.Unmarshal([]byte(text), &lastResp); err != nil {
			t.Fatalf("step %d (%s) parse: %v", i, step.name, err)
		}

		if lastResp.Progress.Completed != i+1 {
			t.Errorf("step %d (%s): completed want %d, got %d", i, step.name, i+1, lastResp.Progress.Completed)
		}

		if i < len(steps)-1 {
			// Not last step — current should be next step.
			if lastResp.Current == nil {
				t.Fatalf("step %d (%s): current should not be nil", i, step.name)
			}
			if lastResp.Current.Name != steps[i+1].name {
				t.Errorf("step %d (%s): next step want %q, got %q", i, step.name, steps[i+1].name, lastResp.Current.Name)
			}
			// Each step should have detailedGuide and tools (Guidance excluded from JSON).
			if lastResp.Current.DetailedGuide == "" {
				t.Errorf("step %d: next step %q has empty detailedGuide", i, lastResp.Current.Name)
			}
		}
	}

	// After all 6 steps: bootstrap complete.
	// Use a fresh variable — json.Unmarshal doesn't zero omitted pointer fields.
	var finalResp workflow.BootstrapResponse
	finalRaw := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "status",
	})
	if err := json.Unmarshal([]byte(finalRaw), &finalResp); err != nil {
		t.Fatalf("parse final status: %v", err)
	}
	if finalResp.Current != nil {
		t.Errorf("current should be nil after all steps complete, got: name=%q", finalResp.Current.Name)
	}
	if finalResp.Progress.Completed != 6 {
		t.Errorf("final completed: want 6, got %d", finalResp.Progress.Completed)
	}
	if !strings.Contains(strings.ToLower(finalResp.Message), "complete") {
		t.Errorf("final message should contain 'complete', got: %q", finalResp.Message)
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

	// Complete mandatory steps: discover and provision.
	for _, step := range []string{"discover", "provision"} {
		callAndGetText(t, session, "zerops_workflow", map[string]any{
			"action": "complete", "step": step,
			"attestation": "Completed " + step + " for managed-only project",
		})
	}

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
	if skipResp.Current == nil || skipResp.Current.Name != "verify" {
		t.Fatal("after skipping deploy, expected verify")
	}

	// Complete verify.
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "verify",
		"attestation": "All managed services verified RUNNING: db, cache. Report presented to user.",
	})

	// Skip strategy (managed-only, no runtime services).
	finalText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "skip", "step": "strategy", "reason": "managed-only project, no strategy needed",
	})

	var finalResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(finalText), &finalResp); err != nil {
		t.Fatalf("parse final response: %v", err)
	}

	// 3 completed + 3 skipped = 6 total.
	if finalResp.Progress.Completed != 6 {
		t.Errorf("completed: want 6, got %d", finalResp.Progress.Completed)
	}
	if finalResp.Current != nil {
		t.Error("current should be nil after completion")
	}

	// Verify step statuses in summary.
	skippedCount := 0
	completedCount := 0
	for _, s := range finalResp.Progress.Steps {
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
	if completedCount != 3 {
		t.Errorf("completed count: want 3, got %d", completedCount)
	}
}

func TestIntegration_BootstrapConductor_StatusRecovery(t *testing.T) {
	t.Parallel()

	mock := defaultMock()
	session, cleanup := setupWorkflowServer(t, mock)
	defer cleanup()

	// Start bootstrap and complete a step.
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "intent": "recovery test",
	})
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"attestation": "FRESH project — no existing services. Plan: appdev, appstage, db validated.",
	})

	// Simulate context compaction: call status to get all attestations.
	statusText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "status",
	})

	var statusResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(statusText), &statusResp); err != nil {
		t.Fatalf("parse status response: %v", err)
	}

	if statusResp.Progress.Completed != 1 {
		t.Errorf("completed: want 1, got %d", statusResp.Progress.Completed)
	}
	if statusResp.Current == nil || statusResp.Current.Name != "provision" {
		t.Error("expected current step to be 'provision'")
	}
	if statusResp.SessionID == "" {
		t.Error("expected session ID in status response")
	}

	// Verify step statuses are preserved.
	if statusResp.Progress.Steps[0].Status != "complete" {
		t.Errorf("discover status: want complete, got %s", statusResp.Progress.Steps[0].Status)
	}
	if statusResp.Progress.Steps[1].Status != "in_progress" {
		t.Errorf("provision status: want in_progress, got %s", statusResp.Progress.Steps[1].Status)
	}

	// Verify we can continue from where we left off.
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "provision",
		"attestation": "Services imported, dev mounted, env vars discovered successfully",
	})

	status2Text := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "status",
	})
	var status2Resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(status2Text), &status2Resp); err != nil {
		t.Fatalf("parse status2 response: %v", err)
	}
	if status2Resp.Progress.Completed != 2 {
		t.Errorf("completed after resume: want 2, got %d", status2Resp.Progress.Completed)
	}
	if status2Resp.Current == nil || status2Resp.Current.Name != "generate" {
		t.Error("expected current step to be 'generate' after resume")
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

	// Error: status without starting.
	result = callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "status",
	})
	if !result.IsError {
		t.Error("expected error for status without active session")
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

	mock := defaultMock()
	session, cleanup := setupWorkflowServer(t, mock)
	defer cleanup()

	// Start and check each step's guidance quality as we advance.
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "intent": "guidance quality check",
	})

	expectedTools := map[string]string{
		"discover":  "zerops_discover",
		"provision": "zerops_import",
		"generate":  "zerops_knowledge",
		"deploy":    "zerops_deploy",
		"verify":    "zerops_discover",
		"strategy":  "zerops_workflow",
	}

	steps := []string{"discover", "provision", "generate", "deploy", "verify", "strategy"}

	// Check first step guidance from start response.
	statusText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "status",
	})
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(statusText), &resp); err != nil {
		t.Fatalf("parse status: %v", err)
	}

	for i, step := range steps {
		if resp.Current == nil {
			t.Fatalf("step %d (%s): current is nil", i, step)
		}
		if resp.Current.Name != step {
			t.Fatalf("step %d: want %q, got %q", i, step, resp.Current.Name)
		}

		// Check detailedGuide has real content (Guidance is excluded from JSON via json:"-").
		if len(resp.Current.DetailedGuide) < 30 {
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

		// Complete and advance.
		completeText := callAndGetText(t, session, "zerops_workflow", map[string]any{
			"action":      "complete",
			"step":        step,
			"attestation": "Quality check attestation for step " + step,
		})
		if err := json.Unmarshal([]byte(completeText), &resp); err != nil {
			t.Fatalf("step %d (%s) complete parse: %v", i, step, err)
		}
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
	for _, step := range []string{"provision", "generate", "deploy", "verify", "strategy"} {
		if _, err := engine.BootstrapComplete(context.Background(), step, "Completed "+step+" for dev mode", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// Verify bootstrap completed (Active=false).
	state, err := engine.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap == nil || state.Bootstrap.Active {
		t.Error("Bootstrap should be completed (Active=false)")
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

	for _, step := range []string{"provision", "generate", "deploy", "verify", "strategy"} {
		if _, err := engine.BootstrapComplete(context.Background(), step, "Completed "+step+" for simple mode", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	state, err := engine.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap == nil || state.Bootstrap.Active {
		t.Error("Bootstrap should be completed (Active=false)")
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
	for _, step := range []string{"provision", "generate", "deploy", "verify", "strategy"} {
		if _, err := engine.BootstrapComplete(context.Background(), step, "Completed "+step+" for mixed mode", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	state, err := engine.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap == nil || state.Bootstrap.Active {
		t.Error("Bootstrap should be completed (Active=false)")
	}
}
