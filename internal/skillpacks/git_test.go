package skillpacks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestFetchCommit_LocalFixture_ChecksOutExactPinnedCommit is set.go's
// pinned-commit-addition primitive proven in isolation: a fixture repo that
// has since moved on to a second commit must, when fetched by its FIRST
// commit's SHA, produce the first commit's content — never the branch tip's.
func TestFetchCommit_LocalFixture_ChecksOutExactPinnedCommit(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	writeFile(t, filepath.Join(repoDir, "file.txt"), "v1\n")
	newFixtureRepo(t, repoDir)
	pinnedCommit := commitAtHEAD(t, repoDir)

	writeFile(t, filepath.Join(repoDir, "file.txt"), "v2\n")
	mustRunGit(t, repoDir, isolatedGitEnv(t), "add", "-A")
	mustRunGit(t, repoDir, isolatedGitEnv(t), "commit", "-q", "-m", "moved on")
	tipCommit := commitAtHEAD(t, repoDir)
	if tipCommit == pinnedCommit {
		t.Fatal("test setup: tip must differ from the pinned commit")
	}

	dest := t.TempDir() + "/fetched"
	if err := fetchCommit(context.Background(), repoDir, pinnedCommit, dest); err != nil {
		t.Fatalf("fetchCommit: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "file.txt"))
	if err != nil {
		t.Fatalf("read fetched file: %v", err)
	}
	if string(got) != "v1\n" {
		t.Errorf("fetched content = %q, want the PINNED commit's content %q (not the tip's)", got, "v1\n")
	}
}

// TestFetchCommit_UnknownCommit_ClearError proves an unreachable/unknown
// commit SHA is a hard, named error — never a silent fallback to the branch
// tip (spec-skill-packs.md §3.1).
func TestFetchCommit_UnknownCommit_ClearError(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	writeFile(t, filepath.Join(repoDir, "file.txt"), "v1\n")
	newFixtureRepo(t, repoDir)

	unknownCommit := "0000000000000000000000000000000000000000"
	dest := t.TempDir() + "/fetched"
	err := fetchCommit(context.Background(), repoDir, unknownCommit, dest)
	if err == nil {
		t.Fatal("expected an error for an unfetchable commit")
	}
	if code := codeOf(t, err); code != CodeDownloadFailed {
		t.Errorf("code = %q, want %q", code, CodeDownloadFailed)
	}
	if !strings.Contains(err.Error(), unknownCommit) {
		t.Errorf("error = %v, want it to name the unfetchable commit %q", err, unknownCommit)
	}
}

// commitAtHEAD returns dir's checked-out HEAD commit SHA via the isolated
// test git environment.
func commitAtHEAD(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-parse", "HEAD")
	cmd.Env = isolatedGitEnv(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
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
