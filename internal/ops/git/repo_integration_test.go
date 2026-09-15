// Tests for: ops/git/repo.go — AdoptBaseline against a real local git repo,
// proving the shell scripts built in repo.go actually run (the fakeRunner
// tests in repo_test.go only pin the command strings). Reuses
// isolatedHomeRunner from integration_test.go so the marker commit's
// inline identity is proven independent of the machine's own ~/.gitconfig.
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitOutput runs a plain git command against dir and returns trimmed
// combined output, failing the test on a non-zero exit.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.CommandContext(context.Background(), "git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestAdoptBaseline_Integration_FilesNoRepo_InitializesWithoutCommittingFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('hi')\n"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}
	r := isolatedHomeRunner{homeDir: t.TempDir()}

	result, err := AdoptBaseline(context.Background(), r, dir)
	if err != nil {
		t.Fatalf("AdoptBaseline: %v", err)
	}
	if result.Case != AdoptCaseInitialized {
		t.Errorf("Case = %q, want %q", result.Case, AdoptCaseInitialized)
	}

	// The marker commit's tree must be EMPTY — app.js was never staged or
	// committed. zcp never commits user files.
	lsTree := gitOutput(t, dir, "ls-tree", "-r", "--name-only", "HEAD")
	if lsTree != "" {
		t.Errorf("marker commit tree is NOT empty, contains: %q — AdoptBaseline must not commit files it found", lsTree)
	}
	msg := gitOutput(t, dir, "log", "-1", "--format=%s", "HEAD")
	if msg != "zcp init" {
		t.Errorf("commit message = %q, want %q", msg, "zcp init")
	}
	authorEmail := gitOutput(t, dir, "log", "-1", "--format=%ae", "HEAD")
	if authorEmail != robotIdentityEmail {
		t.Errorf("author email = %q, want %q (robot identity, no ambient ~/.gitconfig)", authorEmail, robotIdentityEmail)
	}
	// app.js stays on disk, uncommitted.
	porcelain := gitOutput(t, dir, "status", "--porcelain")
	if !strings.Contains(porcelain, "app.js") {
		t.Errorf("app.js should still show as untracked in status: %q", porcelain)
	}
}

func TestAdoptBaseline_Integration_EmptyTreeMarkerHEAD_SkipsInitLeavesFilesUncommitted(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	dir := t.TempDir()
	// Simulate ops.InitServiceGit's GLC-1 marker commit: a repo whose HEAD
	// is a commit over the empty tree, built exactly the way
	// gitHeadEnsureFragment does (git mktree </dev/null + commit-tree +
	// update-ref HEAD) — no working-tree files staged, no index touched.
	run := func(args ...string) string {
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=zcp-test", "GIT_AUTHOR_EMAIL=zcp-test@example.com",
			"GIT_COMMITTER_NAME=zcp-test", "GIT_COMMITTER_EMAIL=zcp-test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")

	cmd := exec.CommandContext(context.Background(), "git", "mktree")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git mktree: %v\n%s", err, out)
	}
	treeSHA := strings.TrimSpace(string(out))
	if treeSHA != emptyTreeSHA {
		t.Fatalf("git mktree </dev/null = %q, want the well-known empty tree %q", treeSHA, emptyTreeSHA)
	}
	commitSHA := run("-c", "user.name=zcp-test", "-c", "user.email=zcp-test@example.com", "commit-tree", treeSHA, "-m", "zcp init")
	run("update-ref", "HEAD", commitSHA)

	// Now the "application files" land next to the marker HEAD — exactly
	// what a buildFromGit-deployed, adopt-without-prior-git service looks
	// like once GLC-1 has run.
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('hi')\n"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}

	r := isolatedHomeRunner{homeDir: t.TempDir()}
	result, err := AdoptBaseline(context.Background(), r, dir)
	if err != nil {
		t.Fatalf("AdoptBaseline: %v", err)
	}
	if result.Case != AdoptCaseInitialized {
		t.Errorf("Case = %q, want %q", result.Case, AdoptCaseInitialized)
	}

	// Exactly one commit reachable — the marker only; no new commit, no
	// re-init (a re-init would have produced a fresh unborn repo with no
	// marker commit at all).
	revCount := gitOutput(t, dir, "rev-list", "--count", "HEAD")
	if revCount != "1" {
		t.Errorf("rev-list --count HEAD = %q, want 1 (marker commit only, no snapshot)", revCount)
	}
	porcelain := gitOutput(t, dir, "status", "--porcelain")
	if !strings.Contains(porcelain, "app.js") {
		t.Errorf("app.js should still show as untracked in status: %q", porcelain)
	}
}

func TestAdoptBaseline_Integration_RealContentHEAD_PreservesHEADWithoutNewCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	dir := t.TempDir()
	sha := initRepo(t, dir) // integration_test.go helper: repo with one real commit (file.txt)

	r := isolatedHomeRunner{homeDir: t.TempDir()}
	result, err := AdoptBaseline(context.Background(), r, dir)
	if err != nil {
		t.Fatalf("AdoptBaseline: %v", err)
	}
	if result.Case != AdoptCaseExisting {
		t.Errorf("Case = %q, want %q", result.Case, AdoptCaseExisting)
	}

	headSHA := gitOutput(t, dir, "rev-parse", "HEAD")
	if headSHA != sha {
		t.Errorf("HEAD moved to %q, want it to stay at %q — AdoptBaseline must not commit on the existing-content path", headSHA, sha)
	}
}

func TestAdoptBaseline_Integration_PreservesTags(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "initialized", true: "existing"}[existing], func(t *testing.T) {
			dir := t.TempDir()
			if existing {
				initRepo(t, dir)
			} else {
				gitOutput(t, dir, "init", "-q", "-b", "main")
				gitOutput(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "init")
			}
			gitOutput(t, dir, "tag", "v1.0.0")
			gitOutput(t, dir, "tag", "zcp/baseline/av-existing")
			before := gitOutput(t, dir, "show-ref", "--tags")
			if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("app"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := AdoptBaseline(context.Background(), isolatedHomeRunner{homeDir: t.TempDir()}, dir); err != nil {
				t.Fatal(err)
			}
			if after := gitOutput(t, dir, "show-ref", "--tags"); after != before {
				t.Errorf("tags changed: before %s, after %s", before, after)
			}
		})
	}
}
