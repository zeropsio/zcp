// Tests for: ops/pool_migrate_census.go — the unsaved-work census of a dev
// service's repository, read in one ssh round trip and changing nothing
// (docs/spec-workflows.md §13 PM-2). Every census test runs the real script
// against a real git repository in t.TempDir().
package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// censusLocalSSH runs each command in a local shell, as ssh hands it to the
// dev service's shell, and fails the way the platform's ssh does: a non-zero
// exit is an *platform.SSHExecError carrying the combined output.
type censusLocalSSH struct {
	mu    sync.Mutex
	calls []string
}

func (s *censusLocalSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	s.mu.Lock()
	s.calls = append(s.calls, hostname+": "+command)
	s.mu.Unlock()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, &platform.SSHExecError{Hostname: hostname, Output: string(out), Err: err}
	}
	return out, nil
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *censusLocalSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *censusLocalSSH) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return nil, errors.New("the census never runs a background command")
}

// censusGit runs git in dir with a fixed identity and no user config; it
// fails the test unless git succeeds.
func censusGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := censusGitRun(dir, args...); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func censusGitRun(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func censusWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func censusAppend(t *testing.T, path, body string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(body); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// censusBase is a fresh directory with symlinks resolved, so the paths git
// prints for worktrees compare equal to the ones the test builds.
func censusBase(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return base
}

// censusCleanRepo is <base>/www: a checkout of <base>/origin.git with main
// pushed and tracked, nothing else.
func censusCleanRepo(t *testing.T, base string) string {
	t.Helper()
	root := filepath.Join(base, "www")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	censusGit(t, root, "init", "-q", "-b", "main")
	censusWrite(t, filepath.Join(root, ".gitignore"), "node_modules/\n*.log\n")
	censusWrite(t, filepath.Join(root, "app.txt"), "v1\n")
	censusWrite(t, filepath.Join(root, "lib.txt"), "lib1\n")
	censusGit(t, root, "add", "-A")
	censusGit(t, root, "commit", "-q", "-m", "c1 main")
	censusAppend(t, filepath.Join(root, "app.txt"), "v2\n")
	censusGit(t, root, "commit", "-q", "-am", "c2 main")
	origin := filepath.Join(base, "origin.git")
	censusGit(t, base, "init", "-q", "--bare", "-b", "main", origin)
	censusGit(t, root, "remote", "add", "origin", origin)
	censusGit(t, root, "push", "-q", "-u", "origin", "main")
	return root
}

// censusFixture is every place a dev service's work can hide, the way the
// dev service holds it at /var/www: main pushed; feat and mate-kept/x
// committed on no remote; a commit only the main checkout's reflog reaches;
// three stash entries; refs/t3 refs; a crew worktree with staged, unstaged
// and untracked edits; a detached worktree with a commit only it holds and
// one only its own HEAD reflog reaches; a worktree whose directory is gone; a
// worktree mid-merge with a conflict; and in the main tree a staged edit, a
// staged new file, an unstaged edit, untracked files, two .env files and
// ignored files.
func censusFixture(t *testing.T) (base, root string) {
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
	g("reset", "-q", "--hard", "HEAD~1")

	censusAppend(t, file("app.txt"), "s1\n")
	g("stash", "push", "-q", "-m", "stash-one")
	censusAppend(t, file("lib.txt"), "s2\n")
	g("stash", "push", "-q", "-m", "stash-two")
	censusWrite(t, file("s3new.txt"), "s3\n")
	g("add", "s3new.txt")
	censusAppend(t, file("app.txt"), "s3b\n")
	g("stash", "push", "-q", "-m", "stash-three")

	g("update-ref", "refs/t3/crew/saved/ada", "feat")
	g("update-ref", "refs/t3/checkpoints/thread-1/turn/1", "main")
	g("update-ref", "refs/t3/checkpoints/thread-1/turn/2", "main")

	censusAppend(t, filepath.Join(root, ".git", "info", "exclude"), ".crew/\n")
	g("worktree", "add", "-q", "-b", "crew/ada", ".crew/ada", "main")
	ada := file(".crew/ada")
	censusWrite(t, filepath.Join(ada, "ada.txt"), "ada1\n")
	censusGit(t, ada, "add", "ada.txt")
	censusGit(t, ada, "commit", "-q", "-m", "ada-1")
	censusAppend(t, filepath.Join(ada, "lib.txt"), "ada-staged\n")
	censusGit(t, ada, "add", "lib.txt")
	censusAppend(t, filepath.Join(ada, "app.txt"), "ada-unstaged\n")
	censusWrite(t, filepath.Join(ada, "ada-new.txt"), "x\n")

	g("worktree", "add", "-q", "--detach", ".crew/det", "main~1")
	det := file(".crew/det")
	censusWrite(t, filepath.Join(det, "det.txt"), "det1\n")
	censusGit(t, det, "add", "det.txt")
	censusGit(t, det, "commit", "-q", "-m", "detached-only")
	// A commit only this worktree's HEAD reflog still reaches.
	censusWrite(t, filepath.Join(det, "wt-lost.txt"), "wt\n")
	censusGit(t, det, "add", "wt-lost.txt")
	censusGit(t, det, "commit", "-q", "-m", "worktree-reflog-only")
	censusGit(t, det, "reset", "-q", "--hard", "HEAD~1")

	g("worktree", "add", "-q", "-b", "crew/gone", ".crew/gone", "main")
	if err := os.RemoveAll(file(".crew/gone")); err != nil {
		t.Fatal(err)
	}

	merging := filepath.Join(base, "merging")
	g("worktree", "add", "-q", "-b", "merging", merging, "main")
	censusWrite(t, filepath.Join(merging, "app.txt"), "conflict A\n")
	censusGit(t, merging, "commit", "-q", "-am", "app A")
	g("branch", "-q", "appB", "main")
	g("switch", "-q", "appB")
	censusWrite(t, file("app.txt"), "conflict B\n")
	g("commit", "-q", "-am", "app B")
	g("switch", "-q", "main")
	if out, err := censusGitRun(merging, "merge", "appB"); err == nil {
		t.Fatalf("the merge was meant to stop on a conflict:\n%s", out)
	}

	censusAppend(t, file("lib.txt"), "staged-edit\n")
	g("add", "lib.txt")
	censusWrite(t, file("newstaged.txt"), "newstaged\n")
	g("add", "newstaged.txt")
	censusAppend(t, file("app.txt"), "unstaged-edit\n")
	censusWrite(t, file("notes/todo.md"), "todo\n")
	censusWrite(t, file("scratch.txt"), "untracked root\n")
	censusWrite(t, file(".env.local"), "DUMMY=not-a-secret\n")
	censusWrite(t, file("notes/.env"), "A=1\n")
	censusWrite(t, file("debug.log"), "ignored\n")
	censusWrite(t, file("node_modules/pkg/index.js"), "x\n")
	censusWrite(t, file("node_modules/pkg/.env.example"), "y\n")
	return base, root
}

func readCensus(t *testing.T, root string) (*PoolMigrateCensus, *censusLocalSSH) {
	t.Helper()
	ssh := &censusLocalSSH{}
	census, err := ReadPoolMigrateCensus(context.Background(), ssh, "appdev", root)
	if err != nil {
		t.Fatalf("ReadPoolMigrateCensus: %v", err)
	}
	return census, ssh
}

func censusTree(t *testing.T, c *PoolMigrateCensus, path string) PoolMigrateTree {
	t.Helper()
	for _, tr := range c.Trees {
		if tr.Path == path {
			return tr
		}
	}
	var paths []string
	for _, tr := range c.Trees {
		paths = append(paths, tr.Path)
	}
	t.Fatalf("no tree %s in the census; trees %v", path, paths)
	return PoolMigrateTree{}
}

func censusPaths(p *PoolMigratePaths) []string {
	if p == nil {
		return nil
	}
	return p.Paths
}

func TestPoolMigrateCensus_EveryPlaceWorkHides_Named(t *testing.T) {
	t.Parallel()
	base, root := censusFixture(t)
	c, _ := readCensus(t, root)

	if c.Clean {
		t.Fatal("a repository holding unsaved work read clean")
	}
	if len(c.Trees) == 0 || c.Trees[0].Path != root {
		t.Fatalf("the main checkout must come first; trees %+v", c.Trees)
	}
	if len(c.Trees) != 5 {
		t.Errorf("trees = %d, want the main checkout and four worktrees", len(c.Trees))
	}

	main := c.Trees[0]
	if main.Branch != "main" || main.Upstream == nil || *main.Upstream != (PoolMigrateUpstream{Remote: "origin", Branch: "main"}) {
		t.Errorf("main tree branch/upstream = %q/%+v, want main on origin/main", main.Branch, main.Upstream)
	}
	if got, want := censusPaths(main.Staged), []string{"lib.txt", "newstaged.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("main staged = %v, want %v", got, want)
	}
	if got, want := censusPaths(main.Modified), []string{"app.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("main modified = %v, want %v", got, want)
	}
	// An .env file is named as one, never as a file to commit.
	if got, want := censusPaths(main.Untracked), []string{"notes/", "scratch.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("main untracked = %v, want %v", got, want)
	}
	if got, want := main.EnvFiles, []string{".env.local", "notes/.env"}; !reflect.DeepEqual(got, want) {
		t.Errorf("main envFiles = %v, want %v (a dependency's file inside an ignored directory is not the dev's)", got, want)
	}
	if got, want := censusPaths(main.Ignored), []string{".crew/", "debug.log", "node_modules/"}; !reflect.DeepEqual(got, want) {
		t.Errorf("main ignored = %v, want %v", got, want)
	}

	ada := censusTree(t, c, filepath.Join(root, ".crew", "ada"))
	if ada.Branch != "crew/ada" || !reflect.DeepEqual(censusPaths(ada.Staged), []string{"lib.txt"}) ||
		!reflect.DeepEqual(censusPaths(ada.Modified), []string{"app.txt"}) ||
		!reflect.DeepEqual(censusPaths(ada.Untracked), []string{"ada-new.txt"}) {
		t.Errorf("crew worktree = %+v, want crew/ada with lib.txt staged, app.txt modified, ada-new.txt untracked", ada)
	}

	det := censusTree(t, c, filepath.Join(root, ".crew", "det"))
	if !det.Detached || det.DetachedCommits != 1 || det.Branch != "" {
		t.Errorf("detached worktree = %+v, want detached holding 1 commit of its own", det)
	}

	gone := censusTree(t, c, filepath.Join(root, ".crew", "gone"))
	if !gone.Missing || gone.Branch != "crew/gone" {
		t.Errorf("worktree whose directory is gone = %+v, want missing on crew/gone", gone)
	}

	merging := censusTree(t, c, filepath.Join(base, "merging"))
	if !slices.Contains(merging.InProgress, "merging") || !reflect.DeepEqual(censusPaths(merging.Conflicted), []string{"app.txt"}) {
		t.Errorf("worktree mid-merge = %+v, want merging with app.txt conflicted", merging)
	}

	unpushed := map[string]int{}
	for _, b := range c.Branches {
		unpushed[b.Name] = b.Unpushed
	}
	for name, want := range map[string]int{"feat": 2, "mate-kept/x": 1, "crew/ada": 1, "merging": 1, "appB": 1} {
		if unpushed[name] != want {
			t.Errorf("branch %s: %d commits on no remote branch, want %d (branches %+v)", name, unpushed[name], want, c.Branches)
		}
	}
	for _, pushed := range []string{"main", "crew/gone"} {
		if _, listed := unpushed[pushed]; listed {
			t.Errorf("branch %s holds nothing a remote lacks, yet is listed", pushed)
		}
	}

	if len(c.Stashes) != 3 {
		t.Fatalf("stashes = %+v, want 3", c.Stashes)
	}
	for i, want := range []string{"stash-three", "stash-two", "stash-one"} {
		s := c.Stashes[i]
		if s.Ref != fmt.Sprintf("stash@{%d}", i) || !strings.Contains(s.Message, want) || len(s.Commit) != 40 {
			t.Errorf("stash %d = %+v, want stash@{%d} %q", i, s, i, want)
		}
	}

	var subjects []string
	if c.ReflogOnly != nil {
		for _, commit := range c.ReflogOnly.Commits {
			subjects = append(subjects, commit.Subject)
		}
	}
	slices.Sort(subjects)
	if c.ReflogOnly == nil || c.ReflogOnly.Count != 2 || !reflect.DeepEqual(subjects, []string{"reflog-only", "worktree-reflog-only"}) {
		t.Errorf("reflogOnly = %+v, want the two reset-away commits, the worktree's included (stash entries are not reflog-only)", c.ReflogOnly)
	}

	if want := map[string]int{"checkpoints": 2, "crew": 1}; !reflect.DeepEqual(c.T3Refs, want) {
		t.Errorf("t3Refs = %v, want %v", c.T3Refs, want)
	}
}

func TestPoolMigrateCensus_CleanRepo_Clean(t *testing.T) {
	t.Parallel()
	root := censusCleanRepo(t, censusBase(t))
	c, _ := readCensus(t, root)

	if !c.Clean {
		t.Fatalf("a checkout holding nothing its origin lacks read unclean: %+v", c)
	}
	if len(c.Trees) != 1 || c.Trees[0].Path != root || c.Trees[0].Branch != "main" {
		t.Fatalf("trees = %+v, want the one main checkout on main", c.Trees)
	}
	if c.Trees[0].Upstream == nil || c.Trees[0].Upstream.Branch != "main" {
		t.Errorf("upstream = %+v, want origin/main", c.Trees[0].Upstream)
	}
	if len(c.Branches) != 0 || len(c.Stashes) != 0 || c.ReflogOnly != nil || len(c.T3Refs) != 0 {
		t.Errorf("a clean checkout named work: %+v", c)
	}
}

// TestPoolMigrateCensus_EachItemMakesItUnclean pins that every kind of unsaved
// work the census names, alone, makes it unclean — and that refs/t3 refs and
// ignored files, alone, do not: Mate's own refs, and files a checkout
// reproduces from its repository.
func TestPoolMigrateCensus_EachItemMakesItUnclean(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		mutate    func(t *testing.T, root string)
		wantClean bool
	}{
		{name: "staged edit", mutate: func(t *testing.T, root string) {
			t.Helper()
			censusAppend(t, filepath.Join(root, "lib.txt"), "x\n")
			censusGit(t, root, "add", "lib.txt")
		}},
		{name: "unstaged edit", mutate: func(t *testing.T, root string) {
			t.Helper()
			censusAppend(t, filepath.Join(root, "lib.txt"), "x\n")
		}},
		{name: "untracked file", mutate: func(t *testing.T, root string) {
			t.Helper()
			censusWrite(t, filepath.Join(root, "new.txt"), "x\n")
		}},
		{name: "ignored .env file", mutate: func(t *testing.T, root string) {
			t.Helper()
			censusAppend(t, filepath.Join(root, ".gitignore"), ".env\n")
			censusGit(t, root, "commit", "-q", "-am", "ignore .env")
			censusGit(t, root, "push", "-q")
			censusWrite(t, filepath.Join(root, ".env"), "A=1\n")
		}},
		{name: "branch on no remote", mutate: func(t *testing.T, root string) {
			t.Helper()
			censusGit(t, root, "branch", "side")
			censusGit(t, root, "switch", "-q", "side")
			censusAppend(t, filepath.Join(root, "lib.txt"), "x\n")
			censusGit(t, root, "commit", "-q", "-am", "side")
			censusGit(t, root, "switch", "-q", "main")
		}},
		{name: "stash entry", mutate: func(t *testing.T, root string) {
			t.Helper()
			censusAppend(t, filepath.Join(root, "lib.txt"), "x\n")
			censusGit(t, root, "stash", "push", "-q")
		}},
		{name: "commit only the reflog reaches", mutate: func(t *testing.T, root string) {
			t.Helper()
			censusAppend(t, filepath.Join(root, "lib.txt"), "x\n")
			censusGit(t, root, "commit", "-q", "-am", "amended away")
			censusGit(t, root, "reset", "-q", "--hard", "HEAD~1")
		}},
		{name: "detached HEAD holding a commit", mutate: func(t *testing.T, root string) {
			t.Helper()
			censusGit(t, root, "switch", "-q", "--detach")
			censusAppend(t, filepath.Join(root, "lib.txt"), "x\n")
			censusGit(t, root, "commit", "-q", "-am", "detached")
		}},
		{name: "refs/t3 ref alone", wantClean: true, mutate: func(t *testing.T, root string) {
			t.Helper()
			censusGit(t, root, "update-ref", "refs/t3/checkpoints/a/turn/1", "HEAD")
		}},
		{name: "ignored file alone", wantClean: true, mutate: func(t *testing.T, root string) {
			t.Helper()
			censusWrite(t, filepath.Join(root, "node_modules", "x", "index.js"), "x\n")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := censusCleanRepo(t, censusBase(t))
			tt.mutate(t, root)
			c, _ := readCensus(t, root)
			if c.Clean != tt.wantClean {
				t.Fatalf("clean = %v, want %v; census %+v", c.Clean, tt.wantClean, c)
			}
		})
	}
}

// censusSnapshot records every entry under root: a file's mode, mtime and
// content hash, a directory's mode and mtime (so even a lock file created and
// removed again shows), a symlink's target.
func censusSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		entry := fmt.Sprintf("%s %d", info.Mode(), info.ModTime().UnixNano())
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entry += " -> " + target
		case info.Mode().IsRegular():
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(b)
			entry += " " + hex.EncodeToString(sum[:])
		}
		out[path] = entry
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return out
}

func censusSnapshotDiff(before, after map[string]string) []string {
	var diff []string
	for p, b := range before {
		if a, ok := after[p]; !ok {
			diff = append(diff, "removed "+p)
		} else if a != b {
			diff = append(diff, "changed "+p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			diff = append(diff, "added "+p)
		}
	}
	slices.Sort(diff)
	return diff
}

// TestPoolMigrateCensus_WritesNothing pins that the census changes nothing in
// the repository, its worktrees or anywhere under them: no ref, no object, no
// reflog, no lock file, and no index — not even an index whose entries are
// stale, which a plain `git status` rewrites. The control at the end proves
// the fixture provokes that rewrite.
func TestPoolMigrateCensus_WritesNothing(t *testing.T) {
	t.Parallel()
	base, root := censusFixture(t)
	// Stale index entries: the files keep their content but no longer match
	// the stat data the index recorded for them.
	old := time.Now().Add(-72 * time.Hour)
	for _, f := range []string{filepath.Join(root, "lib.txt"), filepath.Join(root, ".crew", "ada", "lib.txt")} {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}
	index := filepath.Join(root, ".git", "index")
	worktreeIndex := filepath.Join(root, ".git", "worktrees", "ada", "index")

	before := censusSnapshot(t, base)
	readCensus(t, root)
	readCensus(t, root)
	if diff := censusSnapshotDiff(before, censusSnapshot(t, base)); len(diff) > 0 {
		t.Fatalf("the census changed the repository:\n%s", strings.Join(diff, "\n"))
	}

	censusGit(t, root, "status")
	censusGit(t, filepath.Join(root, ".crew", "ada"), "status")
	after := censusSnapshot(t, base)
	for _, idx := range []string{index, worktreeIndex} {
		if after[idx] == before[idx] {
			t.Fatalf("control: a plain git status left %s as it was, so the fixture does not provoke an index refresh", idx)
		}
	}
}

func TestPoolMigrateCensus_OneRoundTrip(t *testing.T) {
	t.Parallel()
	_, root := censusFixture(t)
	_, ssh := readCensus(t, root)
	if len(ssh.calls) != 1 {
		t.Fatalf("ssh calls = %d, want one round trip", len(ssh.calls))
	}
	if want := "appdev: " + PoolMigrateCensusCommand(root); ssh.calls[0] != want {
		t.Fatalf("ssh call = %q, want %q", ssh.calls[0], want)
	}
}

// TestPoolMigrateCensus_ScriptRunsOnlyReadOnlyGit pins the census to git
// subcommands that read: none can move a ref, write an object, take a lock or
// rewrite the index, and status runs with optional locks off, so it never
// refreshes the index it reads.
func TestPoolMigrateCensus_ScriptRunsOnlyReadOnlyGit(t *testing.T) {
	t.Parallel()
	script := PoolMigrateCensusCommand("/var/www")
	readOnly := map[string]bool{
		"rev-parse": true, "worktree list": true, "for-each-ref": true, "rev-list": true,
		"stash list": true, "log": true, "status": true, "symbolic-ref": true, "ls-files": true,
	}
	re := regexp.MustCompile(`\bgit\s+([a-z-]+)(?:\s+([a-z]+))?`)
	matches := re.FindAllStringSubmatch(script, -1)
	if len(matches) == 0 {
		t.Fatal("the census runs no git at all")
	}
	for _, m := range matches {
		sub := m[1]
		if sub == "stash" || sub == "worktree" {
			sub += " " + m[2]
		}
		if !readOnly[sub] {
			t.Errorf("the census runs `git %s`, which is not a read-only subcommand", sub)
		}
	}
	if !strings.Contains(script, "GIT_OPTIONAL_LOCKS=0") {
		t.Error("the census must run git with optional locks off: a plain status refreshes and rewrites the index")
	}
	if !strings.HasPrefix(script, "cd '/var/www'") {
		t.Errorf("the census must start in the shell-quoted root; script starts %q", script[:min(40, len(script))])
	}
}

func TestPoolMigrateCensus_NotARepository_Error(t *testing.T) {
	t.Parallel()
	root := censusBase(t)
	_, err := ReadPoolMigrateCensus(context.Background(), &censusLocalSSH{}, "appdev", root)
	if err == nil {
		t.Fatal("the census of a directory holding no repository must fail")
	}
	var sshErr *platform.SSHExecError
	if !errors.As(err, &sshErr) || !strings.Contains(sshErr.Output, root) {
		t.Fatalf("err = %v, want the ssh error naming the root", err)
	}
}

// TestParsePoolMigrateCensus_IncompleteOrFailed_Error pins that a census the
// script could not finish never reads as clean: a step that failed, a cut-off
// output and an unknown format all fail the read.
func TestParsePoolMigrateCensus_IncompleteOrFailed_Error(t *testing.T) {
	t.Parallel()
	const head = "zcp-census 1\nW worktree /var/www\nW HEAD 1111111111111111111111111111111111111111\nW branch refs/heads/main\nT /var/www\nTS # branch.head main\n"
	tests := []struct {
		name    string
		out     string
		wantErr string
	}{
		{name: "a step failed", out: head + "fatal: index file corrupt\nzcp-census-error status /var/www\nzcp-census-end\n", wantErr: "index file corrupt"},
		{name: "cut off", out: head, wantErr: "incomplete"},
		{name: "no header", out: "something else\nzcp-census-end\n", wantErr: "census"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parsePoolMigrateCensus(tt.out); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one naming %q", err, tt.wantErr)
			}
		})
	}
}

// TestPoolMigrateCensus_Submodule_NamedNeverClean: a submodule is a repository
// of its own — its objects in .git/modules, never in the superproject's — so
// neither the bundle nor any other census item sees work inside it: a stash
// and a commit on a branch of its own, with the submodule's checkout back at
// the commit the superproject records, leave the superproject's status clean.
// The census names every submodule of every tree, and a repository holding one
// is never clean (PM-2): its work cannot be counted, and the bundle does not
// carry it (PM-3).
func TestPoolMigrateCensus_Submodule_NamedNeverClean(t *testing.T) {
	t.Parallel()
	base := censusBase(t)
	root := censusCleanRepo(t, base)
	lib := filepath.Join(base, "libsrc")
	censusGit(t, base, "init", "-q", "-b", "main", lib)
	censusWrite(t, filepath.Join(lib, "lib.go"), "package lib\n")
	censusGit(t, lib, "add", "-A")
	censusGit(t, lib, "commit", "-q", "-m", "lib-1")
	censusGit(t, root, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "vendor/lib")
	censusGit(t, root, "commit", "-q", "-m", "add lib")
	censusGit(t, root, "push", "-q", "origin", "main")

	sub := filepath.Join(root, "vendor", "lib")
	censusWrite(t, filepath.Join(sub, "lib.go"), "package lib\n// stashed\n")
	censusGit(t, sub, "stash", "push", "-q", "-m", "inside the submodule")
	censusGit(t, sub, "switch", "-q", "-c", "wip")
	censusWrite(t, filepath.Join(sub, "wip.go"), "package lib\n")
	censusGit(t, sub, "add", "wip.go")
	censusGit(t, sub, "commit", "-q", "-m", "wip on no remote")
	censusGit(t, sub, "switch", "-q", "--detach", "HEAD~1")
	if out, err := censusGitRun(root, "status", "--porcelain"); err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("the superproject must read clean for this case (%v):\n%s", err, out)
	}

	c, _ := readCensus(t, root)
	if c.Clean {
		t.Fatalf("a repository whose submodule holds a stash and a commit on no remote read clean: %+v", c)
	}
	raw, err := json.Marshal(c.Trees[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"submodules":{"count":1,"paths":["vendor/lib"]}`) {
		t.Errorf("the main tree = %s, want submodules naming vendor/lib", raw)
	}
}

// TestPoolMigrateCensus_WorktreePathWithNewline_NoCensus: `git worktree list
// --porcelain` prints a worktree path verbatim, a newline in it included, so a
// line-by-line read would cut the path and take the worktree for one whose
// directory is gone — its edits neither holding the retire nor bundled. Such
// a path is no census: the read fails naming it, never a clean census (PM-2).
func TestPoolMigrateCensus_WorktreePathWithNewline_NoCensus(t *testing.T) {
	t.Parallel()
	base := censusBase(t)
	root := censusCleanRepo(t, base)
	odd := filepath.Join(base, "odd\nname")
	censusGit(t, root, "worktree", "add", "-q", "-b", "odd", odd, "main")
	censusWrite(t, filepath.Join(odd, "app.txt"), "edited in the odd worktree\n")

	census, err := ReadPoolMigrateCensus(context.Background(), &censusLocalSSH{}, "appdev", root)
	if err == nil {
		t.Fatalf("a worktree whose path holds a newline read as a census: %+v", census)
	}
	// The script stops like any step that fails: the reason rides the SSH
	// error's output, which the flow's refusal quotes (withSSHStderr).
	said := err.Error()
	var sshErr *platform.SSHExecError
	if errors.As(err, &sshErr) {
		said += " " + sshErr.Output
	}
	if !strings.Contains(said, "newline") {
		t.Errorf("err = %q, want it to name the worktree path holding a newline", said)
	}
}
