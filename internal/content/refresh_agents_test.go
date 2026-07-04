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

	a, c, err := RefreshAgentContext(agents, claude, runtime.Info{InContainer: true, ServiceName: "zcp"}, false)
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
	a, c, err := RefreshAgentContext(agentsPath, claudePath, rt, false)
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
	body, _ := BuildAgentsMD(rt, false)
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

	a, c, err := RefreshAgentContext(agentsPath, claudePath, rt, false)
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
	body, _ := BuildAgentsMD(rt, false)
	if err := os.WriteFile(agentsPath, []byte(wrapManagedBlock(body)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudePath, []byte(wrapManagedBlock(BuildClaudeWrapper())), 0o644); err != nil {
		t.Fatal(err)
	}

	a, c, err := RefreshAgentContext(agentsPath, claudePath, rt, false)
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

	a, _, err := RefreshAgentContext(agentsPath, claudePath, runtime.Info{InContainer: true, ServiceName: "zcp"}, false)
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

	a, _, err := RefreshAgentContext(agentsPath, claudePath, runtime.Info{InContainer: true, ServiceName: "zcp"}, false)
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

// TestRefreshAgentContext_GuidedParam pins that the serve-time refresh carries
// the guided block iff its caller passes guided=true. The caller (the MCP
// server) resolves guided from the committed project config (.zcp.yaml) — so a
// `zcp init --guided` install keeps the block across serve, and a plain install
// (guided=false) stays out.
func TestRefreshAgentContext_GuidedParam(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	claudePath := filepath.Join(dir, "CLAUDE.md")

	// Seed a managed AGENTS.md so refresh acts (refresh is incremental-only).
	seed := agentMarkerBegin + "\n# Zerops\n\nseed\n" + agentMarkerEnd + "\n"
	if err := os.WriteFile(agentsPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := runtime.Info{InContainer: false}

	// guided=false → refresh leaves out the guided block.
	if _, _, err := RefreshAgentContext(agentsPath, claudePath, rt, false); err != nil {
		t.Fatalf("refresh (guided off): %v", err)
	}
	if body, _ := os.ReadFile(agentsPath); strings.Contains(string(body), "## Guided mode (user-only)") {
		t.Error("guided block present when guided=false")
	}

	// guided=true → refresh adds the guided block.
	if _, _, err := RefreshAgentContext(agentsPath, claudePath, rt, true); err != nil {
		t.Fatalf("refresh (guided on): %v", err)
	}
	if body, _ := os.ReadFile(agentsPath); !strings.Contains(string(body), "## Guided mode (user-only)") {
		t.Error("guided block missing when guided=true")
	}
}

// TestRefreshAgentContext_MidLineMarkerMention_ProseIntact pins the
// line-anchored marker contract: a literal marker string appearing
// MID-LINE in prose (e.g. an agent documenting ZCP behavior in its
// notes) is content, not structure. The refresh must locate the managed
// block only via markers that occupy an entire line — otherwise the
// mention is treated as the block boundary and the prose is cut
// mid-sentence, leaving corrupted lines ending in `-->` (real-user
// incident, 2026-07-04).
func TestRefreshAgentContext_MidLineMarkerMention_ProseIntact(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	claudePath := filepath.Join(dir, "CLAUDE.md")

	rt := runtime.Info{InContainer: true, ServiceName: "zcp"}
	body, _ := BuildAgentsMD(rt, false)
	if err := os.WriteFile(agentsPath, []byte(wrapManagedBlock(body)), 0o644); err != nil {
		t.Fatal(err)
	}

	mentionBegin := "ZCP wraps its section in <!-- ZCP:BEGIN --> and you should not edit inside it."
	mentionEnd := "It ends with <!-- ZCP:END --> so everything between is machine-owned."
	stale := "# User notes at top\n" +
		mentionBegin + "\n" +
		"Another precious user line here.\n" +
		mentionEnd + "\n" +
		"\n" + agentMarkerBegin + "\nOLD WRAPPER\n" + agentMarkerEnd + "\n"
	if err := os.WriteFile(claudePath, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	_, c, err := RefreshAgentContext(agentsPath, claudePath, rt, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c {
		t.Error("stale CLAUDE.md wrapper must still refresh")
	}

	got, _ := os.ReadFile(claudePath)
	gotStr := string(got)
	for _, want := range []string{mentionBegin, mentionEnd, "Another precious user line here."} {
		if !strings.Contains(gotStr, want) {
			t.Errorf("prose line with mid-line marker mention was cut:\nmissing %q\ngot:\n%s", want, gotStr)
		}
	}
	if strings.Contains(gotStr, "OLD WRAPPER") {
		t.Errorf("stale wrapper survived refresh:\n%s", gotStr)
	}
	if n := strings.Count(gotStr, "@AGENTS.md"); n != 1 {
		t.Errorf("wrapper body must appear exactly once, got %d:\n%s", n, gotStr)
	}
}

// TestRefreshAgentContext_OnlyMidLineMentions_NoOp pins that a file
// whose only marker occurrences are mid-line mentions has NO managed
// block — the refresh must not touch it at all.
func TestRefreshAgentContext_OnlyMidLineMentions_NoOp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	claudePath := filepath.Join(dir, "CLAUDE.md")

	rt := runtime.Info{InContainer: true, ServiceName: "zcp"}
	body, _ := BuildAgentsMD(rt, false)
	if err := os.WriteFile(agentsPath, []byte(wrapManagedBlock(body)), 0o644); err != nil {
		t.Fatal(err)
	}

	original := "# Notes\n" +
		"The block starts with <!-- ZCP:BEGIN --> and ends with <!-- ZCP:END --> markers.\n"
	if err := os.WriteFile(claudePath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	_, c, err := RefreshAgentContext(agentsPath, claudePath, rt, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c {
		t.Error("mid-line mentions are not a managed block; refresh must no-op")
	}
	got, _ := os.ReadFile(claudePath)
	if string(got) != original {
		t.Errorf("markerless file changed:\noriginal: %q\ngot:      %q", original, string(got))
	}
}
