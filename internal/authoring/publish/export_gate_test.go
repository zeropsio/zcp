package publish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimalRecipeDir sets up a minimal recipe dir with one env folder so
// ExportRecipe can proceed past the directory existence check, including
// the run-23 F-26 .refinement-closed marker. Tests that target the
// refinement gate explicitly call minimalRecipeDirWithoutRefinementClose.
func minimalRecipeDir(t *testing.T, root string) string {
	t.Helper()
	recipeDir := minimalRecipeDirWithoutRefinementClose(t, root)
	writeFile(t, filepath.Join(recipeDir, ".refinement-closed"), "")
	return recipeDir
}

// minimalRecipeDirWithoutRefinementClose is the no-marker variant for
// tests that exercise the F-26 refinement gate.
func minimalRecipeDirWithoutRefinementClose(t *testing.T, root string) string {
	t.Helper()
	recipeDir := filepath.Join(root, "test-showcase")
	writeFile(t, filepath.Join(recipeDir, "README.md"), "# root")
	writeFile(t, filepath.Join(recipeDir, "environments", "0 — AI Agent", "import.yaml"), "project:\n")
	return recipeDir
}

// TestExportRecipe_RefusesWhenRefinementNotClosed — run-23 F-26.
// The export gate refuses unless the .refinement-closed marker exists in
// the recipe dir. The gate runs unconditionally (the retired v2 session
// close-step gate nested this check inside a declared-session branch, so
// the sessionless v3 flow silently skipped it).
func TestExportRecipe_RefusesWhenRefinementNotClosed(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	_, err := ExportRecipe(ExportOpts{
		RecipeDir: minimalRecipeDirWithoutRefinementClose(t, root),
	})
	if err == nil {
		t.Fatal("expected ErrExportBlocked when refinement marker missing, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "EXPORT_BLOCKED") {
		t.Errorf("error missing EXPORT_BLOCKED code; got: %s", msg)
	}
	if !strings.Contains(msg, "refinement phase has not closed") {
		t.Errorf("error must name the refinement gate; got: %s", msg)
	}
	if !strings.Contains(msg, "complete-phase phase=refinement") {
		t.Errorf("error must name the recovery action; got: %s", msg)
	}
}

// TestExportRecipe_AllowsWhenRefinementClosed — the marker present is the
// single gate condition; export proceeds to produce an archive.
func TestExportRecipe_AllowsWhenRefinementClosed(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	result, err := ExportRecipe(ExportOpts{
		RecipeDir: minimalRecipeDir(t, root),
	})
	if err != nil {
		t.Fatalf("expected export success, got: %v", err)
	}
	if result == nil || result.ArchivePath == "" {
		t.Fatalf("expected archive path, got %+v", result)
	}
}

// TestExportRecipe_ForceExportBypassWarning — SkipCloseGate bypasses the
// refinement gate AND prints a stderr warning.
func TestExportRecipe_ForceExportBypassWarning(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	// Capture stderr.
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w

	_, exportErr := ExportRecipe(ExportOpts{
		RecipeDir:     minimalRecipeDirWithoutRefinementClose(t, root),
		SkipCloseGate: true,
	})
	w.Close()
	os.Stderr = origStderr

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	stderrText := string(buf[:n])

	if exportErr != nil {
		t.Fatalf("expected force-export success, got: %v", exportErr)
	}
	if !strings.Contains(stderrText, "--force-export bypasses the refinement close gate") {
		t.Errorf("expected stderr to contain bypass warning; got: %s", stderrText)
	}
}
