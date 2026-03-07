package ops

import (
	"context"
	"fmt"
	"time"
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

// WaitSSHReady polls SSH until the target host responds successfully.
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

	deadline := time.After(cfg.timeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("ssh not ready on %s after %s", hostname, cfg.timeout)
		default:
		}

		_, err := deployer.ExecSSH(ctx, hostname, cfg.command)
		if err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("ssh not ready on %s after %s", hostname, cfg.timeout)
		case <-time.After(cfg.interval):
		}
	}
}
