// Tests for: zerops_verify — MCP tool handler.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
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

// verify marked a healthy worker stage degraded on "subdomain access not
// enabled": the pair's dev half serves HTTP, the worker declares no port. A
// service that declares no port gets no HTTP check, so nothing about HTTP
// degrades it.
func TestVerifyTool_AServiceThatDeclaresNoPortIsNotDegradedOnHTTP(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "difydev", Mode: topology.ModeDev,
		BootstrapSession: "s", BootstrappedAt: "2026-10-16",
		PrimarySetupName: "dev",
		Stages:           []topology.StageRef{{Hostname: "apistage", SetupName: "api"}, {Hostname: "workerstage", SetupName: "worker"}},
		ServesHTTPByHost: map[string]bool{"difydev": true},
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-w", Name: "workerstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/python@3.14", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusRunning, SubdomainAccess: false},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, platform.NewMockLogFetcher(), "proj-1", dir, runtime.Info{}, nil)

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "workerstage"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}
	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if vr.Status != ops.StatusHealthy {
		t.Errorf("status = %q, want %q; checks: %+v", vr.Status, ops.StatusHealthy, vr.Checks)
	}
	for _, c := range vr.Checks {
		if c.Name == "http_public" || c.Name == "http_internal" {
			t.Errorf("a service that declares no port got %s %s: %q", c.Name, c.Status, c.Detail)
		}
	}
}

// A pair recorded one "serves HTTP" for all its hosts, so the last deploy won:
// a worker stage deployed after the API stage turned the API stage into a
// worker, and a broken API read healthy with no HTTP check at all.
func TestVerifyTool_AStageThatServesHTTPKeepsItsHTTPCheckAfterAPortlessSiblingStageDeploys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scaffoldServiceYaml(t, dir, "difydev", `zerops:
  - setup: api
    build:
      base: python@3.14
      deployFiles: [.]
    run:
      ports:
        - port: 5001
          httpSupport: true
      start: gunicorn app:app
  - setup: worker
    build:
      base: python@3.14
      deployFiles: [.]
    run:
      start: celery worker
`)
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "difydev", Mode: topology.ModeStandard,
		BootstrapSession: "s", BootstrappedAt: "2026-10-16",
		Stages: []topology.StageRef{{Hostname: "apistage", SetupName: "api"}, {Hostname: "workerstage", SetupName: "worker"}},
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	for _, stage := range []struct{ host, setup string }{{"apistage", "api"}, {"workerstage", "worker"}} {
		if _, result, err := deployPreFlight(context.Background(), platform.NewMock(), "proj-1", stateDir, "difydev", stage.host, stage.setup, "", true); err != nil || result == nil || !result.Passed {
			t.Fatalf("deployPreFlight %s: err=%v result=%+v", stage.host, err, result)
		}
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-a", Name: "apistage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/python@3.14", ServiceStackTypeCategoryName: "USER"}, Status: platform.ServiceStatusStopped, Ports: []platform.Port{{Port: 5001}}},
		}).
		WithLogAccess(&platform.LogAccess{URL: "http://logs.test"})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, platform.NewMockLogFetcher(), "proj-1", stateDir, runtime.Info{}, nil)

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "apistage"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}
	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if vr.RuntimeClass != "dynamic" {
		t.Errorf("runtimeClass = %q, want %q (%s)", vr.RuntimeClass, "dynamic", vr.RuntimeClassification)
	}
	var httpChecks []string
	for _, c := range vr.Checks {
		if c.Name == "http_public" || c.Name == "http_internal" {
			httpChecks = append(httpChecks, c.Name)
		}
	}
	if len(httpChecks) != 2 {
		t.Errorf("http checks = %v, want http_internal and http_public; checks: %+v", httpChecks, vr.Checks)
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
		Hostname: "web", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "webstage"}},
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
			if tc.wantHit && (strings.Contains(note, "502s after") || !strings.Contains(note, "restart or redeploy") || !strings.Contains(note, "crash")) {
				t.Errorf("zcp starts a kept dev server again after a restart or redeploy; the note says a crash leaves it down: %s", note)
			}
			if !tc.wantHit && note != "" {
				t.Fatalf("expected no note, got: %s", note)
			}
		})
	}
}

// TestVerifyTool_DevServerRunning_ListenerTrue_InternalProbed pins §8 O3
// PA-4 / O4: a dev-mode dynamic runtime whose dev server IS running (its
// supervisor runs a declared server with a port) has a real
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
	ssh := devservertest.NewSupervised(`{"version":1,"servers":[{"name":"appdev","port":3000,"command":"bun index.ts","cwd":"."}]}`)

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
// case: the dev server is NOT running (declared, but the supervisor does
// not run it) —
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
	ssh := devservertest.NewSupervised("")
	ssh.Declaration = `{"version":1,"servers":[{"name":"appdev","port":3000,"command":"bun index.ts","cwd":"."}]}`

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
// mode: the supervisor read errors (no SSH deployer, unreachable
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
	ssh := &stubSSH{err: fmt.Errorf("ssh: connection refused")}

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

// TestVerifyTool_PushSourceRedirect: a verify of a git-push pair's dev half
// goes to its stage only once the stage runs a deploy, and then says which
// service it checked; a stage that never had one leaves the dev half checked.
func TestVerifyTool_PushSourceRedirect(t *testing.T) {
	t.Parallel()
	deployed := &platform.ActiveAppVersionDigest{ID: "av-1", Source: "CLI", Built: true}
	tests := []struct {
		name        string
		stageStatus string
		stageCode   *platform.ActiveAppVersionDigest
		wantHost    string
		wantNote    string
	}{
		{name: "the stage never deployed: the dev half is checked", stageStatus: "READY_TO_DEPLOY", wantHost: "appdev"},
		{name: "the stage runs a deploy: the stage is checked, and the answer says so", stageStatus: serviceStatusRunning, stageCode: deployed,
			wantHost: "appstage", wantNote: `checked "appstage"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
				Hostname: "appdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "appstage"}},
				CloseDeployMode: topology.CloseModeGitPush, GitPushState: topology.GitPushConfigured,
				BootstrapSession: "s", BootstrappedAt: "2026-06-01", FirstDeployedAt: "2026-06-01",
			}); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}
			nodeType := platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}
			mock := platform.NewMock().WithServices([]platform.ServiceStack{
				{ID: "svc-dev", Name: "appdev", ServiceStackTypeInfo: nodeType, Status: serviceStatusRunning, ActiveAppVersion: deployed},
				{ID: "svc-stage", Name: "appstage", ServiceStackTypeInfo: nodeType, Status: tt.stageStatus, ActiveAppVersion: tt.stageCode},
			})
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterVerify(srv, mock, platform.NewMockLogFetcher(), "proj-1", dir, runtime.Info{}, nil)

			result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "appdev"})
			if result.IsError {
				t.Fatalf("unexpected error: %s", getTextContent(t, result))
			}
			var resp struct {
				Hostname string `json:"hostname"`
				Note     string `json:"note"`
			}
			if err := json.Unmarshal([]byte(getTextContent(t, result)), &resp); err != nil {
				t.Fatalf("parse: %v", err)
			}
			if resp.Hostname != tt.wantHost {
				t.Errorf("checked %q, want %q", resp.Hostname, tt.wantHost)
			}
			if tt.wantNote == "" && strings.Contains(resp.Note, "push source") {
				t.Errorf("note = %q, want no redirect", resp.Note)
			}
			if !strings.Contains(resp.Note, tt.wantNote) {
				t.Errorf("note = %q, want it to say %q", resp.Note, tt.wantNote)
			}
		})
	}
}

// TestVerifyPublicAccess_ADevServerServesTheDevHalf: both verify paths tell
// ops which runtimes a dev server serves — a dev-mode runtime, the dev half
// of a standard pair, whatever its dev server's live state — so their HTTP
// probes give a page its first compile; a stage or a simple runtime starts
// its own app and gets the one wait.
func TestVerifyPublicAccess_ADevServerServesTheDevHalf(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, meta := range []*workflow.ServiceMeta{
		{Hostname: "appdev", Mode: topology.PlanModeDev, BootstrapSession: "s", BootstrappedAt: "2026-06-01"},
		{Hostname: "shopdev", Stages: []topology.StageRef{{Hostname: "shopstage"}}, Mode: topology.PlanModeStandard, BootstrapSession: "s", BootstrappedAt: "2026-06-01"},
		{Hostname: "api", Mode: topology.PlanModeSimple, BootstrapSession: "s", BootstrappedAt: "2026-06-01"},
	} {
		if err := workflow.WriteServiceMeta(dir, meta); err != nil {
			t.Fatalf("WriteServiceMeta: %v", err)
		}
	}
	runtimeType := platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}
	hosts := []string{"appdev", "shopdev", "shopstage", "api"}
	services := make([]platform.ServiceStack, 0, len(hosts))
	for _, h := range hosts {
		services = append(services, platform.ServiceStack{ID: "svc-" + h, Name: h, ServiceStackTypeInfo: runtimeType, Status: serviceStatusRunning})
	}
	mock := platform.NewMock().WithServices(services)
	all := publicAccessResolver(dir)
	for host, want := range map[string]bool{"appdev": true, "shopdev": true, "shopstage": false, "api": false} {
		if got := all(host).DevServer; got != want {
			t.Errorf("all services: %s DevServer = %v, want %v", host, got, want)
		}
		if got := publicAccessInputForHost(context.Background(), mock, nil, "proj-1", dir, host).DevServer; got != want {
			t.Errorf("one service: %s DevServer = %v, want %v", host, got, want)
		}
	}
}

// TestVerifyTool_EveryDeclaredPort (TEST-PLAN E1 #11): verify checks every
// declared port of a dev service — one http_internal check per dev server
// with a port, probed while its supervisor runs it, skipped with the way to
// start it while stopped, failed while crashed; a server without a port
// has none. A dev service
// with one server keeps its one unnamed check.
func TestVerifyTool_EveryDeclaredPort(t *testing.T) {
	t.Parallel()
	const decl = `{"version":1,"servers":[` +
		`{"name":"web","port":3000,"command":"pnpm dev","cwd":"apps/web"},` +
		`{"name":"api","port":8080,"command":"pnpm dev","cwd":"apps/api"},` +
		`{"name":"worker","command":"bun worker.ts","cwd":"apps/worker"}]}`
	tests := []struct {
		name  string
		decl  string
		setup func(c *devservertest.Container)
		want  map[string]string // server → check status; "" key = the one unnamed check
	}{
		{name: "two servers with ports run", decl: decl, want: map[string]string{"web": "probed", "api": "probed"}},
		{name: "one of them is stopped", decl: decl, setup: func(c *devservertest.Container) {
			c.Servers["api"].Status = "stopped"
		}, want: map[string]string{"web": "probed", "api": "skip"}},
		{name: "one of them crashed", decl: decl, setup: func(c *devservertest.Container) {
			c.Servers["web"].Status = "crashed"
		}, want: map[string]string{"web": "crashed", "api": "probed"}},
		{name: "one server", decl: `{"version":1,"servers":[{"name":"appdev","port":3000,"command":"bun index.ts","cwd":"."}]}`,
			want: map[string]string{"": "probed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
				Hostname: "appdev", Mode: topology.PlanModeDev, BootstrapSession: "s", BootstrappedAt: "2026-10-14",
			}); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}
			mock := platform.NewMock().WithServices([]platform.ServiceStack{
				{ID: "svc-1", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
					Status: serviceStatusRunning, Ports: []platform.Port{{Port: 3000, HTTPSupport: true}, {Port: 8080, HTTPSupport: true}}},
			})
			c := devservertest.NewSupervised(tt.decl)
			if tt.setup != nil {
				tt.setup(c)
			}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterVerify(srv, mock, platform.NewMockLogFetcher(), "proj-1", dir, runtime.Info{}, c)
			result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "appdev"})
			if result.IsError {
				t.Fatalf("verify: %s", getTextContent(t, result))
			}
			var vr ops.VerifyResult
			if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := map[string]string{}
			for _, ch := range vr.Checks {
				if ch.Name != "http_internal" {
					continue
				}
				// The tool probes over the network, so a probed check's
				// outcome is the ops tests' to pin; this one pins which
				// servers verify probes, skips or fails without probing.
				switch {
				case ch.Status == ops.CheckSkip:
					got[ch.Server] = "skip"
				case strings.Contains(ch.Detail, "crashed"):
					got[ch.Server] = "crashed"
				default:
					got[ch.Server] = "probed"
				}
				if ch.Server != "" && ch.Port != map[string]int{"web": 3000, "api": 8080}[ch.Server] {
					t.Errorf("check %+v names the wrong port", ch)
				}
				if ch.Server == "api" && ch.Status == ops.CheckSkip && !strings.Contains(ch.Detail, `start it with zerops_dev_server action=start hostname="appdev" name="api"`) {
					t.Errorf("stopped server's check = %q, want the way to start it", ch.Detail)
				}
			}
			if len(got) != len(tt.want) {
				t.Fatalf("http_internal checks = %v, want %v", got, tt.want)
			}
			for server, want := range tt.want {
				if got[server] != want {
					t.Errorf("server %q: %s, want %s (all: %v)", server, got[server], want, got)
				}
			}
		})
	}
}

// TestVerifyTool_APartWithoutADevServer_NamesTheStartCall: verifying the dev
// service of a pair whose stages build the app and an added worker, while the
// supervisor declares only the app's server, says that the worker runs no dev
// server and gives the exact start call — and never fails the verify. The same
// repo with one stage, or outside a container, says nothing (the single-part
// reply stays as it is).
func TestVerifyTool_APartWithoutADevServer_NamesTheStartCall(t *testing.T) {
	t.Parallel()
	const appAndWorker = "zerops:\n  - setup: dev\n    run:\n      start: zcp-devsup run\n  - setup: prod\n    run:\n      start: bun index.ts\n  - setup: worker\n    run:\n      start: bun worker.ts\n"
	const startWorker = `zerops_dev_server action=start hostname="appdev" name="worker"`
	tests := []struct {
		name        string
		stages      []topology.StageRef
		inContainer bool
		all         bool
		wantNote    bool
	}{
		{name: "an added worker without a dev server is named with its start call", inContainer: true, wantNote: true,
			stages: []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}, {Hostname: "workerstage", SetupName: "worker"}}},
		{name: "verifying every service names the worker too", inContainer: true, all: true, wantNote: true,
			stages: []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}, {Hostname: "workerstage", SetupName: "worker"}}},
		{name: "a pair with one stage says nothing", inContainer: true,
			stages: []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}}},
		{name: "outside a container says nothing",
			stages: []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}, {Hostname: "workerstage", SetupName: "worker"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
				Hostname: "appdev", Mode: topology.PlanModeStandard, PrimarySetupName: "dev", Stages: tt.stages,
				CloseDeployMode: topology.CloseModeAuto, BootstrapSession: "s", BootstrappedAt: "2026-06-01",
			}); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/bun@1.3.9", ServiceStackTypeCategoryName: "USER"}, Status: serviceStatusRunning, Ports: []platform.Port{{Port: 3000}}},
				})
			ssh := devservertest.NewSupervised(`{"version":1,"servers":[{"name":"appdev","port":3000,"command":"bun --watch index.ts","cwd":"."}]}`)
			ssh.YAMLFile, ssh.YAML = "zerops.yml", appAndWorker

			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterVerify(srv, mock, platform.NewMockLogFetcher(), "proj-1", dir, runtime.Info{InContainer: tt.inContainer}, ssh)
			args := map[string]any{"serviceHostname": "appdev"}
			if tt.all {
				args = map[string]any{}
			}
			result := callTool(t, srv, "zerops_verify", args)
			if result.IsError {
				t.Fatalf("unexpected error: %s", getTextContent(t, result))
			}
			var resp struct {
				Note string `json:"note"`
			}
			if err := json.Unmarshal([]byte(getTextContent(t, result)), &resp); err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !tt.wantNote {
				if strings.Contains(resp.Note, "zerops_dev_server") {
					t.Errorf("note = %q, want no dev-server fact", resp.Note)
				}
				return
			}
			if !strings.Contains(resp.Note, `part "worker"`) || !strings.Contains(resp.Note, startWorker) {
				t.Errorf("note = %q, want the worker part named with %s", resp.Note, startWorker)
			}
			if strings.Contains(resp.Note, `"prod"`) {
				t.Errorf("note = %q, names the app part its server under the dev hostname serves", resp.Note)
			}
		})
	}
}

// A Swarm pool is a service of up to ten containers its orchestrator hands
// out: an HTTP probe addressed to the service hostname reaches one random
// container, so verify reports the pool and its Box by service_running alone
// and never probes either as one runtime (spec-workflows.md §8 PS-3). Outside
// the project a Box adds only its orchestrator check, skipped: the workspace
// API answers only inside the project (PS-9).
func TestVerify_Pool_NotProbedAsSingleRuntime(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		svc      platform.ServiceStack
		wantType string
	}{
		{
			name: "pool_with_ports_and_subdomain",
			svc: platform.ServiceStack{
				ID: "svc-1", Name: "swarmpool", Status: serviceStatusRunning, SubdomainAccess: true,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"},
				Ports:                []platform.Port{{Port: 3000}},
			},
			wantType: "pool",
		},
		{
			name: "pool_vm_composite",
			svc: platform.ServiceStack{
				ID: "svc-1", Name: "swarmpool", Status: serviceStatusRunning,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "alpine/swarm-vm@1", ServiceStackTypeCategoryName: "USER"},
				Ports:                []platform.Port{{Port: 3000}},
			},
			wantType: "pool",
		},
		{
			name: "box",
			svc: platform.ServiceStack{
				ID: "svc-1", Name: "box", Status: serviceStatusRunning,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/zbox@1", ServiceStackTypeCategoryName: "STANDARD"},
			},
			wantType: "sandbox",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().WithServices([]platform.ServiceStack{tc.svc})
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterVerify(srv, mock, platform.NewMockLogFetcher(), "proj-1", "", runtime.Info{}, nil)

			result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": tc.svc.Name})
			if result.IsError {
				t.Fatalf("unexpected error: %s", getTextContent(t, result))
			}
			var vr ops.VerifyResult
			if err := json.Unmarshal([]byte(getTextContent(t, result)), &vr); err != nil {
				t.Fatalf("failed to parse result: %v", err)
			}
			if vr.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", vr.Type, tc.wantType)
			}
			if vr.Status != ops.StatusHealthy {
				t.Errorf("Status = %q, want healthy; checks: %+v", vr.Status, vr.Checks)
			}
			wantChecks := 1
			if tc.wantType == "sandbox" {
				wantChecks = 2
				if last := vr.Checks[len(vr.Checks)-1]; last.Name != "orchestrator" || last.Status != ops.CheckSkip {
					t.Errorf("checks = %+v, want the Box's orchestrator check skipped outside the project", vr.Checks)
				}
			}
			if len(vr.Checks) != wantChecks || vr.Checks[0].Status != ops.CheckPass {
				t.Errorf("checks = %+v, want the passing service_running check (no http_internal/http_public/error_logs probe of the service hostname)", vr.Checks)
			}
		})
	}
}

// TestVerify_Pool_OrchestratorCheck_InContainer: in a container, verify of a
// pool also asks its orchestrator — with the bearer zcp finds — and reports
// how many containers are leased (it is never probed by hostname, PS-3);
// without the option (local, tests) the pool keeps service_running alone.
func TestVerify_Pool_OrchestratorCheck_InContainer(t *testing.T) {
	t.Parallel()

	var gotBearer string
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBearer = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"c-1","number":1,"lockState":"reserved"},{"id":"c-2","number":2,"lockState":""}]`))
	}))
	t.Cleanup(hs.Close)
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).
		WithServices([]platform.ServiceStack{{ID: "svc-pool", Name: "devpool", Status: serviceStatusRunning,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}}}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: "orch-token-1", Sensitive: true}})
	factory := func(_, token string) ops.SwarmAPI { return platform.NewSwarmClient(hs.URL, token, hs.Client()) }
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, platform.NewMockLogFetcher(), "proj-1", t.TempDir(), runtime.Info{InContainer: true, ServiceName: "zcp"}, nil, WithPoolOrchestrators(factory))

	text := getTextContent(t, callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "devpool"}))
	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(text), &vr); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	var orch *ops.CheckResult
	for i := range vr.Checks {
		if vr.Checks[i].Name == "orchestrator" {
			orch = &vr.Checks[i]
		}
	}
	if orch == nil || orch.Status != ops.CheckPass || !strings.Contains(orch.Detail, "1 leased") {
		t.Fatalf("checks = %+v, want an orchestrator pass naming 1 leased", vr.Checks)
	}
	if gotBearer != "Bearer orch-token-1" {
		t.Errorf("orchestrator bearer = %q, want the pool's API_TOKEN", gotBearer)
	}
	if strings.Contains(text, "orch-token-1") {
		t.Errorf("the verify result leaks the pool token: %s", text)
	}
}

// verifyBoxHTTP gives zerops_verify d for its Box orchestrator checks.
func verifyBoxHTTP(d ops.HTTPDoer) VerifyOption { return func(c *verifyConfig) { c.boxHTTP = d } }

// boxPingAnswer answers every request with the Box's ping and records the
// requests.
type boxPingAnswer struct {
	urls []string
	auth []string
}

func (b *boxPingAnswer) Do(req *http.Request) (*http.Response, error) {
	b.urls = append(b.urls, req.URL.String())
	b.auth = append(b.auth, req.Header.Get("Authorization"))
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{},
		Body: io.NopCloser(strings.NewReader(`{"time":"2026-10-24T10:00:00Z","version":"v1.4.2"}`))}, nil
}

// TestVerify_Box_OrchestratorCheck — verify of a Zerops Box asks its
// workspace API's keyless GET /api/rest/ping at http://<box>:8080 when zcp
// runs in the project's container, by hostname and in the all-services scan;
// outside the project it says the API answers only inside, asking nothing
// (spec-workflows.md §8 PS-9).
func TestVerify_Box_OrchestratorCheck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		rt          runtime.Info
		all         bool
		wantStatus  string
		wantDetail  string
		wantRequest bool
	}{
		{"in the project, by hostname", runtime.Info{InContainer: true, ServiceName: "zcp"}, false, ops.CheckPass, "v1.4.2", true},
		{"in the project, every service", runtime.Info{InContainer: true, ServiceName: "zcp"}, true, ops.CheckPass, "v1.4.2", true},
		{"outside the project", runtime.Info{}, false, ops.CheckSkip, "only inside the project", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().
				WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).
				WithServices([]platform.ServiceStack{{ID: "svc-box", Name: "box", Status: serviceStatusRunning,
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/zbox@1", ServiceStackTypeCategoryName: "STANDARD"}}})
			doer := &boxPingAnswer{}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterVerify(srv, mock, platform.NewMockLogFetcher(), "proj-1", t.TempDir(), tt.rt, nil, verifyBoxHTTP(doer))

			args := map[string]any{"serviceHostname": "box"}
			if tt.all {
				args = map[string]any{}
			}
			text := getTextContent(t, callTool(t, srv, "zerops_verify", args))
			var checks []ops.CheckResult
			if tt.all {
				var all ops.VerifyAllResult
				if err := json.Unmarshal([]byte(text), &all); err != nil || len(all.Services) != 1 {
					t.Fatalf("parse: %v\n%s", err, text)
				}
				checks = all.Services[0].Checks
			} else {
				var vr ops.VerifyResult
				if err := json.Unmarshal([]byte(text), &vr); err != nil {
					t.Fatalf("parse: %v\n%s", err, text)
				}
				checks = vr.Checks
				if vr.Status != ops.StatusHealthy {
					t.Errorf("status = %q, want healthy: %+v", vr.Status, checks)
				}
			}
			var orch *ops.CheckResult
			for i := range checks {
				if checks[i].Name == "orchestrator" {
					orch = &checks[i]
				}
			}
			if orch == nil || orch.Status != tt.wantStatus || !strings.Contains(orch.Detail, tt.wantDetail) {
				t.Fatalf("checks = %+v, want an orchestrator %s naming %q", checks, tt.wantStatus, tt.wantDetail)
			}
			if asked := len(doer.urls) > 0; asked != tt.wantRequest {
				t.Fatalf("asked %v, want a request = %v", doer.urls, tt.wantRequest)
			}
			if tt.wantRequest && (doer.urls[0] != "http://box:8080/api/rest/ping" || doer.auth[0] != "") {
				t.Errorf("asked %q with Authorization %q, want the keyless http://box:8080/api/rest/ping", doer.urls[0], doer.auth[0])
			}
		})
	}
}
