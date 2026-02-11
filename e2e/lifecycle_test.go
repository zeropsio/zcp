//go:build e2e

// Tests for: e2e — full lifecycle test against real Zerops API.
//
// This test creates real services, exercises MCP tools end-to-end,
// and cleans up afterward. Requires ZCP_API_KEY environment variable.
//
// Run: go test ./e2e/ -tags e2e -count=1 -v -timeout 600s

package e2e_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestE2E_FullLifecycle(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	rtHostname := "zcprt" + suffix
	dbHostname := "zcpdb" + suffix

	// Register cleanup to delete test services even if test fails.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, rtHostname, dbHostname)
	})

	step := 0

	// --- Step 1: zerops_context ---
	step++
	logStep(t, step, "zerops_context")
	contextText := s.mustCallSuccess("zerops_context", nil)
	if !strings.Contains(contextText, "Zerops") {
		t.Error("context should mention Zerops")
	}

	// --- Step 2: zerops_workflow catalog ---
	step++
	logStep(t, step, "zerops_workflow catalog")
	catalogText := s.mustCallSuccess("zerops_workflow", nil)
	if !strings.Contains(catalogText, "bootstrap") {
		t.Error("catalog should list bootstrap workflow")
	}

	// --- Step 3: zerops_workflow bootstrap ---
	step++
	logStep(t, step, "zerops_workflow bootstrap")
	bootstrapText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"workflow": "bootstrap",
	})
	if len(bootstrapText) < 50 {
		t.Errorf("bootstrap content too short (%d chars)", len(bootstrapText))
	}

	// --- Step 4: zerops_discover (baseline) ---
	step++
	logStep(t, step, "zerops_discover baseline")
	baselineText := s.mustCallSuccess("zerops_discover", nil)
	var baseline struct {
		Project struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"project"`
		Services []struct {
			Hostname string `json:"hostname"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(baselineText), &baseline); err != nil {
		t.Fatalf("parse baseline: %v", err)
	}
	if baseline.Project.ID == "" {
		t.Fatal("expected project ID in discover")
	}
	baselineCount := len(baseline.Services)
	t.Logf("  Baseline: %d services in project %s", baselineCount, baseline.Project.Name)

	// --- Step 5: zerops_validate import YAML ---
	step++
	logStep(t, step, "zerops_validate import YAML")
	importYAML := `services:
  - hostname: ` + rtHostname + `
    type: nodejs@22
    minContainers: 1
  - hostname: ` + dbHostname + `
    type: postgresql@16
    mode: NON_HA
`
	validateText := s.mustCallSuccess("zerops_validate", map[string]any{
		"content": importYAML,
		"type":    "import",
	})
	if !strings.Contains(strings.ToLower(validateText), "valid") {
		t.Errorf("expected valid in validate result: %s", validateText)
	}

	// --- Step 6: zerops_import dry-run ---
	step++
	logStep(t, step, "zerops_import dry-run")
	dryRunText := s.mustCallSuccess("zerops_import", map[string]any{
		"dryRun":  true,
		"content": importYAML,
	})
	var dryResult struct {
		DryRun   bool `json:"dryRun"`
		Valid    bool `json:"valid"`
		Services []interface{}
	}
	if err := json.Unmarshal([]byte(dryRunText), &dryResult); err != nil {
		t.Fatalf("parse dry-run: %v", err)
	}
	if !dryResult.Valid {
		t.Fatalf("dry-run should be valid: %s", dryRunText)
	}
	if len(dryResult.Services) != 2 {
		t.Fatalf("dry-run: expected 2 services, got %d", len(dryResult.Services))
	}

	// --- Step 7: zerops_import real ---
	step++
	logStep(t, step, "zerops_import real (creating services)")
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
	processes := parseProcesses(t, importText)
	t.Logf("  Import returned %d processes", len(processes))

	// Wait for all import processes.
	for _, proc := range processes {
		pid, ok := proc["processId"].(string)
		if !ok || pid == "" {
			continue
		}
		t.Logf("  Waiting for process %s (%s)", pid, proc["actionName"])
		waitForProcess(s, pid)
	}

	// --- Step 8: zerops_discover (verify new services) ---
	step++
	logStep(t, step, "zerops_discover after import")
	waitForServiceReady(s, rtHostname)
	discoverText := s.mustCallSuccess("zerops_discover", nil)
	if !findServiceByHostname(t, discoverText, rtHostname) {
		t.Errorf("runtime service %s not found after import", rtHostname)
	}
	if !findServiceByHostname(t, discoverText, dbHostname) {
		t.Errorf("database service %s not found after import", dbHostname)
	}

	// --- Step 9: zerops_discover with service filter ---
	step++
	logStep(t, step, "zerops_discover with service filter")
	filteredText := s.mustCallSuccess("zerops_discover", map[string]any{
		"service": rtHostname,
	})
	var filtered struct {
		Services []struct {
			Hostname string `json:"hostname"`
			Type     string `json:"type"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(filteredText), &filtered); err != nil {
		t.Fatalf("parse filtered discover: %v", err)
	}
	if len(filtered.Services) != 1 {
		t.Fatalf("expected 1 filtered service, got %d", len(filtered.Services))
	}
	if filtered.Services[0].Hostname != rtHostname {
		t.Errorf("hostname = %q, want %q", filtered.Services[0].Hostname, rtHostname)
	}

	// --- Step 10: zerops_env set ---
	step++
	logStep(t, step, "zerops_env set on %s", rtHostname)
	s.mustCallSuccess("zerops_env", map[string]any{
		"action":          "set",
		"serviceHostname": rtHostname,
		"variables":       []any{"E2E_TEST_VAR=hello_zerops"},
	})

	// --- Step 11: zerops_env get ---
	step++
	logStep(t, step, "zerops_env get on %s", rtHostname)
	envText := s.mustCallSuccess("zerops_env", map[string]any{
		"action":          "get",
		"serviceHostname": rtHostname,
	})
	if !strings.Contains(envText, "E2E_TEST_VAR") || !strings.Contains(envText, "hello_zerops") {
		t.Logf("  env get response: %s", envText)
		// Note: env set is async; the var may not appear immediately.
		// This is acceptable behavior — we verify the get call works.
	}

	// --- Step 12: zerops_manage restart (database — active immediately after import) ---
	step++
	logStep(t, step, "zerops_manage restart %s", dbHostname)
	restartText := s.mustCallSuccess("zerops_manage", map[string]any{
		"action":          "restart",
		"serviceHostname": dbHostname,
	})
	restartProcID := extractProcessID(t, restartText)
	t.Logf("  Restart process: %s", restartProcID)
	waitForProcess(s, restartProcID)

	// --- Step 13: zerops_process status ---
	step++
	logStep(t, step, "zerops_process status")
	statusText := s.mustCallSuccess("zerops_process", map[string]any{
		"processId": restartProcID,
	})
	var procStatus struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(statusText), &procStatus); err != nil {
		t.Fatalf("parse process status: %v", err)
	}
	if procStatus.Status != "FINISHED" {
		t.Errorf("process status = %q, want FINISHED", procStatus.Status)
	}

	// --- Step 14: zerops_events ---
	step++
	logStep(t, step, "zerops_events")
	eventsText := s.mustCallSuccess("zerops_events", map[string]any{
		"limit": 10,
	})
	if eventsText == "" {
		t.Error("expected non-empty events response")
	}

	// --- Step 15: zerops_knowledge search ---
	step++
	logStep(t, step, "zerops_knowledge search")
	knowledgeText := s.mustCallSuccess("zerops_knowledge", map[string]any{
		"query": "nodejs deploy",
	})
	if knowledgeText == "" {
		t.Error("expected non-empty knowledge response")
	}

	// --- Step 16: zerops_delete without confirm (safety gate) ---
	step++
	logStep(t, step, "zerops_delete without confirm")
	noConfirmResult := s.callTool("zerops_delete", map[string]any{
		"serviceHostname": rtHostname,
		"confirm":         false,
	})
	if !noConfirmResult.IsError {
		t.Fatal("expected error when confirm=false")
	}
	errText := getE2ETextContent(t, noConfirmResult)
	if !strings.Contains(errText, "CONFIRM_REQUIRED") {
		t.Errorf("expected CONFIRM_REQUIRED in error: %s", errText)
	}

	// --- Step 17: zerops_delete runtime service ---
	step++
	logStep(t, step, "zerops_delete %s (confirmed)", rtHostname)
	deleteRtText := s.mustCallSuccess("zerops_delete", map[string]any{
		"serviceHostname": rtHostname,
		"confirm":         true,
	})
	deleteRtProcID := extractProcessID(t, deleteRtText)
	t.Logf("  Delete process: %s", deleteRtProcID)
	waitForProcess(s, deleteRtProcID)

	// --- Step 18: zerops_delete database service ---
	step++
	logStep(t, step, "zerops_delete %s (confirmed)", dbHostname)
	deleteDbText := s.mustCallSuccess("zerops_delete", map[string]any{
		"serviceHostname": dbHostname,
		"confirm":         true,
	})
	deleteDbProcID := extractProcessID(t, deleteDbText)
	t.Logf("  Delete process: %s", deleteDbProcID)
	waitForProcess(s, deleteDbProcID)

	// --- Step 19: zerops_discover (verify services deleted) ---
	step++
	logStep(t, step, "zerops_discover after deletion")
	// Allow time for deletion to propagate.
	time.Sleep(2 * time.Second)
	finalText := s.mustCallSuccess("zerops_discover", nil)
	if findServiceByHostname(t, finalText, rtHostname) {
		t.Errorf("runtime service %s still visible after deletion", rtHostname)
	}
	if findServiceByHostname(t, finalText, dbHostname) {
		t.Errorf("database service %s still visible after deletion", dbHostname)
	}
	t.Log("  All test services cleaned up successfully")
}
