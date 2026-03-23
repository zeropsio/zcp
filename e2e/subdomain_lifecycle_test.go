//go:build e2e

// Tests for: e2e — subdomain platform behavior verified against live Zerops API.
//
// Verifies facts from the 2026-03-23 subdomain investigation:
//   - enableSubdomainAccess in import does NOT set subdomainAccess=true in API
//   - zeropsSubdomain env var is always present (even before enable)
//   - zeropsSubdomain env var contains port-aware full URL
//   - subdomainAccess persists across re-deploy (no re-enable needed)
//   - Port 80 URL has no port suffix, non-80 has -{port} suffix
//
// Prerequisites:
//   - ZCP_API_KEY set
//   - zcli installed and in PATH
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_SubdomainLifecycle -v -timeout 900s

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestE2E_SubdomainLifecycle(t *testing.T) {
	if _, err := exec.LookPath("zcli"); err != nil {
		t.Skip("zcli not in PATH — skipping subdomain lifecycle E2E test")
	}

	h := newHarness(t)
	s := newSession(t, h.srv)
	suffix := randomSuffix()
	zcliLogin(t, h.authInfo.Token)

	phpHost := "zcpslph" + suffix // port 80
	goHost := "zcpslgo" + suffix  // port 8080

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, phpHost, goHost)
	})

	step := 0

	// ---------------------------------------------------------------
	// Step 1: Import with enableSubdomainAccess=true
	// ---------------------------------------------------------------
	step++
	logStep(t, step, "import services (php port 80, go port 8080) with enableSubdomainAccess=true")

	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: php-nginx@8.4
    minContainers: 1
    enableSubdomainAccess: true
    startWithoutCode: true
  - hostname: %s
    type: go@1
    minContainers: 1
    enableSubdomainAccess: true
    startWithoutCode: true
`, phpHost, goHost)

	importText := s.mustCallSuccess("zerops_import", map[string]any{"content": importYAML})
	assertImportAllFinished(t, importText)
	waitForServiceReady(s, phpHost)
	waitForServiceReady(s, goHost)
	t.Log("  Both services ACTIVE")

	// ---------------------------------------------------------------
	// Step 2: Verify enableSubdomainAccess does NOT activate routing
	// ---------------------------------------------------------------
	step++
	logStep(t, step, "verify subdomainAccess=false in API after import (flag pre-configures, does not activate)")

	ctx := context.Background()
	phpSvc := mustGetServiceByHostname(t, h, ctx, phpHost)
	goSvc := mustGetServiceByHostname(t, h, ctx, goHost)

	if phpSvc.SubdomainAccess {
		t.Error("php: subdomainAccess should be false after import (not yet enabled)")
	}
	if goSvc.SubdomainAccess {
		t.Error("go: subdomainAccess should be false after import (not yet enabled)")
	}
	t.Log("  Confirmed: enableSubdomainAccess in import does NOT set subdomainAccess=true")

	// ---------------------------------------------------------------
	// Step 3: Verify zeropsSubdomain env var exists BEFORE enable
	// ---------------------------------------------------------------
	step++
	logStep(t, step, "verify zeropsSubdomain env var present before enable")

	phpEnvURL := getZeropsSubdomainEnv(t, h, ctx, phpSvc.ID)
	goEnvURL := getZeropsSubdomainEnv(t, h, ctx, goSvc.ID)

	if phpEnvURL == "" {
		t.Fatal("php: zeropsSubdomain env var not found (expected even before enable)")
	}
	if goEnvURL == "" {
		t.Fatal("go: zeropsSubdomain env var not found (expected even before enable)")
	}
	t.Logf("  php zeropsSubdomain (pre-enable): %s", phpEnvURL)
	t.Logf("  go zeropsSubdomain (pre-enable): %s", goEnvURL)

	// ---------------------------------------------------------------
	// Step 4: Deploy both services
	// ---------------------------------------------------------------
	step++
	logStep(t, step, "deploy both services")

	phpDir := createMinimalPHPApp(t, phpHost)
	goDir := createMinimalGoApp(t, goHost)

	for _, tc := range []struct {
		hostname string
		dir      string
	}{
		{phpHost, phpDir},
		{goHost, goDir},
	} {
		deployText := s.mustCallSuccess("zerops_deploy", map[string]any{
			"targetService": tc.hostname,
			"workingDir":    tc.dir,
		})
		var dr struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(deployText), &dr); err != nil {
			t.Fatalf("parse deploy %s: %v", tc.hostname, err)
		}
		if dr.Status != "DEPLOYED" {
			t.Fatalf("%s deploy status = %s, want DEPLOYED", tc.hostname, dr.Status)
		}
		t.Logf("  %s deployed", tc.hostname)
	}
	waitForServiceRunning(s, phpHost)
	waitForServiceRunning(s, goHost)

	// ---------------------------------------------------------------
	// Step 5: Enable subdomain on both, verify API state + URL format
	// ---------------------------------------------------------------
	step++
	logStep(t, step, "enable subdomain, verify API subdomainAccess=true and URL format")

	phpEnableURL := mustEnableSubdomain(t, s, phpHost)
	goEnableURL := mustEnableSubdomain(t, s, goHost)

	// Verify API state changed.
	phpSvc = mustGetServiceByHostname(t, h, ctx, phpHost)
	goSvc = mustGetServiceByHostname(t, h, ctx, goHost)

	if !phpSvc.SubdomainAccess {
		t.Error("php: subdomainAccess should be true after enable")
	}
	if !goSvc.SubdomainAccess {
		t.Error("go: subdomainAccess should be true after enable")
	}

	// Verify URL format: port 80 has no suffix, port 8080 has -8080 suffix.
	assertSubdomainURL(t, phpEnableURL, phpHost)
	assertSubdomainURL(t, goEnableURL, goHost)

	if strings.Contains(phpEnableURL, "-80.") || strings.Contains(phpEnableURL, "-80/") {
		t.Errorf("php port 80 URL should NOT contain -80 suffix: %s", phpEnableURL)
	}
	if !strings.Contains(goEnableURL, "-8080.") {
		t.Errorf("go port 8080 URL should contain -8080 suffix: %s", goEnableURL)
	}
	t.Logf("  php URL: %s (no port suffix)", phpEnableURL)
	t.Logf("  go URL: %s (-8080 suffix)", goEnableURL)

	// ---------------------------------------------------------------
	// Step 6: Verify zeropsSubdomain env var matches enable URL
	// ---------------------------------------------------------------
	step++
	logStep(t, step, "verify zeropsSubdomain env var matches enable URL")

	phpEnvURL = getZeropsSubdomainEnv(t, h, ctx, phpSvc.ID)
	goEnvURL = getZeropsSubdomainEnv(t, h, ctx, goSvc.ID)

	if phpEnvURL != phpEnableURL {
		t.Errorf("php zeropsSubdomain env var %q != enable URL %q", phpEnvURL, phpEnableURL)
	}
	if goEnvURL != goEnableURL {
		t.Errorf("go zeropsSubdomain env var %q != enable URL %q", goEnvURL, goEnableURL)
	}
	t.Log("  Env var matches enable URL for both services")

	// ---------------------------------------------------------------
	// Step 7: Verify HTTP reachability
	// ---------------------------------------------------------------
	step++
	logStep(t, step, "verify HTTP reachability on both subdomain URLs")

	for _, tc := range []struct {
		name string
		url  string
	}{
		{"php", phpEnableURL},
		{"go", goEnableURL},
	} {
		code, ok := pollHTTPHealth(tc.url+"/health", 5*time.Second, 90*time.Second)
		if !ok {
			t.Fatalf("%s: health check failed (last=%d), want 200", tc.name, code)
		}
		t.Logf("  %s: HTTP %d OK", tc.name, code)
	}

	// ---------------------------------------------------------------
	// Step 8: Re-deploy PHP, verify subdomain survives
	// ---------------------------------------------------------------
	step++
	logStep(t, step, "re-deploy PHP, verify subdomain persists (no re-enable)")

	redeployText := s.mustCallSuccess("zerops_deploy", map[string]any{
		"targetService": phpHost,
		"workingDir":    phpDir,
	})
	var rdr struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(redeployText), &rdr); err != nil {
		t.Fatalf("parse redeploy: %v", err)
	}
	if rdr.Status != "DEPLOYED" {
		t.Fatalf("redeploy status = %s, want DEPLOYED", rdr.Status)
	}
	waitForServiceRunning(s, phpHost)
	t.Log("  Re-deployed, waiting for subdomain...")

	// Verify subdomain still works WITHOUT calling enable again.
	code, ok := pollHTTPHealth(phpEnableURL+"/health", 5*time.Second, 90*time.Second)
	if !ok {
		t.Fatalf("php subdomain after redeploy: health check failed (last=%d), want 200", code)
	}
	t.Logf("  PHP subdomain after redeploy: HTTP %d OK (no re-enable needed)", code)

	// Verify API state still shows subdomainAccess=true.
	phpSvc = mustGetServiceByHostname(t, h, ctx, phpHost)
	if !phpSvc.SubdomainAccess {
		t.Error("php: subdomainAccess should still be true after re-deploy")
	}
	t.Log("  Confirmed: subdomain persists across re-deploy")

	// ---------------------------------------------------------------
	// Step 9: Disable + verify deactivation
	// ---------------------------------------------------------------
	step++
	logStep(t, step, "disable subdomain, verify deactivation")

	s.mustCallSuccess("zerops_subdomain", map[string]any{
		"serviceHostname": phpHost,
		"action":          "disable",
	})

	phpSvc = mustGetServiceByHostname(t, h, ctx, phpHost)
	if phpSvc.SubdomainAccess {
		t.Error("php: subdomainAccess should be false after disable")
	}
	t.Log("  Confirmed: disable sets subdomainAccess=false")

	// ---------------------------------------------------------------
	// Step 10: Cleanup
	// ---------------------------------------------------------------
	step++
	logStep(t, step, "delete test services")
	for _, hostname := range []string{phpHost, goHost} {
		deleteText := s.mustCallSuccess("zerops_delete", map[string]any{
			"serviceHostname": hostname,
			"confirm":         true,
		})
		procID := extractProcessID(t, deleteText)
		waitForProcess(s, procID)
		t.Logf("  Deleted %s", hostname)
	}
}

// mustGetServiceByHostname resolves a service by hostname using the platform client.
func mustGetServiceByHostname(t *testing.T, h *e2eHarness, ctx context.Context, hostname string) *platformServiceInfo {
	t.Helper()
	services, err := h.client.ListServices(ctx, h.projectID)
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	for i := range services {
		if services[i].Name == hostname {
			return &platformServiceInfo{
				ID:              services[i].ID,
				SubdomainAccess: services[i].SubdomainAccess,
			}
		}
	}
	t.Fatalf("service %s not found", hostname)
	return nil
}

// platformServiceInfo carries the API fields we care about for subdomain tests.
type platformServiceInfo struct {
	ID              string
	SubdomainAccess bool
}

// getZeropsSubdomainEnv reads the zeropsSubdomain env var from the API for a service.
func getZeropsSubdomainEnv(t *testing.T, h *e2eHarness, ctx context.Context, serviceID string) string {
	t.Helper()
	envs, err := h.client.GetServiceEnv(ctx, serviceID)
	if err != nil {
		t.Fatalf("get env vars: %v", err)
	}
	for _, env := range envs {
		if env.Key == "zeropsSubdomain" {
			return env.Content
		}
	}
	return ""
}

// mustEnableSubdomain enables subdomain and returns the first URL.
func mustEnableSubdomain(t *testing.T, s *e2eSession, hostname string) string {
	t.Helper()
	text := s.mustCallSuccess("zerops_subdomain", map[string]any{
		"serviceHostname": hostname,
		"action":          "enable",
	})
	var result struct {
		SubdomainUrls []string `json:"subdomainUrls"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("parse enable %s: %v", hostname, err)
	}
	if len(result.SubdomainUrls) == 0 {
		t.Fatalf("%s: no subdomainUrls in enable response", hostname)
	}
	return result.SubdomainUrls[0]
}

// waitForServiceRunning polls until a service reaches RUNNING or ACTIVE.
func waitForServiceRunning(s *e2eSession, hostname string) {
	s.t.Helper()
	for i := 0; i < maxPollAttempts; i++ {
		text := s.mustCallSuccess("zerops_discover", map[string]any{"service": hostname})
		var disc struct {
			Services []struct {
				Status string `json:"status"`
			} `json:"services"`
		}
		if err := json.Unmarshal([]byte(text), &disc); err == nil && len(disc.Services) > 0 {
			st := disc.Services[0].Status
			if st == "RUNNING" || st == "ACTIVE" {
				return
			}
		}
		time.Sleep(pollInterval)
	}
	s.t.Fatalf("service %s did not reach RUNNING after %d attempts", hostname, maxPollAttempts)
}

// assertImportAllFinished checks that all import processes completed.
func assertImportAllFinished(t *testing.T, importJSON string) {
	t.Helper()
	var result struct {
		Processes []struct {
			ProcessID string `json:"processId"`
			Status    string `json:"status"`
			Service   string `json:"service"`
		} `json:"processes"`
	}
	if err := json.Unmarshal([]byte(importJSON), &result); err != nil {
		t.Fatalf("parse import: %v", err)
	}
	for _, p := range result.Processes {
		if p.Status != "FINISHED" {
			t.Fatalf("import process %s for %s: status=%s, want FINISHED", p.ProcessID, p.Service, p.Status)
		}
	}
}
