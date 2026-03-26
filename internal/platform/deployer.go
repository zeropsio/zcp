package platform

import (
	"context"
	"os/exec"
	"time"
)

const deployExecTimeout = 5 * time.Minute

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
