//go:build e2e

// Tests for: e2e — deploy lifecycle via zerops_deploy with real Zerops API.
//
// This test creates a nodejs@22 service, deploys a minimal app via zcli push
// (local mode), waits for build completion (synchronous polling), and verifies
// the service is running.
//
// Prerequisites:
//   - ZCP_API_KEY set
//   - zcli installed and in PATH
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_Deploy -v -timeout 600s

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// zcliLogin logs zcli in with the given token.
func zcliLogin(t *testing.T, token string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "zcli", "login", token).CombinedOutput()
	if err != nil {
		t.Fatalf("zcli login failed: %s (%v)", string(out), err)
	}
}

// createMinimalApp creates a temp directory with a minimal Node.js app and zerops.yml.
// Returns the temp directory path.
func createMinimalApp(t *testing.T, hostname string) string {
	t.Helper()
	dir := t.TempDir()

	zeropsYML := fmt.Sprintf(`zerops:
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

	serverJS := `const http = require('http');
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

	if err := os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte(zeropsYML), 0o644); err != nil {
		t.Fatalf("write zerops.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.js"), []byte(serverJS), 0o644); err != nil {
		t.Fatalf("write server.js: %v", err)
	}

	// zcli push requires a git-initialized directory.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, args := range [][]string{
		{"init"},
		{"add", "."},
		{"commit", "-m", "init"},
	} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s (%v)", args, string(out), err)
		}
	}

	return dir
}

func TestE2E_Deploy(t *testing.T) {
	// Check zcli is available.
	if _, err := exec.LookPath("zcli"); err != nil {
		t.Skip("zcli not in PATH — skipping deploy E2E test")
	}

	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	appHostname := "zcpdpl" + suffix

	// Login zcli with the same token.
	zcliLogin(t, h.authInfo.Token)

	// Create minimal app in temp directory.
	appDir := createMinimalApp(t, appHostname)
	t.Logf("Created minimal app in %s", appDir)

	// Register cleanup to delete test service even if test fails.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, appHostname)
	})

	step := 0

	// --- Step 1: Import nodejs service ---
	step++
	logStep(t, step, "zerops_import (nodejs with enableSubdomainAccess)")
	importYAML := `services:
  - hostname: ` + appHostname + `
    type: nodejs@22
    minContainers: 1
    enableSubdomainAccess: true
`
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
	// Import now blocks until all processes complete.
	var importResult struct {
		Processes []struct {
			ProcessID string `json:"processId"`
			Status    string `json:"status"`
		} `json:"processes"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(importText), &importResult); err != nil {
		t.Fatalf("parse import result: %v", err)
	}
	t.Logf("  Import: %s", importResult.Summary)
	for _, proc := range importResult.Processes {
		if proc.Status != "FINISHED" {
			t.Errorf("import process %s status = %s, want FINISHED", proc.ProcessID, proc.Status)
		}
	}

	// --- Step 2: Wait for service to be ready ---
	step++
	logStep(t, step, "waiting for %s to be ready", appHostname)
	waitForServiceReady(s, appHostname)
	t.Log("  Service ready")

	// --- Step 3: Deploy via zerops_deploy (local mode) ---
	step++
	logStep(t, step, "zerops_deploy targetService=%s workingDir=%s", appHostname, appDir)
	deployText := s.mustCallSuccess("zerops_deploy", map[string]any{
		"targetService": appHostname,
		"workingDir":    appDir,
	})

	var deployResult struct {
		Status      string `json:"status"`
		Mode        string `json:"mode"`
		BuildStatus string `json:"buildStatus"`
	}
	if err := json.Unmarshal([]byte(deployText), &deployResult); err != nil {
		t.Fatalf("parse deploy result: %v", err)
	}
	// Deploy now blocks until build completes — status should be DEPLOYED.
	if deployResult.Status != "DEPLOYED" {
		t.Errorf("status = %s, want DEPLOYED", deployResult.Status)
	}
	if deployResult.Mode != "local" {
		t.Errorf("mode = %s, want local", deployResult.Mode)
	}
	if deployResult.BuildStatus != "ACTIVE" {
		t.Errorf("buildStatus = %s, want ACTIVE", deployResult.BuildStatus)
	}
	t.Logf("  Deploy result: status=%s mode=%s buildStatus=%s", deployResult.Status, deployResult.Mode, deployResult.BuildStatus)

	// --- Step 4: Check for error logs ---
	step++
	logStep(t, step, "zerops_logs severity=error")
	errorLogsText := s.mustCallSuccess("zerops_logs", map[string]any{
		"serviceHostname": appHostname,
		"severity":        "error",
		"since":           "5m",
	})
	// Empty or no-error response is acceptable.
	if strings.Contains(errorLogsText, "FATAL") || strings.Contains(errorLogsText, "panic") {
		t.Errorf("unexpected fatal/panic in error logs: %s", errorLogsText)
	}

	// --- Step 5: Confirm startup in logs ---
	step++
	logStep(t, step, "zerops_logs search for startup confirmation")
	startupText := s.mustCallSuccess("zerops_logs", map[string]any{
		"serviceHostname": appHostname,
		"search":          "listening",
		"since":           "5m",
	})
	if strings.Contains(startupText, "listening") {
		t.Log("  Startup confirmed: found 'listening' in logs")
	} else {
		t.Log("  No 'listening' log yet — may appear later (acceptable)")
	}

	// --- Step 6: Verify service is RUNNING ---
	// After build completion, the service transitions through CREATING before RUNNING.
	// Poll until it reaches a running state.
	step++
	logStep(t, step, "zerops_discover verify RUNNING")
	var svcStatus string
	for i := 0; i < 20; i++ {
		discoverText := s.mustCallSuccess("zerops_discover", map[string]any{
			"service": appHostname,
		})
		var discoverResult struct {
			Services []struct {
				Hostname string `json:"hostname"`
				Status   string `json:"status"`
			} `json:"services"`
		}
		if err := json.Unmarshal([]byte(discoverText), &discoverResult); err != nil {
			t.Fatalf("parse discover: %v", err)
		}
		if len(discoverResult.Services) == 0 {
			t.Fatal("service not found in discover")
		}
		svcStatus = discoverResult.Services[0].Status
		if svcStatus == "RUNNING" || svcStatus == "ACTIVE" {
			break
		}
		t.Logf("  Service status: %s (waiting...)", svcStatus)
		time.Sleep(5 * time.Second)
	}
	t.Logf("  Service status: %s", svcStatus)
	if svcStatus != "RUNNING" && svcStatus != "ACTIVE" {
		t.Errorf("service status = %s, want RUNNING or ACTIVE", svcStatus)
	}

	// --- Step 7: Delete service ---
	step++
	logStep(t, step, "zerops_delete %s", appHostname)
	deleteText := s.mustCallSuccess("zerops_delete", map[string]any{
		"serviceHostname": appHostname,
		"confirm":         true,
	})
	deleteProcID := extractProcessID(t, deleteText)
	t.Logf("  Delete process: %s", deleteProcID)
	waitForProcess(s, deleteProcID)
	t.Log("  Service cleaned up successfully")
}
