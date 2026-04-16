package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Deploy-step `scaffold_hygiene` check: ensures every published codebase
// ships with `.gitignore` + `.env.example` AND no build-output /
// node_modules / OS-cruft leaked into the output tree. v21 apidev
// shipped 208 MB of node_modules because its .gitignore was never
// written — this check prevents that class of regression.

func writeFileHygiene(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestScaffoldHygiene_AllPresent_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFileHygiene(t, filepath.Join(dir, ".gitignore"), "node_modules/\ndist/\n.env\n")
	writeFileHygiene(t, filepath.Join(dir, ".env.example"), "DB_URL=\n")
	writeFileHygiene(t, filepath.Join(dir, "src", "main.ts"), "export {}\n")

	checks := checkScaffoldHygiene(dir, "apidev")
	if len(checks) != 1 || checks[0].Status != statusPass {
		t.Fatalf("expected single pass; got %+v", checks)
	}
	if checks[0].Name != "apidev_scaffold_hygiene" {
		t.Fatalf("expected check name apidev_scaffold_hygiene; got %q", checks[0].Name)
	}
}

func TestScaffoldHygiene_MissingGitignore_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFileHygiene(t, filepath.Join(dir, ".env.example"), "DB_URL=\n")

	checks := checkScaffoldHygiene(dir, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, ".gitignore") {
		t.Fatalf("detail must name `.gitignore`: %s", checks[0].Detail)
	}
}

func TestScaffoldHygiene_MissingEnvExample_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFileHygiene(t, filepath.Join(dir, ".gitignore"), "node_modules/\n")

	checks := checkScaffoldHygiene(dir, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, ".env.example") {
		t.Fatalf("detail must name `.env.example`: %s", checks[0].Detail)
	}
}

func TestScaffoldHygiene_NodeModulesPresent_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFileHygiene(t, filepath.Join(dir, ".gitignore"), "node_modules/\n")
	writeFileHygiene(t, filepath.Join(dir, ".env.example"), "DB_URL=\n")
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}

	checks := checkScaffoldHygiene(dir, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "node_modules") {
		t.Fatalf("detail must name `node_modules`: %s", checks[0].Detail)
	}
}

func TestScaffoldHygiene_DistPresent_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFileHygiene(t, filepath.Join(dir, ".gitignore"), "dist/\n")
	writeFileHygiene(t, filepath.Join(dir, ".env.example"), "")
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}

	checks := checkScaffoldHygiene(dir, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "dist") {
		t.Fatalf("detail must name `dist`: %s", checks[0].Detail)
	}
}

func TestScaffoldHygiene_DSStorePresent_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFileHygiene(t, filepath.Join(dir, ".gitignore"), ".DS_Store\n")
	writeFileHygiene(t, filepath.Join(dir, ".env.example"), "")
	writeFileHygiene(t, filepath.Join(dir, ".DS_Store"), "\x00")

	checks := checkScaffoldHygiene(dir, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, ".DS_Store") {
		t.Fatalf("detail must name `.DS_Store`: %s", checks[0].Detail)
	}
}

func TestScaffoldHygiene_RecursiveDSStoreSearch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFileHygiene(t, filepath.Join(dir, ".gitignore"), ".DS_Store\n")
	writeFileHygiene(t, filepath.Join(dir, ".env.example"), "")
	writeFileHygiene(t, filepath.Join(dir, "src", ".DS_Store"), "\x00")

	checks := checkScaffoldHygiene(dir, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "src") || !strings.Contains(checks[0].Detail, ".DS_Store") {
		t.Fatalf("detail must identify src/.DS_Store: %s", checks[0].Detail)
	}
}

func TestScaffoldHygiene_MultipleIssues_ReportsAll(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Missing both hygiene files + node_modules present + .DS_Store nested.
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFileHygiene(t, filepath.Join(dir, ".DS_Store"), "\x00")

	checks := checkScaffoldHygiene(dir, "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	for _, needle := range []string{".gitignore", ".env.example", "node_modules", ".DS_Store"} {
		if !strings.Contains(checks[0].Detail, needle) {
			t.Fatalf("detail must name %q: %s", needle, checks[0].Detail)
		}
	}
}

func TestScaffoldHygiene_MissingCodebaseDir_Skips(t *testing.T) {
	t.Parallel()
	checks := checkScaffoldHygiene("/nonexistent-path-xyz-42", "apidev")
	if len(checks) != 0 {
		t.Fatalf("nonexistent codebase dir should no-op; got %+v", checks)
	}
}
