package platform

import (
	"context"
	"fmt"
	"os/exec"
)

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
	cmd := exec.CommandContext(ctx, "zcli", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("zcli %v: %w (output: %s)", args, err, string(output))
	}
	return output, nil
}
