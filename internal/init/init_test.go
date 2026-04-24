// Tests for: init package — zcp init subcommand.
package init_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/runtime"
)

// stubContainerCommands prevents container init steps from running real commands.
func stubContainerCommands(t *testing.T) {
	t.Helper()
	zcpinit.SetCommandRunner(func(_ string, _ ...string) error { return nil })
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })
	zcpinit.SetVSCodeWorkDir(t.TempDir())
	t.Cleanup(func() { zcpinit.ResetVSCodeWorkDir() })
}

func TestRun_GeneratesCLAUDEMD(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := zcpinit.Run(dir, runtime.Info{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "# Zerops") {
		t.Error("CLAUDE.md should contain '# Zerops' heading")
	}
}

func TestCLAUDEMD_PreservesReflog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// First run: write the template.
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}

	// Append a REFLOG entry as bootstrap does.
	path := filepath.Join(dir, "CLAUDE.md")
	reflog := "\n<!-- ZEROPS:REFLOG -->\n### 2026-04-19 — Bootstrap: test entry\n\n- **Session:** abc123\n<!-- /ZEROPS:REFLOG -->\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	if _, err := f.WriteString(reflog); err != nil {
		t.Fatalf("append reflog: %v", err)
	}
	f.Close()

	// Second run: re-init. The REFLOG must survive.
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "ZEROPS:REFLOG") {
		t.Error("CLAUDE.md must preserve REFLOG section across re-init")
	}
	if !strings.Contains(content, "Session:** abc123") {
		t.Error("CLAUDE.md must preserve REFLOG entry body across re-init")
	}
	if !strings.Contains(content, "# Zerops") {
		t.Error("CLAUDE.md must still contain template heading after re-init")
	}
}

func TestRun_GeneratesMCPConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := zcpinit.Run(dir, runtime.Info{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "zcp") {
		t.Error(".mcp.json should reference zcp")
	}
}

func TestRun_GeneratesSSHConfig(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubContainerCommands(t)

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".ssh", "config"))
	if err != nil {
		t.Fatalf("read ssh config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "zerops") {
		t.Error("ssh config should mention zerops")
	}
}

func TestRun_GeneratesSettingsLocal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := zcpinit.Run(dir, runtime.Info{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings.local.json: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "mcp__zerops__*") {
		t.Error("settings.local.json should contain mcp__zerops__* permission")
	}
}

func TestRun_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Run twice.
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}

	// Files should still exist and be valid.
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md after second run: %v", err)
	}
	if !strings.Contains(string(data), "# Zerops") {
		t.Error("CLAUDE.md should still contain '# Zerops' after second run")
	}
}

func TestRun_GeneratesAliases(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	err := zcpinit.Run(dir, runtime.Info{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".config", "zerops", "aliases"))
	if err != nil {
		t.Fatalf("read aliases: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "alias zcl=") {
		t.Error("aliases file should contain zcl alias")
	}
	if !strings.Contains(content, "--dangerously-skip-permissions") {
		t.Error("aliases file should contain --dangerously-skip-permissions flag")
	}
}

func TestRun_AliasesBashrcSourceLine(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	err := zcpinit.Run(dir, runtime.Info{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".bashrc"))
	if err != nil {
		t.Fatalf("read .bashrc: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, ".config/zerops/aliases") {
		t.Error(".bashrc should source the aliases file")
	}
}

func TestRun_AliasesZshrcSourceLine(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Create .zshrc so the init detects zsh is installed.
	if err := os.WriteFile(filepath.Join(homeDir, ".zshrc"), []byte("# oh-my-zsh\n"), 0644); err != nil {
		t.Fatalf("write .zshrc: %v", err)
	}

	err := zcpinit.Run(dir, runtime.Info{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".zshrc"))
	if err != nil {
		t.Fatalf("read .zshrc: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, ".config/zerops/aliases") {
		t.Error(".zshrc should source the aliases file")
	}
	// Original content should be preserved.
	if !strings.Contains(content, "# oh-my-zsh") {
		t.Error(".zshrc should preserve original content")
	}
}

func TestRun_AliasesSkipsMissingZshrc(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Don't create .zshrc — simulates bash-only system.
	err := zcpinit.Run(dir, runtime.Info{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// .zshrc should NOT exist (we didn't create it, init shouldn't either).
	if _, err := os.Stat(filepath.Join(homeDir, ".zshrc")); !os.IsNotExist(err) {
		t.Error(".zshrc should not be created on bash-only systems")
	}

	// .bashrc should still be created.
	if _, err := os.Stat(filepath.Join(homeDir, ".bashrc")); os.IsNotExist(err) {
		t.Error(".bashrc should be created")
	}
}

func TestRun_AliasesBashrcIdempotent(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Run twice.
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".bashrc"))
	if err != nil {
		t.Fatalf("read .bashrc: %v", err)
	}

	content := string(data)
	count := strings.Count(content, "# Zerops shell aliases")
	if count != 1 {
		t.Errorf("source block should appear exactly once, got %d", count)
	}
}

func TestRun_ReportsSteps(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubContainerCommands(t)

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Container mode: no project .mcp.json (MCP config is in ~/.claude.json).
	files := []string{
		filepath.Join(dir, "CLAUDE.md"),
		filepath.Join(dir, ".claude", "settings.local.json"),
		filepath.Join(homeDir, ".ssh", "config"),
		filepath.Join(homeDir, ".config", "zerops", "aliases"),
	}
	for _, f := range files {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", f)
		}
	}
}

func TestRun_Container_NoProjectMCPConfig(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubContainerCommands(t)

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	mcpPath := filepath.Join(dir, ".mcp.json")
	if _, err := os.Stat(mcpPath); !os.IsNotExist(err) {
		t.Error(".mcp.json should not be created in container mode (MCP config is global in ~/.claude.json)")
	}
}

func TestSSHConfig_Container_ManagedSection(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubContainerCommands(t)

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".ssh", "config"))
	if err != nil {
		t.Fatalf("read ssh config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "# ZCP:BEGIN") {
		t.Error("ssh config should contain ZCP:BEGIN marker")
	}
	if !strings.Contains(content, "# ZCP:END") {
		t.Error("ssh config should contain ZCP:END marker")
	}
	if !strings.Contains(content, "User zerops") {
		t.Error("ssh config should contain 'User zerops' directive")
	}
}

func TestSSHConfig_Container_ControlMaster(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubContainerCommands(t)

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".ssh", "config"))
	if err != nil {
		t.Fatalf("read ssh config: %v", err)
	}

	content := string(data)
	required := []string{
		"ControlMaster auto",
		"ControlPath /tmp/ssh-mux-",
		"ControlPersist 600",
	}
	for _, keyword := range required {
		if !strings.Contains(content, keyword) {
			t.Errorf("ssh config should contain %q for SSH connection multiplexing", keyword)
		}
	}
}

func TestSSHConfig_Container_PreservesExisting(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubContainerCommands(t)

	// Write pre-existing SSH config.
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	existing := "Host github.com\n    IdentityFile ~/.ssh/id_github\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(existing), 0644); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(sshDir, "config"))
	if err != nil {
		t.Fatalf("read ssh config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Host github.com") {
		t.Error("ssh config should preserve existing 'Host github.com' entry")
	}
	if !strings.Contains(content, "IdentityFile ~/.ssh/id_github") {
		t.Error("ssh config should preserve existing IdentityFile directive")
	}
	if !strings.Contains(content, "# ZCP:BEGIN") {
		t.Error("ssh config should contain ZCP managed section")
	}
}

func TestSSHConfig_Container_Idempotent(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubContainerCommands(t)

	rt := runtime.Info{InContainer: true}

	if err := zcpinit.Run(dir, rt); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	if err := zcpinit.Run(dir, rt); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".ssh", "config"))
	if err != nil {
		t.Fatalf("read ssh config: %v", err)
	}

	content := string(data)
	beginCount := strings.Count(content, "# ZCP:BEGIN")
	if beginCount != 1 {
		t.Errorf("ZCP:BEGIN should appear exactly once after two runs, got %d", beginCount)
	}
	endCount := strings.Count(content, "# ZCP:END")
	if endCount != 1 {
		t.Errorf("ZCP:END should appear exactly once after two runs, got %d", endCount)
	}
}

func TestSSHConfig_Local_Skipped(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	err := zcpinit.Run(dir, runtime.Info{InContainer: false})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	sshConfig := filepath.Join(homeDir, ".ssh", "config")
	if _, err := os.Stat(sshConfig); !os.IsNotExist(err) {
		t.Error("ssh config should not be created in local mode")
	}
}
