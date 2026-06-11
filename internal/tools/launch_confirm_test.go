package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// T3 — confirm-production: the explicit, user-acked close of the launch
// window. Deleting the staged ZCP_LAUNCH_TOKEN secret is the physical
// enforcement ("never works with it again" by absence, not policy);
// WindowClosedAt is stamped for honest status only.

func seedConfirmState(t *testing.T, stateDir string, status topology.LaunchProductionStatus, closedAt time.Time) {
	t.Helper()
	state := &launchState{
		LaunchID:              generateLaunchID("src-proj", "myapp-prod"),
		SourceProjectID:       "src-proj",
		TargetProjectName:     "myapp-prod",
		TargetProjectID:       "prod-proj-123",
		TargetServiceHostname: "appdev",
		Status:                status,
		WindowClosedAt:        closedAt,
		RuntimeProds:          []launchRuntimeProd{{ProdHostname: "app", SetupName: "prod"}},
	}
	if err := writeLaunchState(stateDir, state); err != nil {
		t.Fatalf("writeLaunchState: %v", err)
	}
}

func confirmInput(acked bool) WorkflowInput {
	in := WorkflowInput{
		Action:                "confirm-production",
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
	}
	if acked {
		in.ConfirmFunctional = FlexBool(true)
	}
	return in
}

// TestConfirmProduction_DeletesStageAndStamps pins the close act: with
// the explicit confirmFunctional ack, the staged secret is DELETED
// (delete first), WindowClosedAt is stamped, and the response carries
// the regenerate recommendation with the dashboard pointer — without
// ever echoing the token value.
func TestConfirmProduction_DeletesStageAndStamps(t *testing.T) {
	// non-parallel: captureAdminFactory mutates the package-global factory.
	stateDir := t.TempDir()
	seedConfirmState(t, stateDir, topology.LaunchStatusLaunched, time.Time{})
	stageClient := stagedSourceClient()
	m := platform.NewMockProjectAdminClient().
		WithServices([]platform.ServiceStack{{ID: "svc-prod-app", Name: "app", Status: "ACTIVE"}}).
		WithIntegrationTokens([]platform.IntegrationTokenInfo{
			{ID: "tok-1", Name: "zcp-launch-myapp-prod", CanCreateProjects: true, ProjectIDs: []string{"prod-proj-123"}},
			{ID: "tok-2", Name: "unrelated", CanCreateProjects: false},
		})
	captured := captureAdminFactory(t, m)

	result, _, err := handleLaunchConfirmProduction(context.Background(), "src-proj", stageClient, confirmInput(true), stateDir, "")
	if err != nil {
		t.Fatalf("handleLaunchConfirmProduction: %v", err)
	}
	body := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("acked confirm must close the window, got error: %s", body)
	}
	if !strings.Contains(body, "window-closed") {
		t.Errorf("response must report window-closed: %s", body)
	}
	// Physical close: the staged secret is gone from the env store.
	if got := stagedTokenValue(t, stageClient, "svc-dev"); got != "" {
		t.Errorf("staged secret must be DELETED on confirm; still present with value %q", got)
	}
	// Stamp for honest status.
	updated, readErr := readLaunchState(stateDir, generateLaunchID("src-proj", "myapp-prod"))
	if readErr != nil {
		t.Fatalf("read state: %v", readErr)
	}
	if updated.WindowClosedAt.IsZero() {
		t.Error("WindowClosedAt must be stamped after the close")
	}
	// Regenerate note + dashboard pointer + matched token name.
	for _, want := range []string{"egenerat", "token-management", "zcp-launch-myapp-prod"} {
		if !strings.Contains(body, want) {
			t.Errorf("close response missing %q:\n%s", want, body)
		}
	}
	// The liveness read ran with the staged token; the value never leaks.
	if *captured != sentinelLaunchKey {
		t.Errorf("liveness/token-list read must use the staged token; factory got %q", *captured)
	}
	if strings.Contains(body, sentinelLaunchKey) {
		t.Errorf("close response leaks the token value:\n%s", body)
	}
}

// TestConfirmProduction_RequiresAck pins the consent gate: without
// confirmFunctional=true nothing is deleted or stamped — the response
// is a prompt carrying the prefilled retry call.
func TestConfirmProduction_RequiresAck(t *testing.T) {
	// non-parallel: captureAdminFactory mutates the package-global factory.
	stateDir := t.TempDir()
	seedConfirmState(t, stateDir, topology.LaunchStatusLaunched, time.Time{})
	stageClient := stagedSourceClient()
	m := platform.NewMockProjectAdminClient().
		WithServices([]platform.ServiceStack{{ID: "svc-prod-app", Name: "app", Status: "ACTIVE"}})
	captureAdminFactory(t, m)

	result, _, err := handleLaunchConfirmProduction(context.Background(), "src-proj", stageClient, confirmInput(false), stateDir, "")
	if err != nil {
		t.Fatalf("handleLaunchConfirmProduction: %v", err)
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, "confirm-required") || !strings.Contains(body, "confirmFunctional") {
		t.Errorf("unacked confirm must prompt with the prefilled retry call: %s", body)
	}
	if got := stagedTokenValue(t, stageClient, "svc-dev"); got == "" {
		t.Error("unacked confirm must NOT delete the staged secret")
	}
	updated, _ := readLaunchState(stateDir, generateLaunchID("src-proj", "myapp-prod"))
	if !updated.WindowClosedAt.IsZero() {
		t.Error("unacked confirm must NOT stamp WindowClosedAt")
	}
}

// TestConfirmProduction_RefusesBeforeLaunched pins the precondition:
// the close applies to a LAUNCHED project only.
func TestConfirmProduction_RefusesBeforeLaunched(t *testing.T) {
	stateDir := t.TempDir()
	seedConfirmState(t, stateDir, topology.LaunchStatusFailed, time.Time{})
	stageClient := stagedSourceClient()

	result, _, err := handleLaunchConfirmProduction(context.Background(), "src-proj", stageClient, confirmInput(true), stateDir, "")
	if err != nil {
		t.Fatalf("handleLaunchConfirmProduction: %v", err)
	}
	if !result.IsError {
		t.Fatal("confirm-production on a non-launched state must refuse")
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, "launched") {
		t.Errorf("refusal must name the launched precondition: %s", body)
	}
	if got := stagedTokenValue(t, stageClient, "svc-dev"); got == "" {
		t.Error("refusal must NOT delete the staged secret")
	}
}

// TestConfirmProduction_AlreadyClosed_Idempotent pins the re-call shape:
// a second confirm on a closed window reports the original close and
// mutates nothing (compaction-recovery friendly).
func TestConfirmProduction_AlreadyClosed_Idempotent(t *testing.T) {
	stateDir := t.TempDir()
	closedAt := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	seedConfirmState(t, stateDir, topology.LaunchStatusLaunched, closedAt)
	stageClient := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-dev", Name: "appdev", Status: "ACTIVE"},
	})

	result, _, err := handleLaunchConfirmProduction(context.Background(), "src-proj", stageClient, confirmInput(true), stateDir, "")
	if err != nil {
		t.Fatalf("handleLaunchConfirmProduction: %v", err)
	}
	body := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("re-confirm on a closed window must be a benign echo, got error: %s", body)
	}
	if !strings.Contains(body, "window-closed") || !strings.Contains(body, "2026-06-11T10:00:00Z") {
		t.Errorf("re-confirm must echo the original close time: %s", body)
	}
}

// TestProdOps_AfterClose_LifecycleMessage pins the post-close behavior
// (T3 item 2): with the window closed (stage deleted + WindowClosedAt
// stamped), a prod-ops call without launchKey refuses with the
// lifecycle message naming confirm-production and the close time.
func TestProdOps_AfterClose_LifecycleMessage(t *testing.T) {
	stateDir := t.TempDir()
	closedAt := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	seedConfirmState(t, stateDir, topology.LaunchStatusLaunched, closedAt)
	// Stage deleted at close — the dev service carries no token.
	stageClient := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-dev", Name: "appdev", Status: "ACTIVE"},
	})

	result, _, _ := handleLaunchProdOps(context.Background(), "src-proj", stageClient, nil, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ProdOperation:         "status",
	}, stateDir, "")
	if !result.IsError {
		t.Fatal("prod-ops after the close must refuse")
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, "confirm-production") || !strings.Contains(body, "2026-06-11T10:00:00Z") {
		t.Errorf("post-close refusal must name the close act + time: %s", body)
	}
	if !strings.Contains(body, ops.LaunchTokenEnvKey) {
		t.Errorf("post-close refusal must name the staged-secret lifecycle: %s", body)
	}
}
