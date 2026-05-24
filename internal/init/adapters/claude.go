package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/content"
)

const (
	// DefaultVSCodeWorkDir is the in-container workspace path Claude
	// Code's VS Code extension treats as the project root.
	DefaultVSCodeWorkDir = "/var/www"

	bootstrapExtName    = "zcp-bootstrap"
	bootstrapExtID      = "zerops.zcp-bootstrap"
	bootstrapExtVersion = "0.1.1"
)

// DefaultCommandRunner shells out to the named binary. Production
// adapters use this; tests substitute a recorder.
func DefaultCommandRunner(name string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// DefaultCommandOutput runs the named binary and returns its captured
// stdout. Used by adapters that parse command output (Codex version
// probe, future Gemini version probe).
func DefaultCommandOutput(name string, args ...string) ([]byte, error) {
	return exec.CommandContext(context.Background(), name, args...).Output()
}

// DefaultLookPath delegates to exec.LookPath. Used by adapters that
// probe binary presence to drive Detect().
func DefaultLookPath(name string) (string, error) {
	return exec.LookPath(name)
}

// Claude implements Adapter for the Anthropic Claude Code CLI + its
// VS Code extension. The container template pre-installs both; this
// adapter writes the per-user configs they need to start without
// first-run friction.
type Claude struct{}

// NewClaude returns a zero-value Claude adapter. Claude carries no
// instance state — all environment knobs (HOME, VSCode dir, command
// runner) live on Env.
func NewClaude() Claude { return Claude{} }

// Name returns "claude-code" — the canonical value used in container
// provisioning YAML's ZCP_AGENT_TYPE env var.
func (Claude) Name() string { return "claude-code" }

// Detect for Claude is always true: today's Zerops dev container
// template installs Claude unconditionally, and the existing pinning
// tests (TestContainerSteps_ClaudeConfigs_*) run without a real
// `claude` binary in PATH. A future enhancement could probe `which
// claude` once the platform supports skipping Claude install; that's
// behind a deliberate flag (not a breaking change for today's users).
func (Claude) Detect(_ Env) bool { return true }

// Validate has no version-gated features today. Returns ok.
func (Claude) Validate(_ Env) ([]string, error) { return nil, nil }

// ContainerInit writes ~/.claude.json (merge-aware: preserves user-added
// fields), ~/.claude/settings.json (template overwrite — wholly
// ZCP-managed), and — when ZCP_VSCODE=true — the VS Code Claude
// extension + zcp-bootstrap extension + claude wrapper patch.
//
// Merge-aware ~/.claude.json behavior is a backward-compat improvement
// over the previous overwrite: users who hand-added other MCP servers
// (puppeteer, gmail, custom) or top-level fields keep them across
// `zcp init` re-runs.
func (c Claude) ContainerInit(env Env) error {
	if err := configureClaude(env); err != nil {
		return err
	}
	if os.Getenv("ZCP_VSCODE") == "true" {
		if err := configureVSCode(env); err != nil {
			return err
		}
	}
	return nil
}

// configureClaude merges ZCP-owned keys into ~/.claude.json and writes
// ~/.claude/settings.json. The merge:
//
//   - mcpServers.zerops      — always upsert (our MCP server entry)
//   - projects[VSCodeWorkDir]— always upsert (pre-accepted trust+onboarding)
//   - customApiKeyResponses  — set when ANTHROPIC_API_KEY env present,
//     deleted when absent (ZCP owns the field)
//   - top-level template defaults (theme, hasCompletedOnboarding) —
//     set ONLY if user hasn't customized them
//
// Without these injections the Claude CLI prompts for trust/onboarding
// on first interactive launch and the VS Code Claude extension's
// first-run flow shows a subscription/API-key entry screen that
// overrides the bootstrap.
func configureClaude(env Env) error {
	path := filepath.Join(env.Home, ".claude.json")
	data, err := LoadJSONFile(path)
	if err != nil {
		return fmt.Errorf("load ~/.claude.json: %w", err)
	}

	// Top-level defaults from claude.json template — only when user
	// hasn't set them. Lets users override theme freely while a fresh
	// container still lands the right defaults on first init.
	baseTmpl, err := content.GetTemplate("claude.json")
	if err != nil {
		return err
	}
	var base map[string]any
	if err := json.Unmarshal([]byte(baseTmpl), &base); err != nil {
		return fmt.Errorf("parse claude.json template: %w", err)
	}
	for k, v := range base {
		if !HasPath(data, k) {
			data[k] = v
		}
	}

	// ZCP-owned: MCP server entry. Always upsert (preserves any other
	// mcpServers entries the user may have added).
	mcpTmpl, err := content.GetTemplate("mcp-config.json")
	if err != nil {
		return err
	}
	var mcp map[string]any
	if err := json.Unmarshal([]byte(mcpTmpl), &mcp); err != nil {
		return fmt.Errorf("parse mcp-config.json template: %w", err)
	}
	servers, ok := mcp["mcpServers"].(map[string]any)
	if !ok {
		return fmt.Errorf("mcp-config.json: mcpServers not a map")
	}
	for key, entry := range servers {
		UpsertPath(data, entry, "mcpServers", key)
	}

	// ZCP-owned: pre-trusted project entry under VSCodeWorkDir.
	// Shallow-merge (not replace): ZCP keys overwrite their values
	// every init (idempotent), but any extra keys Claude (or a future
	// version) writes inside the entry — e.g. allowedTools augmented
	// during interactive sessions, custom mcpServers added at runtime
	// — survive across re-init.
	vsDir := env.VSCodeWorkDir
	if vsDir == "" {
		vsDir = DefaultVSCodeWorkDir
	}
	ShallowMergeAtPath(data, claudeProjectEntry(), "projects", vsDir)

	// ZCP-owned: API-key pre-approval shim. Present only when env set;
	// removed on re-init when env unset (preserves the original
	// "no API key → no customApiKeyResponses" pinning contract).
	if key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); key != "" {
		last := key
		if len(key) > 20 {
			last = key[len(key)-20:]
		}
		data["customApiKeyResponses"] = map[string]any{
			"approved": []string{last},
			"rejected": []string{},
		}
	} else {
		delete(data, "customApiKeyResponses")
	}

	if err := SaveJSONFile(path, data); err != nil {
		return fmt.Errorf("write ~/.claude.json: %w", err)
	}

	// ~/.claude/settings.json is wholly ZCP-managed (template overwrite).
	return writeTemplateFile("claude-settings.json", filepath.Join(env.Home, ".claude", "settings.json"))
}

// claudeProjectEntry returns the per-project entry Claude Code writes
// after the user accepts the trust dialog and finishes onboarding. The
// shape matches what a real interactive first-run produces, so the VS
// Code Claude extension sees a "settled" project rather than a fresh
// one and skips its own onboarding gating.
func claudeProjectEntry() map[string]any {
	return map[string]any{
		"allowedTools":                            []string{},
		"mcpContextUris":                          []string{},
		"mcpServers":                              map[string]any{},
		"enabledMcpjsonServers":                   []string{},
		"disabledMcpjsonServers":                  []string{},
		"hasTrustDialogAccepted":                  true,
		"projectOnboardingSeenCount":              0,
		"hasClaudeMdExternalIncludesApproved":     false,
		"hasClaudeMdExternalIncludesWarningShown": false,
		"lastGracefulShutdown":                    true,
		"hasCompletedProjectOnboarding":           true,
	}
}

// configureVSCode writes code-server user settings, terminal config, and
// installs the Claude Code extension + the zcp-bootstrap companion that
// auto-opens Claude Code as a tab on workspace start. Only called when
// ZCP_VSCODE=true.
func configureVSCode(env Env) error {
	vsDir := env.VSCodeWorkDir
	if vsDir == "" {
		vsDir = DefaultVSCodeWorkDir
	}

	settingsPath := filepath.Join(env.Home, ".local", "share", "code-server", "User", "settings.json")
	if err := writeTemplateFile("vscode-settings.json", settingsPath); err != nil {
		return err
	}
	if err := writeTemplateFile("vscode-terminals.json", filepath.Join(vsDir, ".vscode", "terminals.json")); err != nil {
		return err
	}

	run := env.CommandRunner
	if run == nil {
		run = DefaultCommandRunner
	}

	// Install Claude Code extension (idempotent — skips if already installed).
	fmt.Fprintln(os.Stderr, "    installing claude-code extension...")
	if err := run("code-server", "--install-extension", "Anthropic.claude-code"); err != nil {
		// Non-fatal: extension install failure should not block init.
		fmt.Fprintf(os.Stderr, "    (warning: extension install failed: %v)\n", err)
	}

	// Install zcp-bootstrap (file-based; runs after Anthropic install so
	// the CLI's index update lands first and we extend it without racing).
	fmt.Fprintln(os.Stderr, "    installing zcp-bootstrap extension...")
	if err := installBootstrapExtension(env.Home); err != nil {
		fmt.Fprintf(os.Stderr, "    (warning: bootstrap install failed: %v)\n", err)
	}

	// Point the Claude Code extension at the claude CLI binary.
	if err := patchVSCodeClaudeWrapper(settingsPath); err != nil {
		fmt.Fprintf(os.Stderr, "    (warning: claude wrapper patch failed: %v)\n", err)
	}

	return nil
}

// installBootstrapExtension renders the zcp-bootstrap extension files
// into the code-server extensions dir and registers it in the
// user-extensions index. Idempotent — re-runs overwrite the rendered
// files and update the index entry in place (preserving
// installedTimestamp on the existing entry).
func installBootstrapExtension(home string) error {
	extDir := filepath.Join(home, ".local", "share", "code-server", "extensions", bootstrapExtName)
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		return fmt.Errorf("mkdir bootstrap dir: %w", err)
	}
	if err := writeTemplateFile("vscode-bootstrap-package.json", filepath.Join(extDir, "package.json")); err != nil {
		return fmt.Errorf("write bootstrap package.json: %w", err)
	}
	if err := writeTemplateFile("vscode-bootstrap-extension.js", filepath.Join(extDir, "extension.js")); err != nil {
		return fmt.Errorf("write bootstrap extension.js: %w", err)
	}
	indexPath := filepath.Join(home, ".local", "share", "code-server", "extensions", "extensions.json")
	if err := upsertExtensionsIndex(indexPath, extDir); err != nil {
		return fmt.Errorf("update extensions index: %w", err)
	}
	return nil
}

// upsertExtensionsIndex idempotently registers the zcp-bootstrap
// extension in code-server's user-extension index. Other entries are
// round-tripped through []map[string]any so unknown fields they carry
// (e.g. custom metadata written by `code-server --install-extension`)
// survive the rewrite. On re-runs the bootstrap entry's
// installedTimestamp is preserved — without that, every retry of
// `zcp init` would churn it.
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
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return fmt.Errorf("mkdir index dir: %w", err)
	}
	return os.WriteFile(indexPath, out, 0o644) //nolint:gosec // G306: index file must be readable by code-server
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
// JSON numbers decode to float64 by default; values up to 2^53 (year
// 287396) round-trip without precision loss.
func extensionEntryTimestamp(e map[string]any) int64 {
	md, ok := e["metadata"].(map[string]any)
	if !ok {
		return 0
	}
	t, _ := md["installedTimestamp"].(float64)
	return int64(t)
}

// patchVSCodeClaudeWrapper adds claudeCode.claudeProcessWrapper to VS
// Code settings, pointing at the absolute path of the claude binary.
// Without this the extension can't locate claude in the container PATH.
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

	if err := os.WriteFile(settingsPath, append(out, '\n'), 0o644); err != nil { //nolint:gosec // G306: config files need to be readable
		return fmt.Errorf("write settings: %w", err)
	}
	return nil
}

// writeTemplateFile fetches a named template from internal/content and
// writes it verbatim to path, creating parent dirs as needed. Used for
// wholly-ZCP-managed files where no merge is required.
//
// Adapter-local helper — avoids importing init package (which would
// create a cycle init → adapters → init). content/ is fine to import
// since the dependency layer is content/ ← init/adapters/.
func writeTemplateFile(templateName, path string) error {
	tmpl, err := content.GetTemplate(templateName)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return os.WriteFile(path, []byte(tmpl), 0o644) //nolint:gosec // G306: config files need to be readable
}
