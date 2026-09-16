// Tests for: ops/git — end-to-end against a real local git repo, proving
// the shell scripts built in sha.go actually run (the fakeRunner
// tests in sha_test.go only pin the command strings).
package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isolatedHomeRunner is a Runner that isolates HOME (and both git config
// scopes) so a test proves snapshot commits supply their own
// author/committer identity rather than accidentally passing because the
// machine running the test happens to have ~/.gitconfig set.
type isolatedHomeRunner struct {
	homeDir string
}

func (r isolatedHomeRunner) Run(ctx context.Context, dir, script string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	cmd.Dir = dir
	cmd.Env = []string{
		"HOME=" + r.homeDir,
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL=" + filepath.Join(r.homeDir, "no-such-gitconfig"),
		"GIT_CONFIG_SYSTEM=" + filepath.Join(r.homeDir, "no-such-gitconfig-system"),
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return out.String(), errBuf.String(), err
}

// initRepo creates a git repo at dir with one commit containing file.txt,
// returning the commit's full sha.
func initRepo(t *testing.T, dir string) string {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=zcp-test", "GIT_AUTHOR_EMAIL=zcp-test@example.com",
			"GIT_COMMITTER_NAME=zcp-test", "GIT_COMMITTER_EMAIL=zcp-test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run("init", "-q")
	run("add", "file.txt")
	run("commit", "-q", "-m", "initial")

	out, err := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestOpsGit_EndToEnd_ResolveExtractWithoutTags(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	ctx := context.Background()
	repoDir := t.TempDir()
	sha := initRepo(t, repoDir)

	r := LocalRunner{}

	resolved, err := ResolveSHA(ctx, r, repoDir, sha[:7])
	if err != nil {
		t.Fatalf("ResolveSHA: %v", err)
	}
	if resolved != sha {
		t.Fatalf("resolved = %s, want %s", resolved, sha)
	}

	tmpDir, err := MkTempDir(ctx, r)
	if err != nil {
		t.Fatalf("MkTempDir: %v", err)
	}
	defer func() { _ = RemoveTemp(ctx, r, tmpDir) }()
	if strings.HasPrefix(tmpDir, repoDir) {
		t.Fatalf("tmp dir %s must be outside repo dir %s", tmpDir, repoDir)
	}

	if err := ExtractCommitToTemp(ctx, r, repoDir, resolved, tmpDir); err != nil {
		t.Fatalf("ExtractCommitToTemp: %v", err)
	}
	extracted, err := os.ReadFile(filepath.Join(tmpDir, "file.txt"))
	if err != nil {
		t.Fatalf("read extracted file.txt: %v", err)
	}
	if string(extracted) != "hello\n" {
		t.Fatalf("extracted file.txt = %q, want %q", extracted, "hello\n")
	}
	// Extraction must not disturb the source repo's own working tree/.git.
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		t.Fatalf(".git missing from source repo after extraction: %v", err)
	}

	if tags := gitOutput(t, repoDir, "tag", "--list"); tags != "" {
		t.Fatalf("extraction created tags: %s", tags)
	}
	if head := gitOutput(t, repoDir, "rev-parse", "HEAD"); head != sha {
		t.Fatalf("extraction changed HEAD: %s", head)
	}
}

func TestHeadStatus_RealRepo_CleanThenDirty(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	ctx := context.Background()
	repoDir := t.TempDir()
	sha := initRepo(t, repoDir)

	r := LocalRunner{}
	gotSHA, dirty, ok, err := HeadStatus(ctx, r, repoDir)
	if err != nil {
		t.Fatalf("HeadStatus (clean): %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if gotSHA != sha {
		t.Errorf("sha = %s, want %s", gotSHA, sha)
	}
	if dirty {
		t.Error("dirty = true, want false on a freshly committed repo")
	}

	if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("dirty the tree: %v", err)
	}
	gotSHA, dirty, ok, err = HeadStatus(ctx, r, repoDir)
	if err != nil {
		t.Fatalf("HeadStatus (dirty): %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if gotSHA != sha {
		t.Errorf("sha = %s, want %s (HEAD unchanged by an uncommitted edit)", gotSHA, sha)
	}
	if !dirty {
		t.Error("dirty = false, want true after editing a tracked file")
	}
}

func TestHeadStatus_UnbornRepo_ReturnsNotOkNoError(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	ctx := context.Background()
	repoDir := t.TempDir()
	if out, err := exec.CommandContext(context.Background(), "git", "init", "-q", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	_, _, ok, err := HeadStatus(ctx, LocalRunner{}, repoDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("ok = true, want false (unborn HEAD)")
	}
}
