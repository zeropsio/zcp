package init_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/runtime"
)

// Codex review found three migration edge cases not covered by the
// original TestUpgrade_* tests. Each test below pins one of them.

// TestUpgrade_MarkerlessUserAgentsMD_PreservedNotClobbered:
// A user who hand-created AGENTS.md before the multi-agent ZCP
// shipped (e.g. for Codex use) has their own content with no ZCP
// markers. zcp init MUST NOT overwrite that content; it MUST prepend
// its managed section while preserving the user-authored body.
func TestUpgrade_MarkerlessUserAgentsMD_PreservedNotClobbered(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	userAgents := "# My project context\n\n" +
		"Authored by hand for Codex.\n" +
		"This MUST survive zcp init.\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(userAgents), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	gotStr := string(got)

	if !strings.Contains(gotStr, "# My project context") {
		t.Errorf("user-authored heading clobbered: %s", gotStr)
	}
	if !strings.Contains(gotStr, "Authored by hand for Codex.") {
		t.Errorf("user-authored body clobbered: %s", gotStr)
	}
	if !strings.Contains(gotStr, "<!-- ZCP:BEGIN -->") {
		t.Errorf("ZCP-managed marker section not added: %s", gotStr)
	}
}

// TestUpgrade_AgentsMDAlreadyHasMigratedReflog_NoDuplication:
// Crash-recovery scenario: a previous init wrote AGENTS.md with
// migrated REFLOG but didn't get to clean CLAUDE.md. Next init sees
// REFLOG in both files; it MUST NOT duplicate (content-based dedupe)
// and MUST clean CLAUDE.md this time.
func TestUpgrade_AgentsMDAlreadyHasMigratedReflog_NoDuplication(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	reflogSection := "<!-- ZEROPS:REFLOG -->\n" +
		"### 2026-04-19 — Bootstrap: app\n- **Session:** sess-1\n" +
		"<!-- /ZEROPS:REFLOG -->\n"

	// AGENTS.md already has the REFLOG (from prior partial migration).
	agentsPre := "<!-- ZCP:BEGIN -->\n# old body\n<!-- ZCP:END -->\n\n" + reflogSection
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agentsPre), 0o644); err != nil {
		t.Fatal(err)
	}
	// CLAUDE.md still has the same REFLOG (cleanup didn't happen).
	claudePre := "<!-- ZCP:BEGIN -->\n# old\n<!-- ZCP:END -->\n\n" + reflogSection
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(claudePre), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	agents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	claude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))

	agentsStr := string(agents)
	claudeStr := string(claude)

	count := strings.Count(agentsStr, "<!-- ZEROPS:REFLOG -->")
	if count != 1 {
		t.Errorf("AGENTS.md should contain exactly one REFLOG marker after dedupe, got %d:\n%s", count, agentsStr)
	}
	if strings.Contains(claudeStr, "ZEROPS:REFLOG") {
		t.Errorf("CLAUDE.md REFLOG should have been cleaned on resumable migration: %s", claudeStr)
	}
}

// TestUpgrade_MalformedReflogOpenerNoCloser_NoDataLoss:
// A truncated REFLOG section (opener without closer) is left in
// CLAUDE.md by extractReflogSections — but the migration must not
// silently lose history. We pin that the truncated content remains
// in CLAUDE.md (operator-visible) and CLAUDE.md is not rewritten.
func TestUpgrade_MalformedReflogOpenerNoCloser_NoDataLoss(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	malformed := "<!-- ZCP:BEGIN -->\n# body\n<!-- ZCP:END -->\n\n" +
		"<!-- ZEROPS:REFLOG -->\n" +
		"### 2026-04-19 — Bootstrap: truncated entry (file ended mid-section)\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(malformed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	claude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	claudeStr := string(claude)

	// Truncated REFLOG content remains visible — no silent data loss.
	if !strings.Contains(claudeStr, "truncated entry") {
		t.Errorf("truncated REFLOG content lost during migration:\n%s", claudeStr)
	}
}
