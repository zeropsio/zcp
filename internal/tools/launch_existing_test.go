// Existing-project mutation path pins per plan §6.2 + §6.6.
// Three behavioral pins + two P-LP-1 sentinel pins land here:
//
//   1. TestLaunchExistingProject_TokenScopeMismatch_Refuses — token
//      authenticates and resolves to a different project than
//      ExistingProjectID → ErrTokenScopeMismatch (structured).
//   2. TestLaunchExistingProject_HostnameConflict_Refuses — target
//      project already has a service with a hostname the bundle
//      would create → refuses with hostname-conflict blocker.
//   3. TestLaunchExistingProject_ServicesOnlyImport_NoProjectBlock —
//      happy path: VariantLaunchExisting strips project block;
//      ImportServices receives services-only yaml; CreateProjectEnv
//      called per classified USER-scope env.
//   4. TestLaunchExistingProject_BothCredentials_Refused — caller
//      supplied both LaunchKey AND ExistingProjectID+ExistingProdToken
//      → ErrInvalidParameter (ambiguous publish intent).
//   5. TestExistingProdToken_NeverInResponse — P-LP-1 sentinel scan:
//      the token value cannot appear anywhere in serialized response.

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

const sentinelExistingProdToken = "ZCP-EXISTING-PROD-TOKEN-SENTINEL-DO-NOT-LEAK"

// existingTargetMock returns a mock standing in for the target
// project's project-scoped client. Configurable via the test setup:
// projects (single-project scope) + existing services on target +
// import result (echoes ProjectID + services).
func existingTargetMock(targetProjectID string, existingServices []platform.ServiceStack) *platform.Mock {
	return platform.NewMock().
		WithUserInfo(&platform.UserInfo{ID: "user-client-id"}).
		WithProjects([]platform.Project{
			{ID: targetProjectID, Name: "existing-prod-project"},
		}).
		WithServices(existingServices).
		WithImportResult(&platform.ImportResult{
			ProjectID:   targetProjectID,
			ProjectName: "existing-prod-project",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-imported-app", Name: "app"},
			},
		})
}

// existingCompleteInput returns a WorkflowInput populated for the
// existing-project path: target project + project-scoped token + a
// classification for the single user env in pLP3MockClient. P-LP-3
// baseline is computed at compose time inside executeExistingProjectMutation.
func existingCompleteInput(existingProjectID string) WorkflowInput {
	return WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		Region:                "eu-central",
		TargetService:         "app",
		EnvClassifications:    map[string]string{"LOG_LEVEL": "plain-config"},
		ExistingProjectID:     existingProjectID,
		ExistingProdToken:     sentinelExistingProdToken,
	}
}

// TestLaunchExistingProject_TokenScopeMismatch_Refuses pins the
// validateExistingProdTokenScope gate (§6.6 step 2): a token that
// authenticates and resolves to exactly one project, but that
// project's ID does NOT match input.ExistingProjectID, MUST refuse
// with ErrTokenScopeMismatch.
func TestLaunchExistingProject_TokenScopeMismatch_Refuses(t *testing.T) {
	stateDir := t.TempDir()
	sourceClient := pLP3MockClient()

	// Target mock reports the token resolves to project "wrong-target-id"
	// — different from the input.ExistingProjectID below.
	targetMock := existingTargetMock("wrong-target-id", nil)
	defer setExistingProdTokenClientFactory(func(_, _ string) (platform.Client, error) {
		return targetMock, nil
	})()

	input := existingCompleteInput("expected-target-id")

	result, _, err := handleLaunchProduction(
		context.Background(),
		"source-project-id",
		sourceClient,
		input,
		stateDir,
		pLP3ContainerRuntime(),
		pLP3SSHFrozen(),
	)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	if !strings.Contains(text, platform.ErrTokenScopeMismatch) {
		t.Errorf("expected response to carry ErrTokenScopeMismatch code, got body:\n%s", text)
	}
	if !strings.Contains(text, "wrong-target-id") || !strings.Contains(text, "expected-target-id") {
		t.Errorf("expected response to mention both project IDs for diagnostic clarity, got:\n%s", text)
	}
	// P-LP-1 sentinel scan on the refused response.
	if strings.Contains(text, sentinelExistingProdToken) {
		t.Errorf("ExistingProdToken sentinel value leaked into refusal response:\n%s", text)
	}
}

// TestLaunchExistingProject_HostnameConflict_Refuses pins the
// hostname-conflict preflight (§6.6 step 4): if the target project
// already has a service whose Name matches a bundle-incoming
// hostname, mutation refuses BEFORE any CreateProjectEnv /
// ImportServices call.
func TestLaunchExistingProject_HostnameConflict_Refuses(t *testing.T) {
	stateDir := t.TempDir()
	sourceClient := pLP3MockClient()

	// Target has an existing service named "app" — same hostname the
	// launch bundle would create from input.TargetService="app".
	targetMock := existingTargetMock("expected-target-id", []platform.ServiceStack{
		{ID: "svc-existing-app", Name: "app", Status: "ACTIVE"},
	})
	defer setExistingProdTokenClientFactory(func(_, _ string) (platform.Client, error) {
		return targetMock, nil
	})()

	input := existingCompleteInput("expected-target-id")

	result, _, err := handleLaunchProduction(
		context.Background(),
		"source-project-id",
		sourceClient,
		input,
		stateDir,
		pLP3ContainerRuntime(),
		pLP3SSHFrozen(),
	)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	if !strings.Contains(text, "hostname-conflict") {
		t.Errorf("expected response to carry hostname-conflict blocker code, got:\n%s", text)
	}
	if !strings.Contains(text, "app") {
		t.Errorf("expected response to mention the conflicting hostname 'app', got:\n%s", text)
	}
	// No mutation occurred — preflight refuses before write.
	if len(targetMock.CapturedProjectEnvCreations) != 0 {
		t.Errorf("preflight refused but CreateProjectEnv was called: %+v", targetMock.CapturedProjectEnvCreations)
	}
	if targetMock.CapturedImportYAML != "" {
		t.Errorf("preflight refused but ImportServices was called with yaml:\n%s", targetMock.CapturedImportYAML)
	}
	// P-LP-1 sentinel scan.
	if strings.Contains(text, sentinelExistingProdToken) {
		t.Errorf("ExistingProdToken sentinel leaked into hostname-conflict response")
	}
}

// TestLaunchExistingProject_ServicesOnlyImport_NoProjectBlock pins
// P-LP-13: the existing-project mutation path emits services-only
// yaml (VariantLaunchExisting strips the top-level project block);
// the platform's PostProjectServiceStackImport endpoint rejects yaml
// carrying a project block. Also verifies CreateProjectEnv is called
// per USER-classified env (split mutation invariant).
func TestLaunchExistingProject_ServicesOnlyImport_NoProjectBlock(t *testing.T) {
	stateDir := t.TempDir()
	sourceClient := pLP3MockClient()

	targetMock := existingTargetMock("expected-target-id", nil)
	defer setExistingProdTokenClientFactory(func(_, _ string) (platform.Client, error) {
		return targetMock, nil
	})()

	input := existingCompleteInput("expected-target-id")

	result, _, err := handleLaunchProduction(
		context.Background(),
		"source-project-id",
		sourceClient,
		input,
		stateDir,
		pLP3ContainerRuntime(),
		pLP3SSHFrozen(),
	)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)

	// Per-env mutation: pLP3MockClient seeds one USER-scope env
	// (LOG_LEVEL), which is classified as plain-config in input.
	if len(targetMock.CapturedProjectEnvCreations) != 1 {
		t.Fatalf("CreateProjectEnv calls: got %d want 1\ncaptured: %+v",
			len(targetMock.CapturedProjectEnvCreations),
			targetMock.CapturedProjectEnvCreations)
	}
	got := targetMock.CapturedProjectEnvCreations[0]
	if got.ProjectID != "expected-target-id" {
		t.Errorf("CreateProjectEnv projectID: got %q want expected-target-id", got.ProjectID)
	}
	if got.Key != "LOG_LEVEL" || got.Content != "info" {
		t.Errorf("CreateProjectEnv key/content: got %+v want {LOG_LEVEL, info}", got)
	}
	if got.Sensitive {
		t.Errorf("CreateProjectEnv plain-config classification should be non-sensitive, got Sensitive=true")
	}

	// Services-only yaml: ImportServices captured, no top-level
	// `project:` block. VariantLaunchExisting closes P-LP-13.
	if targetMock.CapturedImportYAML == "" {
		t.Fatalf("ImportServices was not called; expected services-only yaml")
	}
	if strings.Contains(targetMock.CapturedImportYAML, "\nproject:\n") ||
		strings.HasPrefix(targetMock.CapturedImportYAML, "project:\n") {
		t.Errorf("services-only yaml must not contain a top-level project: block.\ncaptured yaml:\n%s",
			targetMock.CapturedImportYAML)
	}
	if !strings.Contains(targetMock.CapturedImportYAML, "services:") {
		t.Errorf("services-only yaml must contain a services: section.\ncaptured:\n%s", targetMock.CapturedImportYAML)
	}
	if targetMock.CapturedImportProjectID != "expected-target-id" {
		t.Errorf("ImportServices projectID: got %q want expected-target-id", targetMock.CapturedImportProjectID)
	}

	// Response surfaces launched status.
	resp := decodeLaunchResp(t, []byte(text))
	if resp.Status != "launched" {
		t.Errorf("status: got %q want launched\nbody:\n%s", resp.Status, text)
	}

	// P-LP-1 sentinel scan on the success response.
	if strings.Contains(text, sentinelExistingProdToken) {
		t.Errorf("ExistingProdToken sentinel leaked into launched response")
	}
}

// TestLaunchExistingProject_BothCredentials_Refused pins the
// mutually-exclusive credentials guard: both LaunchKey AND
// (ExistingProjectID + ExistingProdToken) supplied means the agent
// is misclassifying the user's intent — fail closed with
// ErrInvalidParameter. The new-project path's mutation is never
// reached; no platform calls.
func TestLaunchExistingProject_BothCredentials_Refused(t *testing.T) {
	stateDir := t.TempDir()
	sourceClient := pLP3MockClient()

	input := existingCompleteInput("expected-target-id")
	input.LaunchKey = sentinelLaunchKey

	result, _, err := handleLaunchProduction(
		context.Background(),
		"source-project-id",
		sourceClient,
		input,
		stateDir,
		pLP3ContainerRuntime(),
		pLP3SSHFrozen(),
	)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	if !strings.Contains(text, platform.ErrInvalidParameter) {
		t.Errorf("expected ErrInvalidParameter for mutually-exclusive credentials, got:\n%s", text)
	}
	if strings.Contains(text, sentinelLaunchKey) {
		t.Errorf("LaunchKey sentinel leaked into refusal response")
	}
	if strings.Contains(text, sentinelExistingProdToken) {
		t.Errorf("ExistingProdToken sentinel leaked into refusal response")
	}
}

// TestExistingProdToken_NeverInResponse pins P-LP-1 for the existing-
// project token: the sentinel value cannot appear anywhere in the
// JSON-serialized response across every branch the handler can take.
// Mirrors TestHandleLaunchProduction_LaunchKeyNeverInResponse.
func TestExistingProdToken_NeverInResponse(t *testing.T) {
	scenarios := []struct {
		name  string
		setup func() (*platform.Mock, WorkflowInput, func())
	}{
		{
			name: "token-scope-mismatch",
			setup: func() (*platform.Mock, WorkflowInput, func()) {
				targetMock := existingTargetMock("wrong-target-id", nil)
				restore := setExistingProdTokenClientFactory(func(_, _ string) (platform.Client, error) {
					return targetMock, nil
				})
				return targetMock, existingCompleteInput("expected-target-id"), restore
			},
		},
		{
			name: "hostname-conflict",
			setup: func() (*platform.Mock, WorkflowInput, func()) {
				targetMock := existingTargetMock("expected-target-id", []platform.ServiceStack{
					{ID: "svc-existing-app", Name: "app", Status: "ACTIVE"},
				})
				restore := setExistingProdTokenClientFactory(func(_, _ string) (platform.Client, error) {
					return targetMock, nil
				})
				return targetMock, existingCompleteInput("expected-target-id"), restore
			},
		},
		{
			name: "happy-path-launched",
			setup: func() (*platform.Mock, WorkflowInput, func()) {
				targetMock := existingTargetMock("expected-target-id", nil)
				restore := setExistingProdTokenClientFactory(func(_, _ string) (platform.Client, error) {
					return targetMock, nil
				})
				return targetMock, existingCompleteInput("expected-target-id"), restore
			},
		},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			stateDir := t.TempDir()
			_, input, restore := sc.setup()
			defer restore()
			result, _, err := handleLaunchProduction(
				context.Background(),
				"source-project-id",
				pLP3MockClient(),
				input,
				stateDir,
				pLP3ContainerRuntime(),
				pLP3SSHFrozen(),
			)
			if err != nil {
				t.Fatalf("handleLaunchProduction: %v", err)
			}
			if strings.Contains(extractText(result), sentinelExistingProdToken) {
				t.Errorf("scenario %q: ExistingProdToken sentinel leaked into response:\n%s",
					sc.name, extractText(result))
			}
		})
	}
}
