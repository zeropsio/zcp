package adapters

import (
	"fmt"
	"path/filepath"
	"strings"
)

// grokMCPServerKey is the canonical MCP server identifier ZCP writes into
// grok's config. Matches the key every atom references via mcp__zerops__*
// and the value Claude / Codex / Cursor use — pinned cross-package by
// content.TestMCPServerNameCanonical. In grok's config.toml it is the table
// name: [mcp_servers.zerops].
const grokMCPServerKey = "zerops"

// Grok implements Adapter for xAI's official grok CLI (native binary `grok`,
// e.g. `grok 0.2.73`, subcommands agent/mcp/leader/…). The container template
// is expected to install it once the Zerops multi-agent template rolls out;
// until then Detect() returns false and the adapter no-ops gracefully.
//
// Configuration target: ~/.grok/config.toml — grok's USER-scope config
// (`grok mcp add -s user` writes here; `grok mcp list`/`doctor` read it).
// MCP servers live under `[mcp_servers.<name>]` tables — a name-keyed object,
// NOT the array-of-{id,label,…} shape used by the unrelated superagent-ai
// grok-cli (npm `grok-dev`). Live-verified 2026-07-03: grok reads config.toml
// (a user-settings.json written in the other tool's format is ignored — `grok
// mcp list` shows nothing).
//
// NO env block. grok spawns the MCP subprocess inheriting its OWN process env
// (live-verified via `grok mcp doctor`: a [mcp_servers.zerops] entry with no
// env → "server started, handshake OK, 22 tools discovered"). ZCP_API_KEY +
// serviceId/hostname/projectId are all present in grok's container env, so they
// flow through automatically — we must NEVER write them here (a baked
// ZCP_API_KEY would leak the plaintext secret to disk; baked ids would go
// stale). Contrast Cursor, which strips the subprocess env and so needs an
// explicit "${env:NAME}" reference block.
type Grok struct{}

// NewGrok returns a zero-value Grok adapter. Stateless; env knobs flow via Env.
func NewGrok() Grok { return Grok{} }

// Name returns "grok" — the canonical ZCP_AGENT_TYPE value for grok-equipped
// containers and the launcher's agent id.
func (Grok) Name() string { return "grok" }

// Detect probes whether the `grok` binary is on PATH. False → adapter is
// skipped silently (Claude/Codex-only containers are unaffected).
func (Grok) Detect(env Env) bool {
	lookPath := env.LookPath
	if lookPath == nil {
		lookPath = DefaultLookPath
	}
	_, err := lookPath("grok")
	return err == nil
}

// Validate runs `grok --version` to confirm the binary is invokable. No
// version-gated features today; probe failure surfaces as a warning only.
func (Grok) Validate(env Env) ([]string, error) {
	cmd := env.CommandOutput
	if cmd == nil {
		cmd = DefaultCommandOutput
	}
	out, err := cmd("grok", "--version")
	if err != nil {
		return []string{
			fmt.Sprintf("grok --version probe failed: %v (init will continue with defaults)", err),
		}, nil
	}
	if strings.TrimSpace(string(out)) == "" {
		return []string{
			"grok --version returned empty output (init will continue)",
		}, nil
	}
	return nil, nil
}

// ContainerInit upserts the ZCP MCP server into ~/.grok/config.toml under
// [mcp_servers.zerops]. Merge-aware: the [cli] section, any other user
// [mcp_servers.*] tables, and unknown top-level keys survive. Idempotent: the
// zerops table is replaced in place on re-runs (the TOML encoder is
// deterministic — see SaveTOMLFile), never duplicated.
func (Grok) ContainerInit(env Env) error {
	if env.Home == "" {
		return fmt.Errorf("grok adapter: Env.Home is empty")
	}

	configPath := filepath.Join(env.Home, ".grok", "config.toml")
	data, err := LoadTOMLFile(configPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", configPath, err)
	}

	servers, _ := data["mcp_servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[grokMCPServerKey] = grokMCPServerEntry()
	data["mcp_servers"] = servers

	if err := SaveTOMLFile(configPath, data); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}
	return nil
}

// grokMCPServerEntry builds the [mcp_servers.zerops] table grok consumes.
//
// stdio is grok's default transport, so the entry is just command + args +
// enabled — matching exactly what `grok mcp add zerops zcp serve` writes.
// Deliberately NO env field: grok inherits its process env into the subprocess
// (see the Grok type doc), so the four vars zcp serve needs flow through
// without ZCP writing any of them — no secret on disk, no stale ids.
func grokMCPServerEntry() map[string]any {
	return map[string]any{
		"command": "zcp",
		"args":    []any{"serve"},
		"enabled": true,
	}
}
