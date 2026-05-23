package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
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
}

func (s *containerSSHStub) ExecSSH(_ context.Context, _, cmd string) ([]byte, error) {
	s.commands = append(s.commands, cmd)
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
			"git ls-remote": errors.New("exit status 128: authentication failed"),
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
	if !strings.Contains(ssh.commands[0], "git ls-remote") {
		t.Errorf("first (and only) SSH call should be the probe; got: %s", ssh.commands[0])
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
			"git ls-remote": errors.New("exit status 128"),
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
