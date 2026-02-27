// Tests for: integration — bootstrap conductor full E2E flow through MCP server.
//
// Exercises the complete bootstrap conductor lifecycle:
// start → complete all 11 steps → auto-evidence → auto-transition → DONE
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
	engine := workflow.NewEngine(stateDir)

	tools.RegisterWorkflow(mcpSrv, mock, "proj-1", nil, engine, nil)
	tools.RegisterDiscover(mcpSrv, mock, "proj-1")
	tools.RegisterKnowledge(mcpSrv, store, mock, nil, nil)

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
		"mode":     "full",
		"intent":   "bun + postgres app",
	})

	var startResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(startText), &startResp); err != nil {
		t.Fatalf("parse start response: %v", err)
	}
	if startResp.Progress.Total != 11 {
		t.Fatalf("total steps: want 11, got %d", startResp.Progress.Total)
	}
	if startResp.Current == nil || startResp.Current.Name != "detect" {
		t.Fatal("expected first step to be 'detect'")
	}
	if startResp.SessionID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if startResp.Intent != "bun + postgres app" {
		t.Errorf("intent: want 'bun + postgres app', got %q", startResp.Intent)
	}

	// Verify guidance is rich (not empty placeholder).
	if len(startResp.Current.Guidance) < 50 {
		t.Errorf("guidance too short (%d chars), expected rich instructions", len(startResp.Current.Guidance))
	}
	if len(startResp.Current.Tools) == 0 {
		t.Error("expected non-empty tools list for detect step")
	}

	// Step 2: Complete all 11 steps sequentially.
	steps := []struct {
		name        string
		attestation string
	}{
		{"detect", "FRESH project detected — no runtime services, 2 managed services found (db, cache)"},
		{"plan", "Planned: bundev/bunstage (bun@1.2), db (postgresql@16). All hostnames validated [a-z0-9]."},
		{"load-knowledge", "Loaded bun runtime briefing + infrastructure rules. Recipe: hono-bun loaded."},
		{"generate-import", "import.yml generated with 4 services: bundev, bunstage, db. Validated hostnames and types."},
		{"import-services", "All services imported. bundev=RUNNING, bunstage=NEW, db=RUNNING. Process ID proc-123 FINISHED."},
		{"mount-dev", "Mounted bundev at /var/www/bundev/. Stage and managed services skipped as expected."},
		{"discover-envs", "Discovered db envs: connectionString, host, port, user, password, dbName. Recorded all 6 vars."},
		{"generate-code", "zerops.yml and app code written to /var/www/bundev/ with correct env var mappings and /status endpoint."},
		{"deploy", "Deployed bundev: /status returns 200 with SELECT 1 proof. Deployed bunstage: /status 200. Both subdomains enabled."},
		{"verify", "Independent verification: bundev RUNNING + HTTP 200, bunstage RUNNING + HTTP 200, db RUNNING. 3/3 healthy."},
		{"report", "All services operational. Dev: https://bundev-proj1.zerops.app, Stage: https://bunstage-proj1.zerops.app"},
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
			// Each step should have guidance and tools.
			if lastResp.Current.Guidance == "" {
				t.Errorf("step %d: next step %q has empty guidance", i, lastResp.Current.Name)
			}
		}
	}

	// After all 11 steps: bootstrap complete.
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
	if finalResp.Progress.Completed != 11 {
		t.Errorf("final completed: want 11, got %d", finalResp.Progress.Completed)
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
		"action": "start", "workflow": "bootstrap", "mode": "dev_only", "intent": "managed-only project",
	})

	// Complete mandatory steps up to import-services.
	mandatorySteps := []string{"detect", "plan", "load-knowledge", "generate-import", "import-services"}
	for _, step := range mandatorySteps {
		callAndGetText(t, session, "zerops_workflow", map[string]any{
			"action": "complete", "step": step,
			"attestation": "Completed " + step + " for managed-only project",
		})
	}

	// Skip mount-dev (no runtime services).
	skipText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "skip", "step": "mount-dev", "reason": "managed-only project, no runtime services to mount",
	})
	var skipResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(skipText), &skipResp); err != nil {
		t.Fatalf("parse skip response: %v", err)
	}
	if skipResp.Current == nil || skipResp.Current.Name != "discover-envs" {
		t.Fatal("after skipping mount-dev, expected discover-envs")
	}

	// Skip discover-envs (no managed services needing env discovery).
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "skip", "step": "discover-envs", "reason": "env vars already known from import",
	})

	// Skip generate-code (no runtime services).
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "skip", "step": "generate-code", "reason": "managed-only project, no code to generate",
	})

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

	// Complete verify and report.
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "verify",
		"attestation": "All managed services verified RUNNING: db, cache",
	})
	finalText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "report",
		"attestation": "Report presented to user: db and cache running, no runtime services",
	})

	var finalResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(finalText), &finalResp); err != nil {
		t.Fatalf("parse final response: %v", err)
	}

	// 7 completed + 4 skipped = 11 total.
	if finalResp.Progress.Completed != 11 {
		t.Errorf("completed: want 11, got %d", finalResp.Progress.Completed)
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
	if skippedCount != 4 {
		t.Errorf("skipped count: want 4, got %d", skippedCount)
	}
	if completedCount != 7 {
		t.Errorf("completed count: want 7, got %d", completedCount)
	}
}

func TestIntegration_BootstrapConductor_StatusRecovery(t *testing.T) {
	t.Parallel()

	mock := defaultMock()
	session, cleanup := setupWorkflowServer(t, mock)
	defer cleanup()

	// Start bootstrap and complete a few steps.
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "mode": "full", "intent": "recovery test",
	})
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "detect",
		"attestation": "FRESH project — no existing services detected",
	})
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "plan",
		"attestation": "Plan: appdev, appstage, db — all hostnames validated",
	})

	// Simulate context compaction: call status to get all attestations.
	statusText := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "status",
	})

	var statusResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(statusText), &statusResp); err != nil {
		t.Fatalf("parse status response: %v", err)
	}

	if statusResp.Progress.Completed != 2 {
		t.Errorf("completed: want 2, got %d", statusResp.Progress.Completed)
	}
	if statusResp.Current == nil || statusResp.Current.Name != "load-knowledge" {
		t.Error("expected current step to be 'load-knowledge'")
	}
	if statusResp.SessionID == "" {
		t.Error("expected session ID in status response")
	}

	// Verify step statuses are preserved.
	if statusResp.Progress.Steps[0].Status != "complete" {
		t.Errorf("detect status: want complete, got %s", statusResp.Progress.Steps[0].Status)
	}
	if statusResp.Progress.Steps[1].Status != "complete" {
		t.Errorf("plan status: want complete, got %s", statusResp.Progress.Steps[1].Status)
	}
	if statusResp.Progress.Steps[2].Status != "in_progress" {
		t.Errorf("load-knowledge status: want in_progress, got %s", statusResp.Progress.Steps[2].Status)
	}

	// Verify we can continue from where we left off.
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "load-knowledge",
		"attestation": "Loaded runtime briefing and infrastructure rules successfully",
	})

	status2Text := callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "status",
	})
	var status2Resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(status2Text), &status2Resp); err != nil {
		t.Fatalf("parse status2 response: %v", err)
	}
	if status2Resp.Progress.Completed != 3 {
		t.Errorf("completed after resume: want 3, got %d", status2Resp.Progress.Completed)
	}
	if status2Resp.Current == nil || status2Resp.Current.Name != "generate-import" {
		t.Error("expected current step to be 'generate-import' after resume")
	}
}

func TestIntegration_BootstrapConductor_ErrorCases(t *testing.T) {
	t.Parallel()

	mock := defaultMock()
	session, cleanup := setupWorkflowServer(t, mock)
	defer cleanup()

	// Error: complete without starting.
	result := callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "detect", "attestation": "test attestation here",
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
		"action": "start", "workflow": "bootstrap", "mode": "full", "intent": "error test",
	})

	// Error: double start.
	result = callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "mode": "full",
	})
	if !result.IsError {
		t.Error("expected error for double start")
	}

	// Error: complete wrong step.
	result = callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "plan", "attestation": "wrong step attestation here",
	})
	if !result.IsError {
		t.Error("expected error completing out-of-order step")
	}

	// Error: skip mandatory step (detect).
	result = callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "skip", "step": "detect", "reason": "want to skip",
	})
	if !result.IsError {
		t.Error("expected error skipping mandatory step 'detect'")
	}

	// Error: short attestation.
	result = callAndGetResult(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "detect", "attestation": "short",
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
		"action": "start", "workflow": "bootstrap", "mode": "full", "intent": "will reset",
	})
	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "detect", "attestation": "FRESH project detected successfully",
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
		"action": "start", "workflow": "bootstrap", "mode": "dev_only", "intent": "restarted",
	})
	var restartResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(restartText), &restartResp); err != nil {
		t.Fatalf("parse restart response: %v", err)
	}
	if restartResp.Mode != "dev_only" {
		t.Errorf("mode: want dev_only, got %s", restartResp.Mode)
	}
	if restartResp.Intent != "restarted" {
		t.Errorf("intent: want 'restarted', got %q", restartResp.Intent)
	}
	if restartResp.Current == nil || restartResp.Current.Name != "detect" {
		t.Error("expected fresh start at detect step")
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
		"action": "start", "workflow": "bootstrap", "mode": "full", "intent": "guidance quality check",
	})

	expectedTools := map[string]string{
		"detect":          "zerops_discover",
		"plan":            "zerops_knowledge",
		"load-knowledge":  "zerops_knowledge",
		"generate-import": "zerops_knowledge",
		"import-services": "zerops_import",
		"mount-dev":       "zerops_mount",
		"discover-envs":   "zerops_discover",
		"generate-code":   "zerops_knowledge",
		"deploy":          "zerops_deploy",
		"verify":          "zerops_discover",
		"report":          "zerops_discover",
	}

	steps := []string{
		"detect", "plan", "load-knowledge", "generate-import",
		"import-services", "mount-dev", "discover-envs", "generate-code",
		"deploy", "verify", "report",
	}

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

		// Check guidance has real content (not placeholder).
		if len(resp.Current.Guidance) < 30 {
			t.Errorf("step %q: guidance too short (%d chars)", step, len(resp.Current.Guidance))
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
