// Package init implements the zcp init subcommand.
// It generates project configuration files for Zerops + Claude Code integration.
// All operations are idempotent — re-running overwrites with defaults.
package init

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/content"
)

// Run executes the init subcommand, generating configuration files in baseDir.
// Steps:
//  1. Generate CLAUDE.md in baseDir
//  2. Configure MCP server in baseDir/.mcp.json (Claude Code project-scoped config)
//  3. Configure permissions in baseDir/.claude/settings.local.json
//  4. Configure SSH in $HOME/.ssh/config (user home, not baseDir)
//
// All steps are idempotent — re-running resets to defaults.
func Run(baseDir string) error {
	steps := []struct {
		name string
		fn   func(string) error
	}{
		{"CLAUDE.md", generateCLAUDEMD},
		{"MCP config", generateMCPConfig},
		{"Permissions", generateSettingsLocal},
		{"SSH config", generateSSHConfig},
	}

	for _, step := range steps {
		fmt.Fprintf(os.Stderr, "  → %s\n", step.name)
		if err := step.fn(baseDir); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}

	fmt.Fprintln(os.Stderr, "  ✓ Init complete")
	return nil
}

func generateCLAUDEMD(baseDir string) error {
	tmpl, err := content.GetTemplate("claude.md")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(baseDir, "CLAUDE.md"), []byte(tmpl), 0644) //nolint:gosec // G306: config files need to be readable
}

func generateMCPConfig(baseDir string) error {
	tmpl, err := content.GetTemplate("mcp-config.json")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(baseDir, ".mcp.json"), []byte(tmpl), 0644) //nolint:gosec // G306: config files need to be readable
}

func generateSettingsLocal(baseDir string) error {
	tmpl, err := content.GetTemplate("settings-local.json")
	if err != nil {
		return err
	}
	dir := filepath.Join(baseDir, ".claude")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "settings.local.json"), []byte(tmpl), 0644) //nolint:gosec // G306: config files need to be readable
}

func generateSSHConfig(_ string) error {
	tmpl, err := content.GetTemplate("ssh-config")
	if err != nil {
		return err
	}
	home := os.Getenv("HOME")
	if home == "" || home == "/" {
		// Zerops initCommands run with HOME unset or "/".
		// Fall back to the zerops user's home directory.
		home = "/home/zerops"
	}
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config"), []byte(tmpl), 0644) //nolint:gosec // G306: config files need to be readable
}
