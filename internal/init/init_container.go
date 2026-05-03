package init

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/zeropsio/zcp/internal/content"
	"github.com/zeropsio/zcp/internal/runtime"
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
func configureClaude(_ string, _ runtime.Info) error {
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
// installs the Claude Code extension + the zcp-bootstrap companion that
// auto-opens Claude Code as a tab on workspace start. Only called when
// ZCP_VSCODE=true.
func configureVSCode(_ string, _ runtime.Info) error {
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

	// Install zcp-bootstrap (file-based; runs after Anthropic install so the
	// CLI's index update lands first and we extend it without racing).
	fmt.Fprintln(os.Stderr, "    installing zcp-bootstrap extension...")
	if err := installBootstrapExtension(home); err != nil {
		fmt.Fprintf(os.Stderr, "    (warning: bootstrap install failed: %v)\n", err)
	}

	// Point the Claude Code extension at the claude CLI binary.
	if err := patchVSCodeClaudeWrapper(settingsPath); err != nil {
		fmt.Fprintf(os.Stderr, "    (warning: claude wrapper patch failed: %v)\n", err)
	}

	return nil
}

const (
	bootstrapExtName    = "zcp-bootstrap"
	bootstrapExtID      = "zerops.zcp-bootstrap"
	bootstrapExtVersion = "0.1.1"
)

// installBootstrapExtension renders the zcp-bootstrap extension files into
// the code-server extensions dir and registers it in the user-extensions
// index. Idempotent — re-runs overwrite the rendered files and update the
// index entry in place (preserving installedTimestamp on the existing entry).
func installBootstrapExtension(home string) error {
	extDir := filepath.Join(home, ".local", "share", "code-server", "extensions", bootstrapExtName)
	if err := os.MkdirAll(extDir, 0755); err != nil {
		return fmt.Errorf("mkdir bootstrap dir: %w", err)
	}
	if err := writeTemplate("vscode-bootstrap-package.json", filepath.Join(extDir, "package.json")); err != nil {
		return fmt.Errorf("write bootstrap package.json: %w", err)
	}
	if err := writeTemplate("vscode-bootstrap-extension.js", filepath.Join(extDir, "extension.js")); err != nil {
		return fmt.Errorf("write bootstrap extension.js: %w", err)
	}
	indexPath := filepath.Join(home, ".local", "share", "code-server", "extensions", "extensions.json")
	if err := upsertExtensionsIndex(indexPath, extDir); err != nil {
		return fmt.Errorf("update extensions index: %w", err)
	}
	return nil
}

// upsertExtensionsIndex idempotently registers the zcp-bootstrap extension
// in code-server's user-extension index. Other entries are round-tripped
// through []map[string]any so unknown fields they carry (e.g. custom
// metadata written by `code-server --install-extension`) survive the
// rewrite. On re-runs the bootstrap entry's installedTimestamp is
// preserved — without that, every retry of `zcp init` would churn it.
func upsertExtensionsIndex(indexPath, extDir string) error {
	raw, err := os.ReadFile(indexPath)
	var entries []map[string]any
	switch {
	case err == nil:
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &entries); err != nil {
				return fmt.Errorf("parse %s: %w", indexPath, err)
			}
		}
	case os.IsNotExist(err):
		// empty index — start fresh
	default:
		return fmt.Errorf("read %s: %w", indexPath, err)
	}

	var existingTimestamp int64
	filtered := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if extensionEntryID(e) == bootstrapExtID {
			existingTimestamp = extensionEntryTimestamp(e)
			continue
		}
		filtered = append(filtered, e)
	}
	if existingTimestamp == 0 {
		existingTimestamp = time.Now().UnixMilli()
	}

	fileURI := "file://" + extDir
	filtered = append(filtered, map[string]any{
		"identifier": map[string]any{"id": bootstrapExtID},
		"version":    bootstrapExtVersion,
		"location": map[string]any{
			"$mid":     1,
			"fsPath":   extDir,
			"external": fileURI,
			"path":     extDir,
			"scheme":   "file",
		},
		"relativeLocation": bootstrapExtName,
		"metadata": map[string]any{
			"installedTimestamp": existingTimestamp,
			"pinned":             true,
			"source":             "vsix",
		},
	})

	out, err := json.Marshal(filtered)
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(indexPath), 0755); err != nil {
		return fmt.Errorf("mkdir index dir: %w", err)
	}
	return os.WriteFile(indexPath, out, 0644) //nolint:gosec // G306: index file must be readable by code-server
}

// extensionEntryID extracts identifier.id from a generic extensions.json
// entry, returning "" if the field is missing or malformed.
func extensionEntryID(e map[string]any) string {
	id, ok := e["identifier"].(map[string]any)
	if !ok {
		return ""
	}
	s, _ := id["id"].(string)
	return s
}

// extensionEntryTimestamp extracts metadata.installedTimestamp as int64.
// JSON numbers decode to float64 by default; values up to 2^53 (year 287396)
// round-trip without precision loss.
func extensionEntryTimestamp(e map[string]any) int64 {
	md, ok := e["metadata"].(map[string]any)
	if !ok {
		return 0
	}
	t, _ := md["installedTimestamp"].(float64)
	return int64(t)
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
