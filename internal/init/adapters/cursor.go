package adapters

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Cursor implements Adapter for the Cursor IDE's headless CLI
// ("Cursor Agent"). The official installer at
// https://cursor.com/install lands two symlinks in $HOME/.local/bin:
//
//   - `agent`        — primary binary name (per install script v2026.05.20)
//   - `cursor-agent` — legacy alias, kept for backward-compat
//
// Detect probes both — prefers `cursor-agent` because the bare `agent`
// name is generic enough to collide with unrelated tools, but accepts
// either since the installer always creates both.
//
// Configuration target: ~/.cursor/mcp.json — Cursor's user-scope MCP
// registry. Schema (per https://cursor.com/docs/context/mcp):
//
//	{
//	  "mcpServers": {
//	    "<name>": {
//	      "type": "stdio",
//	      "command": "...",
//	      "args": [...],
//	      "env": {...},        // optional, "${env:NAME}" interpolation
//	      "envFile": "..."     // optional, alternative to env
//	    }
//	  }
//	}
//
// Project-scope `.cursor/mcp.json` is also supported by Cursor but
// the container init writes only the user-scope file — `agent` reads
// both and merges, so user-scope is sufficient for "ZCP everywhere".
type Cursor struct{}

// NewCursor returns a zero-value Cursor adapter. Stateless; env knobs
// flow via Env.
func NewCursor() Cursor { return Cursor{} }

// Name returns "cursor" — the canonical ZCP_AGENT_TYPE value for
// Cursor-equipped containers.
func (Cursor) Name() string { return "cursor" }

// Detect probes the cursor-agent / agent binaries on PATH. Returns
// true if either is present. The official installer creates both as
// symlinks pointing at the same versioned binary under
// ~/.local/share/cursor-agent/versions/<version>/.
func (Cursor) Detect(env Env) bool {
	lookPath := env.LookPath
	if lookPath == nil {
		lookPath = DefaultLookPath
	}
	if _, err := lookPath("cursor-agent"); err == nil {
		return true
	}
	_, err := lookPath("agent")
	return err == nil
}

// Validate runs `cursor-agent --version` (or `agent --version` as
// fallback) to confirm the binary is invokable. Cursor v2026.05+
// returns a build identifier like "2026.05.20-2b5dd59". No
// version-gated features today; probe failure surfaces as a warning
// only.
func (Cursor) Validate(env Env) ([]string, error) {
	cmd := env.CommandOutput
	if cmd == nil {
		cmd = DefaultCommandOutput
	}
	lookPath := env.LookPath
	if lookPath == nil {
		lookPath = DefaultLookPath
	}
	bin := "cursor-agent"
	if _, err := lookPath(bin); err != nil {
		bin = "agent"
	}
	out, err := cmd(bin, "--version")
	if err != nil {
		return []string{
			fmt.Sprintf("%s --version probe failed: %v (init will continue with defaults)", bin, err),
		}, nil
	}
	if strings.TrimSpace(string(out)) == "" {
		return []string{
			fmt.Sprintf("%s --version returned empty output (init will continue)", bin),
		}, nil
	}
	return nil, nil
}

// ContainerInit configures Cursor for low-friction headless use:
//
//  1. ~/.cursor/mcp.json — upsert the zerops MCP server entry
//     (merge-aware: pre-existing user entries survive).
//  2. ~/.cursor/projects/<flat-workspace>/.workspace-trusted —
//     pre-trust the workspace so first interactive `agent` run doesn't
//     gate on the workspace-trust prompt.
//
// Auth handling stays operator-driven: Cursor authenticates via
// `CURSOR_API_KEY` env var OR `cursor-agent login` OAuth flow.
//
// MCP server APPROVAL is intentionally NOT pre-written. The approval
// id format is a sha256-derived hash of the server entry + project
// path (visible in `~/.cursor/projects/<workspace>/mcp-approvals.json`
// as `zerops-<16hex>`), but empirical attempts to mirror Cursor's
// canonicalization didn't match for non-trivial cases — and
// `cursor-agent mcp enable` short-circuits as "already enabled and
// approved" against server-side cached state for logged-in users
// without writing the local file. Two operator paths cover the gap:
//
//   - Headless / scripted: `agent -p --approve-mcps "..."` auto-
//     approves all configured MCP servers for the invocation. Zero
//     state, fully scriptable.
//   - Interactive: `agent` in the workspace shows a one-time y/N
//     prompt the first time it tries to call a zerops_* tool. After
//     "y", the approval persists in Cursor's per-workspace state
//     (and synced server-side if user is logged in).
//
// The same pattern as Gemini's operator-driven `security.auth.selectedType`
// or Claude's `claude login` — the adapter's writes are independent of
// the agent's own auth/approval lifecycle.
func (Cursor) ContainerInit(env Env) error {
	if env.Home == "" {
		return fmt.Errorf("cursor adapter: Env.Home is empty")
	}

	configPath := filepath.Join(env.Home, ".cursor", "mcp.json")
	data, err := LoadJSONFile(configPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", configPath, err)
	}
	UpsertPath(data, cursorMCPServerEntry(), "mcpServers", "zerops")
	if err := SaveJSONFile(configPath, data); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	vsDir := env.VSCodeWorkDir
	if vsDir == "" {
		vsDir = DefaultVSCodeWorkDir
	}
	trustPath := filepath.Join(env.Home, ".cursor", "projects", cursorWorkspaceDir(vsDir), ".workspace-trusted")
	trustEntry := map[string]any{
		"trustedAt":     time.Now().UTC().Format(time.RFC3339Nano),
		"workspacePath": vsDir,
	}
	if err := SaveJSONFile(trustPath, trustEntry); err != nil {
		return fmt.Errorf("write %s: %w", trustPath, err)
	}
	return nil
}

// cursorWorkspaceDir maps a workspace absolute path to the directory
// name Cursor uses under ~/.cursor/projects/. Cursor flattens slashes
// to dashes and strips the leading slash:
//
//	/var/www                → var-www
//	/Users/me/myproject     → Users-me-myproject
//	/tmp/cursor-fresh-test  → tmp-cursor-fresh-test
//
// (Verified empirically against Cursor v2026.05.20-2b5dd59 — `find
// ~/.cursor/projects` after `cd <path> && agent mcp enable zerops`.)
func cursorWorkspaceDir(workspacePath string) string {
	trimmed := strings.TrimPrefix(workspacePath, "/")
	return strings.ReplaceAll(trimmed, "/", "-")
}

// cursorMCPServerEntry builds the mcpServers["zerops"] object that
// Cursor consumes. Minimal shape — `type=stdio` is required (Cursor
// distinguishes stdio from SSE / streamable HTTP transports), then
// command + args. NO `env` field by design: Cursor's MCP subprocess
// spawn (confirmed empirically 2026-05-24 — `agent mcp list-tools
// zerops` enumerated all 21 tools without env enumeration) inherits
// parent env so ZCP_API_KEY / serviceId / hostname / projectId /
// PATH / HOME flow through naturally. Codex's restrictive `env_vars`
// list is Codex-specific; Gemini-family + Cursor all use permissive
// parent-env spread.
func cursorMCPServerEntry() map[string]any {
	return map[string]any{
		"type":    "stdio",
		"command": "zcp",
		"args":    []any{"serve"},
	}
}
