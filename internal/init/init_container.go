package init

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/content"
)

// commandRunner executes external commands. Tests replace this via SetCommandRunner.
var commandRunner = defaultCommandRunner

func defaultCommandRunner(name string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// containerSteps returns init steps that only run inside Zerops containers.
// Git setup is intentionally absent: ZCP service on Zerops is pure
// infrastructure from git's perspective (GLC-4). /var/www is an SSHFS
// mount base — each mounted dev service carries its own .git/ inside its
// own container (initialized by ops.InitServiceGit at bootstrap). Global
// `git config --global` on the ZCP host had a single historical consumer
// (`zcp sync recipe push-app`), which is a developer-only CLI that runs
// locally with the developer's own ~/.gitconfig.
func containerSteps() []step {
	steps := []step{
		{"Claude configs", configureClaude},
	}
	if os.Getenv("ZCP_VSCODE") == "true" {
		steps = append(steps, step{"VS Code settings", configureVSCode})
	}
	return steps
}

// configureClaude writes ~/.claude.json and ~/.claude/settings.json.
// On containers zcp init owns these files — ~/.claude.json is composed from
// claude.json + mcp-config.json so the MCP server definition has one source of truth.
func configureClaude(_ string) error {
	home := resolveHome()

	claudeJSON, err := buildClaudeJSON()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), claudeJSON, 0644); err != nil { //nolint:gosec // G306: config files need to be readable
		return fmt.Errorf("write .claude.json: %w", err)
	}

	return writeTemplate("claude-settings.json", filepath.Join(home, ".claude", "settings.json"))
}

// buildClaudeJSON merges the claude.json base template with mcpServers from
// mcp-config.json so the MCP server definition is not duplicated across templates.
func buildClaudeJSON() ([]byte, error) {
	baseTmpl, err := content.GetTemplate("claude.json")
	if err != nil {
		return nil, err
	}
	mcpTmpl, err := content.GetTemplate("mcp-config.json")
	if err != nil {
		return nil, err
	}

	var base map[string]any
	if err := json.Unmarshal([]byte(baseTmpl), &base); err != nil {
		return nil, fmt.Errorf("parse claude.json: %w", err)
	}
	var mcp map[string]any
	if err := json.Unmarshal([]byte(mcpTmpl), &mcp); err != nil {
		return nil, fmt.Errorf("parse mcp-config.json: %w", err)
	}

	maps.Copy(base, mcp)
	return json.Marshal(base)
}

const defaultVSCodeWorkDir = "/var/www"

var vsCodeWorkDir = defaultVSCodeWorkDir

// configureVSCode writes code-server user settings, terminal config, and
// installs the Claude Code extension. Only called when ZCP_VSCODE=true.
func configureVSCode(_ string) error {
	home := resolveHome()

	settingsPath := filepath.Join(home, ".local", "share", "code-server", "User", "settings.json")
	if err := writeTemplate("vscode-settings.json", settingsPath); err != nil {
		return err
	}
	if err := writeTemplate("vscode-terminals.json", filepath.Join(vsCodeWorkDir, ".vscode", "terminals.json")); err != nil {
		return err
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
