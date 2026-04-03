// Package service provides exec wrappers for container services.
// Each wrapper starts the service as a child process and waits for it,
// forwarding signals so the service can shut down gracefully.
package service

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

type execConfig struct {
	binary string   // binary name (resolved via PATH)
	args   []string // argv including argv[0]
}

// services maps service names to their exec configurations.
var services = map[string]execConfig{
	"nginx": {
		binary: "nginx",
		args:   []string{"nginx", "-g", "daemon off;"},
	},
	"vscode": {
		binary: "code-server",
		args:   []string{"code-server", "--auth", "none", "--bind-addr", "127.0.0.1:8081", "--disable-workspace-trust", "/var/www"},
	},
}

// runFunc starts a service and waits for it to exit. Tests override this.
var runFunc = runCommand

// SetRunFunc overrides the run function for testing.
func SetRunFunc(fn func(string, []string) error) { runFunc = fn }

// ResetRunFunc restores the default run function.
func ResetRunFunc() { runFunc = runCommand }

// Start runs the named service as a child process and blocks until it exits.
// Signals (SIGINT, SIGTERM) are forwarded to the child.
func Start(name string) error {
	cfg, ok := services[name]
	if !ok {
		return fmt.Errorf("unknown service %q (available: nginx, vscode)", name)
	}

	binary, err := exec.LookPath(cfg.binary)
	if err != nil {
		return fmt.Errorf("find %s: %w", cfg.binary, err)
	}

	return runFunc(binary, cfg.args)
}

// runCommand starts a child process and waits for it, forwarding signals.
func runCommand(binary string, args []string) error {
	cmd := exec.Command(binary, args[1:]...) //nolint:gosec // binary is resolved from a hardcoded service map via LookPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	// Forward signals to child so it can shut down gracefully.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for s := range sig {
			_ = cmd.Process.Signal(s)
		}
	}()

	if err := cmd.Wait(); err != nil {
		signal.Stop(sig)
		return fmt.Errorf("%s exited: %w", args[0], err)
	}
	signal.Stop(sig)
	return nil
}

// List returns the names of all available services.
func List() []string {
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	return names
}
