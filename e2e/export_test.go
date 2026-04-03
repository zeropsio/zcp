//go:build e2e

// Tests for: e2e — export API + MCP tool against real Zerops API.
//
// Tests export endpoints on the current project (no service creation needed).
// Requires ZCP_API_KEY environment variable.
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_Export -count=1 -v -timeout 120s

package e2e_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
)

func TestE2E_Export_ProjectAPI(t *testing.T) {
	h := newHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Call the raw platform export API.
	yaml, err := h.client.GetProjectExport(ctx, h.projectID)
	if err != nil {
		t.Fatalf("GetProjectExport: %v", err)
	}

	// Basic validation: must be non-empty YAML with project and services.
	if len(yaml) < 20 {
		t.Fatalf("export YAML too short (%d chars): %s", len(yaml), yaml)
	}
	if !strings.Contains(yaml, "project:") {
		t.Error("export YAML missing 'project:' key")
	}
	if !strings.Contains(yaml, "services:") {
		t.Error("export YAML missing 'services:' key")
	}

	t.Logf("Project export YAML (%d chars):\n%s", len(yaml), yaml)
}

func TestE2E_Export_ServiceAPI(t *testing.T) {
	h := newHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Find a service to export (use the first one).
	services, err := h.client.ListServices(ctx, h.projectID)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}

	var targetID, targetName string
	for _, svc := range services {
		if svc.IsSystem() {
			continue
		}
		targetID = svc.ID
		targetName = svc.Name
		break
	}
	if targetID == "" {
		t.Skip("no non-system services to export")
	}

	yaml, err := h.client.GetServiceStackExport(ctx, targetID)
	if err != nil {
		t.Fatalf("GetServiceStackExport(%s): %v", targetName, err)
	}

	if len(yaml) < 10 {
		t.Fatalf("service export YAML too short (%d chars): %s", len(yaml), yaml)
	}
	if !strings.Contains(yaml, "services:") {
		t.Error("service export YAML missing 'services:' key")
	}
	if !strings.Contains(yaml, targetName) {
		t.Errorf("service export YAML missing hostname %q", targetName)
	}

	t.Logf("Service %s export YAML (%d chars):\n%s", targetName, len(yaml), yaml)
}

func TestE2E_Export_OpsExportProject(t *testing.T) {
	h := newHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Call the ops-level ExportProject which merges export + discover.
	result, err := ops.ExportProject(ctx, h.client, h.projectID)
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}

	// Validate result structure.
	if result.ProjectName == "" {
		t.Error("expected non-empty project name")
	}
	if result.ExportYAML == "" {
		t.Error("expected non-empty export YAML")
	}
	if len(result.Services) == 0 {
		t.Error("expected at least one service")
	}

	// Check that services have type and status.
	for _, svc := range result.Services {
		if svc.Hostname == "" {
			t.Error("service has empty hostname")
		}
		if svc.Type == "" {
			t.Errorf("service %s has empty type", svc.Hostname)
		}
		if svc.Status == "" {
			t.Errorf("service %s has empty status", svc.Hostname)
		}
	}

	// Check runtime vs managed classification.
	runtimes := result.RuntimeServices()
	managed := result.ManagedServices()
	t.Logf("Project %q: %d services (%d runtime, %d managed)",
		result.ProjectName, len(result.Services), len(runtimes), len(managed))

	for _, svc := range result.Services {
		t.Logf("  %s: type=%s status=%s mode=%s infra=%v subdomain=%v",
			svc.Hostname, svc.Type, svc.Status, svc.Mode, svc.IsInfrastructure, svc.SubdomainEnabled)
	}
}

func TestE2E_Export_MCPTool(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	// Call zerops_export via MCP tool (project-level export).
	result := s.mustCallSuccess("zerops_export", map[string]any{})

	// Parse the JSON response.
	var export ops.ExportResult
	if err := json.Unmarshal([]byte(result), &export); err != nil {
		t.Fatalf("unmarshal export result: %v\nraw: %s", err, result)
	}

	if export.ProjectName == "" {
		t.Error("expected non-empty project name from MCP tool")
	}
	if export.ExportYAML == "" {
		t.Error("expected non-empty export YAML from MCP tool")
	}
	if len(export.Services) == 0 {
		t.Error("expected at least one service from MCP tool")
	}

	t.Logf("MCP zerops_export: project=%s, services=%d, yaml=%d chars",
		export.ProjectName, len(export.Services), len(export.ExportYAML))
}

func TestE2E_Export_MCPTool_SingleService(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	// First discover to get a hostname.
	discoverResult := s.mustCallSuccess("zerops_discover", map[string]any{})
	var discover ops.DiscoverResult
	if err := json.Unmarshal([]byte(discoverResult), &discover); err != nil {
		t.Fatalf("unmarshal discover: %v", err)
	}
	if len(discover.Services) == 0 {
		t.Skip("no services to export")
	}

	hostname := discover.Services[0].Hostname

	// Call zerops_export for single service — returns plain text YAML.
	result := s.mustCallSuccess("zerops_export", map[string]any{
		"service": hostname,
	})

	if !strings.Contains(result, "services:") {
		t.Errorf("single service export missing 'services:' key: %s", result)
	}
	if !strings.Contains(result, hostname) {
		t.Errorf("single service export missing hostname %q: %s", hostname, result)
	}

	t.Logf("MCP zerops_export service=%s: %d chars", hostname, len(result))
}

func TestE2E_Export_DiscoverMode(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	// Verify Mode field is now populated in discover output.
	discoverResult := s.mustCallSuccess("zerops_discover", map[string]any{})
	var discover ops.DiscoverResult
	if err := json.Unmarshal([]byte(discoverResult), &discover); err != nil {
		t.Fatalf("unmarshal discover: %v", err)
	}

	for _, svc := range discover.Services {
		// Mode should be present (HA or empty for runtimes which force HA).
		t.Logf("  %s: mode=%q type=%s", svc.Hostname, svc.Mode, svc.Type)
	}
}
