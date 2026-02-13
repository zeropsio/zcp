// Package init implements the zcp init subcommand.
// It generates project configuration files for Zerops + Claude Code integration.
// All operations are idempotent — re-running overwrites with defaults.
package init

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/content"
)

// Run executes the init subcommand, generating configuration files in baseDir.
// Steps:
//  1. Generate CLAUDE.md in baseDir
//  2. Configure MCP server in baseDir/.mcp.json (Claude Code project-scoped config)
//  3. Configure permissions in baseDir/.claude/settings.local.json
//  4. Configure SSH in $HOME/.ssh/config (user home, not baseDir)
//  5. Install shell aliases in $HOME/.config/zerops/aliases + source from .bashrc
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
		{"Shell aliases", generateAliases},
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
	home := resolveHome()
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config"), []byte(tmpl), 0644) //nolint:gosec // G306: config files need to be readable
}

const bashrcSourceLine = `# Zerops shell aliases
[ -f "$HOME/.config/zerops/aliases" ] && . "$HOME/.config/zerops/aliases"`

func generateAliases(_ string) error {
	tmpl, err := content.GetTemplate("aliases")
	if err != nil {
		return err
	}
	home := resolveHome()

	// Write managed aliases file (overwritten each run).
	dir := filepath.Join(home, ".config", "zerops")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "aliases"), []byte(tmpl), 0644); err != nil { //nolint:gosec // G306: config files need to be readable
		return err
	}

	// Append source line to .bashrc if not already present.
	bashrcPath := filepath.Join(home, ".bashrc")
	existing, err := os.ReadFile(bashrcPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read .bashrc: %w", err)
	}
	if strings.Contains(string(existing), ".config/zerops/aliases") {
		return nil // already sourced
	}
	f, err := os.OpenFile(bashrcPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open .bashrc: %w", err)
	}
	defer f.Close()
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	if _, err := f.WriteString("\n" + bashrcSourceLine + "\n"); err != nil {
		return err
	}
	return nil
}

// resolveHome returns the user's home directory, falling back to /home/zerops
// when HOME is unset or "/" (common in Zerops initCommands).
func resolveHome() string {
	home := os.Getenv("HOME")
	if home == "" || home == "/" {
		return "/home/zerops"
	}
	return home
}
