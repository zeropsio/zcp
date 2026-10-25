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

	"github.com/zeropsio/zcp/internal/platform"
)

// localCheckoutSSH runs the guard's commands in a real checkout the way ssh
// hands them to the container's shell, with /var/www standing for dir, and
// answers as SystemSSHDeployer does: stdout and stderr together, a failure as
// an *SSHExecError carrying them. path, when set, goes first on PATH.
type localCheckoutSSH struct {
	dir      string
	path     string
	commands []string
}

func (s *localCheckoutSSH) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	s.commands = append(s.commands, command)
	local := strings.ReplaceAll(command, shellQuote(leaseWorkDir), shellQuote(s.dir))
	cmd := exec.CommandContext(ctx, "sh", "-c", local)
	env := []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null"}
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "GIT_"):
		case s.path != "" && strings.HasPrefix(kv, "PATH="):
			env = append(env, "PATH="+s.path+string(os.PathListSeparator)+strings.TrimPrefix(kv, "PATH="))
		default:
			env = append(env, kv)
		}
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, &platform.SSHExecError{Hostname: host, Output: string(out), Err: err}
	}
	return out, nil
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *localCheckoutSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
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

// TestLeaseReleaseGuard_CommitsOnAnyLocalBranch_Refused: a reset takes every
// commit in /var/www with it, not only HEAD's. The guard counts the commits
// on no remote branch across every local branch and a detached HEAD, so work
// committed on one branch and left there while HEAD sits on a clean branch
// origin has — or on a detached HEAD, or beside an unborn one — refuses the
// release; a checkout with no commit at all, or all of it pushed, passes.
func TestLeaseReleaseGuard_CommitsOnAnyLocalBranch_Refused(t *testing.T) {
	t.Parallel()
	commit := func(t *testing.T, work, name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(work, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, work, "add", "-A")
		gitIn(t, work, "commit", "-q", "-m", name)
	}
	tests := []struct {
		name       string
		arrange    func(t *testing.T, work string)
		wantCommit string // "" = the guard passes
	}{
		{"everything pushed", func(*testing.T, string) {}, ""},
		{"a commit left on another branch, HEAD back on a clean pushed one", func(t *testing.T, work string) {
			gitIn(t, work, "checkout", "-q", "-b", "topic")
			commit(t, work, "topic.txt")
			gitIn(t, work, "checkout", "-q", "feat")
		}, "1 commit"},
		{"a commit on a detached HEAD", func(t *testing.T, work string) {
			gitIn(t, work, "checkout", "-q", "--detach")
			commit(t, work, "detached.txt")
		}, "1 commit"},
		{"an unborn HEAD beside a branch holding a commit", func(t *testing.T, work string) {
			gitIn(t, work, "checkout", "-q", "-b", "topic")
			commit(t, work, "topic.txt")
			gitIn(t, work, "checkout", "-q", "--orphan", "fresh")
			gitIn(t, work, "rm", "-rfq", ".")
		}, "1 commit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			origin, _, _ := originWithBranches(t)
			work := t.TempDir()
			runCheckout(t, work, origin, "feat")
			tt.arrange(t, work)

			fx := newLeaseFixture(t)
			fx.deps.SSH = &localCheckoutSSH{dir: work}
			rec := leaseRecord{PoolHostname: testPoolHost, PoolServiceID: "svc-pool", ContainerID: "c-abc", ContainerNumber: 3, ContainerHostname: nodeHostname(3)}
			_, err := guardLeaseRelease(context.Background(), fx.deps, rec)
			switch {
			case tt.wantCommit == "" && err != nil:
				t.Errorf("guard refused: %v", err)
			case tt.wantCommit != "" && (err == nil || !strings.Contains(err.Error(), tt.wantCommit+"(s) on no remote branch")):
				t.Errorf("guard = %v, want a refusal naming %s(s) on no remote branch", err, tt.wantCommit)
			}
		})
	}
}

// TestLeaseReleaseGuard_NoCommitYet_Passes: a checkout with no commit yet —
// HEAD unborn, no branch — holds no commit a reset could take.
func TestLeaseReleaseGuard_NoCommitYet_Passes(t *testing.T) {
	t.Parallel()
	requireGit(t)
	work := t.TempDir()
	gitIn(t, work, "init", "-q")
	fx := newLeaseFixture(t)
	fx.deps.SSH = &localCheckoutSSH{dir: work}
	rec := leaseRecord{PoolHostname: testPoolHost, PoolServiceID: "svc-pool", ContainerID: "c-abc", ContainerNumber: 3, ContainerHostname: nodeHostname(3)}
	if _, err := guardLeaseRelease(context.Background(), fx.deps, rec); err != nil {
		t.Errorf("guard refused a checkout with no commit: %v", err)
	}
}
