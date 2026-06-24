package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

// Codex review found that `zcp serve` could rewrite a pre-upgrade
// CLAUDE.md (full body, no AGENTS.md present) into a thin @AGENTS.md
// wrapper — leaving the include pointing at a nonexistent file. This
// test pins the safeguard: if AGENTS.md is missing, the CLAUDE.md
// wrapper refresh is skipped (zcp init owns the migration; serve must
// not partially-migrate).
func TestRefreshAgentContext_PreUpgradeCLAUDEmdWithoutAgentsMD_LeftUntouched(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	claudePath := filepath.Join(dir, "CLAUDE.md")

	// Pre-upgrade shape: CLAUDE.md has full ZCP-managed body (a stale
	// version that would normally trigger refresh) + REFLOG; AGENTS.md
	// does not exist.
	preUpgradeClaude := agentMarkerBegin + "\n" +
		"# Zerops\n\nSTALE PRE-UPGRADE BODY\n" +
		agentMarkerEnd + "\n" +
		"\n<!-- ZEROPS:REFLOG -->\n" +
		"### 2026-04-19 — Bootstrap: app\n- **Session:** sess-1\n" +
		"<!-- /ZEROPS:REFLOG -->\n"
	if err := os.WriteFile(claudePath, []byte(preUpgradeClaude), 0o644); err != nil {
		t.Fatal(err)
	}

	rt := runtime.Info{InContainer: true, ServiceName: "zcp"}
	agentsChanged, claudeChanged, err := RefreshAgentContext(agentsPath, claudePath, rt, false)
	if err != nil {
		t.Fatalf("RefreshAgentContext: %v", err)
	}

	if agentsChanged {
		t.Error("AGENTS.md must not be created by serve refresh (zcp init owns first-write)")
	}
	if claudeChanged {
		t.Error("CLAUDE.md must NOT be refreshed when AGENTS.md is missing — would orphan the @AGENTS.md include")
	}

	got, _ := os.ReadFile(claudePath)
	if string(got) != preUpgradeClaude {
		t.Errorf("CLAUDE.md content changed despite safeguard:\nbefore: %q\nafter:  %q", preUpgradeClaude, got)
	}
	if _, err := os.Stat(agentsPath); !os.IsNotExist(err) {
		t.Errorf("AGENTS.md should still be missing (zcp init owns creation); stat err=%v", err)
	}
}

// Once AGENTS.md exists, the wrapper refresh CAN run because the
// @AGENTS.md include resolves.
func TestRefreshAgentContext_AgentsMDPresent_RefreshesCLAUDEWrapper(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	claudePath := filepath.Join(dir, "CLAUDE.md")

	rt := runtime.Info{InContainer: true, ServiceName: "zcp"}
	body, _ := BuildAgentsMD(rt, false)
	if err := os.WriteFile(agentsPath, []byte(wrapManagedBlock(body)), 0o644); err != nil {
		t.Fatal(err)
	}
	// CLAUDE.md has stale wrapper content inside markers — should refresh.
	staleClaude := agentMarkerBegin + "\nOLD WRAPPER\n" + agentMarkerEnd + "\n"
	if err := os.WriteFile(claudePath, []byte(staleClaude), 0o644); err != nil {
		t.Fatal(err)
	}

	agentsChanged, claudeChanged, err := RefreshAgentContext(agentsPath, claudePath, rt, false)
	if err != nil {
		t.Fatalf("RefreshAgentContext: %v", err)
	}
	if agentsChanged {
		t.Error("current AGENTS.md should not refresh")
	}
	if !claudeChanged {
		t.Error("stale CLAUDE.md wrapper should refresh when AGENTS.md exists")
	}
	got, _ := os.ReadFile(claudePath)
	if !strings.Contains(string(got), "@AGENTS.md") {
		t.Errorf("CLAUDE.md wrapper missing @AGENTS.md after refresh: %s", got)
	}
}
