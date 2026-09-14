// Tests for: ops/git/runner.go — the Runner abstraction driving git
// commands either via local os/exec (LocalRunner) or over SSH (SSHRunner),
// so the deploy-from-commit resolve/archive/ledger functions work
// identically for the interactive-local and interactive-container
// transports (docs/spec-workflows.md §4.5).
package git

import (
	"context"
	"strings"
	"testing"
)

// fakeSSHExecutor records every command it was asked to run over SSH and
// returns scripted output, satisfying SSHExecutor.
type fakeSSHExecutor struct {
	calls  []fakeSSHCall
	output []byte
	err    error
}

type fakeSSHCall struct {
	hostname string
	command  string
}

func (f *fakeSSHExecutor) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	f.calls = append(f.calls, fakeSSHCall{hostname: hostname, command: command})
	return f.output, f.err
}

func TestLocalRunner_Run_ExecutesShellScriptInDir(t *testing.T) {
	dir := t.TempDir()
	r := LocalRunner{}
	stdout, _, err := r.Run(context.Background(), dir, "pwd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(stdout)
	want := dir
	// macOS symlinks /tmp -> /private/tmp; resolve via EvalSymlinks-free
	// suffix check since exact equality is brittle across platforms.
	if !strings.HasSuffix(got, strings.TrimPrefix(want, "/private")) && got != want {
		t.Errorf("pwd = %q, want a path ending like %q", got, want)
	}
}

func TestLocalRunner_Run_ReturnsStderrOnFailure(t *testing.T) {
	r := LocalRunner{}
	_, stderr, err := r.Run(context.Background(), t.TempDir(), "echo boom 1>&2; exit 3")
	if err == nil {
		t.Fatal("expected error for exit 3")
	}
	if !strings.Contains(stderr, "boom") {
		t.Errorf("stderr = %q, want it to contain boom", stderr)
	}
}

func TestSSHRunner_Run_ComposesCDAndScript(t *testing.T) {
	exec := &fakeSSHExecutor{output: []byte("ok")}
	r := SSHRunner{Executor: exec, Hostname: "appdev"}
	_, _, err := r.Run(context.Background(), "/var/www", "git rev-parse HEAD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(exec.calls) != 1 {
		t.Fatalf("ssh calls = %d, want 1", len(exec.calls))
	}
	if exec.calls[0].hostname != "appdev" {
		t.Errorf("hostname = %q, want appdev", exec.calls[0].hostname)
	}
	if !strings.Contains(exec.calls[0].command, "cd '/var/www'") {
		t.Errorf("command = %q, want it to cd into the dir", exec.calls[0].command)
	}
	if !strings.Contains(exec.calls[0].command, "git rev-parse HEAD") {
		t.Errorf("command = %q, want it to run the script", exec.calls[0].command)
	}
}

func TestMkTempDir_LocalRunner_UsesOSMkdirTempOutsideDir(t *testing.T) {
	repoDir := t.TempDir()
	tmp, err := MkTempDir(context.Background(), LocalRunner{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = RemoveTemp(context.Background(), LocalRunner{}, tmp) }()
	if strings.HasPrefix(tmp, repoDir) {
		t.Errorf("tmp dir %q must not be under repo dir %q", tmp, repoDir)
	}
}

func TestSSHRunner_Run_EmptyDir_OmitsCDPrefix(t *testing.T) {
	exec := &fakeSSHExecutor{output: []byte("/tmp/zcpXXXX\n")}
	r := SSHRunner{Executor: exec, Hostname: "appdev"}
	_, _, err := r.Run(context.Background(), "", "mktemp -d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(exec.calls) != 1 {
		t.Fatalf("ssh calls = %d, want 1", len(exec.calls))
	}
	cmd := exec.calls[0].command
	// `cd ''` is a bash error ("cd: '': No such file or directory") — an
	// empty dir must run the script directly, with no cd prefix at all.
	if strings.Contains(cmd, "cd ''") {
		t.Errorf("command = %q, must not contain cd '' for an empty dir", cmd)
	}
	if cmd != "mktemp -d" {
		t.Errorf("command = %q, want the bare script with no cd prefix", cmd)
	}
}

func TestMkTempDir_SSHRunner_RunsMktempDCommand(t *testing.T) {
	exec := &fakeSSHExecutor{output: []byte("/tmp/zcpXXXX\n")}
	r := SSHRunner{Executor: exec, Hostname: "appdev"}
	tmp, err := MkTempDir(context.Background(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmp != "/tmp/zcpXXXX" {
		t.Errorf("tmp = %q, want /tmp/zcpXXXX", tmp)
	}
	if len(exec.calls) != 1 || !strings.Contains(exec.calls[0].command, "mktemp -d") {
		t.Fatalf("calls = %+v, want one call containing mktemp -d", exec.calls)
	}
}
