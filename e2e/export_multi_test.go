//go:build e2e

// Tests for: e2e — export with multiple service types (runtime + managed + env vars).
//
// Creates a small project with runtime + database services, tests export covers all.
// Cleans up after itself.
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_ExportMulti -count=1 -v -timeout 300s

package e2e_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
)

func TestE2E_ExportMulti_RuntimeAndManaged(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	rtHostname := "zcpex" + suffix
	dbHostname := "zcped" + suffix

	// Register cleanup.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, rtHostname, dbHostname)
	})

	// --- Step 1: Create runtime + managed services via direct API import ---
	t.Log("Step 1: Import runtime + managed services")

	importYAML := "services:\n" +
		"  - hostname: " + dbHostname + "\n" +
		"    type: postgresql@16\n" +
		"    mode: NON_HA\n" +
		"    priority: 10\n" +
		"  - hostname: " + rtHostname + "\n" +
		"    type: nodejs@22\n" +
		"    startWithoutCode: true\n" +
		"    enableSubdomainAccess: true\n" +
		"    verticalAutoscaling:\n" +
		"      minRam: 0.25\n"

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	importResult, err := h.client.ImportServices(ctx, h.projectID, importYAML)
	if err != nil {
		t.Fatalf("ImportServices: %v", err)
	}
	t.Logf("Import result: %+v", importResult)

	// Wait for services to be fully created (not just discoverable).
	t.Log("Step 2: Wait for services to be fully ready")
	waitForService(t, s, dbHostname, 120*time.Second)
	waitForService(t, s, rtHostname, 120*time.Second)
	// Extra wait: export API fails if any service is still being created.
	waitForProjectStable(t, s, 60*time.Second)

	// --- Step 3: Set a project env var (ignore if already exists from previous run) ---
	t.Log("Step 3: Set project env var")
	s.callTool("zerops_env", map[string]any{
		"action":    "set",
		"project":   true,
		"variables": []string{"EXPORT_TEST_VAR=hello_from_e2e"},
	})

	// --- Step 4: Export project ---
	t.Log("Step 4: Export project")
	exportRaw := s.mustCallSuccess("zerops_export", map[string]any{})

	var export ops.ExportResult
	if err := json.Unmarshal([]byte(exportRaw), &export); err != nil {
		t.Fatalf("unmarshal export: %v\nraw: %s", err, truncate(exportRaw, 500))
	}

	// --- Step 5: Validate export covers all service types ---
	t.Log("Step 5: Validate export result")

	// Must have at least our 2 services + zcp.
	if len(export.Services) < 3 {
		t.Errorf("expected at least 3 services, got %d", len(export.Services))
	}

	// Find our services.
	var foundRT, foundDB bool
	for _, svc := range export.Services {
		t.Logf("  service: %s type=%s mode=%s infra=%v status=%s",
			svc.Hostname, svc.Type, svc.Mode, svc.IsInfrastructure, svc.Status)

		switch svc.Hostname {
		case rtHostname:
			foundRT = true
			if svc.IsInfrastructure {
				t.Errorf("runtime service %s should not be infrastructure", rtHostname)
			}
			if !strings.Contains(svc.Type, "nodejs") {
				t.Errorf("runtime service type should contain 'nodejs', got %s", svc.Type)
			}
		case dbHostname:
			foundDB = true
			if !svc.IsInfrastructure {
				t.Errorf("managed service %s should be infrastructure", dbHostname)
			}
			if svc.Mode != "NON_HA" {
				t.Errorf("db mode should be NON_HA, got %s", svc.Mode)
			}
			if !strings.Contains(svc.Type, "postgresql") {
				t.Errorf("db type should contain 'postgresql', got %s", svc.Type)
			}
		}
	}

	if !foundRT {
		t.Errorf("runtime service %s not found in export", rtHostname)
	}
	if !foundDB {
		t.Errorf("managed service %s not found in export", dbHostname)
	}

	// --- Step 6: Validate export YAML contains project env vars ---
	t.Log("Step 6: Validate export YAML content")

	if !strings.Contains(export.ExportYAML, "EXPORT_TEST_VAR") {
		t.Error("export YAML should contain project env var EXPORT_TEST_VAR")
	}
	if !strings.Contains(export.ExportYAML, "project:") {
		t.Error("export YAML should contain 'project:' section")
	}

	// --- Step 7: Validate RuntimeServices / ManagedServices helpers ---
	runtimes := export.RuntimeServices()
	managed := export.ManagedServices()

	t.Logf("Classification: %d runtime, %d managed (total %d)",
		len(runtimes), len(managed), len(export.Services))

	if len(managed) == 0 {
		t.Error("expected at least 1 managed service")
	}
	// Runtime count depends on project state (zcp + our rtHostname).
	if len(runtimes) < 1 {
		t.Error("expected at least 1 runtime service")
	}

	// --- Step 8: Single service export ---
	t.Log("Step 8: Single service export")
	dbExport := s.mustCallSuccess("zerops_export", map[string]any{
		"service": dbHostname,
	})
	if !strings.Contains(dbExport, dbHostname) {
		t.Errorf("single service export should contain hostname %s", dbHostname)
	}
	if !strings.Contains(dbExport, "postgresql") {
		t.Error("single service export should contain 'postgresql'")
	}
	t.Logf("Single service export for %s: %d chars", dbHostname, len(dbExport))

	// --- Step 9: Cleanup project env var ---
	t.Log("Step 9: Cleanup project env var")
	discoverRaw := s.mustCallSuccess("zerops_discover", map[string]any{"includeEnvs": true})
	var discover ops.DiscoverResult
	if err := json.Unmarshal([]byte(discoverRaw), &discover); err == nil {
		for _, env := range discover.Project.Envs {
			if key, ok := env["key"].(string); ok && key == "EXPORT_TEST_VAR" {
				if id, ok := env["id"].(string); ok {
					s.callTool("zerops_env", map[string]any{
						"action":    "delete",
						"project":   true,
						"variables": []string{id},
					})
				}
			}
		}
	}

	t.Log("PASS: Export covers runtime + managed + project env vars")
}

// waitForService polls zerops_discover until the service appears and is not in ERROR state.
func waitForService(t *testing.T, s *e2eSession, hostname string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		result := s.callTool("zerops_discover", map[string]any{"service": hostname})
		if !result.IsError {
			return // Service found.
		}
		time.Sleep(5 * time.Second)
	}
	t.Fatalf("service %s not ready within %v", hostname, timeout)
}

// waitForProjectStable waits until zerops_export succeeds (no pending service creation).
func waitForProjectStable(t *testing.T, s *e2eSession, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		result := s.callTool("zerops_export", map[string]any{})
		if !result.IsError {
			return
		}
		t.Log("  project not stable yet, waiting...")
		time.Sleep(10 * time.Second)
	}
	t.Fatalf("project not stable within %v", timeout)
}
