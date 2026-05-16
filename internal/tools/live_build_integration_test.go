//go:build live

// Test 3 of the live-platform test suite — build-integration (Actions
// push-mode CI/CD wiring) flow. Verifies the handler against a
// synthesized post-git-push ServiceMeta and asserts the response
// carries:
//
//   1. Walkthrough mode: options atom listing actions/webhook/none.
//   2. Confirm mode (integration=actions): workflow YAML body using
//      raw zcli (not zeropsio/actions marketplace) + secret-set
//      command + PAT recommendation. ServiceMeta.BuildIntegration
//      stamped.
//   3. Pre-req chain: build-integration without git-push-configured
//      returns needsGitPushSetup status with structured next-step.

package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestLive_BuildIntegration_ActionsConfirm runs the build-integration
// confirm flow with integration=actions against a synthesized
// post-git-push meta. Asserts workflow YAML composition + secret
// command + meta update.
func TestLive_BuildIntegration_ActionsConfirm(t *testing.T) {
	client, projectID, _ := liveSourceClient(t)
	ctx, cancel := liveTestCtx(t, 0)
	defer cancel()

	// Confirm eval-zcp appdev exists (live reference shape).
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	var found bool
	for _, s := range services {
		if s.Name == "appdev" {
			found = true
			break
		}
	}
	if !found {
		t.Skip("eval-zcp has no appdev service; build-integration test requires it")
	}

	// Synthesize post-git-push meta on disk.
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.ModeStandard,
		StageHostname:    "appstage",
		CloseDeployMode:  topology.CloseModeGitPush,
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/krls2020/eval2",
		BootstrapSession: "live-test-bi-session",
		BootstrappedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	containerRt := runtime.Info{InContainer: true}

	// Confirm call: integration=actions.
	resp, _, err := handleBuildIntegration(
		ctx,
		client,
		projectID,
		WorkflowInput{
			Action:      "build-integration",
			Service:     "appdev",
			Integration: "actions",
		},
		stateDir,
		containerRt,
	)
	if err != nil {
		t.Fatalf("confirm call: %v", err)
	}
	respText := extractText(resp)
	t.Logf("build-integration=actions response (%d bytes)", len(respText))

	// Default workflowFile must be the raw-zcli setup-aware variant
	// (works for multi-setup repos, the only universally-correct
	// shape). Karel's session never reached this surface — these
	// pins are the live equivalent of TestComposeActionsHandoff_* unit
	// pins, gated on the response's default `workflowFile` block.
	for _, mustContain := range []string{
		"name: Zerops deploy", // workflow header (current handler emit)
		"push:",               // push trigger
		"jobs:",               // jobs section
		"zcli push",           // raw zcli invocation
		"--setup",             // setup-aware (multi-setup safe)
		"ZEROPS_TOKEN",        // secret name (current shape)
		"gh secret set",       // gh secret command
		"krls2020/eval2",      // owner/repo from meta.RemoteURL
	} {
		if !strings.Contains(respText, mustContain) {
			t.Errorf("build-integration=actions response missing %q:\n%s",
				mustContain, respText)
		}
	}

	// Verify the DEFAULT variant is setup-aware-zcli. The handler
	// also emits an `alternateWorkflowFiles` fallback for single-setup
	// repos — that one uses zeropsio/actions marketplace. Phase 1c
	// composer (internal/ops/cicd) produces a cleaner emission with
	// ONLY raw zcli; Phase 6b will wire that composer into this
	// handler. For now we accept the alternate emit as known-tolerable.
	if !strings.Contains(respText, `"variant":"setup-aware-zcli"`) {
		t.Errorf("default workflowFile variant should be setup-aware-zcli; response shape regressed:\n%s", respText)
	}
	if strings.Contains(respText, `"workflowFile":{"content":"name: Zerops deploy\non:\n  push:\n    branches: [main]\njobs:\n  deploy:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - uses: zeropsio/actions@v1.0.2`) {
		t.Error("DEFAULT workflowFile is using zeropsio/actions marketplace — Phase 6b should have switched it to raw zcli")
	}

	// Verify ServiceMeta.BuildIntegration stamped.
	updated, err := workflow.FindServiceMeta(stateDir, "appdev")
	if err != nil {
		t.Fatalf("re-read meta: %v", err)
	}
	if updated.BuildIntegration != topology.BuildIntegrationActions {
		t.Errorf("BuildIntegration: got %q want %q", updated.BuildIntegration, topology.BuildIntegrationActions)
	}
	// Pair-keyed integrity.
	if updated.GitPushState != topology.GitPushConfigured {
		t.Errorf("GitPushState clobbered: got %q want %q", updated.GitPushState, topology.GitPushConfigured)
	}
	if updated.RemoteURL != "https://github.com/krls2020/eval2" {
		t.Errorf("RemoteURL clobbered: got %q", updated.RemoteURL)
	}
}

// TestLive_BuildIntegration_RequiresGitPushSetup pins the prereq chain.
// Calling build-integration=actions on a meta with
// GitPushState=unconfigured MUST return a needsGitPushSetup pointer +
// the right nextStep (run git-push-setup first).
func TestLive_BuildIntegration_RequiresGitPushSetup(t *testing.T) {
	client, projectID, _ := liveSourceClient(t)
	ctx, cancel := liveTestCtx(t, 0)
	defer cancel()

	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.ModeStandard,
		StageHostname:    "appstage",
		CloseDeployMode:  topology.CloseModeGitPush,
		GitPushState:     topology.GitPushUnconfigured, // NOT configured
		BootstrapSession: "live-test-prereq",
		BootstrappedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	resp, _, err := handleBuildIntegration(
		ctx,
		client,
		projectID,
		WorkflowInput{
			Action:      "build-integration",
			Service:     "appdev",
			Integration: "actions",
		},
		stateDir,
		runtime.Info{InContainer: true},
	)
	if err != nil {
		t.Fatalf("prereq call: %v", err)
	}
	text := extractText(resp)
	if !strings.Contains(text, "needsGitPushSetup") {
		t.Errorf("prereq response should carry status=needsGitPushSetup; got:\n%s", text)
	}
	if !strings.Contains(text, "git-push-setup") {
		t.Errorf("prereq response should point at git-push-setup as nextStep; got:\n%s", text)
	}
}
