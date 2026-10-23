//go:build unix

// Tests for: ops/lease_checkout.go — the release guard reads a lease's
// checkout without writing into it (DP-4).

package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// localCheckoutSSH runs the guard's commands in a real checkout the way ssh
// hands them to the container's shell, with /var/www standing for dir.
type localCheckoutSSH struct {
	dir      string
	commands []string
}

func (s *localCheckoutSSH) ExecSSH(ctx context.Context, _, command string) ([]byte, error) {
	s.commands = append(s.commands, command)
	local := strings.ReplaceAll(command, shellQuote(leaseWorkDir), shellQuote(s.dir))
	cmd := exec.CommandContext(ctx, "sh", "-c", local)
	env := []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null"}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	cmd.Env = env
	return cmd.Output()
}

func (s *localCheckoutSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

// indexIdentity is the inode and modification time of a checkout's
// .git/index: a rewrite gives it a new inode (git renames index.lock over it)
// and a new mtime, even with the same bytes.
func indexIdentity(t *testing.T, dir string) (uint64, time.Time) {
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

// TestLeaseReleaseGuard_ReadsTheCheckoutWithoutRewritingItsIndex: the guard
// only reads, yet `git status` refreshes the index of a checkout whose tracked
// files' stat info moved (same content) and writes .git/index back — a new
// inode and mtime in a person's checkout (live, idle/p3). Every read-only git
// call the guard makes runs with GIT_OPTIONAL_LOCKS=0, which leaves the index
// alone.
func TestLeaseReleaseGuard_ReadsTheCheckoutWithoutRewritingItsIndex(t *testing.T) {
	t.Parallel()
	origin, _, _ := originWithBranches(t)
	work := t.TempDir()
	runCheckout(t, work, origin, "feat")
	// Same bytes, another mtime: the index's stat info for the file is stale,
	// so a status that may take the index lock refreshes and rewrites it.
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(work, "app.txt"), past, past); err != nil {
		t.Fatal(err)
	}
	inode, mtime := indexIdentity(t, work)

	fx := newLeaseFixture(t)
	ssh := &localCheckoutSSH{dir: work}
	fx.deps.SSH = ssh
	rec := leaseRecord{PoolHostname: testPoolHost, PoolServiceID: "svc-pool", ContainerID: "c-abc", ContainerNumber: 3, ContainerHostname: nodeHostname(3)}
	if _, err := guardLeaseRelease(context.Background(), fx.deps, rec); err != nil {
		t.Fatalf("a clean checkout must pass the guard: %v", err)
	}

	if gotInode, gotMtime := indexIdentity(t, work); gotInode != inode || !gotMtime.Equal(mtime) {
		t.Errorf(".git/index was rewritten by the guard: inode %d -> %d, mtime %s -> %s", inode, gotInode, mtime, gotMtime)
	}
	if len(ssh.commands) == 0 {
		t.Fatal("the guard ran no command in the checkout")
	}
	for _, c := range ssh.commands {
		if all, read := strings.Count(c, "git -C"), strings.Count(c, "GIT_OPTIONAL_LOCKS=0 git -C"); all != read {
			t.Errorf("%d of %d git calls run without GIT_OPTIONAL_LOCKS=0: %s", all-read, all, c)
		}
	}
}
