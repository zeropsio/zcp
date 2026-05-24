package adapters

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Canonical MCP server name. Matches templates/mcp-config.json and
// every atom reference (mcp__zerops__zerops_*); see
// TestMCPServerNameCanonical for the cross-package pinning.
const codexMCPServerKey = "zerops"

// CodexMinVersionForHooks is the lowest Codex CLI version that ships
// the stable hooks API (per OpenAI's 2026-05-18 release notes). Codex
// works at lower versions — only hooks-driven gating is unavailable.
// Validate emits a warning, never a hard error.
const CodexMinVersionForHooks = "0.131.0"

// Codex implements Adapter for the OpenAI Codex CLI. The container
// template is expected to install `codex` from npm (`@openai/codex`)
// once Zerops platform team rolls out the multi-agent template; until
// then Detect() returns false and the adapter no-ops gracefully.
//
// Configuration target: ~/.codex/config.toml — structured merge so
// any user-added MCP servers / projects / global Codex settings
// survive the upsert.
type Codex struct{}

// NewCodex returns a zero-value Codex adapter. Like Claude, Codex
// carries no instance state — env knobs flow via Env.
func NewCodex() Codex { return Codex{} }

// Name returns "codex" — the canonical ZCP_AGENT_TYPE value for
// Codex-equipped containers.
func (Codex) Name() string { return "codex" }

// Detect probes whether the `codex` binary is on PATH. False → adapter
// is skipped (existing Claude-only containers are unaffected).
func (Codex) Detect(env Env) bool {
	lookPath := env.LookPath
	if lookPath == nil {
		lookPath = DefaultLookPath
	}
	_, err := lookPath("codex")
	return err == nil
}

// Validate runs `codex --version` and warns if the installed version
// is below the hooks-stable threshold (Codex still works — hooks just
// don't fire on older versions). Returns a hard error only if the
// version probe itself fails in a way that suggests a broken install.
func (Codex) Validate(env Env) ([]string, error) {
	cmd := env.CommandOutput
	if cmd == nil {
		cmd = DefaultCommandOutput
	}
	out, err := cmd("codex", "--version")
	if err != nil {
		// Soft-fail: maybe the binary is present but version probe
		// is broken in some way. Don't block init; surface a warning.
		return []string{
			fmt.Sprintf("codex --version probe failed: %v (init will continue with defaults)", err),
		}, nil
	}
	version := parseCodexVersion(string(out))
	if version == "" {
		return []string{
			fmt.Sprintf("codex --version output not recognized: %q (init will continue)", strings.TrimSpace(string(out))),
		}, nil
	}
	if compareSemver(version, CodexMinVersionForHooks) < 0 {
		return []string{
			fmt.Sprintf("codex %s is below the hooks-stable version %s; hook-driven gating will not fire (Codex itself works)",
				version, CodexMinVersionForHooks),
		}, nil
	}
	return nil, nil
}

// ContainerInit upserts the ZCP MCP server registration + project trust
// into ~/.codex/config.toml. Merge-aware: any other [mcp_servers.*]
// or [projects."*"] entries the user has configured survive the upsert
// byte-for-byte.
//
// Auth + provider env vars (ZCP_AUTH_TYPE, ZCP_PROVIDER, OPENAI_API_KEY)
// belong to Codex's own login flow (`codex login --with-api-key`), not
// to the ZCP MCP server's runtime — so ContainerInit does NOT copy
// them into [mcp_servers.zerops.env]. The MCP server only needs its
// own ZCP_API_KEY, declared via `env_vars` so Codex inherits the value
// from the calling shell at MCP spawn time (NOT a literal
// `${ZCP_API_KEY}` string — Codex doesn't expand variables in `env`
// values; `env_vars` is the documented pass-through mechanism).
func (Codex) ContainerInit(env Env) error {
	if env.Home == "" {
		return fmt.Errorf("codex adapter: Env.Home is empty")
	}

	configPath := filepath.Join(env.Home, ".codex", "config.toml")
	data, err := LoadTOMLFile(configPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", configPath, err)
	}

	// MCP server entry — preserve any other servers (user-added).
	UpsertPath(data, codexMCPServerEntry(), "mcp_servers", codexMCPServerKey)

	// Project trust — preserve any other trusted projects.
	projectPath := env.VSCodeWorkDir
	if projectPath == "" {
		projectPath = DefaultVSCodeWorkDir
	}
	UpsertPath(data, map[string]any{"trust_level": "trusted"}, "projects", projectPath)

	if err := SaveTOMLFile(configPath, data); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}
	return nil
}

// codexMCPServerEntry builds the [mcp_servers.zerops] table contents.
// The structure matches what Codex CLI expects per its MCP docs:
// command + args + env_vars (literal pass-through list — Codex
// inherits the named variables from its calling shell at MCP spawn).
//
// IMPORTANT: Codex does NOT expand `${...}` placeholders inside `env`
// values. Use `env_vars = ["ZCP_API_KEY"]` for shell-env inheritance;
// `env = {KEY = "static-string"}` is for hard-coded values only.
//
// startup_timeout_sec is generous (30s) because the ZCP binary loads
// recipe + atom corpus at boot. tool_timeout_sec covers the longest
// poll (deploy + build phases, up to 10 minutes).
func codexMCPServerEntry() map[string]any {
	return map[string]any{
		"command":             "zcp",
		"args":                []any{"serve"},
		"startup_timeout_sec": int64(30),
		"tool_timeout_sec":    int64(600),
		"env_vars":            []any{"ZCP_API_KEY"},
	}
}

// parseCodexVersion extracts the semver string from Codex's --version
// output. Codex typically prints "codex 0.133.0" or just "0.133.0" —
// regex matches both.
func parseCodexVersion(raw string) string {
	re := regexp.MustCompile(`\b(\d+)\.(\d+)\.(\d+)\b`)
	m := re.FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	return m[0]
}

// compareSemver returns -1 if a < b, 0 if equal, +1 if a > b.
// Inputs must be normalized "MAJOR.MINOR.PATCH" (parseCodexVersion's
// shape). Unparseable parts compare as 0 — defensive against odd
// future formats. Unrolled rather than looped to keep gosec happy
// about fixed-size array indexing (the [3]int bound is compile-time
// known but the linter can't see through a range index).
func compareSemver(a, b string) int {
	ap := splitVersion(a)
	bp := splitVersion(b)
	if c := cmpInt(ap[0], bp[0]); c != 0 {
		return c
	}
	if c := cmpInt(ap[1], bp[1]); c != 0 {
		return c
	}
	return cmpInt(ap[2], bp[2])
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func splitVersion(v string) [3]int {
	parts := strings.SplitN(v, ".", 3)
	var out [3]int
	if len(parts) > 0 {
		out[0], _ = strconv.Atoi(parts[0])
	}
	if len(parts) > 1 {
		out[1], _ = strconv.Atoi(parts[1])
	}
	if len(parts) > 2 {
		out[2], _ = strconv.Atoi(parts[2])
	}
	return out
}
