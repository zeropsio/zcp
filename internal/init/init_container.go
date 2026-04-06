package init

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/ops"

	"github.com/zeropsio/zcp/internal/content"
)

// commandRunner executes external commands. Tests replace this via SetCommandRunner.
var commandRunner = defaultCommandRunner

func defaultCommandRunner(name string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SetCommandRunner replaces the command runner for testing.
func SetCommandRunner(fn func(string, ...string) error) { commandRunner = fn }

// ResetCommandRunner restores the default command runner.
func ResetCommandRunner() { commandRunner = defaultCommandRunner }

// SetGitInitDir overrides the git init directory for testing.
func SetGitInitDir(dir string) { gitInitDir = dir }

// ResetGitInitDir restores the default git init directory.
func ResetGitInitDir() { gitInitDir = "/var/www" }

// SetVSCodeWorkDir overrides the VS Code workspace directory for testing.
func SetVSCodeWorkDir(dir string) { vsCodeWorkDir = dir }

// ResetVSCodeWorkDir restores the default VS Code workspace directory.
func ResetVSCodeWorkDir() { vsCodeWorkDir = "/var/www" }

// containerSteps returns init steps that only run inside Zerops containers.
func containerSteps() []step {
	steps := []step{
		{"Git config", configureGit},
		{"Claude configs", configureClaude},
	}
	if os.Getenv("ZCP_VSCODE") == "true" {
		steps = append(steps, step{"VS Code settings", configureVSCode})
	}
	return steps
}

// gitInitDir is the directory to initialize as a git repo.
// Tests can override this to avoid writing to /var/www.
var gitInitDir = "/var/www"

// configureGit sets global git identity and initializes the workspace as a repo.
// Idempotent: git config overwrites, git init on existing repo is a no-op.
func configureGit(_ string) error {
	id := ops.DeployGitIdentity
	cmds := [][]string{
		{"git", "config", "--global", "user.email", id.Email},
		{"git", "config", "--global", "user.name", id.Name},
		{"git", "init", gitInitDir},
	}
	for _, args := range cmds {
		if err := commandRunner(args[0], args[1:]...); err != nil {
			return fmt.Errorf("%s: %w", args[0], err)
		}
	}
	return nil
}

// configureClaude writes ~/.claude.json and ~/.claude/settings.json for
// headless Claude Code operation (skip onboarding, dark theme, no permission prompts).
func configureClaude(_ string) error {
	home := resolveHome()

	files := []struct {
		path     string
		template string
	}{
		{filepath.Join(home, ".claude.json"), "claude.json"},
		{filepath.Join(home, ".claude", "settings.json"), "claude-settings.json"},
	}
	for _, f := range files {
		tmpl, err := content.GetTemplate(f.template)
		if err != nil {
			return err
		}
		dir := filepath.Dir(f.path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
		if err := os.WriteFile(f.path, []byte(tmpl), 0644); err != nil { //nolint:gosec // G306: config files need to be readable
			return fmt.Errorf("write %s: %w", f.path, err)
		}
	}
	return nil
}

// vsCodeWorkDir is the workspace root for VS Code terminal config.
// Tests can override this to avoid writing to /var/www.
var vsCodeWorkDir = "/var/www"

// configureVSCode writes code-server user settings, terminal config, and
// installs the Claude Code extension. Only called when ZCP_VSCODE=true.
func configureVSCode(_ string) error {
	home := resolveHome()

	settingsPath := filepath.Join(home, ".local", "share", "code-server", "User", "settings.json")
	files := []struct {
		path     string
		template string
	}{
		{settingsPath, "vscode-settings.json"},
		{filepath.Join(vsCodeWorkDir, ".vscode", "terminals.json"), "vscode-terminals.json"},
	}
	for _, f := range files {
		tmpl, err := content.GetTemplate(f.template)
		if err != nil {
			return err
		}
		dir := filepath.Dir(f.path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
		if err := os.WriteFile(f.path, []byte(tmpl), 0644); err != nil { //nolint:gosec // G306: config files need to be readable
			return fmt.Errorf("write %s: %w", f.path, err)
		}
	}

	// Install Claude Code extension (idempotent — skips if already installed).
	fmt.Fprintln(os.Stderr, "    installing claude-code extension...")
	if err := commandRunner("code-server", "--install-extension", "Anthropic.claude-code"); err != nil {
		// Non-fatal: extension install failure should not block init.
		fmt.Fprintf(os.Stderr, "    (warning: extension install failed: %v)\n", err)
	}

	// Point the Claude Code extension at the claude CLI binary.
	if err := patchVSCodeClaudeWrapper(settingsPath); err != nil {
		fmt.Fprintf(os.Stderr, "    (warning: claude wrapper patch failed: %v)\n", err)
	}

	return nil
}

// patchVSCodeClaudeWrapper adds claudeCode.claudeProcessWrapper to VS Code settings.
func patchVSCodeClaudeWrapper(settingsPath string) error {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("find claude: %w", err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("read settings: %w", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("parse settings: %w", err)
	}

	settings["claudeCode.claudeProcessWrapper"] = claudePath

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, append(out, '\n'), 0644); err != nil { //nolint:gosec // G306: config files need to be readable
		return fmt.Errorf("write settings: %w", err)
	}
	return nil
}
