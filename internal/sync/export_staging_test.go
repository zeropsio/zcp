package sync

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestExportRecipe_OverlaysStagedWriterContent pins the Cx-CLOSE-STEP-STAGING
// export contract: when close-step has staged per-codebase README.md +
// CLAUDE.md under `{recipeDir}/{codebase}/`, ExportRecipe includes
// them in the archive under `{codebase}/` alongside the git-tracked
// source. Without this, sessionless exports produce a tarball missing
// the writer output (v36 F-10).
//
// The test seeds:
//   - A recipeDir with root README.md + environments/0-…/README.md
//   - A staged writer README.md + CLAUDE.md at recipeDir/apidev/
//   - A source mount at tempDir/apidev with a Go source file only
//
// Asserts the archive contains both the staged markdown AND the
// source file at apidev/.
func TestExportRecipe_OverlaysStagedWriterContent(t *testing.T) {
	// Not parallel — uses t.Chdir which requires exclusive working dir.
	recipeDir := t.TempDir()
	appDir := t.TempDir()

	// Recipe root + one env folder so findEnvFolders resolves.
	mustWrite(t, filepath.Join(recipeDir, "README.md"), "# root recipe readme")
	mustWrite(t, filepath.Join(recipeDir, "environments", "0 \u2014 AI Agent", "README.md"), "env 0 body")
	// Run-11 M-3: post-§L stitch lands README + CLAUDE at SourceRoot
	// (= appDir) directly, NOT under <recipeDir>/<codebase>/.
	mustWrite(t, filepath.Join(appDir, "README.md"), "api README at SourceRoot")
	mustWrite(t, filepath.Join(appDir, "CLAUDE.md"), "api CLAUDE at SourceRoot")
	// App source.
	mustWrite(t, filepath.Join(appDir, "main.go"), "package main")

	// Appname must match the staged dir name.
	if err := os.Rename(appDir, filepath.Join(filepath.Dir(appDir), "apidev")); err != nil {
		t.Fatalf("rename appDir: %v", err)
	}
	appDir = filepath.Join(filepath.Dir(appDir), "apidev")

	t.Chdir(t.TempDir())

	result, err := ExportRecipe(ExportOpts{
		RecipeDir:     recipeDir,
		AppDirs:       []string{appDir},
		SkipCloseGate: true, // no session context in this fixture
	})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	entries := listArchive(t, result.ArchivePath)
	prefix := filepath.Base(recipeDir) + "-zcprecipator"
	want := []string{
		prefix + "/README.md",
		prefix + "/environments/0 \u2014 AI Agent/README.md",
		prefix + "/apidev/main.go",
		prefix + "/apidev/README.md",
		prefix + "/apidev/CLAUDE.md",
	}
	for _, w := range want {
		if !contains(entries, w) {
			t.Errorf("archive missing %q\nentries: %v", w, entries)
		}
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func listArchive(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		names = append(names, h.Name)
	}
	return names
}

func contains(ss []string, target string) bool {
	return slices.Contains(ss, target)
}

// TestOverlayStagedWriterContent_MissingDirNoop covers the no-staging
// path — when close-step didn't run (skip or legacy flow), the
// overlay does nothing and export still works.
func TestOverlayStagedWriterContent_MissingDirNoop(t *testing.T) {
	t.Parallel()
	// Absent stagedDir should be silent — not an error.
	dst := "/nonexistent-path-xyz"
	err := overlayStagedWriterContent(nil, dst, "archive/apidev")
	if err != nil {
		t.Errorf("missing stagedDir should noop; got err=%v", err)
	}
}
