package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// containerSSHStub is a minimal SSHDeployer test double. ExecSSH dispatches
// errors keyed by substring match — keeping container-mode tests focused on
// the call order without dragging in the full mockSSHDeployer surface.
type containerSSHStub struct {
	commands []string
	errOn    map[string]error
	// dispatch, when set, fully owns the response per command — for tests
	// that need to distinguish commands a substring map cannot (e.g. the
	// inline-token probe vs the session probe, which differ only by the
	// GIT_TOKEN='…' prefix).
	dispatch func(cmd string) ([]byte, error)
}

func (s *containerSSHStub) ExecSSH(_ context.Context, _, cmd string) ([]byte, error) {
	s.commands = append(s.commands, cmd)
	if s.dispatch != nil {
		return s.dispatch(cmd)
	}
	for substr, err := range s.errOn {
		if strings.Contains(cmd, substr) {
			return nil, err
		}
	}
	return []byte("ok"), nil
}

func (s *containerSSHStub) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return []byte("ok"), nil
}

// Compile-time check the stub satisfies the interface.
var _ ops.SSHDeployer = (*containerSSHStub)(nil)

// writePairMetaForGitPushSetup writes a standard-pair ServiceMeta keyed on
// "appdev" with stage "appstage" — the canonical fixture every test in
// this file reuses. Wrapper exists so the container-test file owns its
// fixture independent of other test helpers (writeBootstrappedMeta in
// workflow_export_test.go has different default fields and shouldn't be
// shared by accident).
func writePairMetaForGitPushSetup(t *testing.T, stateDir string) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-23",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// TestGitPushSetupContainer_RequiresGitToken pins that container mode
// rejects confirm calls without gitToken — handler cannot probe without it,
// so the wire-shape contract refuses early instead of stamping a lie.
func TestGitPushSetupContainer_RequiresGitToken(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	result, _, _ := handleGitPushSetup(
		context.Background(), nil, nil, "test-project",
		WorkflowInput{
			Service:   "appdev",
			RemoteURL: "https://github.com/example/app.git",
		},
		stateDir,
		runtime.Info{InContainer: true},
	)
	if !result.IsError {
		t.Fatalf("expected error for missing gitToken in container mode, got success: %s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, "gitToken") {
		t.Errorf("error should mention gitToken; got: %s", body)
	}

	// Critical: no meta state mutation on the refusal path.
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta != nil && meta.GitPushState == topology.GitPushConfigured {
		t.Errorf("missing-gitToken refusal should NOT stamp configured; meta.GitPushState=%q", meta.GitPushState)
	}
}

// TestGitPushSetupContainer_HTTPSOnly_RejectsSCPForm pins HTTPS-only
// enforcement: scp-form SSH remotes (git@host:owner/repo) don't auth via
// .netrc + PAT — handler refuses early before attempting a probe that
// would fail confusingly. SSH deploy-key flow is a separate feature.
func TestGitPushSetupContainer_HTTPSOnly_RejectsSCPForm(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	result, _, _ := handleGitPushSetup(
		context.Background(), nil, nil, "test-project",
		WorkflowInput{
			Service:   "appdev",
			RemoteURL: "git@github.com:example/app.git",
			GitToken:  "ghp_dummy",
		},
		stateDir,
		runtime.Info{InContainer: true},
	)
	if !result.IsError {
		t.Fatalf("expected SCP-form rejection, got success: %s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, "HTTPS") || !strings.Contains(body, "SCP-form") {
		t.Errorf("error should explain HTTPS-only + SCP rejection; got: %s", body)
	}
}

// TestGitPushSetupContainer_ProbeFailure_NoStateMutation is the load-
// bearing invariant of probe-first design: when the auth probe fails,
// project state stays untouched. No env write, no restart, no meta stamp.
// Agent fixes inputs and re-calls — idempotent.
func TestGitPushSetupContainer_ProbeFailure_NoStateMutation(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	ssh := &containerSSHStub{
		errOn: map[string]error{
			"ls-remote": errors.New("exit status 128: authentication failed"),
		},
	}

	// platform.Client is nil — if any code path tries to write env or
	// restart, the test will panic. Probe-first means we never reach
	// those side effects on failure.
	result, _, _ := handleGitPushSetup(
		context.Background(), nil, ssh, "test-project",
		WorkflowInput{
			Service:   "appdev",
			RemoteURL: "https://github.com/example/app.git",
			GitToken:  "ghp_bad_token",
		},
		stateDir,
		runtime.Info{InContainer: true},
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

	// Meta state must not show configured.
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta != nil && meta.GitPushState == topology.GitPushConfigured {
		t.Errorf("probe failure should NOT stamp configured; meta.GitPushState=%q", meta.GitPushState)
	}

	// Only one SSH call happened (the probe) — origin sync should NOT fire.
	if len(ssh.commands) != 1 {
		t.Errorf("expected exactly 1 SSH call (probe only); got %d: %v", len(ssh.commands), ssh.commands)
	}
	if !strings.Contains(ssh.commands[0], "ls-remote") {
		t.Errorf("first (and only) SSH call should be the probe; got: %s", ssh.commands[0])
	}
}

// TestGitPushSetupContainer_SessionAuthFails_NoStamp is the XCUT-2
// successor pin. The inline probe + origin-sync + env-write succeed, but
// the post-write SESSION probe (fresh SSH session authenticating with the
// just-written secret — replaces the retired container restart) never
// passes. The handler MUST NOT stamp GitPushState=configured.
func TestGitPushSetupContainer_SessionAuthFails_NoStamp(t *testing.T) {
	// non-parallel: narrows the package-level session-auth retry policy.
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	prevAttempts, prevDelay := gitPushSessionAuthAttempts, gitPushSessionAuthDelay
	gitPushSessionAuthAttempts, gitPushSessionAuthDelay = 2, 0
	t.Cleanup(func() { gitPushSessionAuthAttempts, gitPushSessionAuthDelay = prevAttempts, prevDelay })

	// Inline probe (carries the candidate token via GIT_TOKEN='…' prefix)
	// passes; the post-write SESSION probe (same ls-remote WITHOUT the
	// token prefix) fails — the secret did not reach fresh sessions.
	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "ls-remote") && !strings.Contains(cmd, "GIT_TOKEN='") {
				return nil, errors.New("exit status 128: authentication failed")
			}
			return []byte("ok"), nil
		},
	}

	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, ssh, "test-project",
		WorkflowInput{
			Service:   "appdev",
			RemoteURL: "https://github.com/example/app.git",
			GitToken:  "ghp_ok",
		},
		stateDir,
		runtime.Info{InContainer: true},
	)
	if !result.IsError {
		t.Fatalf("session-auth failure should surface an error, got success: %s", extractText(result))
	}
	// XCUT-2 successor: state must NOT be stamped configured when a fresh
	// session could not authenticate with the just-written secret.
	meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
	if meta != nil && meta.GitPushState == topology.GitPushConfigured {
		t.Errorf("session-auth failure must NOT stamp configured; meta.GitPushState=%q", meta.GitPushState)
	}
}

// TestGitPushSetupContainer_SameRemoteNewToken_Rotates pins the rotation
// path (spec-git-delivery-target §4): a confirm re-call on an
// already-configured pair with the SAME canonical remote and a NON-EMPTY
// gitToken is rotation intent — full probe → service-secret re-write →
// fresh-session verification → stamp. No container restart anywhere.
// The token-blind short-circuit forced agents into the raw zerops_env
// bypass (prod.txt T3).
func TestGitPushSetupContainer_SameRemoteNewToken_Rotates(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/example/app.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-23",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	ssh := &containerSSHStub{}
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, ssh, "test-project",
		// Same canonical remote (.git suffix differs) + fresh token.
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app", GitToken: "ghp_rotated_token"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("rotation re-call should succeed, got error: %s", extractText(result))
	}
	body := extractText(result)
	if strings.Contains(body, "already-configured") {
		t.Fatalf("non-empty gitToken on same remote must NOT short-circuit; got: %s", body)
	}
	if !strings.Contains(body, "rotated") {
		t.Errorf("rotation response should carry the rotated marker; got: %s", body)
	}
	// The full chain ran: inline probe + .git presence check + origin
	// sync (with helper assert) + session probe = 4 SSH calls; the first
	// carries the candidate token, the last must NOT (session env).
	if len(ssh.commands) != 4 {
		t.Fatalf("rotation should run probe+presence+origin+session (4 SSH calls); got %d: %v", len(ssh.commands), ssh.commands)
	}
	if !strings.Contains(ssh.commands[0], "GIT_TOKEN='ghp_rotated_token'") {
		t.Errorf("first SSH call should probe the NEW token inline; got: %s", ssh.commands[0])
	}
	if !strings.Contains(ssh.commands[1], "test -d /var/www/.git") {
		t.Errorf("second SSH call should be the presence check; got: %s", ssh.commands[1])
	}
	if !strings.Contains(ssh.commands[2], "credential.https://github.com.helper") {
		t.Errorf("third SSH call should assert the url-scoped helper; got: %s", ssh.commands[2])
	}
	if strings.Contains(ssh.commands[3], "GIT_TOKEN='") {
		t.Errorf("fourth SSH call must verify the SESSION env (no inline token); got: %s", ssh.commands[3])
	}
	// Token must never echo.
	if strings.Contains(body, "ghp_rotated_token") {
		t.Errorf("rotated token leaked into response: %s", body)
	}
}

// TestGitPushSetupContainer_TokenNeverEchoed pins that the token value
// never appears in any response, regardless of probe outcome. Sentinel
// scan against the full JSON body.
func TestGitPushSetupContainer_TokenNeverEchoed(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)
	const sentinelToken = "ghp_sentinel_should_never_echo_VWXYZ"

	ssh := &containerSSHStub{
		errOn: map[string]error{
			"ls-remote": errors.New("exit status 128"),
		},
	}
	result, _, _ := handleGitPushSetup(
		context.Background(), nil, ssh, "test-project",
		WorkflowInput{
			Service:   "appdev",
			RemoteURL: "https://github.com/example/app.git",
			GitToken:  sentinelToken,
		},
		stateDir,
		runtime.Info{InContainer: true},
	)
	body := extractText(result)
	if strings.Contains(body, sentinelToken) {
		t.Errorf("token leaked into response body: %s", body)
	}
}

// TestGitPushSetupContainer_ProbeFailure_SurfacesGitStderr pins B6/F36: the
// probe error must carry the git stderr the SSHExecError holds (so the agent
// learns "Repository not found" vs "Authentication failed"), a structured
// failureClassification, and the user-owned-credential contract (B6b) — no
// fabricated tokens.
func TestGitPushSetupContainer_ProbeFailure_SurfacesGitStderr(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	ssh := &containerSSHStub{
		errOn: map[string]error{
			"ls-remote": &platform.SSHExecError{
				Hostname: "appdev",
				Output:   "remote: Repository not found.\nfatal: repository 'https://github.com/example/app.git/' not found",
				Err:      errors.New("exit status 128"),
			},
		},
	}

	result, _, _ := handleGitPushSetup(
		context.Background(), nil, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_token"},
		stateDir, runtime.Info{InContainer: true},
	)
	if !result.IsError {
		t.Fatalf("expected probe failure, got success: %s", extractText(result))
	}
	body := extractText(result)
	// The distinguishing git stderr must be present (was swallowed by %v on
	// SSHExecError.Error(), which renders only "ssh appdev: exit status 128").
	if !strings.Contains(body, "Repository not found") {
		t.Errorf("probe error must surface the git stderr; got: %s", body)
	}
	// Structured classification with the repo-not-found cause.
	if !strings.Contains(body, "failureClassification") || !strings.Contains(body, "transport:git-repo-not-found") {
		t.Errorf("probe error must carry the repo-not-found classification; got: %s", body)
	}
	// Credential contract (B6b): never fabricate a token, ask the user.
	if !strings.Contains(body, "user-held secret") {
		t.Errorf("credential error must carry the user-owned-credential contract; got: %s", body)
	}
	// No mutation on probe failure.
	if meta, _ := workflow.ReadServiceMeta(stateDir, "appdev"); meta != nil && meta.GitPushState == topology.GitPushConfigured {
		t.Errorf("probe failure must not stamp configured")
	}
}

// TestGitPushSetupContainer_AlreadyConfigured_NoRestart pins B6c/GPS-5: a
// confirm re-called on a pair already configured with the same canonical
// remote short-circuits — no SSH probe, no env write, no container restart.
func TestGitPushSetupContainer_AlreadyConfigured_NoRestart(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/example/app.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-23",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	ssh := &containerSSHStub{}
	// Same canonical remote (".git" suffix differs) — must still short-circuit.
	result, _, _ := handleGitPushSetup(
		context.Background(), nil, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("already-configured re-call should succeed, got error: %s", extractText(result))
	}
	if body := extractText(result); !strings.Contains(body, "already-configured") {
		t.Errorf("expected already-configured short-circuit; got: %s", body)
	}
	// Check-before-claim: the short-circuit performs exactly ONE SSH call
	// — the .git presence check (a missing repo flips it into the
	// reconstruction path instead of claiming working wiring). No probe,
	// no origin sync, no env write.
	if len(ssh.commands) != 1 || !strings.Contains(ssh.commands[0], "test -d /var/www/.git") {
		t.Errorf("short-circuit must perform only the presence check; got %d: %v", len(ssh.commands), ssh.commands)
	}
}
