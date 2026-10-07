package adapters

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Antigravity implements Adapter for Google's Antigravity CLI
// (`agy` binary, installed via the official bootstrap script at
// https://antigravity.google/cli/install.sh which lands the binary at
// $HOME/.local/bin/agy). Product is a Gemini CLI fork (the running
// process self-identifies as `product=antigravity` in its language-
// server logs).
//
// Container targets:
//
//   - ~/.gemini/config/mcp_config.json — Antigravity's dedicated MCP
//     server registry. Distinct from Gemini CLI's ~/.gemini/settings.json
//     because Antigravity migrated config layout to a per-feature
//     directory (~/.gemini/config/.migrated marker).
//   - ~/.gemini/antigravity-cli/settings.json — CLI settings; specifically
//     the trustedWorkspaces array so Antigravity skips the first-run
//     workspace-trust prompt for /var/www, and the Gemini API-key route
//     (see ContainerInit).
type Antigravity struct{}

// antigravityAPIKeyModel is the model label seeded for the Gemini API-key
// route. agy stores the `/model` choice as this display label, not the
// model id, and silently falls back to its route default for a label it
// does not know — so a stale label degrades to agy's own default rather
// than breaking the session.
const antigravityAPIKeyModel = "Gemini 3.8 Flash (High)"

// NewAntigravity returns a zero-value Antigravity adapter. Stateless;
// env knobs flow via Env.
func NewAntigravity() Antigravity { return Antigravity{} }

// Name returns "antigravity" — the canonical ZCP_AGENT_TYPE value for
// Antigravity-equipped containers.
func (Antigravity) Name() string { return "antigravity" }

// Detect probes whether the `agy` binary is on PATH. The official
// bootstrap installs it at $HOME/.local/bin/agy which is on the Zerops
// container PATH by default. False → adapter is skipped silently.
func (Antigravity) Detect(env Env) bool {
	lookPath := env.LookPath
	if lookPath == nil {
		lookPath = DefaultLookPath
	}
	_, err := lookPath("agy")
	return err == nil
}

// Validate runs `agy --version` to confirm the binary is invokable. The
// CLI has no MCP-related version gates we currently care about (the
// MCPServerConfig schema is inherited from Gemini CLI and stable since
// agy 1.0). Probe failure → soft warning, never hard error.
func (Antigravity) Validate(env Env) ([]string, error) {
	cmd := env.CommandOutput
	if cmd == nil {
		cmd = DefaultCommandOutput
	}
	out, err := cmd("agy", "--version")
	if err != nil {
		return []string{
			fmt.Sprintf("agy --version probe failed: %v (init will continue with defaults)", err),
		}, nil
	}
	if strings.TrimSpace(string(out)) == "" {
		return []string{
			"agy --version returned empty output (init will continue)",
		}, nil
	}
	return nil, nil
}

// ContainerInit upserts the ZCP MCP server registration into
// ~/.gemini/config/mcp_config.json and adds VSCodeWorkDir to
// ~/.gemini/antigravity-cli/settings.json::trustedWorkspaces.
// Merge-aware: pre-existing entries / other top-level fields survive.
//
// GEMINI_API_KEY alone does not switch agy off the Google sign-in: it
// reads the key only when settings.json says modelProvider "gemini", and
// otherwise runs on the signed-in account's quota. ZCP owns that field —
// set while the env carries a key, deleted when it does not. On the key
// route agy defaults to a Pro model that a free AI Studio key has no
// quota for, so the Flash label is seeded unless the user already chose
// a model.
func (Antigravity) ContainerInit(env Env) error {
	if env.Home == "" {
		return fmt.Errorf("antigravity adapter: Env.Home is empty")
	}

	mcpPath := filepath.Join(env.Home, ".gemini", "config", "mcp_config.json")
	mcpData, err := LoadJSONFile(mcpPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", mcpPath, err)
	}
	UpsertPath(mcpData, geminiFamilyMCPEntry(), "mcpServers", geminiFamilyMCPServerKey)
	if err := SaveJSONFile(mcpPath, mcpData); err != nil {
		return fmt.Errorf("write %s: %w", mcpPath, err)
	}

	settingsPath := filepath.Join(env.Home, ".gemini", "antigravity-cli", "settings.json")
	settings, err := LoadJSONFile(settingsPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", settingsPath, err)
	}
	vsDir := env.VSCodeWorkDir
	if vsDir == "" {
		vsDir = DefaultVSCodeWorkDir
	}
	// AppendIfMissingString (merge.go) normalizes the historical schema
	// shapes Antigravity has accepted for trustedWorkspaces (nil, []any,
	// or a hand-set scalar path) into an idempotent array.
	settings["trustedWorkspaces"] = AppendIfMissingString(settings["trustedWorkspaces"], vsDir)
	if strings.TrimSpace(os.Getenv("GEMINI_API_KEY")) != "" {
		settings["modelProvider"] = "gemini"
		if !HasPath(settings, "model") {
			settings["model"] = antigravityAPIKeyModel
		}
	} else {
		delete(settings, "modelProvider")
	}
	if err := SaveJSONFile(settingsPath, settings); err != nil {
		return fmt.Errorf("write %s: %w", settingsPath, err)
	}
	return nil
}
