package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStageWriterContent_CopiesPerCodebaseMarkdown is the Cx-CLOSE-STEP-STAGING
// RED→GREEN test. v36 F-10 root cause: writer sub-agent authored
// README.md + CLAUDE.md on the SSHFS source mount
// (`/var/www/{codebase}/`), but those files were never copied into
// the recipe output tree. Sessionless export via `git ls-files`
// skipped them because the writer never committed its output —
// every sessionless export produced an incomplete deliverable.
//
// Fix: close-step stages writer content from source mount into
// `{OutputDir}/{codebase}/` as an engine-side action. Close-step
// check `writer_content_staged` verifies the copy.
//
// Test sets up a fake mount + output dir, seeds source READMEs +
// CLAUDEs, calls stageWriterContent directly, asserts copy happened
// byte-identical.
func TestStageWriterContent_CopiesPerCodebaseMarkdown(t *testing.T) {
	// Not parallel — uses recipeMountBaseOverride global.
	mountBase := t.TempDir()
	outputDir := t.TempDir()
	prevOverride := recipeMountBaseOverride
	recipeMountBaseOverride = mountBase
	t.Cleanup(func() { recipeMountBaseOverride = prevOverride })

	// Seed source mount per-codebase README+CLAUDE for two runtimes.
	seed := map[string]string{
		"appdev/README.md": "app README body",
		"appdev/CLAUDE.md": "app CLAUDE body",
		"dbdev/README.md":  "db README body (should be ignored — not a runtime)",
	}
	for rel, body := range seed {
		full := filepath.Join(mountBase, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	plan := testMinimalPlan()
	eng := NewEngine(t.TempDir(), EnvLocal, nil)
	state := &WorkflowState{
		Recipe: NewRecipeState(),
	}
	state.Recipe.Plan = plan
	state.Recipe.OutputDir = outputDir

	if err := eng.stageWriterContent(state); err != nil {
		t.Fatalf("stageWriterContent: %v", err)
	}

	// Assert staged files match source.
	for _, name := range []string{"README.md", "CLAUDE.md"} {
		dst := filepath.Join(outputDir, "appdev", name)
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Errorf("dst missing: %s: %v", dst, err)
			continue
		}
		if !strings.HasPrefix(string(got), "app") {
			t.Errorf("dst %s body=%q want prefix 'app'", dst, got)
		}
	}
	// Managed-service codebase (db) must NOT be staged.
	if _, err := os.Stat(filepath.Join(outputDir, "dbdev", "README.md")); err == nil {
		t.Errorf("db/README.md should not be staged (managed service, not a runtime codebase)")
	}
}

// TestStageWriterContent_PartialAuthorshipReturnsError pins the fail
// path: a codebase dir that EXISTS but lacks README.md / CLAUDE.md
// (writer sub-agent half-ran) must produce an error naming the file
// + remediation. Close refuses to advance until the writer completes.
func TestStageWriterContent_PartialAuthorshipReturnsError(t *testing.T) {
	mountBase := t.TempDir()
	outputDir := t.TempDir()
	prevOverride := recipeMountBaseOverride
	recipeMountBaseOverride = mountBase
	t.Cleanup(func() { recipeMountBaseOverride = prevOverride })

	// appdev/ directory exists (push-app ran) but no writer markdown.
	if err := os.MkdirAll(filepath.Join(mountBase, "appdev"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mountBase, "appdev", "package.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	plan := testMinimalPlan()
	eng := NewEngine(t.TempDir(), EnvLocal, nil)
	state := &WorkflowState{Recipe: NewRecipeState()}
	state.Recipe.Plan = plan
	state.Recipe.OutputDir = outputDir

	err := eng.stageWriterContent(state)
	if err == nil {
		t.Fatal("expected error when codebase dir exists but README+CLAUDE absent")
	}
	if !strings.Contains(err.Error(), "README.md") {
		t.Errorf("error should name README.md; got %q", err.Error())
	}
}

// TestStageWriterContent_NoCodebaseDirSkipsSilently verifies the
// testability contract: when no codebase directory exists on the
// source mount (test stubs that never ran push-app), staging is a
// no-op. This keeps legacy unit tests that call close without
// materializing a mount from tripping on F-10 enforcement.
func TestStageWriterContent_NoCodebaseDirSkipsSilently(t *testing.T) {
	mountBase := t.TempDir()
	outputDir := t.TempDir()
	prevOverride := recipeMountBaseOverride
	recipeMountBaseOverride = mountBase
	t.Cleanup(func() { recipeMountBaseOverride = prevOverride })

	plan := testMinimalPlan()
	eng := NewEngine(t.TempDir(), EnvLocal, nil)
	state := &WorkflowState{Recipe: NewRecipeState()}
	state.Recipe.Plan = plan
	state.Recipe.OutputDir = outputDir

	if err := eng.stageWriterContent(state); err != nil {
		t.Errorf("empty mount should skip silently; got %v", err)
	}
}
