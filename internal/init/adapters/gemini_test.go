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

func stubGeminiLookPathFound(_ string) (string, error) { return "/usr/bin/gemini", nil }
func stubGeminiLookPathMissing(_ string) (string, error) {
	return "", &exec.Error{Name: "gemini", Err: exec.ErrNotFound}
}

func stubGeminiVersionOutput(version string) func(string, ...string) ([]byte, error) {
	return func(_ string, _ ...string) ([]byte, error) {
		return []byte(version + "\n"), nil
	}
}

func stubGeminiVersionError(_ string, _ ...string) ([]byte, error) {
	return nil, errors.New("simulated probe failure")
}

func newGeminiEnv(t *testing.T, home string) adapters.Env {
	t.Helper()
	return adapters.Env{
		BaseDir:       t.TempDir(),
		Home:          home,
		RT:            runtime.Info{InContainer: true, ServiceName: "zcp"},
		VSCodeWorkDir: "/var/www",
		CommandRunner: func(_ string, _ ...string) error { return nil },
		CommandOutput: stubGeminiVersionOutput("0.39.1"),
		LookPath:      stubGeminiLookPathFound,
	}
}

func TestGemini_Name(t *testing.T) {
	t.Parallel()
	if got := adapters.NewGemini().Name(); got != "gemini" {
		t.Errorf("Name() = %q, want %q (canonical ZCP_AGENT_TYPE value)", got, "gemini")
	}
}

func TestGemini_Detect_BinaryPresent_True(t *testing.T) {
	t.Parallel()
	env := newGeminiEnv(t, t.TempDir())
	if !adapters.NewGemini().Detect(env) {
		t.Error("Detect should be true when gemini binary on PATH")
	}
}

func TestGemini_Detect_BinaryMissing_False(t *testing.T) {
	t.Parallel()
	env := newGeminiEnv(t, t.TempDir())
	env.LookPath = stubGeminiLookPathMissing
	if adapters.NewGemini().Detect(env) {
		t.Error("Detect should be false when gemini binary missing")
	}
}

func TestGemini_Validate_CurrentVersion_NoWarnings(t *testing.T) {
	t.Parallel()
	env := newGeminiEnv(t, t.TempDir())
	env.CommandOutput = stubGeminiVersionOutput("0.39.1")
	warnings, err := adapters.NewGemini().Validate(env)
	if err != nil {
		t.Fatalf("Validate err = %v, want nil", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Validate warnings = %v, want none", warnings)
	}
}

func TestGemini_Validate_ProbeError_SoftWarning(t *testing.T) {
	t.Parallel()
	env := newGeminiEnv(t, t.TempDir())
	env.CommandOutput = stubGeminiVersionError
	warnings, err := adapters.NewGemini().Validate(env)
	if err != nil {
		t.Fatalf("Validate err = %v, want nil (probe failure must be soft warning, not hard fail)", err)
	}
	if len(warnings) == 0 {
		t.Error("Validate should warn (not error) when version probe fails")
	}
}

func TestGemini_Validate_EmptyOutput_Warning(t *testing.T) {
	t.Parallel()
	env := newGeminiEnv(t, t.TempDir())
	env.CommandOutput = stubGeminiVersionOutput("")
	warnings, err := adapters.NewGemini().Validate(env)
	if err != nil {
		t.Fatalf("Validate err = %v, want nil", err)
	}
	if len(warnings) == 0 {
		t.Error("Validate should warn when version probe returns empty output")
	}
}

func TestGemini_ContainerInit_FreshHomeWritesConfig(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newGeminiEnv(t, home)

	if err := adapters.NewGemini().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	config := loadGeminiJSON(t, home)
	servers, _ := config["mcpServers"].(map[string]any)
	if servers == nil {
		t.Fatalf("settings.json missing mcpServers; got %v", config)
	}
	zerops, _ := servers["zerops"].(map[string]any)
	if zerops == nil {
		t.Fatalf("settings.json missing mcpServers.zerops; got %v", servers)
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
}

func TestGemini_ContainerInit_PreservesUserAddedServers(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newGeminiEnv(t, home)

	initial := map[string]any{
		"theme": "dark",
		"mcpServers": map[string]any{
			"github": map[string]any{"command": "github-mcp"},
			"jira":   map[string]any{"command": "jira-mcp", "env": map[string]any{"JIRA_TOKEN": "abc"}},
		},
		"includeDirectories": []any{"/Users/me/docs"},
	}
	configPath := filepath.Join(home, ".gemini", "settings.json")
	if err := adapters.SaveJSONFile(configPath, initial); err != nil {
		t.Fatal(err)
	}

	if err := adapters.NewGemini().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	config := loadGeminiJSON(t, home)
	if config["theme"] != "dark" {
		t.Errorf("top-level user setting lost: theme = %v", config["theme"])
	}
	includes, _ := config["includeDirectories"].([]any)
	if len(includes) != 1 || includes[0] != "/Users/me/docs" {
		t.Errorf("includeDirectories lost: %v", includes)
	}
	servers, _ := config["mcpServers"].(map[string]any)
	for _, want := range []string{"github", "jira", "zerops"} {
		if _, ok := servers[want]; !ok {
			t.Errorf("mcpServers.%s missing after ContainerInit; got %v", want, servers)
		}
	}
	jira, _ := servers["jira"].(map[string]any)
	jiraEnv, _ := jira["env"].(map[string]any)
	if jiraEnv["JIRA_TOKEN"] != "abc" {
		t.Errorf("user's nested jira env lost: %v", jiraEnv)
	}
}

func TestGemini_ContainerInit_Idempotent(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newGeminiEnv(t, home)

	if err := adapters.NewGemini().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	first := loadGeminiJSON(t, home)
	if err := adapters.NewGemini().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	second := loadGeminiJSON(t, home)

	if !reflect.DeepEqual(first, second) {
		t.Errorf("ContainerInit not idempotent:\n  first:  %v\n  second: %v", first, second)
	}
}

func TestGemini_ContainerInit_EmptyHomeReturnsError(t *testing.T) {
	t.Parallel()
	env := newGeminiEnv(t, t.TempDir())
	env.Home = ""
	if err := adapters.NewGemini().ContainerInit(env); err == nil {
		t.Error("ContainerInit should error when Env.Home is empty")
	}
}

// TestGemini_MCPEntry_NoEnvField pins the Gemini/Antigravity schema
// contract: the adapter does NOT write an `env` field. Gemini's MCP
// subprocess spawn uses `env: { ...process.env, ...envMap }` —
// permissive parent-env spread. Adding an empty `env: {}` would still
// work but adds clutter; adding a populated `env` here would risk
// hard-coding stale runtime-detection values at init-time. Codex needs
// env_vars (its env-passthrough is restrictive); Gemini does not.
func TestGemini_MCPEntry_NoEnvField(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newGeminiEnv(t, home)
	if err := adapters.NewGemini().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	config := loadGeminiJSON(t, home)
	zerops := config["mcpServers"].(map[string]any)["zerops"].(map[string]any)

	if _, has := zerops["env"]; has {
		t.Errorf("mcpServers.zerops.env present (%v); Gemini uses permissive parent-env spread — no enumeration needed", zerops["env"])
	}
	if _, has := zerops["env_vars"]; has {
		t.Errorf("mcpServers.zerops.env_vars present (%v); env_vars is a Codex-specific field — Gemini ignores it silently", zerops["env_vars"])
	}
}

func loadGeminiJSON(t *testing.T, home string) map[string]any {
	t.Helper()
	configPath := filepath.Join(home, ".gemini", "settings.json")
	data, err := adapters.LoadJSONFile(configPath)
	if err != nil {
		t.Fatalf("load %s: %v", configPath, err)
	}
	return data
}
