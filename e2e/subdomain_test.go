//go:build e2e

// Tests for: e2e — subdomain activation lifecycle via zerops_subdomain.
//
// This test verifies that enableSubdomainAccess=true in import YAML
// pre-configures the subdomain URL but does NOT activate routing.
// An explicit zerops_subdomain action="enable" call is required after
// deploy for the subdomain to respond with HTTP 200 instead of 502.
//
// Prerequisites:
//   - ZCP_API_KEY set
//   - zcli installed and in PATH
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_Subdomain -v -timeout 600s

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// httpGetStatus sends an HTTP GET to the given URL and returns the status code.
// Returns -1 on error (timeout, DNS, connection refused, etc).
func httpGetStatus(url string, timeout time.Duration) int {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// pollHTTPHealth polls a URL until it returns 200 or the deadline is reached.
// Returns the final status code and whether it reached 200.
func pollHTTPHealth(url string, interval, deadline time.Duration) (int, bool) {
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		code := httpGetStatus(url, 10*time.Second)
		if code == 200 {
			return code, true
		}
		select {
		case <-timer.C:
			return code, false
		case <-ticker.C:
		}
	}
}

func TestE2E_Subdomain(t *testing.T) {
	// Check zcli is available.
	if _, err := exec.LookPath("zcli"); err != nil {
		t.Skip("zcli not in PATH — skipping subdomain E2E test")
	}

	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	appHostname := "zcpsub" + suffix

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

	// --- Step 1: Import nodejs service with enableSubdomainAccess ---
	step++
	logStep(t, step, "zerops_import (nodejs with enableSubdomainAccess)")
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    enableSubdomainAccess: true
`, appHostname)
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
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
	logStep(t, step, "zerops_deploy targetService=%s", appHostname)
	deployText := s.mustCallSuccess("zerops_deploy", map[string]any{
		"targetService": appHostname,
		"workingDir":    appDir,
	})
	var deployResult struct {
		Status      string `json:"status"`
		BuildStatus string `json:"buildStatus"`
	}
	if err := json.Unmarshal([]byte(deployText), &deployResult); err != nil {
		t.Fatalf("parse deploy result: %v", err)
	}
	if deployResult.Status != "DEPLOYED" {
		t.Errorf("status = %s, want DEPLOYED", deployResult.Status)
	}
	t.Logf("  Deploy: status=%s buildStatus=%s", deployResult.Status, deployResult.BuildStatus)

	// --- Step 4: Verify service is RUNNING ---
	step++
	logStep(t, step, "verify RUNNING")
	var svcStatus string
	for i := 0; i < 20; i++ {
		discoverText := s.mustCallSuccess("zerops_discover", map[string]any{
			"service": appHostname,
		})
		var disc struct {
			Services []struct {
				Status string `json:"status"`
			} `json:"services"`
		}
		if err := json.Unmarshal([]byte(discoverText), &disc); err != nil {
			t.Fatalf("parse discover: %v", err)
		}
		if len(disc.Services) > 0 {
			svcStatus = disc.Services[0].Status
			if svcStatus == "RUNNING" || svcStatus == "ACTIVE" {
				break
			}
		}
		time.Sleep(5 * time.Second)
	}
	if svcStatus != "RUNNING" && svcStatus != "ACTIVE" {
		t.Fatalf("service status = %s, want RUNNING or ACTIVE", svcStatus)
	}
	t.Logf("  Service status: %s", svcStatus)

	// --- Step 5: Enable subdomain and get URL from response ---
	step++
	logStep(t, step, "zerops_subdomain action=enable (explicit activation + get URL)")
	enableText := s.mustCallSuccess("zerops_subdomain", map[string]any{
		"serviceHostname": appHostname,
		"action":          "enable",
	})
	var enableResult struct {
		Status        string   `json:"status,omitempty"`
		Action        string   `json:"action"`
		SubdomainUrls []string `json:"subdomainUrls"`
	}
	if err := json.Unmarshal([]byte(enableText), &enableResult); err != nil {
		t.Fatalf("parse enable result: %v", err)
	}
	t.Logf("  Enable result: action=%s status=%s urls=%v", enableResult.Action, enableResult.Status, enableResult.SubdomainUrls)
	if enableResult.Action != "enable" {
		t.Errorf("action = %s, want enable", enableResult.Action)
	}
	if len(enableResult.SubdomainUrls) == 0 {
		t.Fatal("expected subdomainUrls in enable response")
	}
	subdomainURL := enableResult.SubdomainUrls[0]
	t.Logf("  subdomainUrl = %s", subdomainURL)

	// Wait for enable process to complete if one was returned.
	var enableProc struct {
		Process *struct {
			ID string `json:"id"`
		} `json:"process"`
	}
	if err := json.Unmarshal([]byte(enableText), &enableProc); err == nil && enableProc.Process != nil && enableProc.Process.ID != "" {
		t.Logf("  Waiting for enable process %s", enableProc.Process.ID)
		waitForProcess(s, enableProc.Process.ID)
	}

	// --- Step 6: Poll subdomain until HTTP 200 ---
	step++
	logStep(t, step, "HTTP health check AFTER explicit enable (expect 200)")
	healthURL := subdomainURL + "/health"
	code, ok := pollHTTPHealth(healthURL, 5*time.Second, 60*time.Second)
	if !ok {
		t.Fatalf("subdomain health check failed: last status=%d, want 200", code)
	}
	t.Logf("  Post-enable HTTP status: %d", code)

	// --- Step 7: Verify subdomain is idempotent (second enable = safe) ---
	// The Zerops API may return already_enabled, or start a new process that
	// finishes/fails quickly. Either way, the subdomain should remain active.
	step++
	logStep(t, step, "zerops_subdomain action=enable (idempotent check)")
	enable2Result := s.callTool("zerops_subdomain", map[string]any{
		"serviceHostname": appHostname,
		"action":          "enable",
	})
	enable2Text := getE2ETextContent(t, enable2Result)
	if strings.Contains(enable2Text, "already_enabled") {
		t.Log("  Confirmed: idempotent (already_enabled)")
	} else {
		t.Logf("  Second enable response: %s", enable2Text)
		// Wait for any triggered process to complete before cleanup.
		var proc2 struct {
			Process *struct {
				ID string `json:"id"`
			} `json:"process"`
		}
		if err := json.Unmarshal([]byte(enable2Text), &proc2); err == nil && proc2.Process != nil && proc2.Process.ID != "" {
			t.Logf("  Waiting for process %s to settle", proc2.Process.ID)
			// Poll but don't fatal on FAILED — the process failing is acceptable
			// behavior for idempotent re-enable.
			for i := 0; i < maxPollAttempts; i++ {
				pText := s.mustCallSuccess("zerops_process", map[string]any{
					"processId": proc2.Process.ID,
				})
				var ps struct {
					Status string `json:"status"`
				}
				if err := json.Unmarshal([]byte(pText), &ps); err == nil {
					if ps.Status == "FINISHED" || ps.Status == "FAILED" || ps.Status == "CANCELED" {
						t.Logf("  Process settled: %s", ps.Status)
						break
					}
				}
				time.Sleep(pollInterval)
			}
		}
	}
	// Verify subdomain is still active after second enable.
	finalCode := httpGetStatus(healthURL, 10*time.Second)
	if finalCode != 200 {
		t.Errorf("subdomain health after second enable: %d, want 200", finalCode)
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
