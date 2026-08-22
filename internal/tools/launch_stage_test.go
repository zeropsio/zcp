package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// stagedTokenValue reads the staged launch-token secret back from the
// mock platform client's faithful env store. Empty when not staged.
func stagedTokenValue(t *testing.T, client platform.Client, serviceID string) string {
	t.Helper()
	envs, err := client.GetServiceEnv(context.Background(), serviceID)
	if err != nil {
		t.Fatalf("GetServiceEnv(%s): %v", serviceID, err)
	}
	for _, e := range envs {
		if e.Key == ops.LaunchTokenEnvKey {
			return e.Content
		}
	}
	return ""
}

// TestLaunchTokenEnvKey_ClassifiedInfrastructure ties the tell to the
// check: the staged-token key the launch workflow writes MUST be in
// topology's hard-wired infrastructure bucket, else export/launch
// bundles copy the live token value into agent-visible YAML
// (serviceUserEnvsToBundleSecrets filters on IsClassifyInfrastructure).
func TestLaunchTokenEnvKey_ClassifiedInfrastructure(t *testing.T) {
	t.Parallel()
	if !topology.IsClassifyInfrastructure(ops.LaunchTokenEnvKey) {
		t.Fatalf("topology.IsClassifyInfrastructure(%q) = false — the staged launch token would leak into export/launch bundles", ops.LaunchTokenEnvKey)
	}
}

// TestExecuteLaunchMutation_StagesTokenBeforeCreate pins the T1 staging
// protocol on the new-project mutation path: the launch token is staged
// as a service-scope SECRET (ZCP_LAUNCH_TOKEN) on the source push
// service strictly BEFORE the irreversible CreateAndImportProject, and
// a staging failure aborts the mutation with no project created and no
// state file written.
func TestExecuteLaunchMutation_StagesTokenBeforeCreate(t *testing.T) {
	// non-parallel: installMockAdminFactory mutates the package-global factory.
	publishInput := func() WorkflowInput {
		return WorkflowInput{
			Workflow:              workflowLaunchProduction,
			ProductionProjectName: "myapp-prod",
			Region:                "eu-central",
			TargetService:         "app",
			EnvClassifications:    map[string]string{"LOG_LEVEL": "plain-config"},
			LaunchKey:             sentinelLaunchKey,
		}
	}

	t.Run("staged on launched", func(t *testing.T) {
		stateDir := withTempState(t)
		installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
		sourceClient := pLP3MockClient()
		mockAdmin := platform.NewMockProjectAdminClient().
			WithImportResult(&platform.ImportResult{
				ProjectID:   "new-prod-id",
				ProjectName: "myapp-prod",
				ServiceStacks: []platform.ImportedServiceStack{
					{ID: "svc-prod-app", Name: "app", Processes: []platform.Process{{ID: "proc-1", Status: "FINISHED"}}},
				},
			}).
			WithProcess(&platform.Process{ID: "proc-1", Status: "FINISHED"}).
			WithClientUserID("client-user-abc")
		defer installMockAdminFactory(t, mockAdmin)()

		result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
			publishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		resp := decodeLaunchResp(t, []byte(extractText(result)))
		if resp.Status != topology.LaunchStatusLaunched {
			t.Fatalf("status: got %q want launched\n%s", resp.Status, extractText(result))
		}
		if got := stagedTokenValue(t, sourceClient, "svc-app"); got != sentinelLaunchKey {
			t.Errorf("staged token on dev service: got %q want the launch key", got)
		}
		// The staged write is a server-side secret — the value must not
		// surface in the response.
		if strings.Contains(extractText(result), sentinelLaunchKey) {
			t.Errorf("launched response leaks the launch key:\n%s", extractText(result))
		}
	})

	t.Run("staged before failed create", func(t *testing.T) {
		stateDir := withTempState(t)
		installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
		sourceClient := pLP3MockClient()
		mockAdmin := platform.NewMockProjectAdminClient().
			WithImportError(errors.New("simulated create failure"))
		defer installMockAdminFactory(t, mockAdmin)()

		result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
			publishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		resp := decodeLaunchResp(t, []byte(extractText(result)))
		if resp.Status != topology.LaunchStatusFailed {
			t.Fatalf("status: got %q want failed", resp.Status)
		}
		// Order proof: the staged secret exists even though create failed —
		// staging ran BEFORE the irreversible step.
		if got := stagedTokenValue(t, sourceClient, "svc-app"); got != sentinelLaunchKey {
			t.Errorf("token must be staged before CreateAndImportProject; staged value got %q", got)
		}
	})

	t.Run("stage failure aborts before create", func(t *testing.T) {
		stateDir := withTempState(t)
		installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
		sourceClient := pLP3MockClient().
			WithError("CreateServiceEnvVar", errors.New("simulated env write failure"))
		mockAdmin := platform.NewMockProjectAdminClient()
		defer installMockAdminFactory(t, mockAdmin)()

		result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
			publishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		text := extractText(result)
		resp := decodeLaunchResp(t, []byte(text))
		if resp.Status != topology.LaunchStatusFailed {
			t.Fatalf("status: got %q want failed", resp.Status)
		}
		if !strings.Contains(text, "launch-token-stage-failed") {
			t.Errorf("stage failure must surface the launch-token-stage-failed blocker:\n%s", text)
		}
		if mockAdmin.CapturedImportYAML != "" {
			t.Error("CreateAndImportProject must NOT run when staging failed (abort before the irreversible create)")
		}
		// No project, no state: a retry with the same launchKey starts clean.
		if _, readErr := readLaunchState(stateDir, generateLaunchID("source-project-id", "myapp-prod")); !errors.Is(readErr, ErrLaunchStateMissing) {
			t.Errorf("stage failure must not persist launch state; read err = %v", readErr)
		}
	})
}

// TestStageLaunchToken_IsSensitive pins the platform's 2026-08 userData
// model requirement (spec-zerops-env-lifecycle.md §7): the staged
// ZCP_LAUNCH_TOKEN service var must carry sensitive:true, like every other
// ZCP-written service-scope secret.
func TestStageLaunchToken_IsSensitive(t *testing.T) {
	// non-parallel: installMockAdminFactory mutates the package-global factory.
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient()
	mockAdmin := platform.NewMockProjectAdminClient().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "new-prod-id",
			ProjectName: "myapp-prod",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-prod-app", Name: "app", Processes: []platform.Process{{ID: "proc-1", Status: "FINISHED"}}},
			},
		}).
		WithProcess(&platform.Process{ID: "proc-1", Status: "FINISHED"}).
		WithClientUserID("client-user-abc")
	defer installMockAdminFactory(t, mockAdmin)()

	input := WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		Region:                "eu-central",
		TargetService:         "app",
		EnvClassifications:    map[string]string{"LOG_LEVEL": "plain-config"},
		LaunchKey:             sentinelLaunchKey,
	}

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		input, stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != topology.LaunchStatusLaunched {
		t.Fatalf("status: got %q want launched\n%s", resp.Status, extractText(result))
	}

	envs, err := sourceClient.GetServiceEnv(context.Background(), "svc-app")
	if err != nil {
		t.Fatalf("GetServiceEnv: %v", err)
	}
	found := false
	for _, e := range envs {
		if e.Key != ops.LaunchTokenEnvKey {
			continue
		}
		found = true
		if !e.Sensitive {
			t.Errorf("staged %s Sensitive = false, want true", ops.LaunchTokenEnvKey)
		}
	}
	if !found {
		t.Fatalf("%s was not staged on svc-app", ops.LaunchTokenEnvKey)
	}
}

// TestLaunchStaging_KeyNeverInState pins the P-LP-1 extension for the
// staging path: the token value reaches the platform env write ONLY —
// after a staging launch neither the state file nor the audit log
// carries it (the staged secret lives platform-side, never on disk).
func TestLaunchStaging_KeyNeverInState(t *testing.T) {
	// non-parallel: installMockAdminFactory mutates the package-global factory.
	stateDir := withTempState(t)
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient()
	mockAdmin := platform.NewMockProjectAdminClient().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "new-prod-id",
			ProjectName: "myapp-prod",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-prod-app", Name: "app", Processes: []platform.Process{{ID: "proc-1", Status: "FINISHED"}}},
			},
		}).
		WithProcess(&platform.Process{ID: "proc-1", Status: "FINISHED"}).
		WithClientUserID("client-user-abc")
	defer installMockAdminFactory(t, mockAdmin)()

	_, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
		WorkflowInput{
			Workflow:              workflowLaunchProduction,
			ProductionProjectName: "myapp-prod",
			Region:                "eu-central",
			TargetService:         "app",
			EnvClassifications:    map[string]string{"LOG_LEVEL": "plain-config"},
			LaunchKey:             sentinelLaunchKey,
		}, stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}

	launchDir := filepath.Join(stateDir, launchStateDir)
	entries, readErr := os.ReadDir(launchDir)
	if readErr != nil {
		t.Fatalf("read %s: %v", launchDir, readErr)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, fileErr := os.ReadFile(filepath.Join(launchDir, e.Name()))
		if fileErr != nil {
			t.Fatalf("read %s: %v", e.Name(), fileErr)
		}
		if strings.Contains(string(data), sentinelLaunchKey) {
			t.Errorf("%s carries the launch key value after a staging mutation", e.Name())
		}
	}
}

// TestExecuteExistingProjectMutation_StagesToken pins T1 item 5 — the
// existing-project path stages ExistingProdToken under the SAME key
// before its first mutation (CreateProjectEnv), with the same
// abort-on-stage-failure semantics. Its launch window has the same
// recovery needs as the new-project path.
func TestExecuteExistingProjectMutation_StagesToken(t *testing.T) {
	// non-parallel: setExistingProdTokenClientFactory mutates package state.
	t.Run("staged before import", func(t *testing.T) {
		stateDir := t.TempDir()
		installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
		sourceClient := pLP3MockClient()
		targetMock := existingTargetMock(expectedExistingProjectID, nil)
		defer setExistingProdTokenClientFactory(func(_, _ string) (platform.Client, error) {
			return targetMock, nil
		})()

		result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
			existingCompleteInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		text := extractText(result)
		if got := stagedTokenValue(t, sourceClient, "svc-app"); got != sentinelExistingProdToken {
			t.Errorf("existing-project path must stage the prod token on the dev service; staged value got %q", got)
		}
		if strings.Contains(text, sentinelExistingProdToken) {
			t.Errorf("response leaks the existing-prod token:\n%s", text)
		}
	})

	t.Run("stage failure aborts before mutation", func(t *testing.T) {
		stateDir := t.TempDir()
		installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
		sourceClient := pLP3MockClient().
			WithError("CreateServiceEnvVar", errors.New("simulated env write failure"))
		targetMock := existingTargetMock(expectedExistingProjectID, nil)
		defer setExistingProdTokenClientFactory(func(_, _ string) (platform.Client, error) {
			return targetMock, nil
		})()

		result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil,
			existingCompleteInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		text := extractText(result)
		if !strings.Contains(text, "launch-token-stage-failed") {
			t.Errorf("stage failure must surface the launch-token-stage-failed blocker:\n%s", text)
		}
		if len(targetMock.CapturedProjectEnvCreations) != 0 {
			t.Error("target env mutation must NOT run when staging failed")
		}
		if targetMock.CapturedImportYAML != "" {
			t.Error("ImportServices must NOT run when staging failed")
		}
	})
}
