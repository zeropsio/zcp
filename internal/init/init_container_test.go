package init_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
)

// setupContainerTest sets common test overrides for container init tests.
func setupContainerTest(t *testing.T) {
	t.Helper()
	gitDir := t.TempDir()
	zcpinit.SetGitInitDir(gitDir)
	t.Cleanup(func() { zcpinit.ResetGitInitDir() })
	vsDir := t.TempDir()
	zcpinit.SetVSCodeWorkDir(vsDir)
	t.Cleanup(func() { zcpinit.ResetVSCodeWorkDir() })
}

func TestContainerSteps_GitConfig(t *testing.T) {
	// Not parallel — mutates HOME env var and commandRunner.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Don't use setupContainerTest here — we need to check the git init dir.
	gitDir := t.TempDir()
	zcpinit.SetGitInitDir(gitDir)
	t.Cleanup(func() { zcpinit.ResetGitInitDir() })
	zcpinit.SetVSCodeWorkDir(t.TempDir())
	t.Cleanup(func() { zcpinit.ResetVSCodeWorkDir() })

	var executed [][]string
	zcpinit.SetCommandRunner(func(name string, args ...string) error {
		cmd := append([]string{name}, args...)
		executed = append(executed, cmd)
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	tests := []struct {
		name string
		want []string
	}{
		{"git email", []string{"git", "config", "--global", "user.email", ops.DeployGitIdentity.Email}},
		{"git name", []string{"git", "config", "--global", "user.name", ops.DeployGitIdentity.Name}},
		{"git init", []string{"git", "init", gitDir}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := false
			for _, cmd := range executed {
				if slicesEqual(cmd, tt.want) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected command %v to be executed, got: %v", tt.want, executed)
			}
		})
	}
}

func TestContainerSteps_ClaudeConfigs(t *testing.T) {
	// Not parallel — mutates HOME env var and commandRunner.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	setupContainerTest(t)

	zcpinit.SetCommandRunner(func(_ string, _ ...string) error { return nil })
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		contains string
	}{
		{"claude.json exists", filepath.Join(homeDir, ".claude.json"), "hasCompletedOnboarding"},
		{"claude.json theme", filepath.Join(homeDir, ".claude.json"), "dark"},
		{"settings.json exists", filepath.Join(homeDir, ".claude", "settings.json"), "skipDangerousModePermissionPrompt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read %s: %v", tt.path, err)
			}
			if !strings.Contains(string(data), tt.contains) {
				t.Errorf("%s should contain %q, got: %s", tt.path, tt.contains, data)
			}
		})
	}
}

func TestContainerSteps_VSCode_Enabled(t *testing.T) {
	// Not parallel — mutates HOME and ZCP_VSCODE env vars.
	dir := t.TempDir()
	homeDir := t.TempDir()
	vsWorkDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("ZCP_VSCODE", "true")
	zcpinit.SetGitInitDir(t.TempDir())
	t.Cleanup(func() { zcpinit.ResetGitInitDir() })
	zcpinit.SetVSCodeWorkDir(vsWorkDir)
	t.Cleanup(func() { zcpinit.ResetVSCodeWorkDir() })

	var extensionInstalled bool
	zcpinit.SetCommandRunner(func(name string, args ...string) error {
		if name == "code-server" && len(args) > 0 && args[0] == "--install-extension" {
			extensionInstalled = true
		}
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		contains string
	}{
		{"vscode settings", filepath.Join(homeDir, ".local", "share", "code-server", "User", "settings.json"), "Default Dark+"},
		{"vscode terminals", filepath.Join(vsWorkDir, ".vscode", "terminals.json"), "Claude Terminal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read %s: %v", tt.path, err)
			}
			if !strings.Contains(string(data), tt.contains) {
				t.Errorf("%s should contain %q", tt.path, tt.contains)
			}
		})
	}

	if !extensionInstalled {
		t.Error("expected code-server --install-extension to be called")
	}
}

func TestContainerSteps_VSCode_Disabled(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	setupContainerTest(t)
	// ZCP_VSCODE not set — VS Code steps should be skipped.

	zcpinit.SetCommandRunner(func(_ string, _ ...string) error { return nil })
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	vscodePath := filepath.Join(homeDir, ".local", "share", "code-server", "User", "settings.json")
	if _, err := os.Stat(vscodePath); !os.IsNotExist(err) {
		t.Error("VS Code settings should not be created when ZCP_VSCODE is not true")
	}
}

func TestContainerSteps_Idempotent(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	setupContainerTest(t)

	zcpinit.SetCommandRunner(func(_ string, _ ...string) error { return nil })
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	rt := runtime.Info{InContainer: true}

	if err := zcpinit.Run(dir, rt); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	if err := zcpinit.Run(dir, rt); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}

	// Claude config should still be valid after two runs.
	data, err := os.ReadFile(filepath.Join(homeDir, ".claude.json"))
	if err != nil {
		t.Fatalf("read .claude.json: %v", err)
	}
	if !strings.Contains(string(data), "hasCompletedOnboarding") {
		t.Error(".claude.json should contain hasCompletedOnboarding after second run")
	}
}

func TestContainerSteps_SkippedOutsideContainer(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	setupContainerTest(t)

	var gitCalled bool
	zcpinit.SetCommandRunner(func(name string, _ ...string) error {
		if name == "git" {
			gitCalled = true
		}
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	err := zcpinit.Run(dir, runtime.Info{InContainer: false})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if gitCalled {
		t.Error("git should not be called outside container")
	}

	claudePath := filepath.Join(homeDir, ".claude.json")
	if _, err := os.Stat(claudePath); !os.IsNotExist(err) {
		t.Error(".claude.json should not be created outside container")
	}
}

// slicesEqual compares two string slices for equality.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
