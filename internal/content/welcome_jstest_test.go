package content

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestWelcomeJS runs the welcome webview host's node:test suite
// (internal/content/welcomejs/) against the REAL templates embedded from
// internal/content/templates — the executable proof behind W3 (dark/lazy
// load, docs/spec-welcome-mode.md §1) and the panel/message-allowlist
// contracts (§8 W-SEC). node is a required CI dependency for this package;
// a missing `node` degrades to a skip locally, but CI (CI env var set) must
// never silently skip this suite — a missing binary there is a hard
// failure.
//
// Files are passed explicitly (globbed) rather than as a bare directory
// argument: on the node versions this was verified against, `node --test
// <dir>` tries to require() the directory itself instead of discovering
// *.test.js files inside it and fails with MODULE_NOT_FOUND, while an
// explicit file list works reliably.
func TestWelcomeJS(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("node not on PATH in CI — the welcome JS suite must not be silently skipped: %v", err)
		}
		t.Skip("node not on PATH — skipping welcome JS suite")
	}

	dir, err := filepath.Abs("welcomejs")
	if err != nil {
		t.Fatalf("resolve welcomejs dir: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.test.js"))
	if err != nil {
		t.Fatalf("glob welcomejs test files: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no *.test.js files found in %s", dir)
	}

	//nolint:gosec // G204: fixed binary name, args are our own globbed test files
	cmd := exec.CommandContext(context.Background(), "node", append([]string{"--test"}, files...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("node --test %s failed:\n%s", dir, out)
		return
	}
	t.Logf("%s", out)
}
