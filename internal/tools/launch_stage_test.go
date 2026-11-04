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

// TestExecuteLaunchMutation_StagesTheProjectDeployKey: the launchKey of the
// person who presses launch creates the production project and is dropped
// after the call; the source push service receives the new project's deploy
// key (BASIC_USER on that project alone), never the launchKey. A deploy key
// that is not minted or not staged leaves nothing staged and says so.
func TestExecuteLaunchMutation_StagesTheProjectDeployKey(t *testing.T) {
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
	launchedAdmin := func() *platform.MockProjectAdminClient {
		return platform.NewMockProjectAdminClient().
			WithImportResult(&platform.ImportResult{
				ProjectID:   "new-prod-id",
				ProjectName: "myapp-prod",
				ServiceStacks: []platform.ImportedServiceStack{
					{ID: "svc-prod-app", Name: "app", Processes: []platform.Process{{ID: "proc-1", Status: "FINISHED"}}},
				},
			}).
			WithProcess(&platform.Process{ID: "proc-1", Status: "FINISHED"}).
			WithClientUserID("client-user-abc")
	}
	deployKey := platform.MockDeployKeyPrefix + "new-prod-id"

	t.Run("launched stages the deploy key, never the launch key", func(t *testing.T) {
		stateDir := withTempState(t)
		installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
		sourceClient := pLP3MockClient()
		mockAdmin := launchedAdmin()
		defer installMockAdminFactory(t, mockAdmin)()

		result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil, nil,
			publishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		text := extractText(result)
		resp := decodeLaunchResp(t, []byte(text))
		if resp.Status != topology.LaunchStatusLaunched {
			t.Fatalf("status: got %q want launched\n%s", resp.Status, text)
		}
		if mockAdmin.CapturedMintProjectKeyProject != "new-prod-id" {
			t.Errorf("deploy key minted on %q, want the new production project new-prod-id", mockAdmin.CapturedMintProjectKeyProject)
		}
		if got := stagedTokenValue(t, sourceClient, "svc-app"); got != deployKey {
			t.Errorf("staged token on dev service: got %q want the production project's deploy key", got)
		}
		for _, secret := range []string{sentinelLaunchKey, deployKey} {
			if strings.Contains(text, secret) {
				t.Errorf("launched response leaks %q:\n%s", secret, text)
			}
		}
	})

	t.Run("failed create stages nothing", func(t *testing.T) {
		stateDir := withTempState(t)
		installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
		sourceClient := pLP3MockClient()
		mockAdmin := platform.NewMockProjectAdminClient().
			WithImportError(errors.New("simulated create failure"))
		defer installMockAdminFactory(t, mockAdmin)()

		result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil, nil,
			publishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		resp := decodeLaunchResp(t, []byte(extractText(result)))
		if resp.Status != topology.LaunchStatusFailed {
			t.Fatalf("status: got %q want failed", resp.Status)
		}
		if got := stagedTokenValue(t, sourceClient, "svc-app"); got != "" {
			t.Errorf("a failed create must stage nothing; staged value got %q", got)
		}
	})

	t.Run("a refused mint launches with nothing staged and says so", func(t *testing.T) {
		stateDir := withTempState(t)
		installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
		sourceClient := pLP3MockClient()
		mockAdmin := launchedAdmin().WithMintProjectKeyError(
			platform.NewPlatformError(platform.ErrTokenCannotMint, "integration tokens cannot mint", ""))
		defer installMockAdminFactory(t, mockAdmin)()

		result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil, nil,
			publishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		text := extractText(result)
		resp := decodeLaunchResp(t, []byte(text))
		if resp.Status != topology.LaunchStatusLaunched {
			t.Fatalf("status: got %q want launched (the project exists)\n%s", resp.Status, text)
		}
		if got := stagedTokenValue(t, sourceClient, "svc-app"); got != "" {
			t.Errorf("a refused mint must stage nothing, never the launch key; staged value got %q", got)
		}
		if !strings.Contains(text, "deploy key was not minted") {
			t.Errorf("a refused mint must say the deploy key was not minted:\n%s", text)
		}
	})

	t.Run("a stage failure after the create launches and says so", func(t *testing.T) {
		stateDir := withTempState(t)
		installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
		sourceClient := pLP3MockClient().
			WithError("CreateServiceEnvVar", errors.New("simulated env write failure"))
		mockAdmin := launchedAdmin()
		defer installMockAdminFactory(t, mockAdmin)()

		result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil, nil,
			publishInput(), stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), "")
		if err != nil {
			t.Fatalf("handleLaunchProduction: %v", err)
		}
		text := extractText(result)
		resp := decodeLaunchResp(t, []byte(text))
		if resp.Status != topology.LaunchStatusLaunched {
			t.Fatalf("status: got %q want launched (the project exists)\n%s", resp.Status, text)
		}
		if !strings.Contains(text, "deploy key was not staged") {
			t.Errorf("a stage failure must say the deploy key was not staged:\n%s", text)
		}
		if strings.Contains(text, deployKey) {
			t.Errorf("response leaks the deploy key:\n%s", text)
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

	result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil, nil,
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

	_, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil, nil,
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

		result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil, nil,
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

		result, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil, nil,
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
