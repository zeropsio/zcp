package adapters_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zeropsio/zcp/internal/init/adapters"
	"github.com/zeropsio/zcp/internal/runtime"
)

func stubGrokLookPathFound(_ string) (string, error) { return "/home/zerops/.local/bin/grok", nil }
func stubGrokLookPathMissing(_ string) (string, error) {
	return "", &exec.Error{Name: "grok", Err: exec.ErrNotFound}
}

func stubGrokVersionOutput(_ string, _ ...string) ([]byte, error) {
	return []byte("grok-cli 1.1.7\n"), nil
}

func stubGrokVersionError(_ string, _ ...string) ([]byte, error) {
	return nil, errors.New("simulated probe failure")
}

// newGrokEnv builds an Env with the Zerops runtime-detection fields populated
// (RT) so ContainerInit can bake serviceId / hostname / projectId into the
// MCP server's literal env block — grok performs NO ${} interpolation and the
// MCP SDK strips any var not in its small default-inherited set.
func newGrokEnv(t *testing.T, home string) adapters.Env {
	t.Helper()
	return adapters.Env{
		BaseDir: t.TempDir(),
		Home:    home,
		RT: runtime.Info{
			InContainer: true,
			ServiceName: "appdev",
			ServiceID:   "svc-123",
			ProjectID:   "proj-456",
		},
		VSCodeWorkDir: "/var/www",
		CommandRunner: func(_ string, _ ...string) error { return nil },
		CommandOutput: stubGrokVersionOutput,
		LookPath:      stubGrokLookPathFound,
	}
}

func TestGrok_Name(t *testing.T) {
	t.Parallel()
	if got := adapters.NewGrok().Name(); got != "grok" {
		t.Errorf("Name() = %q, want %q (canonical ZCP_AGENT_TYPE value)", got, "grok")
	}
}

func TestGrok_Detect_BinaryPresent_True(t *testing.T) {
	t.Parallel()
	env := newGrokEnv(t, t.TempDir())
	if !adapters.NewGrok().Detect(env) {
		t.Error("Detect should be true when grok binary on PATH")
	}
}

func TestGrok_Detect_BinaryMissing_False(t *testing.T) {
	t.Parallel()
	env := newGrokEnv(t, t.TempDir())
	env.LookPath = stubGrokLookPathMissing
	if adapters.NewGrok().Detect(env) {
		t.Error("Detect should be false when grok binary missing (Claude-only containers must skip)")
	}
}

func TestGrok_Validate_Ok_NoWarnings(t *testing.T) {
	t.Parallel()
	env := newGrokEnv(t, t.TempDir())
	warnings, err := adapters.NewGrok().Validate(env)
	if err != nil {
		t.Fatalf("Validate err = %v, want nil", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Validate warnings = %v, want none for a working version probe", warnings)
	}
}

func TestGrok_Validate_ProbeError_SoftWarning(t *testing.T) {
	t.Parallel()
	env := newGrokEnv(t, t.TempDir())
	env.CommandOutput = stubGrokVersionError
	warnings, err := adapters.NewGrok().Validate(env)
	if err != nil {
		t.Fatalf("Validate err = %v, want nil (probe failure must be soft warning, not hard fail)", err)
	}
	if len(warnings) == 0 {
		t.Error("Validate should warn (not error) when version probe fails")
	}
}

func TestGrok_ContainerInit_FreshHomeWritesServerEntry(t *testing.T) {
	// Not parallel — sets ZCP_API_KEY so the literal-env assertion is deterministic.
	home := t.TempDir()
	env := newGrokEnv(t, home)
	t.Setenv("ZCP_API_KEY", "secret-key-value")

	if err := adapters.NewGrok().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	zerops := requireGrokZeropsServer(t, home)
	if zerops["id"] != "zerops" {
		t.Errorf("server.id = %v, want %q", zerops["id"], "zerops")
	}
	if zerops["label"] != "zerops" {
		t.Errorf("server.label = %v, want %q", zerops["label"], "zerops")
	}
	if zerops["enabled"] != true {
		t.Errorf("server.enabled = %v, want true", zerops["enabled"])
	}
	if zerops["transport"] != "stdio" {
		t.Errorf("server.transport = %v, want %q", zerops["transport"], "stdio")
	}
	if zerops["command"] != "zcp" {
		t.Errorf("server.command = %v, want %q", zerops["command"], "zcp")
	}
	args, _ := zerops["args"].([]any)
	if len(args) != 1 || args[0] != "serve" {
		t.Errorf("server.args = %v, want [\"serve\"]", args)
	}
}

// TestGrok_MCPEntry_BakesLiteralRuntimeEnv pins the grok contract: grok passes
// server.env to the spawned MCP subprocess VERBATIM (no ${} expansion) and the
// MCP SDK only auto-inherits HOME/PATH/USER/SHELL/TERM/LOGNAME — so the Zerops
// runtime-detection vars (serviceId/hostname/projectId) and ZCP_API_KEY must be
// written as RESOLVED LITERAL VALUES. Missing them reproduces the Codex/Cursor
// bug class: zcp serve sees serviceId="" → InContainer=false → local-mode atoms
// shipped into a container session, plus failed API auth.
func TestGrok_MCPEntry_BakesLiteralRuntimeEnv(t *testing.T) {
	// Not parallel — sets ZCP_API_KEY.
	home := t.TempDir()
	env := newGrokEnv(t, home)
	t.Setenv("ZCP_API_KEY", "secret-key-value")

	if err := adapters.NewGrok().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}
	zerops := requireGrokZeropsServer(t, home)
	serverEnv, ok := zerops["env"].(map[string]any)
	if !ok {
		t.Fatalf("server.env missing or wrong shape; got %v (type %T)", zerops["env"], zerops["env"])
	}
	want := map[string]string{
		"serviceId":   "svc-123",
		"hostname":    "appdev",
		"projectId":   "proj-456",
		"ZCP_API_KEY": "secret-key-value",
	}
	for k, v := range want {
		if serverEnv[k] != v {
			t.Errorf("server.env[%q] = %v, want %q (literal value — grok does not interpolate)", k, serverEnv[k], v)
		}
	}
	// PATH/HOME are auto-inherited by the MCP SDK's getDefaultEnvironment — must
	// NOT be baked (baking init-time PATH/HOME would shadow the live values).
	for _, k := range []string{"PATH", "HOME"} {
		if _, present := serverEnv[k]; present {
			t.Errorf("server.env should not bake %q (MCP SDK inherits it live)", k)
		}
	}
}

// TestGrok_MCPEntry_OmitsAPIKeyWhenUnset locks that ZCP_API_KEY is omitted from
// server.env when the env var is unset — no phantom empty-string credential.
func TestGrok_MCPEntry_OmitsAPIKeyWhenUnset(t *testing.T) {
	// Not parallel — clears ZCP_API_KEY.
	home := t.TempDir()
	env := newGrokEnv(t, home)
	t.Setenv("ZCP_API_KEY", "")

	if err := adapters.NewGrok().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}
	zerops := requireGrokZeropsServer(t, home)
	serverEnv, _ := zerops["env"].(map[string]any)
	if _, present := serverEnv["ZCP_API_KEY"]; present {
		t.Errorf("server.env should omit ZCP_API_KEY when unset; got %v", serverEnv)
	}
}

func TestGrok_ContainerInit_PreservesUserSettingsAndServers(t *testing.T) {
	// Not parallel — sets ZCP_API_KEY.
	home := t.TempDir()
	env := newGrokEnv(t, home)
	t.Setenv("ZCP_API_KEY", "secret-key-value")

	// User-edited ~/.grok/user-settings.json: top-level settings + an existing
	// MCP server array entry ZCP doesn't know about.
	initial := map[string]any{
		"apiKey":       "xai-user-key",
		"defaultModel": "grok-4",
		"mcp": map[string]any{
			"servers": []any{
				map[string]any{
					"id":        "github",
					"label":     "github",
					"enabled":   true,
					"transport": "stdio",
					"command":   "github-mcp",
					"args":      []any{},
				},
			},
		},
	}
	configPath := filepath.Join(home, ".grok", "user-settings.json")
	if err := adapters.SaveJSONFile(configPath, initial); err != nil {
		t.Fatal(err)
	}

	if err := adapters.NewGrok().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	settings := loadGrokSettings(t, home)
	if settings["apiKey"] != "xai-user-key" {
		t.Errorf("top-level user setting lost: apiKey = %v", settings["apiKey"])
	}
	if settings["defaultModel"] != "grok-4" {
		t.Errorf("top-level user setting lost: defaultModel = %v", settings["defaultModel"])
	}
	servers := grokServers(t, settings)
	ids := serverIDs(servers)
	for _, want := range []string{"github", "zerops"} {
		if !ids[want] {
			t.Errorf("mcp.servers missing %q after ContainerInit; got ids %v", want, ids)
		}
	}
	// User's github entry survives byte-for-byte.
	for _, s := range servers {
		m := s.(map[string]any)
		if m["id"] == "github" && m["command"] != "github-mcp" {
			t.Errorf("user's github server config clobbered: %v", m)
		}
	}
}

func TestGrok_ContainerInit_Idempotent(t *testing.T) {
	// Not parallel — sets ZCP_API_KEY.
	home := t.TempDir()
	env := newGrokEnv(t, home)
	t.Setenv("ZCP_API_KEY", "secret-key-value")

	if err := adapters.NewGrok().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	first := loadGrokSettings(t, home)
	if err := adapters.NewGrok().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	second := loadGrokSettings(t, home)

	if !reflect.DeepEqual(first, second) {
		t.Errorf("ContainerInit not idempotent:\n  first:  %v\n  second: %v", first, second)
	}

	// Re-running must not duplicate the zerops entry in the servers array.
	count := 0
	for _, s := range grokServers(t, second) {
		if m, ok := s.(map[string]any); ok && m["id"] == "zerops" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("zerops server appears %d times after re-init, want exactly 1", count)
	}
}

func TestGrok_ContainerInit_EmptyHomeReturnsError(t *testing.T) {
	t.Parallel()
	env := newGrokEnv(t, t.TempDir())
	env.Home = ""
	if err := adapters.NewGrok().ContainerInit(env); err == nil {
		t.Error("ContainerInit should error when Env.Home is empty")
	}
}

// TestGrok_ContainerInit_ByteStableAcrossReruns pins byte-stability through the
// full load → upsert → save cycle, the precondition for idempotent re-init.
func TestGrok_ContainerInit_ByteStableAcrossReruns(t *testing.T) {
	// Not parallel — sets ZCP_API_KEY.
	home := t.TempDir()
	env := newGrokEnv(t, home)
	t.Setenv("ZCP_API_KEY", "secret-key-value")

	if err := adapters.NewGrok().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".grok", "user-settings.json")
	first, _ := os.ReadFile(path)

	if err := adapters.NewGrok().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)

	if string(first) != string(second) {
		t.Errorf("user-settings.json not byte-stable across reruns:\n  first:  %s\n  second: %s", first, second)
	}
}

// --- helpers ---

func loadGrokSettings(t *testing.T, home string) map[string]any {
	t.Helper()
	path := filepath.Join(home, ".grok", "user-settings.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return settings
}

func grokServers(t *testing.T, settings map[string]any) []any {
	t.Helper()
	mcp, ok := settings["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("user-settings.json missing mcp object; got %v", settings)
	}
	servers, ok := mcp["servers"].([]any)
	if !ok {
		t.Fatalf("user-settings.json missing mcp.servers array; got %v", mcp)
	}
	return servers
}

func serverIDs(servers []any) map[string]bool {
	ids := make(map[string]bool, len(servers))
	for _, s := range servers {
		if m, ok := s.(map[string]any); ok {
			if id, ok := m["id"].(string); ok {
				ids[id] = true
			}
		}
	}
	return ids
}

func requireGrokZeropsServer(t *testing.T, home string) map[string]any {
	t.Helper()
	for _, s := range grokServers(t, loadGrokSettings(t, home)) {
		if m, ok := s.(map[string]any); ok && m["id"] == "zerops" {
			return m
		}
	}
	t.Fatalf("mcp.servers has no entry with id=zerops")
	return nil
}
