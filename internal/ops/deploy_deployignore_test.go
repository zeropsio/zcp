package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLintDeployignore_ContainsDist_Warns — dist in .deployignore is
// almost always a mistake; the linter surfaces a warning. Deploy is
// not blocked — the TEACH-side .deployignore paragraph in
// internal/knowledge/themes/core.md does the actual teaching.
func TestLintDeployignore_ContainsDist_Warns(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	body := ".git\n.idea\ndist\n"
	if err := os.WriteFile(filepath.Join(dir, ".deployignore"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := LintDeployignore(dir)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "dist") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning to name dist, got: %v", res.Warnings)
	}
}

// TestLintDeployignore_ContainsNodeModules_Warns — same warning shape
// applies to node_modules.
func TestLintDeployignore_ContainsNodeModules_Warns(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	body := "node_modules/\n"
	if err := os.WriteFile(filepath.Join(dir, ".deployignore"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := LintDeployignore(dir)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("expected warning on node_modules line")
	}
}

// TestLintDeployignore_ContainsEditorMetadata_Warns — .idea/.vscode/
// *.log lines warn.
func TestLintDeployignore_ContainsEditorMetadata_Warns(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	body := ".git\n.idea\n.vscode\nbuild.log\n"
	if err := os.WriteFile(filepath.Join(dir, ".deployignore"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := LintDeployignore(dir)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if len(res.Warnings) < 4 {
		t.Errorf("expected 4 warnings (.git, .idea, .vscode, *.log shape), got %d: %v", len(res.Warnings), res.Warnings)
	}
}

// TestLintDeployignore_NoFile_Passes — most-common case. No
// .deployignore at SourceRoot → empty result, no error.
func TestLintDeployignore_NoFile_Passes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	res, err := LintDeployignore(dir)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if len(res.Warnings) > 0 {
		t.Errorf("expected empty result for missing .deployignore, got %+v", res)
	}
}

// TestLintDeployignore_LegitimatePathPasses — a recipe-specific reason
// to ignore (test fixtures, build-tool config) doesn't trip the
// pattern lists.
func TestLintDeployignore_LegitimatePathPasses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	body := "tests/fixtures\n.eslint.cache\n"
	if err := os.WriteFile(filepath.Join(dir, ".deployignore"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := LintDeployignore(dir)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if len(res.Warnings) > 0 {
		t.Errorf("legitimate paths should pass, got %+v", res)
	}
}
