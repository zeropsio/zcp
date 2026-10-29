package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

const (
	// deployExecTimeout bounds foreground SSH commands (deploys, probes,
	// log tails). Large enough for a real deploy's progress stream,
	// small enough to catch a truly stuck ssh process.
	deployExecTimeout = 5 * time.Minute
	// defaultBgSpawnTimeout bounds a fire-and-forget background spawn.
	// A correct detach pattern (setsid + redirects + `-T -n`) returns in
	// well under a second; anything past this ceiling means the remote
	// shell did not release the channel and the spawn shape is wrong.
	defaultBgSpawnTimeout = 10 * time.Second
	// streamExecTimeout bounds a streamed SSH command: a git bundle of a
	// whole repository crossing it takes as long as the repository is big.
	streamExecTimeout = 30 * time.Minute
	// streamStderrKeep is how much of a streamed command's stderr — its
	// last bytes — a failed run keeps for its error.
	streamStderrKeep = 64 << 10
	// sshStdioWaitDelay is how long ExecSSHStdio keeps copying a stream that
	// is no *os.File once ssh has ended, or after its context stopped it.
	sshStdioWaitDelay = 2 * time.Second
)

// SystemSSHDeployer implements ops.SSHDeployer using SSH to sibling Zerops containers.
// Zerops provides key-based SSH access within a project (no password needed).
type SystemSSHDeployer struct{}

// NewSystemSSHDeployer creates a new SystemSSHDeployer.
func NewSystemSSHDeployer() *SystemSSHDeployer {
	return &SystemSSHDeployer{}
}

// sshArgs builds the argument list for an SSH command to a Zerops container.
// Disables host key checking because containers get new keys on each redeploy.
// LogLevel=ERROR suppresses the client's stderr chatter — with the known-hosts
// file at /dev/null, ssh prints "Warning: Permanently added ..." on EVERY
// connection, and ExecSSH returns CombinedOutput, so the warning would mix
// into every parsed single-value read (live-verified: the launch gate's
// `git remote get-url origin` value carried the warning and false-blocked
// remote-mismatch on an exactly-matching URL).
func sshArgs(hostname, command string) []string {
	return []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		hostname, command,
	}
}

// sshArgsBg is the argument list for a fire-and-forget background spawn.
// Tuned to return in well under a second when the remote command properly
// detaches via `setsid` + stdout/stderr redirect + `< /dev/null`.
//
// Why a separate argv from sshArgs:
//   - `-T` disables pty allocation. Without it, an ssh session with an
//     interactive-looking remote shell may hold the channel open until
//     the pty is released, even after the command exits.
//   - `-n` redirects the ssh client's stdin from /dev/null. Without it,
//     the client inherits stdin from the Go parent; if anything upstream
//     writes to os.Stdin, those bytes travel through the ssh channel and
//     keep it alive.
//   - `BatchMode=yes` disables every prompt. Auth failures return fast
//     instead of hanging on "are you sure?" or password entry.
//   - `ConnectTimeout=5` bounds the TCP dial phase, so an unreachable
//     container fails fast instead of eating the kernel default.
//   - Short keepalives (5s × 2) tear down a dead channel well under our
//     10-second spawn budget.
//
// The v17 failure — `zerops_dev_server start` hung 300s on a `nohup ... &
// disown` pattern that looked correct — traces to some combination of
// those flags being missing plus a spawn shape that left `nohup` sharing
// a process group with the outer remote bash. See spawn comment in
// ops/dev_server.go for the shape fix.
func sshArgsBg(hostname, command string) []string {
	return []string{
		"-T", "-n",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=5",
		"-o", "ServerAliveInterval=5",
		"-o", "ServerAliveCountMax=2",
		hostname, command,
	}
}

// ExecSSH runs a command on a remote Zerops container via SSH.
// On failure, returns *SSHExecError with structured output and exit error
// separated — this prevents classifiers from matching on progress output.
func (d *SystemSSHDeployer) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	return d.ExecSSHStdin(ctx, hostname, command, nil)
}

// ExecSSHStdin is ExecSSH with stdin as the session's input: the remote
// command reads it, and neither the local ssh argv nor the remote shell's
// command line carries it.
func (d *SystemSSHDeployer) ExecSSHStdin(ctx context.Context, hostname, command string, stdin []byte) ([]byte, error) {
	return d.ExecSSHStdinWithin(ctx, hostname, command, stdin, deployExecTimeout)
}

// ExecSSHStdinWithin is ExecSSHStdin bounded by ceiling rather than the
// deployer-wide 5 min: a push's call lasts as long as its upload, which its
// remote side waits for by the upload's size (docs/spec-workflows.md §8 DM-9).
func (d *SystemSSHDeployer) ExecSSHStdinWithin(ctx context.Context, hostname, command string, stdin []byte, ceiling time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, ceiling)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", sshArgs(hostname, command)...) //nolint:gosec // hostname and command from trusted internal callers
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			// The client was killed for its context, not the remote command.
			err = fmt.Errorf("ssh stopped: %w (%w)", ctxErr, err)
		}
		return output, &SSHExecError{Hostname: hostname, Output: string(output), Err: err}
	}
	return output, nil
}

// sshArgsStream is sshArgs for a command whose stdout carries bytes, not
// text: `-T` keeps the client from asking for a tty, whose line discipline
// would rewrite them (CR LF, control bytes).
func sshArgsStream(hostname, command string) []string {
	return append([]string{"-T"}, sshArgs(hostname, command)...)
}

// StreamSSH runs command on hostname and copies its stdout — and only its
// stdout — to stdout as it arrives, never buffering it: what carries a git
// bundle of a whole repository off a container. Its stderr is kept apart (the
// last streamStderrKeep bytes) and rides the *SSHExecError of a failed run,
// as ExecSSH's output does. A run stopped by its context says so.
func (d *SystemSSHDeployer) StreamSSH(ctx context.Context, hostname, command string, stdout io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, streamExecTimeout)
	defer cancel()
	stderr := &tailBuffer{keep: streamStderrKeep}
	cmd := exec.CommandContext(ctx, "ssh", sshArgsStream(hostname, command)...) //nolint:gosec // hostname and command from trusted internal callers
	cmd.Stdin = nil
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			err = fmt.Errorf("ssh stopped: %w (%w)", ctxErr, err)
		}
		return &SSHExecError{Hostname: hostname, Output: stderr.String(), Err: err}
	}
	return nil
}

// ExecSSHStdio runs command on hostname with the caller's stdin, stdout and
// stderr wired to the session — the remote command reads the one and its two
// outputs arrive apart, byte for byte, as they are written (no tty: -T) — and
// returns the exit code ssh ended with: the remote command's, or ssh's own 255.
// A non-zero exit is the command's answer, not an error. It has no deadline
// of its own — a push of a large repository takes as long as it takes — so
// ctx alone stops it, and a call ctx stopped says so in its error. An *os.File
// stream is handed to ssh as it is; any other is copied, and stops being
// waited for sshStdioWaitDelay after ssh ends.
func (d *SystemSSHDeployer) ExecSSHStdio(ctx context.Context, hostname, command string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	cmd := exec.CommandContext(ctx, "ssh", sshArgsStream(hostname, command)...) //nolint:gosec // hostname and command from trusted internal callers
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = sshStdioWaitDelay
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		// The client was killed for its context, not ended by the remote command.
		return -1, &SSHExecError{Hostname: hostname, Err: fmt.Errorf("ssh stopped: %w (%w)", ctxErr, err)}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
		return exitErr.ExitCode(), nil
	}
	return -1, &SSHExecError{Hostname: hostname, Err: fmt.Errorf("ssh did not complete: %w", err)}
}

// tailBuffer keeps the last keep bytes written to it.
type tailBuffer struct {
	keep int
	buf  []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	if over := len(b.buf) - b.keep; over > 0 {
		b.buf = append(b.buf[:0], b.buf[over:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string { return string(b.buf) }

// ExecSSHBackground runs a fire-and-forget SSH command tuned for
// detaching a long-running process on the remote host. It enforces a
// tight per-call timeout (default 10s) and uses sshArgsBg so that ssh
// does not hold the channel open waiting for pty/stdin/stdout of the
// backgrounded child. Callers are responsible for supplying a remote
// command that redirects stdio and detaches (see ops/dev_server.go).
//
// On deadline, returns an *SSHExecError whose underlying error is
// context.DeadlineExceeded so callers can type-assert and surface a
// spawn-timeout reason instead of a generic SSH error. Any other
// failure is wrapped in an *SSHExecError as usual.
func (d *SystemSSHDeployer) ExecSSHBackground(parent context.Context, hostname, command string, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		timeout = defaultBgSpawnTimeout
	}
	if timeout > deployExecTimeout {
		timeout = deployExecTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ssh", sshArgsBg(hostname, command)...) //nolint:gosec // hostname and command from trusted internal callers
	// Paranoia belt-and-suspenders: even with -n, force Stdin to nil so
	// Go's exec package never hooks up a pipe to os.Stdin.
	cmd.Stdin = nil
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return output, &SSHExecError{
			Hostname: hostname,
			Output:   string(output),
			Err:      fmt.Errorf("ssh background spawn exceeded %s: %w", timeout, context.DeadlineExceeded),
		}
	}
	if err != nil {
		return output, &SSHExecError{Hostname: hostname, Output: string(output), Err: err}
	}
	return output, nil
}

// IsSpawnTimeout reports whether an error returned by ExecSSHBackground
// is a bounded-deadline spawn-timeout (as opposed to an auth/connection
// failure or a remote shell exit error). Exposed so ops callers can
// classify failure modes without string-matching.
func IsSpawnTimeout(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded)
}
