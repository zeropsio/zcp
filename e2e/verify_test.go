//go:build e2e

// Tests for: zerops_verify — E2E health verification against real Zerops API.
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_Verify -v -timeout 300s

package e2e_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
)

func TestE2E_Verify(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	rtHostname := "zcpvrt" + suffix
	dbHostname := "zcpvdb" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, rtHostname, dbHostname)
	})

	// --- Step 1: Import services ---
	logStep(t, 1, "zerops_import (runtime + managed)")
	importYAML := `services:
  - hostname: ` + rtHostname + `
    type: nodejs@22
    minContainers: 1
  - hostname: ` + dbHostname + `
    type: postgresql@16
    mode: NON_HA
`
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
	var importResult struct {
		Processes []struct {
			ProcessID string `json:"processId"`
			Status    string `json:"status"`
		} `json:"processes"`
	}
	if err := json.Unmarshal([]byte(importText), &importResult); err != nil {
		t.Fatalf("parse import result: %v", err)
	}
	for _, proc := range importResult.Processes {
		if proc.Status != "FINISHED" {
			t.Errorf("import process %s status = %s, want FINISHED", proc.ProcessID, proc.Status)
		}
	}

	// --- Step 2: Wait for services to be ready ---
	logStep(t, 2, "waiting for services ready")
	waitForServiceReady(s, rtHostname)
	waitForServiceReady(s, dbHostname)

	// --- Step 3: Verify managed service (postgresql) ---
	logStep(t, 3, "zerops_verify on managed service %s", dbHostname)
	dbResult := s.callTool("zerops_verify", map[string]any{"serviceHostname": dbHostname})
	if dbResult.IsError {
		t.Fatalf("zerops_verify on %s returned error: %s", dbHostname, getE2ETextContent(t, dbResult))
	}
	var dbVerify ops.VerifyResult
	if err := json.Unmarshal([]byte(getE2ETextContent(t, dbResult)), &dbVerify); err != nil {
		t.Fatalf("parse verify result: %v", err)
	}
	if dbVerify.Type != "managed" {
		t.Errorf("db Type = %q, want managed", dbVerify.Type)
	}
	if dbVerify.Status != "healthy" {
		t.Errorf("db Status = %q, want healthy", dbVerify.Status)
	}
	if len(dbVerify.Checks) != 1 {
		t.Errorf("db Checks count = %d, want 1", len(dbVerify.Checks))
	}
	if len(dbVerify.Checks) > 0 && dbVerify.Checks[0].Name != "service_running" {
		t.Errorf("db check name = %q, want service_running", dbVerify.Checks[0].Name)
	}
	if len(dbVerify.Checks) > 0 && dbVerify.Checks[0].Status != "pass" {
		t.Errorf("db service_running = %q, want pass", dbVerify.Checks[0].Status)
	}
	t.Logf("  Managed service: type=%s status=%s checks=%d", dbVerify.Type, dbVerify.Status, len(dbVerify.Checks))

	// --- Step 4: Verify runtime service (nodejs) ---
	logStep(t, 4, "zerops_verify on runtime service %s", rtHostname)
	rtResult := s.callTool("zerops_verify", map[string]any{"serviceHostname": rtHostname})
	if rtResult.IsError {
		t.Fatalf("zerops_verify on %s returned error: %s", rtHostname, getE2ETextContent(t, rtResult))
	}
	var rtVerify ops.VerifyResult
	if err := json.Unmarshal([]byte(getE2ETextContent(t, rtResult)), &rtVerify); err != nil {
		t.Fatalf("parse verify result: %v", err)
	}
	if rtVerify.Type != "runtime" {
		t.Errorf("rt Type = %q, want runtime", rtVerify.Type)
	}
	// service_running must pass — this is the core ACTIVE bug regression test.
	serviceRunningFound := false
	for _, c := range rtVerify.Checks {
		if c.Name == "service_running" {
			serviceRunningFound = true
			if c.Status != "pass" {
				t.Errorf("rt service_running = %q, want pass (ACTIVE status must be accepted)", c.Status)
			}
		}
	}
	if !serviceRunningFound {
		t.Error("service_running check not found in runtime verify result")
	}
	if len(rtVerify.Checks) != 6 {
		t.Errorf("rt Checks count = %d, want 6", len(rtVerify.Checks))
	}
	// Runtime should NOT be unhealthy if service is running.
	if rtVerify.Status == "unhealthy" {
		t.Errorf("rt Status = unhealthy — ACTIVE status bug not fixed")
	}
	t.Logf("  Runtime service: type=%s status=%s checks=%d", rtVerify.Type, rtVerify.Status, len(rtVerify.Checks))

	// --- Step 5: Verify nonexistent service ---
	logStep(t, 5, "zerops_verify on nonexistent hostname")
	notFoundResult := s.callTool("zerops_verify", map[string]any{"serviceHostname": "nonexistent"})
	if !notFoundResult.IsError {
		t.Error("expected IsError for nonexistent service")
	}
	errText := getE2ETextContent(t, notFoundResult)
	if !strings.Contains(errText, "SERVICE_NOT_FOUND") {
		t.Errorf("expected SERVICE_NOT_FOUND in error: %s", errText)
	}
	t.Log("  Nonexistent service correctly returned SERVICE_NOT_FOUND")
}
