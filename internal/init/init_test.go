// Tests for: init package — zcp init subcommand.
package init_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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
// Run writes the WHOLE guided-skill subtree (router + phase files) and the
// guided block into AGENTS.md.
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

	skill, err := os.ReadFile(filepath.Join(dir, ".claude", "skills", "guided", "SKILL.md"))
	if err != nil {
		t.Fatalf("guided SKILL.md not written: %v", err)
	}
	if !strings.Contains(string(skill), "name: guided") {
		t.Errorf("guided SKILL.md missing expected content:\n%s", skill)
	}

	// Every embedded subtree file (router + phases/*.md) must land on disk.
	tree, err := content.ReadGuidedSkillTree()
	if err != nil {
		t.Fatalf("ReadGuidedSkillTree: %v", err)
	}
	if len(tree) < 2 {
		t.Fatalf("expected a multi-file guided subtree, got %d files", len(tree))
	}
	for _, f := range tree {
		p := filepath.Join(dir, ".claude", "skills", "guided", filepath.FromSlash(f.RelPath))
		got, readErr := os.ReadFile(p)
		if readErr != nil {
			t.Errorf("guided skill file %q not materialized: %v", f.RelPath, readErr)
			continue
		}
		if string(got) != f.Content {
			t.Errorf("guided skill file %q content drifted from the embedded template", f.RelPath)
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
	phasesDir := filepath.Join(dir, ".claude", "skills", "guided", "phases")
	if _, err := os.Stat(phasesDir); err != nil {
		t.Fatalf("phases dir should exist under guided: %v", err)
	}

	// Then: guided off → whole dir gone.
	if err := content.SetGuided(dir, false); err != nil {
		t.Fatalf("SetGuided(off): %v", err)
	}
	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run() (guided off): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "guided")); !os.IsNotExist(err) {
		t.Error("guided skill dir must be fully removed when guided toggles off")
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

	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "guided", "SKILL.md")); !os.IsNotExist(err) {
		t.Error("guided SKILL.md must not be written when guided is off")
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

	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "guided", "SKILL.md")); !os.IsNotExist(err) {
		t.Error("guided SKILL.md must NOT be written under authoring (mutual exclusion)")
	}
	agents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if strings.Contains(string(agents), "## Guided mode (user-only)") {
		t.Error("AGENTS.md must NOT carry guided block under authoring (mutual exclusion)")
	}
}
