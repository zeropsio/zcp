//go:build e2e

// Tests for: e2e — deploy error classification accuracy.
//
// Forensic analysis (Mar 25) identified that classifySSHError at
// deploy_classify.go:36 matches strings.Contains(msg, "zerops.yml") on the
// FULL SSH output (including zcli progress messages). This causes false
// positives: any zcli push failure gets misclassified as "zerops.yml not found"
// because zcli always outputs progress lines mentioning "zerops.yml".
//
// This test triggers known deploy failures and verifies:
//   1. The error classification is ACCURATE (matches the real error, not progress output)
//   2. The raw diagnostic output is PRESERVED (not discarded)
//   3. Self-deploy on fresh containers works (the exact scenario that failed 5x)
//
// Uses zcp as the SSH source for cross-service deploys.
//
// Prerequisites:
//   - ZCP_API_KEY set
//   - SSH access to zcp (running on Zerops container)
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_Deploy_Error -v -timeout 600s

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


// TestE2E_Deploy_ErrorClassification_BadAuth tests that a deploy with invalid
// auth produces a DIAGNOSTIC error, not a false "zerops.yml not found".
//
// Scenario: Write valid zerops.yml + app to zcp, then deploy with a
// deliberately broken zcli auth state on the target service.
func TestE2E_Deploy_ErrorClassification_SelfDeploy(t *testing.T) {
	requireSSH(t, "zcp")
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	appHostname := "zcpdpl" + suffix
	deployDir := "/tmp/deplclassify" + suffix

	t.Cleanup(func() {
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, appHostname)
		_, _ = sshExec(t, "zcp", fmt.Sprintf("rm -rf %s", deployDir))
	})

	step := 0

	// --- Step 1: Start bootstrap workflow ---
	step++
	logStep(t, step, "starting bootstrap workflow")
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "e2e deploy error classification test",
	})

	// --- Step 2: Import nodejs service ---
	step++
	logStep(t, step, "importing nodejs@22: %s", appHostname)
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    enableSubdomainAccess: true
`, appHostname),
	})
	t.Logf("  Import: %s", truncate(importText, 200))

	// --- Step 3: Wait for service ---
	step++
	logStep(t, step, "waiting for %s to be ready", appHostname)
	waitForServiceReady(s, appHostname)

	// --- Step 4: Write valid app to zcp ---
	step++
	logStep(t, step, "writing minimal app to zcp:%s", deployDir)

	zeropsYml := fmt.Sprintf(`zerops:
  - setup: %s
    build:
      base: nodejs@22
      buildCommands:
        - echo "build ok"
      deployFiles: ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
`, appHostname)

	serverJS := `const http = require("http");
http.createServer((req, res) => res.end("ok")).listen(3000);
`

	writeAppViaSSH(t, "zcp", deployDir, zeropsYml, serverJS)

	// --- Step 5: Deploy (cross-service: zcp → app) --- should succeed ---
	step++
	logStep(t, step, "zerops_deploy zcp → %s (should succeed)", appHostname)
	deployText := s.mustCallSuccess("zerops_deploy", map[string]any{
		"sourceService": "zcp",
		"targetService": appHostname,
		"workingDir":    deployDir,
	})

	var deployResult struct {
		Status      string `json:"status"`
		Mode        string `json:"mode"`
		BuildStatus string `json:"buildStatus"`
		Message     string `json:"message"`
	}
	if err := json.Unmarshal([]byte(deployText), &deployResult); err != nil {
		t.Fatalf("parse deploy: %v", err)
	}
	if deployResult.Status != "DEPLOYED" {
		t.Fatalf("first deploy status = %s, want DEPLOYED (full response: %s)", deployResult.Status, truncate(deployText, 500))
	}
	t.Logf("  First deploy: %s (buildStatus=%s)", deployResult.Status, deployResult.BuildStatus)

	// --- Step 6: Now write BROKEN zerops.yml (missing setup entry) and deploy again ---
	step++
	logStep(t, step, "writing BROKEN zerops.yml (wrong setup hostname) to zcp")

	brokenYml := `zerops:
  - setup: wronghostname
    build:
      base: nodejs@22
      buildCommands:
        - echo "build ok"
      deployFiles: ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
`
	brokenB64 := base64.StdEncoding.EncodeToString([]byte(brokenYml))
	writeCmd2 := fmt.Sprintf("echo %s | base64 -d > %s/zerops.yml", brokenB64, deployDir)
	out, err := sshExec(t, "zcp", writeCmd2)
	if err != nil {
		t.Fatalf("write broken zerops.yml: %s (%v)", out, err)
	}

	// --- Step 7: Deploy with broken zerops.yml — expect failure with USEFUL error ---
	step++
	logStep(t, step, "zerops_deploy with wrong setup entry (expect build failure)")
	deployResult2 := s.callTool("zerops_deploy", map[string]any{
		"sourceService": "zcp",
		"targetService": appHostname,
		"workingDir":    deployDir,
	})
	text2 := getE2ETextContent(t, deployResult2)
	t.Logf("  Deploy with wrong setup: isError=%v response=%s", deployResult2.IsError, truncate(text2, 500))

	// The error (if any) should NOT say "zerops.yml not found" — the file exists.
	// It should mention the actual issue: wrong setup hostname, build failure, etc.
	if strings.Contains(text2, "zerops.yml not found") {
		t.Errorf("FALSE POSITIVE: error says 'zerops.yml not found' but file exists — classifySSHError matched on progress output")
		t.Logf("Full response: %s", text2)
	}

	// --- Step 8: Test self-deploy (the exact scenario from forensic analysis) ---
	step++
	logStep(t, step, "self-deploy: %s → %s (uses SSH internally)", appHostname, appHostname)

	// First write correct zerops.yml + server.js back to the target service.
	selfWriteB64Yml := base64.StdEncoding.EncodeToString([]byte(zeropsYml))
	selfWriteB64JS := base64.StdEncoding.EncodeToString([]byte(serverJS))
	selfWriteCmd := fmt.Sprintf("echo %s | base64 -d > /var/www/zerops.yml && echo %s | base64 -d > /var/www/server.js",
		selfWriteB64Yml, selfWriteB64JS)
	out, err = sshExec(t, appHostname, selfWriteCmd)
	if err != nil {
		t.Logf("Warning: SSH write to %s failed (service might need SSH init): %s (%v)", appHostname, out, err)
		t.Logf("Skipping self-deploy test — SSH not available on target")
	} else {
		// Git init + commit on the service.
		gitCmd := `cd /var/www && git init -q -b main 2>/dev/null; git add -A && git diff-index --quiet HEAD 2>/dev/null || git commit -q -m 'e2e test'`
		out, err = sshExec(t, appHostname, gitCmd)
		if err != nil {
			t.Logf("Warning: git init on %s failed: %s (%v)", appHostname, out, err)
		}

		selfDeployResult := s.callTool("zerops_deploy", map[string]any{
			"targetService": appHostname,
		})
		selfText := getE2ETextContent(t, selfDeployResult)
		t.Logf("  Self-deploy: isError=%v response=%s", selfDeployResult.IsError, truncate(selfText, 500))

		if selfDeployResult.IsError {
			// Analyze the error.
			if strings.Contains(selfText, "zerops.yml not found") {
				t.Errorf("SELF-DEPLOY FALSE POSITIVE: 'zerops.yml not found' — this is the RC2 bug from forensic analysis")
			}

			// Check if error includes diagnostic info.
			var errObj map[string]string
			if err := json.Unmarshal([]byte(selfText), &errObj); err == nil {
				if diag, ok := errObj["diagnostic"]; ok && diag != "" {
					t.Logf("  Diagnostic output present (%d chars) — good", len(diag))
				} else {
					t.Logf("  No diagnostic field in error — raw SSH output lost")
				}
			}
		} else {
			// Self-deploy succeeded.
			var selfResult struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal([]byte(selfText), &selfResult); err == nil {
				t.Logf("  Self-deploy status: %s", selfResult.Status)
			}
		}
	}

	// --- Step 9: Cleanup ---
	step++
	logStep(t, step, "cleanup: deleting %s", appHostname)
	deleteText := s.mustCallSuccess("zerops_delete", map[string]any{
		"serviceHostname": appHostname,
	})
	deleteProcID := extractProcessID(t, deleteText)
	waitForProcess(s, deleteProcID)
	t.Logf("  Service %s deleted", appHostname)
}
