package adapters_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
	"github.com/zeropsio/zcp/internal/init/adapters"
	"github.com/zeropsio/zcp/internal/runtime"
)

// Behavioral contract: Claude adapter's ContainerInit MUST be
// merge-aware. The previous implementation overwrote ~/.claude.json on
// every run, silently dropping any mcpServers entries the user had
// added by hand or any top-level fields outside ZCP's owned set.
// Published-product backward compat means we preserve all of that
// across re-init.

func newClaudeEnv(t *testing.T, home string) adapters.Env {
	t.Helper()
	return adapters.Env{
		BaseDir:       t.TempDir(),
		Home:          home,
		RT:            runtime.Info{InContainer: true, ServiceName: "zcp"},
		VSCodeWorkDir: filepath.Join(home, "var-www"),
		CommandRunner: func(_ string, _ ...string) error { return nil },
	}
}

func TestClaude_ContainerInit_FreshHomeWritesExpectedFields(t *testing.T) {
	// Not parallel — t.Setenv mutates process-global state.
	home := t.TempDir()
	env := newClaudeEnv(t, home)
	t.Setenv("ANTHROPIC_API_KEY", "")

	if err := adapters.NewClaude().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	data := loadClaudeJSON(t, home)
	if got := data["theme"]; got != "dark" {
		t.Errorf("theme = %v, want dark (template default)", got)
	}
	if got := data["hasCompletedOnboarding"]; got != true {
		t.Errorf("hasCompletedOnboarding = %v, want true", got)
	}
	servers, _ := data["mcpServers"].(map[string]any)
	if _, ok := servers["zerops"]; !ok {
		t.Errorf("mcpServers.zerops missing, got %v", servers)
	}
	projects, _ := data["projects"].(map[string]any)
	if _, ok := projects[env.VSCodeWorkDir]; !ok {
		t.Errorf("projects[%q] missing, got %v", env.VSCodeWorkDir, projects)
	}
	if _, ok := data["customApiKeyResponses"]; ok {
		t.Error("customApiKeyResponses must be absent when ANTHROPIC_API_KEY unset")
	}
}

func TestClaude_ContainerInit_PreservesUserAddedMCPServers(t *testing.T) {
	// Not parallel — t.Setenv mutates process-global state.
	home := t.TempDir()
	env := newClaudeEnv(t, home)
	t.Setenv("ANTHROPIC_API_KEY", "")

	// Simulate user-edited ~/.claude.json carrying other MCP servers.
	existing := map[string]any{
		"mcpServers": map[string]any{
			"puppeteer": map[string]any{"command": "puppeteer-mcp"},
			"gmail":     map[string]any{"command": "gmail-mcp", "args": []any{"--auth"}},
		},
		"theme":         "light",     // user override
		"customUserKey": "preserved", // unknown-to-ZCP top-level field
		"projects":      map[string]any{"/Users/me/other-project": map[string]any{"trusted": true}},
	}
	writeClaudeJSON(t, home, existing)

	if err := adapters.NewClaude().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	data := loadClaudeJSON(t, home)

	// User's other MCP servers preserved.
	servers, _ := data["mcpServers"].(map[string]any)
	if _, ok := servers["puppeteer"]; !ok {
		t.Error("user's puppeteer MCP server lost across re-init")
	}
	if _, ok := servers["gmail"]; !ok {
		t.Error("user's gmail MCP server lost across re-init")
	}
	if _, ok := servers["zerops"]; !ok {
		t.Error("ZCP server entry not added")
	}

	// User's theme override preserved (template default only fills in absent).
	if got := data["theme"]; got != "light" {
		t.Errorf("user's theme=light overwritten to %v", got)
	}

	// User's unknown top-level key preserved.
	if got := data["customUserKey"]; got != "preserved" {
		t.Errorf("user's customUserKey clobbered: got %v", got)
	}

	// User's other project entry preserved.
	projects, _ := data["projects"].(map[string]any)
	if _, ok := projects["/Users/me/other-project"]; !ok {
		t.Error("user's other-project entry lost")
	}
	if _, ok := projects[env.VSCodeWorkDir]; !ok {
		t.Error("ZCP's VSCodeWorkDir project entry not added")
	}
}

func TestClaude_ContainerInit_IdempotentOnRerun(t *testing.T) {
	// Not parallel — t.Setenv mutates process-global state.
	home := t.TempDir()
	env := newClaudeEnv(t, home)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-"+strings.Repeat("k", 20))

	if err := adapters.NewClaude().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit (first run): %v", err)
	}
	first, _ := os.ReadFile(filepath.Join(home, ".claude.json"))

	if err := adapters.NewClaude().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit (second run): %v", err)
	}
	second, _ := os.ReadFile(filepath.Join(home, ".claude.json"))

	if string(first) != string(second) {
		t.Errorf("ContainerInit not idempotent:\n  first:  %s\n  second: %s", first, second)
	}
}

func TestClaude_ContainerInit_AnthropicAPIKeyTransitionsClearAndSet(t *testing.T) {
	// Not parallel — t.Setenv mutates process-global state.
	home := t.TempDir()
	env := newClaudeEnv(t, home)

	// Run 1: with API key → customApiKeyResponses present.
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-"+strings.Repeat("a", 20))
	if err := adapters.NewClaude().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit (with key): %v", err)
	}
	withKey := loadClaudeJSON(t, home)
	if _, ok := withKey["customApiKeyResponses"]; !ok {
		t.Fatal("customApiKeyResponses missing when API key set")
	}

	// Run 2: without API key → customApiKeyResponses cleared.
	t.Setenv("ANTHROPIC_API_KEY", "")
	if err := adapters.NewClaude().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit (no key): %v", err)
	}
	withoutKey := loadClaudeJSON(t, home)
	if _, ok := withoutKey["customApiKeyResponses"]; ok {
		t.Error("customApiKeyResponses must be removed when API key unset on re-init")
	}
}

// TestBootstrapExtVersion_ParityWithManifest locks that the Go
// BootstrapExtVersion const and the shipped extension manifest's
// "version" field never drift. code-server's extensions.json index —
// written from the Go const, not read back from package.json — is what
// code-server consults to decide whether an extension needs reloading,
// so a drift here can leave a stale extension.js loaded indefinitely
// even though the on-disk manifest says otherwise.
func TestBootstrapExtVersion_ParityWithManifest(t *testing.T) {
	t.Parallel()
	tmpl, err := content.GetTemplate("vscode-bootstrap-package.json")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(tmpl), &manifest); err != nil {
		t.Fatalf("parse vscode-bootstrap-package.json: %v", err)
	}
	if manifest.Version != adapters.BootstrapExtVersion {
		t.Errorf("manifest version = %q, adapters.BootstrapExtVersion = %q — must match", manifest.Version, adapters.BootstrapExtVersion)
	}
}

func TestClaude_Name_Canonical(t *testing.T) {
	t.Parallel()
	if got := adapters.NewClaude().Name(); got != "claude-code" {
		t.Errorf("Name() = %q, want %q (canonical ZCP_AGENT_TYPE value)", got, "claude-code")
	}
}

func TestClaude_Detect_AlwaysTrue(t *testing.T) {
	t.Parallel()
	// Today's contract: Claude adapter runs unconditionally (container
	// template installs Claude). Future change to a `which claude` probe
	// would be a deliberate breaking flag, not a silent shift.
	if !adapters.NewClaude().Detect(adapters.Env{}) {
		t.Error("Claude.Detect should be true today (container template always installs claude)")
	}
}

func TestClaude_Validate_NoWarningsNoError(t *testing.T) {
	t.Parallel()
	warnings, err := adapters.NewClaude().Validate(adapters.Env{})
	if err != nil {
		t.Errorf("Validate err = %v, want nil", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Validate warnings = %v, want none", warnings)
	}
}

func loadClaudeJSON(t *testing.T, home string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("read .claude.json: %v", err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("parse .claude.json: %v\nraw: %s", err, raw)
	}
	return data
}

func writeClaudeJSON(t *testing.T, home string, data map[string]any) {
	t.Helper()
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}
