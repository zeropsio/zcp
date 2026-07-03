package adapters

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Cursor implements Adapter for the Cursor IDE's headless CLI
// ("Cursor Agent"). The official installer at
// https://cursor.com/install ALWAYS creates two symlinks in
// $HOME/.local/bin, both pointing at the same versioned binary:
//
//   - `cursor-agent` — legacy alias name, the one ZCP keys on.
//   - `agent`        — primary name per the install script, but NOT
//     probed: it is generic enough to collide with unrelated tools.
//     Live-confirmed 2026-07-03 on a Zerops container: the grok CLI
//     also installs to ~/.local/bin/agent (-> ~/.grok/bin/agent ->
//     grok binary), so a container WITHOUT Cursor but WITH grok
//     satisfied the old bare-`agent` fallback — Detect returned true
//     and Validate's `agent --version` probe got grok's own version
//     string (non-empty -> "pass"), writing Cursor configs on a
//     Cursor-less container. ZCP keys ONLY on `cursor-agent`, which
//     the installer always creates regardless.
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
// (Project-scope `.cursor/{cli.json,permissions.json,mcp.json}` are
// written separately by the shared init step
// generateCursorProjectConfig, for both local and container mode.)
type Cursor struct{}

// NewCursor returns a zero-value Cursor adapter. Stateless; env knobs
// flow via Env.
func NewCursor() Cursor { return Cursor{} }

// Name returns "cursor" — the canonical ZCP_AGENT_TYPE value for
// Cursor-equipped containers.
func (Cursor) Name() string { return "cursor" }

// Detect probes ONLY cursor-agent on PATH. The official installer
// always creates it as a symlink pointing at the versioned binary
// under ~/.local/share/cursor-agent/versions/<version>/. The bare
// `agent` name is deliberately NOT probed — see the struct doc
// comment for the live-confirmed grok collision this avoids.
func (Cursor) Detect(env Env) bool {
	lookPath := env.LookPath
	if lookPath == nil {
		lookPath = DefaultLookPath
	}
	_, err := lookPath("cursor-agent")
	return err == nil
}

// Validate runs `cursor-agent --version` to confirm the binary is
// invokable. Cursor v2026.05+ returns a build identifier like
// "2026.05.20-2b5dd59". No version-gated features today; probe
// failure surfaces as a warning only. Only cursor-agent is probed —
// see the struct doc comment for why the bare `agent` fallback was
// removed.
func (Cursor) Validate(env Env) ([]string, error) {
	cmd := env.CommandOutput
	if cmd == nil {
		cmd = DefaultCommandOutput
	}
	out, err := cmd("cursor-agent", "--version")
	if err != nil {
		return []string{
			fmt.Sprintf("cursor-agent --version probe failed: %v (init will continue with defaults)", err),
		}, nil
	}
	if strings.TrimSpace(string(out)) == "" {
		return []string{
			"cursor-agent --version returned empty output (init will continue)",
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
// Cursor consumes.
//
//   - type=stdio is required by Cursor's schema (distinguishes from
//     SSE / streamable HTTP transports).
//   - command + args invoke `zcp serve` via the stdio transport.
//   - env forwards the four vars the zcp serve subprocess needs, via
//     Cursor's documented "${env:NAME}" substitution. This is REQUIRED
//     because Cursor spawns the MCP subprocess with a STRIPPED env
//     (verified 2026-05-24 by wrapping zcp serve with a logger — Cursor
//     passed only HOME/USER/PATH). Without forwarding, zcp serve sees
//     runtime.Detect returning InContainer=false and a missing
//     ZCP_API_KEY.
//
// PASSTHROUGH, NEVER BAKE. "${env:NAME}" is a REFERENCE — Cursor resolves
// it from cursor-agent's own process env at spawn time and passes the
// VALUE to the subprocess; the config file only ever contains the
// reference string, never the secret. The values ARE universally present
// in the container's process environment (live-verified 2026-07-03:
// ZCP_API_KEY + serviceId/hostname/projectId resolve in every on-container
// shell — plain `sh -c`, `bash --norc`, code-server's own process — so
// the reference always resolves for an agent launched on the container).
//
// Writing the RESOLVED value here instead (as grok is forced to by its
// SDK) would leak the ZCP_API_KEY secret in plaintext into
// ~/.cursor/mcp.json AND go stale on rotation — do not. If a specific
// launch context lacks the vars, the fix is to make them present in that
// environment, not to bake them into the config.
func cursorMCPServerEntry() map[string]any {
	return map[string]any{
		"type":    "stdio",
		"command": "zcp",
		"args":    []any{"serve"},
		"env": map[string]any{
			"ZCP_API_KEY": "${env:ZCP_API_KEY}",
			"serviceId":   "${env:serviceId}",
			"hostname":    "${env:hostname}",
			"projectId":   "${env:projectId}",
		},
	}
}
