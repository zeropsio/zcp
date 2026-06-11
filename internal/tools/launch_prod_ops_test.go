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
	for _, want := range []string{`"hostname":"app"`, `"hostname":"db"`, `"doneBoundary"`, `"done":true`, "Revoke the launch-window key NOW"} {
		if !strings.Contains(body, want) {
			t.Errorf("status missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "key-123") {
		t.Error("launchKey leaked into the response")
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
