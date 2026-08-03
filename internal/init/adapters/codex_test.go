package adapters_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/zeropsio/zcp/internal/init/adapters"
	"github.com/zeropsio/zcp/internal/runtime"
)

// Stub helpers for Env hook injection — tests substitute these to
// simulate codex-installed / codex-missing / version-old states
// without touching the real filesystem PATH.

func stubLookPathFound(_ string) (string, error) { return "/usr/local/bin/codex", nil }
func stubLookPathMissing(_ string) (string, error) {
	return "", &exec.Error{Name: "codex", Err: exec.ErrNotFound}
}

func stubVersionOutput(version string) func(string, ...string) ([]byte, error) {
	return func(_ string, _ ...string) ([]byte, error) {
		return []byte("codex " + version + "\n"), nil
	}
}

func stubVersionError(_ string, _ ...string) ([]byte, error) {
	return nil, errors.New("simulated probe failure")
}

func newCodexEnv(t *testing.T, home string) adapters.Env {
	t.Helper()
	return adapters.Env{
		BaseDir:       t.TempDir(),
		Home:          home,
		RT:            runtime.Info{InContainer: true, ServiceName: "zcp"},
		VSCodeWorkDir: "/var/www",
		CommandRunner: func(_ string, _ ...string) error { return nil },
		CommandOutput: stubVersionOutput("0.133.0"),
		LookPath:      stubLookPathFound,
	}
}

func TestCodex_Name(t *testing.T) {
	t.Parallel()
	if got := adapters.NewCodex().Name(); got != "codex" {
		t.Errorf("Name() = %q, want %q (canonical ZCP_AGENT_TYPE value)", got, "codex")
	}
}

func TestCodex_Detect_BinaryPresent_True(t *testing.T) {
	t.Parallel()
	env := newCodexEnv(t, t.TempDir())
	if !adapters.NewCodex().Detect(env) {
		t.Error("Detect should be true when codex binary on PATH")
	}
}

func TestCodex_Detect_BinaryMissing_False(t *testing.T) {
	t.Parallel()
	env := newCodexEnv(t, t.TempDir())
	env.LookPath = stubLookPathMissing
	if adapters.NewCodex().Detect(env) {
		t.Error("Detect should be false when codex binary missing (existing Claude-only containers must skip)")
	}
}

func TestCodex_Validate_CurrentVersion_NoWarnings(t *testing.T) {
	t.Parallel()
	env := newCodexEnv(t, t.TempDir())
	env.CommandOutput = stubVersionOutput("0.133.0")
	warnings, err := adapters.NewCodex().Validate(env)
	if err != nil {
		t.Fatalf("Validate err = %v, want nil", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Validate warnings = %v, want none for current version", warnings)
	}
}

func TestCodex_Validate_OldVersion_HooksWarning(t *testing.T) {
	t.Parallel()
	env := newCodexEnv(t, t.TempDir())
	env.CommandOutput = stubVersionOutput("0.120.0")
	warnings, err := adapters.NewCodex().Validate(env)
	if err != nil {
		t.Fatalf("Validate err = %v, want nil", err)
	}
	if len(warnings) == 0 {
		t.Error("Validate should warn on Codex version below hooks-stable threshold")
	}
}

func TestCodex_Validate_ProbeError_SoftWarning(t *testing.T) {
	t.Parallel()
	env := newCodexEnv(t, t.TempDir())
	env.CommandOutput = stubVersionError
	warnings, err := adapters.NewCodex().Validate(env)
	if err != nil {
		t.Fatalf("Validate err = %v, want nil (probe failure must be soft warning, not hard fail)", err)
	}
	if len(warnings) == 0 {
		t.Error("Validate should warn (not error) when version probe fails")
	}
}

func TestCodex_ContainerInit_FreshHomeWritesConfig(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newCodexEnv(t, home)

	if err := adapters.NewCodex().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	config := loadCodexTOML(t, home)

	servers, _ := config["mcp_servers"].(map[string]any)
	if servers == nil {
		t.Fatalf("config.toml missing [mcp_servers]; got %v", config)
	}
	zerops, _ := servers["zerops"].(map[string]any)
	if zerops == nil {
		t.Fatalf("config.toml missing [mcp_servers.zerops]; got %v", servers)
	}
	if zerops["command"] != "zcp" {
		t.Errorf("mcp_servers.zerops.command = %v, want %q", zerops["command"], "zcp")
	}
	args, _ := zerops["args"].([]any)
	if len(args) != 1 || args[0] != "serve" {
		t.Errorf("mcp_servers.zerops.args = %v, want [\"serve\"]", args)
	}

	projects, _ := config["projects"].(map[string]any)
	if projects == nil {
		t.Fatalf("config.toml missing [projects]")
	}
	work, _ := projects["/var/www"].(map[string]any)
	if work == nil {
		t.Fatalf("projects[/var/www] missing; got %v", projects)
	}
	if work["trust_level"] != "trusted" {
		t.Errorf("projects[/var/www].trust_level = %v, want %q", work["trust_level"], "trusted")
	}
}

func TestCodex_ContainerInit_PreservesUserAddedServers(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newCodexEnv(t, home)

	// User-edited ~/.codex/config.toml with other MCP servers + a
	// trusted project ZCP doesn't know about.
	initial := map[string]any{
		"model": "gpt-5.5",
		"mcp_servers": map[string]any{
			"github": map[string]any{"command": "github-mcp"},
			"jira":   map[string]any{"command": "jira-mcp", "env": map[string]any{"JIRA_TOKEN": "abc"}},
		},
		"projects": map[string]any{
			"/Users/me/other-project": map[string]any{"trust_level": "trusted"},
		},
	}
	configPath := filepath.Join(home, ".codex", "config.toml")
	if err := adapters.SaveTOMLFile(configPath, initial); err != nil {
		t.Fatal(err)
	}

	if err := adapters.NewCodex().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	config := loadCodexTOML(t, home)
	if config["model"] != "gpt-5.5" {
		t.Errorf("top-level user setting lost: model = %v", config["model"])
	}
	servers, _ := config["mcp_servers"].(map[string]any)
	for _, want := range []string{"github", "jira", "zerops"} {
		if _, ok := servers[want]; !ok {
			t.Errorf("mcp_servers.%s missing after ContainerInit; got %v", want, servers)
		}
	}
	jira, _ := servers["jira"].(map[string]any)
	if jira["command"] != "jira-mcp" {
		t.Errorf("user's jira server config clobbered: %v", jira)
	}
	jiraEnv, _ := jira["env"].(map[string]any)
	if jiraEnv["JIRA_TOKEN"] != "abc" {
		t.Errorf("user's nested jira env lost: %v", jiraEnv)
	}
	projects, _ := config["projects"].(map[string]any)
	if _, ok := projects["/Users/me/other-project"]; !ok {
		t.Error("user's other-project trust entry lost")
	}
	if _, ok := projects["/var/www"]; !ok {
		t.Error("ZCP project trust entry not added")
	}
}

func TestCodex_ContainerInit_Idempotent(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newCodexEnv(t, home)

	if err := adapters.NewCodex().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	first := loadCodexTOML(t, home)
	if err := adapters.NewCodex().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	second := loadCodexTOML(t, home)

	if !reflect.DeepEqual(first, second) {
		t.Errorf("ContainerInit not idempotent:\n  first:  %v\n  second: %v", first, second)
	}
}

func TestCodex_ContainerInit_EmptyHomeReturnsError(t *testing.T) {
	t.Parallel()
	env := newCodexEnv(t, t.TempDir())
	env.Home = ""
	if err := adapters.NewCodex().ContainerInit(env); err == nil {
		t.Error("ContainerInit should error when Env.Home is empty")
	}
}

// TestCodex_MCPEntry_UsesEnvVarsNotEnv pins the Codex CLI contract:
// `env_vars = ["NAME"]` is the documented mechanism for forwarding a
// named environment variable from Codex's calling shell to the MCP
// subprocess. `env = {KEY = "${VAR}"}` would pass the LITERAL string
// "${VAR}" (Codex does not expand placeholders), and ZCP MCP would
// receive a bogus token and fail auth. This regression was caught by
// Codex review of the initial implementation.
//
// env_vars MUST also include the Zerops runtime detection variables
// (serviceId / hostname / projectId) — without them, runtime.Detect()
// returns InContainer=false and the workflow ships local-mode atoms
// to a session that's actually inside a Zerops container. Observed
// bug 2026-05-24: Codex skipped the appdev runtime and rsynced the
// recipe code into /var/www as if it were a local dev checkout.
//
// `zeropsSubdomain` is required for the same reason one level up:
// auth.containerAPIDefaults() derives the API host + region from it, so
// a stripped var silently drops a devel-instance (.zerops.dev) session
// back onto the production API host.
func TestCodex_MCPEntry_UsesEnvVarsNotEnv(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newCodexEnv(t, home)
	if err := adapters.NewCodex().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	config := loadCodexTOML(t, home)
	zerops := config["mcp_servers"].(map[string]any)["zerops"].(map[string]any)

	envVars, ok := zerops["env_vars"].([]any)
	if !ok {
		t.Fatalf("[mcp_servers.zerops].env_vars missing or wrong shape; got %v (type %T)", zerops["env_vars"], zerops["env_vars"])
	}
	present := make(map[string]bool, len(envVars))
	for _, v := range envVars {
		if s, ok := v.(string); ok {
			present[s] = true
		}
	}

	// Required vars: API auth + runtime detection (container/local) +
	// subprocess basics. Missing any of these has observed regressions.
	required := []string{
		"ZCP_API_KEY",
		"serviceId",
		"hostname",
		"projectId",
		"zeropsSubdomain",
		"PATH",
		"HOME",
	}
	for _, name := range required {
		if !present[name] {
			t.Errorf("env_vars must include %q (got %v) — required so zcp serve subprocess can detect Zerops container env / authenticate / locate child binaries",
				name, envVars)
		}
	}

	// env MUST NOT contain a literal "${ZCP_API_KEY}" placeholder
	// (the original bug Codex review caught — Codex doesn't expand
	// placeholders in `env` values).
	if envMap, ok := zerops["env"].(map[string]any); ok {
		if v, present := envMap["ZCP_API_KEY"]; present {
			if s, _ := v.(string); strings.Contains(s, "${") {
				t.Errorf("env.ZCP_API_KEY contains unexpanded placeholder %q — use env_vars for shell pass-through", s)
			}
		}
	}
}

// TestCodex_ContainerInit_ByteStableAfterReloadResave pins
// byte-stability through the full load → upsert → save cycle (not
// just decoded-map equality). BurntSushi/toml emits keys in stable
// sorted order; this test catches any future regression where the
// encoder loses determinism.
func TestCodex_ContainerInit_ByteStableAfterReloadResave(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newCodexEnv(t, home)

	if err := adapters.NewCodex().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))

	// Load + save cycle without ContainerInit (pure encoder round-trip).
	data, err := adapters.LoadTOMLFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := adapters.SaveTOMLFile(filepath.Join(home, ".codex", "config.toml"), data); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))

	if string(first) != string(second) {
		t.Errorf("TOML not byte-stable through load+save round-trip:\n  first:  %s\n  second: %s", first, second)
	}

	// Now re-run ContainerInit — should also be byte-stable.
	if err := adapters.NewCodex().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	third, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if string(first) != string(third) {
		t.Errorf("ContainerInit not byte-stable across reruns:\n  first:  %s\n  third:  %s", first, third)
	}
}

func loadCodexTOML(t *testing.T, home string) map[string]any {
	t.Helper()
	configPath := filepath.Join(home, ".codex", "config.toml")
	var config map[string]any
	if _, err := toml.DecodeFile(configPath, &config); err != nil {
		t.Fatalf("decode %s: %v", configPath, err)
	}
	return config
}
