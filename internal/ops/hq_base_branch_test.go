// Tests for: ops/hq_base_branch.go — a pair's checkout must descend from the
// repository's `main`. Run against a temp BARE repository standing in for HQ:
// HQ makes `main` with one commit of the empty tree, zcp git-initialises the
// pair with a history of its own, and a branch that shares no history with
// `main` cannot be merged onto it.
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
	// branchJoined: the pair's tree and working copy are untouched.
	branchJoined baseOutcome = iota
	// branchFromBase: a marker-only pair now holds the base's tree.
	branchFromBase
	// branchRefused: named in the output, and nothing moved.
	branchRefused
	// branchNamed: a history the base moved past is named where it is — its
	// commits and its working copy untouched, for the agent to take the
	// base in.
	branchNamed
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
			name: "local commits are joined onto the seed, the pair's files kept",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "README.md"), "the pair's README\n")
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "the first page")
			},
			want: branchJoined,
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
			want: branchJoined,
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
			want:      branchJoined,
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
			name: "a staged change stays staged through the seed join",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "init")
				writeLabFile(t, filepath.Join(dir, "index.js"), "staged\n")
				runGit(t, dir, "add", "index.js")
			},
			want:      branchJoined,
			wantFiles: map[string]string{"index.js": "staged\n"},
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
// sharing its history with `origin/main` — or it is refused by name and
// nothing moved.
// A pair joined onto HQ's seed keeps its tree and its working copy
// byte for byte: the seed is a placeholder, and the pair's code is not zcp's
// to rewrite (the rebase this replaced lost a hand-resolved merge in
// wasp-hello-world-app — measured 2026-09-24).
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
			refusal: "ZCP_BASE_NOT_SEED",
			remedy:  []string{KeepOwnHistory + " && git fetch origin 'main' && git reset --keep FETCH_HEAD", "never joined", "git cherry-pick", "ask the person first"},
			apply: func(t *testing.T, pair string) {
				t.Helper()
				own := gitOr(t, pair, "rev-parse", "HEAD")
				// Run twice: a remedy stopped half-way is run again as it is.
				for range 2 {
					runRemedyShell(t, pair, KeepOwnHistory+" && git fetch -q origin 'main' && git reset -q --keep FETCH_HEAD")
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
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "init")
				runGit(t, dir, "checkout", "-q", "-b", "feature")
			},
			refusal: "ZCP_BRANCH_ELSEWHERE",
			remedy:  []string{"git checkout 'main'"},
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
			remedy:  []string{"git restore --staged ."},
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

			out, err := exec.CommandContext(t.Context(), "sh", "-c", command).CombinedOutput()
			if err == nil {
				t.Fatalf("want a refusal, the command succeeded:\n%s", out)
			}
			refusal := BaseBranchRefusal(string(out))
			if refusal != tc.refusal {
				t.Fatalf("BaseBranchRefusal = %q, want %q; output:\n%s", refusal, tc.refusal, out)
			}
			remedy := BaseBranchRefusalRemedy(refusal)
			for _, want := range tc.remedy {
				if !strings.Contains(remedy, want) {
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
// branch is decided, and the join is plumbing — no porcelain that replays,
// rewrites or runs the pair's hooks.
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
		`commit-tree "HEAD^{tree}" -p HEAD -p FETCH_HEAD`,
		"git symbolic-ref HEAD",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command missing %q:\n%s", want, cmd)
		}
	}
	if fetch, join := strings.Index(cmd, "fetch --no-tags"), strings.Index(cmd, "commit-tree \"HEAD"); fetch > join {
		t.Errorf("the base must be fetched before the join reads FETCH_HEAD:\n%s", cmd)
	}
	// Never a rebase or a porcelain merge (they rewrite the pair's history or
	// its working copy), never `-B`, a force or a reset: moving or resetting
	// an existing branch would discard work the Mate has already pushed.
	for _, absent := range []string{"rebase", "git merge ", "checkout -B", "--force", "reset"} {
		if strings.Contains(cmd, absent) {
			t.Errorf("command must not carry %q:\n%s", absent, cmd)
		}
	}
}
