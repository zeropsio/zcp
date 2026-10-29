// Package testgit owns Git isolation and checkout protection for package tests.
package testgit

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Run runs a package's tests and fails if they change the enclosing checkout's
// configuration. It isolates inherited Git bindings, identity and user/system
// configuration once, before any tests can start goroutines in TestMain.
func Run(m *testing.M) int {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return code
}

func run(m *testing.M) (int, error) {
	// Ignore inherited repository bindings while locating the checkout that
	// contains the package, rather than the repository named by a hook's GIT_DIR.
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "--path-format=absolute", "--git-path", "config")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("locate enclosing checkout config: %w", err)
	}
	path := strings.TrimSpace(string(out))
	before, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("snapshot enclosing checkout config: %w", err)
	}
	dir, err := os.MkdirTemp("", "zcp-test-git-")
	if err != nil {
		return 0, fmt.Errorf("create isolated Git home: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := isolate(dir); err != nil {
		return 0, err
	}
	code := m.Run()
	after, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read enclosing checkout config after package tests: %w", err)
	}
	if !bytes.Equal(before, after) {
		return 0, fmt.Errorf("package tests changed enclosing checkout Git config: %s", path)
	}
	return code, nil
}

// isolate removes hook bindings as well as inherited config/identity overrides.
// Keep only the CA installed by the package's fake HTTPS origin setup.
func isolate(home string) error {
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "GIT_") && name != "GIT_SSL_CAINFO" {
			if err := os.Unsetenv(name); err != nil {
				return fmt.Errorf("clear inherited Git environment %s: %w", name, err)
			}
		}
	}
	for name, value := range map[string]string{
		"HOME":                home,
		"XDG_CONFIG_HOME":     filepath.Join(home, "xdg"),
		"GIT_CONFIG_GLOBAL":   filepath.Join(home, ".gitconfig"),
		"GIT_CONFIG_SYSTEM":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
	} {
		if err := os.Setenv(name, value); err != nil {
			return fmt.Errorf("set isolated Git environment %s: %w", name, err)
		}
	}
	return nil
}
