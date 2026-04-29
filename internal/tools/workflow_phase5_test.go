package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestHandleCloseMode_Update pins the happy path: writing meta.CloseDeployMode +
// CloseDeployModeConfirmed=true, returning status=updated. Single-service
// case; handler accepts a per-service map even for one entry.
func TestHandleCloseMode_Update(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, err := handleCloseMode(WorkflowInput{
		CloseModes: map[string]string{"appdev": string(topology.CloseModeAuto)},
	}, stateDir)
	if err != nil {
		t.Fatalf("handleCloseMode: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %s", getTextContent(t, result))
	}
	if !strings.Contains(getTextContent(t, result), `"status":"updated"`) {
		t.Errorf("response missing status=updated: %s", getTextContent(t, result))
	}

	// Re-read and verify persistence.
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta == nil || meta.CloseDeployMode != topology.CloseModeAuto {
		t.Errorf("CloseDeployMode not persisted: %+v", meta)
	}
	if !meta.CloseDeployModeConfirmed {
		t.Error("CloseDeployModeConfirmed not flipped on update")
	}
}

// TestHandleCloseMode_GitPushChainsSetup pins §3.4 Scenario B: switching
// to git-push close-mode while GitPushState != configured succeeds the
// write but surfaces a chained pointer at action=git-push-setup.
func TestHandleCloseMode_GitPushChainsSetup(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
		// GitPushState left unconfigured — migrate at parseMeta would land
		// at GitPushUnconfigured anyway; explicit for readability.
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, err := handleCloseMode(WorkflowInput{
		CloseModes: map[string]string{"appdev": string(topology.CloseModeGitPush)},
	}, stateDir)
	if err != nil || result.IsError {
		t.Fatalf("expected success, got: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, `"nextSteps"`) {
		t.Errorf("response missing nextSteps pointer: %s", body)
	}
	if !strings.Contains(body, `git-push-setup`) {
		t.Errorf("nextSteps should mention git-push-setup: %s", body)
	}
}

// TestHandleCloseMode_InvalidValue pins the value-validation gate:
// closeMode values outside the closed enum set are rejected with
// ErrInvalidParameter and the valid-set listing.
func TestHandleCloseMode_InvalidValue(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	result, _, _ := handleCloseMode(WorkflowInput{
		CloseModes: map[string]string{"appdev": "auto-close"},
	}, stateDir)
	if !result.IsError {
		t.Fatal("expected error for invalid closeMode value")
	}
	body := getTextContent(t, result)
	for _, want := range []string{"Invalid closeMode", "auto-close", "auto, git-push, manual"} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q: %s", want, body)
		}
	}
}

// TestHandleGitPushSetup_Confirm pins the confirm-mode happy path:
// passing service + remoteUrl writes GitPushState=configured + RemoteURL.
func TestHandleGitPushSetup_Confirm(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, err := handleGitPushSetup(WorkflowInput{
		Service:   "appdev",
		RemoteURL: "https://github.com/example/app.git",
	}, stateDir, runtime.Info{})
	if err != nil || result.IsError {
		t.Fatalf("expected success, got: %s", getTextContent(t, result))
	}

	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta.GitPushState != topology.GitPushConfigured {
		t.Errorf("GitPushState = %q, want configured", meta.GitPushState)
	}
	if meta.RemoteURL != "https://github.com/example/app.git" {
		t.Errorf("RemoteURL not persisted: %q", meta.RemoteURL)
	}
}

// TestHandleGitPushSetup_RejectsStageHostname pins the source-of-push
// gate: a stage-hostname target resolves to the dev-keyed meta but is
// not a push source — rejected with remediation pointing at the dev half.
func TestHandleGitPushSetup_RejectsStageHostname(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleGitPushSetup(WorkflowInput{
		Service:   "appstage",
		RemoteURL: "https://github.com/example/app.git",
	}, stateDir, runtime.Info{})
	if !result.IsError {
		t.Fatal("expected error for stage-hostname target")
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, "stage half") {
		t.Errorf("response should call out the stage-half rejection: %s", body)
	}
	if !strings.Contains(body, "appdev") {
		t.Errorf("response should redirect to dev hostname: %s", body)
	}
}

// TestHandleBuildIntegration_NeedsGitPushSetup pins the prereq-chain
// pre-check: setting integration=webhook on a service with
// GitPushState != configured returns the chained guidance pointer.
func TestHandleBuildIntegration_NeedsGitPushSetup(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleBuildIntegration(context.Background(), nil, "", WorkflowInput{
		Service:     "appdev",
		Integration: string(topology.BuildIntegrationWebhook),
	}, stateDir, runtime.Info{})
	if result.IsError {
		t.Fatalf("expected guidance response, not error: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, `"status":"needsGitPushSetup"`) {
		t.Errorf("response missing needsGitPushSetup: %s", body)
	}
	if !strings.Contains(body, "git-push-setup") {
		t.Errorf("response should chain to git-push-setup: %s", body)
	}
}

// TestHandleBuildIntegration_Configures pins the happy path: with
// GitPushState=configured, setting integration=webhook writes
// meta.BuildIntegration.
func TestHandleBuildIntegration_Configures(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleBuildIntegration(context.Background(), nil, "", WorkflowInput{
		Service:     "appdev",
		Integration: string(topology.BuildIntegrationActions),
	}, stateDir, runtime.Info{})
	if result.IsError {
		t.Fatalf("expected configured, got error: %s", getTextContent(t, result))
	}
	if !strings.Contains(getTextContent(t, result), `"status":"configured"`) {
		t.Errorf("response missing status=configured: %s", getTextContent(t, result))
	}

	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta.BuildIntegration != topology.BuildIntegrationActions {
		t.Errorf("BuildIntegration = %q, want actions", meta.BuildIntegration)
	}
}

// TestHandleGitPush_RejectsUnconfiguredState pins the missing R-state
// rejection unit test flagged by the Phase 4 Codex POST-WORK review:
// a service with GitPushState=unconfigured triggers gitPushMetaPreflight's
// ErrPrerequisiteMissing branch with a setup-pointer remediation.
func TestHandleGitPush_RejectsUnconfiguredState(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
		// GitPushState left unset — migrate at parseMeta lands at
		// GitPushUnconfigured given DeployStrategy is also empty.
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	captured := make([]string, 0, 1)
	recordAttempt := func(err string, _ topology.FailureClass) {
		captured = append(captured, err)
	}
	result := gitPushMetaPreflight(stateDir, "appdev", recordAttempt)
	if result == nil {
		t.Fatal("expected pre-flight rejection on unconfigured GitPushState")
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, "git-push not configured") {
		t.Errorf("error should call out unconfigured state: %s", body)
	}
	if !strings.Contains(body, "git-push-setup") {
		t.Errorf("error should redirect to git-push-setup: %s", body)
	}
	if len(captured) != 1 {
		t.Errorf("recordAttempt called %d times, want 1", len(captured))
	}
}

// TestGitPushMetaPreflight_AndRemoteFallback documents the contract that
// closed Codex finding HIGH#1 (2026-04-29 review): atoms now route the
// agent to call action="git-push-setup" before the deploy, and the deploy
// handler reads meta.RemoteURL as a fallback when input.RemoteURL is empty.
// gitPushMetaPreflight is the read site for source-of-push + state checks;
// the read of meta.RemoteURL itself happens in handleGitPush after the
// preflight passes. This test pins the "configured + ready" state shape so
// the integration test couldn't silently regress to PREREQUISITE_MISSING.
func TestGitPushMetaPreflight_PassesAfterSetup(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.ModeSimple,
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/example/demo.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-29",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	captured := make([]string, 0, 1)
	record := func(err string, _ topology.FailureClass) {
		captured = append(captured, err)
	}
	if blocked := gitPushMetaPreflight(stateDir, "appdev", record); blocked != nil {
		body := getTextContent(t, blocked)
		t.Fatalf("preflight blocked a configured/ready service: %s", body)
	}
	if len(captured) != 0 {
		t.Errorf("recordAttempt fired on success path: %v", captured)
	}

	// Verify the meta still carries the stamped RemoteURL — the deploy
	// handler reads this exact field as the fallback for an empty input.
	meta, err := workflow.ReadServiceMeta(stateDir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if meta.RemoteURL != "https://github.com/example/demo.git" {
		t.Errorf("meta.RemoteURL = %q, want stamped value", meta.RemoteURL)
	}
}

// TestHandleBuildIntegration_ActionsConfirmEnrichesResponse pins the
// post-2026-04-29 confirm shape for `integration=actions`. The terse
// `status:configured + nextStep:"After the integration is wired..."` body
// surfaced in live agent feedback as actionably useless: agent didn't know
// the workflow YAML to write, the secrets to set, or that ZEROPS_TOKEN is
// the same PAT as ZCP_API_KEY. The new body MUST carry:
//
//   - workflowFile.path + content (.github/workflows/zerops.yml YAML body)
//   - secrets[] with ZEROPS_TOKEN + ZEROPS_SERVICE_ID, each with a
//     ready-to-run `gh secret set` command
//   - ZCP_API_KEY reuse hint (no new PAT generation)
//   - per-repo fine-grained PAT recommendation
//   - env-aware ZCP_API_KEY source (this test pins local env via
//     runtime.Info{InContainer:false} → jq extraction from .mcp.json)
func TestHandleBuildIntegration_ActionsConfirmEnrichesResponse(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/example/demo.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-29",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleBuildIntegration(context.Background(), nil, "", WorkflowInput{
		Service:     "appdev",
		Integration: string(topology.BuildIntegrationActions),
	}, stateDir, runtime.Info{InContainer: false})
	if result.IsError {
		t.Fatalf("expected configured, got error: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)

	mustContain := []string{
		`"status":"configured"`,
		`"buildIntegration":"actions"`,
		`"workflowFile"`,
		".github/workflows/zerops.yml",
		"actions/checkout@v4",
		"zeropsio/actions-setup-zcli@v1",
		`"secrets"`,
		"ZEROPS_TOKEN",
		"ZEROPS_SERVICE_ID",
		"gh secret set ZEROPS_TOKEN",
		"gh secret set ZEROPS_SERVICE_ID",
		"example/demo", // owner/repo splice from RemoteURL
		// Local env hint: jq extraction from .mcp.json
		`jq -r '.mcpServers.zcp.env.ZCP_API_KEY' .mcp.json`,
		"ZCP_API_KEY",
		// Reuse hint — no new PAT generation
		"DON'T generate a new token",
		// Per-repo fine-grained PAT lead recommendation
		"fine-grained GitHub PAT scoped ONLY to example/demo",
		"Secrets: Read and write",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q in body: %s", want, body)
		}
	}

	// Container env path: $ZCP_API_KEY substitution instead of jq.
	stateDirContainer := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDirContainer, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/example/demo.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-29",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	resultContainer, _, _ := handleBuildIntegration(context.Background(), nil, "", WorkflowInput{
		Service:     "appdev",
		Integration: string(topology.BuildIntegrationActions),
	}, stateDirContainer, runtime.Info{InContainer: true})
	containerBody := getTextContent(t, resultContainer)
	if strings.Contains(containerBody, "jq -r") {
		t.Errorf("container response should NOT contain jq extraction (that's the local env path): %s", containerBody)
	}
	if !strings.Contains(containerBody, `\"$ZCP_API_KEY\"`) {
		t.Errorf("container response missing direct $ZCP_API_KEY substitution: %s", containerBody)
	}
}

// TestHandleBuildIntegration_NoneIsTerse pins the BuildIntegrationNone
// confirm shape: clearing an integration shouldn't produce the rich
// Actions handoff (no workflow YAML, no `gh secret set` snippets — just an
// acknowledgment that the integration was cleared).
func TestHandleBuildIntegration_NoneIsTerse(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		BuildIntegration: topology.BuildIntegrationActions,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-29",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleBuildIntegration(context.Background(), nil, "", WorkflowInput{
		Service:     "appdev",
		Integration: string(topology.BuildIntegrationNone),
	}, stateDir, runtime.Info{})
	if result.IsError {
		t.Fatalf("expected configured, got error: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)

	if !strings.Contains(body, `"buildIntegration":"none"`) {
		t.Errorf("response should reflect cleared integration: %s", body)
	}
	mustNotContain := []string{
		"workflowFile",
		"gh secret set",
		"ZEROPS_TOKEN",
		"dashboardSteps",
	}
	for _, forbidden := range mustNotContain {
		if strings.Contains(body, forbidden) {
			t.Errorf("none-confirm response should not carry %q (Actions/Webhook richness): %s", forbidden, body)
		}
	}
}

// TestHandleBuildIntegration_ActionsConfirmDegradesGracefully pins the
// degradation paths surfaced in Codex post-implementation review LOW#5:
// (a) RemoteURL empty → owner/repo placeholders + repoParseWarning,
// (b) RemoteURL unparseable → same warning fires,
// (c) client/projectID empty → serviceID placeholder + lookup warning.
// The response must remain self-describing in all three cases — no panics,
// no silent data loss, no commands the agent can't run.
func TestHandleBuildIntegration_ActionsConfirmDegradesGracefully(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		remoteURL  string
		wantWarns  []string
		wantInBody []string
	}{
		{
			// json.Marshal HTML-escapes "<" / ">" to < / > on
			// the wire, so the substring assertion uses the escaped form.
			name:       "empty RemoteURL surfaces repoParseWarning",
			remoteURL:  "",
			wantWarns:  []string{"repoParseWarning"},
			wantInBody: []string{"\\u003cowner\\u003e/\\u003crepo\\u003e"},
		},
		{
			name:       "unparseable RemoteURL surfaces repoParseWarning",
			remoteURL:  "not-a-url",
			wantWarns:  []string{"repoParseWarning"},
			wantInBody: []string{"\\u003cowner\\u003e/\\u003crepo\\u003e"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
				Hostname:         "appdev",
				Mode:             topology.PlanModeStandard,
				StageHostname:    "appstage",
				GitPushState:     topology.GitPushConfigured,
				RemoteURL:        tc.remoteURL,
				BootstrapSession: "test",
				BootstrappedAt:   "2026-04-29",
			}); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}
			result, _, _ := handleBuildIntegration(context.Background(), nil, "", WorkflowInput{
				Service:     "appdev",
				Integration: string(topology.BuildIntegrationActions),
			}, stateDir, runtime.Info{})
			if result.IsError {
				t.Fatalf("expected configured, got error: %s", getTextContent(t, result))
			}
			body := getTextContent(t, result)
			for _, warn := range tc.wantWarns {
				if !strings.Contains(body, warn) {
					t.Errorf("response missing degradation warning %q: %s", warn, body)
				}
			}
			for _, must := range tc.wantInBody {
				if !strings.Contains(body, must) {
					t.Errorf("response missing degradation placeholder %q: %s", must, body)
				}
			}
			// Always present — service-id lookup is impossible with nil
			// client / empty projectID.
			if !strings.Contains(body, "serviceIDLookupWarning") {
				t.Errorf("response missing serviceIDLookupWarning: %s", body)
			}
		})
	}
}

// TestHandleGitPushSetup_StandaloneModeDevSurfacesModeUnsupported pins the
// fix for the "X instead of X" templating bug. Standalone ModeDev services
// (no stage half) had input.Service == meta.Hostname; the legacy boolean
// IsPushSourceFor returned false without distinguishing the cause, so the
// handler rendered "set up from %q instead" with both placeholders bound
// to the same hostname. The new PushSourceCheckFor returns a discriminating
// PushSourceModeUnsupported and the handler routes it to a mode-expansion
// remediation message that names a different action entirely.
func TestHandleGitPushSetup_StandaloneModeDevSurfacesModeUnsupported(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "remindersdev",
		Mode:             topology.ModeDev,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-29",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, err := handleGitPushSetup(WorkflowInput{Service: "remindersdev"}, stateDir, runtime.Info{})
	if err != nil {
		t.Fatalf("handleGitPushSetup: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result on standalone ModeDev")
	}
	body := getTextContent(t, result)

	// Regression: the legacy "set up from %q instead" wording rendered both
	// placeholders identically when meta.Hostname == input.Service, producing
	// the nonsensical "set up from remindersdev instead". The new path must
	// not contain that wording at all on this branch.
	if strings.Contains(body, "set up from \"remindersdev\" instead") {
		t.Errorf("templating bug regression: error rendered \"X instead of X\": %s", body)
	}
	if !strings.Contains(body, "does not support push-git") {
		t.Errorf("error should explain mode-unsupported: %s", body)
	}
	if !strings.Contains(body, "mode-expansion") {
		t.Errorf("error should redirect to mode-expansion: %s", body)
	}
}

// TestHandleGitPushSetup_StageHalfRedirectsToDevHalf pins the
// PushSourceIsStageHalf path: when the agent passes the stage hostname as
// service, the handler should route to "set up from the dev half" with
// distinct service names (regression risk would be conflating this branch
// with the mode-unsupported branch above).
func TestHandleGitPushSetup_StageHalfRedirectsToDevHalf(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		StageHostname:    "appstage",
		Mode:             topology.ModeStandard,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-29",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, err := handleGitPushSetup(WorkflowInput{Service: "appstage"}, stateDir, runtime.Info{})
	if err != nil {
		t.Fatalf("handleGitPushSetup: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result on stage-half target")
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, "stage half") {
		t.Errorf("error should call out stage-half: %s", body)
	}
	if !strings.Contains(body, "appdev") {
		t.Errorf("error should redirect to dev half hostname: %s", body)
	}
}

var _ = context.Background // keep import alive for future test additions
