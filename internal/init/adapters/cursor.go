package adapters

import (
	"fmt"
	"path/filepath"
	"strings"
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

// ContainerInit upserts the ZCP MCP server registration into
// ~/.cursor/mcp.json. Merge-aware: any other mcpServers entries the
// user has configured survive the upsert.
//
// Auth handling: out of scope. Cursor authenticates via the
// `CURSOR_API_KEY` env var OR an interactive `agent login` OAuth
// flow — operator picks one. The adapter's MCP config write is
// independent of auth: mcp.json is loaded regardless and
// mcpServers.zerops registers even before login.
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
	return nil
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
