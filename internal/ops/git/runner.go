// Package git drives the git operations behind zerops_deploy's sha
// parameter — resolving a commit, materializing it into a temp directory
// outside the working tree, and recording the deploy-from-commit ledger in
// refs/zcp/* (docs/spec-workflows.md §4.5).
//
// Every operation goes through a Runner so the same logic works for both
// deploy transports: LocalRunner shells out via os/exec when the repo lives
// on the caller's disk (interactive-local deploy); SSHRunner composes the
// identical shell script into one remote command when the repo lives in a
// container (interactive-container deploy). This package must not import
// internal/ops (that would cycle back through the caller) — SSHExecutor is
// a minimal structural subset of ops.SSHDeployer's ExecSSH method so a real
// ops.SSHDeployer value satisfies it without any adapter.
package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
)

// Runner executes script (a shell script, run via `sh -c`) rooted at dir.
type Runner interface {
	Run(ctx context.Context, dir, script string) (stdout, stderr string, err error)
}

// LocalRunner is the production Runner for the interactive-local deploy
// transport, where dir is a real path on the machine running zcp.
type LocalRunner struct{}

func (LocalRunner) Run(ctx context.Context, dir, script string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	cmd.Dir = dir
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return out.String(), errBuf.String(), err
}

// SSHExecutor is the subset of ops.SSHDeployer that SSHRunner needs. Kept
// minimal here (rather than importing ops.SSHDeployer directly) so this
// package never imports internal/ops — any ops.SSHDeployer value already
// satisfies this interface structurally.
type SSHExecutor interface {
	ExecSSH(ctx context.Context, hostname string, command string) ([]byte, error)
}

// SSHRunner is the production Runner for the interactive-container deploy
// transport, where dir is a path inside the container reached at Hostname.
type SSHRunner struct {
	Executor SSHExecutor
	Hostname string
}

func (r SSHRunner) Run(ctx context.Context, dir, script string) (string, string, error) {
	cmd := "cd " + shellQuote(dir) + " && " + script
	out, err := r.Executor.ExecSSH(ctx, r.Hostname, cmd)
	// ExecSSH returns combined output; stderr distinguishing detail (if
	// any) lives on the *platform.SSHExecError itself — callers that need
	// it wrap via tools.withSSHStderr at the tools layer.
	return string(out), "", err
}

// shellQuote wraps s in POSIX single quotes, escaping embedded single
// quotes. Duplicated from ops.ShellQuote (the codebase's single-owner
// implementation) because this package must not import internal/ops —
// same algorithm, kept in sync by inspection since both are one line.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// MkTempDir creates a fresh directory OUTSIDE any repository for
// ExtractCommitToTemp's output: os.MkdirTemp directly for LocalRunner (no
// subprocess needed), `mktemp -d` for every other Runner (SSHRunner —
// the directory must exist inside the remote container, not on the zcp
// host).
func MkTempDir(ctx context.Context, r Runner) (string, error) {
	if _, ok := r.(LocalRunner); ok {
		return os.MkdirTemp("", "zcp-deploy-sha-*")
	}
	out, stderr, err := r.Run(ctx, "", "mktemp -d")
	if err != nil {
		return "", wrapErr("mktemp -d", err, stderr)
	}
	return strings.TrimSpace(out), nil
}

// RemoveTemp removes a directory created by MkTempDir, symmetric per Runner
// kind (os.RemoveAll locally, `rm -rf` remotely).
func RemoveTemp(ctx context.Context, r Runner, path string) error {
	if _, ok := r.(LocalRunner); ok {
		return os.RemoveAll(path)
	}
	_, stderr, err := r.Run(ctx, "", "rm -rf "+shellQuote(path))
	if err != nil {
		return wrapErr("rm -rf "+path, err, stderr)
	}
	return nil
}
