package adapters

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/runtime"
)

// grokMCPServerKey is the canonical MCP server identifier ZCP writes into
// grok's settings. Matches the key every atom references via mcp__zerops__*
// and the value Claude / Codex / Gemini use — pinned cross-package by
// content.TestMCPServerNameCanonical. grok derives its own id from this label
// via toMcpServerId (lowercase + slug); "zerops" is already in that form.
const grokMCPServerKey = "zerops"

// Grok implements Adapter for the grok CLI (superagent-ai/grok-cli, npm
// package `grok-dev`, binary `grok`). The container template is expected to
// install it once the Zerops multi-agent template rolls out; until then
// Detect() returns false and the adapter no-ops gracefully.
//
// Configuration target: ~/.grok/user-settings.json — grok's USER-scope
// settings file. MCP servers live ONLY in user settings (the project-scope
// .grok/settings.json schema has no mcp field — verified against grok-dev's
// own type declarations: UserSettings.mcp exists, ProjectSettings.mcp does
// not), so a single user-scope write registers ZCP for every grok invocation
// regardless of cwd.
//
// Schema (grok-dev's McpSettings / McpServerConfig): the servers live in an
// ARRAY under `mcp.servers`, NOT a name-keyed object — each entry carries
// {id, label, enabled, transport, command, args, env, cwd}. This differs from
// the Claude/Cursor/Gemini `mcpServers` object shape; the array is grok's own.
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
// version-gated features today; probe failure surfaces as a warning only
// (grok's bin runs under bun — a node-only container may have the symlink
// present but no runtime, which is a soft, operator-visible condition, not a
// reason to skip the config write).
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

// ContainerInit upserts the ZCP MCP server into ~/.grok/user-settings.json.
// Merge-aware: top-level user settings (apiKey, defaultModel, sandbox, telegram,
// hooks, …) and any other servers in the mcp.servers array survive the upsert.
// Idempotent: the zerops entry is replaced in place on re-runs (never
// duplicated), keyed on id.
func (Grok) ContainerInit(env Env) error {
	if env.Home == "" {
		return fmt.Errorf("grok adapter: Env.Home is empty")
	}

	configPath := filepath.Join(env.Home, ".grok", "user-settings.json")
	data, err := LoadJSONFile(configPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", configPath, err)
	}

	mcp, _ := data["mcp"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
	}
	mcp["servers"] = upsertMCPServerByID(mcp["servers"], grokMCPServerKey, grokMCPServerEntry(env.RT))
	data["mcp"] = mcp

	if err := SaveJSONFile(configPath, data); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}
	return nil
}

// grokMCPServerEntry builds the McpServerConfig object grok consumes for the
// zerops stdio server.
//
// Env handling is the load-bearing detail. grok spawns the MCP subprocess via
// the MCP TS SDK's StdioClientTransport, whose env is
// `{...getDefaultEnvironment(), ...server.env}`:
//
//   - getDefaultEnvironment() inherits ONLY a fixed safe set from grok's own
//     process env at spawn time: HOME, LOGNAME, PATH, SHELL, TERM, USER. So
//     PATH / HOME flow through live — we must NOT bake them (a baked init-time
//     value would shadow the real one).
//   - server.env values are passed VERBATIM — grok performs no ${VAR}
//     interpolation (unlike Cursor's "${env:NAME}"). So the Zerops-injected
//     runtime-detection vars (serviceId / hostname / projectId) and ZCP_API_KEY
//     — none of which are in the SDK's default-inherited set — must be written
//     as RESOLVED LITERAL VALUES, or the SDK strips them.
//
// Missing serviceId/hostname/projectId reproduces the Codex/Cursor bug class:
// zcp serve sees serviceId="" → runtime.Detect returns InContainer=false →
// bootstrap ships local-mode atoms into a session that's actually inside a
// Zerops container. Missing ZCP_API_KEY → API auth fails.
//
// The three identifiers come from env.RT (already resolved by runtime.Detect at
// init); ZCP_API_KEY is read from the init process env (same pattern as Claude's
// ANTHROPIC_API_KEY). Each is omitted when empty so no phantom blank values
// land in the config.
func grokMCPServerEntry(rt runtime.Info) map[string]any {
	serverEnv := map[string]any{}
	if rt.ServiceID != "" {
		serverEnv["serviceId"] = rt.ServiceID
	}
	if rt.ServiceName != "" {
		serverEnv["hostname"] = rt.ServiceName
	}
	if rt.ProjectID != "" {
		serverEnv["projectId"] = rt.ProjectID
	}
	if key := strings.TrimSpace(os.Getenv("ZCP_API_KEY")); key != "" {
		serverEnv["ZCP_API_KEY"] = key
	}

	return map[string]any{
		"id":        grokMCPServerKey,
		"label":     grokMCPServerKey,
		"enabled":   true,
		"transport": "stdio",
		"command":   "zcp",
		"args":      []any{"serve"},
		"env":       serverEnv,
	}
}

// upsertMCPServerByID replaces the server whose "id" equals id (preserving its
// position) with entry, or appends entry when absent. Other servers (user-added
// MCP entries) survive untouched. Defensively normalizes nil / non-array
// existing values — a hand-corrupted mcp.servers won't drop the ZCP entry.
func upsertMCPServerByID(existing any, id string, entry map[string]any) []any {
	servers, _ := existing.([]any)
	for i, s := range servers {
		if m, ok := s.(map[string]any); ok {
			if sid, _ := m["id"].(string); sid == id {
				servers[i] = entry
				return servers
			}
		}
	}
	return append(servers, entry)
}
