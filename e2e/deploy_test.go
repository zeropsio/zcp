//go:build e2e

// Tests for: e2e — deploy lifecycle via zerops_deploy with real Zerops API.
//
// Two deploy patterns tested:
//   - Self-deploy: service deploys itself from /var/www (code written via SSH)
//   - Cross-service: code on source service, pushed to target service
//
// Both patterns verify end-to-end: import → write code → deploy → HTTP 200.
//
// Prerequisites:
//   - ZCP_API_KEY set
//   - SSH access to zcp and freshly created services
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_Deploy -v -timeout 900s

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// minimalZeropsYml returns a zerops.yml for a Node.js app matching the given hostname.
func minimalZeropsYml(hostname string) string {
	return fmt.Sprintf(`zerops:
  - setup: %s
    build:
      base: nodejs@22
      buildCommands:
        - echo "build done"
      deployFiles: ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
`, hostname)
}

// minimalServerJS returns a Node.js HTTP server with a /health endpoint.
const minimalServerJS = `const http = require('http');
const server = http.createServer((req, res) => {
  if (req.url === '/health') {
    res.writeHead(200, {'Content-Type': 'text/plain'});
    res.end('ok');
  } else {
    res.writeHead(200, {'Content-Type': 'text/plain'});
    res.end('hello from e2e deploy test');
  }
});
server.listen(3000, () => console.log('listening on 3000'));
`

// TestE2E_Deploy_SelfDeploy tests the self-deploy pattern:
// service deploys itself from /var/www after code is written via SSH.
func TestE2E_Deploy_SelfDeploy(t *testing.T) {
	requireSSH(t, "zcp")
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	appHostname := "zcpdpl" + suffix

	t.Cleanup(func() {
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, appHostname)
	})

	step := 0

	// --- Step 1: Start workflow ---
	step++
	logStep(t, step, "zerops_workflow bootstrap")
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "e2e self-deploy test",
	})

	// --- Step 2: Import with startWithoutCode (container must exist for SSH) ---
	step++
	logStep(t, step, "zerops_import %s (startWithoutCode + subdomain)", appHostname)
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    startWithoutCode: true
    enableSubdomainAccess: true
`, appHostname)
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
	assertImportAllFinished(t, importText)

	// --- Step 3: Wait for service to be RUNNING/ACTIVE ---
	step++
	logStep(t, step, "waiting for %s to be RUNNING", appHostname)
	waitForServiceStatus(s, appHostname, "RUNNING", "ACTIVE")
	// Extra wait for SSH daemon to start on fresh container.
	time.Sleep(5 * time.Second)

	// --- Step 4: Verify SSH access to fresh service ---
	step++
	logStep(t, step, "verify SSH access to %s", appHostname)
	out, err := sshExec(t, appHostname, "echo ok")
	if err != nil {
		t.Fatalf("SSH to %s failed: %s (%v)", appHostname, out, err)
	}

	// --- Step 5: Write app code to target's /var/www via SSH ---
	step++
	logStep(t, step, "writing app to %s:/var/www/", appHostname)
	writeAppViaSSH(t, appHostname, "/var/www", minimalZeropsYml(appHostname), minimalServerJS)

	// Git init on target (zcli push requires git repo).
	gitCmd := `cd /var/www && git init -q -b main 2>/dev/null; git config user.email 'test@test.com' && git config user.name 'test' && git add -A && git diff-index --quiet HEAD 2>/dev/null || git commit -q -m 'e2e deploy'`
	out, err = sshExec(t, appHostname, gitCmd)
	if err != nil {
		t.Fatalf("git init on %s: %s (%v)", appHostname, out, err)
	}
	t.Log("  App written + git initialized")

	// --- Step 6: Self-deploy (no sourceService → auto-inferred as targetService) ---
	step++
	logStep(t, step, "zerops_deploy targetService=%s (self-deploy)", appHostname)
	deployText := s.mustCallSuccess("zerops_deploy", map[string]any{
		"targetService": appHostname,
	})

	var deployResult struct {
		Status            string `json:"status"`
		Mode              string `json:"mode"`
		BuildStatus       string `json:"buildStatus"`
		TargetServiceType string `json:"targetServiceType"`
		SourceService     string `json:"sourceService"`
		TargetService     string `json:"targetService"`
	}
	if err := json.Unmarshal([]byte(deployText), &deployResult); err != nil {
		t.Fatalf("parse deploy result: %v", err)
	}
	if deployResult.Status != "DEPLOYED" {
		t.Fatalf("status = %s, want DEPLOYED (full: %s)", deployResult.Status, truncate(deployText, 500))
	}
	if deployResult.Mode != "ssh" {
		t.Errorf("mode = %s, want ssh", deployResult.Mode)
	}
	// Self-deploy: sourceService == targetService.
	if deployResult.SourceService != deployResult.TargetService {
		t.Errorf("self-deploy: source=%q != target=%q", deployResult.SourceService, deployResult.TargetService)
	}
	if !strings.Contains(deployResult.TargetServiceType, "nodejs") {
		t.Errorf("targetServiceType = %q, want to contain 'nodejs'", deployResult.TargetServiceType)
	}
	t.Logf("  Deploy: status=%s mode=%s source=%s target=%s", deployResult.Status, deployResult.Mode, deployResult.SourceService, deployResult.TargetService)

	// --- Step 7: Wait for RUNNING + HTTP health check ---
	step++
	logStep(t, step, "verify RUNNING + HTTP 200")
	waitForServiceStatus(s, appHostname, "RUNNING", "ACTIVE")
	deployAndVerifyHTTP(t, s, appHostname)

	// --- Step 8: Delete ---
	step++
	logStep(t, step, "zerops_delete %s", appHostname)
	deleteText := s.mustCallSuccess("zerops_delete", map[string]any{
		"serviceHostname": appHostname,
	})
	procID := extractProcessID(t, deleteText)
	waitForProcess(s, procID)
	t.Log("  Service deleted")
}

// TestE2E_Deploy_CrossService tests the cross-service deploy pattern:
// code on source (dev) is pushed to target (stage).
func TestE2E_Deploy_CrossService(t *testing.T) {
	requireSSH(t, "zcp")
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	devHostname := "zcpddev" + suffix
	stageHostname := "zcpdstg" + suffix
	deployDir := "/tmp/crossdeploy" + suffix

	t.Cleanup(func() {
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, devHostname, stageHostname)
		_, _ = sshExec(t, "zcp", fmt.Sprintf("rm -rf %s", deployDir))
	})

	step := 0

	// --- Step 1: Start workflow ---
	step++
	logStep(t, step, "zerops_workflow bootstrap")
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "e2e cross-service deploy test",
	})

	// --- Step 2: Import dev + stage ---
	step++
	logStep(t, step, "zerops_import dev=%s stage=%s", devHostname, stageHostname)
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    startWithoutCode: true
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    startWithoutCode: true
    enableSubdomainAccess: true
`, devHostname, stageHostname)
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
	assertImportAllFinished(t, importText)

	// --- Step 3: Wait for both RUNNING ---
	step++
	logStep(t, step, "waiting for both services RUNNING")
	waitForServiceStatus(s, devHostname, "RUNNING", "ACTIVE")
	waitForServiceStatus(s, stageHostname, "RUNNING", "ACTIVE")

	// --- Step 4: Write app to zcp deploy dir (source for cross-deploy) ---
	// zerops.yml setup must match TARGET hostname (stage), not source.
	step++
	logStep(t, step, "writing app to zcp:%s (setup=%s)", deployDir, stageHostname)
	writeAppViaSSH(t, "zcp", deployDir, minimalZeropsYml(stageHostname), minimalServerJS)
	t.Log("  App written to zcp")

	// --- Step 5: Cross-deploy: zcp → stage ---
	step++
	logStep(t, step, "zerops_deploy source=zcp target=%s", stageHostname)
	deployText := s.mustCallSuccess("zerops_deploy", map[string]any{
		"sourceService": "zcp",
		"targetService": stageHostname,
		"workingDir":    deployDir,
	})

	var deployResult struct {
		Status        string `json:"status"`
		Mode          string `json:"mode"`
		BuildStatus   string `json:"buildStatus"`
		SourceService string `json:"sourceService"`
		TargetService string `json:"targetService"`
	}
	if err := json.Unmarshal([]byte(deployText), &deployResult); err != nil {
		t.Fatalf("parse deploy result: %v", err)
	}
	if deployResult.Status != "DEPLOYED" {
		t.Fatalf("status = %s, want DEPLOYED (full: %s)", deployResult.Status, truncate(deployText, 500))
	}
	// Cross-service: source != target.
	if deployResult.SourceService == deployResult.TargetService {
		t.Errorf("cross-deploy: source=%q should differ from target=%q", deployResult.SourceService, deployResult.TargetService)
	}
	t.Logf("  Deploy: status=%s source=%s target=%s", deployResult.Status, deployResult.SourceService, deployResult.TargetService)

	// --- Step 6: Wait for stage RUNNING + HTTP health check ---
	step++
	logStep(t, step, "verify stage RUNNING + HTTP 200")
	waitForServiceStatus(s, stageHostname, "RUNNING", "ACTIVE")
	deployAndVerifyHTTP(t, s, stageHostname)

	// --- Step 7: Delete ---
	step++
	logStep(t, step, "delete test services")
	for _, hostname := range []string{devHostname, stageHostname} {
		deleteText := s.mustCallSuccess("zerops_delete", map[string]any{
			"serviceHostname": hostname,
			})
		procID := extractProcessID(t, deleteText)
		waitForProcess(s, procID)
		t.Logf("  Deleted %s", hostname)
	}
}

// TestE2E_Deploy_ErrorDiagnostic verifies that deploy to non-existent service
// returns SERVICE_NOT_FOUND with useful error info.
func TestE2E_Deploy_ErrorDiagnostic(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	result := s.callTool("zerops_deploy", map[string]any{
		"targetService": "nonexistentservice" + randomSuffix(),
	})

	if !result.IsError {
		t.Fatal("expected error for deploy to non-existent service")
	}

	text := getE2ETextContent(t, result)
	t.Logf("Error for non-existent service: %s", text)

	if strings.Contains(text, "SERVICE_NOT_FOUND") {
		t.Log("Correct: error code is SERVICE_NOT_FOUND")
	} else if strings.Contains(text, "SSH_DEPLOY_FAILED") {
		t.Error("Error classified as SSH_DEPLOY_FAILED for a pre-deploy validation failure — should be SERVICE_NOT_FOUND")
	}

	if !strings.Contains(text, "suggestion") {
		t.Error("Error response missing 'suggestion' field — LLM has no guidance")
	}
}
