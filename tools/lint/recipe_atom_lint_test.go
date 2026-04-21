package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunLint_ProductionTreeIsClean exercises runLint against the live
// atom tree and asserts zero violations. This is the regression floor
// — any commit that introduces an atom-tree violation fails here + at
// `make lint-local`. Fixture drift (new atoms added outside C-13's
// rules) is caught at the CI layer.
func TestRunLint_ProductionTreeIsClean(t *testing.T) {
	t.Parallel()
	// Walk up from the package dir to the repo root so the relative
	// atomRoot resolves regardless of `go test`'s cwd.
	root := findRepoRoot(t)
	violations, scanned, err := runLint(filepath.Join(root, atomRoot))
	if err != nil {
		t.Fatalf("runLint: %v", err)
	}
	if scanned == 0 {
		t.Fatal("scanned 0 files — atomRoot resolution likely wrong")
	}
	if len(violations) != 0 {
		t.Fatalf("production atom tree has %d violations:\n%s", len(violations), strings.Join(violations, "\n"))
	}
}

// TestRunLint_FiresOnKnownViolations builds a synthetic fixture tree
// that trips every rule (B-1..B-5, B-7, H-4) and asserts the lint
// catches each. Pinning rule-fire behavior prevents a silent regression
// where a rule stops firing (e.g. a regex typo) without failing any
// existing atom.
func TestRunLint_FiresOnKnownViolations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	fixtures := map[string]string{
		"briefs/test/b1_version_anchor.md":   "# bad\n\nThis references v8.96 as a version anchor.\n",
		"briefs/test/b2_dispatcher_vocab.md": "# bad\n\nUse this.\n\nThe `dispatcher` composes this from atoms.\n",
		"briefs/test/b3_check_names.md":      "# bad\n\nUse this.\n\nCheck writer_manifest_honesty for the gate.\n",
		"briefs/test/b4_go_source_path.md":   "# bad\n\nUse this.\n\nSee internal/tools/workflow_checks_recipe.go for details.\n",
		// B-5 oversized file — 301 lines.
		"briefs/test/b5_oversized.md": strings.Repeat("a\n", 301),
		// B-7 orphan prohibition — prohibition with no positive-form sibling in window.
		"briefs/test/b7_orphan_prohibition.md": "# bad\n\nSome prose.\n\nDo not do that.\n\nMore prose.\n",
		// H-4 step-entry atom with forbidden phrase.
		"phases/test/entry.md": "# bad\n\nYour tasks for this phase are to X and Y.\n",
	}
	for rel, body := range fixtures {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}

	violations, _, err := runLint(dir)
	if err != nil {
		t.Fatalf("runLint: %v", err)
	}

	wantIDs := []string{"B-1", "B-2", "B-3", "B-4", "B-5", "B-7", "H-4"}
	joined := strings.Join(violations, "\n")
	for _, id := range wantIDs {
		if !strings.Contains(joined, "["+id+"]") && !strings.Contains(joined, id+" ") {
			t.Errorf("expected rule %s to fire in fixture violations; got:\n%s", id, joined)
		}
	}
}

// findRepoRoot walks up from the test binary's working directory to
// the nearest parent containing go.mod. Returns the repo root or
// fails the test when not found within 10 levels.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := cwd
	for range 10 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find go.mod from %s", cwd)
	return ""
}
