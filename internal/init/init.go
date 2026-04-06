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
	"github.com/zeropsio/zcp/internal/runtime"
)

const (
	markerBegin = "# ZCP:BEGIN"
	markerEnd   = "# ZCP:END"
)

// step is a named, idempotent init operation.
type step struct {
	name string
	fn   func(string) error
}

// Run executes the init subcommand, generating configuration files in baseDir.
// Steps:
//  1. Generate CLAUDE.md in baseDir
//  2. Configure MCP server in baseDir/.mcp.json (Claude Code project-scoped config)
//  3. Configure permissions in baseDir/.claude/settings.local.json
//  4. Configure SSH in $HOME/.ssh/config (container only, managed section)
//  5. Install shell aliases in $HOME/.config/zerops/aliases + source from .bashrc
//     6-9. Container-only: git config, Claude configs, VS Code settings (if InContainer)
//
// All steps are idempotent — re-running resets to defaults.
func Run(baseDir string, rt runtime.Info) error {
	// Ensure HOME is set — many tools (git, ssh) need it but Zerops
	// containers may start with HOME="" or HOME="/".
	if home := os.Getenv("HOME"); home == "" || home == "/" {
		_ = os.Setenv("HOME", resolveHome())
	}

	steps := []step{
		{"CLAUDE.md", generateCLAUDEMD},
		{"Permissions", generateSettingsLocal},
		{"SSH config", func(_ string) error { return generateSSHConfig(rt) }},
		{"Shell aliases", generateAliases},
	}
	// Local mode: MCP config in project dir (carries ZCP_API_KEY per-project).
	// Container mode: MCP config in ~/.claude.json (global, survives cd into cloned repos).
	if !rt.InContainer {
		steps = append(steps, step{"MCP config", generateMCPConfig})
	}
	if rt.InContainer {
		steps = append(steps, containerSteps()...)
	}

	for _, s := range steps {
		fmt.Fprintf(os.Stderr, "  → %s\n", s.name)
		if err := s.fn(baseDir); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
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

func generateSSHConfig(rt runtime.Info) error {
	if !rt.InContainer {
		fmt.Fprintln(os.Stderr, "    (skipped — not in container, SSH config left unchanged)")
		return nil
	}

	tmpl, err := content.GetTemplate("ssh-config")
	if err != nil {
		return err
	}

	home := resolveHome()
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	path := filepath.Join(dir, "config")
	block := markerBegin + "\n" + strings.TrimRight(tmpl, "\n") + "\n" + markerEnd + "\n"
	return upsertManagedSection(path, block)
}

// upsertManagedSection reads the file at path, finds the ZCP managed block
// (between markerBegin and markerEnd lines), replaces it with content, and
// writes back atomically. If no markers exist, the block is appended.
// If the file doesn't exist, it is created with just the block.
func upsertManagedSection(path, block string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var result string
	text := string(existing)

	beginIdx := strings.Index(text, markerBegin)
	endIdx := strings.Index(text, markerEnd)

	if beginIdx >= 0 && endIdx >= 0 {
		// Replace existing managed section (include the markerEnd line).
		endLineEnd := endIdx + len(markerEnd)
		if endLineEnd < len(text) && text[endLineEnd] == '\n' {
			endLineEnd++
		}
		result = text[:beginIdx] + block + text[endLineEnd:]
	} else {
		// Append managed section.
		if len(text) > 0 && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if len(text) > 0 {
			text += "\n"
		}
		result = text + block
	}

	// Atomic write: temp file + rename.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ssh-config-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.WriteString(result); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	if err := os.Chmod(tmpName, 0644); err != nil {
		os.Remove(tmpName)
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

const shellAliasSourceLine = `# Zerops shell aliases
[ -f "$HOME/.config/zerops/aliases" ] && . "$HOME/.config/zerops/aliases"`

// shellRCFile defines a shell RC file to source aliases from.
type shellRCFile struct {
	name   string
	create bool // create if not exists (.bashrc: yes for compat, .zshrc: no)
}

// shellRCFiles lists shell RC files to patch with alias sourcing.
// .bashrc: always create (backwards compat).
// .zshrc: only patch if it exists (don't create on bash-only systems).
var shellRCFiles = []shellRCFile{
	{".bashrc", true},
	{".zshrc", false},
}

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

	// Append source line to each shell RC file.
	for _, rc := range shellRCFiles {
		if err := patchShellRC(home, rc); err != nil {
			return err
		}
	}
	return nil
}

// patchShellRC appends the alias source line to a shell RC file if not already present.
func patchShellRC(home string, rc shellRCFile) error {
	rcPath := filepath.Join(home, rc.name)
	existing, err := os.ReadFile(rcPath)
	if err != nil {
		if os.IsNotExist(err) {
			if !rc.create {
				return nil // shell not installed, skip
			}
			// Create the file (backwards compat for .bashrc).
		} else {
			return fmt.Errorf("read %s: %w", rc.name, err)
		}
	}
	if strings.Contains(string(existing), ".config/zerops/aliases") {
		return nil // already sourced
	}
	flags := os.O_APPEND | os.O_WRONLY
	if rc.create {
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(rcPath, flags, 0644)
	if err != nil {
		if !rc.create && os.IsNotExist(err) {
			return nil // race: file deleted between ReadFile and OpenFile
		}
		return fmt.Errorf("open %s: %w", rc.name, err)
	}
	defer f.Close()
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	if _, err := f.WriteString("\n" + shellAliasSourceLine + "\n"); err != nil {
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
