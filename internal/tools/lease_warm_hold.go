package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// startHeldWarmClone starts `zcp lease warm-clone --pool <pool> --container
// <id>` (ops.LeaseWarmCloneHeld) from this zcp's own binary: the process that
// holds the relayed session of a warm clone of a repository on the Mate's HQ
// until the clone ends (DP-32). It runs in a session of its own and reads and
// writes none of this process's stdio, so it outlives a `zcp lease` call —
// whose stdout the Mate reads to its end — and an MCP server, whose agent may
// end first; it ends with its clone, within its own bound, and records the
// outcome as the pool's warm clone. A goroutine reaps it while this process
// lives.
func startHeldWarmClone(pool, containerID string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find zcp's own binary: %w", err)
	}
	cmd := exec.CommandContext(context.Background(), exe, "lease", "warm-clone", "--pool", pool, "--container", containerID)
	cmd.SysProcAttr = heldWarmCloneProcAttr()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start zcp lease warm-clone: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
