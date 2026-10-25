//go:build unix

// Tests for: ops/lease_warm_clone.go and the checkout of lease_checkout.go,
// run by a local shell against real git (docs/spec-workflows.md §8 DP-32): the
// clone zcp starts in a pool's warm container, detached, puts the pool's
// repository at origin/main into the empty /var/www; the next checkout takes it
// when it is a finished clone of its own repository, waits for one still
// running, and removes one of another repository or one that did not finish
// before it checks out — never checking out on top of it.
package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// setsidStub stands in for setsid on a machine without one (macOS): it runs
// its command as the leader of a new session, as util-linux's does.
const setsidStub = `#!/bin/sh
exec perl -e 'use POSIX (); if (POSIX::setsid() < 0) { exit 0 if fork(); POSIX::setsid(); } exec { $ARGV[0] } @ARGV or die "setsid: $!\n";' -- "$@"
`

// slowGitStub is git whose fetch first sleeps $WARM_FETCH_SLEEP seconds: a
// clone still running when the checkout comes.
const slowGitStub = `#!/bin/sh
for a in "$@"; do if [ "$a" = fetch ]; then sleep "${WARM_FETCH_SLEEP:-0}"; break; fi; done
exec REALGIT "$@"
`

// warmCloneBox is a pool container played by a local shell: a /var/www holding
// what the image left (stale files and baked, ignored dependencies), a $HOME,
// and a directory of stubs put first on PATH.
type warmCloneBox struct {
	dir, home, stubs string
	shell            string
}

func newWarmCloneBox(t *testing.T, shell string) *warmCloneBox {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock is not installed")
	}
	root := t.TempDir()
	b := &warmCloneBox{dir: filepath.Join(root, "www"), home: filepath.Join(root, "home"), stubs: filepath.Join(root, "stubs"), shell: shell}
	for _, d := range []string{b.dir, b.home, b.stubs, filepath.Join(b.dir, "node_modules")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"stale.txt": "image", "node_modules/dep.js": "baked"} {
		if err := os.WriteFile(filepath.Join(b.dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := exec.LookPath("setsid"); err != nil {
		if _, err := exec.LookPath("perl"); err != nil {
			t.Skip("neither setsid nor perl is installed")
		}
		if err := os.WriteFile(filepath.Join(b.stubs, "setsid"), []byte(setsidStub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

// slowGit puts a git whose fetch sleeps for seconds first on PATH (dir).
func slowGit(t *testing.T) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(strings.ReplaceAll(slowGitStub, "REALGIT", shellQuote(gitPath))), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// run runs command as the container's ssh session would: $HOME the box's, no
// git config of this machine, extra directories first on PATH (the stubs
// always), and extra env.
func (b *warmCloneBox) run(t *testing.T, command string, pathFirst []string, env ...string) (string, error) {
	t.Helper()
	path := append(append([]string{b.stubs}, pathFirst...), filepath.SplitList(os.Getenv("PATH"))...)
	return b.runPath(t, command, path, env...)
}

func (b *warmCloneBox) runPath(t *testing.T, command string, path []string, env ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, b.shell, "-c", command) //nolint:gosec // a test shell running the command under test
	cmd.Env = append([]string{
		"HOME=" + b.home, "PATH=" + strings.Join(path, string(os.PathListSeparator)),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	}, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// launch starts the warm clone of repo and returns its answer.
func (b *warmCloneBox) launch(t *testing.T, repo string, pathFirst []string, env ...string) string {
	t.Helper()
	out, err := b.run(t, leaseWarmCloneCommand(b.dir, repo, ""), pathFirst, env...)
	if err != nil {
		t.Fatalf("the warm clone's launch failed: %v\n%s", err, out)
	}
	return out
}

// state is the clone's state file, "" when there is none.
func (b *warmCloneBox) state() string {
	raw, err := os.ReadFile(filepath.Join(b.dir, ".git", leaseWarmStateFile))
	if err != nil {
		return ""
	}
	return string(raw)
}

// waitFor polls until cond holds, failing after 30 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func hasLine(text, line string) bool { return slices.Contains(strings.Split(text, "\n"), line) }

// cloned waits for the detached clone to finish.
func (b *warmCloneBox) cloned(t *testing.T) {
	t.Helper()
	waitFor(t, "the warm clone to finish", func() bool { return hasLine(b.state(), "done") })
}

// checkout runs the lease's checkout of branch of repo and returns its answer.
func (b *warmCloneBox) checkout(t *testing.T, repo, branch string) string {
	t.Helper()
	out, err := b.run(t, leaseCheckoutCommand(b.dir, repo, branch, "", ""), nil)
	if err != nil {
		t.Fatalf("checkout failed: %v\n%s", err, out)
	}
	return out
}

func (b *warmCloneBox) exists(name string) bool {
	_, err := os.Lstat(filepath.Join(b.dir, name))
	return err == nil
}

func (b *warmCloneBox) git(t *testing.T, args ...string) string {
	t.Helper()
	return gitIn(t, b.dir, args...)
}

// warmOrigin is a bare repository whose main holds files and whose feat adds
// feat.txt.
func warmOrigin(t *testing.T, files map[string]string) (origin, mainSHA string) {
	t.Helper()
	origin = filepath.Join(t.TempDir(), "origin.git")
	work := t.TempDir()
	gitIn(t, work, "init", "-q", "-b", "main")
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(work, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, work, "add", "-A", "-f")
	gitIn(t, work, "commit", "-q", "-m", "main")
	mainSHA = gitIn(t, work, "rev-parse", "HEAD")
	gitIn(t, work, "checkout", "-q", "-b", "feat")
	if err := os.WriteFile(filepath.Join(work, "feat.txt"), []byte("f"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "add", "-A")
	gitIn(t, work, "commit", "-q", "-m", "feat")
	gitIn(t, filepath.Dir(origin), "init", "-q", "--bare", "-b", "main", origin)
	gitIn(t, work, "push", "-q", origin, "main", "feat")
	return origin, mainSHA
}

// appFiles is a repository that ignores the dependencies the image bakes.
var appFiles = map[string]string{".gitignore": "node_modules/\n", "README": "r", "app.txt": "a", "src/main.js": "m"}

// TestLeaseWarmClone_LeavesNoEntryRacilyClean: a finished clone's index is
// written in a later second than every file it checked out, so the checkout
// that takes it re-hashes nothing (git compares whole seconds: a file whose
// mtime is the index's own second is racily clean and is read again). Live, on
// z3's 24k files, the first checkout after a clone spent 2.3 s and its clean
// 1.3 s re-reading such files for a one-file branch change (2026-10-24).
func TestLeaseWarmClone_LeavesNoEntryRacilyClean(t *testing.T) {
	t.Parallel()
	origin, _ := warmOrigin(t, appFiles)
	b := newWarmCloneBox(t, "sh")

	b.launch(t, origin, nil)
	b.cloned(t)

	index, err := os.Stat(filepath.Join(b.dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	for name := range strings.FieldsSeq(b.git(t, "ls-files")) {
		info, err := os.Stat(filepath.Join(b.dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.ModTime().Unix() >= index.ModTime().Unix() {
			t.Errorf("%s was written in the index's second (%s, index %s): racily clean", name, info.ModTime(), index.ModTime())
		}
	}
}

// TestLeaseWarmClone_ThenCheckout_TakesTheClone: the warm clone puts the
// pool's repository at origin/main into /var/www, detached, leaving the
// image's baked dependencies; the checkout that follows takes it — it answers
// so, checks the branch out tracking origin, leaves no local main behind,
// cleans what the image left like any checkout, and drops the clone's state.
func TestLeaseWarmClone_ThenCheckout_TakesTheClone(t *testing.T) {
	t.Parallel()
	for _, shell := range posixShells() {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			origin, mainSHA := warmOrigin(t, appFiles)
			b := newWarmCloneBox(t, shell)

			if out := b.launch(t, origin, nil); !strings.Contains(out, "@@zcp-warm started") {
				t.Fatalf("launch answered %q, want started", out)
			}
			b.cloned(t)
			st := b.state()
			if !hasLine(st, "repo "+leaseWarmRepoKey(origin)) || !hasLine(st, "commit "+mainSHA) {
				t.Errorf("state = %q, want the repository's key and main's commit", st)
			}
			if got := b.git(t, "rev-parse", "HEAD"); got != mainSHA {
				t.Errorf("the warm clone's HEAD = %s, want origin/main %s", got, mainSHA)
			}
			if branches := b.git(t, "branch", "--list"); !strings.Contains(branches, "detached") || strings.Count(branches, "\n") > 0 {
				t.Errorf("the warm clone's branches = %q, want only a detached HEAD", branches)
			}
			if !b.exists("src/main.js") || !b.exists("node_modules/dep.js") {
				t.Error("the warm clone must hold main's files beside the image's baked dependencies")
			}

			out := b.checkout(t, origin, "feat")
			if !strings.Contains(out, leaseWarmCheckoutMarker+leaseWarmUsed) {
				t.Errorf("checkout answered %q, want it to say it took the warm clone", out)
			}
			if got := b.git(t, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat" {
				t.Errorf("branch = %s, want feat", got)
			}
			if got := b.git(t, "rev-parse", "--abbrev-ref", "feat@{upstream}"); got != "origin/feat" {
				t.Errorf("upstream = %s, want origin/feat", got)
			}
			if got := b.git(t, "branch", "--list", "main"); got != "" {
				t.Errorf("a local main is left behind: %q", got)
			}
			if !b.exists("feat.txt") || b.exists("stale.txt") || !b.exists("node_modules/dep.js") {
				t.Errorf("tree after the checkout: feat.txt %v (want), stale.txt %v (want gone), node_modules/dep.js %v (want kept)",
					b.exists("feat.txt"), b.exists("stale.txt"), b.exists("node_modules/dep.js"))
			}
			if b.state() != "" {
				t.Error("the checkout must drop the warm clone's state once it took it")
			}
			if s := b.git(t, "status", "--porcelain"); s != "" {
				t.Errorf("the checkout is not clean: %q", s)
			}
		})
	}
}

// TestLeaseWarmClone_CheckoutOfAnotherRepository_RemovesItFirst: a finished
// warm clone of another repository is removed before the checkout — every
// file it checked out, even one the new repository ignores (a plain checkout
// over it would keep that one), and its .git — while the image's baked
// dependencies stay.
func TestLeaseWarmClone_CheckoutOfAnotherRepository_RemovesItFirst(t *testing.T) {
	t.Parallel()
	for _, shell := range posixShells() {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			other, _ := warmOrigin(t, map[string]string{".gitignore": "node_modules/\n", "a.log": "a", "a.txt": "a", "lib/a.js": "a"})
			mine, mineSHA := warmOrigin(t, map[string]string{".gitignore": "node_modules/\n*.log\n", "b.txt": "b"})
			b := newWarmCloneBox(t, shell)
			b.launch(t, other, nil)
			b.cloned(t)

			out := b.checkout(t, mine, "main")

			if !strings.Contains(out, leaseWarmCheckoutMarker+leaseWarmDiscarded) {
				t.Errorf("checkout answered %q, want it to say it removed the warm clone", out)
			}
			for _, gone := range []string{"a.log", "a.txt", "lib/a.js"} {
				if b.exists(gone) {
					t.Errorf("%s of the other repository survived the checkout", gone)
				}
			}
			if !b.exists("b.txt") || !b.exists("node_modules/dep.js") {
				t.Error("want the checked-out repository's files and the image's baked dependencies")
			}
			if got := b.git(t, "rev-parse", "HEAD"); got != mineSHA {
				t.Errorf("HEAD = %s, want %s", got, mineSHA)
			}
			if got := b.git(t, "remote", "get-url", "origin"); got != mine {
				t.Errorf("origin = %s, want %s", got, mine)
			}
			if b.state() != "" {
				t.Error("the warm clone's state must be gone")
			}
		})
	}
}

// TestLeaseWarmClone_Unfinished_RemovedBeforeTheCheckout: a warm clone that
// stopped before it finished — during its fetch (no commit yet) or during its
// checkout (a commit, no done) — is removed, and the checkout then runs as on
// an empty /var/www, never on top of it.
func TestLeaseWarmClone_Unfinished_RemovedBeforeTheCheckout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		keep func(state string) string
	}{
		{"stopped during its fetch", func(st string) string {
			var keep []string
			for line := range strings.SplitSeq(st, "\n") {
				if strings.HasPrefix(line, "repo ") || strings.HasPrefix(line, "pid ") {
					keep = append(keep, line)
				}
			}
			return strings.Join(keep, "\n") + "\n"
		}},
		{"stopped during its checkout", func(st string) string { return strings.Replace(st, "done\n", "", 1) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			origin, _ := warmOrigin(t, appFiles)
			b := newWarmCloneBox(t, "sh")
			b.launch(t, origin, nil)
			b.cloned(t)
			if err := os.WriteFile(filepath.Join(b.dir, ".git", leaseWarmStateFile), []byte(tt.keep(b.state())), 0o644); err != nil {
				t.Fatal(err)
			}
			// What a checkout killed half-way leaves: a file of main it had
			// written, no longer in any index.
			if err := os.Remove(filepath.Join(b.dir, ".git", "index")); err != nil {
				t.Fatal(err)
			}

			out := b.checkout(t, origin, "feat")

			if !strings.Contains(out, leaseWarmCheckoutMarker+leaseWarmDiscarded) {
				t.Errorf("checkout answered %q, want it to say it removed the unfinished clone", out)
			}
			if got := b.git(t, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat" {
				t.Errorf("branch = %s, want feat", got)
			}
			if !b.exists("feat.txt") || !b.exists("src/main.js") || b.exists("stale.txt") || !b.exists("node_modules/dep.js") {
				t.Error("the tree must be the branch's, with the image's baked dependencies")
			}
			if s := b.git(t, "status", "--porcelain"); s != "" {
				t.Errorf("the checkout is not clean: %q", s)
			}
		})
	}
}

// TestLeaseWarmClone_Running_CheckoutWaitsForIt: a checkout that finds a warm
// clone of its own repository still running waits for it to finish, then
// takes it — the clone already did most of what the checkout would.
func TestLeaseWarmClone_Running_CheckoutWaitsForIt(t *testing.T) {
	t.Parallel()
	origin, _ := warmOrigin(t, appFiles)
	b := newWarmCloneBox(t, "sh")
	b.launch(t, origin, []string{slowGit(t)}, "WARM_FETCH_SLEEP=2")
	waitFor(t, "the warm clone to hold /var/www", func() bool { return hasLine(b.state(), "repo "+leaseWarmRepoKey(origin)) })

	begun := time.Now()
	out := b.checkout(t, origin, "feat")

	if took := time.Since(begun); took < time.Second {
		t.Errorf("the checkout took %s: it did not wait for the running clone", took)
	}
	if !strings.Contains(out, leaseWarmCheckoutMarker+leaseWarmUsed) {
		t.Errorf("checkout answered %q, want it to have taken the clone it waited for", out)
	}
	if got := b.git(t, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat" || !b.exists("feat.txt") {
		t.Errorf("branch = %s, want feat checked out", got)
	}
}

// TestLeaseWarmClone_RunningForAnotherRepository_CheckoutStopsIt: a warm clone
// of another repository still running is stopped — its whole process group —
// rather than waited for, then removed, and the checkout runs.
func TestLeaseWarmClone_RunningForAnotherRepository_CheckoutStopsIt(t *testing.T) {
	t.Parallel()
	other, _ := warmOrigin(t, map[string]string{"a.txt": "a"})
	mine, mineSHA := warmOrigin(t, appFiles)
	b := newWarmCloneBox(t, "sh")
	b.launch(t, other, []string{slowGit(t)}, "WARM_FETCH_SLEEP=60")
	waitFor(t, "the warm clone to hold /var/www", func() bool { return hasLine(b.state(), "repo "+leaseWarmRepoKey(other)) })

	begun := time.Now()
	out := b.checkout(t, mine, "main")

	if took := time.Since(begun); took > 20*time.Second {
		t.Errorf("the checkout took %s: it waited for a clone of another repository", took)
	}
	if !strings.Contains(out, leaseWarmCheckoutMarker+leaseWarmDiscarded) {
		t.Errorf("checkout answered %q, want it to say it removed the clone", out)
	}
	if got := b.git(t, "rev-parse", "HEAD"); got != mineSHA {
		t.Errorf("HEAD = %s, want %s", got, mineSHA)
	}
	if b.exists("a.txt") {
		t.Error("a file of the other repository survived")
	}
}

// TestLeaseWarmClone_TreeAlreadyARepository_LeftAlone: a /var/www that already
// holds a repository — a checkout claimed it, or a clone is there — is never
// cloned into: the launch says so and changes nothing.
func TestLeaseWarmClone_TreeAlreadyARepository_LeftAlone(t *testing.T) {
	t.Parallel()
	origin, _ := warmOrigin(t, appFiles)
	b := newWarmCloneBox(t, "sh")
	b.checkout(t, origin, "feat")
	head := b.git(t, "rev-parse", "HEAD")

	out := b.launch(t, origin, nil)

	if !strings.Contains(out, "@@zcp-warm present") {
		t.Errorf("launch answered %q, want present", out)
	}
	time.Sleep(300 * time.Millisecond)
	if b.state() != "" || b.git(t, "rev-parse", "HEAD") != head || b.git(t, "rev-parse", "--abbrev-ref", "HEAD") != "feat" {
		t.Error("the launch changed a checkout it must leave alone")
	}
}

// TestLeaseWarmClone_ImageWithoutTheTools_StartsNothing: an image without
// flock (or git, or setsid) gets no clone: nothing could keep a checkout from
// running beside it.
func TestLeaseWarmClone_ImageWithoutTheTools_StartsNothing(t *testing.T) {
	t.Parallel()
	origin, _ := warmOrigin(t, appFiles)
	b := newWarmCloneBox(t, "sh")

	out, err := b.runPath(t, leaseWarmCloneCommand(b.dir, origin, ""), append([]string{b.stubs}, pathWithout("flock")...))
	if err != nil {
		t.Fatalf("launch: %v\n%s", err, out)
	}

	if !strings.Contains(out, "@@zcp-warm tools") {
		t.Errorf("launch answered %q, want tools", out)
	}
	time.Sleep(300 * time.Millisecond)
	if b.exists(".git") {
		t.Error("a clone was started without flock")
	}
}

// gitInput runs git in dir with stdin and returns its trimmed output.
func gitInput(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// climbingOrigin is a bare repository whose main holds app.txt and a tree
// entry named `..` holding victim — `../victim` to git ls-tree -r. git's own
// commands never write such a tree, but a hostile server can serve one: a
// fetch checks no object by default (review R4-3, reproduced on git 2.50.1).
func climbingOrigin(t *testing.T) string {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitIn(t, filepath.Dir(origin), "init", "-q", "--bare", "-b", "main", origin)
	blob := gitInput(t, origin, "v", "hash-object", "-w", "--stdin")
	app := gitInput(t, origin, "a", "hash-object", "-w", "--stdin")
	inner := gitInput(t, origin, "100644 blob "+blob+"\tvictim\n", "mktree")
	tree := gitInput(t, origin, "040000 tree "+inner+"\t..\n100644 blob "+app+"\tapp.txt\n", "mktree")
	commit := gitInput(t, origin, "", "commit-tree", tree, "-m", "climb")
	gitIn(t, origin, "update-ref", "refs/heads/main", commit)
	return origin
}

// TestLeaseWarmClone_DiscardNeverDeletesOutsideTheWorkTree: a checkout that
// removes a warm clone removes only files inside /var/www (review R4-3). A
// tree whose entries climb out with `..` — the clone fetched it, its checkout
// refused it, so no `done` — and a directory the image left as a symlink out
// of /var/www both name files outside it; neither is deleted, and the
// checkout still runs.
func TestLeaseWarmClone_DiscardNeverDeletesOutsideTheWorkTree(t *testing.T) {
	t.Parallel()
	for _, shell := range posixShells() {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			discardStaysInside(t, shell)
		})
	}
}

func discardStaysInside(t *testing.T, shell string) {
	t.Helper()
	t.Run("a tree entry climbing out with ..", func(t *testing.T) {
		t.Parallel()
		evil := climbingOrigin(t)
		mine, mineSHA := warmOrigin(t, appFiles)
		b := newWarmCloneBox(t, shell)
		victim := filepath.Join(filepath.Dir(b.dir), "victim")
		if err := os.WriteFile(victim, []byte("outside /var/www"), 0o644); err != nil {
			t.Fatal(err)
		}
		b.launch(t, evil, nil)
		waitFor(t, "the warm clone to record its commit", func() bool { return strings.Contains(b.state(), "commit ") })

		out := b.checkout(t, mine, "main")

		if _, err := os.Stat(victim); err != nil {
			t.Errorf("the discard deleted %s, outside the work tree: %v", victim, err)
		}
		if !strings.Contains(out, leaseWarmCheckoutMarker+leaseWarmDiscarded) {
			t.Errorf("checkout answered %q, want it to say it removed the clone", out)
		}
		if got := b.git(t, "rev-parse", "HEAD"); got != mineSHA {
			t.Errorf("HEAD = %s, want %s", got, mineSHA)
		}
	})
	t.Run("a directory the image left as a symlink out of the work tree", func(t *testing.T) {
		t.Parallel()
		other, _ := warmOrigin(t, map[string]string{"link/secret.txt": "s", "a.txt": "a"})
		mine, mineSHA := warmOrigin(t, appFiles)
		b := newWarmCloneBox(t, shell)
		outside := filepath.Join(filepath.Dir(b.dir), "outside")
		if err := os.MkdirAll(outside, 0o755); err != nil {
			t.Fatal(err)
		}
		secret := filepath.Join(outside, "secret.txt")
		if err := os.WriteFile(secret, []byte("outside /var/www"), 0o644); err != nil {
			t.Fatal(err)
		}
		// A clone of other stopped before its checkout wrote anything: its
		// .git holds the commit, and the image's symlink is still there.
		b.git(t, "init", "-q")
		b.git(t, "fetch", "-q", other, "main")
		c := b.git(t, "rev-parse", "FETCH_HEAD")
		if err := os.WriteFile(filepath.Join(b.dir, ".git", leaseWarmStateFile),
			[]byte("repo "+leaseWarmRepoKey(other)+"\npid 999999\ncommit "+c+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(b.dir, "link")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(b.dir, "a.txt"), []byte("a"), 0o644); err != nil {
			t.Fatal(err)
		}

		out := b.checkout(t, mine, "main")

		if _, err := os.Stat(secret); err != nil {
			t.Errorf("the discard deleted %s through the symlink link: %v", secret, err)
		}
		if b.exists("a.txt") {
			t.Error("a file of the discarded clone inside the work tree survived")
		}
		if !strings.Contains(out, leaseWarmCheckoutMarker+leaseWarmDiscarded) {
			t.Errorf("checkout answered %q, want it to say it removed the clone", out)
		}
		if got := b.git(t, "rev-parse", "HEAD"); got != mineSHA {
			t.Errorf("HEAD = %s, want %s", got, mineSHA)
		}
	})
}
