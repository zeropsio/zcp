// Tests for: ops/gitea_absorb.go — a squash (or merge-commit) landing of a
// Mate's own pull request is folded into the Mate's branch as a real merge,
// BEFORE the ordinary take-the-base-in step runs, so history stays
// forward-only (MB-26: merge, never rebase, never force) while the tree
// converges at the exact moment the delivery that observed the landing
// needed it to. A rebase-merge, or anything the container's git cannot
// prove lossless, is left for the ordinary merge to handle.
package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildGiteaDeliveryCommand_AbsorbsASquashLanding is the observed bug,
// reproduced: a Mate's own pull request is squash-merged onto the
// repository's protected base with the SAME tree the branch tip already
// carried, the Mate's checkout stays on the pre-squash tip, new work is
// committed on top, and a delivery must land it without an add/add conflict
// against history the squash discarded.
func TestBuildGiteaDeliveryCommand_AbsorbsASquashLanding(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := giteaBranchLab(t, nil)
	root := filepath.Dir(pair)
	remote := filepath.Join(root, "remote.git")
	runShell(t, BuildGiteaMateBranchCommand(pair, "mate/mate-p1", "main"))

	// PR #1: this Mate's own first delivery.
	writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
	runShell(t, BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Build the app", "", ""))
	branchTip := runGit(t, remote, "rev-parse", "mate/mate-p1")

	// Gitea squash-merges it: one commit on main, same tree as the branch
	// tip, parent = whatever main was before (the broker's Initial commit).
	other := filepath.Join(root, "other")
	runGit(t, root, "clone", "-q", remote, "other")
	runGit(t, other, "config", "user.email", "other@example.invalid")
	runGit(t, other, "config", "user.name", "other")
	runGit(t, other, "merge", "-q", "--squash", "origin/mate/mate-p1")
	runGit(t, other, "commit", "-q", "-m", "Build the app (#1)")
	runGit(t, other, "push", "-q", "origin", "main")
	squashSHA := runGit(t, remote, "rev-parse", "main")

	// The checkout is still on the pre-squash tip; new work lands on it —
	// the ordinary next task, unaware anything landed.
	writeLabFile(t, filepath.Join(pair, "footer.js"), "the footer\n")
	//nolint:gosec // test-only, the command under test against a t.TempDir repository
	out, err := exec.CommandContext(t.Context(), "sh", "-c",
		BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Add a footer", squashSHA, branchTip)).CombinedOutput()
	if err != nil {
		t.Fatalf("delivery after a squash landing of the Mate's own work: %v\n%s", err, out)
	}
	if got := GiteaDeliveryConflict(string(out)); got != "" {
		t.Fatalf("the squash must be absorbed without a conflict, got %q; output:\n%s", got, out)
	}

	// History stays forward-only: the delivered branch descends from BOTH
	// the squash commit and the pushed base — a real merge, never a rebase.
	for _, ancestor := range []string{squashSHA, "main"} {
		if err := exec.CommandContext(t.Context(), "git", "-C", remote,
			"merge-base", "--is-ancestor", ancestor, "mate/mate-p1").Run(); err != nil {
			t.Errorf("mate/mate-p1 must descend from %s: %v", ancestor, err)
		}
	}
	files := runGit(t, remote, "ls-tree", "-r", "--name-only", "mate/mate-p1")
	for _, want := range []string{"index.js", "footer.js"} {
		if !strings.Contains(files, want) {
			t.Errorf("the delivered tree misses %q; got %q", want, files)
		}
	}
}

// TestBuildGiteaDeliveryCommand_AbsorbsASquashLanding_AColleaguesWorkSurvives
// is the second shape the fix has to cover: the squash that landed this
// Mate's PR also carried a colleague's change committed on the base between
// this Mate's last delivery and the merge (a group's ordinary life — MB-26).
// Absorbing must not discard it.
func TestBuildGiteaDeliveryCommand_AbsorbsASquashLanding_AColleaguesWorkSurvives(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := giteaBranchLab(t, nil)
	root := filepath.Dir(pair)
	remote := filepath.Join(root, "remote.git")
	runShell(t, BuildGiteaMateBranchCommand(pair, "mate/mate-p1", "main"))

	writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
	runShell(t, BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Build the app", "", ""))
	branchTip := runGit(t, remote, "rev-parse", "mate/mate-p1")

	other := filepath.Join(root, "other")
	runGit(t, root, "clone", "-q", remote, "other")
	runGit(t, other, "config", "user.email", "other@example.invalid")
	runGit(t, other, "config", "user.name", "other")
	runGit(t, other, "merge", "-q", "--squash", "origin/mate/mate-p1")
	runGit(t, other, "commit", "-q", "-m", "Build the app (#1)")
	// A colleague's own, unrelated file, landed on main right after.
	writeLabFile(t, filepath.Join(other, "colleague.js"), "somebody else's work\n")
	runGit(t, other, "add", "-A")
	runGit(t, other, "commit", "-q", "-m", "A colleague's change")
	runGit(t, other, "push", "-q", "origin", "main")
	squashSHA := runGit(t, remote, "log", "-2", "--format=%H", "main")
	squashSHA = strings.Split(squashSHA, "\n")[1] // the squash, not the colleague's commit on top

	writeLabFile(t, filepath.Join(pair, "footer.js"), "the footer\n")
	//nolint:gosec // test-only, the command under test against a t.TempDir repository
	out, err := exec.CommandContext(t.Context(), "sh", "-c",
		BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Add a footer", squashSHA, branchTip)).CombinedOutput()
	if err != nil {
		t.Fatalf("delivery: %v\n%s", err, out)
	}
	if got := GiteaDeliveryConflict(string(out)); got != "" {
		t.Fatalf("no conflict was expected, got %q; output:\n%s", got, out)
	}
	files := runGit(t, remote, "ls-tree", "-r", "--name-only", "mate/mate-p1")
	for _, want := range []string{"index.js", "footer.js", "colleague.js"} {
		if !strings.Contains(files, want) {
			t.Errorf("the delivered tree misses %q; got %q", want, files)
		}
	}
	body, readErr := exec.CommandContext(t.Context(), "git", "-C", pair, "show", "HEAD:colleague.js").CombinedOutput()
	if readErr != nil || string(body) != "somebody else's work\n" {
		t.Errorf("the colleague's file = %q (%v), want it kept whole", body, readErr)
	}
}

// TestBuildGiteaDeliveryCommand_UnprovableLandingFallsThroughToTheOrdinaryMerge
// covers a rebase-merge (or any landing the container's git cannot prove
// lossless): the absorb step must skip it cleanly, never fail the delivery
// by itself, and leave the ordinary take-the-base-in merge to do the work —
// which still succeeds here because the two sides touch different files.
func TestBuildGiteaDeliveryCommand_UnprovableLandingFallsThroughToTheOrdinaryMerge(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := giteaBranchLab(t, nil)
	root := filepath.Dir(pair)
	remote := filepath.Join(root, "remote.git")
	runShell(t, BuildGiteaMateBranchCommand(pair, "mate/mate-p1", "main"))

	writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
	runShell(t, BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Build the app", "", ""))
	branchTip := runGit(t, remote, "rev-parse", "mate/mate-p1")

	// A fabricated "landing" whose tree does NOT match the branch tip's —
	// stands in for a rebase-merge (or any shape merge-tree cannot prove
	// lossless against branchTip's parent).
	other := filepath.Join(root, "other")
	runGit(t, root, "clone", "-q", remote, "other")
	runGit(t, other, "config", "user.email", "other@example.invalid")
	runGit(t, other, "config", "user.name", "other")
	writeLabFile(t, filepath.Join(other, "unrelated.js"), "not what the branch carried\n")
	runGit(t, other, "add", "-A")
	runGit(t, other, "commit", "-q", "-m", "an unprovable landing")
	runGit(t, other, "push", "-q", "origin", "main")
	unprovableSHA := runGit(t, remote, "rev-parse", "main")

	writeLabFile(t, filepath.Join(pair, "footer.js"), "the footer\n")
	//nolint:gosec // test-only, the command under test against a t.TempDir repository
	out, err := exec.CommandContext(t.Context(), "sh", "-c",
		BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Add a footer", unprovableSHA, branchTip)).CombinedOutput()
	if err != nil {
		t.Fatalf("an unprovable landing must fall through to the ordinary merge, not fail: %v\n%s", err, out)
	}
	if !GiteaAbsorbUnprovable(string(out)) {
		t.Errorf("a recorded landing that could not be proven lossless must be marked, output:\n%s", out)
	}
	files := runGit(t, remote, "ls-tree", "-r", "--name-only", "mate/mate-p1")
	for _, want := range []string{"index.js", "footer.js", "unrelated.js"} {
		if !strings.Contains(files, want) {
			t.Errorf("the delivered tree misses %q; got %q", want, files)
		}
	}
}

// TestBuildGiteaDeliveryCommand_ARealS1ConflictAbortsTheWholeChain is the
// blocker the judge reproduced (scen2.sh): a colleague X changes f and adds
// to k on main BEFORE this Mate's PR1 (touching g only) is squashed on top
// of X; the Mate then edits f on its own checkout. Absorbing S must merge
// S^1 (main as it stood right before the squash, i.e. X's own change) into
// HEAD — and HEAD's conflicting edit to f makes that merge conflict for
// real. The bug: the conflict handler was `|| (…; exit 4)` — a NESTED
// subshell, so `exit 4` only left THAT subshell; the `; `-joined chain
// carried on to `git merge -s ours` regardless, the whole absorb block
// exited 0, the ordinary step then saw origin/main as already an ancestor
// and skipped, and the delivery PUSHED a history that records S as merged
// while its tree still lacks X's change to k — the next pull request would
// propose REVERTING it on main. Fixed with a brace group, `|| { …; exit 4; }`,
// which exits the ENCLOSING subshell instead.
func TestBuildGiteaDeliveryCommand_ARealS1ConflictAbortsTheWholeChain(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair, remote, squashSHA, branchTip := giteaS1ConflictLab(t)
	preHead := runGit(t, pair, "rev-parse", "HEAD")

	out, err := exec.CommandContext(t.Context(), "sh", "-c", //nolint:gosec // test-only, the command under test against a t.TempDir repository
		BuildGiteaDeliveryCommand(pair, "mate/x", "main", "Add a footer", squashSHA, branchTip)).CombinedOutput()
	if err == nil {
		t.Fatalf("a real S^1 conflict must abort the whole chain, not push:\n%s", out)
	}
	if got := GiteaAbsorbConflict(string(out)); !strings.Contains(got, "f") {
		t.Fatalf("GiteaAbsorbConflict = %q, want f; output:\n%s", got, out)
	}

	if head := runGit(t, pair, "rev-parse", "HEAD"); head != preHead {
		t.Errorf("HEAD must be unchanged, was %s now %s", preHead, head)
	}
	if state := runGit(t, pair, "status", "--porcelain=v1", "--untracked-files=no"); strings.Contains(state, "UU") {
		t.Errorf("the checkout must be left whole, not half-merged: %q", state)
	}
	if got := runGit(t, remote, "rev-parse", "mate/x"); got != branchTip {
		t.Errorf("origin must be untouched, mate/x is still %s, got %s", branchTip, got)
	}
}

// TestBuildGiteaAbsorbAndSyncCommand_ARealS1ConflictAbortsCleanly is the
// same shape as the delivery test above, against the OTHER command that
// embeds the absorb — the one the reconcile-pass catch-up and a plain
// git-push's pre-push sync both run.
func TestBuildGiteaAbsorbAndSyncCommand_ARealS1ConflictAbortsCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair, _, squashSHA, branchTip := giteaS1ConflictLab(t)
	preHead := runGit(t, pair, "rev-parse", "HEAD")

	out, err := exec.CommandContext(t.Context(), "sh", "-c", //nolint:gosec // test-only, the command under test against a t.TempDir repository
		BuildGiteaAbsorbAndSyncCommand(pair, "main", squashSHA, branchTip)).CombinedOutput()
	if err == nil {
		t.Fatalf("a real S^1 conflict must abort, not silently succeed:\n%s", out)
	}
	if got := GiteaAbsorbConflict(string(out)); got == "" {
		t.Fatalf("GiteaAbsorbConflict = %q, want the conflicting file; output:\n%s", got, out)
	}
	if head := runGit(t, pair, "rev-parse", "HEAD"); head != preHead {
		t.Errorf("HEAD must be unchanged, was %s now %s", preHead, head)
	}
}

// giteaS1ConflictLab builds the scen2.sh shape: colleague X changes f and
// adds to k on main BEFORE this Mate's PR1 (touching g only) is squashed on
// top of X; the Mate's own checkout then edits f on the SAME line X did.
// Absorbing S has to merge S^1 (main as it stood right before the squash,
// i.e. X's own commit) into HEAD, and HEAD's conflicting edit to f makes
// that merge a REAL conflict — the shape the absorb's own conflict handler
// has to abort on, not the ordinary take-the-base-in step's.
func giteaS1ConflictLab(t *testing.T) (pair, remote, squashSHA, branchTip string) {
	t.Helper()
	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	runGit(t, root, "init", "--bare", "-q", "-b", "main", "remote.git")

	seed := filepath.Join(root, "seed")
	runGit(t, root, "init", "-q", "-b", "main", "seed")
	runGit(t, seed, "config", "user.email", "seed@example.invalid")
	runGit(t, seed, "config", "user.name", "seed")
	writeLabFile(t, filepath.Join(seed, "f"), "f1\nf2\nf3\n")
	writeLabFile(t, filepath.Join(seed, "g"), "g1\ng2\ng3\n")
	writeLabFile(t, filepath.Join(seed, "k"), "k1\n")
	runGit(t, seed, "add", "-A")
	runGit(t, seed, "commit", "-qm", "Initial")
	runGit(t, seed, "remote", "add", "origin", remote)
	runGit(t, seed, "push", "-q", "origin", "main")

	pair = filepath.Join(root, "mate")
	runGit(t, root, "clone", "-q", remote, "mate")
	runGit(t, pair, "config", "user.email", "mate@example.invalid")
	runGit(t, pair, "config", "user.name", "mate")
	runGit(t, pair, "checkout", "-qb", "mate/x")
	writeLabFile(t, filepath.Join(pair, "g"), "G1-H\ng2\ng3\n")
	runGit(t, pair, "commit", "-qam", "H1")
	branchTip = runGit(t, pair, "rev-parse", "HEAD")
	runGit(t, pair, "push", "-q", "origin", "mate/x")

	// Colleague X, on main, before the squash.
	writeLabFile(t, filepath.Join(seed, "f"), "F1-X\nf2\nf3\n")
	f, err := os.OpenFile(filepath.Join(seed, "k"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("k2-X\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	runGit(t, seed, "commit", "-qam", "X")
	runGit(t, seed, "push", "-q", "origin", "main")

	// Squash PR1 (mate/x) onto main, after X.
	runGit(t, seed, "fetch", "-q", "origin")
	runShell(t, "cd "+shellQuoteForTest(seed)+" && git merge --squash -q origin/mate/x >/dev/null 2>&1; git commit -qm 'Squash PR1'")
	squashSHA = runGit(t, seed, "rev-parse", "HEAD")
	runGit(t, seed, "push", "-q", "origin", "main")

	// The Mate's own checkout, unaware, edits the SAME line X touched in f.
	writeLabFile(t, filepath.Join(pair, "f"), "F1-MATE\nf2\nf3\n")
	runGit(t, pair, "commit", "-qam", "W")
	return pair, remote, squashSHA, branchTip
}

// shellQuoteForTest is a local, minimal single-quote escaper — the test
// composes a raw shell command directly (not through the package under
// test) to run `git merge --squash` outside any BuildXCommand helper.
func shellQuoteForTest(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// TestBuildGiteaDeliveryCommand_ARealConflictAfterTheAbsorbedLandingStillAborts
// pins that the absorb step does not swallow a genuine conflict: once the
// squash is folded in cleanly, a LATER colleague's change that collides with
// this Mate's own work must still stop the delivery exactly like the
// ordinary merge does, and leave the checkout whole.
func TestBuildGiteaDeliveryCommand_ARealConflictAfterTheAbsorbedLandingStillAborts(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := giteaBranchLab(t, nil)
	root := filepath.Dir(pair)
	remote := filepath.Join(root, "remote.git")
	runShell(t, BuildGiteaMateBranchCommand(pair, "mate/mate-p1", "main"))

	writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
	runShell(t, BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Build the app", "", ""))
	branchTip := runGit(t, remote, "rev-parse", "mate/mate-p1")

	other := filepath.Join(root, "other")
	runGit(t, root, "clone", "-q", remote, "other")
	runGit(t, other, "config", "user.email", "other@example.invalid")
	runGit(t, other, "config", "user.name", "other")
	runGit(t, other, "merge", "-q", "--squash", "origin/mate/mate-p1")
	runGit(t, other, "commit", "-q", "-m", "Build the app (#1)")
	runGit(t, other, "push", "-q", "origin", "main")
	squashSHA := runGit(t, remote, "rev-parse", "main")

	// A colleague's change collides on the SAME line this Mate is about to
	// write, landed on main after the squash the delivery is absorbing.
	writeLabFile(t, filepath.Join(other, "index.js"), "somebody else's line\n")
	runGit(t, other, "add", "-A")
	runGit(t, other, "commit", "-q", "-m", "A colliding change")
	runGit(t, other, "push", "-q", "origin", "main")

	writeLabFile(t, filepath.Join(pair, "index.js"), "my line\n")
	//nolint:gosec // test-only, the command under test against a t.TempDir repository
	out, err := exec.CommandContext(t.Context(), "sh", "-c",
		BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Change the app", squashSHA, branchTip)).CombinedOutput()
	if err == nil {
		t.Fatalf("a real conflict must still stop the delivery:\n%s", out)
	}
	if got := GiteaDeliveryConflict(string(out)); !strings.Contains(got, "index.js") {
		t.Fatalf("GiteaDeliveryConflict = %q, want index.js; output:\n%s", got, out)
	}
	if state := runGit(t, pair, "status", "--porcelain=v1", "--untracked-files=no"); strings.Contains(state, "UU") {
		t.Errorf("the checkout must be left whole, not half-merged: %q", state)
	}
}

// TestBuildAbsorbLandedPullRequestCommand_FallsBackToPortablePlumbingWhenMergeTreeFails
// pins L1(a) of the judge's review: on a container git without
// `merge-tree --write-tree` (or that call failing for any other reason), a
// genuinely lossless squash must still be provable — via a portable
// plumbing 3-way merge into a temporary index — rather than falling
// through to giteaAbsorbUnprovableMarker and reproducing the false
// add/add conflict this whole mechanism exists to prevent. A fake `git`
// ahead on PATH makes `merge-tree --write-tree` fail while every other
// git subcommand runs for real, standing in for "unavailable".
func TestBuildAbsorbLandedPullRequestCommand_FallsBackToPortablePlumbingWhenMergeTreeFails(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := giteaBranchLab(t, nil)
	root := filepath.Dir(pair)
	remote := filepath.Join(root, "remote.git")
	runShell(t, BuildGiteaMateBranchCommand(pair, "mate/mate-p1", "main"))

	writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
	runShell(t, BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Build the app", "", ""))
	branchTip := runGit(t, remote, "rev-parse", "mate/mate-p1")

	other := filepath.Join(root, "other")
	runGit(t, root, "clone", "-q", remote, "other")
	runGit(t, other, "config", "user.email", "other@example.invalid")
	runGit(t, other, "config", "user.name", "other")
	runGit(t, other, "merge", "-q", "--squash", "origin/mate/mate-p1")
	runGit(t, other, "commit", "-q", "-m", "Build the app (#1)")
	runGit(t, other, "push", "-q", "origin", "main")
	squashSHA := runGit(t, remote, "rev-parse", "main")

	writeLabFile(t, filepath.Join(pair, "footer.js"), "the footer\n")

	fakeGitPath := fakeGitRefusingMergeTreeWriteTree(t)
	//nolint:gosec // test-only, the command under test against a t.TempDir repository
	cmd := exec.CommandContext(t.Context(), "sh", "-c",
		BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Add a footer", squashSHA, branchTip))
	cmd.Env = append(os.Environ(), "PATH="+fakeGitPath+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("delivery with merge-tree unavailable: %v\n%s", err, out)
	}
	if GiteaAbsorbUnprovable(string(out)) {
		t.Fatalf("the portable fallback must still prove a genuine squash lossless, not fall through:\n%s", out)
	}
	if GiteaDeliveryConflict(string(out)) != "" || GiteaAbsorbConflict(string(out)) != "" {
		t.Fatalf("no conflict was expected:\n%s", out)
	}
	files := runGit(t, remote, "ls-tree", "-r", "--name-only", "mate/mate-p1")
	for _, want := range []string{"index.js", "footer.js"} {
		if !strings.Contains(files, want) {
			t.Errorf("the delivered tree misses %q; got %q", want, files)
		}
	}
	if err := exec.CommandContext(t.Context(), "git", "-C", remote,
		"merge-base", "--is-ancestor", "main", "mate/mate-p1").Run(); err != nil {
		t.Errorf("the delivered branch must descend from main: %v", err)
	}
}

// fakeGitRefusingMergeTreeWriteTree returns a directory holding a `git`
// wrapper that fails exactly `git merge-tree --write-tree ...` (simulating
// a container git older than 2.38) and execs the real git for everything
// else — prepend it to PATH to force BuildAbsorbLandedPullRequestCommand's
// fast path to fail while keeping every other git call real.
func fakeGitRefusingMergeTreeWriteTree(t *testing.T) string {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("no real git on PATH: %v", err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		`if [ "$1" = "merge-tree" ] && [ "$2" = "--write-tree" ]; then echo "fatal: unknown option --write-tree" >&2; exit 129; fi` + "\n" +
		"exec " + shellQuoteForTest(realGit) + ` "$@"` + "\n"
	path := filepath.Join(dir, "git")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	return dir
}

// TestBuildAbsorbLandedPullRequestCommand_UncommittedChangesBlockTheMerge
// pins L2 of the judge's review: uncommitted changes in a file S^1's merge
// would touch make git refuse to even start the merge — no unmerged index
// entry results, so GiteaAbsorbConflict's file list would read as empty,
// indistinguishable from "no conflict" if a caller only checked that. The
// dirty checkout must be its own marked case, checked BEFORE the merge is
// attempted, and the chain must abort — no merge, no push, checkout left
// exactly as it was (the uncommitted change included).
func TestBuildAbsorbLandedPullRequestCommand_UncommittedChangesBlockTheMerge(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := giteaBranchLab(t, nil)
	root := filepath.Dir(pair)
	remote := filepath.Join(root, "remote.git")
	runShell(t, BuildGiteaMateBranchCommand(pair, "mate/mate-p1", "main"))

	writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
	runShell(t, BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Build the app", "", ""))
	branchTip := runGit(t, remote, "rev-parse", "mate/mate-p1")

	other := filepath.Join(root, "other")
	runGit(t, root, "clone", "-q", remote, "other")
	runGit(t, other, "config", "user.email", "other@example.invalid")
	runGit(t, other, "config", "user.name", "other")
	runGit(t, other, "merge", "-q", "--squash", "origin/mate/mate-p1")
	runGit(t, other, "commit", "-q", "-m", "Build the app (#1)")
	runGit(t, other, "push", "-q", "origin", "main")
	squashSHA := runGit(t, remote, "rev-parse", "main")

	// An UNCOMMITTED edit to index.js — the exact file S^1's merge would
	// touch — never staged, never committed.
	writeLabFile(t, filepath.Join(pair, "index.js"), "an uncommitted edit\n")
	preHead := runGit(t, pair, "rev-parse", "HEAD")

	//nolint:gosec // test-only, the command under test against a t.TempDir repository
	out, err := exec.CommandContext(t.Context(), "sh", "-c",
		BuildGiteaAbsorbAndSyncCommand(pair, "main", squashSHA, branchTip)).CombinedOutput()
	if err == nil {
		t.Fatalf("uncommitted changes must block the absorb, not silently succeed:\n%s", out)
	}
	if !GiteaAbsorbDirty(string(out)) {
		t.Fatalf("want the dirty marker, got:\n%s", out)
	}
	if conflicts := GiteaAbsorbConflict(string(out)); conflicts != "" {
		t.Fatalf("a dirty-tree refusal is not a merge conflict, got file list %q", conflicts)
	}
	if head := runGit(t, pair, "rev-parse", "HEAD"); head != preHead {
		t.Errorf("HEAD must be unchanged, was %s now %s", preHead, head)
	}
	body, readErr := os.ReadFile(filepath.Join(pair, "index.js"))
	if readErr != nil || string(body) != "an uncommitted edit\n" {
		t.Errorf("the uncommitted edit must survive untouched, got %q (%v)", body, readErr)
	}
}
