package ops

import (
	"context"
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

type sshReadyConfig struct {
	interval time.Duration
	timeout  time.Duration
	command  string
}

var defaultSSHReadyConfig = sshReadyConfig{
	interval: 1 * time.Second,
	timeout:  30 * time.Second,
	command:  "true",
}

// WaitSSHReady runs the readiness probe (`true`) on hostname over ssh until
// it answers. Only an unreachable failure is waited on, bounded; any other —
// a refusal: `true` cannot fail, so any exit but ssh's own 255 is one — comes
// back at once. Either way the error is a *platform.SSHProbeError with the
// last ssh error verbatim, its class ClassOf's.
func WaitSSHReady(ctx context.Context, deployer SSHDeployer, hostname string) error {
	return waitSSHReady(ctx, deployer, hostname, defaultSSHReadyConfig)
}

// OverrideSSHReadyConfigForTest overrides the SSH readiness polling config.
// Returns a restore function. Only for use in tests.
func OverrideSSHReadyConfigForTest(interval, timeout time.Duration) func() {
	old := defaultSSHReadyConfig
	defaultSSHReadyConfig = sshReadyConfig{
		interval: interval,
		timeout:  timeout,
		command:  "true",
	}
	return func() { defaultSSHReadyConfig = old }
}

func waitSSHReady(ctx context.Context, deployer SSHDeployer, hostname string, cfg sshReadyConfig) error {
	if deployer == nil {
		return fmt.Errorf("no SSH deployer configured")
	}

	start := time.Now()
	deadline := time.After(cfg.timeout)
	for {
		_, err := deployer.ExecSSH(ctx, hostname, cfg.command)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		probe := &platform.SSHProbeError{Hostname: hostname, Last: err}
		if !platform.ClassOf(probe).WaitedOn() {
			return probe
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			probe.Waited = time.Since(start)
			return probe
		case <-time.After(cfg.interval):
		}
	}
}
