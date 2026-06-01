//go:build live

// Test 2 of the live-platform test suite — git-push-setup flow.
// Verifies the source-side git-push-setup handler against a real
// eval-zcp source service (appdev). Validates:
//
//   1. Walkthrough mode emits the env-aware setup-git-push-* atom
//      with the fine-grained PAT scope table (Karel 2026-05-16 said
//      this atom didn't fire correctly in his session — assert
//      content is present + scope table is intact).
//   2. Confirm mode (with remoteUrl) stamps ServiceMeta on disk:
//      GitPushState=configured + RemoteURL set, no other fields
//      clobbered.
//   3. Stage-half rejection: calling git-push-setup on the STAGE
//      hostname returns the structured "build target, never push
//      source" error pointing at the dev hostname.
//
// Real source state: eval-zcp service "appdev" (php-nginx@8.4) +
// pair-keyed meta synthesized at test setup (mirroring what bootstrap
// would have stamped). Tests use a per-test stateDir (t.TempDir()) so
// no interaction with live container's /var/www/.zcp state.

package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestLive_GitPushSetup_WalkthroughAndConfirm runs the two-call
// git-push-setup flow against a synthesized ServiceMeta matching
// eval-zcp's appdev service shape. Verifies atom content + ServiceMeta
// state transitions.
func TestLive_GitPushSetup_WalkthroughAndConfirm(t *testing.T) {
	// 1. Real platform read — confirm eval-zcp appdev exists. This
	// proves the test reference shape matches live source.
	client, projectID, _ := liveSourceClient(t)
	ctx, cancel := liveTestCtx(t, 0)
	defer cancel()

	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	var appdev *string
	for i, s := range services {
		if s.Name == "appdev" {
			n := services[i].Name
			appdev = &n
			break
		}
	}
	if appdev == nil {
		t.Skip("eval-zcp has no appdev service; git-push-setup test requires it")
	}
	t.Logf("eval-zcp appdev confirmed; synthesizing matching meta in temp stateDir")

	// 2. Synthesize ServiceMeta on disk — what bootstrap would have
	// stamped. Standard pair (appdev + appstage) matches behavioral
	// fixture shape.
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.ModeStandard,
		StageHostname:    "appstage",
		CloseDeployMode:  topology.CloseModeGitPush,
		GitPushState:     topology.GitPushUnconfigured,
		BootstrapSession: "live-test-session",
		BootstrappedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	// 3. Walkthrough call (no remoteUrl) — handler synthesizes the
	// env-aware setup-git-push-* atom.
	containerRt := runtime.Info{InContainer: true}
	walkthrough, _, err := handleGitPushSetup(
		WorkflowInput{Action: "git-push-setup", Service: "appdev"},
		stateDir,
		containerRt,
	)
	if err != nil {
		t.Fatalf("walkthrough call: %v", err)
	}
	walkText := extractText(walkthrough)
	t.Logf("walkthrough response (%d bytes)", len(walkText))

	// Atom content assertions — Karel's session showed atom didn't
	// fire correctly. Pin the specific PAT-scope keywords + deep-link
	// shape so a future regression is loud.
	for _, mustContain := range []string{
		"GIT_TOKEN",      // env var name
		"fine-grained",   // PAT type recommendation
		"Contents:",      // GitHub scopes table column
		"GitHub fine",    // scopes table row
		"GitLab",         // GitLab scopes row
		"git-push-setup", // self-reference for confirm step
	} {
		if !strings.Contains(walkText, mustContain) {
			t.Errorf("walkthrough atom missing %q — content regression vs Karel's session expectation:\n%s",
				mustContain, walkText)
		}
	}
	if !strings.Contains(walkText, "\"status\":\"walkthrough\"") {
		t.Errorf("walkthrough call did not return status=walkthrough:\n%s", walkText)
	}

	// 4. Confirm call (with remoteUrl) — handler stamps ServiceMeta.
	const remoteURL = "https://github.com/krls2020/eval2"
	confirm, _, err := handleGitPushSetup(
		WorkflowInput{Action: "git-push-setup", Service: "appdev", RemoteURL: remoteURL},
		stateDir,
		containerRt,
	)
	if err != nil {
		t.Fatalf("confirm call: %v", err)
	}
	confirmText := extractText(confirm)
	t.Logf("confirm response (%d bytes)", len(confirmText))

	// 5. Verify ServiceMeta updated correctly.
	updated, err := workflow.FindServiceMeta(stateDir, "appdev")
	if err != nil {
		t.Fatalf("re-read meta: %v", err)
	}
	if updated == nil {
		t.Fatal("meta vanished after confirm call")
	}
	if updated.GitPushState != topology.GitPushConfigured {
		t.Errorf("GitPushState: got %q want %q", updated.GitPushState, topology.GitPushConfigured)
	}
	if updated.RemoteURL != remoteURL {
		t.Errorf("RemoteURL: got %q want %q", updated.RemoteURL, remoteURL)
	}
	// Pair-keyed integrity — stage hostname survives, mode survives.
	if updated.StageHostname != "appstage" {
		t.Errorf("stage hostname clobbered: got %q want appstage", updated.StageHostname)
	}
	if updated.Mode != topology.ModeStandard {
		t.Errorf("mode clobbered: got %q want %q", updated.Mode, topology.ModeStandard)
	}
}

// TestLive_GitPushSetup_StageHalfRejected pins the stage-half
// classification — calling git-push-setup on a stage hostname returns
// a structured error pointing the agent at the dev half. Same
// behavioral retro pattern Karel saw (stage hard-reject is good UX).
func TestLive_GitPushSetup_StageHalfRejected(t *testing.T) {
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.ModeStandard,
		StageHostname:    "appstage",
		CloseDeployMode:  topology.CloseModeGitPush,
		GitPushState:     topology.GitPushUnconfigured,
		BootstrapSession: "live-test-stage-half",
		BootstrappedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	rt := runtime.Info{InContainer: true}
	resp, _, err := handleGitPushSetup(
		WorkflowInput{Action: "git-push-setup", Service: "appstage"},
		stateDir,
		rt,
	)
	if err != nil {
		t.Fatalf("stage-half call: %v", err)
	}
	text := extractText(resp)

	if !strings.Contains(text, "stage half") {
		t.Errorf("stage-half rejection should explain the classification, got:\n%s", text)
	}
	if !strings.Contains(text, "appdev") {
		t.Errorf("stage-half rejection should point at the dev hostname for retry, got:\n%s", text)
	}
}
