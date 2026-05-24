package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

// RefreshAgentContext is the canonical refresh entrypoint after the
// multi-agent migration. The tests below pin its idempotence,
// migration-safety, and crash-resistance properties — the same
// invariants the old RefreshClaudeMD held.

func TestRefreshAgentContext_NoFiles_NoOp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agents := filepath.Join(dir, "AGENTS.md")
	claude := filepath.Join(dir, "CLAUDE.md")

	a, c, err := RefreshAgentContext(agents, claude, runtime.Info{InContainer: true, ServiceName: "zcp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a || c {
		t.Errorf("missing files should not trigger refresh; got agents=%v claude=%v", a, c)
	}
	if _, err := os.Stat(agents); !os.IsNotExist(err) {
		t.Error("AGENTS.md should not be created (zcp init owns first-write)")
	}
	if _, err := os.Stat(claude); !os.IsNotExist(err) {
		t.Error("CLAUDE.md should not be created (zcp init owns first-write)")
	}
}

func TestRefreshAgentContext_StaleAgentsMD_RefreshesAndPreservesReflog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	claudePath := filepath.Join(dir, "CLAUDE.md")

	stale := agentMarkerBegin + "\n" +
		"# Zerops\n\n" +
		"OLD AGENTS.md WORDING from a prior version.\n" +
		agentMarkerEnd + "\n" +
		"\n<!-- ZEROPS:REFLOG -->\n" +
		"- 2026-04-01: bootstrap appdev\n" +
		"- 2026-04-15: bootstrap apidev\n" +
		"<!-- /ZEROPS:REFLOG -->\n"
	if err := os.WriteFile(agentsPath, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	rt := runtime.Info{InContainer: true, ServiceName: "zcp"}
	a, c, err := RefreshAgentContext(agentsPath, claudePath, rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !a {
		t.Error("stale AGENTS.md must refresh")
	}
	if c {
		t.Error("missing CLAUDE.md must not be created by refresh")
	}

	got, _ := os.ReadFile(agentsPath)
	gotStr := string(got)

	if strings.Contains(gotStr, "OLD AGENTS.md WORDING") {
		t.Errorf("stale managed body survived refresh:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "ZCP control-plane container `zcp`") {
		t.Errorf("fresh template content not written:\n%s", gotStr)
	}
	for _, want := range []string{
		"<!-- ZEROPS:REFLOG -->",
		"2026-04-01: bootstrap appdev",
		"2026-04-15: bootstrap apidev",
	} {
		if !strings.Contains(gotStr, want) {
			t.Errorf("REFLOG entry %q lost during refresh:\n%s", want, gotStr)
		}
	}
}

func TestRefreshAgentContext_ClaudeWrapper_PreservesContentOutsideMarkers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	claudePath := filepath.Join(dir, "CLAUDE.md")

	// AGENTS.md already current — refresh should leave it alone.
	rt := runtime.Info{InContainer: true, ServiceName: "zcp"}
	body, _ := BuildAgentsMD(rt)
	if err := os.WriteFile(agentsPath, []byte(wrapManagedBlock(body)), 0o644); err != nil {
		t.Fatal(err)
	}

	// CLAUDE.md has stale wrapper + user-authored content outside markers.
	stale := agentMarkerBegin + "\n" +
		"OLD WRAPPER CONTENT\n" +
		agentMarkerEnd + "\n" +
		"\n## User notes\n" +
		"these are MY notes, do not touch\n"
	if err := os.WriteFile(claudePath, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	a, c, err := RefreshAgentContext(agentsPath, claudePath, rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a {
		t.Error("current AGENTS.md should not be touched on refresh")
	}
	if !c {
		t.Error("stale CLAUDE.md wrapper must be refreshed")
	}

	got, _ := os.ReadFile(claudePath)
	gotStr := string(got)
	if strings.Contains(gotStr, "OLD WRAPPER CONTENT") {
		t.Errorf("stale wrapper survived refresh:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "@AGENTS.md") {
		t.Errorf("CLAUDE.md wrapper missing @AGENTS.md include:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "## User notes") || !strings.Contains(gotStr, "these are MY notes") {
		t.Errorf("user content outside markers clobbered:\n%s", gotStr)
	}
}

func TestRefreshAgentContext_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	claudePath := filepath.Join(dir, "CLAUDE.md")

	rt := runtime.Info{InContainer: true, ServiceName: "zcp"}
	body, _ := BuildAgentsMD(rt)
	if err := os.WriteFile(agentsPath, []byte(wrapManagedBlock(body)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudePath, []byte(wrapManagedBlock(BuildClaudeWrapper())), 0o644); err != nil {
		t.Fatal(err)
	}

	a, c, err := RefreshAgentContext(agentsPath, claudePath, rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a || c {
		t.Errorf("matching content must not trigger refresh; got agents=%v claude=%v", a, c)
	}
}

func TestRefreshAgentContext_ReversedMarkers_NoCrash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	claudePath := filepath.Join(dir, "CLAUDE.md")

	reversed := agentMarkerEnd + "\nstray content\n" + agentMarkerBegin + "\nbody after begin\n"
	if err := os.WriteFile(agentsPath, []byte(reversed), 0o644); err != nil {
		t.Fatal(err)
	}

	a, _, err := RefreshAgentContext(agentsPath, claudePath, runtime.Info{InContainer: true, ServiceName: "zcp"})
	if err != nil {
		t.Fatalf("reversed-marker file must not return an error (got: %v)", err)
	}
	if a {
		t.Error("malformed marker layout must not trigger a rewrite")
	}

	got, _ := os.ReadFile(agentsPath)
	if string(got) != reversed {
		t.Errorf("reversed-marker file contents changed:\noriginal: %q\ngot:      %q", reversed, string(got))
	}
}

func TestRefreshAgentContext_BeginOnly_NoCrash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	claudePath := filepath.Join(dir, "CLAUDE.md")

	beginOnly := agentMarkerBegin + "\nbody but no end marker\n"
	if err := os.WriteFile(agentsPath, []byte(beginOnly), 0o644); err != nil {
		t.Fatal(err)
	}

	a, _, err := RefreshAgentContext(agentsPath, claudePath, runtime.Info{InContainer: true, ServiceName: "zcp"})
	if err != nil {
		t.Fatalf("begin-only file must not return an error (got: %v)", err)
	}
	if a {
		t.Error("missing end marker must not trigger a rewrite")
	}
}

// Deprecated-alias coverage: RefreshClaudeMD (single-file refresh path
// for callers not yet migrated) still works against AGENTS.md-shaped
// content because it routes through BuildAgentsMD internally.
func TestRefreshClaudeMD_DeprecatedAlias_StillRefreshes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	stale := agentMarkerBegin + "\nOLD\n" + agentMarkerEnd + "\n"
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	refreshed, err := RefreshClaudeMD(path, runtime.Info{InContainer: true, ServiceName: "zcp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !refreshed {
		t.Error("stale managed section must refresh via deprecated alias too")
	}
}
