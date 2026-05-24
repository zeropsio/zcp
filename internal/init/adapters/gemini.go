package adapters

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Gemini implements Adapter for the Google Gemini CLI (`gemini` binary,
// npm @google/gemini-cli). Container target: ~/.gemini/settings.json —
// the user-scope settings file Gemini reads at startup. mcpServers entry
// here registers the ZCP MCP server for every gemini invocation
// regardless of cwd.
//
// Configuration model: structured merge so any user-added MCP servers
// or top-level Gemini settings (theme, model selection, includeDirectories)
// survive the upsert byte-for-byte.
type Gemini struct{}

// NewGemini returns a zero-value Gemini adapter. Like Claude / Codex,
// Gemini carries no instance state — env knobs flow via Env.
func NewGemini() Gemini { return Gemini{} }

// Name returns "gemini" — the canonical ZCP_AGENT_TYPE value for
// Gemini-equipped containers.
func (Gemini) Name() string { return "gemini" }

// Detect probes whether the `gemini` binary is on PATH. False → adapter
// is skipped (existing Claude/Codex-only containers are unaffected).
func (Gemini) Detect(env Env) bool {
	lookPath := env.LookPath
	if lookPath == nil {
		lookPath = DefaultLookPath
	}
	_, err := lookPath("gemini")
	return err == nil
}

// Validate runs `gemini --version` to confirm the binary is invokable.
// No version-gated features today (gemini's MCP config schema has been
// stable since 0.30+). Returns soft warning on probe failure — never a
// hard error.
func (Gemini) Validate(env Env) ([]string, error) {
	cmd := env.CommandOutput
	if cmd == nil {
		cmd = DefaultCommandOutput
	}
	out, err := cmd("gemini", "--version")
	if err != nil {
		return []string{
			fmt.Sprintf("gemini --version probe failed: %v (init will continue with defaults)", err),
		}, nil
	}
	if strings.TrimSpace(string(out)) == "" {
		return []string{
			"gemini --version returned empty output (init will continue)",
		}, nil
	}
	return nil, nil
}

// ContainerInit upserts the ZCP MCP server registration into
// ~/.gemini/settings.json. Merge-aware: any other mcpServers entries
// or top-level Gemini settings the user has configured survive
// untouched.
func (Gemini) ContainerInit(env Env) error {
	if env.Home == "" {
		return fmt.Errorf("gemini adapter: Env.Home is empty")
	}

	configPath := filepath.Join(env.Home, ".gemini", "settings.json")
	data, err := LoadJSONFile(configPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", configPath, err)
	}

	UpsertPath(data, geminiFamilyMCPEntry(), "mcpServers", geminiFamilyMCPServerKey)

	if err := SaveJSONFile(configPath, data); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}
	return nil
}
