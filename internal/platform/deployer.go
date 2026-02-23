package platform

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

const deployExecTimeout = 5 * time.Minute

// SystemLocalDeployer implements ops.LocalDeployer using the local zcli binary.
type SystemLocalDeployer struct{}

// NewSystemLocalDeployer creates a new SystemLocalDeployer.
func NewSystemLocalDeployer() *SystemLocalDeployer {
	return &SystemLocalDeployer{}
}

// ExecZcli runs a zcli command with the given arguments.
// Uses CombinedOutput to capture stderr where zcli writes progress/errors.
// No zcli presence check at startup — error surfaces at call time.
func (d *SystemLocalDeployer) ExecZcli(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, deployExecTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zcli", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("zcli %v: %w (output: %s)", args, err, string(output))
	}
	return output, nil
}

// SystemSSHDeployer implements ops.SSHDeployer using SSH to sibling Zerops containers.
// Zerops provides key-based SSH access within a project (no password needed).
type SystemSSHDeployer struct{}

// NewSystemSSHDeployer creates a new SystemSSHDeployer.
func NewSystemSSHDeployer() *SystemSSHDeployer {
	return &SystemSSHDeployer{}
}

// ExecSSH runs a command on a remote Zerops container via SSH.
func (d *SystemSSHDeployer) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, deployExecTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", hostname, command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("ssh %s: %w (output: %s)", hostname, err, string(output))
	}
	return output, nil
}
