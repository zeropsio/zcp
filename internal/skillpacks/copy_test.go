package skillpacks

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyTreeIntoRoot_CopiesFilesAndSkipsNothingButGit(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "SKILL.md"), "# skill\n")
	writeFile(t, filepath.Join(src, "sub", "nested.md"), "nested\n")

	root, cwd := openTestRoot(t)
	if err := copyTreeIntoRoot(root, src, "dest"); err != nil {
		t.Fatalf("copyTreeIntoRoot: %v", err)
	}

	dst := filepath.Join(cwd, "dest")
	for _, rel := range []string{"SKILL.md", filepath.Join("sub", "nested.md")} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err != nil {
			t.Errorf("expected %s to exist: %v", rel, err)
		}
	}
}

func TestCopyTreeIntoRoot_SymlinkFile_HardError(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "SKILL.md"), "# skill\n")
	target := filepath.Join(t.TempDir(), "outside.txt")
	writeFile(t, target, "outside content\n")
	writeSymlinkOrSkip(t, target, filepath.Join(src, "link.txt"))

	root, _ := openTestRoot(t)
	err := copyTreeIntoRoot(root, src, "dest")
	if err == nil {
		t.Fatal("expected a hard error for a symlinked file, not a silent skip")
	}
	if code := codeOf(t, err); code != CodeInvalidSource {
		t.Errorf("code = %q, want %q", code, CodeInvalidSource)
	}
}

func TestCopyTreeIntoRoot_SymlinkedDirectory_HardError(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "SKILL.md"), "# skill\n")
	realDir := t.TempDir()
	writeFile(t, filepath.Join(realDir, "secret.txt"), "should never appear in dest\n")
	writeSymlinkOrSkip(t, realDir, filepath.Join(src, "linked-dir"))

	root, _ := openTestRoot(t)
	err := copyTreeIntoRoot(root, src, "dest")
	if err == nil {
		t.Fatal("expected a hard error for a symlinked directory, not a silent skip")
	}
}

func TestRenameNoReplace_Success(t *testing.T) {
	t.Parallel()
	root, cwd := openTestRoot(t)
	writeFile(t, filepath.Join(cwd, "staged", "SKILL.md"), "# x\n")

	if err := renameNoReplace(root, "staged", "final"); err != nil {
		t.Fatalf("renameNoReplace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "final", "SKILL.md")); err != nil {
		t.Errorf("expected final/SKILL.md to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "staged")); !os.IsNotExist(err) {
		t.Errorf("expected staged to be gone after rename, stat err = %v", err)
	}
}

func TestRenameNoReplace_DestinationExists_Collision(t *testing.T) {
	t.Parallel()
	root, cwd := openTestRoot(t)
	writeFile(t, filepath.Join(cwd, "staged", "SKILL.md"), "# new\n")
	writeFile(t, filepath.Join(cwd, "final", "SKILL.md"), "# pre-existing\n")

	err := renameNoReplace(root, "staged", "final")
	if err == nil {
		t.Fatal("expected an error when the destination already exists")
	}
	var ce *CodedError
	if !errors.As(err, &ce) || ce.Code != CodeCollision {
		t.Errorf("error = %v, want a *CodedError with code %q", err, CodeCollision)
	}
	data, readErr := os.ReadFile(filepath.Join(cwd, "final", "SKILL.md"))
	if readErr != nil {
		t.Fatalf("read final/SKILL.md: %v", readErr)
	}
	if string(data) != "# pre-existing\n" {
		t.Errorf("pre-existing destination was overwritten: %q", data)
	}
	if _, err := os.Stat(filepath.Join(cwd, "staged", "SKILL.md")); err != nil {
		t.Errorf("staged content should be left in place on a collision: %v", err)
	}
}
