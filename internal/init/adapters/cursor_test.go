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

// Cursor installer creates BOTH /home/<user>/.local/bin/cursor-agent
// AND /home/<user>/.local/bin/agent — Detect must accept either.

func stubCursorLookPathBothFound(name string) (string, error) {
	switch name {
	case "cursor-agent":
		return "/home/zerops/.local/bin/cursor-agent", nil
	case "agent":
		return "/home/zerops/.local/bin/agent", nil
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

func stubCursorLookPathOnlyAgent(name string) (string, error) {
	if name == "agent" {
		return "/home/zerops/.local/bin/agent", nil
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

func stubCursorLookPathOnlyCursorAgent(name string) (string, error) {
	if name == "cursor-agent" {
		return "/home/zerops/.local/bin/cursor-agent", nil
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

func stubCursorLookPathMissing(name string) (string, error) {
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

func stubCursorVersionOutput(version string) func(string, ...string) ([]byte, error) {
	return func(_ string, _ ...string) ([]byte, error) {
		return []byte(version + "\n"), nil
	}
}

func stubCursorVersionError(_ string, _ ...string) ([]byte, error) {
	return nil, errors.New("simulated probe failure")
}

func newCursorEnv(t *testing.T, home string) adapters.Env {
	t.Helper()
	return adapters.Env{
		BaseDir:       t.TempDir(),
		Home:          home,
		RT:            runtime.Info{InContainer: true, ServiceName: "zcp"},
		VSCodeWorkDir: "/var/www",
		CommandRunner: func(_ string, _ ...string) error { return nil },
		CommandOutput: stubCursorVersionOutput("2026.05.20-2b5dd59"),
		LookPath:      stubCursorLookPathBothFound,
	}
}

func TestCursor_Name(t *testing.T) {
	t.Parallel()
	if got := adapters.NewCursor().Name(); got != "cursor" {
		t.Errorf("Name() = %q, want %q (canonical ZCP_AGENT_TYPE value)", got, "cursor")
	}
}

func TestCursor_Detect_BothBinariesPresent_True(t *testing.T) {
	t.Parallel()
	env := newCursorEnv(t, t.TempDir())
	if !adapters.NewCursor().Detect(env) {
		t.Error("Detect should be true when both cursor-agent and agent are present")
	}
}

func TestCursor_Detect_OnlyCursorAgentPresent_True(t *testing.T) {
	t.Parallel()
	env := newCursorEnv(t, t.TempDir())
	env.LookPath = stubCursorLookPathOnlyCursorAgent
	if !adapters.NewCursor().Detect(env) {
		t.Error("Detect should be true with only cursor-agent (canonical name)")
	}
}

func TestCursor_Detect_OnlyAgentPresent_True(t *testing.T) {
	t.Parallel()
	env := newCursorEnv(t, t.TempDir())
	env.LookPath = stubCursorLookPathOnlyAgent
	if !adapters.NewCursor().Detect(env) {
		t.Error("Detect should be true with only agent (primary name per installer)")
	}
}

func TestCursor_Detect_BothMissing_False(t *testing.T) {
	t.Parallel()
	env := newCursorEnv(t, t.TempDir())
	env.LookPath = stubCursorLookPathMissing
	if adapters.NewCursor().Detect(env) {
		t.Error("Detect should be false when both cursor-agent and agent missing")
	}
}

func TestCursor_Validate_CurrentVersion_NoWarnings(t *testing.T) {
	t.Parallel()
	env := newCursorEnv(t, t.TempDir())
	env.CommandOutput = stubCursorVersionOutput("2026.05.20-2b5dd59")
	warnings, err := adapters.NewCursor().Validate(env)
	if err != nil {
		t.Fatalf("Validate err = %v, want nil", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Validate warnings = %v, want none", warnings)
	}
}

func TestCursor_Validate_ProbeError_SoftWarning(t *testing.T) {
	t.Parallel()
	env := newCursorEnv(t, t.TempDir())
	env.CommandOutput = stubCursorVersionError
	warnings, err := adapters.NewCursor().Validate(env)
	if err != nil {
		t.Fatalf("Validate err = %v, want nil (probe failure must be soft warning)", err)
	}
	if len(warnings) == 0 {
		t.Error("Validate should warn when version probe fails")
	}
}

func TestCursor_Validate_EmptyOutput_Warning(t *testing.T) {
	t.Parallel()
	env := newCursorEnv(t, t.TempDir())
	env.CommandOutput = stubCursorVersionOutput("")
	warnings, err := adapters.NewCursor().Validate(env)
	if err != nil {
		t.Fatalf("Validate err = %v, want nil", err)
	}
	if len(warnings) == 0 {
		t.Error("Validate should warn on empty version output")
	}
}

func TestCursor_ContainerInit_FreshHomeWritesConfig(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newCursorEnv(t, home)

	if err := adapters.NewCursor().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	config := loadCursorJSON(t, home)
	servers, _ := config["mcpServers"].(map[string]any)
	if servers == nil {
		t.Fatalf("mcp.json missing mcpServers; got %v", config)
	}
	zerops, _ := servers["zerops"].(map[string]any)
	if zerops == nil {
		t.Fatalf("mcp.json missing mcpServers.zerops; got %v", servers)
	}
	if zerops["type"] != "stdio" {
		t.Errorf("mcpServers.zerops.type = %v, want %q (required by Cursor schema)", zerops["type"], "stdio")
	}
	if zerops["command"] != "zcp" {
		t.Errorf("mcpServers.zerops.command = %v, want %q", zerops["command"], "zcp")
	}
	args, _ := zerops["args"].([]any)
	if len(args) != 1 || args[0] != "serve" {
		t.Errorf("mcpServers.zerops.args = %v, want [\"serve\"]", args)
	}
}

func TestCursor_ContainerInit_PreservesUserAddedServers(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newCursorEnv(t, home)

	initial := map[string]any{
		"mcpServers": map[string]any{
			"github":     map[string]any{"type": "stdio", "command": "github-mcp"},
			"playwright": map[string]any{"type": "stdio", "command": "npx", "args": []any{"-y", "@playwright/mcp"}},
			"remote": map[string]any{
				"url":     "https://example.com/mcp",
				"headers": map[string]any{"Authorization": "Bearer x"},
			},
		},
	}
	configPath := filepath.Join(home, ".cursor", "mcp.json")
	if err := adapters.SaveJSONFile(configPath, initial); err != nil {
		t.Fatal(err)
	}

	if err := adapters.NewCursor().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	config := loadCursorJSON(t, home)
	servers, _ := config["mcpServers"].(map[string]any)
	for _, want := range []string{"github", "playwright", "remote", "zerops"} {
		if _, ok := servers[want]; !ok {
			t.Errorf("mcpServers.%s missing after ContainerInit; got %v", want, servers)
		}
	}
	pw, _ := servers["playwright"].(map[string]any)
	pwArgs, _ := pw["args"].([]any)
	if len(pwArgs) != 2 || pwArgs[0] != "-y" || pwArgs[1] != "@playwright/mcp" {
		t.Errorf("playwright args clobbered: %v", pwArgs)
	}
	remote, _ := servers["remote"].(map[string]any)
	if remote["url"] != "https://example.com/mcp" {
		t.Errorf("remote SSE/HTTP server url lost: %v", remote)
	}
	headers, _ := remote["headers"].(map[string]any)
	if headers["Authorization"] != "Bearer x" {
		t.Errorf("remote headers lost: %v", headers)
	}
}

func TestCursor_ContainerInit_Idempotent(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newCursorEnv(t, home)

	if err := adapters.NewCursor().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	first := loadCursorJSON(t, home)
	if err := adapters.NewCursor().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	second := loadCursorJSON(t, home)

	if !reflect.DeepEqual(first, second) {
		t.Errorf("ContainerInit not idempotent:\n  first:  %v\n  second: %v", first, second)
	}
}

func TestCursor_ContainerInit_EmptyHomeReturnsError(t *testing.T) {
	t.Parallel()
	env := newCursorEnv(t, t.TempDir())
	env.Home = ""
	if err := adapters.NewCursor().ContainerInit(env); err == nil {
		t.Error("ContainerInit should error when Env.Home is empty")
	}
}

// TestCursor_MCPEntry_NoEnvField pins the same contract as
// TestGemini_MCPEntry_NoEnvField / TestAntigravity_MCPEntry_NoEnvField:
// Cursor's MCP spawn inherits parent env (verified empirically — `agent
// mcp list-tools zerops` enumerated all tools without env enumeration).
// Writing an `env` field here would risk hard-coding stale
// runtime-detection values at init-time. Codex's restrictive `env_vars`
// model is the outlier; Cursor / Gemini / Antigravity all spread
// parent env.
func TestCursor_MCPEntry_NoEnvField(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newCursorEnv(t, home)
	if err := adapters.NewCursor().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	config := loadCursorJSON(t, home)
	zerops := config["mcpServers"].(map[string]any)["zerops"].(map[string]any)

	if _, has := zerops["env"]; has {
		t.Errorf("mcpServers.zerops.env present (%v); Cursor uses permissive parent-env spread", zerops["env"])
	}
	if _, has := zerops["envFile"]; has {
		t.Errorf("mcpServers.zerops.envFile present (%v); ZCP doesn't manage envFile (user-owned secret)", zerops["envFile"])
	}
	if _, has := zerops["env_vars"]; has {
		t.Errorf("mcpServers.zerops.env_vars present (%v); env_vars is Codex-specific", zerops["env_vars"])
	}
}

// TestCursor_MCPEntry_TypeStdioRequired pins the `type=stdio` field —
// Cursor's schema requires explicit transport type to distinguish from
// SSE / Streamable HTTP. Omitting type would either default to remote
// (silently broken) or trigger a schema error at agent startup,
// depending on Cursor version. Pin guards against well-meaning
// "minimization" that would break the adapter.
func TestCursor_MCPEntry_TypeStdioRequired(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newCursorEnv(t, home)
	if err := adapters.NewCursor().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	config := loadCursorJSON(t, home)
	zerops := config["mcpServers"].(map[string]any)["zerops"].(map[string]any)
	if zerops["type"] != "stdio" {
		t.Errorf("type = %v, want \"stdio\" (Cursor distinguishes stdio from SSE/HTTP transports)", zerops["type"])
	}
}

func loadCursorJSON(t *testing.T, home string) map[string]any {
	t.Helper()
	configPath := filepath.Join(home, ".cursor", "mcp.json")
	data, err := adapters.LoadJSONFile(configPath)
	if err != nil {
		t.Fatalf("load %s: %v", configPath, err)
	}
	return data
}
