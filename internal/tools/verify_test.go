// Tests for: zerops_verify — MCP tool handler.
package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestVerifyTool_RuntimeHealthy(t *testing.T) {
	t.Parallel()

	// No log access configured → log checks skip (not fail).
	// No ports → worker runtime, so no HTTP check. Service running → pass.
	// All pass/skip → healthy.
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusRunning},
		})
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1", "", runtime.Info{}, nil)

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "app"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if vr.Type != "runtime" {
		t.Errorf("Type = %q, want runtime", vr.Type)
	}
	// service_running=pass, log checks=skip (no log access), no HTTP checks for a worker → healthy.
	if vr.Status != "healthy" {
		t.Errorf("Status = %q, want healthy", vr.Status)
	}
}

func TestVerifyTool_ManagedHealthy(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16", ServiceStackTypeCategoryName: "STANDARD"}, Status: serviceStatusRunning},
		})
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1", "", runtime.Info{}, nil)

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "db"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if vr.Type != "managed" {
		t.Errorf("Type = %q, want managed", vr.Type)
	}
	if vr.Status != "healthy" {
		t.Errorf("Status = %q, want healthy", vr.Status)
	}
	if len(vr.Checks) != 1 {
		t.Errorf("Checks count = %d, want 1", len(vr.Checks))
	}
}

func TestVerifyTool_RuntimeActive(t *testing.T) {
	t.Parallel()

	// ACTIVE is the real status returned by Zerops API for running services.
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusActive},
		})
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1", "", runtime.Info{}, nil)

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "app"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if vr.Type != "runtime" {
		t.Errorf("Type = %q, want runtime", vr.Type)
	}
	if vr.Status != "healthy" {
		t.Errorf("Status = %q, want healthy (ACTIVE should be accepted)", vr.Status)
	}
	// service_running must pass for ACTIVE status.
	for _, c := range vr.Checks {
		if c.Name == "service_running" && c.Status != "pass" {
			t.Errorf("service_running = %q, want pass for ACTIVE status", c.Status)
		}
	}
}

func TestVerifyTool_NotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1", "", runtime.Info{}, nil)

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "nonexistent"})

	if !result.IsError {
		t.Error("expected IsError for nonexistent service")
	}
}

func TestVerifyTool_GracefulLogError(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusRunning},
		}).
		WithError("GetProjectLog", fmt.Errorf("log backend down"))
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1", "", runtime.Info{}, nil)

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "app"})

	if result.IsError {
		t.Fatalf("unexpected error: %s — log errors should be graceful", getTextContent(t, result))
	}

	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	// Log checks should be skip, not fail or crash.
	for _, c := range vr.Checks {
		if c.Name == "error_logs" {
			if c.Status != "skip" {
				t.Errorf("Check %q: status = %q, want skip", c.Name, c.Status)
			}
		}
	}
}

func TestVerifyTool_BatchMode(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusRunning, Ports: []platform.Port{{Port: 3000}}},
			{ID: "svc-2", Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16", ServiceStackTypeCategoryName: "STANDARD"}, Status: serviceStatusRunning},
		})
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1", "", runtime.Info{}, nil)

	// Call with empty serviceHostname → batch mode.
	result := callTool(t, srv, "zerops_verify", map[string]any{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var vr ops.VerifyAllResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(vr.Services) != 2 {
		t.Errorf("Services count = %d, want 2", len(vr.Services))
	}
	if vr.Status != "degraded" {
		t.Errorf("Status = %q, want degraded", vr.Status)
	}
}

func TestVerifyTool_SingleMode(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusRunning, Ports: []platform.Port{{Port: 3000}}},
			{ID: "svc-2", Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16", ServiceStackTypeCategoryName: "STANDARD"}, Status: serviceStatusRunning},
		})
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1", "", runtime.Info{}, nil)

	// Call with serviceHostname → single mode, returns VerifyResult.
	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "app"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if vr.Hostname != "app" {
		t.Errorf("Hostname = %q, want app", vr.Hostname)
	}
	if vr.Type != "runtime" {
		t.Errorf("Type = %q, want runtime", vr.Type)
	}
}

// P7: verify response includes autoCloseProgress so the agent sees how
// the call advanced the work session — either via in-flight progress
// (status=open + progress.ready/total) or by triggering auto-close
// (status=auto-closed + closedAt + closeReason). Without this the agent
// can't tell verify apart from a plain curl probe and defaults to curl —
// the exact pattern the fizzy log showed. F5 closure: signal lives on
// workSessionState now, not a top-level autoCloseProgress field.
func TestVerifyTool_ReportsLifecycleState(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	// Seed a work session with two services, one already deploy+verified,
	// one awaiting verify. Verifying the second advances ready 1→2.
	ws := workflow.NewWorkSession("proj-1", string(workflow.EnvContainer), "scope demo", []string{"app", "worker"})
	ws.Deploys = map[string][]workflow.DeployAttempt{
		"app":    {{AttemptedAt: now, SucceededAt: now}},
		"worker": {{AttemptedAt: now, SucceededAt: now}},
	}
	ws.Verifies = map[string][]workflow.VerifyAttempt{
		"app": {{AttemptedAt: now, PassedAt: now, Passed: true}},
	}
	if err := workflow.SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-worker", Name: "worker", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusRunning},
		})
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1", dir, runtime.Info{}, nil)

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "worker"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	// Verifying the worker is the LAST in-scope service to land green —
	// auto-close fires and the session terminates as part of this call.
	// Response carries status=auto-closed + closedAt + closeReason so the
	// agent knows the lifecycle terminated mid-call (no extra round-trip
	// through action="status" needed).
	for _, needle := range []string{`"workSessionState"`, `"status":"auto-closed"`, `"closedAt"`, `"closeReason":"auto-complete"`} {
		if !contains(text, needle) {
			t.Errorf("response missing %q:\n%s", needle, text)
		}
	}
}

// TestVerifyTool_PassesIntentFromMeta pins that the tool layer reads the
// service's persisted public-access intent (ServiceMeta.PublicAccessFor,
// §8 O3 PA-4) and passes it into ops.VerifyWithMeta: an intent=none host
// with subdomain access off gets a SKIPPED http_public check, never the
// zerops_subdomain-enable Recovery the old single-rule behavior always
// emitted for an off subdomain.
func TestVerifyTool_PassesIntentFromMeta(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "app",
		BootstrapSession: "s", BootstrappedAt: "2026-06-01",
		PublicAccess: map[string]topology.PublicAccessRecord{
			"app": {Intent: topology.PublicAccessNone},
		},
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusRunning, SubdomainAccess: false, Ports: []platform.Port{{Port: 3000}}},
		})
	fetcher := platform.NewMockLogFetcher()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1", dir, runtime.Info{}, nil)

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "app"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	var found bool
	for _, c := range vr.Checks {
		if c.Name != "http_public" {
			continue
		}
		found = true
		if c.Status != "skip" {
			t.Errorf("http_public status = %q, want skip (intent=none)", c.Status)
		}
		if c.Recovery != nil {
			t.Errorf("http_public.Recovery = %+v, want nil (intent=none must never emit the subdomain-enable recovery)", c.Recovery)
		}
	}
	if !found {
		t.Fatalf("http_public check not found in %+v", vr.Checks)
	}
}

// TestDeferredStartDurabilityNote pins RC-A′: a passing verify on a dev-mode
// dynamic runtime annotates the transience; durable runtimes / non-healthy
// results get no note.
func TestDeferredStartDurabilityNote(t *testing.T) {
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeDev,
		BootstrapSession: "s", BootstrappedAt: "2026-06-01",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "web", Mode: topology.PlanModeStandard, StageHostname: "webstage",
		BootstrapSession: "s", BootstrappedAt: "2026-06-01",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	cases := []struct {
		name    string
		host    string
		result  *ops.VerifyResult
		wantHit bool
	}{
		{"dev-mode dynamic healthy → note", "appdev", &ops.VerifyResult{Hostname: "appdev", TypeVersion: "ubuntu/bun@1.3.9", Status: ops.StatusHealthy}, true},
		{"php-nginx standard healthy → no note", "web", &ops.VerifyResult{Hostname: "web", TypeVersion: "php-nginx@8.4", Status: ops.StatusHealthy}, false},
		{"dev-mode dynamic unhealthy → no note", "appdev", &ops.VerifyResult{Hostname: "appdev", TypeVersion: "ubuntu/bun@1.3.9", Status: ops.StatusUnhealthy}, false},
		{"no meta → no note", "ghost", &ops.VerifyResult{Hostname: "ghost", TypeVersion: "ubuntu/bun@1.3.9", Status: ops.StatusHealthy}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			note := deferredStartDurabilityNote(dir, tc.host, tc.result)
			if tc.wantHit && note == "" {
				t.Fatalf("expected durability note, got empty")
			}
			if !tc.wantHit && note != "" {
				t.Fatalf("expected no note, got: %s", note)
			}
		})
	}
}

// TestVerifyTool_DevServerRunning_ListenerTrue_InternalProbed pins §8 O3
// PA-4 / O4: a dev-mode dynamic runtime whose dev server IS running (the
// pidfile liveness check ops.DevServerRunning reports alive) has a real
// listener — http_internal probes instead of skipping with the
// "start it with zerops_dev_server" deferred-start message.
func TestVerifyTool_DevServerRunning_ListenerTrue_InternalProbed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeDev,
		BootstrapSession: "s", BootstrappedAt: "2026-06-01",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/bun@1.3.9", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusRunning, Ports: []platform.Port{{Port: 3000}}},
		})
	fetcher := platform.NewMockLogFetcher()
	ssh := &scriptSSH{queue: []scriptStep{{output: "alive"}}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1", dir, runtime.Info{}, ssh)

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "appdev"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	var found bool
	for _, c := range vr.Checks {
		if c.Name != "http_internal" {
			continue
		}
		found = true
		if c.Status == "skip" && contains(c.Detail, "start it with zerops_dev_server") {
			t.Errorf("http_internal = %+v, want a real probe (dev server is running, not the deferred-start skip)", c)
		}
	}
	if !found {
		t.Fatalf("http_internal check not found in %+v", vr.Checks)
	}
}

// TestVerifyTool_DevServerStopped_StaysDeferred pins the complementary
// case: the dev server is NOT running (liveness check reports "dead") —
// http_internal stays the deferred-start skip, and no probe is attempted.
func TestVerifyTool_DevServerStopped_StaysDeferred(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeDev,
		BootstrapSession: "s", BootstrappedAt: "2026-06-01",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/bun@1.3.9", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusRunning, Ports: []platform.Port{{Port: 3000}}},
		})
	fetcher := platform.NewMockLogFetcher()
	ssh := &scriptSSH{queue: []scriptStep{{output: "dead"}}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1", dir, runtime.Info{}, ssh)

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "appdev"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	var found bool
	for _, c := range vr.Checks {
		if c.Name != "http_internal" {
			continue
		}
		found = true
		if c.Status != "skip" || !contains(c.Detail, "start it with zerops_dev_server") {
			t.Errorf("http_internal = %+v, want skip with the deferred-start message (dev server not running)", c)
		}
	}
	if !found {
		t.Fatalf("http_internal check not found in %+v", vr.Checks)
	}
}

// TestVerifyTool_DevServerStatusError_FallsBackToStatic pins the failure
// mode: the SSH liveness read errors (no SSH deployer, unreachable
// container) — verify must not fail because of it, and the classification
// falls back to the static (mode, class) DeferredStart, same as "stopped".
func TestVerifyTool_DevServerStatusError_FallsBackToStatic(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeDev,
		BootstrapSession: "s", BootstrappedAt: "2026-06-01",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/bun@1.3.9", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusRunning, Ports: []platform.Port{{Port: 3000}}},
		})
	fetcher := platform.NewMockLogFetcher()
	ssh := &scriptSSH{queue: []scriptStep{{err: fmt.Errorf("ssh: connection refused")}}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, fetcher, "proj-1", dir, runtime.Info{}, ssh)

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "appdev"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if contains(text, "warning") {
		t.Errorf("response carries an unexpected warning field on SSH liveness failure:\n%s", text)
	}

	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(text), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	var found bool
	for _, c := range vr.Checks {
		if c.Name != "http_internal" {
			continue
		}
		found = true
		if c.Status != "skip" || !contains(c.Detail, "start it with zerops_dev_server") {
			t.Errorf("http_internal = %+v, want skip with the deferred-start message (SSH error falls back to static)", c)
		}
	}
	if !found {
		t.Fatalf("http_internal check not found in %+v", vr.Checks)
	}
}
