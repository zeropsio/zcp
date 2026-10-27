// Tests for: ops/hq_main_sync.go — a wired pair's local `main` against HQ's.
// A checkout wired before branches of the agent's own kept zcp's empty seed
// as `main`, so `git merge --ff-only origin/main` failed on it: the sync puts
// such a `main` on HQ's and leaves every commit of the agent's where it is.
package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// zcpSeedMain makes the pair's `main` zcp's seed — one parentless commit of
// the empty tree, unrelated to HQ's — the way a checkout wired before
// branches of the agent's own kept it.
func zcpSeedMain(t *testing.T, dir string) {
	t.Helper()
	tree := runGit(t, dir, "mktree")
	sha := runGit(t, dir, "commit-tree", tree, "-m", "zcp init")
	runGit(t, dir, "update-ref", "refs/heads/main", sha)
}

func TestMainSync_ASeedOnlyMainTakesHQsMainAndNothingOfTheAgentsMoves(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	for _, tc := range []struct {
		name string
		// pair makes the pair's history; the base is landedBase.
		pair func(t *testing.T, dir string)
		// want is the outcome MainSync reads; mainIsBase: main is HQ's after.
		want       string
		mainIsBase bool
		// onMain: HEAD is on main after; else on the agent's branch "work".
		onMain    bool
		wantFiles map[string]string
	}{
		{
			name: "the agent works on a branch of its own, main is zcp's seed",
			pair: func(t *testing.T, dir string) {
				t.Helper()
				zcpSeedMain(t, dir)
				runGit(t, dir, "checkout", "-q", "-b", "work", "main")
				writeLabFile(t, filepath.Join(dir, "work.txt"), "the agent's work\n")
				commitAll(t, dir, "the agent's work")
			},
			want: MainFromSeed, mainIsBase: true,
			wantFiles: map[string]string{"work.txt": "the agent's work\n"},
		},
		{
			name: "the checkout is on zcp's seed main, nothing in the way",
			pair: func(t *testing.T, dir string) {
				t.Helper()
				zcpSeedMain(t, dir)
				runGit(t, dir, "checkout", "-q", "main")
				writeLabFile(t, filepath.Join(dir, "notes.txt"), "untracked notes\n")
			},
			want: MainFromSeed, mainIsBase: true, onMain: true,
			wantFiles: map[string]string{"app.js": "the first Mate's work\n", "notes.txt": "untracked notes\n"},
		},
		{
			name: "the checkout is on zcp's seed main, an uncommitted file is in HQ's way",
			pair: func(t *testing.T, dir string) {
				t.Helper()
				zcpSeedMain(t, dir)
				runGit(t, dir, "checkout", "-q", "main")
				writeLabFile(t, filepath.Join(dir, "app.js"), "the agent's uncommitted app\n")
			},
			want: MainSeedInTheWay, onMain: true,
			wantFiles: map[string]string{"app.js": "the agent's uncommitted app\n"},
		},
		{
			name: "main behind HQ's is the agent's fast-forward, not the sync's",
			pair: func(t *testing.T, dir string) {
				t.Helper()
				runGit(t, dir, "fetch", "-q", "origin", "main")
				runGit(t, dir, "update-ref", "refs/heads/main", "FETCH_HEAD~1")
				runGit(t, dir, "checkout", "-q", "-b", "work", "main")
			},
		},
		{
			name: "main with a commit only it holds is left where it is, with its step named",
			pair: func(t *testing.T, dir string) {
				t.Helper()
				runGit(t, dir, "fetch", "-q", "origin", "main")
				runGit(t, dir, "checkout", "-q", "-b", "main", "FETCH_HEAD")
				writeLabFile(t, filepath.Join(dir, "own.txt"), "committed on main\n")
				commitAll(t, dir, "on main")
			},
			want: MainOwnCommits, onMain: true,
			wantFiles: map[string]string{"own.txt": "committed on main\n"},
		},
		{
			name: "main whose commits a branch holds too says nothing: nothing of the agent's is at stake",
			pair: func(t *testing.T, dir string) {
				t.Helper()
				runGit(t, dir, "fetch", "-q", "origin", "main")
				runGit(t, dir, "checkout", "-q", "-b", "main", "FETCH_HEAD")
				writeLabFile(t, filepath.Join(dir, "own.txt"), "committed on main\n")
				commitAll(t, dir, "on main")
				runGit(t, dir, "push", "-q", "origin", "main:refs/heads/fix-login")
				runGit(t, dir, "fetch", "-q", "origin", "fix-login:refs/remotes/origin/fix-login")
			},
			onMain:    true,
			wantFiles: map[string]string{"own.txt": "committed on main\n"},
		},
		{
			name: "a main of real code sharing nothing with HQ's is left where it is",
			pair: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "index.js"), "another history\n")
				commitAll(t, dir, "another history")
			},
			want: MainOwnCommits, onMain: true,
			wantFiles: map[string]string{"index.js": "another history\n"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pair := baseLabOn(t, landedBase, tc.pair)
			remote := runGit(t, pair, "remote", "get-url", "origin")
			base := runGit(t, filepath.Dir(remote), "--git-dir", remote, "rev-parse", "main")
			mainBefore := gitOr(t, pair, "rev-parse", "-q", "--verify", "refs/heads/main")
			headBefore := runGit(t, pair, "rev-parse", "HEAD")

			//nolint:gosec // test-only, the command under test against a t.TempDir repository
			out, err := exec.CommandContext(t.Context(), "sh", "-c", BuildMainSyncCommand(pair, remote)).CombinedOutput()
			if err != nil {
				t.Fatalf("the sync failed: %v\n%s", err, out)
			}

			state, _ := MainSync(string(out))
			if state != tc.want {
				t.Errorf("MainSync = %q, want %q; output:\n%s", state, tc.want, out)
			}
			mainAfter := gitOr(t, pair, "rev-parse", "-q", "--verify", "refs/heads/main")
			switch {
			case tc.mainIsBase && mainAfter != base:
				t.Errorf("main is %s, want HQ's main %s", mainAfter, base)
			case !tc.mainIsBase && mainAfter != mainBefore:
				t.Errorf("main moved %s → %s", mainBefore, mainAfter)
			}
			head := gitOr(t, pair, "symbolic-ref", "--short", "HEAD")
			if tc.onMain && head != "main" || !tc.onMain && head != "work" {
				t.Errorf("HEAD is on %q", head)
			}
			if !tc.onMain && runGit(t, pair, "rev-parse", "HEAD") != headBefore {
				t.Errorf("the agent's branch moved")
			}
			assertLabFiles(t, pair, tc.wantFiles)
			if tc.mainIsBase {
				runGit(t, pair, "fetch", "-q", "origin")
				runGit(t, pair, "checkout", "-q", "main")
				runGit(t, pair, "merge", "--ff-only", "origin/main")
			}
		})
	}
}

func TestMainSync_RunTwiceChangesNothingMore(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := baseLabOn(t, landedBase, func(t *testing.T, dir string) {
		t.Helper()
		zcpSeedMain(t, dir)
		runGit(t, dir, "checkout", "-q", "-b", "work", "main")
	})
	remote := runGit(t, pair, "remote", "get-url", "origin")
	runShell(t, BuildMainSyncCommand(pair, remote))
	main := runGit(t, pair, "rev-parse", "main")

	//nolint:gosec // test-only, the command under test against a t.TempDir repository
	out, err := exec.CommandContext(t.Context(), "sh", "-c", BuildMainSyncCommand(pair, remote)).CombinedOutput()
	if err != nil {
		t.Fatalf("the second sync failed: %v\n%s", err, out)
	}
	if state, _ := MainSync(string(out)); state != "" {
		t.Errorf("the second sync read %q, want nothing to say", state)
	}
	if got := runGit(t, pair, "rev-parse", "main"); got != main {
		t.Errorf("main moved again: %s → %s", main, got)
	}
}

// TestMainSync_AMainThatMovesMidSwitchKeepsTheCheckoutOnIt: the person commits
// on the checked-out seed-only `main` in their own shell while the sync
// switches it to HQ's. The switch does not take — `main` is theirs now —
// and the checkout is left on `main` at their commit, never detached.
func TestMainSync_AMainThatMovesMidSwitchKeepsTheCheckoutOnIt(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := baseLabOn(t, landedBase, func(t *testing.T, dir string) {
		t.Helper()
		zcpSeedMain(t, dir)
		runGit(t, dir, "checkout", "-q", "main")
	})
	remote := runGit(t, pair, "remote", "get-url", "origin")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	// git as the sync sees it: the person's commit lands on `main` just
	// before the sync moves it.
	bin := t.TempDir()
	wrapper := "#!/bin/sh\n" +
		"if [ \"$1\" = update-ref ] && [ \"$2\" = refs/heads/main ]; then\n" +
		"  person=$(GIT_AUTHOR_NAME=p GIT_AUTHOR_EMAIL=p@example.invalid GIT_COMMITTER_NAME=p GIT_COMMITTER_EMAIL=p@example.invalid " +
		realGit + " commit-tree \"$4^{tree}\" -p \"$4\" -m \"the person's commit\") && " + realGit + " update-ref refs/heads/main \"$person\"\n" +
		"fi\nexec " + realGit + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}

	//nolint:gosec // test-only, the command under test against a t.TempDir repository
	cmd := exec.CommandContext(t.Context(), "sh", "-c", BuildMainSyncCommand(pair, remote))
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, _ := cmd.CombinedOutput()

	if state, _ := MainSync(string(out)); state == MainFromSeed {
		t.Errorf("the sync says main is HQ's, but the person's commit is on it; output:\n%s", out)
	}
	if head := gitOr(t, pair, "symbolic-ref", "-q", "HEAD"); head != "refs/heads/main" {
		t.Fatalf("HEAD is %q, want on main; output:\n%s", head, out)
	}
	if got := runGit(t, pair, "log", "-1", "--format=%s", "main"); got != "the person's commit" {
		t.Errorf("main is at %q, want the person's commit", got)
	}
	if got := runGit(t, pair, "status", "--porcelain"); got != "" {
		t.Errorf("the checkout differs from main:\n%s", got)
	}
}

// TestMainSync_AFirstHistoryOnHQsUnwrittenRootIsPushedAsItIs: against HQ's
// unwritten root — one parentless commit of the empty tree — a `main` of a
// history of its own is the repository's first history, never commits to
// carry onto that root: the sync names it (FIRST_HISTORY) and moves nothing,
// so the agent pushes it as it is and HQ makes its change `main`. Kept on a
// branch too, it is already on its way and nothing is said; against a `main`
// holding code it stays commits of its own.
func TestMainSync_AFirstHistoryOnHQsUnwrittenRootIsPushedAsItIs(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	ownHistory := func(t *testing.T, dir string) {
		t.Helper()
		writeLabFile(t, filepath.Join(dir, "index.js"), "the imported app\n")
		commitAll(t, dir, "the imported history")
	}
	for _, tc := range []struct {
		name string
		base func(t *testing.T, dir string)
		pair func(t *testing.T, dir string)
		want string
	}{
		{name: "a main of its own on HQ's unwritten root is its first history", base: hqSeed, pair: ownHistory, want: "FIRST_HISTORY"},
		{
			name: "a first history a branch holds too says nothing",
			base: hqSeed,
			pair: func(t *testing.T, dir string) {
				t.Helper()
				ownHistory(t, dir)
				runGit(t, dir, "branch", "bring-it-in")
			},
		},
		{name: "against a main holding code it stays commits of its own", base: landedBase, pair: ownHistory, want: MainOwnCommits},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pair := baseLabOn(t, tc.base, tc.pair)
			remote := runGit(t, pair, "remote", "get-url", "origin")
			mainBefore := runGit(t, pair, "rev-parse", "refs/heads/main")

			//nolint:gosec // test-only, the command under test against a t.TempDir repository
			out, err := exec.CommandContext(t.Context(), "sh", "-c", BuildMainSyncCommand(pair, remote)).CombinedOutput()
			if err != nil {
				t.Fatalf("the sync failed: %v\n%s", err, out)
			}
			if state, _ := MainSync(string(out)); state != tc.want {
				t.Errorf("MainSync = %q, want %q; output:\n%s", state, tc.want, out)
			}
			if got := runGit(t, pair, "rev-parse", "refs/heads/main"); got != mainBefore {
				t.Errorf("main moved %s → %s", mainBefore, got)
			}
			assertLabFiles(t, pair, map[string]string{"index.js": "the imported app\n"})
		})
	}
}
