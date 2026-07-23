package skillpacks

import (
	"context"
	"path/filepath"
	"testing"
)

// TestCloneArgs_UsesDoubleDashBeforeURL is finding 4a: an option-shaped
// registry URL (or dest) must never be interpreted as a git flag — "--" has
// to appear before the repository URL argument.
func TestCloneArgs_UsesDoubleDashBeforeURL(t *testing.T) {
	t.Parallel()

	args := cloneArgs("--upload-pack=evil", "main", "/tmp/dest")

	idx := -1
	for i, a := range args {
		if a == "--" {
			idx = i
			break
		}
	}
	if idx == -1 {
		t.Fatalf("expected a literal \"--\" argument, got %v", args)
	}
	if idx+1 >= len(args) || args[idx+1] != "--upload-pack=evil" {
		t.Errorf("expected the repository URL immediately after \"--\", got %v", args)
	}
	want := []string{"clone", "--depth", "1", "--branch", "main", "--single-branch", "--", "--upload-pack=evil", "/tmp/dest"}
	if len(args) != len(want) {
		t.Fatalf("cloneArgs = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("cloneArgs[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestCloneRepo_MissingGitBinary_ClearError(t *testing.T) {
	// Non-parallel: mutates process-wide PATH.
	emptyPathDir := t.TempDir()
	t.Setenv("PATH", emptyPathDir)

	err := cloneRepo(context.Background(), "irrelevant", "main", t.TempDir())
	if err == nil {
		t.Fatal("expected an error when git is not on PATH")
	}
	if code := codeOf(t, err); code != CodeGitMissing {
		t.Errorf("code = %q, want %q", code, CodeGitMissing)
	}
}

func TestCloneRepo_LocalFixture_Success(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	writeSkillMD(t, filepath.Join(repoDir, "SKILL.md"), "foo", "does foo")
	newFixtureRepo(t, repoDir)

	dest := t.TempDir() + "/clone"
	if err := cloneRepo(context.Background(), repoDir, "main", dest); err != nil {
		t.Fatalf("cloneRepo: %v", err)
	}
	commit, err := headCommit(context.Background(), dest)
	if err != nil {
		t.Fatalf("headCommit: %v", err)
	}
	if len(commit) != 40 {
		t.Errorf("commit = %q, want a 40-char SHA", commit)
	}
}

func TestCloneRepo_ContextCanceled_Errors(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	writeSkillMD(t, filepath.Join(repoDir, "SKILL.md"), "foo", "does foo")
	newFixtureRepo(t, repoDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := cloneRepo(ctx, repoDir, "main", t.TempDir()+"/clone")
	if err == nil {
		t.Fatal("expected an error for an already-canceled context")
	}
}
