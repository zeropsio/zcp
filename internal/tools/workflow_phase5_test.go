package tools

import (
	"context"
	"os"
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

// TestHandleCloseMode_GitPushRejectsNonPushSourceMode pins the O3 fix
// (round-3 audit): close-mode=git-push is invalid for modes that cannot
// act as a push source (ModeDev, ModeStage). Without this gate an agent
// can set close-mode=git-push for a dev-mode service, walk the
// git-push-setup chain to provision GIT_TOKEN/.netrc — then hit a hard
// rejection at deploy time when handleGitPush returns
// PushSourceModeUnsupported. The gate catches the invalid combination at
// intent-set time, mirroring the local-only gate.
func TestHandleCloseMode_GitPushRejectsNonPushSourceMode(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeDev, // not a push source
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, err := handleCloseMode(WorkflowInput{
		CloseModes: map[string]string{"appdev": string(topology.CloseModeGitPush)},
	}, stateDir)
	if err != nil {
		t.Fatalf("handleCloseMode: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error: ModeDev cannot act as git-push source")
	}
	body := getTextContent(t, result)
	for _, want := range []string{"cannot push", "Standard/Simple/LocalStage/LocalOnly", "mode-expansion"} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q: %s", want, body)
		}
	}

	// Meta must be untouched — the gate runs BEFORE the write.
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta.CloseDeployMode != "" {
		t.Errorf("CloseDeployMode should not be persisted on rejection, got %q", meta.CloseDeployMode)
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

// TestHandleGitPushSetup_Confirm pins the local-mode confirm-mode happy
// path: probe passes (stubbed), origin sync runs (stubbed), meta stamps
// configured. Container-mode confirm-mode tests live separately in
// workflow_git_push_setup_container_test.go and exercise the real probe-
// before-mutate sequence with a stubbed SSHDeployer.
func TestHandleGitPushSetup_Confirm(t *testing.T) {
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

	// Stub local probe + origin sync — return nil for both so the
	// verifier reaches the meta-stamp step.
	defer setLocalGitProbeReader(func(context.Context, string, string) error { return nil })()
	defer setLocalGitOriginSyncer(func(context.Context, string, string) error { return nil })()

	result, _, err := handleGitPushSetup(context.Background(), nil, nil, "test-project", WorkflowInput{
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
	// Phase 3 sweep: handler attaches workSessionState so the lifecycle
	// signal is uniform across mutation handlers (spec §1.3).
	body := getTextContent(t, result)
	if !strings.Contains(body, `"workSessionState"`) {
		t.Errorf("response missing workSessionState attachment: %s", body)
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

	result, _, _ := handleGitPushSetup(context.Background(), nil, nil, "test-project", WorkflowInput{
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

	result, _, _ := handleBuildIntegration(context.Background(), nil, nil, "", WorkflowInput{
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

	result, _, _ := handleBuildIntegration(context.Background(), nil, nil, "", WorkflowInput{
		Service:     "appdev",
		Integration: string(topology.BuildIntegrationActions),
	}, stateDir, runtime.Info{})
	if result.IsError {
		t.Fatalf("expected declared, got error: %s", getTextContent(t, result))
	}
	// F1 earned-state: confirm DECLARES the choice — it must not claim
	// "configured" (the 4 GitHub-side steps have not happened yet).
	if !strings.Contains(getTextContent(t, result), `"status":"declared"`) {
		t.Errorf("response missing status=declared: %s", getTextContent(t, result))
	}
	if !strings.Contains(getTextContent(t, result), `"verified":false`) {
		t.Errorf("confirm response must carry verified:false: %s", getTextContent(t, result))
	}

	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta.BuildIntegration != topology.BuildIntegrationActions {
		t.Errorf("BuildIntegration = %q, want actions", meta.BuildIntegration)
	}
	// Core F1 invariant: the confirm stamps the CHOICE, never the
	// verification — VerifiedAt is earned on the publish-side launch gate.
	if meta.BuildIntegrationVerifiedAt != "" {
		t.Errorf("confirm must NOT stamp BuildIntegrationVerifiedAt; got %q", meta.BuildIntegrationVerifiedAt)
	}
	// Phase 3 sweep: handler attaches workSessionState (via actionsConfirmResponse)
	// so the lifecycle signal is uniform across mutation handlers (spec §1.3).
	if !strings.Contains(getTextContent(t, result), `"workSessionState"`) {
		t.Errorf("response missing workSessionState attachment: %s", getTextContent(t, result))
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
		// GitPushState left unset — parseMeta lands at GitPushUnconfigured
		// given closeDeployMode is also empty (no migration triggers).
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

	result, _, _ := handleBuildIntegration(context.Background(), nil, nil, "", WorkflowInput{
		Service:     "appdev",
		Integration: string(topology.BuildIntegrationActions),
	}, stateDir, runtime.Info{InContainer: false})
	if result.IsError {
		t.Fatalf("expected configured, got error: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)

	mustContain := []string{
		`"status":"declared"`,
		`"verified":false`,
		`"buildIntegration":"actions"`,
		// Phase 3: standard pair resolves to stage half + prod setup.
		// Workflow YAML targets the build runtime, not the push source.
		`"buildTarget":"appstage"`,
		`"buildSetup":"prod"`,
		`"service":"appdev"`,
		`"workflowFile"`,
		".github/workflows/zerops.yml",
		"actions/checkout@v4",
		"setup-aware-zcli",
		"curl -sSL https://zerops.io/zcli/install.sh",
		"zcli login",
		"zcli push --service-id",
		`--setup \"prod\"`,
		"single-setup-action",
		"zeropsio/actions@v1.0.2",
		"access-token",
		"service-id",
		`"secrets"`,
		"ZEROPS_TOKEN",
		"ZEROPS_SERVICE_ID",
		"gh secret set ZEROPS_TOKEN",
		"gh secret set ZEROPS_SERVICE_ID",
		"example/demo", // owner/repo splice from RemoteURL
		// Local env hint: jq extraction from .mcp.json — the MCP server is
		// keyed "zerops" in .mcp.json (BI-NEW-1: the prior "zcp" key was a
		// phantom path that returned null → empty secret).
		`jq -r '.mcpServers.zerops.env.ZCP_API_KEY' .mcp.json`,
		"ZCP_API_KEY",
		// Reuse hint — no new PAT generation
		"DON'T generate a new token",
		// Per-repo fine-grained PAT lead recommendation
		"fine-grained GitHub PAT scoped ONLY to example/demo",
		"Secrets: Read and write",
		// B1 local-mode gh-auth tell: authenticate with the user-provided PAT,
		// never a phantom env var, never a generated token.
		"gh auth login --with-token",
		"collect via AskUserQuestion; NEVER generate one",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q in body: %s", want, body)
		}
	}
	// B1: the eval-harness env var must NEVER reach an agent-facing payload,
	// and the phantom 401 failureSymptom it shipped with is gone. Local mode
	// holds no credential to SSH-read, so it must not name a push source read.
	mustNotContainLocal := []string{
		"ZCP_E2E_GITHUB_PAT",
		"HTTP 401: Bad credentials",
		"StrictHostKeyChecking", // SSH read is container-only
	}
	for _, bad := range mustNotContainLocal {
		if strings.Contains(body, bad) {
			t.Errorf("local response must not contain %q: %s", bad, body)
		}
	}
	if strings.Contains(body, "zeropsio/actions-setup-zcli") {
		t.Errorf("response must not reference nonexistent setup action: %s", body)
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
	resultContainer, _, _ := handleBuildIntegration(context.Background(), nil, nil, "", WorkflowInput{
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
	// B1 container-mode gh-auth tell: read $GIT_TOKEN over SSH from the
	// push-source (appdev — the dev half, NOT buildTarget appstage), guarded
	// against the empty-token device-code hang, idempotent on an already-authed
	// CLI. The eval var must be gone here too.
	containerMustContain := []string{
		"ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null appdev",
		"$GIT_TOKEN",
		"gh auth login --with-token",
		`[ -n \"$_t\" ]`, // empty-token guard
		// idempotent short-circuit + SSH read in one escape-safe fragment
		// (the leading `gh auth status >/dev/null` JSON-escapes `>` to >).
		`|| { _t=$(ssh`,
	}
	for _, want := range containerMustContain {
		if !strings.Contains(containerBody, want) {
			t.Errorf("container response missing %q: %s", want, containerBody)
		}
	}
	for _, bad := range []string{"ZCP_E2E_GITHUB_PAT", "HTTP 401: Bad credentials"} {
		if strings.Contains(containerBody, bad) {
			t.Errorf("container response must not contain %q: %s", bad, containerBody)
		}
	}
	// The SSH read must target the push source (dev half), never the build
	// target — sending the agent to appstage would read a token-less shell.
	if strings.Contains(containerBody, "UserKnownHostsFile=/dev/null appstage") {
		t.Errorf("gh-auth SSH read must target push-source appdev, not build-target appstage: %s", containerBody)
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

	result, _, _ := handleBuildIntegration(context.Background(), nil, nil, "", WorkflowInput{
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
			result, _, _ := handleBuildIntegration(context.Background(), nil, nil, "", WorkflowInput{
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

	result, _, err := handleGitPushSetup(context.Background(), nil, nil, "test-project", WorkflowInput{Service: "remindersdev"}, stateDir, runtime.Info{})
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

	result, _, err := handleGitPushSetup(context.Background(), nil, nil, "test-project", WorkflowInput{Service: "appstage"}, stateDir, runtime.Info{})
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

// TestHandleGitPushSetup_LocalStage_StageHostnameTarget_Proceeds pins the
// Phase-12 carve-out: when meta.Mode is local-stage and the agent passes
// the stage hostname as service, the handler must NOT reject as
// PushSourceIsStageHalf — local-stage's dev half is the user's CWD
// (m.Hostname), not a Zerops service that can receive a push, so
// `targetService=apistage` is the legitimate call. Passes through to the
// walkthrough synthesis (no remoteUrl supplied → walkthrough mode).
func TestHandleGitPushSetup_LocalStage_StageHostnameTarget_Proceeds(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:                 "myproject",
		StageHostname:            "apistage",
		Mode:                     topology.PlanModeLocalStage,
		CloseDeployMode:          topology.CloseModeUnset,
		CloseDeployModeConfirmed: false,
		BootstrapSession:         "test",
		BootstrappedAt:           "2026-04-29",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, err := handleGitPushSetup(context.Background(), nil, nil, "test-project", WorkflowInput{Service: "apistage"}, stateDir, runtime.Info{})
	if err != nil {
		t.Fatalf("handleGitPushSetup: %v", err)
	}
	if result.IsError {
		body := getTextContent(t, result)
		t.Fatalf("local-stage stage-hostname target should proceed past PushSourceCheckFor; got error: %s", body)
	}
	body := getTextContent(t, result)
	if strings.Contains(body, "stage half") {
		t.Errorf("response must not carry stage-half rejection wording for local-stage; got: %s", body)
	}
	if !strings.Contains(body, "walkthrough") {
		t.Errorf("expected walkthrough response for local-stage stage-hostname target; got: %s", body)
	}
}

// TestHandleGitPushSetup_ContainerStandard_StageHalfStillRejects — regression:
// container standard pair keeps the IsStageHalf rejection so existing
// behavior for `targetService=appstage` stays unchanged.
func TestHandleGitPushSetup_ContainerStandard_StageHalfStillRejects(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		StageHostname:    "appstage",
		Mode:             topology.PlanModeStandard,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-29",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, err := handleGitPushSetup(context.Background(), nil, nil, "test-project", WorkflowInput{Service: "appstage"}, stateDir, runtime.Info{InContainer: true, ServiceName: "zcp"})
	if err != nil {
		t.Fatalf("handleGitPushSetup: %v", err)
	}
	if !result.IsError {
		t.Fatal("standard pair stage-half target must still reject")
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, "stage half") {
		t.Errorf("standard pair must keep stage-half wording; got: %s", body)
	}
}

var _ = context.Background // keep import alive for future test additions

// TestHandleCloseMode_FiresAutoCloseWhenScopeReady pins the structural
// fix for issue #3 (auto-close gate after close-mode write): when the
// session has every in-scope service deployed + verified and the
// close-mode write tips the last gate input, handleCloseMode must
// (a) stamp ws.ClosedAt + CloseReason via sessionAnnotations →
//
//	MaybeFireAutoClose lazy trigger, and
//
// (b) surface workSessionState.status="auto-closed" in the response
//
//	so the agent observes the lifecycle effect in the same call.
//
// Pre-fix: handler returned terse {status:"updated", services:"..."}
// and the session stayed open until the agent round-tripped via
// action="status" — violating spec §1.3 + §9.1 step 11.
//
// Note: not parallel — touches the per-PID work session file.
func TestHandleCloseMode_FiresAutoCloseWhenScopeReady(t *testing.T) {
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
	}); err != nil {
		t.Fatalf("WriteServiceMeta appdev: %v", err)
	}
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appstage",
		Mode:             topology.ModeStage,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
	}); err != nil {
		t.Fatalf("WriteServiceMeta appstage: %v", err)
	}

	// Pre-seed work session with both halves deployed + verified.
	ws := workflow.NewWorkSession("p", "container", "test", []string{"appdev", "appstage"})
	ws.Deploys = map[string][]workflow.DeployAttempt{
		"appdev":   {{AttemptedAt: "t", SucceededAt: "t", Setup: "dev"}},
		"appstage": {{AttemptedAt: "t", SucceededAt: "t", Setup: "prod"}},
	}
	ws.Verifies = map[string][]workflow.VerifyAttempt{
		"appdev":   {{AttemptedAt: "t", PassedAt: "t", Passed: true}},
		"appstage": {{AttemptedAt: "t", PassedAt: "t", Passed: true}},
	}
	if err := workflow.SaveWorkSession(stateDir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}

	// One handler call sets close-mode on both halves — last gate input tipped.
	result, _, err := handleCloseMode(WorkflowInput{
		CloseModes: map[string]string{
			"appdev":   string(topology.CloseModeAuto),
			"appstage": string(topology.CloseModeAuto),
		},
	}, stateDir)
	if err != nil {
		t.Fatalf("handleCloseMode: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)

	// (a) Response surfaces auto-closed state — F5 closure parity with
	// deploy/verify responses (spec §1.3 invariant).
	if !strings.Contains(body, `"workSessionState"`) {
		t.Errorf("response missing workSessionState field: %s", body)
	}
	if !strings.Contains(body, `"status":"auto-closed"`) {
		t.Errorf("response missing status=auto-closed: %s", body)
	}
	if !strings.Contains(body, `"closeReason":"`+workflow.CloseReasonAutoComplete+`"`) {
		t.Errorf("response missing closeReason=auto-complete: %s", body)
	}

	// (b) Auto-complete is DERIVED, not stamped on disk (the rebuild removed the
	// lazy MaybeFireAutoClose stamp — the gate can't desync from the display).
	// The session file stays unstamped; DeriveCloseState computes the close.
	loaded, err := workflow.LoadWorkSession(stateDir, os.Getpid())
	if err != nil {
		t.Fatalf("LoadWorkSession: %v", err)
	}
	if loaded.ClosedAt != "" {
		t.Errorf("ClosedAt must NOT be persisted (auto-complete is derived): %q", loaded.ClosedAt)
	}
	if closed, _, reason := workflow.DeriveCloseState(stateDir, loaded); !closed || reason != workflow.CloseReasonAutoComplete {
		t.Errorf("DeriveCloseState = (closed=%v, reason=%q), want (true, %q)", closed, reason, workflow.CloseReasonAutoComplete)
	}
}

// TestHandleCloseMode_StaysOpenWhenManualBlocks asserts the symmetric
// case: setting manual on at least one in-scope service blocks the gate
// even when scope is otherwise green, and the response surfaces
// status="open" (not auto-closed). Pins that lazy stamp doesn't
// over-fire.
func TestHandleCloseMode_StaysOpenWhenManualBlocks(t *testing.T) {
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
	}); err != nil {
		t.Fatalf("WriteServiceMeta appdev: %v", err)
	}
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appstage",
		Mode:             topology.ModeStage,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-28",
	}); err != nil {
		t.Fatalf("WriteServiceMeta appstage: %v", err)
	}
	ws := workflow.NewWorkSession("p", "container", "test", []string{"appdev", "appstage"})
	ws.Deploys = map[string][]workflow.DeployAttempt{
		"appdev":   {{AttemptedAt: "t", SucceededAt: "t", Setup: "dev"}},
		"appstage": {{AttemptedAt: "t", SucceededAt: "t", Setup: "prod"}},
	}
	ws.Verifies = map[string][]workflow.VerifyAttempt{
		"appdev":   {{AttemptedAt: "t", PassedAt: "t", Passed: true}},
		"appstage": {{AttemptedAt: "t", PassedAt: "t", Passed: true}},
	}
	if err := workflow.SaveWorkSession(stateDir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}

	// appstage gets manual → blocks gate even on green scope.
	result, _, err := handleCloseMode(WorkflowInput{
		CloseModes: map[string]string{
			"appdev":   string(topology.CloseModeAuto),
			"appstage": string(topology.CloseModeManual),
		},
	}, stateDir)
	if err != nil || result.IsError {
		t.Fatalf("handleCloseMode: %v / %s", err, getTextContent(t, result))
	}
	body := getTextContent(t, result)
	if strings.Contains(body, `"status":"auto-closed"`) {
		t.Errorf("auto-close fired despite manual mode: %s", body)
	}
	if !strings.Contains(body, `"status":"open"`) {
		t.Errorf("response missing status=open: %s", body)
	}
	loaded, _ := workflow.LoadWorkSession(stateDir, os.Getpid())
	if loaded == nil {
		t.Fatal("session lost")
	}
	if loaded.ClosedAt != "" {
		t.Errorf("ClosedAt stamped on manual-blocked gate: %q", loaded.ClosedAt)
	}
}

// Phase 3 — build-integration response carries explicit pushSource +
// topologyNote when the input hostname differs from the canonical pair
// meta. Without this the agent reads `service:"appstage"` as input echo
// and never sees that the configuration landed on the dev half.

func TestHandleBuildIntegration_PushSourceComputedFromCanonicalMeta(t *testing.T) {
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

	// Agent passes the STAGE hostname; handler resolves to canonical dev meta.
	result, _, _ := handleBuildIntegration(context.Background(), nil, nil, "", WorkflowInput{
		Service:     "appstage",
		Integration: string(topology.BuildIntegrationActions),
	}, stateDir, runtime.Info{InContainer: false})
	if result.IsError {
		t.Fatalf("expected configured, got error: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)

	for _, want := range []string{
		`"service":"appstage"`,     // input echo preserved
		`"pushSource":"appdev"`,    // canonical role, NOT input echo
		`"buildTarget":"appstage"`, // resolved build target
		`"topologyNote":`,          // explanatory note present
		"Standard-pair build-integration",
		`from the dev half \"appdev\"`,
		`stage half \"appstage\"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q:\n%s", want, body)
		}
	}
}

func TestHandleBuildIntegration_NoOpReCall_MatchesFirstCallShape(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	// Pre-set BuildIntegration so the next call hits the noop branch.
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		BuildIntegration: topology.BuildIntegrationActions,
		RemoteURL:        "https://github.com/example/demo.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-29",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleBuildIntegration(context.Background(), nil, nil, "", WorkflowInput{
		Service:     "appdev",
		Integration: string(topology.BuildIntegrationActions),
	}, stateDir, runtime.Info{InContainer: false})
	if result.IsError {
		t.Fatalf("expected noop, got error: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)

	// Idempotent verification path must surface canonical roles + topologyNote
	// identically to the first-call response (eval-friction: agent could not
	// tell whether build-integration had stuck without re-calling and
	// re-deriving the topology).
	// BI-NOOP-1: the re-call returns the FULL enriched handoff (stateless
	// recompute) — post-compaction this is the only way the agent
	// re-fetches the workflow file + secret commands it lost. The terse
	// `status:noop` body is gone for actions/webhook (kept only for none).
	for _, want := range []string{
		`"status":"declared"`,
		`"service":"appdev"`,
		`"pushSource":"appdev"`,
		`"buildTarget":"appstage"`,
		`"buildSetup":"prod"`,
		`"topologyNote":`,
		`"workflowFile":`,
		`"secrets":`,
		`"ghAuthPrecondition":`,
		`"verified":false`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("noop re-call missing %q:\n%s", want, body)
		}
	}
}

// Phase 4 — close-mode dual-half conflict detection. ServiceMeta is
// pair-keyed; passing both halves with divergent values would
// silently last-write-wins by Go's map iteration order. Reject the
// ambiguous input; same-value duals stay accepted (no-op shortcut
// absorbs the second pass).

func TestHandleCloseMode_DivergentDualHalves_Rejected(t *testing.T) {
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
		CloseModes: map[string]string{
			"appdev":   string(topology.CloseModeAuto),
			"appstage": string(topology.CloseModeManual),
		},
	}, stateDir)
	if err != nil {
		t.Fatalf("handleCloseMode: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for divergent dual-halves, got: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)
	for _, want := range []string{
		"close-mode conflict",
		`pair \"appdev\"`,
		"appdev=auto",
		"appstage=manual",
		"Pass a single value for the pair",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q:\n%s", want, body)
		}
	}

	// Meta must NOT have been mutated — reject happens before any write.
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta != nil && meta.CloseDeployMode != "" {
		t.Errorf("preflight must reject before mutation; meta.CloseDeployMode=%q", meta.CloseDeployMode)
	}
}

func TestHandleCloseMode_SameValueDualHalves_Accepted(t *testing.T) {
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

	// Both halves with the SAME value → no conflict; write once to canonical.
	result, _, err := handleCloseMode(WorkflowInput{
		CloseModes: map[string]string{
			"appdev":   string(topology.CloseModeManual),
			"appstage": string(topology.CloseModeManual),
		},
	}, stateDir)
	if err != nil {
		t.Fatalf("handleCloseMode: %v", err)
	}
	if result.IsError {
		t.Fatalf("same-value duals must be accepted, got error: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, `"status":"updated"`) {
		t.Errorf("response missing status=updated:\n%s", body)
	}
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta == nil || meta.CloseDeployMode != topology.CloseModeManual {
		t.Errorf("canonical meta missing manual close-mode after dual-half input: %+v", meta)
	}
}

func TestHandleBuildIntegration_SimpleNoTopologyNote(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	// Simple mode: pushSource == buildTarget, so topologyNote would be noise.
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeSimple,
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/example/demo.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-04-29",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleBuildIntegration(context.Background(), nil, nil, "", WorkflowInput{
		Service:     "appdev",
		Integration: string(topology.BuildIntegrationActions),
	}, stateDir, runtime.Info{InContainer: false})
	if result.IsError {
		t.Fatalf("expected configured, got error: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)

	if !strings.Contains(body, `"pushSource":"appdev"`) {
		t.Errorf("simple mode must still carry pushSource:\n%s", body)
	}
	if strings.Contains(body, `"topologyNote":`) {
		t.Errorf("simple mode must NOT carry topologyNote (pushSource == buildTarget):\n%s", body)
	}
}
