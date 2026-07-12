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
// bearing invariant of probe-first design: when the auth probe fails, NO
// PROJECT state is mutated — no secret write, no origin sync, no meta
// stamp, no restart. Agent fixes inputs and re-calls — idempotent.
//
// This is narrower than "nothing changed": the pre-probe HEAD-ensure step
// (F2) DOES self-heal the push source's LOCAL repo (init-if-missing,
// identity filled if absent, HEAD guaranteed) unconditionally, before the
// probe even runs — that local repair is best-effort and happens
// regardless of whether the probe subsequently succeeds or fails (Codex
// diff-review finding 2: the old "NO project state was modified" wording
// overclaimed this). The test below asserts BOTH halves: the local
// self-heal call demonstrably ran (modeling the real git state instead of
// just trusting the response prose), and the narrower project-state
// guarantee holds in the response text and in meta.
func TestGitPushSetupContainer_ProbeFailure_NoStateMutation(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	ssh := &containerSSHStub{
		errOn: map[string]error{
			// The candidate probe is now a WRITE-proof (push --dry-run) — a
			// garbage/read-only token fails HERE, before any secret is written,
			// so an existing working token is never clobbered (#2).
			"push --dry-run": errors.New("exit status 128: authentication failed"),
		},
	}

	// platform.Client is nil — if any code path tries to write env or
	// restart, the test will panic. Probe-first means we never reach
	// those PROJECT-state side effects on failure (the local self-heal
	// below is not one of them — it's SSH-only, no platform API call).
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
	if !strings.Contains(body, "NO remote ref, secret, origin, or meta state was modified") {
		t.Errorf("response should confirm the narrower PROJECT-state guarantee (not a blanket 'nothing changed' claim); got: %s", body)
	}

	// Meta state must not show configured.
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta != nil && meta.GitPushState == topology.GitPushConfigured {
		t.Errorf("probe failure should NOT stamp configured; meta.GitPushState=%q", meta.GitPushState)
	}

	// Exactly 2 SSH calls happened: the pre-probe local self-heal (F2), then
	// the probe itself — origin sync should NOT fire. The self-heal call
	// carries the HEAD-guarantee marker commit shape, modeling that the
	// local repo really was touched even though the probe then failed.
	if len(ssh.commands) != 2 {
		t.Fatalf("expected exactly 2 SSH calls (self-heal + probe); got %d: %v", len(ssh.commands), ssh.commands)
	}
	if strings.Contains(ssh.commands[0], "push --dry-run") {
		t.Errorf("first SSH call should be the local self-heal, not the probe; got: %s", ssh.commands[0])
	}
	if !strings.Contains(ssh.commands[0], "-m 'zcp init'") || !strings.Contains(ssh.commands[0], "test -d .git || git init") {
		t.Errorf("first SSH call should be the full self-heal chain (init guard + HEAD guarantee); got: %s", ssh.commands[0])
	}
	if !strings.Contains(ssh.commands[1], "push --dry-run") {
		t.Errorf("second SSH call should be the write-auth probe; got: %s", ssh.commands[1])
	}
}

// TestGitPushSetupContainer_GarbageTokenSameRemote_DoesNotClobber is the #2
// destruction-prevention pin: a service ALREADY git-push-configured (working
// token) is re-called on the SAME remote with a garbage token (e.g. an agent
// fabricating a placeholder). The write-auth probe fails, so probe-first leaves
// the existing working secret UNTOUCHED — the old read-only probe passed any
// token on a public repo and clobbered the working secret. nil client: any env
// write would panic, proving no mutation reached the secret.
func TestGitPushSetupContainer_GarbageTokenSameRemote_DoesNotClobber(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-23",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/me/app.git",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	ssh := &containerSSHStub{
		errOn: map[string]error{
			"push --dry-run": errors.New("exit status 128: authentication failed"),
		},
	}
	result, _, _ := handleGitPushSetup(
		context.Background(), nil, ssh, "test-project",
		WorkflowInput{
			Service:   "appdev",
			RemoteURL: "https://github.com/me/app.git", // same remote → rotation intent
			GitToken:  "github_pat_garbage_placeholder",
		},
		stateDir,
		runtime.Info{InContainer: true},
	)
	if !result.IsError {
		t.Fatalf("garbage token must fail the write-proof, got success: %s", extractText(result))
	}
	// The existing working secret + configured state must be preserved.
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta == nil || meta.GitPushState != topology.GitPushConfigured {
		t.Errorf("configured state must survive a failed rotation; got %+v", meta)
	}
	if meta != nil && meta.RemoteURL != "https://github.com/me/app.git" {
		t.Errorf("RemoteURL must stay intact; got %q", meta.RemoteURL)
	}
	// Probe-first: only the pre-step-0 presence check (this pair is
	// GitPushState=configured — Codex finding 1's ordering fix) + the
	// local self-heal (F2) + the probe ran, no origin sync / env write.
	if len(ssh.commands) != 3 {
		t.Fatalf("expected exactly 3 SSH calls (presence + self-heal + probe); got %d: %v", len(ssh.commands), ssh.commands)
	}
	if !strings.Contains(ssh.commands[0], "test -d /var/www/.git") {
		t.Errorf("first SSH call should be the presence check; got: %s", ssh.commands[0])
	}
	if !strings.Contains(ssh.commands[2], "push --dry-run") {
		t.Errorf("third SSH call should be the write-auth probe; got: %s", ssh.commands[2])
	}
}

// TestGitPushSetupContainer_ShallowCloneUnshallowFails_BlocksBeforeOriginSync
// pins F1b: a shallow/incomplete recipe clone whose `git fetch --unshallow`
// cannot recover the object graph must BLOCK before the origin sync — so the
// original (recipe) remote stays intact for manual recovery, never silently
// overwritten. Reproduces the p2 #1 trap.
func TestGitPushSetupContainer_ShallowCloneUnshallowFails_BlocksBeforeOriginSync(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			// Probe (push --dry-run) succeeds; the shallow-fix fetch fails.
			if strings.Contains(cmd, "fetch --unshallow") {
				return []byte("ZCP_UNSHALLOW_FAIL https://github.com/zeropsio/recipe-laravel.git\n"), nil
			}
			return []byte("ok"), nil
		},
	}

	// nil client: if the handler reaches env write / restart, it panics —
	// proving the blocker fired before any state mutation.
	result, _, _ := handleGitPushSetup(
		context.Background(), nil, ssh, "test-project",
		WorkflowInput{
			Service:   "appdev",
			RemoteURL: "https://github.com/me/app.git",
			GitToken:  "ghp_good",
		},
		stateDir,
		runtime.Info{InContainer: true},
	)
	if !result.IsError {
		t.Fatalf("shallow+unrecoverable must block, got success: %s", extractText(result))
	}
	body := extractText(result)
	for _, want := range []string{"shallow", "fetch --unshallow", "NO remote ref, secret, origin, or meta state was modified"} {
		if !strings.Contains(body, want) {
			t.Errorf("blocker should mention %q; got: %s", want, body)
		}
	}
	// Origin must NOT have been overwritten — no set-url ran.
	for _, c := range ssh.commands {
		if strings.Contains(c, "set-url origin") || strings.Contains(c, "git remote add origin") {
			t.Errorf("origin must stay intact on shallow block; saw: %s", c)
		}
	}
	// Meta must not be stamped configured.
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta != nil && meta.GitPushState == topology.GitPushConfigured {
		t.Errorf("shallow block must NOT stamp configured; got %q", meta.GitPushState)
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
	// The full chain ran: .git presence check (captured BEFORE any self-heal
	// — Codex finding 1's ordering fix) + local self-heal (F2) + inline
	// probe + origin sync (with helper assert) + session probe = 5 SSH
	// calls; the probe carries the candidate token, the last must NOT
	// (session env). This fixture's stub reports "ok" (not "absent") for
	// the presence check, so needsReconstruct is false and the self-heal
	// still runs — the reconstruction-instead-of-self-heal path is covered
	// separately by TestGitPushSetupContainer_RotationWithToken_MissingGitStillReconstructs.
	if len(ssh.commands) != 5 {
		t.Fatalf("rotation should run presence+self-heal+probe+origin+session (5 SSH calls); got %d: %v", len(ssh.commands), ssh.commands)
	}
	if !strings.Contains(ssh.commands[0], "test -d /var/www/.git") {
		t.Errorf("first SSH call should be the presence check; got: %s", ssh.commands[0])
	}
	if strings.Contains(ssh.commands[1], "push --dry-run") || strings.Contains(ssh.commands[1], "GIT_TOKEN=") {
		t.Errorf("second SSH call should be the local self-heal, not the probe; got: %s", ssh.commands[1])
	}
	if !strings.Contains(ssh.commands[2], "GIT_TOKEN='ghp_rotated_token'") {
		t.Errorf("third SSH call should probe the NEW token inline; got: %s", ssh.commands[2])
	}
	if !strings.Contains(ssh.commands[3], "credential.https://github.com.helper") {
		t.Errorf("fourth SSH call should assert the url-scoped helper; got: %s", ssh.commands[3])
	}
	if strings.Contains(ssh.commands[4], "GIT_TOKEN='") {
		t.Errorf("fifth SSH call must verify the SESSION env (no inline token); got: %s", ssh.commands[4])
	}
	// Token must never echo.
	if strings.Contains(body, "ghp_rotated_token") {
		t.Errorf("rotated token leaked into response: %s", body)
	}
}

// TestGitPushSetupContainer_RotationWithToken_MissingGitStillReconstructs
// is the Codex diff-review finding-1 regression pin: a configured pair,
// re-confirmed with a NEW token (rotation-with-token — the ONE path that
// does NOT take the early gitPushConfiguredRecall short-circuit, since
// that short-circuit only fires when gitToken is empty), whose
// /var/www/.git has actually vanished must still RECONSTRUCT from the
// recorded remote. Before the fix, the pre-probe local self-heal (step 2)
// ran unconditionally and created a fresh .git with only the 'zcp init'
// marker commit BEFORE the presence check could observe the true state —
// so BuildGitReconstructCommand's own `test ! -d .git` guard would then
// no-op post-heal, and setup would report success on a repo carrying none
// of the recorded remote's real history. The presence check must run
// BEFORE the self-heal, and the self-heal must be skipped whenever
// reconstruction is going to run.
func TestGitPushSetupContainer_RotationWithToken_MissingGitStillReconstructs(t *testing.T) {
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

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "test -d /var/www/.git") {
				return []byte("absent"), nil
			}
			if strings.Contains(cmd, "git status --porcelain") {
				return []byte(""), nil // clean tree after reconstruction
			}
			return []byte("ok"), nil
		},
	}
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_rotated_token"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("rotation with missing .git should reconstruct, not fail: %s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, "reconstructed") {
		t.Errorf("response should carry the reconstructed marker; got: %s", body)
	}

	// The local self-heal (step 2) must NOT have run — it would have
	// created a marker-only .git and masked the reconstruction below.
	for _, cmd := range ssh.commands {
		if strings.Contains(cmd, "-m 'zcp init'") {
			t.Errorf("local self-heal ran despite pending reconstruction — would strand the repo on the marker commit instead of the recorded remote's history: %s", cmd)
		}
	}

	// The actual reconstruction command (init + identity + fetch + reset
	// against the RECORDED remote) must have run.
	var recon string
	for _, cmd := range ssh.commands {
		if strings.Contains(cmd, "if test ! -d .git") {
			recon = cmd
			break
		}
	}
	if recon == "" {
		t.Fatalf("reconstruction command never issued; commands: %v", ssh.commands)
	}
	if !strings.Contains(recon, "git remote add origin 'https://github.com/example/app.git'") {
		t.Errorf("reconstruction must target the RECORDED remote, not just any origin: %s", recon)
	}
	if !strings.Contains(recon, "fetch -q origin HEAD") || !strings.Contains(recon, "git reset -q FETCH_HEAD") {
		t.Errorf("reconstruction command missing fetch/reset onto the recorded remote's HEAD: %s", recon)
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

// TestGitPushSetupContainer_EnsuresRepoHeadBeforeProbe pins F2 item 2
// (Codex finding 3): a first-time configuration on a pair whose bootstrap
// meta exists but whose /var/www/.git never got InitServiceGit'd (its
// failure is swallowed at bootstrap — spec GLC-1) must self-heal the repo
// BEFORE the write-auth probe runs, so the probe is always the real `push
// --dry-run` proof — never its unborn-HEAD read-only ls-remote fallback,
// which a garbage/read-only token could still pass. The ensure call must
// be the FIRST SSH call, strictly before the probe.
func TestGitPushSetupContainer_EnsuresRepoHeadBeforeProbe(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir) // GitPushState unconfigured — first-time config

	ssh := &containerSSHStub{}
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})
	result, _, _ := handleGitPushSetup(
		context.Background(), client, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_good"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(result))
	}
	if len(ssh.commands) < 2 {
		t.Fatalf("expected at least 2 SSH calls (ensure + probe); got %d: %v", len(ssh.commands), ssh.commands)
	}
	first := ssh.commands[0]
	if strings.Contains(first, "push --dry-run") || strings.Contains(first, "ls-remote") {
		t.Errorf("first SSH call must be the HEAD-ensure, not the probe; got: %s", first)
	}
	for _, want := range []string{"test -d .git || git init -q -b main", "git rev-parse -q --verify HEAD", "commit-tree", "-m 'zcp init'"} {
		if !strings.Contains(first, want) {
			t.Errorf("first SSH call missing HEAD-ensure piece %q; got: %s", want, first)
		}
	}
	second := ssh.commands[1]
	if !strings.Contains(second, "push --dry-run") {
		t.Errorf("second SSH call should be the write-auth probe; got: %s", second)
	}
}
