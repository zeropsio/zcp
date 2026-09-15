// Tests for: git_push_remote_state.go — the git-push-setup success
// result's probe-time early warning (§4.4, `remote` block).
package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// noNetworkGitRunner is the test-safe stand-in for localGitDivergenceRunner:
// every call fails immediately, so probeGitPushRemoteState degrades to nil
// (best-effort) instead of a local-mode test shelling out to real git
// against the test process's actual working directory.
func noNetworkGitRunner(context.Context, string) ops.GitRunner {
	return func(args ...string) (string, error) {
		return "", errors.New("noNetworkGitRunner: real git disabled in tests")
	}
}

// gitPushSetupDivergenceDispatch builds a containerSSHStub dispatch func
// that answers the §4.4 probe steps (fetch / rev-list ×2 / merge-base)
// with the given canned values and "ok" for every other command in the
// git-push-setup confirm chain (presence, self-heal, probe, origin sync,
// session probe).
func gitPushSetupDivergenceDispatch(remoteAhead, localAhead string, fetchErr, mergeErr error) func(string) ([]byte, error) {
	return func(cmd string) ([]byte, error) {
		switch {
		case strings.Contains(cmd, "symbolic-ref"):
			// GF-7 trackedRef detection (git.CurrentBranch) — pin it to
			// "main" so these tests assert against a known ref regardless
			// of the container-side branch-detection mechanics.
			return []byte("main"), nil
		case strings.Contains(cmd, "'fetch'"):
			return nil, fetchErr
		case strings.Contains(cmd, "'HEAD..FETCH_HEAD'"):
			return []byte(remoteAhead), nil
		case strings.Contains(cmd, "'FETCH_HEAD..HEAD'"):
			return []byte(localAhead), nil
		case strings.Contains(cmd, "'merge-base'"):
			if mergeErr != nil {
				return nil, mergeErr
			}
			return []byte("abc123"), nil
		default:
			return []byte("ok"), nil
		}
	}
}

func gitPushSetupConfirmForRemoteState(t *testing.T, dispatch func(string) ([]byte, error)) *mcpCallResultForRemoteStateTest {
	t.Helper()
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)
	ssh := &containerSSHStub{dispatch: dispatch}
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, nil, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_token"},
		stateDir, runtime.Info{InContainer: true},
	)
	return &mcpCallResultForRemoteStateTest{t: t, body: extractText(result), isErr: result.IsError}
}

// mcpCallResultForRemoteStateTest is a tiny assertion helper so each
// TestGitPushSetup_RemoteState_* case reads as one call + a few checks.
type mcpCallResultForRemoteStateTest struct {
	t     *testing.T
	body  string
	isErr bool
}

func (r *mcpCallResultForRemoteStateTest) requireSuccess() {
	r.t.Helper()
	if r.isErr {
		r.t.Fatalf("expected git-push-setup success, got error: %s", r.body)
	}
}

// TestGitPushSetup_RemoteState_Empty pins the "first push" shape: the
// tracked ref does not exist on the remote yet (fetch fails with git's
// "couldn't find remote ref" rejection) — always safe, never a
// push-decision warning.
func TestGitPushSetup_RemoteState_Empty(t *testing.T) {
	t.Parallel()
	r := gitPushSetupConfirmForRemoteState(t, gitPushSetupDivergenceDispatch("0", "0",
		errors.New("fatal: couldn't find remote ref main"), nil))
	r.requireSuccess()
	if !strings.Contains(r.body, `"ref":"main"`) || !strings.Contains(r.body, `"state":"empty"`) {
		t.Errorf(`expected remote={"ref":"main","state":"empty"}; got: %s`, r.body)
	}
	if strings.Contains(r.body, "remoteStateWarning") {
		t.Errorf("empty state must not carry a push-decision warning: %s", r.body)
	}
}

// TestGitPushSetup_RemoteState_InSync pins the identical-histories shape:
// no divergence, no warning.
func TestGitPushSetup_RemoteState_InSync(t *testing.T) {
	t.Parallel()
	r := gitPushSetupConfirmForRemoteState(t, gitPushSetupDivergenceDispatch("0", "0", nil, nil))
	r.requireSuccess()
	if !strings.Contains(r.body, `"state":"in-sync"`) {
		t.Errorf(`expected state="in-sync"; got: %s`, r.body)
	}
	if strings.Contains(r.body, "remoteStateWarning") {
		t.Errorf("in-sync state must not carry a push-decision warning: %s", r.body)
	}
}

// TestGitPushSetup_RemoteState_Ahead pins the dangerous shape: the remote
// already carries commits this checkout lacks (e.g. a fresh GitHub repo
// seeded with a README) — the FIRST push would be rejected non-fast-
// forward, so the result names the three options (rebase/merge/
// replace-remote) up front, before any push is attempted.
func TestGitPushSetup_RemoteState_Ahead(t *testing.T) {
	t.Parallel()
	r := gitPushSetupConfirmForRemoteState(t, gitPushSetupDivergenceDispatch("2", "0", nil, nil))
	r.requireSuccess()
	if !strings.Contains(r.body, `"state":"ahead"`) {
		t.Errorf(`expected state="ahead"; got: %s`, r.body)
	}
	if !strings.Contains(r.body, "remoteStateWarning") {
		t.Fatalf("ahead state must carry a push-decision warning: %s", r.body)
	}
	for _, want := range []string{"rebase", "merge", "replace-remote"} {
		if !strings.Contains(r.body, want) {
			t.Errorf("remoteStateWarning should name option %q: %s", want, r.body)
		}
	}
}

// TestGitPushSetup_RemoteState_Unrelated pins the no-common-ancestor
// shape (merge-base finds nothing) — also a push-decision warning.
func TestGitPushSetup_RemoteState_Unrelated(t *testing.T) {
	t.Parallel()
	r := gitPushSetupConfirmForRemoteState(t, gitPushSetupDivergenceDispatch("4", "4", nil, errors.New("exit status 1")))
	r.requireSuccess()
	if !strings.Contains(r.body, `"state":"unrelated"`) {
		t.Errorf(`expected state="unrelated"; got: %s`, r.body)
	}
	if !strings.Contains(r.body, "remoteStateWarning") {
		t.Errorf("unrelated state must carry a push-decision warning: %s", r.body)
	}
}
