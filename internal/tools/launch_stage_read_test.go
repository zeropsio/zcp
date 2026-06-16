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

// T2 — secret-sourced operations: launch-window calls resolve the token
// from the staged ZCP_LAUNCH_TOKEN service secret when launchKey is
// absent; the value stays in-request (never echoed, never persisted).

// stagedSourceClient returns a source-project mock whose dev service
// (fixed identity "svc-dev"/"appdev") carries the staged launch token
// (the sentinel value — assertions track where it must and must not
// surface).
func stagedSourceClient() *platform.Mock {
	return platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev", Status: "ACTIVE"},
		}).
		WithServiceEnv("svc-dev", []platform.ServiceEnvVar{
			{ID: "env-1", Key: ops.LaunchTokenEnvKey, Content: sentinelLaunchKey, Type: platform.ServiceEnvSecret},
		})
}

// captureAdminFactory installs a factory that records the launchKey
// value it was constructed with and hands out the given mock.
func captureAdminFactory(t *testing.T, m *platform.MockProjectAdminClient) *string {
	t.Helper()
	var captured string
	cleanup := setProjectAdminClientFactory(func(launchKey, _ string) (platform.ProjectAdminClient, error) {
		captured = launchKey
		m.Closed = false
		return m, nil
	})
	t.Cleanup(cleanup)
	return &captured
}

func seedProdOpsStateWithDevHost(t *testing.T, stateDir, devHostname string) {
	t.Helper()
	state := &launchState{
		LaunchID:              generateLaunchID("src-proj", "myapp-prod"),
		SourceProjectID:       "src-proj",
		TargetProjectName:     "myapp-prod",
		TargetProjectID:       "prod-proj-123",
		TargetServiceHostname: devHostname,
		Status:                topology.LaunchStatusLaunched,
	}
	if err := writeLaunchState(stateDir, state); err != nil {
		t.Fatalf("writeLaunchState: %v", err)
	}
}

// TestProdOps_ReadsStagedToken pins the T2 fallback: a prod-ops call
// without launchKey resolves the token from the staged secret on the
// source dev service and proceeds; the value never appears in the
// response.
func TestProdOps_ReadsStagedToken(t *testing.T) {
	// non-parallel: captureAdminFactory mutates the package-global factory.
	stateDir := t.TempDir()
	seedProdOpsStateWithDevHost(t, stateDir, "appdev")
	m := platform.NewMockProjectAdminClient().WithServices([]platform.ServiceStack{
		{ID: "svc-app", Name: "app", Status: "ACTIVE"},
	})
	captured := captureAdminFactory(t, m)
	stageClient := stagedSourceClient()

	result, _, _ := handleLaunchProdOps(context.Background(), "src-proj", stageClient, nil, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ProdOperation:         "status",
	}, stateDir, "")
	body := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("prod-ops with staged token must proceed, got error: %s", body)
	}
	if *captured != sentinelLaunchKey {
		t.Errorf("admin factory must receive the STAGED token value; got %q", *captured)
	}
	if strings.Contains(body, sentinelLaunchKey) {
		t.Errorf("staged token value leaked into the prod-ops response:\n%s", body)
	}
}

// TestProdOps_StageFirst_PrefersStageOverExplicit pins B5 / P-LP-14:
// window-op token resolution is STAGE-FIRST — the staged ZCP_LAUNCH_TOKEN
// secret wins over an explicit launchKey passed on the call (spec §10.2 names
// the explicit key only a fallback). Guards against a stale re-supplied key
// overriding the live staged value.
func TestProdOps_StageFirst_PrefersStageOverExplicit(t *testing.T) {
	// non-parallel: captureAdminFactory mutates the package-global factory.
	stateDir := t.TempDir()
	seedProdOpsStateWithDevHost(t, stateDir, "appdev")
	m := platform.NewMockProjectAdminClient().WithServices([]platform.ServiceStack{
		{ID: "svc-app", Name: "app", Status: "ACTIVE"},
	})
	captured := captureAdminFactory(t, m)
	stageClient := stagedSourceClient() // staged secret == sentinelLaunchKey

	result, _, _ := handleLaunchProdOps(context.Background(), "src-proj", stageClient, nil, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ProdOperation:         "status",
		LaunchKey:             "explicit-stale-key-must-not-win",
	}, stateDir, "")
	if result.IsError {
		t.Fatalf("prod-ops must proceed with the staged token: %s", getTextContent(t, result))
	}
	if *captured != sentinelLaunchKey {
		t.Errorf("stage-first (P-LP-14): the STAGED secret must win over an explicit launchKey; admin factory got %q want staged %q", *captured, sentinelLaunchKey)
	}
}

// TestProdOps_StageEmpty_Refuses pins the lifecycle refusal: no
// launchKey AND no staged secret → the refusal names the staged-secret
// lifecycle (window closed / stage deleted) instead of a bare
// "launchKey required".
func TestProdOps_StageEmpty_Refuses(t *testing.T) {
	// non-parallel: captureAdminFactory mutates the package-global factory.
	stateDir := t.TempDir()
	seedProdOpsStateWithDevHost(t, stateDir, "appdev")
	m := platform.NewMockProjectAdminClient()
	captured := captureAdminFactory(t, m)
	// Dev service exists but carries NO staged secret (window closed or
	// never staged).
	stageClient := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-dev", Name: "appdev", Status: "ACTIVE"},
	})

	result, _, _ := handleLaunchProdOps(context.Background(), "src-proj", stageClient, nil, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ProdOperation:         "status",
	}, stateDir, "")
	if !result.IsError {
		t.Fatal("prod-ops without key or staged secret must refuse")
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, ops.LaunchTokenEnvKey) || !strings.Contains(body, "window") {
		t.Errorf("refusal must carry the staged-secret lifecycle line: %s", body)
	}
	if *captured != "" {
		t.Errorf("no admin client may be constructed without a token; factory got %q", *captured)
	}
}

// TestPipelineResume_StagedToken pins the T2 resume fallback: a
// launched state with a pending pipeline check resumes WITHOUT
// launchKey — the handler reads the staged secret and re-runs the
// pipeline check with it.
func TestPipelineResume_StagedToken(t *testing.T) {
	// non-parallel: captureAdminFactory mutates the package-global factory.
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient().
		WithServiceEnv("svc-app", []platform.ServiceEnvVar{
			{ID: "env-1", Key: ops.LaunchTokenEnvKey, Content: sentinelLaunchKey, Type: platform.ServiceEnvSecret},
		})

	state := &launchState{
		LaunchID:              generateLaunchID("source-project-id", "myapp-prod"),
		SourceProjectID:       "source-project-id",
		TargetProjectName:     "myapp-prod",
		TargetProjectID:       "prod-1",
		TargetServiceHostname: "app",
		Status:                topology.LaunchStatusLaunched,
		PipelineConfigurations: map[string]pipelineConfigEntry{
			"app": {Configured: false},
		},
		PipelineCheckedAt: time.Now().Add(-time.Hour).UTC(),
		RuntimeProds: []launchRuntimeProd{
			{ProdHostname: "app", RepoURL: canonicalLaunchTestRemoteURL, SetupName: "prod"},
		},
		ImportedServices: []importedServiceEntry{{ID: "svc-prod-app", Name: "app"}},
	}
	if err := writeLaunchState(stateDir, state); err != nil {
		t.Fatalf("writeLaunchState: %v", err)
	}

	m := platform.NewMockProjectAdminClient()
	captured := captureAdminFactory(t, m)

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		WorkflowInput{
			Workflow:              workflowLaunchProduction,
			ProductionProjectName: "myapp-prod",
			TargetService:         "app",
			EnvClassifications:    map[string]string{"LOG_LEVEL": "plain-config"},
		}, stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	if *captured != sentinelLaunchKey {
		t.Errorf("pipeline resume must run with the STAGED token; factory got %q", *captured)
	}
	updated, readErr := readLaunchState(stateDir, state.LaunchID)
	if readErr != nil {
		t.Fatalf("read state: %v", readErr)
	}
	if !updated.PipelineCheckedAt.After(state.PipelineCheckedAt) {
		t.Error("pipeline check must re-run on the staged-token resume")
	}
	if strings.Contains(text, sentinelLaunchKey) {
		t.Errorf("staged token value leaked into the resume response:\n%s", text)
	}
}

// TestLaunchReset_StagedToken pins the T2 reset fallback: a reset
// without launchKey reads the staged secret, so the orphan production
// project is deletable through the diagnose-before-destruct gate
// without re-asking the user for the token.
func TestLaunchReset_StagedToken(t *testing.T) {
	// non-parallel: captureAdminFactory mutates the package-global factory.
	dir := t.TempDir()
	launchID := generateLaunchID("src", "myapp-prod")
	state := &launchState{
		LaunchID:              launchID,
		SourceProjectID:       "src",
		TargetProjectName:     "myapp-prod",
		TargetProjectID:       "tgt-orphan",
		TargetServiceHostname: "appdev",
		Status:                topology.LaunchStatusFailed,
	}
	if err := writeLaunchState(dir, state); err != nil {
		t.Fatalf("write state: %v", err)
	}
	stageClient := stagedSourceClient()
	m := platform.NewMockProjectAdminClient().
		WithDeleteResult(&platform.Process{ID: "del-proc-1", Status: "RUNNING"})
	captured := captureAdminFactory(t, m)

	// First call (no ack): the refusal's wouldDestroy must list the
	// orphan project — proof the staged token was resolved and the
	// delete path is armed.
	result, _, _ := handleLaunchReset(context.Background(), dir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
	}, "")
	body := getTextContent(t, result)
	if !strings.Contains(body, "tgt-orphan") {
		t.Fatalf("first reset call must arm the orphan-project delete from the staged token (wouldDestroy lists the project): %s", body)
	}

	// Acked call: orphan deleted via the staged token.
	result, _, _ = handleLaunchReset(context.Background(), dir, "src", stageClient, WorkflowInput{
		ProductionProjectName: "myapp-prod",
		ConfirmDestructive: &DestructiveAck{
			Operation:           launchResetOperation,
			AcknowledgedTargets: []string{"myapp-prod", "tgt-orphan"},
		},
	}, "")
	body = getTextContent(t, result)
	if m.CapturedDeleteProject != "tgt-orphan" {
		t.Errorf("DeleteProject must run with the staged token; captured project %q", m.CapturedDeleteProject)
	}
	if *captured != sentinelLaunchKey {
		t.Errorf("admin factory must receive the STAGED token value; got %q", *captured)
	}
	if strings.Contains(body, sentinelLaunchKey) {
		t.Errorf("staged token value leaked into the reset response:\n%s", body)
	}
}

// TestProdOpsDoneBoundary_ActionsFamilyPendingIsDone pins the J5 parity
// gap the live lifecycle e2e surfaced: for the actions family the
// platform integration-status is EXPECTED not-configured (GitHub
// Actions registers no Zerops webhook), so a pending pipeline entry
// must NOT hold the done boundary open forever — the boundary points
// at confirm-production once the launch is terminal.
func TestProdOpsDoneBoundary_ActionsFamilyPendingIsDone(t *testing.T) {
	t.Parallel()
	state := &launchState{
		Status:            topology.LaunchStatusLaunched,
		TargetProjectName: "myapp-prod",
		PipelineConfigurations: map[string]pipelineConfigEntry{
			"app": {Configured: false},
		},
	}

	actions := prodOpsDoneBoundary(state, topology.BuildIntegrationActions)
	if done, _ := actions["done"].(bool); !done {
		t.Errorf("actions family with expected-not-configured pipeline must report done=true: %+v", actions)
	}
	if next, _ := actions["nextStep"].(string); !strings.Contains(next, "confirm-production") {
		t.Errorf("actions done boundary must chain confirm-production: %+v", actions)
	}

	webhook := prodOpsDoneBoundary(state, topology.BuildIntegrationWebhook)
	if done, _ := webhook["done"].(bool); done {
		t.Errorf("webhook family with a pending pipeline entry must stay open: %+v", webhook)
	}
}
