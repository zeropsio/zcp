package platform

import (
	"context"
	"fmt"
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
func (d *SystemSSHDeployer) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, deployExecTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", sshArgs(hostname, command)...) //nolint:gosec // hostname and command from trusted internal callers
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("ssh %s: %w (output: %s)", hostname, err, string(output))
	}
	return output, nil
}
