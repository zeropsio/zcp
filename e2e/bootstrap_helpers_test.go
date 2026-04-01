//go:build e2e

// Tests for: e2e — shared helpers for bootstrap workflow E2E tests.

package e2e_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// importService defines a service for import YAML generation.
type importService struct {
	Hostname         string
	Type             string
	Mode             string // NON_HA, HA (managed only)
	StartWithoutCode bool
	MinContainers    int
	EnableSubdomain  bool
	ObjStorageSize   int // only for object-storage
	Priority         int
}

// buildImportYAML constructs import YAML from test service entries.
func buildImportYAML(services []importService) string {
	var b strings.Builder
	b.WriteString("services:\n")
	for _, svc := range services {
		b.WriteString("  - hostname: " + svc.Hostname + "\n")
		b.WriteString("    type: " + svc.Type + "\n")
		if svc.Mode != "" {
			b.WriteString("    mode: " + svc.Mode + "\n")
		}
		if svc.StartWithoutCode {
			b.WriteString("    startWithoutCode: true\n")
		}
		if svc.MinContainers > 0 {
			b.WriteString(fmt.Sprintf("    minContainers: %d\n", svc.MinContainers))
		}
		if svc.EnableSubdomain {
			b.WriteString("    enableSubdomainAccess: true\n")
		}
		if svc.ObjStorageSize > 0 {
			b.WriteString(fmt.Sprintf("    objectStorageSize: %d\n", svc.ObjStorageSize))
		}
		if svc.Priority > 0 {
			b.WriteString(fmt.Sprintf("    priority: %d\n", svc.Priority))
		}
	}
	return b.String()
}

// bootstrapAndProvision runs a full bootstrap flow through provision completion.
// Steps: reset → start → complete discover (plan) → import → wait → complete provision.
// Returns parsed bootstrapProgress for assertions.
func bootstrapAndProvision(t *testing.T, s *e2eSession, plan []any, importYAML string, waitHostnames []string) bootstrapProgress {
	t.Helper()

	// Reset and start bootstrap.
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	startText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   t.Name(),
	})
	var startResp bootstrapProgress
	if err := json.Unmarshal([]byte(startText), &startResp); err != nil {
		t.Fatalf("parse bootstrap start: %v", err)
	}
	if startResp.SessionID == "" {
		t.Fatal("expected non-empty sessionId")
	}
	if startResp.Current == nil || startResp.Current.Name != "discover" {
		t.Fatal("expected current step to be 'discover'")
	}

	// Complete discover with plan.
	discoverText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan":   plan,
	})
	var discoverResp bootstrapProgress
	if err := json.Unmarshal([]byte(discoverText), &discoverResp); err != nil {
		t.Fatalf("parse discover complete: %v", err)
	}
	if discoverResp.Current == nil || discoverResp.Current.Name != "provision" {
		t.Fatal("expected current step to advance to 'provision'")
	}

	// Import services.
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
	t.Logf("  Import result: %s", truncate(importText, 200))

	// Wait for all services — runtime services need RUNNING/ACTIVE,
	// managed services just need to exist with any status.
	for _, wh := range waitHostnames {
		waitForServiceStatus(s, wh, "RUNNING", "ACTIVE", "NEW", "READY_TO_DEPLOY")
	}

	// Discover env vars — the provision checker needs this to verify managed service env vars.
	s.mustCallSuccess("zerops_discover", map[string]any{"includeEnvs": true})

	// Complete provision step.
	provText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "provision",
		"attestation": "All services created and env vars discovered.",
	})
	var provResp bootstrapProgress
	if err := json.Unmarshal([]byte(provText), &provResp); err != nil {
		t.Fatalf("parse provision complete: %v", err)
	}

	// Verify workflow progress after provision.
	if provResp.Progress.Completed != 2 {
		t.Errorf("expected 2 completed steps (discover + provision), got %d", provResp.Progress.Completed)
	}
	if provResp.Current == nil || provResp.Current.Name != "generate" {
		t.Errorf("expected current step 'generate' after provision, got %v", provResp.Current)
	}

	return provResp
}

// bootstrapAndProvisionExpectFail runs the bootstrap flow but expects provision to fail.
// Returns the provision response with a failed checkResult.
func bootstrapAndProvisionExpectFail(t *testing.T, s *e2eSession, plan []any, importYAML string, waitHostnames []string) bootstrapProgress {
	t.Helper()

	// Reset and start bootstrap.
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	startText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   t.Name(),
	})
	var startResp bootstrapProgress
	if err := json.Unmarshal([]byte(startText), &startResp); err != nil {
		t.Fatalf("parse bootstrap start: %v", err)
	}

	// Complete discover with plan.
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan":   plan,
	})

	// Import services.
	s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})

	// Wait for services to exist.
	for _, wh := range waitHostnames {
		waitForServiceStatus(s, wh, "RUNNING", "ACTIVE", "NEW", "READY_TO_DEPLOY")
	}

	// Discover env vars.
	s.mustCallSuccess("zerops_discover", map[string]any{"includeEnvs": true})

	// Complete provision step — expect it returns but check fails.
	provText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "provision",
		"attestation": "Expecting provision to fail.",
	})
	var provResp bootstrapProgress
	if err := json.Unmarshal([]byte(provText), &provResp); err != nil {
		t.Fatalf("parse provision complete: %v", err)
	}
	return provResp
}

// assertProvisionFailed verifies provision check failed and logs details.
func assertProvisionFailed(t *testing.T, resp bootstrapProgress) {
	t.Helper()
	if resp.CheckResult == nil {
		t.Fatal("expected checkResult in provision response (got nil)")
	}
	if resp.CheckResult.Passed {
		t.Logf("  unexpected pass: %s", resp.CheckResult.Summary)
		for _, c := range resp.CheckResult.Checks {
			t.Logf("    check %s: %s %s", c.Name, c.Status, c.Detail)
		}
		t.Fatal("expected provision check to fail, but it passed")
	}
}

// assertNoStageCheck verifies no stage_status check exists (for simple/dev modes).
func assertNoStageCheck(t *testing.T, resp bootstrapProgress) {
	t.Helper()
	if resp.CheckResult == nil {
		t.Fatal("expected checkResult in provision response (got nil)")
	}
	for _, c := range resp.CheckResult.Checks {
		if strings.HasSuffix(c.Name, "stage_status") {
			t.Errorf("unexpected stage check %q in simple/dev mode", c.Name)
		}
	}
}

// assertHasStageCheck verifies a stage_status check exists for standard mode.
func assertHasStageCheck(t *testing.T, resp bootstrapProgress, stageHostname string) {
	t.Helper()
	if resp.CheckResult == nil {
		t.Fatal("expected checkResult in provision response (got nil)")
	}
	checkName := stageHostname + "_status"
	for _, c := range resp.CheckResult.Checks {
		if c.Name == checkName {
			if c.Status != "pass" {
				t.Errorf("stage check %s: expected pass, got %s (%s)", checkName, c.Status, c.Detail)
			}
			return
		}
	}
	t.Errorf("stage check %s not found in provision checks", checkName)
}

// assertProvisionPassed fatals if provision check is missing or failed.
func assertProvisionPassed(t *testing.T, resp bootstrapProgress) {
	t.Helper()
	if resp.CheckResult == nil {
		t.Fatal("expected checkResult in provision response (got nil)")
	}
	if !resp.CheckResult.Passed {
		t.Errorf("provision check failed: %s", resp.CheckResult.Summary)
		for _, c := range resp.CheckResult.Checks {
			t.Logf("  check %s: %s %s", c.Name, c.Status, c.Detail)
		}
		t.Fatal("provision step check must pass")
	}
}

// assertEnvVarCheck verifies a specific hostname's env_vars check exists and passed.
func assertEnvVarCheck(t *testing.T, resp bootstrapProgress, hostname string) {
	t.Helper()
	if resp.CheckResult == nil {
		t.Fatalf("expected checkResult for env var verification of %s (got nil)", hostname)
	}
	checkName := hostname + "_env_vars"
	for _, c := range resp.CheckResult.Checks {
		if c.Name == checkName {
			if c.Status != "pass" {
				t.Errorf("env var check %s: expected pass, got %s (%s)", checkName, c.Status, c.Detail)
			}
			return
		}
	}
	t.Errorf("env var check %s not found in provision checks", checkName)
}

// assertNoEnvVarCheck verifies no env_vars check exists for a hostname (storage types).
func assertNoEnvVarCheck(t *testing.T, resp bootstrapProgress, hostname string) {
	t.Helper()
	if resp.CheckResult == nil {
		return
	}
	checkName := hostname + "_env_vars"
	for _, c := range resp.CheckResult.Checks {
		if c.Name == checkName {
			t.Errorf("unexpected env var check for %s (storage types should have none)", hostname)
			return
		}
	}
}
