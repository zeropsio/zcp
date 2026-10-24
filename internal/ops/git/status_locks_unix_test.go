//go:build unix

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// staleIndexRepo is a repository with one committed file whose mtime moved
// after the commit: same bytes, stale stat info in the index, so a git status
// that may take the index lock refreshes and rewrites .git/index (a new inode
// and mtime) — what a status read on someone else's checkout must never do.
func staleIndexRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "app.txt")
	run("commit", "-q", "-m", "one")
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "app.txt"), past, past); err != nil {
		t.Fatal(err)
	}
	return dir
}

func gitIndexIdentity(t *testing.T, dir string) (uint64, time.Time) {
	t.Helper()
	info, err := os.Stat(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("no inode on this platform")
	}
	return uint64(st.Ino), info.ModTime() //nolint:unconvert // Ino is uint32 on some unix platforms
}

// TestStatusProbes_NeverRewriteTheIndex pins that zcp's read-only repository
// probes — the envelope's repo state, a working-tree deploy's head status and
// the self-deploy preflight — leave .git/index as it was: they run on the
// owner's dev services on every status call, and a status that takes the
// index lock rewrites the index and collides with the user's own git (live
// 2026-10-24: every status call ran git status in each dev's /var/www).
func TestStatusProbes_NeverRewriteTheIndex(t *testing.T) {
	t.Parallel()
	probes := map[string]func(context.Context, string) error{
		"repo state": func(ctx context.Context, dir string) error {
			_, _, err := LocalRunner{}.Run(ctx, dir, repoStatusScript)
			return err
		},
		"head status": func(ctx context.Context, dir string) error {
			_, _, _, _, err := HeadStatus(ctx, LocalRunner{}, dir)
			return err
		},
		"self-deploy preflight": func(ctx context.Context, dir string) error {
			_, err := ReadSelfDeployPreflight(ctx, LocalRunner{}, dir)
			return err
		},
	}
	for name, probe := range probes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := staleIndexRepo(t)
			inode, mtime := gitIndexIdentity(t, dir)
			if err := probe(context.Background(), dir); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if gotInode, gotMtime := gitIndexIdentity(t, dir); gotInode != inode || !gotMtime.Equal(mtime) {
				t.Errorf("%s rewrote .git/index: inode %d -> %d, mtime %s -> %s", name, inode, gotInode, mtime, gotMtime)
			}
		})
	}
}
