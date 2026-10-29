// Tests for: ops/hq_base_branch.go — a pair's checkout must descend from the
// repository's `main`. Run against a temp BARE repository standing in for HQ:
// HQ makes `main` with one commit of the empty tree, zcp git-initialises the
// pair with a history of its own, and a branch that shares no history with a
// `main` holding code cannot be merged onto it; onto that unwritten root, HQ
// makes it `main`.
package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hqSeed is what HQ leaves on a repository's `main` when it makes one: one
// parentless commit of the empty tree.
func hqSeed(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "Initial commit")
}

// baseLab builds a bare "HQ" whose `main` carries HQ's seed, plus a
// pair repository wired to it as `origin`. seedPair runs inside the pair and
// decides what local history it has.
func baseLab(t *testing.T, seedPair func(t *testing.T, dir string)) (pairDir string) {
	t.Helper()
	return baseLabOn(t, hqSeed, seedPair)
}

// baseLabOn is baseLab with the base's history chosen too:
// seedBase runs in a clone whose `main` is then pushed as the remote's.
func baseLabOn(t *testing.T, seedBase, seedPair func(t *testing.T, dir string)) (pairDir string) {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	runGit(t, root, "init", "--bare", "-q", "-b", "main", "remote.git")

	seed := filepath.Join(root, "seed")
	runGit(t, root, "init", "-q", "-b", "main", "seed")
	runGit(t, seed, "config", "user.email", "hq@hq.invalid")
	runGit(t, seed, "config", "user.name", "HQ")
	seedBase(t, seed)
	runGit(t, seed, "remote", "add", "origin", remote)
	runGit(t, seed, "push", "-q", "origin", "main")

	pairDir = filepath.Join(root, "pair")
	runGit(t, root, "init", "-q", "-b", "main", "pair")
	runGit(t, pairDir, "config", "user.email", "mate@example.invalid")
	runGit(t, pairDir, "config", "user.name", "mate")
	runGit(t, pairDir, "remote", "add", "origin", remote)
	if seedPair != nil {
		seedPair(t, pairDir)
	}
	return pairDir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitOr is runGit for a read that may legitimately fail (an unborn HEAD, a
// missing branch): it answers "" instead of failing the test.
func gitOr(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func writeLabFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// runShell executes a built command the way ExecSSH would on the container.
func runShell(t *testing.T, command string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "-c", command)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("command failed: %v\ncommand: %s\noutput:\n%s", err, command, out)
	}
}

// commitAll commits the pair's whole tree.
func commitAll(t *testing.T, dir, message string) {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", message)
}

// zcpInitMarker makes the pair's HEAD what InitServiceGit leaves: one
// parentless commit of the empty tree.
func zcpInitMarker(t *testing.T, dir string) {
	t.Helper()
	tree := runGit(t, dir, "mktree")
	sha := runGit(t, dir, "commit-tree", tree, "-m", "zcp init")
	runGit(t, dir, "update-ref", "HEAD", sha)
}

// emptyTree is git's well-known empty tree — the pre-step tree of a pair
// with no commits at all.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// baseOutcome is what a branch step row expects.
type baseOutcome int

const (
	// branchJoined: the pair's tree and working copy are untouched. Only the
	// plain-project door joins HQ's seed (TestBuildProjectRepositoryCommand_JoinsTheSeed).
	branchJoined baseOutcome = iota
	// branchFromBase: a marker-only pair now holds the base's tree.
	branchFromBase
	// branchRefused: named in the output, and nothing moved.
	branchRefused
	// branchNamed: a history the base moved past is named where it is — its
	// commits and its working copy untouched, for the agent to take the
	// base in.
	branchNamed
	// branchFirstHistory: a history of its own against HQ's unwritten root
	// is named `main` exactly as it is — no commit made, HQ's root never
	// joined in — so the change pushed from it lands as `main`.
	branchFirstHistory
)

type baseCase struct {
	name string
	// base builds the remote's `main`; nil is HQ's seed.
	base func(t *testing.T, dir string)
	seed func(t *testing.T, dir string)
	want baseOutcome
	// refusal is the state a refused row names, with what it names.
	refusal string
	// wantFiles is path → content expected in the working tree after.
	wantFiles map[string]string
}

func baseCases() []baseCase {
	return []baseCase{
		{
			name: "local commits on HQ's unwritten root stay as they are, named main",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "README.md"), "the pair's README\n")
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "the first page")
			},
			want: branchFirstHistory,
			wantFiles: map[string]string{
				"README.md": "the pair's README\n",
				"index.js":  "the app\n",
			},
		},
		{
			name: "a two-root history with a hand-resolved merge",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "app.txt"), "base\n")
				commitAll(t, dir, "root one")
				runGit(t, dir, "checkout", "-q", "-b", "feat")
				writeLabFile(t, filepath.Join(dir, "app.txt"), "feat\n")
				commitAll(t, dir, "feat")
				runGit(t, dir, "checkout", "-q", "main")
				writeLabFile(t, filepath.Join(dir, "app.txt"), "main\n")
				commitAll(t, dir, "main")
				// The conflict is settled by hand, to neither side.
				cmd := exec.CommandContext(t.Context(), "git", "merge", "-q", "feat")
				cmd.Dir = dir
				_ = cmd.Run()
				writeLabFile(t, filepath.Join(dir, "app.txt"), "resolved\n")
				commitAll(t, dir, "merge feat")
				runGit(t, dir, "branch", "-q", "-D", "feat")
				// A second root, merged in: the recipe shape.
				runGit(t, dir, "checkout", "-q", "--orphan", "other")
				runGit(t, dir, "rm", "-rqf", ".")
				writeLabFile(t, filepath.Join(dir, "other.txt"), "other root\n")
				runGit(t, dir, "add", "other.txt")
				runGit(t, dir, "commit", "-qm", "root two")
				runGit(t, dir, "checkout", "-q", "main")
				runGit(t, dir, "merge", "-q", "--allow-unrelated-histories", "--no-edit", "other")
				runGit(t, dir, "branch", "-q", "-D", "other")
			},
			want: branchFirstHistory,
			wantFiles: map[string]string{
				"app.txt":   "resolved\n",
				"other.txt": "other root\n",
			},
		},
		{
			name: "an unstaged edit to a tracked file stays unstaged",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "zerops.yaml"), "zerops: []\n")
				commitAll(t, dir, "init")
				writeLabFile(t, filepath.Join(dir, "zerops.yaml"), "zerops: [edited]\n")
			},
			want:      branchFirstHistory,
			wantFiles: map[string]string{"zerops.yaml": "zerops: [edited]\n"},
		},
		{
			name: "the zcp-init marker with an untracked README",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				zcpInitMarker(t, dir)
				writeLabFile(t, filepath.Join(dir, "README.md"), "written, never committed\n")
			},
			want:      branchFromBase,
			wantFiles: map[string]string{"README.md": "written, never committed\n"},
		},
		{
			name: "the zcp-init marker alone",
			seed: zcpInitMarker,
			want: branchFromBase,
		},
		{
			name: "a pair with no local commits",
			seed: nil,
			want: branchFromBase,
		},
		{
			name: "a staged change stays staged on a history of its own",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "init")
				writeLabFile(t, filepath.Join(dir, "index.js"), "staged\n")
				runGit(t, dir, "add", "index.js")
			},
			want:      branchFirstHistory,
			wantFiles: map[string]string{"index.js": "staged\n"},
		},
		{
			name: "a history of its own on another branch, no main, is named main where it is",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				runGit(t, dir, "checkout", "-q", "-b", "imported")
				writeLabFile(t, filepath.Join(dir, "index.js"), "the imported app\n")
				commitAll(t, dir, "the imported history")
			},
			want:      branchFirstHistory,
			wantFiles: map[string]string{"index.js": "the imported app\n"},
		},
		{
			name: "a marker-only pair with a staged file is refused cleanly",
			base: landedBase,
			seed: func(t *testing.T, dir string) {
				t.Helper()
				zcpInitMarker(t, dir)
				writeLabFile(t, filepath.Join(dir, "notes.txt"), "staged, never committed\n")
				runGit(t, dir, "add", "notes.txt")
			},
			want:    branchRefused,
			refusal: "ZCP_STAGED_CHANGES",
		},
		{
			name: "main already exists and HEAD is elsewhere",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "init")
				runGit(t, dir, "checkout", "-q", "-b", "feature")
			},
			want:    branchRefused,
			refusal: "ZCP_BRANCH_ELSEWHERE",
		},
		{
			name: "a history main left behind is named where it is, its work kept",
			base: landedBase,
			seed: func(t *testing.T, dir string) {
				t.Helper()
				runGit(t, dir, "fetch", "-q", "origin", "main")
				runGit(t, dir, "checkout", "-q", "-b", "feature", "FETCH_HEAD~1")
				writeLabFile(t, filepath.Join(dir, "todo.js"), "this Mate's open work\n")
				commitAll(t, dir, "Add todos")
				writeLabFile(t, filepath.Join(dir, "todo.js"), "not committed yet\n")
			},
			want:      branchNamed,
			wantFiles: map[string]string{"todo.js": "not committed yet\n"},
		},
		{
			name: "a base with landed code and an unrelated pair history",
			base: landedBase,
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "app.js"), "this pair's work\n")
				commitAll(t, dir, "init")
			},
			want:    branchRefused,
			refusal: "ZCP_BASE_NOT_SEED",
		},
		{
			name: "a one-commit base that holds code",
			base: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "README.md"), "readme\n")
				writeLabFile(t, filepath.Join(dir, "app.js"), "code\n")
				commitAll(t, dir, "everything at once")
			},
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "init")
			},
			want:    branchRefused,
			refusal: "ZCP_BASE_NOT_SEED",
		},
		{
			name: "a one-commit base of only a README is no seed of HQ's",
			base: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "README.md"), "readme\n")
				commitAll(t, dir, "Initial commit")
			},
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "init")
			},
			want:    branchRefused,
			refusal: "ZCP_BASE_NOT_SEED",
		},
		{
			name: "a marker-only pair against a populated main branches from main",
			base: landedBase,
			seed: func(t *testing.T, dir string) {
				t.Helper()
				zcpInitMarker(t, dir)
				writeLabFile(t, filepath.Join(dir, "notes.txt"), "untracked, not on main\n")
			},
			want: branchFromBase,
			wantFiles: map[string]string{
				"app.js":    "the first Mate's work\n",
				"notes.txt": "untracked, not on main\n",
			},
		},
		{
			name: "a marker-only pair whose untracked file main would overwrite",
			base: landedBase,
			seed: func(t *testing.T, dir string) {
				t.Helper()
				zcpInitMarker(t, dir)
				writeLabFile(t, filepath.Join(dir, "app.js"), "untracked work\n")
			},
			want:      branchRefused,
			refusal:   "ZCP_UNTRACKED_COLLISION app.js",
			wantFiles: map[string]string{"app.js": "untracked work\n"},
		},
	}
}

// TestBuildBaseBranchCommand_DescendsFromMain is the table the wiring
// owes. Whatever local history the pair has, it ends on its own branch
// sharing its history with `origin/main`, or — against HQ's unwritten root,
// one parentless commit of the empty tree — on `main` holding its own
// history exactly as it is, or it is refused by name and nothing moved.
// A history of its own keeps its commits, tree and working copy byte for
// byte, and HQ's root is never joined into it (the owner, 2026-10-27: clean
// history, no merge commits): HQ makes the change pushed from it `main`. The
// pair's code is not zcp's to rewrite (the rebase the join replaced lost a
// hand-resolved merge in wasp-hello-world-app — measured 2026-09-24).
func TestBuildBaseBranchCommand_DescendsFromMain(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	for _, tt := range baseCases() {
		t.Run(tt.name, func(t *testing.T) {
			base := tt.base
			if base == nil {
				base = hqSeed
			}
			pair := baseLabOn(t, base, tt.seed)
			before := snapshotPair(t, pair)

			//nolint:gosec // test-only, the command under test against a t.TempDir repository
			out, err := exec.CommandContext(t.Context(), "sh", "-c",
				BuildBaseBranchCommand(pair)).CombinedOutput()

			assertLabFiles(t, pair, tt.wantFiles)
			switch tt.want {
			case branchRefused:
				assertRefusedNothingMoved(t, pair, before, tt.refusal, string(out), err)
			case branchFromBase:
				assertOnBase(t, pair, string(out), err)
				if got, want := runGit(t, pair, "rev-parse", "HEAD"), runGit(t, pair, "rev-parse", "origin/main"); got != want {
					t.Errorf("a marker-only pair must branch AT main: HEAD %s, main %s", got, want)
				}
			case branchNamed:
				if err != nil {
					t.Fatalf("command failed: %v\noutput:\n%s", err, out)
				}
				if got := runGit(t, pair, "rev-parse", "--abbrev-ref", "HEAD"); got != baseBranch {
					t.Errorf("HEAD is on %q, want %s", got, baseBranch)
				}
				if got := runGit(t, pair, "rev-parse", "HEAD"); got != before.head {
					t.Errorf("the history moved: HEAD %s → %s", before.head, got)
				}
				if got := runGit(t, pair, "status", "--porcelain"); got != before.status {
					t.Errorf("the working copy changed:\nbefore:\n%s\nafter:\n%s", before.status, got)
				}
				runGit(t, pair, "merge-base", "origin/main", "HEAD")
			case branchFirstHistory:
				if err != nil {
					t.Fatalf("command failed: %v\noutput:\n%s", err, out)
				}
				if got := runGit(t, pair, "symbolic-ref", "HEAD"); got != "refs/heads/"+baseBranch {
					t.Errorf("HEAD is %s, want refs/heads/%s", got, baseBranch)
				}
				if got := runGit(t, pair, "rev-parse", "HEAD"); got != before.head {
					t.Errorf("the history moved, a commit was made on it: HEAD %s → %s\n%s", before.head, got,
						runGit(t, pair, "log", "--oneline", "--graph", "--all"))
				}
				if exec.CommandContext(t.Context(), "git", "-C", pair, "merge-base", "--is-ancestor", "origin/main", "HEAD").Run() == nil {
					t.Errorf("HQ's unwritten root was joined into the history:\n%s", runGit(t, pair, "log", "--oneline", "--graph", "--all"))
				}
				if got := runGit(t, pair, "rev-parse", "HEAD^{tree}"); got != before.tree {
					t.Errorf("the pair's tree changed: %s → %s", before.tree, got)
				}
				if got := runGit(t, pair, "status", "--porcelain"); got != before.status {
					t.Errorf("the working copy changed:\nbefore:\n%s\nafter:\n%s", before.status, got)
				}
			case branchJoined:
				assertOnBase(t, pair, string(out), err)
				if got := runGit(t, pair, "rev-parse", "HEAD^{tree}"); got != before.tree {
					t.Errorf("the pair's tree changed: %s → %s\n%s", before.tree, got,
						gitOr(t, pair, "diff", "--stat", before.tree, "HEAD"))
				}
				if got := runGit(t, pair, "status", "--porcelain"); got != before.status {
					t.Errorf("the working copy changed:\nbefore:\n%s\nafter:\n%s", before.status, got)
				}
			}
		})
	}
}

// landedBase is a base another Mate's work has already landed on: the
// HQ's seed and real code on top.
func landedBase(t *testing.T, dir string) {
	t.Helper()
	hqSeed(t, dir)
	writeLabFile(t, filepath.Join(dir, "app.js"), "the first Mate's work\n")
	commitAll(t, dir, "landed")
}

// runRemedyShell runs a remedy's commands in the pair, as the agent would.
func runRemedyShell(t *testing.T, pair, command string) {
	t.Helper()
	runShell(t, "cd "+shellQuote(pair)+" && "+command)
}

// remedyCommand is the one command in backticks a remedy names after "run".
func remedyCommand(t *testing.T, remedy string) string {
	t.Helper()
	_, rest, ok := strings.Cut(remedy, "run `")
	command, _, closed := strings.Cut(rest, "`")
	if !ok || !closed {
		t.Fatalf("the remedy names no command to run: %s", remedy)
	}
	return command
}

// zcpGitStandIn is a shell function standing in for `zcp git appdev <args>`
// as git run in pair, appdev's checkout, prefixed to a command line.
func zcpGitStandIn(pair string) string {
	return `zcp() { [ "$1" = git ] && [ "$2" = appdev ] || return 99; shift 2; git -C ` + shellQuote(pair) + ` "$@"; }; `
}

// TestBaseBranchRefusal_NotSeed_CarriesTheCommit: zcp's own command reads the
// checkout's commit in the dev service when it refuses a history sharing
// nothing with a main that holds code, and the refusal carries it — so the
// remedy can name it literally, with nothing left for the agent's shell to
// work out.
func TestBaseBranchRefusal_NotSeed_CarriesTheCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := baseLabOn(t, landedBase, func(t *testing.T, dir string) {
		t.Helper()
		writeLabFile(t, filepath.Join(dir, "app.js"), "this pair's work\n")
		commitAll(t, dir, "init")
	})
	head := gitOr(t, pair, "rev-parse", "HEAD")
	command := BuildBaseBranchCommand(pair)

	out, err := exec.CommandContext(t.Context(), "sh", "-c", command).CombinedOutput()

	if err == nil {
		t.Fatalf("want a refusal, the command succeeded:\n%s", out)
	}
	if got, want := BaseBranchRefusal(string(out)), "ZCP_BASE_NOT_SEED "+head[:12]; got != want {
		t.Errorf("BaseBranchRefusal = %q, want %q; output:\n%s", got, want, out)
	}
}

// TestBaseBranchRefusalRemedy_Mate_NamesTheCommitLiterally: a Mate's remedy
// runs every git command in the dev service through `zcp git <host>` and
// names the commit its refusal carries literally — the branch keeps that
// commit however often it is run — with no command substitution an agent's
// shell would work out, and none that fails open.
func TestBaseBranchRefusalRemedy_Mate_NamesTheCommitLiterally(t *testing.T) {
	t.Parallel()
	mate := RemedyRunner{DevHost: "appdev"}
	tests := []struct {
		refusal string
		want    []string
	}{
		{refusal: "ZCP_BASE_NOT_SEED 0123456789ab", want: []string{
			"run `zcp git appdev branch -f own-history-0123456789ab 0123456789ab && zcp git appdev fetch origin 'main' && zcp git appdev reset --keep FETCH_HEAD`",
			"`zcp git appdev cherry-pick`", "never joined", "ask the person first",
		}},
		{refusal: "ZCP_BASE_NOT_SEED", want: []string{"`zcp git appdev rev-parse --short=12 HEAD`", "own-history-<that commit>"}},
		{refusal: "ZCP_BRANCH_ELSEWHERE", want: []string{"run `zcp git appdev checkout 'main'`"}},
		{refusal: "ZCP_STAGED_CHANGES", want: []string{"run `zcp git appdev restore --staged .`"}},
		{refusal: "ZCP_UNTRACKED_COLLISION app.js", want: []string{"(app.js)", "move or delete them"}},
	}
	for _, tt := range tests {
		t.Run(tt.refusal, func(t *testing.T) {
			t.Parallel()
			remedy := BaseBranchRefusalRemedy(tt.refusal, mate)
			for _, want := range tt.want {
				if !strings.Contains(remedy, want) {
					t.Errorf("the remedy lacks %q: %s", want, remedy)
				}
			}
			for _, never := range []string{"$(", "zcp hq git", "`git ", "&& git "} {
				if strings.Contains(remedy, never) {
					t.Errorf("the remedy says %q: %s", never, remedy)
				}
			}
		})
	}
}

// TestBaseBranchRefusalRemedy_PlainProject_Unchanged: a project without a
// Mate keeps its git on the mounted checkout, reading HQ through `zcp hq
// git` (docs/spec-mate.md §6.7): its remedies are byte for byte what they
// were.
func TestBaseBranchRefusalRemedy_PlainProject_Unchanged(t *testing.T) {
	t.Parallel()
	for refusal, want := range map[string]string{
		"ZCP_BASE_NOT_SEED":              "\"main\" already holds code this history shares nothing with, and two unrelated histories are never joined: keep this history on a branch of its own and put the working copy on \"main\" — run `git branch -f \"own-history-$(git rev-parse --short=12 HEAD)\" && zcp hq git fetch origin 'main' && git reset --keep FETCH_HEAD`, which can be run again as it is — then carry what it needs over with `git cherry-pick` onto a branch cut from \"main\". That replaces the dev service's files with \"main\"'s code: when that code is not this project's own, as after the Mate moved to another application, ask the person first.",
		"ZCP_BRANCH_ELSEWHERE":           "\"main\" already exists and the working copy is on another branch: run `git checkout 'main'`.",
		"ZCP_STAGED_CHANGES":             "the working copy is about to take \"main\"'s files and has changes staged: run `git restore --staged .` (the files stay in the working copy).",
		"ZCP_UNTRACKED_COLLISION app.js": "\"main\" carries files the working copy has untracked (app.js): move or delete them.",
	} {
		if got := BaseBranchRefusalRemedy(refusal, RemedyRunner{}); got != want {
			t.Errorf("%s:\n got %s\nwant %s", refusal, got, want)
		}
	}
}

// TestBaseBranchRefusalRemedy_TheNextAttemptWires: a refusal the agent is
// only told to wait out is a pair stuck for good — the push guard refuses
// every push until the wiring completes. Each refusal names one remedy, and
// once it is done the very next attempt puts the pair on its branch.
func TestBaseBranchRefusalRemedy_TheNextAttemptWires(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	for _, tc := range []struct {
		name    string
		base    func(t *testing.T, dir string)
		seed    func(t *testing.T, dir string)
		refusal string
		// remedy is what the sentence must name.
		remedy []string
		// apply does the remedy the way the agent would.
		apply func(t *testing.T, pair string)
	}{
		{
			name: "unrelated own history against a base that holds code",
			base: landedBase,
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "app.js"), "this pair's work\n")
				commitAll(t, dir, "init")
			},
			refusal: "ZCP_BASE_NOT_SEED <head>",
			remedy:  []string{"`zcp git appdev branch -f own-history-<head> <head> && zcp git appdev fetch origin 'main' && zcp git appdev reset --keep FETCH_HEAD`", "never joined", "zcp git appdev cherry-pick", "ask the person first"},
			apply: func(t *testing.T, pair string) {
				t.Helper()
				own := gitOr(t, pair, "rev-parse", "HEAD")
				command := remedyCommand(t, BaseBranchRefusalRemedy("ZCP_BASE_NOT_SEED "+own[:12], RemedyRunner{DevHost: "appdev"}))
				// Run twice: a remedy stopped half-way is run again as it is,
				// and run again after it went through it keeps the same commit.
				for range 2 {
					runRemedyShell(t, pair, zcpGitStandIn(pair)+command)
				}
				if got := gitOr(t, pair, "rev-parse", "own-history-"+own[:12]); got != own {
					t.Errorf("own-history-%s = %s, want the pair's own history %s kept", own[:12], got, own)
				}
			},
		},
		{
			name: "main exists and HEAD is elsewhere",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				runGit(t, dir, "fetch", "-q", "origin", "main")
				runGit(t, dir, "checkout", "-q", "-b", "main", "FETCH_HEAD")
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "init")
				runGit(t, dir, "checkout", "-q", "-b", "feature")
			},
			refusal: "ZCP_BRANCH_ELSEWHERE",
			remedy:  []string{"`zcp git appdev checkout 'main'`"},
			apply: func(t *testing.T, pair string) {
				t.Helper()
				runRemedyShell(t, pair, "git checkout -q 'main'")
			},
		},
		{
			name: "a marker-only pair with a staged file",
			base: landedBase,
			seed: func(t *testing.T, dir string) {
				t.Helper()
				zcpInitMarker(t, dir)
				writeLabFile(t, filepath.Join(dir, "notes.txt"), "staged\n")
				runGit(t, dir, "add", "notes.txt")
			},
			refusal: "ZCP_STAGED_CHANGES",
			remedy:  []string{"`zcp git appdev restore --staged .`"},
			apply: func(t *testing.T, pair string) {
				t.Helper()
				runRemedyShell(t, pair, "git restore --staged .")
			},
		},
		{
			name: "a marker-only pair whose untracked file the base would overwrite",
			base: landedBase,
			seed: func(t *testing.T, dir string) {
				t.Helper()
				zcpInitMarker(t, dir)
				writeLabFile(t, filepath.Join(dir, "app.js"), "untracked work\n")
			},
			refusal: "ZCP_UNTRACKED_COLLISION app.js",
			remedy:  []string{"app.js", "move or delete"},
			apply: func(t *testing.T, pair string) {
				t.Helper()
				if err := os.Rename(filepath.Join(pair, "app.js"), filepath.Join(filepath.Dir(pair), "app.js.kept")); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := tc.base
			if base == nil {
				base = hqSeed
			}
			pair := baseLabOn(t, base, tc.seed)
			command := BuildBaseBranchCommand(pair)

			head := gitOr(t, pair, "rev-parse", "--short=12", "HEAD")
			out, err := exec.CommandContext(t.Context(), "sh", "-c", command).CombinedOutput()
			if err == nil {
				t.Fatalf("want a refusal, the command succeeded:\n%s", out)
			}
			refusal := BaseBranchRefusal(string(out))
			if want := strings.ReplaceAll(tc.refusal, "<head>", head); refusal != want {
				t.Fatalf("BaseBranchRefusal = %q, want %q; output:\n%s", refusal, want, out)
			}
			remedy := BaseBranchRefusalRemedy(refusal, RemedyRunner{DevHost: "appdev"})
			for _, want := range tc.remedy {
				if want = strings.ReplaceAll(want, "<head>", head); !strings.Contains(remedy, want) {
					t.Errorf("the remedy for %s must name %q:\n%s", refusal, want, remedy)
				}
			}
			if strings.Contains(remedy, "next pass") {
				t.Errorf("a remedy is something to do, not a wait:\n%s", remedy)
			}

			tc.apply(t, pair)
			out, err = exec.CommandContext(t.Context(), "sh", "-c", command).CombinedOutput()
			assertOnBase(t, pair, string(out), err)
		})
	}
}

// pairSnapshot is what a branch step must leave alone.
type pairSnapshot struct {
	tree, head, headRef, branch, status string
}

func snapshotPair(t *testing.T, pair string) pairSnapshot {
	t.Helper()
	tree := gitOr(t, pair, "rev-parse", "HEAD^{tree}")
	if tree == "" {
		tree = emptyTree
	}
	return pairSnapshot{
		tree:    tree,
		head:    gitOr(t, pair, "rev-parse", "HEAD"),
		headRef: gitOr(t, pair, "symbolic-ref", "HEAD"),
		branch:  gitOr(t, pair, "rev-parse", "-q", "--verify", "refs/heads/"+baseBranch+""),
		status:  runGit(t, pair, "status", "--porcelain"),
	}
}

func assertLabFiles(t *testing.T, pair string, want map[string]string) {
	t.Helper()
	for path, body := range want {
		got, err := os.ReadFile(filepath.Join(pair, path))
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		if string(got) != body {
			t.Errorf("%s = %q, want %q", path, got, body)
		}
	}
}

func assertRefusedNothingMoved(t *testing.T, pair string, before pairSnapshot, refusal, out string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("want a refusal, the command succeeded:\n%s", out)
	}
	if refusal == "ZCP_BASE_NOT_SEED" {
		refusal += " " + before.head[:12]
	}
	if got := BaseBranchRefusal(out); got != refusal {
		t.Errorf("BaseBranchRefusal = %q, want %q; output:\n%s", got, refusal, out)
	}
	if after := snapshotPair(t, pair); after != before {
		t.Errorf("a refusal moved something:\nbefore: %+v\nafter:  %+v", before, after)
	}
}

func assertOnBase(t *testing.T, pair, out string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("command failed: %v\noutput:\n%s", err, out)
	}
	if got := runGit(t, pair, "rev-parse", "--abbrev-ref", "HEAD"); got != baseBranch {
		t.Errorf("HEAD is on %q, want %s", got, baseBranch)
	}
	// The whole point: HQ's squash needs a shared history.
	if err := exec.CommandContext(t.Context(), "git", "-C", pair, "merge-base", "--is-ancestor", "origin/main", "HEAD").Run(); err != nil {
		t.Errorf("the branch does not descend from origin/main: %v\n%s",
			err, runGit(t, pair, "log", "--oneline", "--all"))
	}
}

// TestBuildBaseBranchCommand_Idempotent pins the reconcile property: a
// second pass on a branch already based on `main` rewrites no commit — a
// rebase that ran unconditionally would change every sha under an open pull
// request on every pass.
func TestBuildBaseBranchCommand_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := baseLab(t, func(t *testing.T, dir string) {
		t.Helper()
		writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
		runGit(t, dir, "add", "-A")
		runGit(t, dir, "commit", "-qm", "the first page")
	})
	cmd := BuildBaseBranchCommand(pair)
	runShell(t, cmd)
	first := runGit(t, pair, "rev-parse", "HEAD")
	runShell(t, cmd)
	if second := runGit(t, pair, "rev-parse", "HEAD"); second != first {
		t.Errorf("a second pass moved HEAD from %s to %s", first, second)
	}
}

// TestBuildBaseBranchCommand_Shape pins the pieces that cannot be seen from
// the outcome: the fetch authenticates through HQ's session credential
// helper (the credential never reaches argv), `main` is fetched BEFORE the
// branch is decided, and a Mate's wiring makes no commit at all — HQ's
// unwritten root is never joined into a history (no `commit-tree`, no
// unrelated-histories merge), and no porcelain replays, rewrites or runs the
// pair's hooks. The plain-project door keeps its join of HQ's seed.
func TestBuildBaseBranchCommand_Shape(t *testing.T) {
	t.Parallel()

	cmd := BuildBaseBranchCommand("/var/www")
	for _, want := range []string{
		"cd '/var/www'",
		"credential.helper=",
		"echo username=mate",
		"fetch --no-tags origin 'main'",
		"b='main'",
		"git merge-base FETCH_HEAD HEAD",
		"git symbolic-ref HEAD",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command missing %q:\n%s", want, cmd)
		}
	}
	if fetch, decide := strings.Index(cmd, "fetch --no-tags"), strings.Index(cmd, "git merge-base FETCH_HEAD HEAD"); fetch > decide {
		t.Errorf("the base must be fetched before the branch is decided from FETCH_HEAD:\n%s", cmd)
	}
	// Never a join of HQ's root, a rebase or a porcelain merge (they rewrite
	// the pair's history or its working copy), never `-B`, a force or a
	// reset: moving or resetting an existing branch would discard work the
	// Mate has already pushed.
	for _, absent := range []string{`commit-tree "HEAD^{tree}"`, "-p FETCH_HEAD", "Join the repository's base", "allow-unrelated-histories", "rebase", "git merge ", "checkout -B", "--force", "reset"} {
		if strings.Contains(cmd, absent) {
			t.Errorf("command must not carry %q:\n%s", absent, cmd)
		}
	}

	plain := BuildProjectRepositoryCommand("/var/www", "https://hq.invalid/git/o/a/appdev.git", "https://hq.invalid")
	if !strings.Contains(plain, `commit-tree "HEAD^{tree}" -p HEAD -p FETCH_HEAD`) {
		t.Errorf("the plain-project door must keep its join of HQ's seed:\n%s", plain)
	}
}

// TestBuildProjectRepositoryCommand_JoinsTheSeed pins the plain-project door
// as it was: a plain project pushes `main` itself, with no change for HQ to
// land, so its history of its own is still joined onto HQ's seed — tree and
// working copy untouched — and its `main` descends from HQ's.
func TestBuildProjectRepositoryCommand_JoinsTheSeed(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := baseLab(t, func(t *testing.T, dir string) {
		t.Helper()
		writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
		commitAll(t, dir, "the first page")
	})
	before := snapshotPair(t, pair)
	remote := runGit(t, pair, "remote", "get-url", "origin")

	cmd := exec.CommandContext(t.Context(), "sh", "-c", BuildProjectRepositoryCommand(pair, remote, "https://hq.invalid")) //nolint:gosec // test-only, the command under test against a t.TempDir repository
	cmd.Stdin = strings.NewReader("project-key\n")
	out, err := cmd.CombinedOutput()

	assertOnBase(t, pair, string(out), err)
	if got := runGit(t, pair, "rev-parse", "HEAD^{tree}"); got != before.tree {
		t.Errorf("the pair's tree changed: %s → %s", before.tree, got)
	}
	if got := runGit(t, pair, "status", "--porcelain"); got != before.status {
		t.Errorf("the working copy changed:\nbefore:\n%s\nafter:\n%s", before.status, got)
	}
}

// unseededLab is baseLab for a repository HQ holds with no `main` yet (made by
// connect, follow or createRepo, not seeded): a bare remote with nothing in it.
func unseededLab(t *testing.T, seedPair func(t *testing.T, dir string)) (pairDir string) {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "--bare", "-q", "-b", "main", "remote.git")
	pairDir = filepath.Join(root, "pair")
	runGit(t, root, "init", "-q", "-b", "main", "pair")
	runGit(t, pairDir, "config", "user.email", "mate@example.invalid")
	runGit(t, pairDir, "config", "user.name", "mate")
	runGit(t, pairDir, "remote", "add", "origin", filepath.Join(root, "remote.git"))
	if seedPair != nil {
		seedPair(t, pairDir)
	}
	return pairDir
}

// TestBuildBaseBranchCommand_UnseededRepository pins the decision for a pair
// whose HQ repository has no `main`: the pair is wired with its current HEAD
// as the local `main` and its tree untouched (the first push creates `main`
// in HQ, which merges a first change into a repository with none), where a
// fetch of the missing ref used to fail every pass forever. A fetch that
// fails for any other reason still fails the command, nothing moved.
func TestBuildBaseBranchCommand_UnseededRepository(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	withCode := func(t *testing.T, dir string) {
		t.Helper()
		writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
		commitAll(t, dir, "the first page")
	}
	tests := []struct {
		name string
		seed func(t *testing.T, dir string)
	}{
		{name: "pair with its own history on main keeps it", seed: withCode},
		{name: "pair on another branch gets main at its HEAD", seed: func(t *testing.T, dir string) {
			t.Helper()
			runGit(t, dir, "checkout", "-q", "-b", "work")
			withCode(t, dir)
		}},
		{name: "pair with only the init marker gets main at the marker", seed: zcpInitMarker},
		{name: "pair with uncommitted work keeps it", seed: func(t *testing.T, dir string) {
			t.Helper()
			zcpInitMarker(t, dir)
			writeLabFile(t, filepath.Join(dir, "wip.js"), "unsaved\n")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pair := unseededLab(t, tt.seed)
			before := snapshotPair(t, pair)

			//nolint:gosec // test-only, the command under test against a t.TempDir repository
			out, err := exec.CommandContext(t.Context(), "sh", "-c", BuildBaseBranchCommand(pair)).CombinedOutput()
			if err != nil {
				t.Fatalf("command failed: %v\noutput:\n%s", err, out)
			}
			if got := runGit(t, pair, "symbolic-ref", "HEAD"); got != "refs/heads/main" {
				t.Errorf("HEAD is %s, want refs/heads/main", got)
			}
			if got := runGit(t, pair, "rev-parse", "main"); got != before.head {
				t.Errorf("main is %s, want the pair's HEAD %s", got, before.head)
			}
			if got := runGit(t, pair, "status", "--porcelain"); got != before.status {
				t.Errorf("the working copy changed:\nbefore:\n%s\nafter:\n%s", before.status, got)
			}
			runShell(t, BuildBaseBranchCommand(pair))
			if got := runGit(t, pair, "rev-parse", "main"); got != before.head {
				t.Errorf("a second pass moved main to %s, want %s", got, before.head)
			}
		})
	}

	t.Run("a fetch failing for another reason fails the command and moves nothing", func(t *testing.T) {
		pair := unseededLab(t, withCode)
		runGit(t, pair, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
		runGit(t, pair, "checkout", "-q", "-b", "work")
		before := snapshotPair(t, pair)

		//nolint:gosec // test-only, the command under test against a t.TempDir repository
		out, err := exec.CommandContext(t.Context(), "sh", "-c", BuildBaseBranchCommand(pair)).CombinedOutput()
		if err == nil {
			t.Fatalf("an unreachable remote must fail the command:\n%s", out)
		}
		if got := runGit(t, pair, "symbolic-ref", "HEAD"); got != "refs/heads/work" {
			t.Errorf("HEAD moved to %s", got)
		}
		if got := gitOr(t, pair, "rev-parse", "--verify", "-q", "refs/heads/main"); got != before.head {
			t.Errorf("main moved: %s, want %s", got, before.head)
		}
	})
}
