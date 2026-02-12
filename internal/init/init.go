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
//  2. Configure MCP server in baseDir/.claude/mcp-config.json
//  3. Configure SSH in $HOME/.ssh/config (user home, not baseDir)
//
// All steps are idempotent — re-running resets to defaults.
func Run(baseDir string) error {
	steps := []struct {
		name string
		fn   func(string) error
	}{
		{"CLAUDE.md", generateCLAUDEMD},
		{"MCP config", generateMCPConfig},
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
	dir := filepath.Join(baseDir, ".claude")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "mcp-config.json"), []byte(tmpl), 0644) //nolint:gosec // G306: config files need to be readable
}

func generateSSHConfig(_ string) error {
	tmpl, err := content.GetTemplate("ssh-config")
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config"), []byte(tmpl), 0644) //nolint:gosec // G306: config files need to be readable
}
