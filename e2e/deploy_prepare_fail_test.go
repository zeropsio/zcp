//go:build e2e

// Tests for: e2e — deploy with PREPARING_RUNTIME_FAILED status.
//
// Verifies that when prepareCommands fail during deploy, the polling
// terminates immediately (not 15-min timeout) and the response includes
// the actual PREPARING_RUNTIME_FAILED status with build logs.
//
// Prerequisites:
//   - ZCP_API_KEY set
//   - zcli vpn up <project-id> active (SSH access to zcp)
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_DeployPrepareCommandsFailed -v -timeout 600s

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestE2E_DeployPrepareCommandsFailed(t *testing.T) {
	const sourceHost = "zcp"
	requireSSH(t, sourceHost)
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	appHostname := "zcppf" + suffix
	deployDir := "/tmp/prepfail" + suffix

	// Register cleanup.
	t.Cleanup(func() {
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, appHostname)
		_, _ = sshExec(t, sourceHost, fmt.Sprintf("rm -rf %s", deployDir))
	})

	step := 0

	// --- Step 1: Bootstrap + adopt (deploy gate refuses un-adopted targets) ---
	step++
	logStep(t, step, "bootstrap dev service %s (full close — deploy gate needs adoption)", appHostname)
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    startWithoutCode: true
    minContainers: 1
`, appHostname)
	bootstrapDevServiceForDeploy(t, s, appHostname, "nodejs@22", importYAML, nil)
	t.Log("  Service ready")

	// --- Step 4: Write app with broken prepareCommands to zcp ---
	step++
	logStep(t, step, "writing app with broken prepareCommands to %s:%s", sourceHost, deployDir)

	// zerops.yml with valid build but broken run.prepareCommands.
	zeropsYml := fmt.Sprintf(`zerops:
  - setup: %s
    build:
      base: nodejs@22
      buildCommands:
        - echo "build ok"
      deployFiles: ./
    run:
      base: nodejs@22
      prepareCommands:
        - nonexistent_prepare_cmd_xyz_12345
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
`, appHostname)

	serverJS := `const http = require("http");
http.createServer((req, res) => res.end("ok")).listen(3000);
`

	writeAppViaSSH(t, sourceHost, deployDir, zeropsYml, serverJS)

	// Verify the broken prepareCommand is in the written file.
	out, err := sshExec(t, sourceHost, fmt.Sprintf("cat %s/zerops.yml", deployDir))
	if err != nil {
		t.Fatalf("verify zerops.yml: %s (%v)", out, err)
	}
	if !strings.Contains(out, "nonexistent_prepare_cmd_xyz_12345") {
		t.Fatalf("broken prepareCommand not found in zerops.yml: %s", out)
	}
	t.Log("  App with broken prepareCommands written to zcp")

	// --- Step 5: Deploy and measure time ---
	step++
	logStep(t, step, "zerops_deploy sourceService=zcp targetService=%s workingDir=%s", appHostname, deployDir)

	deployStart := time.Now()
	deployResult := s.callTool("zerops_deploy", map[string]any{
		"sourceService": sourceHost,
		"targetService": appHostname,
		"workingDir":    deployDir,
	})
	deployDuration := time.Since(deployStart)

	deployText := getE2ETextContent(t, deployResult)
	t.Logf("  Deploy completed in %s", deployDuration.Truncate(time.Second))
	t.Logf("  Deploy response: %.500s", deployText)

	// Should NOT be an MCP error — failed deploy is a valid response.
	if deployResult.IsError {
		t.Fatalf("zerops_deploy returned MCP error (unexpected): %s", deployText)
	}

	// Should complete in < 5 minutes (not the 15-min timeout).
	if deployDuration > 5*time.Minute {
		t.Errorf("deploy took %s — expected < 5 minutes (defensive polling should terminate quickly)", deployDuration)
	}

	var parsed struct {
		Status          string   `json:"status"`
		BuildStatus     string   `json:"buildStatus"`
		BuildLogs       []string `json:"buildLogs"`
		BuildLogsSource string   `json:"buildLogsSource"`
		Suggestion      string   `json:"suggestion"`
	}
	if err := json.Unmarshal([]byte(deployText), &parsed); err != nil {
		t.Fatalf("parse deploy result: %v", err)
	}

	// --- Step 6: Verify PREPARING_RUNTIME_FAILED status ---
	step++
	logStep(t, step, "verifying PREPARING_RUNTIME_FAILED status")
	if parsed.Status != "PREPARING_RUNTIME_FAILED" {
		t.Errorf("status = %q, want PREPARING_RUNTIME_FAILED", parsed.Status)
	}
	if parsed.BuildStatus != "PREPARING_RUNTIME_FAILED" {
		t.Errorf("buildStatus = %q, want PREPARING_RUNTIME_FAILED", parsed.BuildStatus)
	}

	// --- Step 7: Verify buildLogs populated ---
	step++
	logStep(t, step, "verifying buildLogs populated")
	if len(parsed.BuildLogs) == 0 {
		t.Error("buildLogs is empty — expected pipeline output")
	} else {
		t.Logf("  buildLogs: %d lines", len(parsed.BuildLogs))
		for i, line := range parsed.BuildLogs {
			t.Logf("    [%d] %s", i, line)
		}
	}
	if parsed.BuildLogsSource != "build_container" {
		t.Errorf("buildLogsSource = %q, want %q", parsed.BuildLogsSource, "build_container")
	}

	// --- Step 8: Verify suggestion mentions the status ---
	step++
	logStep(t, step, "verifying suggestion")
	if parsed.Suggestion == "" {
		t.Error("expected non-empty suggestion")
	}
	// The curated prepare-failure suggestion names the failing phase
	// (run.prepareCommands) with concrete causes — it no longer echoes the
	// status constant.
	if !strings.Contains(parsed.Suggestion, "prepareCommands") {
		t.Errorf("suggestion should target run.prepareCommands, got: %q", parsed.Suggestion)
	}
	t.Logf("  Suggestion: %s", parsed.Suggestion)

	// --- Step 9: Delete service ---
	step++
	logStep(t, step, "cleaning up %s", appHostname)
	deleteText := s.mustCallSuccess("zerops_delete", map[string]any{
		"serviceHostname": appHostname,
	})
	deleteProcID := extractProcessID(t, deleteText)
	waitForProcess(s, deleteProcID)
	t.Logf("  Service %s deleted", appHostname)

	t.Log("")
	t.Log("=== PREPARE FAIL E2E RESULT ===")
	t.Logf("  status:           %s", parsed.Status)
	t.Logf("  buildLogs lines:  %d", len(parsed.BuildLogs))
	t.Logf("  deploy duration:  %s", deployDuration.Truncate(time.Second))
	t.Log("  PASS: PREPARING_RUNTIME_FAILED detected immediately with logs")
}
