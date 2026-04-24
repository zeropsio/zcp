// Tests for: ops/service_git_init.go — InitServiceGit canonical primitive.
package ops

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestInitServiceGit_HappyPath verifies the canonical command emitted by
// InitServiceGit contains all the pieces downstream deploys rely on: the
// /var/www cd, the test-d guarded init, and both identity config lines
// pointing at ops.DeployGitIdentity.
func TestInitServiceGit_HappyPath(t *testing.T) {
	t.Parallel()

	ssh := &mockSSHDeployer{}
	err := InitServiceGit(context.Background(), ssh, "probe")
	if err != nil {
		t.Fatalf("InitServiceGit: unexpected error: %v", err)
	}

	if len(ssh.calls) != 1 {
		t.Fatalf("expected 1 SSH call, got %d", len(ssh.calls))
	}
	call := ssh.calls[0]
	if call.hostname != "probe" {
		t.Errorf("hostname: got %q, want %q", call.hostname, "probe")
	}
	if call.background {
		t.Errorf("expected foreground ExecSSH, got background spawn")
	}

	wantSubstrings := []string{
		"cd /var/www",
		"test -d .git || git init -q -b main",
		"git config user.email 'agent@zerops.io'",
		"git config user.name 'Zerops Agent'",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(call.command, want) {
			t.Errorf("command missing %q\nfull command: %s", want, call.command)
		}
	}
}

// TestInitServiceGit_Idempotent verifies two back-to-back calls against
// the same hostname emit byte-identical commands — no hidden per-call
// state drift (counters, timestamps, random nonces).
func TestInitServiceGit_Idempotent(t *testing.T) {
	t.Parallel()

	ssh := &mockSSHDeployer{}
	for i := range 2 {
		if err := InitServiceGit(context.Background(), ssh, "probe"); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}
	if len(ssh.calls) != 2 {
		t.Fatalf("expected 2 SSH calls, got %d", len(ssh.calls))
	}
	if ssh.calls[0].command != ssh.calls[1].command {
		t.Errorf("commands differ across idempotent calls:\n  [0] %s\n  [1] %s", ssh.calls[0].command, ssh.calls[1].command)
	}
}

// TestInitServiceGit_EmptyHostname rejects upfront — no SSH call emitted,
// clear INVALID_PARAMETER error surfaced to the caller.
func TestInitServiceGit_EmptyHostname(t *testing.T) {
	t.Parallel()

	ssh := &mockSSHDeployer{}
	err := InitServiceGit(context.Background(), ssh, "")
	if err == nil {
		t.Fatal("InitServiceGit(\"\"): expected error, got nil")
	}
	if !strings.Contains(err.Error(), "hostname") {
		t.Errorf("error should mention hostname: %v", err)
	}
	if len(ssh.calls) != 0 {
		t.Errorf("no SSH call expected on empty hostname, got %d", len(ssh.calls))
	}
}

// TestInitServiceGit_NilSSH rejects upfront too — the caller must supply
// a deployer. Mirrors the guard in DeploySSH; keeps local-env callers
// (where sshDeployer == nil) from triggering a nil-pointer panic.
func TestInitServiceGit_NilSSH(t *testing.T) {
	t.Parallel()

	err := InitServiceGit(context.Background(), nil, "probe")
	if err == nil {
		t.Fatal("InitServiceGit(nil ssh): expected error, got nil")
	}
	if !strings.Contains(err.Error(), "SSH deployer") {
		t.Errorf("error should mention SSH deployer: %v", err)
	}
}

// TestInitServiceGit_SSHFailure verifies the transport error propagates
// with a "init git on <hostname>" wrap so diagnostics stay legible when
// the error bubbles through autoMountTargets → bootstrap response.
func TestInitServiceGit_SSHFailure(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("ssh: connection refused")
	ssh := &mockSSHDeployer{err: sentinel}
	err := InitServiceGit(context.Background(), ssh, "probe")
	if err == nil {
		t.Fatal("InitServiceGit: expected error when ExecSSH fails")
	}
	if !strings.Contains(err.Error(), "init git on probe") {
		t.Errorf("error should wrap with 'init git on probe': %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("sentinel error should propagate via errors.Is: %v", err)
	}
}
