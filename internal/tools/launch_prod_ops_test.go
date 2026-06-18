package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// F7 — bring-up management window. Per-call launchKey, never persisted;
// management ops run over the public REST surface via the admin client.

func seedProdOpsState(t *testing.T, stateDir string, status topology.LaunchProductionStatus) {
	t.Helper()
	state := &launchState{
		LaunchID:          generateLaunchID("src-proj", "myapp-prod"),
		SourceProjectID:   "src-proj",
		TargetProjectName: "myapp-prod",
		TargetProjectID:   "prod-proj-123",
		Status:            status,
	}
	if err := writeLaunchState(stateDir, state); err != nil {
		t.Fatalf("writeLaunchState: %v", err)
	}
}

func prodOpsAdminMock(t *testing.T) *platform.MockProjectAdminClient {
	t.Helper()
	m := platform.NewMockProjectAdminClient().WithServices([]platform.ServiceStack{
		{ID: "svc-app", Name: "app", Status: "ACTIVE"},
		{ID: "svc-db", Name: "db", Status: "ACTIVE"},
	})
	cleanup := setProjectAdminClientFactory(func(launchKey, _ string) (platform.ProjectAdminClient, error) {
		if launchKey == "" {
			t.Fatal("factory called with empty launchKey")
		}
		// The handler Close()s the client per call; the test factory hands
		// out the same capture-bearing mock, so reopen it (a real factory
		// constructs a fresh client every call).
		m.Closed = false
		return m, nil
	})
	t.Cleanup(cleanup)
	return m
}

// TestProdOps_RequiresLaunchKeyEveryCall pins P-LP-1 in the window: no
// key → no client construction, with the re-supply instruction.
func TestProdOps_RequiresLaunchKeyEveryCall(t *testing.T) {
	stateDir := t.TempDir()
	seedProdOpsState(t, stateDir, topology.LaunchStatusLaunched)

	result, _, _ := handleLaunchProdOps(context.Background(), "src-proj", nil, nil, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ProdOperation:         "status",
	}, stateDir, "")
	body := getTextContent(t, result)
	if !result.IsError {
		t.Fatal("missing launchKey must refuse")
	}
	if !strings.Contains(body, "never persisted") {
		t.Errorf("refusal must explain the per-call key model: %s", body)
	}
}

// TestProdOps_ThreadsAPIHostToFactory pins parity with reset/publish: the
// in-scope ZCP_API_HOST reaches the admin client factory. A non-default-host
// user's production project lives on that host; constructing the admin client
// against the default host (the pre-fix empty string) 404s every prod-ops op.
func TestProdOps_ThreadsAPIHostToFactory(t *testing.T) {
	stateDir := t.TempDir()
	seedProdOpsState(t, stateDir, topology.LaunchStatusLaunched)
	m := platform.NewMockProjectAdminClient().WithServices([]platform.ServiceStack{
		{ID: "svc-app", Name: "app", Status: "ACTIVE"},
	})
	var gotHost string
	cleanup := setProjectAdminClientFactory(func(launchKey, apiHost string) (platform.ProjectAdminClient, error) {
		gotHost = apiHost
		m.Closed = false
		return m, nil
	})
	t.Cleanup(cleanup)

	const wantHost = "api.app-fra1.zerops.io"
	result, _, _ := handleLaunchProdOps(context.Background(), "src-proj", nil, nil, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ProdOperation:         "status",
		LaunchKey:             "key-123",
	}, stateDir, wantHost)
	if result.IsError {
		t.Fatalf("status failed: %s", getTextContent(t, result))
	}
	if gotHost != wantHost {
		t.Errorf("apiHost not threaded to admin client factory: got %q want %q", gotHost, wantHost)
	}
}

// TestProdOps_StatusListsServicesAndDoneBoundary pins the read surface:
// status lists the prod services and renders the done boundary (launched
// + no pending pipeline → revoke-now guidance).
func TestProdOps_StatusListsServicesAndDoneBoundary(t *testing.T) {
	stateDir := t.TempDir()
	seedProdOpsState(t, stateDir, topology.LaunchStatusLaunched)
	prodOpsAdminMock(t)

	result, _, _ := handleLaunchProdOps(context.Background(), "src-proj", nil, nil, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ProdOperation:         "status",
		LaunchKey:             "key-123",
	}, stateDir, "")
	body := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("status failed: %s", body)
	}
	for _, want := range []string{`"hostname":"app"`, `"hostname":"db"`, `"doneBoundary"`, `"done":true`, "confirm-production"} {
		if !strings.Contains(body, want) {
			t.Errorf("status missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "key-123") {
		t.Error("launchKey leaked into the response")
	}
}

// TestProdOps_StatusSurfacesSubdomainURL pins the fix for the p3/p4 finding:
// after enable-subdomain, prod-ops status must surface each subdomain-enabled
// service's zerops.app URL so the agent never falls back to raw REST API calls
// (which re-surface the launch token).
//
// REALISTIC fixture (the v9.116.1 bug was a mock-vs-reality gap): GetProject
// returns the subdomain-host PREFIX only ("23b4", NOT a full dotted host —
// live-verified in TestE2E_ProdSubdomainURL). The region domain derives from the
// apiHost ("api.app-prg1.zerops.io" → "prg1.zerops.app"), NO env values (P-LP-5).
func TestProdOps_StatusSurfacesSubdomainURL(t *testing.T) {
	stateDir := t.TempDir()
	seedProdOpsState(t, stateDir, topology.LaunchStatusLaunched)

	m := platform.NewMockProjectAdminClient().
		WithProject(&platform.Project{ID: "prod-proj-123", Name: "myapp-prod", SubdomainHost: "23b4"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", Status: "ACTIVE", SubdomainAccess: true, Ports: []platform.Port{{Port: 3000, HTTPSupport: true, Scheme: "http"}}},
			{ID: "svc-core", Name: "core", Status: "ACTIVE"}, // no subdomain → no URL
		})
	cleanup := setProjectAdminClientFactory(func(launchKey, _ string) (platform.ProjectAdminClient, error) {
		m.Closed = false
		return m, nil
	})
	t.Cleanup(cleanup)

	result, _, _ := handleLaunchProdOps(context.Background(), "src-proj", nil, nil, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ProdOperation:         "status",
		LaunchKey:             "key-123",
	}, stateDir, "api.app-prg1.zerops.io")
	body := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("status failed: %s", body)
	}
	if !strings.Contains(body, "https://app-23b4-3000.prg1.zerops.app") {
		t.Errorf("status must surface the app subdomain URL: %s", body)
	}
	// A service without subdomain access must NOT get a fabricated URL.
	if strings.Contains(body, "core-23b4") {
		t.Errorf("non-subdomain service must not carry a URL: %s", body)
	}
}

// TestProdOps_DeleteServiceRequiresAck pins the destructive gate: first
// call refuses with wouldDestroy + prefilled retryCall; the ack-bearing
// call deletes.
func TestProdOps_DeleteServiceRequiresAck(t *testing.T) {
	stateDir := t.TempDir()
	seedProdOpsState(t, stateDir, topology.LaunchStatusLaunched)
	m := prodOpsAdminMock(t)

	input := WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ProdOperation:         "delete-service",
		TargetService:         "app",
		LaunchKey:             "key-123",
	}
	result, _, _ := handleLaunchProdOps(context.Background(), "src-proj", nil, nil, input, stateDir, "")
	body := getTextContent(t, result)
	if !strings.Contains(body, `"refused":true`) || !strings.Contains(body, `"wouldDestroy"`) {
		t.Fatalf("first delete call must refuse with wouldDestroy: %s", body)
	}
	if len(m.DeletedServiceIDs) != 0 {
		t.Fatal("refusal must not delete anything")
	}

	input.ConfirmDestructive = &DestructiveAck{
		Operation:           "prod-delete-service",
		AcknowledgedTargets: []string{"app"},
	}
	result, _, _ = handleLaunchProdOps(context.Background(), "src-proj", nil, nil, input, stateDir, "")
	body = getTextContent(t, result)
	if result.IsError {
		t.Fatalf("acked delete failed: %s", body)
	}
	if len(m.DeletedServiceIDs) != 1 || m.DeletedServiceIDs[0] != "svc-app" {
		t.Errorf("acked call must delete svc-app; got %v", m.DeletedServiceIDs)
	}
}

// TestProdOps_LifecycleTargetsProdService pins restart routing to the
// resolved prod service ID.
func TestProdOps_LifecycleTargetsProdService(t *testing.T) {
	stateDir := t.TempDir()
	seedProdOpsState(t, stateDir, topology.LaunchStatusLaunching)
	m := prodOpsAdminMock(t)

	result, _, _ := handleLaunchProdOps(context.Background(), "src-proj", nil, nil, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ProdOperation:         "restart",
		TargetService:         "db",
		LaunchKey:             "key-123",
	}, stateDir, "")
	if result.IsError {
		t.Fatalf("restart failed: %s", getTextContent(t, result))
	}
	if len(m.LifecycleCalls) != 1 || m.LifecycleCalls[0] != "restart:svc-db" {
		t.Errorf("restart must target svc-db; got %v", m.LifecycleCalls)
	}
}

// TestProdOps_EnableSubdomainTargetsProdService pins F4c: enable-subdomain
// turns on the zerops.app subdomain on the PROD project's service (closing the
// launch loop that P-PROD-2 + the source-bound zerops_subdomain left open).
func TestProdOps_EnableSubdomainTargetsProdService(t *testing.T) {
	stateDir := t.TempDir()
	seedProdOpsState(t, stateDir, topology.LaunchStatusLaunching)
	m := prodOpsAdminMock(t)

	result, _, _ := handleLaunchProdOps(context.Background(), "src-proj", nil, nil, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ProdOperation:         "enable-subdomain",
		TargetService:         "db",
		LaunchKey:             "key-123",
	}, stateDir, "")
	if result.IsError {
		t.Fatalf("enable-subdomain failed: %s", getTextContent(t, result))
	}
	if len(m.LifecycleCalls) != 1 || m.LifecycleCalls[0] != "enable-subdomain:svc-db" {
		t.Errorf("enable-subdomain must target svc-db via the prod admin client; got %v", m.LifecycleCalls)
	}
	if !strings.Contains(getTextContent(t, result), "enable-subdomain") {
		t.Errorf("response should name the prodOperation; got: %s", getTextContent(t, result))
	}
}
