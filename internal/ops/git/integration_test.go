// Tests for: ops/git — end-to-end against a real local git repo, proving
// the shell scripts built in sha.go/ledger.go actually run (the fakeRunner
// tests in sha_test.go/ledger_test.go only pin the command strings).
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

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

func TestOpsGit_EndToEnd_ResolveExtractLedgerRoundTrip(t *testing.T) {
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

	entry := topology.LedgerEntry{
		SHA:          resolved,
		AppVersionID: "av-1",
		Target:       "appstage",
		Project:      "proj-1",
		At:           "2026-09-14T12:00:00Z",
	}
	if err := WriteLedger(ctx, r, repoDir, entry); err != nil {
		t.Fatalf("WriteLedger: %v", err)
	}

	gotSHA, err := ReadEnvRef(ctx, r, repoDir, "appstage")
	if err != nil {
		t.Fatalf("ReadEnvRef: %v", err)
	}
	if gotSHA != resolved {
		t.Fatalf("ReadEnvRef = %s, want %s", gotSHA, resolved)
	}

	entries, err := ListDeploys(ctx, r, repoDir)
	if err != nil {
		t.Fatalf("ListDeploys: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1: %+v", len(entries), entries)
	}
	if entries[0] != entry {
		t.Fatalf("entries[0] = %+v, want %+v", entries[0], entry)
	}

	// Ledger refs are on a NEVER-checked-out branch — the repo's own HEAD
	// (and working tree) must stay untouched.
	headOut, err := exec.CommandContext(context.Background(), "git", "-C", repoDir, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse HEAD after ledger write: %v\n%s", err, headOut)
	}
	if strings.TrimSpace(string(headOut)) != sha {
		t.Fatalf("HEAD moved after WriteLedger: got %s, want %s", strings.TrimSpace(string(headOut)), sha)
	}
}

func TestReadEnvRef_UnbornRepo_ReturnsEmptyNoError(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	ctx := context.Background()
	repoDir := t.TempDir()
	if out, err := exec.CommandContext(context.Background(), "git", "init", "-q", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	got, err := ReadEnvRef(ctx, LocalRunner{}, repoDir, "appstage")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got = %q, want empty", got)
	}
}
