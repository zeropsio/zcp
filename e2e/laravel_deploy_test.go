//go:build e2e

// Tests for: e2e — Laravel full deploy + managed service connectivity.
//
// PREREQUISITE: appdev, db, mydb, cache, storage services exist with code in /var/www.
//
// Run on zcp: /tmp/e2e-test -test.run TestLaravelFullStack -test.v -test.timeout 300s

package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLaravelFullStack(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)
	zcliLogin(t, h.authInfo.Token)

	step := 0

	// --- Deploy ---
	step++
	logStep(t, step, "reset + start workflow + deploy")
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "intent": "Laravel full stack test",
	})

	deployText := s.mustCallSuccess("zerops_deploy", map[string]any{
		"targetService": "appdev",
	})
	var dr struct {
		Status      string `json:"status"`
		BuildStatus string `json:"buildStatus"`
	}
	if err := json.Unmarshal([]byte(deployText), &dr); err != nil {
		t.Fatalf("parse deploy: %v", err)
	}
	t.Logf("  Deploy: status=%s buildStatus=%s", dr.Status, dr.BuildStatus)

	if dr.BuildStatus == "DEPLOY_FAILED" {
		t.Fatalf("DEPLOY_FAILED — check initCommands")
	}

	// --- Enable subdomain ---
	step++
	logStep(t, step, "enable subdomain + wait")
	s.mustCallSuccess("zerops_subdomain", map[string]any{
		"serviceHostname": "appdev", "action": "enable",
	})
	time.Sleep(10 * time.Second)

	// --- Verify all connections via /status ---
	step++
	logStep(t, step, "zerops_verify")
	verifyText := s.mustCallSuccess("zerops_verify", map[string]any{
		"serviceHostname": "appdev",
	})
	t.Logf("  Verify: %s", truncate(verifyText, 300))

	// --- Check logs for init errors ---
	step++
	logStep(t, step, "check logs")
	logsText := s.mustCallSuccess("zerops_logs", map[string]any{
		"serviceHostname": "appdev", "since": "10m",
	})
	if strings.Contains(logsText, "INIT COMMANDS FINISHED WITH ERROR") {
		t.Error("initCommands FAILED")
		t.Log(truncate(logsText, 500))
	}
	if strings.Contains(logsText, "INIT COMMANDS FINISHED") {
		t.Log("  initCommands OK")
	}

	t.Log("=== Deploy complete. Check /status manually or via curl ===")
}
