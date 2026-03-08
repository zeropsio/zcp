//go:build e2e

// Tests for: e2e — build logs in deploy result on BUILD_FAILED.
//
// Verifies that when a deploy fails during the build pipeline, the
// zerops_deploy response includes buildLogs with actual build output.
// This is the key feature: LLM gets immediate visibility into what
// went wrong without extra tool calls.
//
// Prerequisites:
//   - ZCP_API_KEY set
//   - zcli vpn up <project-id> active (SSH access to zcpx)
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_BuildLogsOnFailure -v -timeout 600s

package e2e_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestE2E_BuildLogsOnFailure(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	const sourceHost = "zcpx"

	// Verify SSH access to zcpx.
	out, err := sshExec(t, sourceHost, "echo ok")
	if err != nil {
		t.Skipf("SSH to %s failed (VPN not active?): %s (%v)", sourceHost, out, err)
	}

	suffix := randomSuffix()
	appHostname := "zcpbl" + suffix
	deployDir := "/tmp/buildlogs" + suffix

	// Register cleanup: delete test service + remote temp dir.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, appHostname)
		_, _ = sshExec(t, sourceHost, fmt.Sprintf("rm -rf %s", deployDir))
	})

	step := 0

	// --- Step 1: Start bootstrap workflow (required for import) ---
	step++
	logStep(t, step, "starting bootstrap workflow session")
	// Reset any stale session from a previous run.
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "e2e build_logs test — create service then intentionally fail build",
	})
	t.Log("  Workflow session started")

	// --- Step 2: Import nodejs service ---
	step++
	logStep(t, step, "zerops_import nodejs@22 service: %s", appHostname)
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    minContainers: 1
`, appHostname)
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
			t.Fatalf("import process %s status = %s, want FINISHED", proc.ProcessID, proc.Status)
		}
	}
	t.Logf("  Service %s imported", appHostname)

	// --- Step 2: Wait for service to be ready ---
	step++
	logStep(t, step, "waiting for %s to be ready", appHostname)
	waitForServiceReady(s, appHostname)
	t.Log("  Service ready")

	// --- Step 3: Write broken app to zcpx ---
	step++
	logStep(t, step, "writing broken app to %s:%s", sourceHost, deployDir)

	// zerops.yml with a buildCommand that will definitely fail.
	brokenZeropsYml := fmt.Sprintf(`zerops:
  - setup: %s
    build:
      base: nodejs@22
      buildCommands:
        - echo "starting build..."
        - nonexistent_command_xyz_12345
      deployFiles: ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
`, appHostname)

	serverJS := `console.log("this should never run");`

	zeropsB64 := base64.StdEncoding.EncodeToString([]byte(brokenZeropsYml))
	serverB64 := base64.StdEncoding.EncodeToString([]byte(serverJS))

	prepareCmd := fmt.Sprintf(
		"rm -rf %s && mkdir -p %s && echo %s | base64 -d > %s/zerops.yml && echo %s | base64 -d > %s/server.js",
		deployDir, deployDir, zeropsB64, deployDir, serverB64, deployDir,
	)
	out, err = sshExec(t, sourceHost, prepareCmd)
	if err != nil {
		t.Fatalf("prepare deploy dir on %s: %s (%v)", sourceHost, out, err)
	}

	// Verify files written correctly.
	out, err = sshExec(t, sourceHost, fmt.Sprintf("cat %s/zerops.yml", deployDir))
	if err != nil {
		t.Fatalf("verify zerops.yml: %s (%v)", out, err)
	}
	if !strings.Contains(out, "nonexistent_command_xyz_12345") {
		t.Fatalf("broken command not found in zerops.yml: %s", out)
	}
	t.Log("  Broken app written to zcpx")

	// --- Step 5: Deploy (cross-deploy from zcpx → appHostname with broken code) ---
	step++
	logStep(t, step, "zerops_deploy sourceService=zcpx targetService=%s workingDir=%s", appHostname, deployDir)

	// Deploy will block until build pipeline completes. Expect BUILD_FAILED.
	deployResult := s.callTool("zerops_deploy", map[string]any{
		"sourceService": sourceHost,
		"targetService": appHostname,
		"workingDir":    deployDir,
	})

	deployText := getE2ETextContent(t, deployResult)
	t.Logf("  Deploy response: %.500s", deployText)

	// The deploy itself should NOT be an MCP error — BUILD_FAILED is a valid response.
	if deployResult.IsError {
		t.Fatalf("zerops_deploy returned MCP error (unexpected): %s", deployText)
	}

	var parsed struct {
		Status          string   `json:"status"`
		Mode            string   `json:"mode"`
		BuildStatus     string   `json:"buildStatus"`
		BuildDuration   string   `json:"buildDuration"`
		BuildLogs       []string `json:"buildLogs"`
		BuildLogsSource string   `json:"buildLogsSource"`
		Suggestion      string   `json:"suggestion"`
		NextActions     string   `json:"nextActions"`
	}
	if err := json.Unmarshal([]byte(deployText), &parsed); err != nil {
		t.Fatalf("parse deploy result: %v", err)
	}

	// --- Step 6: Verify BUILD_FAILED status ---
	step++
	logStep(t, step, "verifying BUILD_FAILED status")
	if parsed.Status != "BUILD_FAILED" {
		t.Errorf("status = %q, want BUILD_FAILED", parsed.Status)
	}
	if parsed.BuildStatus != "BUILD_FAILED" {
		t.Errorf("buildStatus = %q, want BUILD_FAILED", parsed.BuildStatus)
	}
	if parsed.Mode != "ssh" {
		t.Errorf("mode = %q, want ssh", parsed.Mode)
	}
	t.Logf("  Status: %s, BuildStatus: %s, BuildDuration: %s", parsed.Status, parsed.BuildStatus, parsed.BuildDuration)

	// --- Step 7: Verify buildLogs populated ---
	step++
	logStep(t, step, "verifying buildLogs populated")
	if len(parsed.BuildLogs) == 0 {
		t.Fatal("buildLogs is empty — this is the whole point of this feature")
	}
	t.Logf("  buildLogs: %d lines", len(parsed.BuildLogs))
	for i, line := range parsed.BuildLogs {
		t.Logf("    [%d] %s", i, line)
	}

	if parsed.BuildLogsSource != "build_container" {
		t.Errorf("buildLogsSource = %q, want %q", parsed.BuildLogsSource, "build_container")
	}

	// Verify logs contain evidence of the broken command.
	logsJoined := strings.Join(parsed.BuildLogs, "\n")
	if !strings.Contains(logsJoined, "nonexistent_command") && !strings.Contains(logsJoined, "not found") && !strings.Contains(logsJoined, "failed") {
		t.Errorf("buildLogs should contain evidence of the failed command, got:\n%s", logsJoined)
	}

	// --- Step 8: Verify suggestion and nextActions reference buildLogs ---
	step++
	logStep(t, step, "verifying suggestion and nextActions")
	if !strings.Contains(parsed.Suggestion, "buildLogs") {
		t.Errorf("suggestion should mention buildLogs, got: %q", parsed.Suggestion)
	}
	if !strings.Contains(parsed.NextActions, "buildLogs") {
		t.Errorf("nextActions should mention buildLogs, got: %q", parsed.NextActions)
	}
	t.Logf("  Suggestion: %s", parsed.Suggestion)
	t.Logf("  NextActions: %s", parsed.NextActions)

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
	t.Log("=== BUILD LOGS E2E RESULT ===")
	t.Logf("  buildLogs lines:  %d", len(parsed.BuildLogs))
	t.Logf("  buildLogsSource:  %s", parsed.BuildLogsSource)
	t.Logf("  buildDuration:    %s", parsed.BuildDuration)
	t.Log("  PASS: BUILD_FAILED response includes build pipeline output")
}
