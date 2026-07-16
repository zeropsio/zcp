package init_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/init/adapters"
	"github.com/zeropsio/zcp/internal/runtime"
)

// setupContainerTest sets common test overrides for container init tests.
func setupContainerTest(t *testing.T) {
	t.Helper()
	vsDir := t.TempDir()
	zcpinit.SetVSCodeWorkDir(vsDir)
	t.Cleanup(func() { zcpinit.ResetVSCodeWorkDir() })
}

// TestContainerSteps_NoGitSetup locks GLC-4 — the ZCP service container
// performs no git-related setup. Global `git config --global` was the
// only inline-git surface containerSteps ever exposed; it was removed
// because its only consumer (zcp sync recipe push-app) runs on the
// developer's laptop, not inside the ZCP service container. If this test
// fails, something has re-added `git` invocations to containerSteps —
// verify the new consumer actually exists AS a Zerops-service workflow
// before accepting the change.
func TestContainerSteps_NoGitSetup(t *testing.T) {
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	setupContainerTest(t)

	var executed [][]string
	zcpinit.SetCommandRunner(func(name string, args ...string) error {
		executed = append(executed, append([]string{name}, args...))
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	if err := zcpinit.Run(dir, runtime.Info{InContainer: true}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	for _, cmd := range executed {
		if len(cmd) > 0 && cmd[0] == "git" {
			t.Errorf("containerSteps must not invoke git (GLC-4): saw %v", cmd)
		}
	}
}

func TestContainerSteps_ClaudeConfigs(t *testing.T) {
	// Not parallel — mutates HOME env var and commandRunner.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	setupContainerTest(t)

	zcpinit.SetCommandRunner(func(_ string, _ ...string) error { return nil })
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		contains string
	}{
		{"claude.json exists", filepath.Join(homeDir, ".claude.json"), "hasCompletedOnboarding"},
		{"claude.json theme", filepath.Join(homeDir, ".claude.json"), "dark"},
		{"claude.json has global MCP", filepath.Join(homeDir, ".claude.json"), "mcpServers"},
		{"claude.json has zcp serve", filepath.Join(homeDir, ".claude.json"), "zcp"},
		{"settings.json exists", filepath.Join(homeDir, ".claude", "settings.json"), "skipDangerousModePermissionPrompt"},
		{"settings.json effort max", filepath.Join(homeDir, ".claude", "settings.json"), `"effortLevel":"max"`},
		{"settings.json long transcript retention", filepath.Join(homeDir, ".claude", "settings.json"), `"cleanupPeriodDays":3650`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read %s: %v", tt.path, err)
			}
			if !strings.Contains(string(data), tt.contains) {
				t.Errorf("%s should contain %q, got: %s", tt.path, tt.contains, data)
			}
		})
	}
}

// TestContainerSteps_ClaudeConfigs_ProjectEntry locks the contract that
// .claude.json always carries a projects[vsCodeWorkDir] entry with
// trust+onboarding pre-accepted, independent of auth mode. Without this,
// the Claude CLI prompts on first interactive launch and the VS Code
// Claude extension's first-run flow can short-circuit before reaching
// the API-key approval path.
func TestContainerSteps_ClaudeConfigs_ProjectEntry(t *testing.T) {
	dir := t.TempDir()
	homeDir := t.TempDir()
	vsWorkDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("ANTHROPIC_API_KEY", "")
	zcpinit.SetVSCodeWorkDir(vsWorkDir)
	t.Cleanup(func() { zcpinit.ResetVSCodeWorkDir() })

	zcpinit.SetCommandRunner(func(_ string, _ ...string) error { return nil })
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	if err := zcpinit.Run(dir, runtime.Info{InContainer: true}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".claude.json"))
	if err != nil {
		t.Fatalf("read .claude.json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse .claude.json: %v\nraw: %s", err, data)
	}

	projects, ok := got["projects"].(map[string]any)
	if !ok {
		t.Fatalf(".claude.json missing top-level projects map; got: %s", data)
	}
	entry, ok := projects[vsWorkDir].(map[string]any)
	if !ok {
		t.Fatalf("projects[%q] missing or wrong shape; projects=%v", vsWorkDir, projects)
	}

	checks := []struct {
		field string
		want  any
	}{
		{"hasTrustDialogAccepted", true},
		{"hasCompletedProjectOnboarding", true},
		{"lastGracefulShutdown", true},
		{"hasClaudeMdExternalIncludesApproved", false},
		{"hasClaudeMdExternalIncludesWarningShown", false},
	}
	for _, c := range checks {
		if got := entry[c.field]; got != c.want {
			t.Errorf("projects[%q].%s = %v, want %v", vsWorkDir, c.field, got, c.want)
		}
	}
	if _, ok := entry["allowedTools"].([]any); !ok {
		t.Errorf("projects[%q].allowedTools should be a JSON array, got %T", vsWorkDir, entry["allowedTools"])
	}
}

// TestContainerSteps_ClaudeConfigs_APIKeyApproved locks the contract that
// when ANTHROPIC_API_KEY is set at init time, .claude.json carries
// customApiKeyResponses.approved = [last 20 chars of the key] and
// rejected = []. Without this, the VS Code Claude extension treats the
// container as unauthenticated and shows its subscription/API-key entry
// landing screen, overriding zcp init's setup.
func TestContainerSteps_ClaudeConfigs_APIKeyApproved(t *testing.T) {
	dir := t.TempDir()
	homeDir := t.TempDir()
	vsWorkDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-fake-"+strings.Repeat("a", 20))
	zcpinit.SetVSCodeWorkDir(vsWorkDir)
	t.Cleanup(func() { zcpinit.ResetVSCodeWorkDir() })

	zcpinit.SetCommandRunner(func(_ string, _ ...string) error { return nil })
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	if err := zcpinit.Run(dir, runtime.Info{InContainer: true}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".claude.json"))
	if err != nil {
		t.Fatalf("read .claude.json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse .claude.json: %v\nraw: %s", err, data)
	}

	resp, ok := got["customApiKeyResponses"].(map[string]any)
	if !ok {
		t.Fatalf(".claude.json missing customApiKeyResponses; got: %s", data)
	}

	approved, ok := resp["approved"].([]any)
	if !ok {
		t.Fatalf("customApiKeyResponses.approved missing or wrong shape; resp=%v", resp)
	}
	if len(approved) != 1 {
		t.Fatalf("customApiKeyResponses.approved = %v, want exactly one entry", approved)
	}
	wantLast20 := strings.Repeat("a", 20)
	if got, _ := approved[0].(string); got != wantLast20 {
		t.Errorf("customApiKeyResponses.approved[0] = %q, want %q (last 20 chars of key)", got, wantLast20)
	}

	rejected, ok := resp["rejected"].([]any)
	if !ok {
		t.Fatalf("customApiKeyResponses.rejected missing or wrong shape; resp=%v", resp)
	}
	if len(rejected) != 0 {
		t.Errorf("customApiKeyResponses.rejected = %v, want []", rejected)
	}
}

// TestContainerSteps_ClaudeConfigs_NoAPIKey locks the contract that
// customApiKeyResponses is OMITTED when ANTHROPIC_API_KEY is not set —
// OAuth/Subscription mode users must not get a phantom approval entry
// pre-written that would shadow their own auth flow.
func TestContainerSteps_ClaudeConfigs_NoAPIKey(t *testing.T) {
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("ANTHROPIC_API_KEY", "")
	setupContainerTest(t)

	zcpinit.SetCommandRunner(func(_ string, _ ...string) error { return nil })
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	if err := zcpinit.Run(dir, runtime.Info{InContainer: true}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".claude.json"))
	if err != nil {
		t.Fatalf("read .claude.json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse .claude.json: %v\nraw: %s", err, data)
	}
	if _, present := got["customApiKeyResponses"]; present {
		t.Errorf("customApiKeyResponses should be absent without ANTHROPIC_API_KEY; got: %s", data)
	}
}

func TestContainerSteps_VSCode_Enabled(t *testing.T) {
	// Not parallel — mutates HOME and ZCP_VSCODE env vars.
	dir := t.TempDir()
	homeDir := t.TempDir()
	vsWorkDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("ZCP_VSCODE", "true")
	zcpinit.SetVSCodeWorkDir(vsWorkDir)
	t.Cleanup(func() { zcpinit.ResetVSCodeWorkDir() })

	var extensionInstalled bool
	zcpinit.SetCommandRunner(func(name string, args ...string) error {
		if name == "code-server" && len(args) > 0 && args[0] == "--install-extension" {
			extensionInstalled = true
		}
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	settingsPath := filepath.Join(homeDir, ".local", "share", "code-server", "User", "settings.json")
	bootstrapDir := filepath.Join(homeDir, ".local", "share", "code-server", "extensions", "zcp-bootstrap-"+adapters.BootstrapExtVersion)
	indexPath := filepath.Join(homeDir, ".local", "share", "code-server", "extensions", "extensions.json")

	tests := []struct {
		name     string
		path     string
		contains string
	}{
		{"vscode settings theme", settingsPath, "Default Dark+"},
		{"vscode settings bypass mode", settingsPath, "bypassPermissions"},
		{"vscode settings panel location", settingsPath, "\"panel\""},
		{"vscode settings hide secondary sidebar", settingsPath, "secondarySideBar"},
		{"vscode terminals", filepath.Join(vsWorkDir, ".vscode", "terminals.json"), "Claude Terminal"},
		{"bootstrap package.json", filepath.Join(bootstrapDir, "package.json"), "zcp-bootstrap"},
		{"bootstrap extension.js", filepath.Join(bootstrapDir, "extension.js"), "claude-vscode.editor.open"},
		{"extensions.json registers bootstrap", indexPath, "zerops.zcp-bootstrap"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read %s: %v", tt.path, err)
			}
			if !strings.Contains(string(data), tt.contains) {
				t.Errorf("%s should contain %q, got: %s", tt.path, tt.contains, data)
			}
		})
	}

	if !extensionInstalled {
		t.Error("expected code-server --install-extension to be called")
	}
}

// TestContainerSteps_VSCode_AgentLauncher_LiveNoBakedConfig locks that the
// agent launcher is purely runtime-resolved: even with ZCP_AGENT_TYPES set,
// `zcp init` writes NO zcp-launcher.json — the bootstrap extension reads the
// agent set live from the zembed env store at activation/watch time, so there
// is no baked config to drift or go stale across restarts.
func TestContainerSteps_VSCode_AgentLauncher_LiveNoBakedConfig(t *testing.T) {
	// Not parallel — mutates HOME, ZCP_VSCODE, ZCP_AGENT_TYPES.
	dir := t.TempDir()
	homeDir := t.TempDir()
	vsWorkDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("ZCP_VSCODE", "true")
	t.Setenv("ZCP_AGENT_TYPES", "claude-code, codex, grok")
	zcpinit.SetVSCodeWorkDir(vsWorkDir)
	t.Cleanup(func() { zcpinit.ResetVSCodeWorkDir() })
	zcpinit.SetCommandRunner(func(_ string, _ ...string) error { return nil })
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	if err := zcpinit.Run(dir, runtime.Info{InContainer: true}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	bootstrapDir := filepath.Join(homeDir, ".local", "share", "code-server", "extensions", "zcp-bootstrap-"+adapters.BootstrapExtVersion)
	if _, err := os.Stat(filepath.Join(bootstrapDir, "zcp-launcher.json")); !os.IsNotExist(err) {
		t.Errorf("expected NO baked zcp-launcher.json (launcher is live-resolved), stat err = %v", err)
	}
	// The installed extension carries the live launcher logic.
	ext, err := os.ReadFile(filepath.Join(bootstrapDir, "extension.js"))
	if err != nil {
		t.Fatalf("read extension.js: %v", err)
	}
	for _, marker := range []string{"ZCP_AGENT_TYPES", "/etc/zerops-zembed", "fs.watch"} {
		if !strings.Contains(string(ext), marker) {
			t.Errorf("installed extension.js missing live-launcher marker %q", marker)
		}
	}
	// The activity-bar entry point: logo asset installed + package.json declares it.
	if _, err := os.Stat(filepath.Join(bootstrapDir, "logo.svg")); err != nil {
		t.Errorf("expected logo.svg installed for activity-bar icon: %v", err)
	}
	pkg, err := os.ReadFile(filepath.Join(bootstrapDir, "package.json"))
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}
	if !strings.Contains(string(pkg), "zcpLauncher") {
		t.Errorf("installed package.json missing activity-bar container (zcpLauncher)")
	}
}

// TestContainerSteps_VSCode_Bootstrap_Idempotent locks the contract that
// re-running zcp init does not duplicate the zcp-bootstrap entry in
// extensions.json and preserves its installedTimestamp. Without this,
// repeat container provisioning (zcli replay, retry on failure) would
// either grow the index unboundedly or churn the timestamp every run.
func TestContainerSteps_VSCode_Bootstrap_Idempotent(t *testing.T) {
	dir := t.TempDir()
	homeDir := t.TempDir()
	vsWorkDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("ZCP_VSCODE", "true")
	zcpinit.SetVSCodeWorkDir(vsWorkDir)
	t.Cleanup(func() { zcpinit.ResetVSCodeWorkDir() })

	zcpinit.SetCommandRunner(func(_ string, _ ...string) error { return nil })
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	rt := runtime.Info{InContainer: true}

	if err := zcpinit.Run(dir, rt); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	indexPath := filepath.Join(homeDir, ".local", "share", "code-server", "extensions", "extensions.json")
	first, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index after first run: %v", err)
	}

	var firstEntries []map[string]any
	if err := json.Unmarshal(first, &firstEntries); err != nil {
		t.Fatalf("parse index after first run: %v", err)
	}
	firstTimestamp := bootstrapTimestamp(t, firstEntries)
	if firstTimestamp == 0 {
		t.Fatalf("first run did not record installedTimestamp for bootstrap; index=%s", first)
	}

	if err := zcpinit.Run(dir, rt); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}

	second, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index after second run: %v", err)
	}
	var secondEntries []map[string]any
	if err := json.Unmarshal(second, &secondEntries); err != nil {
		t.Fatalf("parse index after second run: %v", err)
	}

	var bootstrapCount int
	for _, e := range secondEntries {
		if entryID(e) == "zerops.zcp-bootstrap" {
			bootstrapCount++
		}
	}
	if bootstrapCount != 1 {
		t.Fatalf("expected exactly one bootstrap entry, got %d; index=%s", bootstrapCount, second)
	}

	secondTimestamp := bootstrapTimestamp(t, secondEntries)
	if secondTimestamp != firstTimestamp {
		t.Errorf("installedTimestamp churned across re-runs: first=%d second=%d (idempotency broken)", firstTimestamp, secondTimestamp)
	}
}

// TestContainerSteps_VSCode_Bootstrap_PreservesExistingEntries locks that
// upsertExtensionsIndex does not lose unknown fields on entries it did not
// author. code-server's --install-extension can write entries with shapes
// (e.g. an "__metadata" field on .vsix imports) that we do not model
// statically; round-tripping through []map[string]any preserves them.
func TestContainerSteps_VSCode_Bootstrap_PreservesExistingEntries(t *testing.T) {
	dir := t.TempDir()
	homeDir := t.TempDir()
	vsWorkDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("ZCP_VSCODE", "true")
	zcpinit.SetVSCodeWorkDir(vsWorkDir)
	t.Cleanup(func() { zcpinit.ResetVSCodeWorkDir() })

	zcpinit.SetCommandRunner(func(_ string, _ ...string) error { return nil })
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	indexPath := filepath.Join(homeDir, ".local", "share", "code-server", "extensions", "extensions.json")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	preseed := `[{"identifier":{"id":"anthropic.claude-code"},"version":"2.1.120","relativeLocation":"anthropic.claude-code-2.1.120","metadata":{"installedTimestamp":1777170768624,"pinned":true,"source":"vsix","customField":"keep-me"}}]`
	if err := os.WriteFile(indexPath, []byte(preseed), 0644); err != nil {
		t.Fatalf("seed index: %v", err)
	}

	if err := zcpinit.Run(dir, runtime.Info{InContainer: true}); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	out, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var entries []map[string]any
	if err := json.Unmarshal(out, &entries); err != nil {
		t.Fatalf("parse index: %v", err)
	}

	var sawClaude, sawBootstrap bool
	for _, e := range entries {
		switch entryID(e) {
		case "anthropic.claude-code":
			sawClaude = true
			md, ok := e["metadata"].(map[string]any)
			if !ok {
				t.Errorf("claude-code metadata missing or wrong shape: %v", e["metadata"])
				break
			}
			if got, _ := md["customField"].(string); got != "keep-me" {
				t.Errorf("claude-code metadata.customField lost across upsert: got %q want %q", got, "keep-me")
			}
			if ts, _ := md["installedTimestamp"].(float64); int64(ts) != 1777170768624 {
				t.Errorf("claude-code installedTimestamp churned: got %v", md["installedTimestamp"])
			}
		case "zerops.zcp-bootstrap":
			sawBootstrap = true
		}
	}
	if !sawClaude {
		t.Error("claude-code entry was dropped during upsert")
	}
	if !sawBootstrap {
		t.Error("bootstrap entry was not added to existing index")
	}
}

// bootstrapTimestamp finds the zerops.zcp-bootstrap entry in entries and
// returns its metadata.installedTimestamp as int64 (0 if missing).
func bootstrapTimestamp(t *testing.T, entries []map[string]any) int64 {
	t.Helper()
	for _, e := range entries {
		if entryID(e) != "zerops.zcp-bootstrap" {
			continue
		}
		md, ok := e["metadata"].(map[string]any)
		if !ok {
			return 0
		}
		t, _ := md["installedTimestamp"].(float64)
		return int64(t)
	}
	return 0
}

// entryID extracts identifier.id from a generic extensions.json entry.
func entryID(e map[string]any) string {
	id, ok := e["identifier"].(map[string]any)
	if !ok {
		return ""
	}
	s, _ := id["id"].(string)
	return s
}

func TestContainerSteps_VSCode_Disabled(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	setupContainerTest(t)
	// ZCP_VSCODE not set — VS Code steps should be skipped.

	zcpinit.SetCommandRunner(func(_ string, _ ...string) error { return nil })
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	err := zcpinit.Run(dir, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	vscodePath := filepath.Join(homeDir, ".local", "share", "code-server", "User", "settings.json")
	if _, err := os.Stat(vscodePath); !os.IsNotExist(err) {
		t.Error("VS Code settings should not be created when ZCP_VSCODE is not true")
	}
}

func TestContainerSteps_Idempotent(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	setupContainerTest(t)

	zcpinit.SetCommandRunner(func(_ string, _ ...string) error { return nil })
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	rt := runtime.Info{InContainer: true}

	if err := zcpinit.Run(dir, rt); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	if err := zcpinit.Run(dir, rt); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}

	// Claude config should still be valid after two runs.
	data, err := os.ReadFile(filepath.Join(homeDir, ".claude.json"))
	if err != nil {
		t.Fatalf("read .claude.json: %v", err)
	}
	if !strings.Contains(string(data), "hasCompletedOnboarding") {
		t.Error(".claude.json should contain hasCompletedOnboarding after second run")
	}
}

func TestContainerSteps_SkippedOutsideContainer(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	setupContainerTest(t)

	var gitCalled bool
	zcpinit.SetCommandRunner(func(name string, _ ...string) error {
		if name == "git" {
			gitCalled = true
		}
		return nil
	})
	t.Cleanup(func() { zcpinit.ResetCommandRunner() })

	err := zcpinit.Run(dir, runtime.Info{InContainer: false})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if gitCalled {
		t.Error("git should not be called outside container")
	}

	claudePath := filepath.Join(homeDir, ".claude.json")
	if _, err := os.Stat(claudePath); !os.IsNotExist(err) {
		t.Error(".claude.json should not be created outside container")
	}
}
