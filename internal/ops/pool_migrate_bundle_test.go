// Tests for: ops/pool_migrate_bundle.go — the retire's bundle of a dev
// service's repository: every ref, every stash entry, every reflog tip, every
// worktree's HEAD, a WIP commit of each tree's index and tracked files and a
// commit of its untracked files, streamed over stdout only and verified on
// receipt, the source left as it was (docs/spec-workflows.md §13 PM-3). Every
// test runs the real scripts against a real git repository in t.TempDir().
package ops

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// gitObjectCommit is what `git cat-file -t` names a commit.
const gitObjectCommit = "commit"

// bundleLocalSSH is the dev service: each command runs in a local shell, as
// ssh hands it to the dev's shell. ExecSSH answers the combined output and
// StreamSSH streams stdout alone, the way the platform's ssh does; stream,
// when set, rewrites what reaches the writer (a transport that corrupts).
type bundleLocalSSH struct {
	mu      sync.Mutex
	calls   []string
	streams int
	stream  func(io.Writer) io.Writer
}

func (s *bundleLocalSSH) record(call string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
}

func (s *bundleLocalSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	s.record("exec " + hostname + ": " + command)
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, &platform.SSHExecError{Hostname: hostname, Output: string(out), Err: err}
	}
	return out, nil
}

func (s *bundleLocalSSH) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return nil, errors.New("the bundle never runs a background command")
}

func (s *bundleLocalSSH) StreamSSH(ctx context.Context, hostname, command string, stdout io.Writer) error {
	s.record("stream " + hostname + ": " + command)
	s.mu.Lock()
	s.streams++
	wrap := s.stream
	s.mu.Unlock()
	if wrap != nil {
		stdout = wrap(stdout)
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	cmd.Stdout = stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return &platform.SSHExecError{Hostname: hostname, Output: stderr.String(), Err: err}
	}
	return nil
}

// execOnlySSH is an SSHDeployer that cannot stream: it records every call.
type execOnlySSH struct{ calls []string }

func (s *execOnlySSH) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	s.calls = append(s.calls, hostname+": "+command)
	return nil, errors.New("not reached")
}

func (s *execOnlySSH) ExecSSHBackground(_ context.Context, hostname, command string, _ time.Duration) ([]byte, error) {
	s.calls = append(s.calls, hostname+": "+command)
	return nil, errors.New("not reached")
}

// truncating passes on only the first n bytes of what is written to it.
type truncating struct {
	w io.Writer
	n int
}

func (t *truncating) Write(p []byte) (int, error) {
	if t.n <= 0 {
		return len(p), nil
	}
	keep := p[:min(len(p), t.n)]
	t.n -= len(keep)
	if _, err := t.w.Write(keep); err != nil {
		return 0, err
	}
	return len(p), nil
}

func bundleGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := censusGitRun(dir, args...)
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return out
}

// bundleFixture is a dev service's repository holding work in every place it
// hides, built the way PROVE P3 did: main pushed; feat and mate-kept/x on no
// remote; a commit only main's reflog reaches; three stash entries, the
// newest with a staged new file; refs/t3 refs; a linked worktree inside the
// main checkout with a commit and staged, unstaged and untracked edits; a
// detached worktree outside it with a commit only it holds and one only its
// own HEAD reflog reaches; a worktree whose directory is gone; and in the
// main tree a staged edit, a staged new file, an unstaged edit, untracked
// files (one with a space, one a symlink), an untracked repository of its
// own, two .env files and ignored files. It returns the base directory, the
// main checkout and the commits only a reflog reaches.
func bundleFixture(t *testing.T) (base, root string, reflogOnly []string) {
	t.Helper()
	base = censusBase(t)
	root = censusCleanRepo(t, base)
	g := func(args ...string) { t.Helper(); censusGit(t, root, args...) }
	file := func(name string) string { return filepath.Join(root, name) }

	g("switch", "-q", "-c", "feat")
	censusWrite(t, file("feat.txt"), "feat1\n")
	g("add", "feat.txt")
	g("commit", "-q", "-m", "feat-1")
	censusAppend(t, file("feat.txt"), "feat2\n")
	g("commit", "-q", "-am", "feat-2")
	g("switch", "-q", "-c", "mate-kept/x", "main")
	censusWrite(t, file("kept.txt"), "kept\n")
	g("add", "kept.txt")
	g("commit", "-q", "-m", "kept-1")
	g("switch", "-q", "main")

	censusWrite(t, file("lost.txt"), "lost\n")
	g("add", "lost.txt")
	g("commit", "-q", "-m", "reflog-only")
	reflogOnly = append(reflogOnly, strings.TrimSpace(bundleGitOut(t, root, "rev-parse", "HEAD")))
	g("reset", "-q", "--hard", "HEAD~1")

	censusAppend(t, file("app.txt"), "s1\n")
	g("stash", "push", "-q", "-m", "stash-one")
	censusAppend(t, file("lib.txt"), "s2\n")
	g("stash", "push", "-q", "-m", "stash-two")
	censusWrite(t, file("s3new.txt"), "s3\n")
	g("add", "s3new.txt")
	censusAppend(t, file("app.txt"), "s3b\n")
	g("stash", "push", "-q", "-m", "stash-three staged-new-file")

	g("update-ref", "refs/t3/crew/saved/ada", "feat")
	g("update-ref", "refs/t3/checkpoints/thread-1/turn/1", "main")

	g("worktree", "add", "-q", "-b", "one", "trees/one", "main")
	one := file("trees/one")
	censusWrite(t, filepath.Join(one, "one.txt"), "one1\n")
	censusGit(t, one, "add", "one.txt")
	censusGit(t, one, "commit", "-q", "-m", "one-1")
	censusAppend(t, filepath.Join(one, "lib.txt"), "one-staged\n")
	censusGit(t, one, "add", "lib.txt")
	censusAppend(t, filepath.Join(one, "app.txt"), "one-unstaged\n")
	censusWrite(t, filepath.Join(one, "one-new.txt"), "one untracked\n")

	det := filepath.Join(base, "det")
	g("worktree", "add", "-q", "--detach", det, "main~1")
	censusWrite(t, filepath.Join(det, "det.txt"), "det1\n")
	censusGit(t, det, "add", "det.txt")
	censusGit(t, det, "commit", "-q", "-m", "detached-only")
	censusWrite(t, filepath.Join(det, "wt-lost.txt"), "wt\n")
	censusGit(t, det, "add", "wt-lost.txt")
	censusGit(t, det, "commit", "-q", "-m", "worktree-reflog-only")
	reflogOnly = append(reflogOnly, strings.TrimSpace(bundleGitOut(t, det, "rev-parse", "HEAD")))
	censusGit(t, det, "reset", "-q", "--hard", "HEAD~1")

	g("worktree", "add", "-q", "-b", "gone", filepath.Join(base, "gone"), "main")
	if err := os.RemoveAll(filepath.Join(base, "gone")); err != nil {
		t.Fatal(err)
	}

	censusAppend(t, file("lib.txt"), "staged-edit\n")
	g("add", "lib.txt")
	censusWrite(t, file("newstaged.txt"), "newstaged\n")
	g("add", "newstaged.txt")
	censusAppend(t, file("app.txt"), "unstaged-edit\n")
	censusWrite(t, file("notes/todo.md"), "todo\n")
	censusWrite(t, file("scratch.txt"), "untracked root\n")
	censusWrite(t, file("my notes.txt"), "a name with a space\n")
	if err := os.Symlink("scratch.txt", file("scratch-link")); err != nil {
		t.Fatal(err)
	}
	censusWrite(t, file(".env.local"), "DUMMY=not-a-secret\n")
	censusWrite(t, file("notes/.env"), "A=1\n")
	censusWrite(t, file("debug.log"), "ignored\n")
	censusWrite(t, file("node_modules/pkg/index.js"), "x\n")
	vendor := file("vendor/lib")
	censusWrite(t, filepath.Join(vendor, "lib.go"), "package lib\n")
	censusGit(t, vendor, "init", "-q", "-b", "main")
	censusGit(t, vendor, "add", "-A")
	censusGit(t, vendor, "commit", "-q", "-m", "vendored")
	return base, root, reflogOnly
}

// buildBundle bundles root into <out>/dev.bundle, the scratch directory on
// the "dev" its own temp directory.
func buildBundle(t *testing.T, ssh SSHDeployer, root string) (*PoolMigrateBundle, string, error) {
	t.Helper()
	out := t.TempDir()
	work := filepath.Join(t.TempDir(), "zcp-pool-migrate")
	b, err := BuildPoolMigrateBundle(context.Background(), ssh, PoolMigrateBundleInput{
		Dev:     "appdev",
		Root:    root,
		WorkDir: work,
		Path:    filepath.Join(out, "dev.bundle"),
	})
	return b, work, err
}

// bundleRefs fetches every ref of the bundle at path into a fresh repository
// under refs/migrated/ and returns them, ref → sha, with the repository.
func bundleRefs(t *testing.T, path string) (map[string]string, string) {
	t.Helper()
	check := filepath.Join(t.TempDir(), "check.git")
	censusGit(t, filepath.Dir(check), "init", "-q", "--bare", check)
	censusGit(t, check, "fetch", "-q", path, "refs/*:refs/migrated/*")
	refs := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(bundleGitOut(t, check, "for-each-ref", "--format=%(objectname) %(refname)", "refs/migrated/")), "\n") {
		sha, ref, _ := strings.Cut(line, " ")
		refs[strings.TrimPrefix(ref, "refs/migrated/")] = sha
	}
	return refs, check
}

func TestPoolMigrateBundle_CarriesEveryRefStashWIPAndReflogTip(t *testing.T) {
	t.Parallel()
	base, root, reflogOnly := bundleFixture(t)
	ssh := &bundleLocalSSH{}
	b, work, err := buildBundle(t, ssh, root)
	if err != nil {
		t.Fatalf("BuildPoolMigrateBundle: %v", err)
	}

	info, err := os.Stat(b.Path)
	if err != nil {
		t.Fatalf("the bundle was not written: %v", err)
	}
	if info.Mode().Perm() != 0o600 || info.Size() != b.Bytes || b.Bytes == 0 || len(b.SHA256) != 64 {
		t.Errorf("bundle file mode %v size %d, reported %d bytes sha %q; want 0600 and the size it reports", info.Mode().Perm(), info.Size(), b.Bytes, b.SHA256)
	}
	if _, err := os.Stat(work); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the scratch directory on the dev service is left behind: %v", err)
	}
	if ssh.streams != 1 {
		t.Errorf("streams = %d, want the bundle streamed once", ssh.streams)
	}

	refs, check := bundleRefs(t, b.Path)
	if len(refs) != b.Refs {
		t.Errorf("bundle carries %d refs, reported %d", len(refs), b.Refs)
	}
	// Every ref of the source, at the same commit.
	for line := range strings.SplitSeq(strings.TrimSpace(bundleGitOut(t, root, "for-each-ref", "--format=%(objectname) %(refname)")), "\n") {
		sha, ref, _ := strings.Cut(line, " ")
		if refs[strings.TrimPrefix(ref, "refs/")] != sha {
			t.Errorf("source ref %s at %s: bundle has %q", ref, sha, refs[strings.TrimPrefix(ref, "refs/")])
		}
	}
	// Every stash entry, newest first, not only stash@{0}.
	stashes := strings.Fields(bundleGitOut(t, root, "stash", "list", "--format=%H"))
	if len(stashes) != 3 || b.Stashes != 3 {
		t.Fatalf("source stashes %d, reported %d; the fixture holds three", len(stashes), b.Stashes)
	}
	for i, sha := range stashes {
		if got := refs["zcp-migrate/stash/"+strconv.Itoa(i)]; got != sha {
			t.Errorf("stash@{%d} = %s, bundle has %q", i, sha, got)
		}
	}
	// Every commit only a reflog reaches, main's and a worktree's own HEAD's.
	for _, sha := range reflogOnly {
		if refs["zcp-migrate/reflog/"+sha] != sha {
			t.Errorf("reflog-only commit %s has no ref in the bundle", sha)
		}
		if got := strings.TrimSpace(bundleGitOut(t, check, "cat-file", "-t", sha)); got != gitObjectCommit {
			t.Errorf("reflog-only commit %s is a %q in the bundle", sha, got)
		}
	}
	if b.ReflogTips == 0 {
		t.Error("no reflog tips reported")
	}

	byPath := map[string]PoolMigrateBundleTree{}
	for _, tr := range b.Trees {
		byPath[tr.Path] = tr
	}
	one, det, gone := filepath.Join(root, "trees", "one"), filepath.Join(base, "det"), filepath.Join(base, "gone")
	if len(b.Trees) != 4 || b.Trees[0].Path != root || b.Trees[0].Branch != "main" {
		t.Fatalf("trees = %+v, want the main checkout first, then three worktrees", b.Trees)
	}
	if tr := byPath[det]; tr.Branch != "" || tr.Head == "" {
		t.Errorf("detached tree = %+v", tr)
	}
	if tr := byPath[gone]; !tr.Missing || tr.WIP || tr.Untracked {
		t.Errorf("gone tree = %+v, want missing with nothing but its HEAD", tr)
	}
	// Each worktree's HEAD, the detached one's above all.
	detHead := strings.TrimSpace(bundleGitOut(t, det, "rev-parse", "HEAD"))
	if got := refs["zcp-migrate/trees/"+strconv.Itoa(byPath[det].Index)+"/head"]; got != detHead {
		t.Errorf("detached HEAD %s, bundle has %q", detHead, got)
	}

	// A WIP commit per tree with tracked work: its tree is the working tree,
	// its second parent the index — what `git diff HEAD` and `git diff
	// --cached` show in the source.
	for _, dir := range []string{root, one} {
		tr := byPath[dir]
		if !tr.WIP {
			t.Fatalf("tree %s has no WIP commit: %+v", dir, tr)
		}
		wip := "refs/migrated/zcp-migrate/trees/" + strconv.Itoa(tr.Index) + "/wip"
		if got, want := bundleGitOut(t, check, "diff", "--binary", wip+"^1", wip), bundleGitOut(t, dir, "diff", "--binary", "HEAD"); got != want {
			t.Errorf("tree %s: WIP working tree differs from the source's diff HEAD:\n%s\n---\n%s", dir, got, want)
		}
		if got, want := bundleGitOut(t, check, "diff", "--binary", wip+"^1", wip+"^2"), bundleGitOut(t, dir, "diff", "--binary", "--cached"); got != want {
			t.Errorf("tree %s: WIP index differs from the source's diff --cached:\n%s\n---\n%s", dir, got, want)
		}
	}
	if byPath[det].WIP {
		t.Error("the clean detached tree got a WIP commit")
	}

	// A commit of each tree's untracked files: never a .env file, an
	// ignored file or a repository of its own.
	untracked := func(tr PoolMigrateBundleTree) []string {
		t.Helper()
		if !tr.Untracked {
			t.Fatalf("tree %s has no untracked commit: %+v", tr.Path, tr)
		}
		ref := "refs/migrated/zcp-migrate/trees/" + strconv.Itoa(tr.Index) + "/untracked"
		files := strings.Split(strings.TrimSpace(bundleGitOut(t, check, "ls-tree", "-r", "--name-only", ref)), "\n")
		slices.Sort(files)
		return files
	}
	if got, want := untracked(byPath[root]), []string{"my notes.txt", "notes/todo.md", "scratch-link", "scratch.txt"}; !slices.Equal(got, want) {
		t.Errorf("main tree's untracked commit = %q, want %q (no .env file, nothing ignored, no nested repository or worktree)", got, want)
	}
	if got, want := untracked(byPath[one]), []string{"one-new.txt"}; !slices.Equal(got, want) {
		t.Errorf("worktree's untracked commit = %q, want %q", got, want)
	}
	if got, want := b.Nested, []string{filepath.Join(root, "vendor", "lib")}; !slices.Equal(got, want) {
		t.Errorf("nested repositories named = %q, want %q (a linked worktree is a tree, not one)", got, want)
	}
	if b.Fingerprint.Digest == "" || len(b.Fingerprint.Parts) == 0 {
		t.Errorf("no fingerprint taken with the bundle: %+v", b.Fingerprint)
	}
}

// bundleSourceSnapshot is the source repository as the retire must leave it:
// every file under base — the raw index bytes and their mtime included — but
// the objects directory, where `git stash create` and the untracked commit
// may add unreferenced objects; refs, reflogs, the stash list and fsck's
// verdict are read besides.
func bundleSourceSnapshot(t *testing.T, base, root string) map[string]string {
	t.Helper()
	snap := censusSnapshot(t, base)
	objects := filepath.Join(root, ".git", "objects") + string(filepath.Separator)
	for p := range snap {
		if strings.HasPrefix(p, objects) || p == strings.TrimSuffix(objects, string(filepath.Separator)) {
			delete(snap, p)
		}
	}
	snap["refs"] = bundleGitOut(t, root, "for-each-ref", "--format=%(objectname) %(refname)")
	snap["stash"] = bundleGitOut(t, root, "stash", "list", "--format=%H %gd %gs")
	snap["reflog"] = bundleGitOut(t, root, "reflog", "show", "--all", "--format=%H %gd %gs")
	snap["fsck"] = bundleGitOut(t, root, "fsck", "--no-dangling", "--no-progress")
	return snap
}

func TestPoolMigrateBundle_SourceUntouched(t *testing.T) {
	t.Parallel()
	base, root, _ := bundleFixture(t)
	// Stale index entries, which a plain `git status` or a plain `git stash
	// create` would rewrite.
	old := time.Now().Add(-72 * time.Hour)
	for _, f := range []string{filepath.Join(root, "lib.txt"), filepath.Join(root, "trees", "one", "lib.txt")} {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}
	before := bundleSourceSnapshot(t, base, root)
	if _, _, err := buildBundle(t, &bundleLocalSSH{}, root); err != nil {
		t.Fatalf("BuildPoolMigrateBundle: %v", err)
	}
	if _, err := ReadPoolMigrateFingerprint(context.Background(), &bundleLocalSSH{}, "appdev", root, filepath.Join(t.TempDir(), "zcp-pool-migrate")); err != nil {
		t.Fatalf("ReadPoolMigrateFingerprint: %v", err)
	}
	after := bundleSourceSnapshot(t, base, root)
	if diff := censusSnapshotDiff(before, after); len(diff) > 0 {
		t.Fatalf("the bundle changed the source repository:\n%s", strings.Join(diff, "\n"))
	}
}

// TestPoolMigrateBundle_RestoresEveryItem runs the restore steps the bundle
// names in a fresh directory and finds every item where the source held it:
// each branch at its commit, the stash list in order, each tree's HEAD, index,
// tracked edits and untracked files — the .env files and ignored files
// excepted, which the bundle never carries.
func TestPoolMigrateBundle_RestoresEveryItem(t *testing.T) {
	t.Parallel()
	base, root, reflogOnly := bundleFixture(t)
	b, _, err := buildBundle(t, &bundleLocalSSH{}, root)
	if err != nil {
		t.Fatalf("BuildPoolMigrateBundle: %v", err)
	}
	steps := PoolMigrateRestoreSteps(b, strings.TrimSpace(bundleGitOut(t, root, "remote", "get-url", "origin")))
	restored := filepath.Join(censusBase(t), "www")
	if err := os.Mkdir(restored, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "set -e\n" + strings.Join(steps, "\n")
	cmd := exec.CommandContext(context.Background(), "sh", "-c", script)
	cmd.Dir = restored
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=r", "GIT_AUTHOR_EMAIL=r@example.com", "GIT_COMMITTER_NAME=r", "GIT_COMMITTER_EMAIL=r@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restore steps failed: %v\n%s\nsteps:\n%s", err, out, script)
	}

	same := func(what, src, dst string, args ...string) {
		t.Helper()
		if got, want := bundleGitOut(t, dst, args...), bundleGitOut(t, src, args...); got != want {
			t.Errorf("%s differs after restore:\n%s\n--- source ---\n%s", what, got, want)
		}
	}
	same("branches", root, restored, "for-each-ref", "--format=%(objectname) %(refname)", "refs/heads/", "refs/t3/")
	same("remote branches", root, restored, "for-each-ref", "--format=%(objectname) %(refname)", "refs/remotes/")
	same("stash list", root, restored, "stash", "list", "--format=%H %gs")
	same("origin", root, restored, "remote", "get-url", "origin")
	for _, sha := range reflogOnly {
		if got := strings.TrimSpace(bundleGitOut(t, restored, "cat-file", "-t", sha)); got != gitObjectCommit {
			t.Errorf("reflog-only commit %s is a %q after restore", sha, got)
		}
	}

	one := filepath.Join(root, "trees", "one")
	pairs := [][2]string{{root, restored}, {one, filepath.Join(restored, "trees", "one")}, {filepath.Join(base, "det"), filepath.Join(filepath.Dir(restored), "det")}}
	for _, p := range pairs {
		src, dst := p[0], p[1]
		same(src+" HEAD", src, dst, "rev-parse", "HEAD")
		same(src+" branch", src, dst, "rev-parse", "--abbrev-ref", "HEAD")
		same(src+" index", src, dst, "diff", "--binary", "--cached")
		same(src+" tracked edits", src, dst, "diff", "--binary")
	}
	for _, name := range []string{"notes/todo.md", "scratch.txt", "my notes.txt", "trees/one/one-new.txt"} {
		want, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(filepath.Join(restored, name)); err != nil || !bytes.Equal(got, want) {
			t.Errorf("untracked %s after restore = %q (%v), want %q", name, got, err, want)
		}
	}
	if target, err := os.Readlink(filepath.Join(restored, "scratch-link")); err != nil || target != "scratch.txt" {
		t.Errorf("untracked symlink after restore = %q (%v)", target, err)
	}
	for _, name := range []string{".env.local", "notes/.env", "debug.log", "node_modules/pkg/index.js"} {
		if _, err := os.Stat(filepath.Join(restored, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s came back from the bundle (err %v): .env and ignored files are never carried", name, err)
		}
	}
}

func TestPoolMigrateFingerprint_UnchangedEqual_EachChangeNamed(t *testing.T) {
	t.Parallel()
	read := func(t *testing.T, root string) PoolMigrateFingerprint {
		t.Helper()
		fp, err := ReadPoolMigrateFingerprint(context.Background(), &bundleLocalSSH{}, "appdev", root, filepath.Join(t.TempDir(), "zcp-pool-migrate"))
		if err != nil {
			t.Fatalf("ReadPoolMigrateFingerprint: %v", err)
		}
		return fp
	}
	tests := []struct {
		name   string
		change func(t *testing.T, base, root string)
		moved  string
	}{
		{"a tracked file edited again", func(t *testing.T, _, root string) {
			t.Helper()
			censusAppend(t, filepath.Join(root, "app.txt"), "more\n")
		}, "the checkout at "},
		{"a worktree's untracked file edited", func(t *testing.T, _, root string) {
			t.Helper()
			censusAppend(t, filepath.Join(root, "trees", "one", "one-new.txt"), "more\n")
		}, filepath.Join("trees", "one")},
		{"a file staged", func(t *testing.T, _, root string) {
			t.Helper()
			censusGit(t, root, "add", "app.txt")
		}, "the checkout at "},
		{"a commit on a branch", func(t *testing.T, _, root string) {
			t.Helper()
			censusGit(t, filepath.Join(root, "trees", "one"), "commit", "-q", "-m", "one-2")
		}, "a ref moved"},
		{"a stash entry dropped", func(t *testing.T, _, root string) {
			t.Helper()
			censusGit(t, root, "stash", "drop", "-q", "stash@{1}")
		}, "a reflog"},
		{"a worktree added", func(t *testing.T, base, root string) {
			t.Helper()
			censusGit(t, root, "worktree", "add", "-q", "--detach", filepath.Join(base, "new"), "main")
		}, "added"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base, root, _ := bundleFixture(t)
			first := read(t, root)
			if again := read(t, root); again.Digest != first.Digest || len(first.Moved(again)) > 0 {
				t.Fatalf("an unchanged repository read another fingerprint: moved %q", first.Moved(again))
			}
			tt.change(t, base, root)
			now := read(t, root)
			moved := first.Moved(now)
			if now.Digest == first.Digest || len(moved) == 0 {
				t.Fatalf("%s: the fingerprint did not change", tt.name)
			}
			if !strings.Contains(strings.Join(moved, "; "), tt.moved) {
				t.Errorf("%s: moved = %q, want it to name %q", tt.name, moved, tt.moved)
			}
		})
	}
}

// TestPoolMigrateBundle_ConflictedTree_Refused: a tree in the middle of a
// merge with a conflict has no WIP commit to save — git refuses to create
// one — so the bundle is refused naming the tree, and nothing is written.
func TestPoolMigrateBundle_ConflictedTree_Refused(t *testing.T) {
	t.Parallel()
	base := censusBase(t)
	root := censusCleanRepo(t, base)
	censusGit(t, root, "switch", "-q", "-c", "other")
	censusWrite(t, filepath.Join(root, "app.txt"), "other\n")
	censusGit(t, root, "commit", "-q", "-am", "other")
	censusGit(t, root, "switch", "-q", "main")
	censusWrite(t, filepath.Join(root, "app.txt"), "mine\n")
	censusGit(t, root, "commit", "-q", "-am", "mine")
	if out, err := censusGitRun(root, "merge", "other"); err == nil {
		t.Fatalf("the merge was meant to stop on a conflict:\n%s", out)
	}
	b, _, err := buildBundle(t, &bundleLocalSSH{}, root)
	if err == nil {
		t.Fatalf("a conflicted tree was bundled: %+v", b)
	}
	if !strings.Contains(err.Error(), root) {
		t.Errorf("error = %v, want it to name the tree", err)
	}
}

func TestPoolMigrateBundle_StreamCutShort_VerifyFails_NothingKept(t *testing.T) {
	t.Parallel()
	_, root, _ := bundleFixture(t)
	ssh := &bundleLocalSSH{stream: func(w io.Writer) io.Writer { return &truncating{w: w, n: 2000} }}
	out := t.TempDir()
	path := filepath.Join(out, "dev.bundle")
	_, err := BuildPoolMigrateBundle(context.Background(), ssh, PoolMigrateBundleInput{
		Dev: "appdev", Root: root, WorkDir: filepath.Join(t.TempDir(), "zcp-pool-migrate"), Path: path,
	})
	if err == nil {
		t.Fatal("a bundle cut short in transit verified")
	}
	entries, rerr := os.ReadDir(out)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("an unverified bundle is left behind: %q", names)
	}
}

func TestPoolMigrateBundle_NoStreamingSSH_Refused(t *testing.T) {
	t.Parallel()
	_, root, _ := bundleFixture(t)
	ssh := &execOnlySSH{}
	_, _, err := buildBundle(t, ssh, root)
	if err == nil || !strings.Contains(err.Error(), "stream") {
		t.Fatalf("BuildPoolMigrateBundle without a streaming ssh = %v, want a refusal naming the stream", err)
	}
	if len(ssh.calls) > 0 {
		t.Errorf("the dev service was touched: %q", ssh.calls)
	}
}

func TestParsePoolMigrateBundle_CutShortOrFailed_Error(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, out, want string
	}{
		{"no header", "bash: git: command not found\n", "no bundle"},
		{"no end", "zcp-bundle 1\nW worktree /var/www\n", "incomplete"},
		{"a step failed", "zcp-bundle 1\nzcp-bundle-error tree /var/www\nfatal: needs merge\n", "tree /var/www"},
		{"moved while bundling", "zcp-bundle 1\nW worktree /var/www\nF R 1111 refs/heads/main\nG R 2222 refs/heads/main\nzcp-bundle-error moved\n", "a ref moved"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parsePoolMigrateBundle(tt.out)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parse = %v, want an error naming %q", err, tt.want)
			}
		})
	}
}
