//go:build !linux

package ops

import (
	"context"
	"fmt"
	"os/exec"
)

func managedBrowserCommand(context.Context) (*exec.Cmd, func() error, error) {
	return nil, nil, fmt.Errorf("managed browsers require a Linux systemd container")
}
