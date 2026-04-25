package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLintDeployignore_ContainsDist_Rejects — run-11 gap P-3. dist in
// .deployignore filters the deploy artifact; deploy must reject loud.
func TestLintDeployignore_ContainsDist_Rejects(t *testing.T) {
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
	if len(res.Errors) == 0 {
		t.Fatal("expected hard reject on dist line, got none")
	}
	found := false
	for _, e := range res.Errors {
		if strings.Contains(e, "dist") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error to name dist, got: %v", res.Errors)
	}
}

// TestLintDeployignore_ContainsNodeModules_Rejects — same hard-reject
// applies to node_modules.
func TestLintDeployignore_ContainsNodeModules_Rejects(t *testing.T) {
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
	if len(res.Errors) == 0 {
		t.Fatal("expected hard reject on node_modules line")
	}
}

// TestLintDeployignore_ContainsEditorMetadata_Warns — .idea/.vscode/
// *.log lines warn but don't block.
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
	if len(res.Errors) > 0 {
		t.Errorf("editor metadata should not hard-reject, got errors: %v", res.Errors)
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
	if len(res.Errors) > 0 || len(res.Warnings) > 0 {
		t.Errorf("expected empty result for missing .deployignore, got %+v", res)
	}
}

// TestLintDeployignore_LegitimatePathPasses — a recipe-specific reason
// to ignore (test fixtures, build-tool config) doesn't trip the warn
// or reject lists.
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
	if len(res.Errors) > 0 || len(res.Warnings) > 0 {
		t.Errorf("legitimate paths should pass, got %+v", res)
	}
}
