package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestGitPushSetupLocal_RejectsGitToken pins the local-mode contract:
// gitToken is never collected in local mode because the user's local git
// already holds credentials. An agent that supplies it (e.g. confused by
// container-mode atom guidance) must be redirected explicitly.
func TestGitPushSetupLocal_RejectsGitToken(t *testing.T) {
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	result, _, _ := handleGitPushSetup(
		context.Background(), nil, nil, "test-project",
		WorkflowInput{
			Service:   "appdev",
			RemoteURL: "https://github.com/example/app.git",
			GitToken:  "ghp_unwanted_in_local_mode",
		},
		stateDir,
		runtime.Info{InContainer: false},
	)
	if !result.IsError {
		t.Fatalf("expected gitToken rejection in local mode, got success: %s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, "does not collect gitToken") {
		t.Errorf("error should explain local-mode token convention; got: %s", body)
	}
	// Token must NOT echo into the response body.
	if strings.Contains(body, "ghp_unwanted_in_local_mode") {
		t.Errorf("token leaked into response: %s", body)
	}
}

// TestGitPushSetupLocal_ProbeFailure_NoStateMutation is the local-mode
// counterpart to the container-mode probe-failure test. Probe-first: when
// the local probe fails, meta is left untouched.
func TestGitPushSetupLocal_ProbeFailure_NoStateMutation(t *testing.T) {
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	// Stub probe to fail; sync should never be reached.
	defer setLocalGitProbeReader(func(context.Context, string, string) error {
		return errors.New("git ls-remote: exit status 128: authentication failed")
	})()
	syncCalled := false
	defer setLocalGitOriginSyncer(func(context.Context, string, string) error {
		syncCalled = true
		return nil
	})()

	result, _, _ := handleGitPushSetup(
		context.Background(), nil, nil, "test-project",
		WorkflowInput{
			Service:   "appdev",
			RemoteURL: "https://github.com/example/app.git",
		},
		stateDir,
		runtime.Info{InContainer: false},
	)
	if !result.IsError {
		t.Fatalf("expected probe failure, got success: %s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, "GIT_TOKEN_INVALID") {
		t.Errorf("error code should be GIT_TOKEN_INVALID; got: %s", body)
	}
	if !strings.Contains(body, "NO project state was modified") {
		t.Errorf("response should confirm no mutation; got: %s", body)
	}
	if syncCalled {
		t.Error("origin sync must NOT run after probe failure")
	}

	// Meta state must not show configured.
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta != nil && meta.GitPushState == topology.GitPushConfigured {
		t.Errorf("probe failure should NOT stamp configured; meta.GitPushState=%q", meta.GitPushState)
	}
}

// TestGitPushSetupLocal_Success_NextStepStatesWriteAuthProven pins R0: the
// local probe is now write-class (RunGitAuthProbeLocal = push --dry-run), so the
// success nextStep must state write AUTHENTICATION is proven — NOT the stale
// "read probe / Write permission is NOT proven yet" the read-only ls-remote
// probe shipped. What genuinely remains unproven is fast-forwardability (a
// divergent remote), which the wording must still hedge.
func TestGitPushSetupLocal_Success_NextStepStatesWriteAuthProven(t *testing.T) {
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	defer setLocalGitProbeReader(func(context.Context, string, string) error { return nil })()
	defer setLocalGitOriginSyncer(func(context.Context, string, string) error { return nil })()

	result, _, _ := handleGitPushSetup(
		context.Background(), nil, nil, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git"},
		stateDir,
		runtime.Info{InContainer: false},
	)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(result))
	}
	body := extractText(result)
	for _, stale := range []string{"read probe", "NOT proven", "read-auth"} {
		if strings.Contains(body, stale) {
			t.Errorf("nextStep still carries stale read-only wording %q (probe is write-class now): %s", stale, body)
		}
	}
	if !strings.Contains(body, "non-fast-forward") {
		t.Errorf("nextStep should hedge the remaining unknown (non-fast-forward divergence): %s", body)
	}
}

// TestGitPushSetupLocal_OriginSyncFailure_NoMetaStamp pins that even if
// the probe succeeds, an origin-sync failure surfaces without stamping
// configured — partial side effects are surfaced, not papered over.
func TestGitPushSetupLocal_OriginSyncFailure_NoMetaStamp(t *testing.T) {
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	defer setLocalGitProbeReader(func(context.Context, string, string) error { return nil })()
	defer setLocalGitOriginSyncer(func(context.Context, string, string) error {
		return errors.New("git remote set-url: exit status 128")
	})()

	result, _, _ := handleGitPushSetup(
		context.Background(), nil, nil, "test-project",
		WorkflowInput{
			Service:   "appdev",
			RemoteURL: "https://github.com/example/app.git",
		},
		stateDir,
		runtime.Info{InContainer: false},
	)
	if !result.IsError {
		t.Fatalf("expected origin-sync failure, got success: %s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, "origin sync") {
		t.Errorf("error should mention origin sync; got: %s", body)
	}

	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta != nil && meta.GitPushState == topology.GitPushConfigured {
		t.Errorf("origin-sync failure should NOT stamp configured; meta.GitPushState=%q", meta.GitPushState)
	}
}
