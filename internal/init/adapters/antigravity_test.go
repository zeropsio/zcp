package adapters_test

import (
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zeropsio/zcp/internal/init/adapters"
	"github.com/zeropsio/zcp/internal/runtime"
)

func stubAgyLookPathFound(_ string) (string, error) {
	return "/home/zerops/.local/bin/agy", nil
}
func stubAgyLookPathMissing(_ string) (string, error) {
	return "", &exec.Error{Name: "agy", Err: exec.ErrNotFound}
}

func stubAgyVersionOutput(version string) func(string, ...string) ([]byte, error) {
	return func(_ string, _ ...string) ([]byte, error) {
		return []byte(version + "\n"), nil
	}
}

func stubAgyVersionError(_ string, _ ...string) ([]byte, error) {
	return nil, errors.New("simulated probe failure")
}

func newAntigravityEnv(t *testing.T, home string) adapters.Env {
	t.Helper()
	return adapters.Env{
		BaseDir:       t.TempDir(),
		Home:          home,
		RT:            runtime.Info{InContainer: true, ServiceName: "zcp"},
		VSCodeWorkDir: "/var/www",
		CommandRunner: func(_ string, _ ...string) error { return nil },
		CommandOutput: stubAgyVersionOutput("1.0.2"),
		LookPath:      stubAgyLookPathFound,
	}
}

func TestAntigravity_Name(t *testing.T) {
	t.Parallel()
	if got := adapters.NewAntigravity().Name(); got != "antigravity" {
		t.Errorf("Name() = %q, want %q (canonical ZCP_AGENT_TYPE value)", got, "antigravity")
	}
}

func TestAntigravity_Detect_BinaryPresent_True(t *testing.T) {
	t.Parallel()
	env := newAntigravityEnv(t, t.TempDir())
	if !adapters.NewAntigravity().Detect(env) {
		t.Error("Detect should be true when agy binary on PATH")
	}
}

func TestAntigravity_Detect_BinaryMissing_False(t *testing.T) {
	t.Parallel()
	env := newAntigravityEnv(t, t.TempDir())
	env.LookPath = stubAgyLookPathMissing
	if adapters.NewAntigravity().Detect(env) {
		t.Error("Detect should be false when agy binary missing")
	}
}

func TestAntigravity_Validate_CurrentVersion_NoWarnings(t *testing.T) {
	t.Parallel()
	env := newAntigravityEnv(t, t.TempDir())
	env.CommandOutput = stubAgyVersionOutput("1.0.2")
	warnings, err := adapters.NewAntigravity().Validate(env)
	if err != nil {
		t.Fatalf("Validate err = %v, want nil", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Validate warnings = %v, want none", warnings)
	}
}

func TestAntigravity_Validate_ProbeError_SoftWarning(t *testing.T) {
	t.Parallel()
	env := newAntigravityEnv(t, t.TempDir())
	env.CommandOutput = stubAgyVersionError
	warnings, err := adapters.NewAntigravity().Validate(env)
	if err != nil {
		t.Fatalf("Validate err = %v, want nil", err)
	}
	if len(warnings) == 0 {
		t.Error("Validate should warn when version probe fails")
	}
}

func TestAntigravity_Validate_EmptyOutput_Warning(t *testing.T) {
	t.Parallel()
	env := newAntigravityEnv(t, t.TempDir())
	env.CommandOutput = stubAgyVersionOutput("")
	warnings, err := adapters.NewAntigravity().Validate(env)
	if err != nil {
		t.Fatalf("Validate err = %v, want nil", err)
	}
	if len(warnings) == 0 {
		t.Error("Validate should warn on empty version output")
	}
}

func TestAntigravity_ContainerInit_FreshHomeWritesBothConfigs(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newAntigravityEnv(t, home)

	if err := adapters.NewAntigravity().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	mcp := loadAntigravityMCP(t, home)
	servers, _ := mcp["mcpServers"].(map[string]any)
	if servers == nil {
		t.Fatalf("mcp_config.json missing mcpServers; got %v", mcp)
	}
	zerops, _ := servers["zerops"].(map[string]any)
	if zerops == nil {
		t.Fatalf("mcp_config.json missing mcpServers.zerops; got %v", servers)
	}
	if zerops["command"] != "zcp" {
		t.Errorf("mcpServers.zerops.command = %v, want %q", zerops["command"], "zcp")
	}
	args, _ := zerops["args"].([]any)
	if len(args) != 1 || args[0] != "serve" {
		t.Errorf("mcpServers.zerops.args = %v, want [\"serve\"]", args)
	}
	if zerops["trust"] != true {
		t.Errorf("mcpServers.zerops.trust = %v, want true", zerops["trust"])
	}

	settings := loadAntigravitySettings(t, home)
	trusted, _ := settings["trustedWorkspaces"].([]any)
	if len(trusted) != 1 || trusted[0] != "/var/www" {
		t.Errorf("trustedWorkspaces = %v, want [\"/var/www\"]", trusted)
	}
}

func TestAntigravity_ContainerInit_PreservesUserAddedServersAndWorkspaces(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newAntigravityEnv(t, home)

	mcpInit := map[string]any{
		"mcpServers": map[string]any{
			"github":     map[string]any{"command": "github-mcp"},
			"playwright": map[string]any{"command": "npx", "args": []any{"-y", "@playwright/mcp"}},
		},
	}
	if err := adapters.SaveJSONFile(filepath.Join(home, ".gemini", "config", "mcp_config.json"), mcpInit); err != nil {
		t.Fatal(err)
	}
	settingsInit := map[string]any{
		"trustedWorkspaces": []any{"/Users/me/other"},
		"telemetryOptIn":    true,
	}
	if err := adapters.SaveJSONFile(filepath.Join(home, ".gemini", "antigravity-cli", "settings.json"), settingsInit); err != nil {
		t.Fatal(err)
	}

	if err := adapters.NewAntigravity().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	mcp := loadAntigravityMCP(t, home)
	servers, _ := mcp["mcpServers"].(map[string]any)
	for _, want := range []string{"github", "playwright", "zerops"} {
		if _, ok := servers[want]; !ok {
			t.Errorf("mcpServers.%s missing after ContainerInit; got %v", want, servers)
		}
	}
	pw, _ := servers["playwright"].(map[string]any)
	pwArgs, _ := pw["args"].([]any)
	if len(pwArgs) != 2 || pwArgs[0] != "-y" || pwArgs[1] != "@playwright/mcp" {
		t.Errorf("playwright args clobbered: %v", pwArgs)
	}

	settings := loadAntigravitySettings(t, home)
	if settings["telemetryOptIn"] != true {
		t.Errorf("top-level user setting lost: telemetryOptIn = %v", settings["telemetryOptIn"])
	}
	trusted, _ := settings["trustedWorkspaces"].([]any)
	seen := map[string]bool{}
	for _, v := range trusted {
		if s, ok := v.(string); ok {
			seen[s] = true
		}
	}
	if !seen["/Users/me/other"] {
		t.Error("user's pre-existing trustedWorkspaces entry lost")
	}
	if !seen["/var/www"] {
		t.Error("VSCodeWorkDir not added to trustedWorkspaces")
	}
}

func TestAntigravity_ContainerInit_TrustedWorkspaceAlreadyPresent_NoDuplicate(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newAntigravityEnv(t, home)

	if err := adapters.SaveJSONFile(
		filepath.Join(home, ".gemini", "antigravity-cli", "settings.json"),
		map[string]any{"trustedWorkspaces": []any{"/var/www"}},
	); err != nil {
		t.Fatal(err)
	}

	if err := adapters.NewAntigravity().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	settings := loadAntigravitySettings(t, home)
	trusted, _ := settings["trustedWorkspaces"].([]any)
	count := 0
	for _, v := range trusted {
		if s, ok := v.(string); ok && s == "/var/www" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("trustedWorkspaces contains /var/www %d times, want 1 (must be idempotent): %v", count, trusted)
	}
}

// TestAntigravity_ContainerInit_PreservesScalarTrustedWorkspaces pins
// that a user-edited `"trustedWorkspaces": "/some/path"` scalar (instead
// of the canonical array) is preserved across the adapter's
// normalization. Without this, the adapter would silently replace the
// scalar with `["/var/www"]` and the user would lose their hand-set
// trusted path. Caught by Codex review of the initial implementation.
func TestAntigravity_ContainerInit_PreservesScalarTrustedWorkspaces(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newAntigravityEnv(t, home)

	if err := adapters.SaveJSONFile(
		filepath.Join(home, ".gemini", "antigravity-cli", "settings.json"),
		map[string]any{"trustedWorkspaces": "/Users/me/other"},
	); err != nil {
		t.Fatal(err)
	}

	if err := adapters.NewAntigravity().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	settings := loadAntigravitySettings(t, home)
	trusted, _ := settings["trustedWorkspaces"].([]any)
	seen := map[string]bool{}
	for _, v := range trusted {
		if s, ok := v.(string); ok {
			seen[s] = true
		}
	}
	if !seen["/Users/me/other"] {
		t.Errorf("user's scalar trustedWorkspaces entry lost during normalization: %v", trusted)
	}
	if !seen["/var/www"] {
		t.Errorf("VSCodeWorkDir not added to trustedWorkspaces: %v", trusted)
	}
}

func TestAntigravity_ContainerInit_Idempotent(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newAntigravityEnv(t, home)

	if err := adapters.NewAntigravity().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	firstMCP := loadAntigravityMCP(t, home)
	firstSettings := loadAntigravitySettings(t, home)
	if err := adapters.NewAntigravity().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	secondMCP := loadAntigravityMCP(t, home)
	secondSettings := loadAntigravitySettings(t, home)

	if !reflect.DeepEqual(firstMCP, secondMCP) {
		t.Errorf("mcp_config not idempotent:\n  first:  %v\n  second: %v", firstMCP, secondMCP)
	}
	if !reflect.DeepEqual(firstSettings, secondSettings) {
		t.Errorf("settings not idempotent:\n  first:  %v\n  second: %v", firstSettings, secondSettings)
	}
}

func TestAntigravity_ContainerInit_EmptyHomeReturnsError(t *testing.T) {
	t.Parallel()
	env := newAntigravityEnv(t, t.TempDir())
	env.Home = ""
	if err := adapters.NewAntigravity().ContainerInit(env); err == nil {
		t.Error("ContainerInit should error when Env.Home is empty")
	}
}

// TestAntigravity_MCPEntry_NoEnvField mirrors the Gemini pin: Antigravity
// is a Gemini CLI fork (product=antigravity, identical MCPServerConfig
// schema) and uses the same permissive parent-env spread at MCP spawn.
// Writing an `env` field here would risk hard-coding stale
// runtime-detection values at init-time — runtime.Detect's serviceId /
// hostname / projectId flow through naturally from the calling shell.
func TestAntigravity_MCPEntry_NoEnvField(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newAntigravityEnv(t, home)
	if err := adapters.NewAntigravity().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	mcp := loadAntigravityMCP(t, home)
	zerops := mcp["mcpServers"].(map[string]any)["zerops"].(map[string]any)

	if _, has := zerops["env"]; has {
		t.Errorf("mcpServers.zerops.env present (%v); Antigravity uses permissive parent-env spread", zerops["env"])
	}
	if _, has := zerops["env_vars"]; has {
		t.Errorf("mcpServers.zerops.env_vars present (%v); env_vars is Codex-specific", zerops["env_vars"])
	}
}

func loadAntigravityMCP(t *testing.T, home string) map[string]any {
	t.Helper()
	configPath := filepath.Join(home, ".gemini", "config", "mcp_config.json")
	data, err := adapters.LoadJSONFile(configPath)
	if err != nil {
		t.Fatalf("load %s: %v", configPath, err)
	}
	return data
}

func loadAntigravitySettings(t *testing.T, home string) map[string]any {
	t.Helper()
	settingsPath := filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
	data, err := adapters.LoadJSONFile(settingsPath)
	if err != nil {
		t.Fatalf("load %s: %v", settingsPath, err)
	}
	return data
}
