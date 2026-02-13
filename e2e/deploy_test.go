//go:build e2e

// Tests for: e2e — deploy lifecycle via zerops_deploy with real Zerops API.
//
// This test creates a nodejs@22 service, deploys a minimal app via zcli push
// (local mode), polls zerops_events for build completion, and verifies the
// service is running. Mirrors the mount_test.go pattern.
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
	processes := parseProcesses(t, importText)
	t.Logf("  Import returned %d processes", len(processes))

	for _, proc := range processes {
		pid, ok := proc["processId"].(string)
		if !ok || pid == "" {
			continue
		}
		t.Logf("  Waiting for process %s (%s)", pid, proc["actionName"])
		waitForProcess(s, pid)
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
		MonitorHint string `json:"monitorHint"`
	}
	if err := json.Unmarshal([]byte(deployText), &deployResult); err != nil {
		t.Fatalf("parse deploy result: %v", err)
	}
	if deployResult.Status != "BUILD_TRIGGERED" {
		t.Errorf("status = %s, want BUILD_TRIGGERED", deployResult.Status)
	}
	if deployResult.Mode != "local" {
		t.Errorf("mode = %s, want local", deployResult.Mode)
	}
	if deployResult.MonitorHint == "" {
		t.Error("monitorHint should not be empty")
	}
	t.Logf("  Deploy result: status=%s mode=%s", deployResult.Status, deployResult.Mode)

	// --- Step 4: Poll zerops_events for build completion (10s interval, 300s max) ---
	step++
	logStep(t, step, "polling zerops_events for build completion")
	buildFinished := false
	for i := range 30 {
		if i > 0 {
			time.Sleep(10 * time.Second)
		} else {
			// Initial 5s wait for pipeline to register.
			time.Sleep(5 * time.Second)
		}

		eventsText := s.mustCallSuccess("zerops_events", map[string]any{
			"serviceHostname": appHostname,
			"limit":           10,
		})

		// Check for build/deploy events with terminal status.
		if strings.Contains(eventsText, "FINISHED") {
			t.Logf("  Build FINISHED (poll %d)", i+1)
			buildFinished = true
			break
		}
		if strings.Contains(eventsText, "FAILED") {
			// Grab logs for diagnosis.
			logsText := s.mustCallSuccess("zerops_logs", map[string]any{
				"serviceHostname": appHostname,
				"severity":        "error",
				"since":           "10m",
			})
			t.Fatalf("Build FAILED. Error logs: %s\nEvents: %s", logsText, eventsText)
		}
		t.Logf("  Build in progress (poll %d)", i+1)
	}
	if !buildFinished {
		t.Fatal("Build did not finish within 300s timeout")
	}

	// --- Step 5: Check for error logs ---
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

	// --- Step 6: Confirm startup in logs ---
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

	// --- Step 7: Verify service is RUNNING ---
	step++
	logStep(t, step, "zerops_discover verify RUNNING")
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
	svcStatus := discoverResult.Services[0].Status
	t.Logf("  Service status: %s", svcStatus)
	if svcStatus != "RUNNING" && svcStatus != "ACTIVE" {
		t.Errorf("service status = %s, want RUNNING or ACTIVE", svcStatus)
	}

	// --- Step 8: Delete service ---
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
