package adapters_test

import (
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/init/adapters"
	"github.com/zeropsio/zcp/internal/runtime"
)

// Cursor installer creates BOTH /home/<user>/.local/bin/cursor-agent
// AND /home/<user>/.local/bin/agent, but Detect keys ONLY on
// cursor-agent: the bare `agent` name collides with the grok CLI on
// Zerops containers (live-confirmed false positive 2026-07-03 — see
// cursor.go's doc comment).

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

// TestCursor_Detect_OnlyAgentPresent_False pins the collision fix: the
// bare `agent` name is also grok's binary name on Zerops containers
// (~/.local/bin/agent -> ~/.grok/bin/agent -> grok), so a container
// without Cursor but with grok installed must NOT be detected as
// having Cursor. This kills the live-confirmed false positive where
// grok's `agent --version` satisfied Validate and Cursor configs were
// written on a Cursor-less container.
func TestCursor_Detect_OnlyAgentPresent_False(t *testing.T) {
	t.Parallel()
	env := newCursorEnv(t, t.TempDir())
	env.LookPath = stubCursorLookPathOnlyAgent
	if adapters.NewCursor().Detect(env) {
		t.Error("Detect should be false with only bare `agent` present (grok collision on Zerops containers)")
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

// TestCursor_ContainerInit_WritesWorkspaceTrust pins that ContainerInit
// writes ~/.cursor/projects/<flat-workspace>/.workspace-trusted with
// the schema Cursor expects ({trustedAt, workspacePath}). Without this,
// first interactive `agent` run in /var/www gates on the workspace-trust
// prompt — defeats the headless container UX.
func TestCursor_ContainerInit_WritesWorkspaceTrust(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newCursorEnv(t, home)

	if err := adapters.NewCursor().ContainerInit(env); err != nil {
		t.Fatalf("ContainerInit: %v", err)
	}

	trustPath := filepath.Join(home, ".cursor", "projects", "var-www", ".workspace-trusted")
	data, err := adapters.LoadJSONFile(trustPath)
	if err != nil {
		t.Fatalf("load %s: %v", trustPath, err)
	}
	if data["workspacePath"] != "/var/www" {
		t.Errorf("trustedFile.workspacePath = %v, want \"/var/www\"", data["workspacePath"])
	}
	ts, _ := data["trustedAt"].(string)
	if ts == "" {
		t.Errorf("trustedFile.trustedAt missing; got %v", data)
	}
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		t.Errorf("trustedFile.trustedAt = %q must be RFC3339Nano: %v", ts, err)
	}
}

// TestCursor_ContainerInit_WorkspaceDirFlattening pins the directory-name
// convention Cursor uses (slashes → dashes, leading slash stripped).
// Verified empirically against Cursor v2026.05.20-2b5dd59: `cd /var/www
// && agent mcp enable zerops` populated ~/.cursor/projects/var-www/.
// Without identical flattening, our trust file lands in the wrong dir
// and Cursor still gates on the trust prompt.
func TestCursor_ContainerInit_WorkspaceDirFlattening(t *testing.T) {
	t.Parallel()
	cases := []struct {
		workspace string
		want      string
	}{
		{"/var/www", "var-www"},
		{"/Users/me/myproject", "Users-me-myproject"},
		{"/tmp/cursor-fresh-test", "tmp-cursor-fresh-test"},
		{"/a", "a"},
	}
	for _, c := range cases {
		t.Run(c.workspace, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			env := newCursorEnv(t, home)
			env.VSCodeWorkDir = c.workspace
			if err := adapters.NewCursor().ContainerInit(env); err != nil {
				t.Fatalf("ContainerInit: %v", err)
			}
			trustPath := filepath.Join(home, ".cursor", "projects", c.want, ".workspace-trusted")
			if _, err := adapters.LoadJSONFile(trustPath); err != nil {
				t.Errorf("expected trust file at %s for workspace %q, got load error: %v",
					trustPath, c.workspace, err)
			}
		})
	}
}

// TestCursor_ContainerInit_RefreshesTrustTimestamp pins that re-running
// ContainerInit updates the `trustedAt` timestamp (idempotent for
// schema, not for timestamp — every init bumps it). This is intentional:
// the trust file existence + recent timestamp is itself the contract;
// stale trust files would be confusing.
func TestCursor_ContainerInit_RefreshesTrustTimestamp(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := newCursorEnv(t, home)

	if err := adapters.NewCursor().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	trustPath := filepath.Join(home, ".cursor", "projects", "var-www", ".workspace-trusted")
	first, _ := adapters.LoadJSONFile(trustPath)
	firstTS := first["trustedAt"].(string)

	time.Sleep(2 * time.Millisecond)

	if err := adapters.NewCursor().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	second, _ := adapters.LoadJSONFile(trustPath)
	secondTS := second["trustedAt"].(string)

	if firstTS == secondTS {
		t.Errorf("trustedAt should refresh on re-init; got identical %q", firstTS)
	}
	if second["workspacePath"] != "/var/www" {
		t.Errorf("workspacePath should remain /var/www across re-init; got %v", second["workspacePath"])
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

// TestCursor_ContainerInit_MCPJSONIdempotent pins that the mcp.json
// write is byte-stable across reruns. The .workspace-trusted file is
// NOT byte-stable (timestamp refreshes — see
// TestCursor_ContainerInit_RefreshesTrustTimestamp), so this only
// asserts on mcp.json.
func TestCursor_ContainerInit_MCPJSONIdempotent(t *testing.T) {
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
		t.Errorf("ContainerInit mcp.json not idempotent:\n  first:  %v\n  second: %v", first, second)
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

// TestCursor_MCPEntry_BakesLiteralRuntimeEnv pins the env block
// contract — Cursor RESTRICTS the spawned MCP subprocess's env (verified
// 2026-05-24 by wrapping zcp serve with a logger; Cursor passed only
// HOME/USER/PATH). Without explicit forwarding of ZCP_API_KEY +
// serviceId + hostname + projectId, zcp serve sees runtime.Detect
// returning InContainer=false / loses API auth.
//
// The values are baked as RESOLVED LITERALS (grok-parity), NOT Cursor's
// "${env:NAME}" substitution. "${env:NAME}" resolves against
// CURSOR-AGENT's own launch env — which some real launch contexts lack
// (live-confirmed 2026-07-03: the Zerops webterminal launches
// cursor-agent without the zembed vars, so "${env:ZCP_API_KEY}"
// resolved empty and zcp serve closed the MCP connection — "MCP error
// -32000: Connection closed"). Baking the value the init process already
// holds makes the server independent of the launch env. Pin guards
// against a regression back to "${env:...}".
func TestCursor_MCPEntry_BakesLiteralRuntimeEnv(t *testing.T) {
	// Not parallel — sets ZCP_API_KEY so the literal assertion is deterministic.
	home := t.TempDir()
	env := newCursorEnv(t, home)
	env.RT = runtime.Info{InContainer: true, ServiceID: "svc-123", ServiceName: "appdev", ProjectID: "proj-456"}
	t.Setenv("ZCP_API_KEY", "secret-key-value")

	if err := adapters.NewCursor().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	config := loadCursorJSON(t, home)
	zerops := config["mcpServers"].(map[string]any)["zerops"].(map[string]any)

	envMap, ok := zerops["env"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers.zerops.env missing or wrong shape; got %v (type %T)", zerops["env"], zerops["env"])
	}
	want := map[string]string{
		"serviceId":   "svc-123",
		"hostname":    "appdev",
		"projectId":   "proj-456",
		"ZCP_API_KEY": "secret-key-value",
	}
	for k, v := range want {
		if envMap[k] != v {
			t.Errorf("env.%s = %v, want %q (literal value — must not interpolate: launch env may lack the var)", k, envMap[k], v)
		}
	}

	if _, has := zerops["env_vars"]; has {
		t.Errorf("mcpServers.zerops.env_vars present (%v); env_vars is Codex-specific syntax — Cursor silently ignores", zerops["env_vars"])
	}
	if _, has := zerops["envFile"]; has {
		t.Errorf("mcpServers.zerops.envFile present (%v); ZCP doesn't own envFile (user-owned secret)", zerops["envFile"])
	}
}

// TestCursor_MCPEntry_OmitsEmptyEnvVars pins that env keys are omitted
// (not written blank) when unresolved — a blank ZCP_API_KEY/serviceId is
// worse than absent (it shadows nothing but reads as a phantom value).
// Grok-parity (TestGrok_MCPEntry_OmitsAPIKeyWhenUnset).
func TestCursor_MCPEntry_OmitsEmptyEnvVars(t *testing.T) {
	// Not parallel — clears ZCP_API_KEY.
	home := t.TempDir()
	env := newCursorEnv(t, home)
	env.RT = runtime.Info{InContainer: true} // no ServiceID/Name/ProjectID
	t.Setenv("ZCP_API_KEY", "")

	if err := adapters.NewCursor().ContainerInit(env); err != nil {
		t.Fatal(err)
	}
	config := loadCursorJSON(t, home)
	zerops := config["mcpServers"].(map[string]any)["zerops"].(map[string]any)
	envMap, _ := zerops["env"].(map[string]any)
	for _, k := range []string{"ZCP_API_KEY", "serviceId", "hostname", "projectId"} {
		if _, present := envMap[k]; present {
			t.Errorf("env.%s present but should be omitted when unresolved; got %v", k, envMap[k])
		}
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
