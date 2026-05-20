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

	"github.com/zeropsio/zcp/internal/ops/bundle"
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

// expectedExistingProjectID is the canonical target-project ID used
// by the existing-project test fixtures. Tests inject "wrong-target-id"
// (or similar) into the MOCK's WithProjects to exercise scope-mismatch
// failure; input.ExistingProjectID always carries the canonical value.
const expectedExistingProjectID = "expected-target-id"

// existingCompleteInput returns a WorkflowInput populated for the
// existing-project path: target project + project-scoped token + a
// classification for the single user env in pLP3MockClient. P-LP-3
// baseline is computed at compose time inside executeExistingProjectMutation.
func existingCompleteInput() WorkflowInput {
	return WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		Region:                "eu-central",
		TargetService:         "app",
		EnvClassifications:    map[string]string{"LOG_LEVEL": "plain-config"},
		ExistingProjectID:     expectedExistingProjectID,
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
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient()

	// Target mock reports the token resolves to project "wrong-target-id"
	// — different from the input.ExistingProjectID below.
	targetMock := existingTargetMock("wrong-target-id", nil)
	defer setExistingProdTokenClientFactory(func(_, _ string) (platform.Client, error) {
		return targetMock, nil
	})()

	input := existingCompleteInput()

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
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient()

	// Target has an existing service named "app" — same hostname the
	// launch bundle would create from input.TargetService="app".
	targetMock := existingTargetMock("expected-target-id", []platform.ServiceStack{
		{ID: "svc-existing-app", Name: "app", Status: "ACTIVE"},
	})
	defer setExistingProdTokenClientFactory(func(_, _ string) (platform.Client, error) {
		return targetMock, nil
	})()

	input := existingCompleteInput()

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
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient()

	targetMock := existingTargetMock("expected-target-id", nil)
	defer setExistingProdTokenClientFactory(func(_, _ string) (platform.Client, error) {
		return targetMock, nil
	})()

	input := existingCompleteInput()

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
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient()

	input := existingCompleteInput()
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

// TestLaunchExistingProject_ClassificationAppliedToTargetEnvs pins
// bug_001: the existing-project mutation path MUST transform each
// composer env according to its classification before calling
// CreateProjectEnv. Before the fix, the loop wrote raw source values
// verbatim — AutoSecret dev/stage secrets leaked to prod, ExternalSecret
// values never became REPLACE_ME, Infrastructure entries polluted the
// target with stale ${...} refs. This test seeds one env per
// non-PlainConfig classification + one PlainConfig and asserts the
// CreateProjectEnv contract per bucket.
func TestLaunchExistingProject_ClassificationAppliedToTargetEnvs(t *testing.T) {
	stateDir := t.TempDir()
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)

	// Source has one env per non-PlainConfig classification + one PlainConfig.
	sourceClient := pLP3MockClient().WithProjectEnv([]platform.ProjectEnvVar{
		{Key: "DB_HOST", Content: "${db_hostname}"},    // infrastructure → drop
		{Key: "APP_KEY", Content: "dev-secret-leaked"}, // auto-secret    → regenerate
		{Key: "STRIPE_SECRET", Content: "sk_test_xyz"}, // external-secret → REPLACE_ME
		{Key: "LOG_LEVEL", Content: "info"},            // plain-config    → verbatim
	})

	targetMock := existingTargetMock(expectedExistingProjectID, nil)
	defer setExistingProdTokenClientFactory(func(_, _ string) (platform.Client, error) {
		return targetMock, nil
	})()

	input := existingCompleteInput()
	input.EnvClassifications = map[string]string{
		"DB_HOST":       "infrastructure",
		"APP_KEY":       "auto-secret",
		"STRIPE_SECRET": "external-secret",
		"LOG_LEVEL":     "plain-config",
	}

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

	// Each CreateProjectEnv call captured by hostname keyed by env key
	// so the per-bucket asserts read clean.
	seen := map[string]platform.CapturedProjectEnvCreate{}
	for _, c := range targetMock.CapturedProjectEnvCreations {
		seen[c.Key] = c
	}

	// Infrastructure: dropped, never reaches CreateProjectEnv.
	if c, ok := seen["DB_HOST"]; ok {
		t.Errorf("Infrastructure env DB_HOST must be dropped from existing-project mutation; got CreateProjectEnv call: %+v", c)
	}

	// AutoSecret: present, value differs from source, length 32,
	// not the preprocessor directive literal, Sensitive=true.
	autoSec, ok := seen["APP_KEY"]
	if !ok {
		t.Fatalf("AutoSecret env APP_KEY missing from CreateProjectEnv captures; response:\n%s", text)
	}
	if autoSec.Content == "dev-secret-leaked" {
		t.Errorf("AutoSecret APP_KEY leaked dev source value verbatim to prod: %q", autoSec.Content)
	}
	if strings.Contains(autoSec.Content, "<@generateRandomString") {
		t.Errorf("AutoSecret APP_KEY emitted preprocessor directive as literal (CreateProjectEnv bypasses preprocessor): %q", autoSec.Content)
	}
	if len(autoSec.Content) != 32 {
		t.Errorf("AutoSecret APP_KEY expected 32-char value, got %d chars: %q", len(autoSec.Content), autoSec.Content)
	}
	if !autoSec.Sensitive {
		t.Errorf("AutoSecret APP_KEY should be Sensitive=true, got false")
	}

	// ExternalSecret: present, value=REPLACE_ME (literal), Sensitive=true.
	extSec, ok := seen["STRIPE_SECRET"]
	if !ok {
		t.Fatalf("ExternalSecret env STRIPE_SECRET missing from CreateProjectEnv captures; response:\n%s", text)
	}
	if extSec.Content != bundle.ExternalSecretPlaceholder {
		t.Errorf("ExternalSecret STRIPE_SECRET expected %q, got %q (source value leaked?)",
			bundle.ExternalSecretPlaceholder, extSec.Content)
	}
	if !extSec.Sensitive {
		t.Errorf("ExternalSecret STRIPE_SECRET should be Sensitive=true, got false")
	}

	// PlainConfig: present, value verbatim, Sensitive=false.
	plain, ok := seen["LOG_LEVEL"]
	if !ok {
		t.Fatalf("PlainConfig env LOG_LEVEL missing from CreateProjectEnv captures; response:\n%s", text)
	}
	if plain.Content != "info" {
		t.Errorf("PlainConfig LOG_LEVEL expected verbatim %q, got %q", "info", plain.Content)
	}
	if plain.Sensitive {
		t.Errorf("PlainConfig LOG_LEVEL should be Sensitive=false, got true")
	}

	// P-LP-1 sentinel scan: the project-scoped token MUST NOT appear
	// anywhere in the success response.
	if strings.Contains(text, sentinelExistingProdToken) {
		t.Errorf("ExistingProdToken sentinel leaked into launched response")
	}

	// Bug_001 regression net: the literal raw source secret value of
	// the AutoSecret/ExternalSecret rows MUST NOT appear in any
	// CreateProjectEnv emission. (Defense in depth — if a future
	// regression makes one of the buckets fall through to verbatim,
	// this catches it independently of the per-bucket asserts above.)
	for _, c := range targetMock.CapturedProjectEnvCreations {
		if c.Content == "dev-secret-leaked" {
			t.Errorf("regression: dev-secret-leaked value emitted on %q", c.Key)
		}
		if c.Content == "sk_test_xyz" {
			t.Errorf("regression: sk_test_xyz value emitted on %q", c.Key)
		}
	}
}

// TestLaunchExistingProject_SetupNameOverride_HonoredInBundle pins
// bug_004: the existing-project mutation path MUST consult
// effectiveProdSetupName(input) when constructing LaunchBundleInputs.
// Before the fix, executeExistingProjectMutation hardcoded
// SetupName: "prod" regardless of input.ProdSetupNameOverride. The
// upstream readAndValidateSourceState gate honored the override (so
// the source yaml gate-check used "production"), then the bundle
// composer was asked for "prod" anyway — producing either a silent
// wrong-block compose (when both names existed in the source yaml)
// or a late confusing rejection (when only the override existed).
func TestLaunchExistingProject_SetupNameOverride_HonoredInBundle(t *testing.T) {
	stateDir := t.TempDir()
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient()

	targetMock := existingTargetMock(expectedExistingProjectID, nil)
	defer setExistingProdTokenClientFactory(func(_, _ string) (platform.Client, error) {
		return targetMock, nil
	})()

	// Source zerops.yaml exposes BOTH `prod` and `production` setup
	// blocks. With ProdSetupNameOverride="production" the gate accepts
	// it; the composer must also emit `production` (not the default
	// "prod") for the two ends to agree.
	sshBothSetups := &stubSSHDeployer{
		responses: map[string][]byte{
			"git rev-parse HEAD": []byte("frozen-baseline-sha\n"),
			"git remote get-url": []byte("https://github.com/example/myapp\n"),
			"/var/www/zerops.yaml": []byte("zerops:\n" +
				"  - setup: prod\n" +
				"    build:\n      base: nodejs@22\n" +
				"    run:\n      base: nodejs@22\n      start: node prod.js\n" +
				"  - setup: production\n" +
				"    build:\n      base: nodejs@22\n" +
				"    run:\n      base: nodejs@22\n      start: node production.js\n"),
		},
	}

	input := existingCompleteInput()
	input.ProdSetupNameOverride = "production"

	result, _, err := handleLaunchProduction(
		context.Background(),
		"source-project-id",
		sourceClient,
		input,
		stateDir,
		pLP3ContainerRuntime(),
		sshBothSetups,
	)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)

	if targetMock.CapturedImportYAML == "" {
		t.Fatalf("ImportServices was not called; response:\n%s", text)
	}
	if !strings.Contains(targetMock.CapturedImportYAML, "zeropsSetup: production") {
		t.Errorf("captured launch yaml must carry zeropsSetup: production (override honored), got:\n%s",
			targetMock.CapturedImportYAML)
	}
	if strings.Contains(targetMock.CapturedImportYAML, "zeropsSetup: prod\n") ||
		strings.HasSuffix(strings.TrimRight(targetMock.CapturedImportYAML, "\n"), "zeropsSetup: prod") {
		t.Errorf("captured launch yaml must NOT carry zeropsSetup: prod when override=production, got:\n%s",
			targetMock.CapturedImportYAML)
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
				return targetMock, existingCompleteInput(), restore
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
				return targetMock, existingCompleteInput(), restore
			},
		},
		{
			name: "happy-path-launched",
			setup: func() (*platform.Mock, WorkflowInput, func()) {
				targetMock := existingTargetMock("expected-target-id", nil)
				restore := setExistingProdTokenClientFactory(func(_, _ string) (platform.Client, error) {
					return targetMock, nil
				})
				return targetMock, existingCompleteInput(), restore
			},
		},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			stateDir := t.TempDir()
			installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
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
