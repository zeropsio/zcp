// Tests for: init package — zcp init subcommand.
package init_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/runtime"
)

// stubContainerCommands prevents container init steps from running real commands.
func stubContainerCommands(t *testing.T) {
	t.Helper()
	zcpinit.SetCommandRunner(func(_ string, _ ...string) error { return nil })
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })
	zcpinit.SetVSCodeWorkDir(t.TempDir())
	t.Cleanup(func() { zcpinit.ResetVSCodeWorkDir() })
}

func TestRun_GeneratesCLAUDEMD(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := zcpinit.Run(dir, runtime.Info{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Post-multi-agent migration: canonical body lives in AGENTS.md;
	// CLAUDE.md is a thin @AGENTS.md wrapper that pulls the body into
	// Claude Code's system prompt via its native @-include syntax.
	agentsData, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if !strings.Contains(string(agentsData), "# Zerops") {
		t.Error("AGENTS.md should contain '# Zerops' heading (canonical body)")
	}

	claudeData, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(claudeData), "@AGENTS.md") {
		t.Errorf("CLAUDE.md should contain @AGENTS.md include (got: %s)", claudeData)
	}
}

// TestUpgrade_MigratesReflogFromClaudeMDToAgentsMD pins the
// backward-compat migration path: a user upgrading from a pre-multi-
// agent ZCP has CLAUDE.md with REFLOG entries appended by past
// bootstraps. The first `zcp init` after upgrade must relocate REFLOG
// to AGENTS.md (so all agents — Claude, Codex, future Cursor/Gemini —
// see the history at startup) and shrink CLAUDE.md to the @AGENTS.md
// wrapper.
func TestUpgrade_MigratesReflogFromClaudeMDToAgentsMD(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Simulate a pre-upgrade state: CLAUDE.md exists with ZCP-managed
	// body and a REFLOG section appended by a past bootstrap. AGENTS.md
	// does NOT exist (multi-agent migration hasn't run yet).
	initialClaude := "<!-- ZCP:BEGIN -->\n# Zerops\n\nOLD BODY\n<!-- ZCP:END -->\n" +
		"\n<!-- ZEROPS:REFLOG -->\n" +
		"### 2026-04-19 — Bootstrap: test entry\n\n- **Session:** abc123\n" +
		"<!-- /ZEROPS:REFLOG -->\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(initialClaude), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run the new multi-agent init.
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// AGENTS.md should now exist with current body + migrated REFLOG.
	agentsContent, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md must be created during migration: %v", err)
	}
	agents := string(agentsContent)
	if !strings.Contains(agents, "# Zerops") {
		t.Error("AGENTS.md must contain template body after migration")
	}
	if !strings.Contains(agents, "ZEROPS:REFLOG") {
		t.Error("AGENTS.md must contain migrated REFLOG section")
	}
	if !strings.Contains(agents, "Session:** abc123") {
		t.Error("AGENTS.md must preserve REFLOG entry body")
	}

	// CLAUDE.md should be the thin wrapper now — REFLOG moved to AGENTS.md.
	claudeContent, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	claude := string(claudeContent)
	if strings.Contains(claude, "ZEROPS:REFLOG") {
		t.Errorf("CLAUDE.md must not retain REFLOG after migration (moved to AGENTS.md): %s", claude)
	}
	if !strings.Contains(claude, "@AGENTS.md") {
		t.Errorf("CLAUDE.md must be @AGENTS.md wrapper after migration: %s", claude)
	}
	if strings.Contains(claude, "OLD BODY") {
		t.Errorf("CLAUDE.md must not retain old body after migration: %s", claude)
	}
}

// TestUpgrade_MigrationIdempotent pins idempotence: the migration runs
// at most once. Once AGENTS.md exists, subsequent inits leave it (and
// CLAUDE.md) byte-stable.
func TestUpgrade_MigrationIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	initialClaude := "<!-- ZCP:BEGIN -->\n# old\n<!-- ZCP:END -->\n" +
		"\n<!-- ZEROPS:REFLOG -->\nentry 1\n<!-- /ZEROPS:REFLOG -->\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(initialClaude), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatal(err)
	}
	firstAgents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	firstClaude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatal(err)
	}
	secondAgents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	secondClaude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))

	if string(firstAgents) != string(secondAgents) {
		t.Errorf("AGENTS.md not byte-stable across reruns:\n  first:  %s\n  second: %s", firstAgents, secondAgents)
	}
	if string(firstClaude) != string(secondClaude) {
		t.Errorf("CLAUDE.md not byte-stable across reruns:\n  first:  %s\n  second: %s", firstClaude, secondClaude)
	}
}

// TestUpgrade_PreservesUserContentOutsideMarkers pins user-edit
// preservation: hand-added content outside the ZCP:BEGIN/END markers
// in CLAUDE.md survives the multi-agent migration and subsequent
// re-inits. Critical published-product backward-compat invariant.
func TestUpgrade_PreservesUserContentOutsideMarkers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	initialClaude := "<!-- ZCP:BEGIN -->\n# old\n<!-- ZCP:END -->\n" +
		"\n## User-authored notes\nMy custom guidance for this project.\n" +
		"\n<!-- ZEROPS:REFLOG -->\nentry\n<!-- /ZEROPS:REFLOG -->\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(initialClaude), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	gotStr := string(got)
	if !strings.Contains(gotStr, "## User-authored notes") {
		t.Errorf("user heading outside markers lost:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "My custom guidance for this project.") {
		t.Errorf("user body outside markers lost:\n%s", gotStr)
	}
}

func TestRun_GeneratesMCPConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := zcpinit.Run(dir, runtime.Info{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "zcp") {
		t.Error(".mcp.json should reference zcp")
	}
}

// TestRun_MCPConfig_PreservesUserContent pins the merge-aware local
// .mcp.json write: ZCP owns mcpServers.zerops.{command,args} (reasserted
// every init); everything else is user-owned and must survive re-init —
// extra keys inside the zerops entry (env.ZCP_API_KEY is the documented
// per-project key location that build-integration reads via jq), other
// mcpServers entries, and top-level fields. Published-product
// backward-compat invariant: the pre-merge verbatim template overwrite
// wiped all three on every re-init.
func TestRun_MCPConfig_PreservesUserContent(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	seed := `{
  "mcpServers": {
    "zerops": {
      "command": "stale-binary",
      "args": ["stale-arg"],
      "env": { "ZCP_API_KEY": "user-secret" }
    },
    "github": { "command": "gh-mcp", "args": ["--stdio"] }
  },
  "customTopLevel": true
}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parse .mcp.json: %v", err)
	}

	servers, _ := got["mcpServers"].(map[string]any)
	zerops, _ := servers["zerops"].(map[string]any)
	if zerops == nil {
		t.Fatalf("mcpServers.zerops missing:\n%s", raw)
	}

	// ZCP-owned keys reasserted from the template.
	if cmd, _ := zerops["command"].(string); cmd != "zcp" {
		t.Errorf("zerops.command = %q, want %q (ZCP-owned key must be reasserted)", cmd, "zcp")
	}
	args, _ := zerops["args"].([]any)
	if len(args) != 1 || args[0] != "serve" {
		t.Errorf("zerops.args = %v, want [serve] (ZCP-owned key must be reasserted)", args)
	}

	// User-owned content preserved.
	env, _ := zerops["env"].(map[string]any)
	if key, _ := env["ZCP_API_KEY"].(string); key != "user-secret" {
		t.Errorf("zerops.env.ZCP_API_KEY = %q, want %q (user key location must survive re-init)", key, "user-secret")
	}
	github, _ := servers["github"].(map[string]any)
	if cmd, _ := github["command"].(string); cmd != "gh-mcp" {
		t.Errorf("user-added github server lost: %v", servers["github"])
	}
	if v, _ := got["customTopLevel"].(bool); !v {
		t.Errorf("user top-level field lost:\n%s", raw)
	}
}

// TestRun_MCPConfig_IdempotentBytes pins byte-stability of .mcp.json
// across re-runs — a re-init must not churn a committed file.
func TestRun_MCPConfig_IdempotentBytes(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if string(first) != string(second) {
		t.Errorf(".mcp.json not byte-stable across reruns:\n  first:  %s\n  second: %s", first, second)
	}
}

// TestRun_MCPConfig_MalformedFails pins the failure mode: a malformed
// .mcp.json must abort init with an error, never be silently overwritten
// (the broken bytes may still hold the user's ZCP_API_KEY).
func TestRun_MCPConfig_MalformedFails(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := zcpinit.Run(dir, runtime.Info{})
	if err == nil {
		t.Fatal("Run() succeeded on malformed .mcp.json; want parse error (silent overwrite would destroy user content)")
	}
	if !strings.Contains(err.Error(), ".mcp.json") {
		t.Errorf("error should name .mcp.json: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if string(raw) != "{not json" {
		t.Errorf("malformed .mcp.json was rewritten to %q; original bytes must be left untouched", raw)
	}
}

// TestRun_CursorProjectConfig_LocalFreshInit_WritesAllThreeFiles pins the
// local-mode Cursor project-scope config step: .cursor/cli.json's
// permissions.allow pre-approves the zerops MCP server for cursor-agent's
// per-tool-call gate, .cursor/permissions.json's mcpAllowlist does the
// same for Cursor IDE, and .cursor/mcp.json registers the server (local
// mode owns registration at project scope; container mode's single owner
// stays user-scope ~/.cursor/mcp.json — see the container test below).
func TestRun_CursorProjectConfig_LocalFreshInit_WritesAllThreeFiles(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	cli, err := os.ReadFile(filepath.Join(dir, ".cursor", "cli.json"))
	if err != nil {
		t.Fatalf("read .cursor/cli.json: %v", err)
	}
	var cliData map[string]any
	if err := json.Unmarshal(cli, &cliData); err != nil {
		t.Fatalf("parse .cursor/cli.json: %v", err)
	}
	allow, _ := cliData["permissions"].(map[string]any)["allow"].([]any)
	if len(allow) != 1 || allow[0] != "Mcp(zerops:*)" {
		t.Errorf("cli.json permissions.allow = %v, want [%q]", allow, "Mcp(zerops:*)")
	}
	// permissions.deny is REQUIRED by Cursor's cli.json schema —
	// live-verified 2026-07-03 (binary 2026.07.01-41b2de7): without the
	// key, schema validation fails and cursor-agent refuses to start in
	// the workspace entirely (even --version exits 1).
	deny, ok := cliData["permissions"].(map[string]any)["deny"].([]any)
	if !ok || len(deny) != 0 {
		t.Errorf("cli.json permissions.deny = %v (present=%v), want [] (schema-required empty array)", deny, ok)
	}

	perm, err := os.ReadFile(filepath.Join(dir, ".cursor", "permissions.json"))
	if err != nil {
		t.Fatalf("read .cursor/permissions.json: %v", err)
	}
	var permData map[string]any
	if err := json.Unmarshal(perm, &permData); err != nil {
		t.Fatalf("parse .cursor/permissions.json: %v", err)
	}
	mcpAllowlist, _ := permData["mcpAllowlist"].([]any)
	if len(mcpAllowlist) != 1 || mcpAllowlist[0] != "zerops:*" {
		t.Errorf("permissions.json mcpAllowlist = %v, want [%q]", mcpAllowlist, "zerops:*")
	}

	mcp, err := os.ReadFile(filepath.Join(dir, ".cursor", "mcp.json"))
	if err != nil {
		t.Fatalf("read .cursor/mcp.json: %v", err)
	}
	var mcpData map[string]any
	if err := json.Unmarshal(mcp, &mcpData); err != nil {
		t.Fatalf("parse .cursor/mcp.json: %v", err)
	}
	servers, _ := mcpData["mcpServers"].(map[string]any)
	zerops, _ := servers["zerops"].(map[string]any)
	if zerops == nil {
		t.Fatalf("mcp.json missing mcpServers.zerops; got %v", mcpData)
	}
	if zerops["type"] != "stdio" {
		t.Errorf("mcp.json zerops.type = %v, want %q (required by Cursor schema)", zerops["type"], "stdio")
	}
	if zerops["command"] != "zcp" {
		t.Errorf("mcp.json zerops.command = %v, want %q", zerops["command"], "zcp")
	}
	args, _ := zerops["args"].([]any)
	if len(args) != 1 || args[0] != "serve" {
		t.Errorf("mcp.json zerops.args = %v, want [\"serve\"]", args)
	}
}

// TestRun_CursorProjectConfig_UserAllowWithoutDeny_DenyAdded pins the
// schema-required deny key against a pre-existing user cli.json that has
// allow entries but no deny: the deny array must be created empty while
// the user's allow entries survive (Cursor's schema rejects the file —
// and cursor-agent refuses to run in the workspace — when deny is absent).
func TestRun_CursorProjectConfig_UserAllowWithoutDeny_DenyAdded(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	if err := os.MkdirAll(filepath.Join(dir, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{"permissions":{"allow":["Shell(ls)"]}}`
	if err := os.WriteFile(filepath.Join(dir, ".cursor", "cli.json"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".cursor", "cli.json"))
	if err != nil {
		t.Fatalf("read .cursor/cli.json: %v", err)
	}
	var cliData map[string]any
	if err := json.Unmarshal(raw, &cliData); err != nil {
		t.Fatalf("parse .cursor/cli.json: %v", err)
	}
	perms, _ := cliData["permissions"].(map[string]any)
	allow, _ := perms["allow"].([]any)
	if len(allow) != 2 || allow[0] != "Shell(ls)" || allow[1] != "Mcp(zerops:*)" {
		t.Errorf("allow = %v, want [Shell(ls) Mcp(zerops:*)]", allow)
	}
	deny, ok := perms["deny"].([]any)
	if !ok || len(deny) != 0 {
		t.Errorf("deny = %v (present=%v), want [] (schema-required)", deny, ok)
	}
}

// TestRun_CursorProjectConfig_WrongShape_AbortsUntouched pins the
// wrong-TYPE posture (Codex review 2026-07-03): a well-formed JSON file
// whose node at a ZCP-written path has the wrong type (permissions as
// array, mcpAllowlist as object, mcpServers as array) must abort init
// with an error naming the file, leaving the original bytes untouched —
// the merge helpers would otherwise silently replace the node (data
// loss) or wrap a map into a schema-invalid array. Same posture as
// malformed JSON: never silently rewrite unexpected user content.
func TestRun_CursorProjectConfig_WrongShape_AbortsUntouched(t *testing.T) {
	// Not parallel — subtests mutate HOME env var.
	cases := []struct {
		name string
		file string
		seed string
	}{
		{"permissions is array", "cli.json", `{"permissions":["Shell(ls)"]}`},
		{"allow is object", "cli.json", `{"permissions":{"allow":{"x":true}}}`},
		{"mcpAllowlist is object", "permissions.json", `{"mcpAllowlist":{"other":true}}`},
		{"mcpServers is array", "mcp.json", `{"mcpServers":[{"command":"zcp"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			if err := os.MkdirAll(filepath.Join(dir, ".cursor"), 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, ".cursor", tc.file)
			if err := os.WriteFile(path, []byte(tc.seed), 0o644); err != nil {
				t.Fatal(err)
			}

			err := zcpinit.Run(dir, runtime.Info{})
			if err == nil {
				t.Fatalf("Run() succeeded with wrong-shape %s; want error (silent rewrite loses user content)", tc.file)
			}
			if !strings.Contains(err.Error(), tc.file) {
				t.Errorf("error should name the file %s: %v", tc.file, err)
			}
			raw, _ := os.ReadFile(path)
			if string(raw) != tc.seed {
				t.Errorf("wrong-shape %s was rewritten to %q; original bytes must be left untouched", tc.file, raw)
			}
		})
	}
}

// TestRun_MCPConfig_WrongShape_AbortsUntouched pins the same wrong-TYPE
// posture for the local .mcp.json writer: mcpServers present as a
// non-object must abort, never be replaced.
func TestRun_MCPConfig_WrongShape_AbortsUntouched(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	seed := `{"mcpServers":"oops"}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	err := zcpinit.Run(dir, runtime.Info{})
	if err == nil {
		t.Fatal("Run() succeeded with wrong-shape .mcp.json; want error")
	}
	if !strings.Contains(err.Error(), ".mcp.json") {
		t.Errorf("error should name .mcp.json: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if string(raw) != seed {
		t.Errorf(".mcp.json was rewritten to %q; original bytes must be left untouched", raw)
	}
}

// TestRun_CursorProjectConfig_ScalarAllow_NormalizedPreserving pins the
// one tolerated shape deviation: a hand-set scalar string in an
// array-valued slot is wrapped to a one-element array (intent preserved,
// output valid) rather than rejected — mirrors AppendIfMissingString's
// historical normalization contract.
func TestRun_CursorProjectConfig_ScalarAllow_NormalizedPreserving(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	if err := os.MkdirAll(filepath.Join(dir, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{"permissions":{"allow":"Shell(ls)","deny":[]}}`
	if err := os.WriteFile(filepath.Join(dir, ".cursor", "cli.json"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(dir, ".cursor", "cli.json"))
	var cliData map[string]any
	if err := json.Unmarshal(raw, &cliData); err != nil {
		t.Fatalf("parse cli.json: %v", err)
	}
	allow, _ := cliData["permissions"].(map[string]any)["allow"].([]any)
	if len(allow) != 2 || allow[0] != "Shell(ls)" || allow[1] != "Mcp(zerops:*)" {
		t.Errorf("allow = %v, want [Shell(ls) Mcp(zerops:*)] (scalar wrapped, intent preserved)", allow)
	}
}

// TestRun_CursorProjectConfig_ContainerMode_SkipsMCPJSON pins that
// container mode writes ONLY .cursor/cli.json + .cursor/permissions.json
// (project-scope permission grants) and never .cursor/mcp.json — MCP
// registration in container mode has exactly one owner, the Cursor
// adapter's ContainerInit writing user-scope ~/.cursor/mcp.json. A
// project-scope copy would fork registration across two owners and risk
// Cursor's project→global same-key-replaces-wholesale merge breaking the
// env-forwarding entry the container adapter depends on.
func TestRun_CursorProjectConfig_ContainerMode_SkipsMCPJSON(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubContainerCommands(t)

	if err := zcpinit.Run(dir, runtime.Info{InContainer: true}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".cursor", "cli.json")); err != nil {
		t.Errorf(".cursor/cli.json should be created in container mode: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".cursor", "permissions.json")); err != nil {
		t.Errorf(".cursor/permissions.json should be created in container mode: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".cursor", "mcp.json")); !os.IsNotExist(err) {
		t.Error(".cursor/mcp.json should NOT be created in container mode (registration owner is user-scope ~/.cursor/mcp.json)")
	}
}

// TestRun_CursorProjectConfig_PreservesUserContent pins the merge-aware
// write across all three files: ZCP owns exactly the tokens/keys it
// asserts; everything else (other allow/deny entries, other allowlist
// entries, terminalAllowlist, other mcpServers entries, extra keys inside
// the zerops mcp.json entry, unrelated top-level fields) survives re-init.
func TestRun_CursorProjectConfig_PreservesUserContent(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	if err := os.MkdirAll(filepath.Join(dir, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	cliSeed := `{"permissions":{"allow":["Shell(ls)"],"deny":["Shell(rm)"]},"customTopLevel":true}`
	if err := os.WriteFile(filepath.Join(dir, ".cursor", "cli.json"), []byte(cliSeed), 0o644); err != nil {
		t.Fatal(err)
	}
	permSeed := `{"mcpAllowlist":["other:*"],"terminalAllowlist":["ls","pwd"]}`
	if err := os.WriteFile(filepath.Join(dir, ".cursor", "permissions.json"), []byte(permSeed), 0o644); err != nil {
		t.Fatal(err)
	}
	mcpSeed := `{
  "mcpServers": {
    "zerops": {"command": "stale-binary", "args": ["stale-arg"], "env": {"CUSTOM": "keep-me"}},
    "github": {"command": "gh-mcp", "args": ["--stdio"]}
  }
}`
	if err := os.WriteFile(filepath.Join(dir, ".cursor", "mcp.json"), []byte(mcpSeed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	cliRaw, err := os.ReadFile(filepath.Join(dir, ".cursor", "cli.json"))
	if err != nil {
		t.Fatalf("read cli.json: %v", err)
	}
	var cliData map[string]any
	if err := json.Unmarshal(cliRaw, &cliData); err != nil {
		t.Fatalf("parse cli.json: %v", err)
	}
	allow, _ := cliData["permissions"].(map[string]any)["allow"].([]any)
	wantAllow := []any{"Shell(ls)", "Mcp(zerops:*)"}
	if !reflect.DeepEqual(allow, wantAllow) {
		t.Errorf("cli.json permissions.allow = %v, want %v", allow, wantAllow)
	}
	deny, _ := cliData["permissions"].(map[string]any)["deny"].([]any)
	if len(deny) != 1 || deny[0] != "Shell(rm)" {
		t.Errorf("cli.json permissions.deny lost: %v", deny)
	}
	if v, _ := cliData["customTopLevel"].(bool); !v {
		t.Errorf("cli.json customTopLevel lost: %v", cliData)
	}

	permRaw, err := os.ReadFile(filepath.Join(dir, ".cursor", "permissions.json"))
	if err != nil {
		t.Fatalf("read permissions.json: %v", err)
	}
	var permData map[string]any
	if err := json.Unmarshal(permRaw, &permData); err != nil {
		t.Fatalf("parse permissions.json: %v", err)
	}
	mcpAllowlist, _ := permData["mcpAllowlist"].([]any)
	wantAllowlist := []any{"other:*", "zerops:*"}
	if !reflect.DeepEqual(mcpAllowlist, wantAllowlist) {
		t.Errorf("permissions.json mcpAllowlist = %v, want %v", mcpAllowlist, wantAllowlist)
	}
	termAllow, _ := permData["terminalAllowlist"].([]any)
	if len(termAllow) != 2 {
		t.Errorf("permissions.json terminalAllowlist lost: %v", termAllow)
	}

	mcpRaw, err := os.ReadFile(filepath.Join(dir, ".cursor", "mcp.json"))
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	var mcpData map[string]any
	if err := json.Unmarshal(mcpRaw, &mcpData); err != nil {
		t.Fatalf("parse mcp.json: %v", err)
	}
	servers, _ := mcpData["mcpServers"].(map[string]any)
	zerops, _ := servers["zerops"].(map[string]any)
	if zerops == nil {
		t.Fatalf("mcp.json missing mcpServers.zerops: %v", servers)
	}
	if zerops["command"] != "zcp" {
		t.Errorf("mcp.json zerops.command = %v, want %q (ZCP-owned key must be reasserted)", zerops["command"], "zcp")
	}
	args, _ := zerops["args"].([]any)
	if len(args) != 1 || args[0] != "serve" {
		t.Errorf("mcp.json zerops.args = %v, want [serve] (ZCP-owned key must be reasserted)", args)
	}
	env, _ := zerops["env"].(map[string]any)
	if key, _ := env["CUSTOM"].(string); key != "keep-me" {
		t.Errorf("mcp.json zerops.env.CUSTOM = %q, want %q (user-added key inside the entry must survive)", key, "keep-me")
	}
	github, _ := servers["github"].(map[string]any)
	if cmd, _ := github["command"].(string); cmd != "gh-mcp" {
		t.Errorf("mcp.json user-added github server lost: %v", servers["github"])
	}
}

// TestRun_CursorProjectConfig_Rerun_ByteIdentical pins byte-stability of
// all three project-scope files across re-runs — a re-init must not churn
// committed files or duplicate array entries.
func TestRun_CursorProjectConfig_Rerun_ByteIdentical(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	files := []string{
		filepath.Join(dir, ".cursor", "cli.json"),
		filepath.Join(dir, ".cursor", "permissions.json"),
		filepath.Join(dir, ".cursor", "mcp.json"),
	}
	first := make(map[string][]byte, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		first[f] = data
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}
	for _, f := range files {
		second, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s after second run: %v", f, err)
		}
		if string(first[f]) != string(second) {
			t.Errorf("%s not byte-stable across reruns:\n  first:  %s\n  second: %s", f, first[f], second)
		}
	}
}

// TestRun_CursorProjectConfig_MalformedCliJSON_RunFails pins the failure
// mode: malformed .cursor/cli.json must abort init with an error and must
// never be silently overwritten (same posture as .mcp.json).
func TestRun_CursorProjectConfig_MalformedCliJSON_RunFails(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	if err := os.MkdirAll(filepath.Join(dir, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	cliPath := filepath.Join(dir, ".cursor", "cli.json")
	if err := os.WriteFile(cliPath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := zcpinit.Run(dir, runtime.Info{})
	if err == nil {
		t.Fatal("Run() succeeded on malformed .cursor/cli.json; want parse error (silent overwrite would destroy user content)")
	}
	raw, _ := os.ReadFile(cliPath)
	if string(raw) != "{not json" {
		t.Errorf("malformed cli.json was rewritten to %q; original bytes must be left untouched", raw)
	}
}

// TestRun_CursorProjectConfig_Tokens_DerivedFromTemplateKey is the
// single-owner pin: the emitted Mcp(<key>:*) / <key>:* tokens must be
// derived from the mcp-config.json template's canonical server key, not
// a second hardcoded "zerops" literal. Parses the template itself so a
// future key rename breaks this test loudly rather than silently forking
// the Cursor permission namespace from the rest of the adapter family.
func TestRun_CursorProjectConfig_Tokens_DerivedFromTemplateKey(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	tmpl, err := content.GetTemplate("mcp-config.json")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(tmpl), &parsed); err != nil {
		t.Fatalf("parse mcp-config.json template: %v", err)
	}
	servers, _ := parsed["mcpServers"].(map[string]any)
	if len(servers) != 1 {
		t.Fatalf("expected exactly one canonical server key in the template (pinned by content.TestMCPServerNameCanonical), got %d: %v", len(servers), servers)
	}
	var key string
	for k := range servers {
		key = k
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	cli, err := os.ReadFile(filepath.Join(dir, ".cursor", "cli.json"))
	if err != nil {
		t.Fatalf("read cli.json: %v", err)
	}
	wantAllowToken := fmt.Sprintf("Mcp(%s:*)", key)
	if !strings.Contains(string(cli), wantAllowToken) {
		t.Errorf("cli.json missing token %q derived from template key %q: %s", wantAllowToken, key, cli)
	}

	perm, err := os.ReadFile(filepath.Join(dir, ".cursor", "permissions.json"))
	if err != nil {
		t.Fatalf("read permissions.json: %v", err)
	}
	wantAllowlistToken := fmt.Sprintf("%q", key+":*")
	if !strings.Contains(string(perm), wantAllowlistToken) {
		t.Errorf("permissions.json missing token %s derived from template key %q: %s", wantAllowlistToken, key, perm)
	}
}

func TestRun_GeneratesSSHConfig(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubContainerCommands(t)

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".ssh", "config"))
	if err != nil {
		t.Fatalf("read ssh config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "zerops") {
		t.Error("ssh config should mention zerops")
	}
}

func TestRun_GeneratesSettingsLocal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := zcpinit.Run(dir, runtime.Info{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings.local.json: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "mcp__zerops__*") {
		t.Error("settings.local.json should contain mcp__zerops__* permission")
	}
}

func TestRun_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Run twice.
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}

	// Files should still exist and be valid.
	agentsData, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md after second run: %v", err)
	}
	if !strings.Contains(string(agentsData), "# Zerops") {
		t.Error("AGENTS.md should still contain '# Zerops' after second run (canonical body)")
	}
	claudeData, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md after second run: %v", err)
	}
	if !strings.Contains(string(claudeData), "@AGENTS.md") {
		t.Error("CLAUDE.md should still contain @AGENTS.md include after second run (wrapper)")
	}
}

func TestRun_GeneratesAliases(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	err := zcpinit.Run(dir, runtime.Info{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".config", "zerops", "aliases"))
	if err != nil {
		t.Fatalf("read aliases: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "alias zcl=") {
		t.Error("aliases file should contain zcl alias")
	}
	if !strings.Contains(content, "--dangerously-skip-permissions") {
		t.Error("aliases file should contain --dangerously-skip-permissions flag")
	}
}

func TestRun_AliasesBashrcSourceLine(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	err := zcpinit.Run(dir, runtime.Info{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".bashrc"))
	if err != nil {
		t.Fatalf("read .bashrc: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, ".config/zerops/aliases") {
		t.Error(".bashrc should source the aliases file")
	}
}

func TestRun_AliasesZshrcSourceLine(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Create .zshrc so the init detects zsh is installed.
	if err := os.WriteFile(filepath.Join(homeDir, ".zshrc"), []byte("# oh-my-zsh\n"), 0644); err != nil {
		t.Fatalf("write .zshrc: %v", err)
	}

	err := zcpinit.Run(dir, runtime.Info{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".zshrc"))
	if err != nil {
		t.Fatalf("read .zshrc: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, ".config/zerops/aliases") {
		t.Error(".zshrc should source the aliases file")
	}
	// Original content should be preserved.
	if !strings.Contains(content, "# oh-my-zsh") {
		t.Error(".zshrc should preserve original content")
	}
}

func TestRun_AliasesSkipsMissingZshrc(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Don't create .zshrc — simulates bash-only system.
	err := zcpinit.Run(dir, runtime.Info{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// .zshrc should NOT exist (we didn't create it, init shouldn't either).
	if _, err := os.Stat(filepath.Join(homeDir, ".zshrc")); !os.IsNotExist(err) {
		t.Error(".zshrc should not be created on bash-only systems")
	}

	// .bashrc should still be created.
	if _, err := os.Stat(filepath.Join(homeDir, ".bashrc")); os.IsNotExist(err) {
		t.Error(".bashrc should be created")
	}
}

func TestRun_AliasesBashrcIdempotent(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Run twice.
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".bashrc"))
	if err != nil {
		t.Fatalf("read .bashrc: %v", err)
	}

	content := string(data)
	count := strings.Count(content, "# Zerops shell aliases")
	if count != 1 {
		t.Errorf("source block should appear exactly once, got %d", count)
	}
}

func TestRun_ReportsSteps(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubContainerCommands(t)

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Container mode: no project .mcp.json (MCP config is in ~/.claude.json).
	files := []string{
		filepath.Join(dir, "CLAUDE.md"),
		filepath.Join(dir, ".claude", "settings.local.json"),
		filepath.Join(homeDir, ".ssh", "config"),
		filepath.Join(homeDir, ".config", "zerops", "aliases"),
	}
	for _, f := range files {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", f)
		}
	}
}

func TestRun_Container_NoProjectMCPConfig(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubContainerCommands(t)

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	mcpPath := filepath.Join(dir, ".mcp.json")
	if _, err := os.Stat(mcpPath); !os.IsNotExist(err) {
		t.Error(".mcp.json should not be created in container mode (MCP config is global in ~/.claude.json)")
	}
}

func TestSSHConfig_Container_ManagedSection(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubContainerCommands(t)

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".ssh", "config"))
	if err != nil {
		t.Fatalf("read ssh config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "# ZCP:BEGIN") {
		t.Error("ssh config should contain ZCP:BEGIN marker")
	}
	if !strings.Contains(content, "# ZCP:END") {
		t.Error("ssh config should contain ZCP:END marker")
	}
	if !strings.Contains(content, "User zerops") {
		t.Error("ssh config should contain 'User zerops' directive")
	}
}

func TestSSHConfig_Container_ControlMaster(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubContainerCommands(t)

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".ssh", "config"))
	if err != nil {
		t.Fatalf("read ssh config: %v", err)
	}

	content := string(data)
	required := []string{
		"ControlMaster auto",
		"ControlPath /tmp/ssh-mux-",
		"ControlPersist 600",
	}
	for _, keyword := range required {
		if !strings.Contains(content, keyword) {
			t.Errorf("ssh config should contain %q for SSH connection multiplexing", keyword)
		}
	}
}

func TestSSHConfig_Container_PreservesExisting(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubContainerCommands(t)

	// Write pre-existing SSH config.
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	existing := "Host github.com\n    IdentityFile ~/.ssh/id_github\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(existing), 0644); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(sshDir, "config"))
	if err != nil {
		t.Fatalf("read ssh config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Host github.com") {
		t.Error("ssh config should preserve existing 'Host github.com' entry")
	}
	if !strings.Contains(content, "IdentityFile ~/.ssh/id_github") {
		t.Error("ssh config should preserve existing IdentityFile directive")
	}
	if !strings.Contains(content, "# ZCP:BEGIN") {
		t.Error("ssh config should contain ZCP managed section")
	}
}

func TestSSHConfig_Container_Idempotent(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubContainerCommands(t)

	rt := runtime.Info{InContainer: true}

	if err := zcpinit.Run(dir, rt); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	if err := zcpinit.Run(dir, rt); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".ssh", "config"))
	if err != nil {
		t.Fatalf("read ssh config: %v", err)
	}

	content := string(data)
	beginCount := strings.Count(content, "# ZCP:BEGIN")
	if beginCount != 1 {
		t.Errorf("ZCP:BEGIN should appear exactly once after two runs, got %d", beginCount)
	}
	endCount := strings.Count(content, "# ZCP:END")
	if endCount != 1 {
		t.Errorf("ZCP:END should appear exactly once after two runs, got %d", endCount)
	}
}

func TestSSHConfig_Local_Skipped(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	err := zcpinit.Run(dir, runtime.Info{InContainer: false})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	sshConfig := filepath.Join(homeDir, ".ssh", "config")
	if _, err := os.Stat(sshConfig); !os.IsNotExist(err) {
		t.Error("ssh config should not be created in local mode")
	}
}

// TestRun_GuidedSkillMaterialized — with guided enabled (the .zcp marker),
// Run writes the WHOLE guided-skill subtree (router + phase files) into EVERY
// agent-discovery root, and the guided block into AGENTS.md. Guided is
// agent-neutral: an agent that reads .agents/skills/ must find the same tree
// Claude Code finds under .claude/skills/.
func TestRun_GuidedSkillMaterialized(t *testing.T) {
	// Not parallel — generateAliases writes to HOME.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	if err := content.SetGuided(dir, true); err != nil {
		t.Fatalf("SetGuided: %v", err)
	}
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Every embedded subtree file (router + phases/*.md) must land in every root.
	tree, err := content.ReadGuidedSkillTree()
	if err != nil {
		t.Fatalf("ReadGuidedSkillTree: %v", err)
	}
	if len(tree) < 2 {
		t.Fatalf("expected a multi-file guided subtree, got %d files", len(tree))
	}
	if len(content.GuidedSkillDirsRel) < 2 {
		t.Fatalf("expected guided to materialize into both agent-discovery roots, got %v", content.GuidedSkillDirsRel)
	}
	for _, root := range content.GuidedSkillDirsRel {
		skill, readErr := os.ReadFile(filepath.Join(dir, filepath.FromSlash(root), "SKILL.md"))
		if readErr != nil {
			t.Fatalf("guided SKILL.md not written under %s: %v", root, readErr)
		}
		if !strings.Contains(string(skill), "name: guided") {
			t.Errorf("guided SKILL.md under %s missing expected content:\n%s", root, skill)
		}
		for _, f := range tree {
			p := filepath.Join(dir, filepath.FromSlash(root), filepath.FromSlash(f.RelPath))
			got, readErr := os.ReadFile(p)
			if readErr != nil {
				t.Errorf("guided skill file %q not materialized under %s: %v", f.RelPath, root, readErr)
				continue
			}
			if string(got) != f.Content {
				t.Errorf("guided skill file %q under %s drifted from the embedded template", f.RelPath, root)
			}
		}
	}

	agents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.Contains(string(agents), "## Guided mode (user-only)") {
		t.Error("AGENTS.md missing guided block under guided mode")
	}
}

// TestRun_GuidedSkill_ToggleOffRemovesSubtree — flipping guided off (plain
// `zcp init` after a `--guided` install) removes the ENTIRE guided subtree
// (phase files included), not just the router, so a plain init leaves a clean
// tree. Pins the toggle's reset semantics for the multi-file subtree.
func TestRun_GuidedSkill_ToggleOffRemovesSubtree(t *testing.T) {
	// Not parallel — generateAliases writes to HOME.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	// First: guided on → subtree present.
	if err := content.SetGuided(dir, true); err != nil {
		t.Fatalf("SetGuided(on): %v", err)
	}
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() (guided on): %v", err)
	}
	for _, root := range content.GuidedSkillDirsRel {
		phasesDir := filepath.Join(dir, filepath.FromSlash(root), "phases")
		if _, err := os.Stat(phasesDir); err != nil {
			t.Fatalf("phases dir should exist under %s when guided is on: %v", root, err)
		}
	}

	// Then: guided off → whole dir gone.
	if err := content.SetGuided(dir, false); err != nil {
		t.Fatalf("SetGuided(off): %v", err)
	}
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() (guided off): %v", err)
	}
	for _, root := range content.GuidedSkillDirsRel {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(root))); !os.IsNotExist(err) {
			t.Errorf("guided skill dir %s must be fully removed when guided toggles off", root)
		}
	}
}

// TestRun_GuidedSkill_NotWrittenWhenOff — off-by-default: plain `zcp init`
// writes no guided skill and no guided block.
func TestRun_GuidedSkill_NotWrittenWhenOff(t *testing.T) {
	// Not parallel — generateAliases writes to HOME.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	for _, root := range content.GuidedSkillDirsRel {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(root), "SKILL.md")); !os.IsNotExist(err) {
			t.Errorf("guided SKILL.md must not be written under %s when guided is off", root)
		}
	}
	agents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if strings.Contains(string(agents), "## Guided mode (user-only)") {
		t.Error("AGENTS.md must not carry guided block when guided is off")
	}
}

// TestRun_GuidedSkill_NotWrittenUnderAuthoring — the mutual-exclusion
// pin at the init layer: authoring context never receives the guided
// skill or the guided block, even when guided is also requested.
func TestRun_GuidedSkill_NotWrittenUnderAuthoring(t *testing.T) {
	// Not parallel — generateAliases writes to HOME.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	// Guided enabled in config, but authoring is on → guided must be suppressed.
	if err := content.SetGuided(dir, true); err != nil {
		t.Fatalf("SetGuided: %v", err)
	}
	if err := zcpinit.Run(dir, runtime.Info{Authoring: true}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	for _, root := range content.GuidedSkillDirsRel {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(root), "SKILL.md")); !os.IsNotExist(err) {
			t.Errorf("guided SKILL.md must NOT be written under %s in authoring (mutual exclusion)", root)
		}
	}
	agents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if strings.Contains(string(agents), "## Guided mode (user-only)") {
		t.Error("AGENTS.md must NOT carry guided block under authoring (mutual exclusion)")
	}
}

// The three tests below pin the line-anchored marker contract for `zcp
// init`: a literal marker string appearing MID-LINE in prose (an agent
// documenting ZCP behavior in CLAUDE.md is the real-world source) is
// content, not structure. Treating a mention as a boundary cuts user
// prose mid-sentence — lines left ending in `-->` with their tails
// swallowed or relocated (real-user incident, 2026-07-04).

func TestInit_MidLineReflogMention_NotMigrated(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	mentionOpen := "The ZCP process maintains the <!-- ZEROPS:REFLOG --> header in this file."
	mentionClose := "The section is closed by the <!-- /ZEROPS:REFLOG --> marker, watch out."
	initialClaude := "<!-- ZCP:BEGIN -->\n@AGENTS.md\n<!-- ZCP:END -->\n" +
		"\n# My notes\n" +
		mentionOpen + "\n" +
		"Some middle prose line the user cares about deeply.\n" +
		mentionClose + "\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(initialClaude), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"),
		[]byte("<!-- ZCP:BEGIN -->\nbody\n<!-- ZCP:END -->\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	claude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	for _, want := range []string{mentionOpen, mentionClose, "Some middle prose line the user cares about deeply."} {
		if !strings.Contains(string(claude), want) {
			t.Errorf("prose with mid-line REFLOG mention was cut from CLAUDE.md:\nmissing %q\ngot:\n%s", want, claude)
		}
	}
	agents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if strings.Contains(string(agents), "watch out") {
		t.Errorf("prose span was relocated to AGENTS.md as a bogus REFLOG section:\n%s", agents)
	}
}

func TestInit_MidLineMarkerMentionAboveBlock_ProseIntact(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	mentionBegin := "ZCP wraps its section in <!-- ZCP:BEGIN --> and you should not edit inside it."
	mentionEnd := "It ends with <!-- ZCP:END --> so everything between is machine-owned."
	initialClaude := "# User notes at top\n" +
		mentionBegin + "\n" +
		"Another precious user line here.\n" +
		mentionEnd + "\n" +
		"\n<!-- ZCP:BEGIN -->\nOLD WRAPPER\n<!-- ZCP:END -->\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(initialClaude), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	claude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	gotStr := string(claude)
	for _, want := range []string{mentionBegin, mentionEnd, "Another precious user line here."} {
		if !strings.Contains(gotStr, want) {
			t.Errorf("prose with mid-line marker mention was cut:\nmissing %q\ngot:\n%s", want, gotStr)
		}
	}
	if strings.Contains(gotStr, "OLD WRAPPER") {
		t.Errorf("stale managed block survived init:\n%s", gotStr)
	}
	if n := strings.Count(gotStr, "@AGENTS.md"); n != 1 {
		t.Errorf("wrapper body must appear exactly once, got %d:\n%s", n, gotStr)
	}
}

func TestInit_MidLineReflogMentionMarkerless_PrependPreserves(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	original := "# Notes\n" +
		"Bootstrap history uses the <!-- ZEROPS:REFLOG --> marker, appended per run.\n" +
		"Precious tail line.\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	claude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	gotStr := string(claude)
	if !strings.Contains(gotStr, original) {
		t.Errorf("markerless file with a mid-line REFLOG mention must be preserved whole (prepend branch):\ngot:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "@AGENTS.md") {
		t.Errorf("managed wrapper block missing after init:\n%s", gotStr)
	}
}

// TestInit_DamagedAgentsMD_NoDataLoss pins the fix for the highest-
// severity regression the marker-anchoring change first introduced: a
// user's AGENTS.md whose managed block was corrupted by the pre-fix
// mid-line-mention bug (a spliced BEGIN, so no line-anchored block)
// still carries a real REFLOG and user prose above it. `zcp init` must
// preserve ALL user prose — the earlier reflog-drop branch dropped
// everything above the first REFLOG marker, deleting it. This is the
// exact population the anchoring fix exists to protect, so the init
// must never make their data loss worse. (Real-user incident context,
// 2026-07-04.)
func TestInit_DamagedAgentsMD_NoDataLoss(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agents := "PRECIOUS prose above the reflog\n" +
		"ZCP maintains the <!-- ZCP:BEGIN -->\n" +
		"OLD AGENTS BODY\n" +
		"<!-- ZCP:END -->\n" +
		"PRECIOUS prose between end and reflog\n" +
		"<!-- ZEROPS:REFLOG -->\n### 2026-01-01 — Bootstrap: x\n- **Session:** s1\n<!-- /ZEROPS:REFLOG -->\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agents), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	gotStr := string(got)
	for _, want := range []string{
		"PRECIOUS prose above the reflog",
		"PRECIOUS prose between end and reflog",
		"Session:** s1",
	} {
		if !strings.Contains(gotStr, want) {
			t.Errorf("data loss: %q gone from AGENTS.md after init:\n%s", want, gotStr)
		}
	}
	// A fresh managed block must have been added.
	if !strings.Contains(gotStr, "<!-- ZCP:BEGIN -->\n") {
		t.Errorf("fresh managed block missing:\n%s", gotStr)
	}
}

// TestInit_DamagedCLAUDEMD_NoDataLoss is the CLAUDE.md sibling: a
// spliced BEGIN with user prose, run through init, must not lose prose.
func TestInit_DamagedCLAUDEMD_NoDataLoss(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	claude := "PRECIOUS head prose\n" +
		"ZCP maintains the <!-- ZCP:BEGIN -->\n" +
		"OLD BODY\n" +
		"<!-- ZCP:END -->\n" +
		"PRECIOUS tail prose\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(claude), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	gotStr := string(got)
	for _, want := range []string{"PRECIOUS head prose", "PRECIOUS tail prose"} {
		if !strings.Contains(gotStr, want) {
			t.Errorf("data loss: %q gone from CLAUDE.md after init:\n%s", want, gotStr)
		}
	}
	if !strings.Contains(gotStr, "@AGENTS.md") {
		t.Errorf("fresh wrapper missing:\n%s", gotStr)
	}
}

// TestRun_SkillRootsAlwaysExist_PlainInit pins spec-skill-packs.md §2: a
// plain `zcp init` (no guided mode, no skill pack installed) must still
// leave both agent-discovery skill roots present, because Claude Code and
// Codex only watch a directory that already exists at session start.
func TestRun_SkillRootsAlwaysExist_PlainInit(t *testing.T) {
	// Not parallel — generateAliases writes to HOME.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	for _, rel := range []string{filepath.Join(".agents", "skills"), filepath.Join(".claude", "skills")} {
		info, err := os.Stat(filepath.Join(dir, rel))
		if err != nil {
			t.Fatalf("%s must exist after plain init: %v", rel, err)
		}
		if !info.IsDir() {
			t.Errorf("%s must be a directory", rel)
		}
	}
}

// TestRun_SkillRootsAlwaysExist_GuidedInit proves the same guarantee holds
// under `zcp init --guided`, where .claude/skills/guided already exists as
// a side effect — .agents/skills must exist too, not just the root the
// guided skill happens to touch.
func TestRun_SkillRootsAlwaysExist_GuidedInit(t *testing.T) {
	// Not parallel — generateAliases writes to HOME.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	if err := content.SetGuided(dir, true); err != nil {
		t.Fatalf("SetGuided: %v", err)
	}
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	for _, rel := range []string{filepath.Join(".agents", "skills"), filepath.Join(".claude", "skills")} {
		info, err := os.Stat(filepath.Join(dir, rel))
		if err != nil {
			t.Fatalf("%s must exist after guided init: %v", rel, err)
		}
		if !info.IsDir() {
			t.Errorf("%s must be a directory", rel)
		}
	}
}

// TestRun_SkillRoots_IdempotentAcrossReinit proves the skill-roots step
// never disturbs existing content: a file placed under .agents/skills
// between two `zcp init` runs (simulating an already-installed skill pack
// skill) must survive byte-identical, and both roots must still exist.
func TestRun_SkillRoots_IdempotentAcrossReinit(t *testing.T) {
	// Not parallel — generateAliases writes to HOME.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}

	marker := filepath.Join(dir, ".agents", "skills", "existing-pack", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatalf("mkdir marker dir: %v", err)
	}
	const markerContent = "---\nname: existing-pack\ndescription: pre-existing\n---\n"
	if err := os.WriteFile(marker, []byte(markerContent), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("marker must survive a re-init: %v", err)
	}
	if string(got) != markerContent {
		t.Errorf("marker content = %q, want %q (must not be disturbed)", got, markerContent)
	}
	for _, rel := range []string{filepath.Join(".agents", "skills"), filepath.Join(".claude", "skills")} {
		if info, statErr := os.Stat(filepath.Join(dir, rel)); statErr != nil || !info.IsDir() {
			t.Errorf("%s must still exist as a directory after re-init: %v", rel, statErr)
		}
	}
}
