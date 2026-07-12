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

// TestBuildGitIdentitySeedCommand_Shape pins the dispatch-token shape (F3
// item 2): per-key independent decisions, robot-exact-match comparison
// against DeployGitIdentity (not the supplied identity), and one
// seeded/preserved token per key so the caller never needs a second SSH
// round-trip to learn what happened.
func TestBuildGitIdentitySeedCommand_Shape(t *testing.T) {
	t.Parallel()
	identity := GitIdentity{Name: "octocat", Email: "octocat@users.noreply.github.com"}
	cmd := BuildGitIdentitySeedCommand("/var/www", identity)

	for _, want := range []string{
		"cd '/var/www'",
		`cur_email=$(git config user.email)`,
		`[ -z "$cur_email" ] || [ "$cur_email" = 'agent@zerops.io' ]`,
		`git config user.email 'octocat@users.noreply.github.com'`,
		GitIdentitySeedEmailSeeded,
		GitIdentitySeedEmailPreserved,
		GitIdentitySeedEmailWriteFailed,
		`cur_name=$(git config user.name)`,
		`[ -z "$cur_name" ] || [ "$cur_name" = 'Zerops Agent' ]`,
		`git config user.name 'octocat'`,
		GitIdentitySeedNameSeeded,
		GitIdentitySeedNamePreserved,
		GitIdentitySeedNameWriteFailed,
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("seed command missing %q:\n%s", want, cmd)
		}
	}
}

// TestBuildGitIdentitySeedCommand_AlwaysExactlyTwoLines pins the Codex
// diff-review finding 2 shape guarantee: every branch of the generated
// command (seeded, preserved, OR a write failure) terminates in exactly
// ONE echo per key, so the total stdout is always exactly two lines
// regardless of outcome. Verified against real git by forcing a WRITE
// FAILURE via a read-only .git/config, which fails BOTH keys' writes —
// proving the write-failure token appears (never silently dropped) and
// the two-lines guarantee survives failure. The genuinely MIXED case
// (one key seeded, the other cleanly preserved) is covered separately by
// TestBuildGitIdentitySeedCommand_MixedOutcome.
func TestBuildGitIdentitySeedCommand_AlwaysExactlyTwoLines(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	t.Parallel()
	derived := GitIdentity{Name: "octocat", Email: "octocat@users.noreply.github.com"}

	dir := t.TempDir()
	env := isolatedGitEnv(t)
	mustRunGit(t, dir, env, "init", "-q", "-b", "main")

	// `git config <key> <value>` writes via a lock file
	// (.git/config.lock) then renames it over .git/config — rename()
	// only needs write permission on the DIRECTORY, not the target file,
	// so making config itself read-only does NOT reproduce a write
	// failure. Making the .git DIRECTORY read-only blocks creating the
	// lock file in the first place, which does.
	gitDir := dir + "/.git"
	if err := os.Chmod(gitDir, 0o555); err != nil {
		t.Fatalf("chmod .git read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(gitDir, 0o755) })

	//nolint:gosec // test-only, dir is a t.TempDir path and derived is a test-local literal
	cmd := exec.CommandContext(context.Background(), "bash", "-c", BuildGitIdentitySeedCommand(dir, derived))
	cmd.Env = env
	stdout, err := cmd.Output()
	// The overall command must still exit 0 — every branch (including the
	// write-failure one) terminates in a successful echo, per the doc
	// comment's guarantee.
	if err != nil {
		t.Fatalf("seed command should exit 0 even when a write fails: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(stdout), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected exactly 2 lines even with a write failure, got %d: %q", len(lines), stdout)
	}
	if strings.TrimSpace(lines[0]) != GitIdentitySeedEmailWriteFailed {
		t.Errorf("email line = %q, want %q (config file is read-only)", lines[0], GitIdentitySeedEmailWriteFailed)
	}
	if strings.TrimSpace(lines[1]) != GitIdentitySeedNameWriteFailed {
		t.Errorf("name line = %q, want %q (same read-only config file)", lines[1], GitIdentitySeedNameWriteFailed)
	}
}

// TestBuildGitIdentitySeedCommand_MixedOutcome is the Codex diff-review
// finding 2 mixed-result pin: user.email is left UNSET (absent → seeds)
// while user.name already carries a genuine custom value (present, not
// robot → preserved). Both outcomes are legitimate and clean — this is
// NOT an error case — but the two-line positional result must report
// them independently rather than collapsing into a single seeded/
// preserved flag for the whole identity.
func TestBuildGitIdentitySeedCommand_MixedOutcome(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	t.Parallel()
	derived := GitIdentity{Name: "octocat", Email: "octocat@users.noreply.github.com"}

	dir := t.TempDir()
	env := isolatedGitEnv(t)
	mustRunGit(t, dir, env, "init", "-q", "-b", "main")
	mustRunGit(t, dir, env, "config", "user.name", "Custom User")
	// user.email intentionally left unset.

	//nolint:gosec // test-only, dir is a t.TempDir path and derived is a test-local literal
	cmd := exec.CommandContext(context.Background(), "bash", "-c", BuildGitIdentitySeedCommand(dir, derived))
	cmd.Env = env
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("seed command failed: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(stdout), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected exactly 2 lines, got %d: %q", len(lines), stdout)
	}
	if strings.TrimSpace(lines[0]) != GitIdentitySeedEmailSeeded {
		t.Errorf("email line = %q, want %q (was absent)", lines[0], GitIdentitySeedEmailSeeded)
	}
	if strings.TrimSpace(lines[1]) != GitIdentitySeedNamePreserved {
		t.Errorf("name line = %q, want %q (genuine custom value)", lines[1], GitIdentitySeedNamePreserved)
	}
	if got := gitConfigGet(dir, env, "user.email"); got != derived.Email {
		t.Errorf("user.email = %q, want %q (seeded)", got, derived.Email)
	}
	if got := gitConfigGet(dir, env, "user.name"); got != "Custom User" {
		t.Errorf("user.name = %q, want Custom User (must survive untouched)", got)
	}
}

// TestBuildGitIdentitySeedCommand_Behavioral is the real-git proof of the
// three F3 item-2 outcomes: absent identity seeds, exact-robot identity
// gets replaced (the stomped-repo migration case), and a genuinely custom
// identity is left untouched.
func TestBuildGitIdentitySeedCommand_Behavioral(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	derived := GitIdentity{Name: "octocat", Email: "octocat@users.noreply.github.com"}

	t.Run("absent identity seeds", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		env := isolatedGitEnv(t)
		mustRunGit(t, dir, env, "init", "-q", "-b", "main")

		//nolint:gosec // test-only, dir is a t.TempDir path and derived is a test-local literal
		cmd := exec.CommandContext(context.Background(), "bash", "-c", BuildGitIdentitySeedCommand(dir, derived))
		cmd.Env = env
		stdout, err := cmd.Output()
		if err != nil {
			t.Fatalf("seed command failed: %v", err)
		}
		if !strings.Contains(string(stdout), GitIdentitySeedEmailSeeded) || !strings.Contains(string(stdout), GitIdentitySeedNameSeeded) {
			t.Errorf("expected both keys seeded, got dispatch output: %q", stdout)
		}
		if got := gitConfigGet(dir, env, "user.email"); got != derived.Email {
			t.Errorf("user.email = %q, want %q", got, derived.Email)
		}
		if got := gitConfigGet(dir, env, "user.name"); got != derived.Name {
			t.Errorf("user.name = %q, want %q", got, derived.Name)
		}
	})

	t.Run("exact-robot identity gets replaced", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		env := isolatedGitEnv(t)
		mustRunGit(t, dir, env, "init", "-q", "-b", "main")
		mustRunGit(t, dir, env, "config", "user.email", DeployGitIdentity.Email)
		mustRunGit(t, dir, env, "config", "user.name", DeployGitIdentity.Name)

		//nolint:gosec // test-only, dir is a t.TempDir path and derived is a test-local literal
		cmd := exec.CommandContext(context.Background(), "bash", "-c", BuildGitIdentitySeedCommand(dir, derived))
		cmd.Env = env
		stdout, err := cmd.Output()
		if err != nil {
			t.Fatalf("seed command failed: %v", err)
		}
		if !strings.Contains(string(stdout), GitIdentitySeedEmailSeeded) || !strings.Contains(string(stdout), GitIdentitySeedNameSeeded) {
			t.Errorf("expected both keys seeded (exact-robot migration), got dispatch output: %q", stdout)
		}
		if got := gitConfigGet(dir, env, "user.email"); got != derived.Email {
			t.Errorf("robot identity was not replaced: user.email = %q, want %q", got, derived.Email)
		}
		if got := gitConfigGet(dir, env, "user.name"); got != derived.Name {
			t.Errorf("robot identity was not replaced: user.name = %q, want %q", got, derived.Name)
		}
	})

	t.Run("custom identity is preserved", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		env := isolatedGitEnv(t)
		mustRunGit(t, dir, env, "init", "-q", "-b", "main")
		mustRunGit(t, dir, env, "config", "user.email", "custom@example.com")
		mustRunGit(t, dir, env, "config", "user.name", "Custom User")

		//nolint:gosec // test-only, dir is a t.TempDir path and derived is a test-local literal
		cmd := exec.CommandContext(context.Background(), "bash", "-c", BuildGitIdentitySeedCommand(dir, derived))
		cmd.Env = env
		stdout, err := cmd.Output()
		if err != nil {
			t.Fatalf("seed command failed: %v", err)
		}
		if !strings.Contains(string(stdout), GitIdentitySeedEmailPreserved) || !strings.Contains(string(stdout), GitIdentitySeedNamePreserved) {
			t.Errorf("expected both keys preserved (custom identity), got dispatch output: %q", stdout)
		}
		if got := gitConfigGet(dir, env, "user.email"); got != "custom@example.com" {
			t.Errorf("custom identity was overwritten: user.email = %q, want custom@example.com", got)
		}
		if got := gitConfigGet(dir, env, "user.name"); got != "Custom User" {
			t.Errorf("custom identity was overwritten: user.name = %q, want Custom User", got)
		}
	})
}

// TestBuildGitIdentityReadCommand_Shape pins the two-lines-always
// guarantee: printf captures git config's stdout (empty on absence) but
// still emits its own newline, so the output is always exactly two lines
// regardless of whether either key is set.
func TestBuildGitIdentityReadCommand_Shape(t *testing.T) {
	t.Parallel()
	cmd := BuildGitIdentityReadCommand("/var/www")
	for _, want := range []string{
		"cd '/var/www'",
		`printf '%s\n' "$(git config user.email)"`,
		`printf '%s\n' "$(git config user.name)"`,
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("read command missing %q:\n%s", want, cmd)
		}
	}
}

// TestBuildGitIdentityReadCommand_Behavioral proves the exactly-two-lines
// guarantee against a real git repo with NO identity configured at all —
// the scenario a naive `git config user.email; git config user.name`
// would get wrong (absent key produces zero output lines, shifting a
// line-indexed parse).
func TestBuildGitIdentityReadCommand_Behavioral(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	t.Parallel()
	dir := t.TempDir()
	env := isolatedGitEnv(t)
	mustRunGit(t, dir, env, "init", "-q", "-b", "main")

	//nolint:gosec // test-only, dir is a t.TempDir path
	cmd := exec.CommandContext(context.Background(), "bash", "-c", BuildGitIdentityReadCommand(dir))
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("read command failed: %v", err)
	}
	// TrimSuffix removes exactly the ONE trailing newline printf's second
	// call always emits — TrimRight would strip ALL trailing newlines and
	// collapse this exact all-empty case (each printf emits a bare "\n")
	// down to fewer than 2 elements, which is the bug this test exists to
	// catch (gitPushIdentityMigrationNote had it).
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected exactly 2 lines with no identity set, got %d: %q", len(lines), out)
	}
	if lines[0] != "" || lines[1] != "" {
		t.Errorf("expected both lines empty with no identity set, got %q", lines)
	}
}
