package adapters_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/init/adapters"
	"github.com/zeropsio/zcp/internal/runtime"
)

func stubGrokLookPathFound(_ string) (string, error) { return "/home/zerops/.local/bin/grok", nil }
func stubGrokLookPathMissing(_ string) (string, error) {
	return "", &exec.Error{Name: "grok", Err: exec.ErrNotFound}
}

func stubGrokVersionOutput(_ string, _ ...string) ([]byte, error) {
	return []byte("grok 0.2.73 (9ff14c43bb)\n"), nil
}

func stubGrokVersionError(_ string, _ ...string) ([]byte, error) {
	return nil, errors.New("simulated probe failure")
}

// newGrokEnv builds an Env for grok's container init. grok inherits its own
// process env into the MCP subprocess, so ContainerInit writes NO env block —
// RT is populated only to mirror a real container Env, not because grok reads it.
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

// TestGrok_ContainerInit_FreshHomeWritesServerEntry pins the config.toml shape:
// [mcp_servers.zerops] with command="zcp", args=["serve"], enabled=true —
// exactly what `grok mcp add zerops zcp serve` produces (stdio is grok's default
// transport, so no transport field). No superagent-format id/label fields.
func TestGrok_ContainerInit_FreshHomeWritesServerEntry(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newGrokEnv(t, home)

	if err := adapters.NewGrok().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	zerops := requireGrokZeropsServer(t, home)
	if zerops["command"] != "zcp" {
		t.Errorf("command = %v, want %q", zerops["command"], "zcp")
	}
	args, _ := zerops["args"].([]any)
	if len(args) != 1 || args[0] != "serve" {
		t.Errorf("args = %v, want [\"serve\"]", args)
	}
	if zerops["enabled"] != true {
		t.Errorf("enabled = %v, want true", zerops["enabled"])
	}
}

// TestGrok_ContainerInit_NoEnvBlock_NeverBakesSecret pins the load-bearing
// contract: grok inherits its process env into the MCP subprocess, so the entry
// carries NO env block at all — not ZCP_API_KEY (would leak the plaintext secret
// to disk) and not the runtime ids (grok forwards them live). Live-verified via
// `grok mcp doctor`: a no-env entry → 22 tools discovered.
func TestGrok_ContainerInit_NoEnvBlock_NeverBakesSecret(t *testing.T) {
	// Not parallel — sets ZCP_API_KEY to assert it is NOT written.
	home := t.TempDir()
	env := newGrokEnv(t, home)
	t.Setenv("ZCP_API_KEY", "super-secret-should-never-be-in-file")

	if err := adapters.NewGrok().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}
	zerops := requireGrokZeropsServer(t, home)
	if _, present := zerops["env"]; present {
		t.Errorf("entry must have NO env block (grok inherits env); got env = %v", zerops["env"])
	}

	raw := rawGrokConfig(t, home)
	if strings.Contains(raw, "super-secret-should-never-be-in-file") {
		t.Errorf("ZCP_API_KEY secret value leaked into config.toml:\n%s", raw)
	}
	for _, id := range []string{"svc-123", "proj-456"} {
		if strings.Contains(raw, id) {
			t.Errorf("runtime id %q baked into config.toml (grok forwards it live, don't write it):\n%s", id, raw)
		}
	}
}

// TestGrok_ContainerInit_PreservesConfig pins merge-awareness: the [cli] section
// and a pre-existing user [mcp_servers.github] table survive the zerops upsert.
func TestGrok_ContainerInit_PreservesConfig(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newGrokEnv(t, home)

	configPath := filepath.Join(home, ".grok", "config.toml")
	initial := map[string]any{
		"cli": map[string]any{"installer": "internal"},
		"mcp_servers": map[string]any{
			"github": map[string]any{"command": "github-mcp", "args": []any{}, "enabled": true},
		},
	}
	if err := adapters.SaveTOMLFile(configPath, initial); err != nil {
		t.Fatal(err)
	}

	if err := adapters.NewGrok().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	data := loadGrokConfig(t, home)
	cli, _ := data["cli"].(map[string]any)
	if cli["installer"] != "internal" {
		t.Errorf("[cli] section lost: %v", data["cli"])
	}
	servers, _ := data["mcp_servers"].(map[string]any)
	if servers["zerops"] == nil {
		t.Errorf("zerops not added; got %v", servers)
	}
	github, _ := servers["github"].(map[string]any)
	if github["command"] != "github-mcp" {
		t.Errorf("user's github server clobbered: %v", servers["github"])
	}
}

// TestGrok_ContainerInit_Idempotent pins that re-running yields byte-identical
// config.toml with exactly one zerops table.
func TestGrok_ContainerInit_Idempotent(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newGrokEnv(t, home)

	if err := adapters.NewGrok().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	first := loadGrokConfig(t, home)
	firstRaw := rawGrokConfig(t, home)
	if err := adapters.NewGrok().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	second := loadGrokConfig(t, home)
	secondRaw := rawGrokConfig(t, home)

	if !reflect.DeepEqual(first, second) {
		t.Errorf("ContainerInit not idempotent:\n  first:  %v\n  second: %v", first, second)
	}
	if firstRaw != secondRaw {
		t.Errorf("config.toml not byte-stable across reruns:\n  first:  %s\n  second: %s", firstRaw, secondRaw)
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

// --- helpers ---

func loadGrokConfig(t *testing.T, home string) map[string]any {
	t.Helper()
	data, err := adapters.LoadTOMLFile(filepath.Join(home, ".grok", "config.toml"))
	if err != nil {
		t.Fatalf("load grok config.toml: %v", err)
	}
	return data
}

// rawGrokConfig returns the raw config.toml bytes as a string — used to assert
// a secret (or a would-be-baked id) never appears anywhere in the file.
func rawGrokConfig(t *testing.T, home string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, ".grok", "config.toml"))
	if err != nil {
		t.Fatalf("read grok config.toml: %v", err)
	}
	return string(raw)
}

func requireGrokZeropsServer(t *testing.T, home string) map[string]any {
	t.Helper()
	servers, ok := loadGrokConfig(t, home)["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatalf("config.toml missing [mcp_servers] table")
	}
	zerops, ok := servers["zerops"].(map[string]any)
	if !ok {
		t.Fatalf("config.toml missing [mcp_servers.zerops]; got %v", servers)
	}
	return zerops
}
