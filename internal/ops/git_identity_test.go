// Tests for: ops/git_identity.go — the single-owner set-if-absent
// identity + HEAD-guarantee fragments shared by every self-heal call site
// (InitServiceGit, buildSSHCommand's safety-net, BuildGitReconstructCommand,
// BuildGitOriginSyncCommand, git-push-setup's pre-probe ensure).
package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGitIdentityEnsureFragment_Shape pins the two load-bearing shell
// properties (plan-verified 2026-07-12): per-key parenthesized grouping
// (an ungrouped `a && b || c && d` fires `c` whenever `a` fails, because
// `&&`/`||` share precedence and associate left-to-right — that shape would
// force-overwrite user.name whenever the email branch's probe fails), and
// probing VALUE non-emptiness rather than exit code (`git config user.email`
// on a key set to the EMPTY string still exits 0).
func TestGitIdentityEnsureFragment_Shape(t *testing.T) {
	t.Parallel()

	got := gitIdentityEnsureFragment()
	want := `(test -n "$(git config user.email)" || git config user.email 'agent@zerops.io') && (test -n "$(git config user.name)" || git config user.name 'Zerops Agent')`
	if got != want {
		t.Errorf("gitIdentityEnsureFragment() =\n%q\nwant:\n%q", got, want)
	}
}

// TestGitHeadEnsureFragment_Shape pins the index/worktree-independent HEAD
// guarantee: `git commit --allow-empty` was rejected because it would
// commit whatever is currently STAGED on an unborn repo — this fragment
// instead builds a parentless commit from the empty tree
// (`mktree </dev/null` + `commit-tree`) and points HEAD at it via
// `update-ref`, touching neither the index nor the working tree.
func TestGitHeadEnsureFragment_Shape(t *testing.T) {
	t.Parallel()

	got := gitHeadEnsureFragment()
	want := `(git rev-parse -q --verify HEAD >/dev/null || git update-ref HEAD "$(git -c user.email='agent@zerops.io' -c user.name='Zerops Agent' commit-tree "$(git mktree </dev/null)" -m 'zcp init')")`
	if got != want {
		t.Errorf("gitHeadEnsureFragment() =\n%q\nwant:\n%q", got, want)
	}
}

// TestGitEnsureRepoHeadCommand_Shape pins the composed chain order: cd,
// then the init guard, then identity ensure, then the HEAD guarantee — the
// order every call site (InitServiceGit, buildSSHCommand, git-push-setup's
// pre-probe ensure) relies on.
func TestGitEnsureRepoHeadCommand_Shape(t *testing.T) {
	t.Parallel()

	cmd := GitEnsureRepoHeadCommand("/var/www")
	cdIdx := strings.Index(cmd, "cd '/var/www'")
	initIdx := strings.Index(cmd, "(test -d .git || git init -q -b main)")
	identityIdx := strings.Index(cmd, `test -n "$(git config user.email)"`)
	headIdx := strings.Index(cmd, "git rev-parse -q --verify HEAD")

	for name, idx := range map[string]int{"cd": cdIdx, "init guard": initIdx, "identity ensure": identityIdx, "HEAD guarantee": headIdx} {
		if idx < 0 {
			t.Fatalf("command missing %s piece:\n%s", name, cmd)
		}
	}
	if cdIdx >= initIdx || initIdx >= identityIdx || identityIdx >= headIdx {
		t.Errorf("chain out of order (want cd < init < identity < head): cd=%d init=%d identity=%d head=%d\n%s",
			cdIdx, initIdx, identityIdx, headIdx, cmd)
	}
}

// TestGitHeadEnsureFragment_UnbornRepoWithStagedFiles_NoStagedContentCommitted
// is the F2 hazard test: an unborn repo with files already staged (`git add`
// ran, no commit yet) must NOT have that staged content swept into the
// 'zcp init' marker commit, and the staging area must stay exactly as the
// caller left it. This is precisely the scenario `commit --allow-empty`
// would have gotten wrong.
func TestGitHeadEnsureFragment_UnbornRepoWithStagedFiles_NoStagedContentCommitted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	t.Parallel()

	dir := t.TempDir()
	env := isolatedGitEnv(t)
	mustRunGit(t, dir, env, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("staged content"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	mustRunGit(t, dir, env, "add", "staged.txt")

	// Run just the HEAD-ensure fragment (identity ensure is orthogonal to
	// this hazard) via bash, cd'd into dir.
	runShellChain(t, "cd "+shellQuote(dir)+" && "+gitHeadEnsureFragment(), env)

	if n := gitCommitCount(dir, env); n != 1 {
		t.Fatalf("commit count = %d, want exactly 1 (the zcp init marker)", n)
	}
	// The marker commit's tree must be EMPTY — no staged content swept in.
	lsTree := mustOutputGit(t, dir, env, "ls-tree", "-r", "--name-only", "HEAD")
	if strings.TrimSpace(lsTree) != "" {
		t.Errorf("zcp init commit tree is NOT empty, contains: %q", lsTree)
	}
	// The staging area must be untouched: staged.txt is still staged
	// (shows as "A" in `git status --porcelain`, index differs from HEAD).
	porcelain := gitPorcelain(dir, env)
	if !strings.Contains(porcelain, "staged.txt") {
		t.Errorf("staging area was disturbed — staged.txt no longer shows in status: %q", porcelain)
	}
	if !strings.HasPrefix(strings.TrimSpace(porcelain), "A ") {
		t.Errorf("staged.txt should still show as a staged addition (\"A \" prefix), got status line: %q", porcelain)
	}
}

func mustOutputGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	//nolint:gosec // test-only, inputs are t.TempDir paths
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

// TestGitEnsureRepoHeadCommand_MakesWriteProbeUseDryRunBranch is the F2
// item-2 real-git proof: BuildGitWritePushProbeCommand branches on `git
// rev-parse --verify -q HEAD` to decide between the real `push --dry-run`
// write proof and its weaker unborn-HEAD ls-remote fallback. On a totally
// missing/unborn repo that predicate is false; after
// GitEnsureRepoHeadCommand runs, it must be true — proving git-push-setup's
// pre-probe ensure (workflow_git_push_setup.go step 0) actually upgrades
// the probe to the real write-proof branch, not just that ZCP issues an
// extra SSH call.
func TestGitEnsureRepoHeadCommand_MakesWriteProbeUseDryRunBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	t.Parallel()

	dir := t.TempDir()
	env := isolatedGitEnv(t)

	// Before: no .git/ at all — the probe's HEAD predicate must read false.
	//nolint:gosec // test-only, dir is a t.TempDir path
	before := exec.CommandContext(context.Background(), "bash", "-c", "cd "+shellQuote(dir)+" && git rev-parse --verify -q HEAD >/dev/null 2>&1 && echo yes || echo no")
	before.Env = env
	out, err := before.Output()
	if err != nil {
		t.Fatalf("pre-check command: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "no" {
		t.Fatalf("pre-condition violated: HEAD predicate = %q before ensure, want %q", got, "no")
	}

	runShellChain(t, GitEnsureRepoHeadCommand(dir), env)

	//nolint:gosec // test-only, dir is a t.TempDir path
	after := exec.CommandContext(context.Background(), "bash", "-c", "cd "+shellQuote(dir)+" && git rev-parse --verify -q HEAD >/dev/null 2>&1 && echo yes || echo no")
	after.Env = env
	out, err = after.Output()
	if err != nil {
		t.Fatalf("post-check command: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "yes" {
		t.Errorf("HEAD predicate = %q after GitEnsureRepoHeadCommand, want %q — the write probe would still fall back to its weaker ls-remote branch", got, "yes")
	}
}

// TestGitEnsureRepoHeadCommand_Idempotent proves running the composed
// chain twice on the same fresh dir never mints a second commit — the
// HEAD guarantee no-ops once HEAD exists.
func TestGitEnsureRepoHeadCommand_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	t.Parallel()

	dir := t.TempDir()
	env := isolatedGitEnv(t)
	cmd := GitEnsureRepoHeadCommand(dir)

	runShellChain(t, cmd, env)
	first := gitHeadSHA(dir, env)
	if n := gitCommitCount(dir, env); n != 1 {
		t.Fatalf("after first run: commit count = %d, want 1", n)
	}

	runShellChain(t, cmd, env)
	second := gitHeadSHA(dir, env)
	if n := gitCommitCount(dir, env); n != 1 {
		t.Fatalf("after second run: commit count = %d, want still 1", n)
	}
	if first != second {
		t.Errorf("HEAD moved on the second run: first=%s second=%s", first, second)
	}
}
