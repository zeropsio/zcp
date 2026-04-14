package platform

import (
	"context"
	"errors"
	"fmt"
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
func sshArgs(hostname, command string) []string {
	return []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
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
	ctx, cancel := context.WithTimeout(ctx, deployExecTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", sshArgs(hostname, command)...) //nolint:gosec // hostname and command from trusted internal callers
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, &SSHExecError{Hostname: hostname, Output: string(output), Err: err}
	}
	return output, nil
}

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
