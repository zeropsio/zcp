// Package service provides exec wrappers for container services.
// Each wrapper replaces the current process via syscall.Exec,
// making the service the direct child of the container supervisor.
package service

import (
	"fmt"
	"os"
	"os/exec"
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

// execFunc replaces the current process. Tests override this to avoid actual exec.
var execFunc = syscall.Exec

// SetExecFunc overrides the exec function for testing.
func SetExecFunc(fn func(string, []string, []string) error) { execFunc = fn }

// ResetExecFunc restores the default exec function.
func ResetExecFunc() { execFunc = syscall.Exec }

// Start replaces the current process with the named service.
// On success, this function never returns (process is replaced).
func Start(name string) error {
	cfg, ok := services[name]
	if !ok {
		return fmt.Errorf("unknown service %q (available: nginx, vscode)", name)
	}

	binary, err := exec.LookPath(cfg.binary)
	if err != nil {
		return fmt.Errorf("find %s: %w", cfg.binary, err)
	}

	return execFunc(binary, cfg.args, os.Environ())
}

// List returns the names of all available services.
func List() []string {
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	return names
}
