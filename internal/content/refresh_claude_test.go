package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

// TestRefreshClaudeMD_NoFile_NoOp pins the install-time boundary: when
// the project has not been `zcp init`-ed yet, RefreshClaudeMD does not
// create a CLAUDE.md from scratch. First-write is `zcp init`'s
// responsibility; the serve-time refresh is incremental only.
func TestRefreshClaudeMD_NoFile_NoOp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	refreshed, err := RefreshClaudeMD(path, runtime.Info{InContainer: true, ServiceName: "zcp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if refreshed {
		t.Error("missing file should not trigger refresh")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should not be created: stat err=%v", err)
	}
}

// TestRefreshClaudeMD_LegacyNoMarkers_LeavesAlone pins the migration
// boundary: a file that exists but lacks ZCP:BEGIN/END markers (legacy
// install shape pre-marker era) is left for `zcp init` to migrate. The
// serve-path has no anchor for an idempotent rewrite and must not
// guess.
func TestRefreshClaudeMD_LegacyNoMarkers_LeavesAlone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	original := "# my project notes\n\nsome content from before zcp\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	refreshed, err := RefreshClaudeMD(path, runtime.Info{InContainer: true, ServiceName: "zcp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if refreshed {
		t.Error("legacy file (no markers) must not be rewritten by serve-path refresh")
	}
	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Errorf("legacy file content changed:\noriginal: %q\ngot:      %q", original, string(got))
	}
}

// TestRefreshClaudeMD_Current_NoOp pins idempotence: when the on-disk
// managed section already matches the freshly composed body, RefreshClaudeMD
// returns (false, nil) without rewriting. Critical for the MCP startup
// path which fires this on every `zcp serve` invocation.
func TestRefreshClaudeMD_Current_NoOp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	rt := runtime.Info{InContainer: true, ServiceName: "zcp"}
	body, err := BuildClaudeMD(rt)
	if err != nil {
		t.Fatalf("BuildClaudeMD: %v", err)
	}
	block := claudeMarkerBegin + "\n" + strings.TrimRight(body, "\n") + "\n" + claudeMarkerEnd + "\n"
	if err := os.WriteFile(path, []byte(block), 0o644); err != nil {
		t.Fatal(err)
	}
	statBefore, _ := os.Stat(path)

	refreshed, err := RefreshClaudeMD(path, rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if refreshed {
		t.Error("matching content must not trigger refresh")
	}
	statAfter, _ := os.Stat(path)
	if statBefore.ModTime() != statAfter.ModTime() {
		t.Error("file was rewritten despite matching content (mtime changed)")
	}
}

// TestRefreshClaudeMD_ReversedMarkers_NoCrash pins the malformed-input
// guard: a user-edited or legacy CLAUDE.md whose ZCP:END appears before
// ZCP:BEGIN must NOT panic the MCP server during startup. Pre-fix the
// reversed-order case sliced text[beginIdx:endLineEnd] with low > high
// and crashed (Codex review of the audit fixes, high-severity finding).
// Bail out cleanly; `zcp init` is the right place to re-stamp such a
// file with proper markers.
func TestRefreshClaudeMD_ReversedMarkers_NoCrash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	// END marker physically before BEGIN marker — pathological but
	// possible after a hand-edit gone wrong.
	reversed := claudeMarkerEnd + "\nstray content\n" + claudeMarkerBegin + "\nbody after begin\n"
	if err := os.WriteFile(path, []byte(reversed), 0o644); err != nil {
		t.Fatal(err)
	}

	refreshed, err := RefreshClaudeMD(path, runtime.Info{InContainer: true, ServiceName: "zcp"})
	if err != nil {
		t.Fatalf("reversed-marker file must not return an error (got: %v)", err)
	}
	if refreshed {
		t.Error("malformed marker layout must not trigger a rewrite — `zcp init` owns repair")
	}

	got, _ := os.ReadFile(path)
	if string(got) != reversed {
		t.Errorf("reversed-marker file contents changed:\noriginal: %q\ngot:      %q", reversed, string(got))
	}
}

// TestRefreshClaudeMD_BeginOnly_NoCrash pins the missing-end guard:
// a file with only the begin marker (truncation, partial write) must
// not panic and must not rewrite — same rationale as the reversed case.
func TestRefreshClaudeMD_BeginOnly_NoCrash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	beginOnly := claudeMarkerBegin + "\nbody but no end marker\n"
	if err := os.WriteFile(path, []byte(beginOnly), 0o644); err != nil {
		t.Fatal(err)
	}

	refreshed, err := RefreshClaudeMD(path, runtime.Info{InContainer: true, ServiceName: "zcp"})
	if err != nil {
		t.Fatalf("begin-only file must not return an error (got: %v)", err)
	}
	if refreshed {
		t.Error("missing end marker must not trigger a rewrite")
	}
}

// TestRefreshClaudeMD_StaleMarked_Refreshes pins the actual G9 fix: a
// container with a stale CLAUDE.md from a previous zcp version (forbidden
// drift wording) gets refreshed on serve start. The trailing REFLOG
// section (outside the markers) must be preserved verbatim.
func TestRefreshClaudeMD_StaleMarked_Refreshes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	stale := claudeMarkerBegin + "\n" +
		"# Zerops\n\n" +
		"OLD WORDING that doesn't match the current template at all.\n" +
		claudeMarkerEnd + "\n" +
		"\n<!-- ZEROPS:REFLOG -->\n" +
		"- 2026-04-01: bootstrap appdev\n" +
		"- 2026-04-15: bootstrap apidev\n"
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	rt := runtime.Info{InContainer: true, ServiceName: "zcp"}
	refreshed, err := RefreshClaudeMD(path, rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !refreshed {
		t.Fatal("stale managed section must trigger refresh")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	gotStr := string(got)

	// Markers must remain.
	if !strings.Contains(gotStr, claudeMarkerBegin) || !strings.Contains(gotStr, claudeMarkerEnd) {
		t.Errorf("markers missing after refresh:\n%s", gotStr)
	}
	// Stale prose must be gone.
	if strings.Contains(gotStr, "OLD WORDING") {
		t.Errorf("stale managed section content survived refresh:\n%s", gotStr)
	}
	// Fresh template content must be present.
	if !strings.Contains(gotStr, "ZCP control-plane container `zcp`") {
		t.Errorf("fresh template content not written:\n%s", gotStr)
	}
	// REFLOG (outside markers) must be preserved verbatim.
	for _, want := range []string{
		"<!-- ZEROPS:REFLOG -->",
		"2026-04-01: bootstrap appdev",
		"2026-04-15: bootstrap apidev",
	} {
		if !strings.Contains(gotStr, want) {
			t.Errorf("REFLOG entry %q lost in refresh:\n%s", want, gotStr)
		}
	}
}
