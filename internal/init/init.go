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
	// Shell-comment markers used in files parsed as shell config (e.g. SSH config).
	shellMarkerBegin = "# ZCP:BEGIN"
	shellMarkerEnd   = "# ZCP:END"
	// HTML-comment markers used in files rendered as markdown (e.g. CLAUDE.md).
	// Invisible in rendered output but work as text markers for upsert.
	mdMarkerBegin = "<!-- ZCP:BEGIN -->"
	mdMarkerEnd   = "<!-- ZCP:END -->"
	// Reflog section marker — historical bootstrap records appended by the
	// workflow engine. Must be preserved across re-init.
	reflogMarker = "<!-- ZEROPS:REFLOG -->"
)

// step is a named, idempotent init operation. The runtime.Info argument
// lets steps that need env-aware behaviour (currently CLAUDE.md render)
// pick the right shape; steps that don't simply ignore it.
type step struct {
	name string
	fn   func(string, runtime.Info) error
}

// Run executes the init subcommand, generating configuration files in baseDir.
// All steps are idempotent — re-running resets to defaults.
func Run(baseDir string, rt runtime.Info) error {
	// Ensure HOME is set — many tools (git, ssh) need it but Zerops
	// containers may start with HOME="" or HOME="/".
	if home := os.Getenv("HOME"); home == "" || home == "/" {
		_ = os.Setenv("HOME", resolveHome())
	}

	// Shared steps (both local and container).
	steps := []step{
		{"CLAUDE.md", generateCLAUDEMD},
		{"Permissions", generateSettingsLocal},
		{"Shell aliases", generateAliases},
	}
	if rt.InContainer {
		// Container: SSH config, git identity, Claude configs (incl. global MCP server).
		steps = append(steps, step{"SSH config", generateSSHConfig})
		steps = append(steps, containerSteps()...)
	} else {
		// Local: project-scoped .mcp.json (carries ZCP_API_KEY per-project).
		steps = append(steps, step{"MCP config", generateMCPConfig})
	}

	for _, s := range steps {
		fmt.Fprintf(os.Stderr, "  → %s\n", s.name)
		if err := s.fn(baseDir, rt); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
	}

	fmt.Fprintln(os.Stderr, "  ✓ Init complete")
	return nil
}

// writeTemplate reads a named template and writes it to path, creating parent dirs.
func writeTemplate(templateName, path string) error {
	tmpl, err := content.GetTemplate(templateName)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return os.WriteFile(path, []byte(tmpl), 0644) //nolint:gosec // G306: config files need to be readable
}

// generateCLAUDEMD writes the env-rendered CLAUDE.md to baseDir, wrapped
// in <!-- ZCP:BEGIN/END --> markers. The body is composed by
// content.BuildClaudeMD from one env-specific preamble (container or
// local) plus the env-agnostic shared body. Container preamble has its
// {{.SelfHostname}} template var resolved to rt.ServiceName at render
// time. Re-runs replace only the marked section, preserving any
// trailing content (REFLOG entries appended by bootstrap, user
// additions outside the markers).
//
// Migration path: if the file exists without markers but contains a
// REFLOG section (legacy layout where the template was overwritten
// verbatim), the pre-REFLOG portion is replaced by the marker-wrapped
// body while REFLOG onwards is preserved.
func generateCLAUDEMD(baseDir string, rt runtime.Info) error {
	body, err := content.BuildClaudeMD(rt)
	if err != nil {
		return err
	}
	path := filepath.Join(baseDir, "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	block := mdMarkerBegin + "\n" + strings.TrimRight(body, "\n") + "\n" + mdMarkerEnd + "\n"

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read CLAUDE.md: %w", err)
	}
	text := string(existing)

	if strings.Contains(text, mdMarkerBegin) && strings.Contains(text, mdMarkerEnd) {
		return upsertManagedSection(path, block, mdMarkerBegin, mdMarkerEnd)
	}
	if idx := strings.Index(text, reflogMarker); idx >= 0 {
		return os.WriteFile(path, []byte(block+"\n"+text[idx:]), 0644) //nolint:gosec // G306: config file
	}
	return os.WriteFile(path, []byte(block), 0644) //nolint:gosec // G306: config file
}

func generateMCPConfig(baseDir string, _ runtime.Info) error {
	return writeTemplate("mcp-config.json", filepath.Join(baseDir, ".mcp.json"))
}

func generateSettingsLocal(baseDir string, _ runtime.Info) error {
	return writeTemplate("settings-local.json", filepath.Join(baseDir, ".claude", "settings.local.json"))
}

func generateSSHConfig(_ string, _ runtime.Info) error {
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
	block := shellMarkerBegin + "\n" + strings.TrimRight(tmpl, "\n") + "\n" + shellMarkerEnd + "\n"
	return upsertManagedSection(path, block, shellMarkerBegin, shellMarkerEnd)
}

// upsertManagedSection reads the file at path, finds the managed block
// (between beginMarker and endMarker lines), replaces it with block, and
// writes back atomically. If no markers exist, block is appended. If the
// file doesn't exist, it is created with just block.
func upsertManagedSection(path, block, beginMarker, endMarker string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var result string
	text := string(existing)

	beginIdx := strings.Index(text, beginMarker)
	endIdx := strings.Index(text, endMarker)

	if beginIdx >= 0 && endIdx >= 0 {
		// Replace existing managed section (include the endMarker line).
		endLineEnd := endIdx + len(endMarker)
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

func generateAliases(_ string, _ runtime.Info) error {
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
