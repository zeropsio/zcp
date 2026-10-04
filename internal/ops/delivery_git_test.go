// Tests for: ops/delivery_git.go — a Mate's branch must descend from the
// repository's `main`, and a delivery commits the deployed tree, takes `main`
// in and says how far ahead it is before anything is pushed. Run against a
// temp BARE repository standing in for HQ: HQ makes `main` with one commit of
// the empty tree, zcp git-initialises the pair with a history of its own, and
// a change that shares no history with `main` cannot be squashed onto it.
package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The Mate's local branch in a lab, and the branches of its first two changes
// at HQ.
const (
	labBranch  = "mate/p-mate"
	labChange1 = "mate/p-mate/1"
	labChange2 = "mate/p-mate/2"
)

// hqSeed is what HQ leaves on a repository's `main` when it makes one: one
// parentless commit of the empty tree.
func hqSeed(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "Initial commit")
}

// mateBranchLab builds a bare "HQ" whose `main` carries HQ's seed, plus a
// pair repository wired to it as `origin`. seedPair runs inside the pair and
// decides what local history it has.
func mateBranchLab(t *testing.T, seedPair func(t *testing.T, dir string)) (pairDir string) {
	t.Helper()
	return mateBranchLabOn(t, hqSeed, seedPair)
}

// mateBranchLabOn is mateBranchLab with the base's history chosen too:
// seedBase runs in a clone whose `main` is then pushed as the remote's.
func mateBranchLabOn(t *testing.T, seedBase, seedPair func(t *testing.T, dir string)) (pairDir string) {
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

// deliverInLab delivers the pair's first change, "Build the app", and pushes
// it to that change's branch, the way zcp does once the change is open.
func deliverInLab(t *testing.T, pair string) {
	t.Helper()
	runShell(t, BuildDeliveryCommand(pair, "Build the app", "", ""))
	runShell(t, BuildChangePushCommand(pair, labChange1))
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

// mateBranchOutcome is what a branch step row expects.
type mateBranchOutcome int

const (
	// branchJoined: the pair's tree and working copy are untouched.
	branchJoined mateBranchOutcome = iota
	// branchFromBase: a marker-only pair now holds the base's tree.
	branchFromBase
	// branchRefused: named in the output, and nothing moved.
	branchRefused
	// branchNamed: a history the base moved past is named where it is — its
	// commits and its working copy untouched, for the delivery to take the
	// base in.
	branchNamed
)

type mateBranchCase struct {
	name string
	// base builds the remote's `main`; nil is HQ's seed.
	base func(t *testing.T, dir string)
	seed func(t *testing.T, dir string)
	want mateBranchOutcome
	// refusal is the state a refused row names, with what it names.
	refusal string
	// wantFiles is path → content expected in the working tree after.
	wantFiles map[string]string
}

func mateBranchCases() []mateBranchCase {
	return []mateBranchCase{
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
			name: "a branch an earlier failed pass left HEAD on is joined in place",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "init")
				runGit(t, dir, "checkout", "-q", "-b", labBranch)
			},
			want:      branchJoined,
			wantFiles: map[string]string{"index.js": "the app\n"},
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
			name: "the Mate's branch already exists and HEAD is elsewhere",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "init")
				runGit(t, dir, "branch", labBranch)
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
				runGit(t, dir, "checkout", "-q", "-b", "mate/mate-p-mate", "FETCH_HEAD~1")
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

// TestBuildMateBranchCommand_DescendsFromMain is the table the wiring
// owes. Whatever local history the pair has, it ends on its own branch
// sharing its history with `origin/main` — or it is refused by name and
// nothing moved.
// A pair joined onto HQ's seed keeps its tree and its working copy
// byte for byte: the seed is a placeholder, and the pair's code is not zcp's
// to rewrite (the rebase this replaced lost a hand-resolved merge in
// wasp-hello-world-app — measured 2026-09-24).
func TestBuildMateBranchCommand_DescendsFromMain(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	for _, tt := range mateBranchCases() {
		t.Run(tt.name, func(t *testing.T) {
			base := tt.base
			if base == nil {
				base = hqSeed
			}
			pair := mateBranchLabOn(t, base, tt.seed)
			before := snapshotPair(t, pair)

			//nolint:gosec // test-only, the command under test against a t.TempDir repository
			out, err := exec.CommandContext(t.Context(), "sh", "-c",
				BuildMateBranchCommand(pair, labBranch)).CombinedOutput()

			assertLabFiles(t, pair, tt.wantFiles)
			switch tt.want {
			case branchRefused:
				assertRefusedNothingMoved(t, pair, before, tt.refusal, string(out), err)
			case branchFromBase:
				assertOnMateBranch(t, pair, string(out), err)
				if got, want := runGit(t, pair, "rev-parse", "HEAD"), runGit(t, pair, "rev-parse", "origin/main"); got != want {
					t.Errorf("a marker-only pair must branch AT main: HEAD %s, main %s", got, want)
				}
			case branchNamed:
				if err != nil {
					t.Fatalf("command failed: %v\noutput:\n%s", err, out)
				}
				if got := runGit(t, pair, "rev-parse", "--abbrev-ref", "HEAD"); got != labBranch {
					t.Errorf("HEAD is on %q, want %s", got, labBranch)
				}
				if got := runGit(t, pair, "rev-parse", "HEAD"); got != before.head {
					t.Errorf("the history moved: HEAD %s → %s", before.head, got)
				}
				if got := runGit(t, pair, "status", "--porcelain"); got != before.status {
					t.Errorf("the working copy changed:\nbefore:\n%s\nafter:\n%s", before.status, got)
				}
				runGit(t, pair, "merge-base", "origin/main", "HEAD")
			case branchJoined:
				assertOnMateBranch(t, pair, string(out), err)
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

// TestMateBranchRefusalRemedy_TheNextAttemptWires: a refusal the agent is
// only told to wait out is a pair stuck for good — the push guard refuses
// every push until the wiring completes. Each refusal names one remedy, and
// once it is done the very next attempt puts the pair on its branch.
func TestMateBranchRefusalRemedy_TheNextAttemptWires(t *testing.T) {
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
			remedy:  []string{"git fetch origin 'main' && git merge --allow-unrelated-histories --no-edit FETCH_HEAD", "resolve", "commit"},
			apply: func(t *testing.T, pair string) {
				t.Helper()
				cmd := exec.CommandContext(t.Context(), "sh", "-c",
					"git fetch -q origin 'main' && git merge --allow-unrelated-histories --no-edit FETCH_HEAD")
				cmd.Dir = pair
				_ = cmd.Run() // both sides add app.js: a conflict, settled by hand
				writeLabFile(t, filepath.Join(pair, "app.js"), "both Mates' work\n")
				runGit(t, pair, "add", "app.js")
				runGit(t, pair, "commit", "-q", "--no-edit")
			},
		},
		{
			name: "the Mate's branch exists and HEAD is elsewhere",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "init")
				runGit(t, dir, "branch", labBranch)
			},
			refusal: "ZCP_BRANCH_ELSEWHERE",
			remedy:  []string{"git checkout 'mate/p-mate'"},
			apply: func(t *testing.T, pair string) {
				t.Helper()
				runRemedyShell(t, pair, "git checkout -q 'mate/p-mate'")
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
			pair := mateBranchLabOn(t, base, tc.seed)
			command := BuildMateBranchCommand(pair, labBranch)

			out, err := exec.CommandContext(t.Context(), "sh", "-c", command).CombinedOutput()
			if err == nil {
				t.Fatalf("want a refusal, the command succeeded:\n%s", out)
			}
			refusal := MateBranchRefusal(string(out))
			if refusal != tc.refusal {
				t.Fatalf("MateBranchRefusal = %q, want %q; output:\n%s", refusal, tc.refusal, out)
			}
			remedy := MateBranchRefusalRemedy(refusal, labBranch)
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
			assertOnMateBranch(t, pair, string(out), err)
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
		branch:  gitOr(t, pair, "rev-parse", "-q", "--verify", "refs/heads/"+labBranch+""),
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
	if got := MateBranchRefusal(out); got != refusal {
		t.Errorf("MateBranchRefusal = %q, want %q; output:\n%s", got, refusal, out)
	}
	if after := snapshotPair(t, pair); after != before {
		t.Errorf("a refusal moved something:\nbefore: %+v\nafter:  %+v", before, after)
	}
}

func assertOnMateBranch(t *testing.T, pair, out string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("command failed: %v\noutput:\n%s", err, out)
	}
	if got := runGit(t, pair, "rev-parse", "--abbrev-ref", "HEAD"); got != labBranch {
		t.Errorf("HEAD is on %q, want %s", got, labBranch)
	}
	// The whole point: HQ's squash needs a shared history.
	if err := exec.CommandContext(t.Context(), "git", "-C", pair, "merge-base", "--is-ancestor", "origin/main", "HEAD").Run(); err != nil {
		t.Errorf("the branch does not descend from origin/main: %v\n%s",
			err, runGit(t, pair, "log", "--oneline", "--all"))
	}
}

// TestBuildMateBranchCommand_Idempotent pins the reconcile property: a
// second pass on a branch already based on `main` rewrites no commit — a
// rebase that ran unconditionally would change every sha under an open pull
// request on every pass.
func TestBuildMateBranchCommand_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := mateBranchLab(t, func(t *testing.T, dir string) {
		t.Helper()
		writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
		runGit(t, dir, "add", "-A")
		runGit(t, dir, "commit", "-qm", "the first page")
	})
	cmd := BuildMateBranchCommand(pair, labBranch)
	runShell(t, cmd)
	first := runGit(t, pair, "rev-parse", "HEAD")
	runShell(t, cmd)
	if second := runGit(t, pair, "rev-parse", "HEAD"); second != first {
		t.Errorf("a second pass moved HEAD from %s to %s", first, second)
	}
}

// TestBuildMateBranchCommand_Shape pins the pieces that cannot be seen from
// the outcome: the fetch authenticates through HQ's session credential
// helper (the credential never reaches argv), `main` is fetched BEFORE the
// branch is decided, and the join is plumbing — no porcelain that replays,
// rewrites or runs the pair's hooks.
func TestBuildMateBranchCommand_Shape(t *testing.T) {
	t.Parallel()

	cmd := BuildMateBranchCommand("/var/www", "mate/p-mate")
	for _, want := range []string{
		"cd '/var/www'",
		"credential.helper=",
		"echo username=mate",
		"fetch --no-tags origin 'main'",
		"b='mate/p-mate'",
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

	// A hostile branch name is quoted, not interpolated.
	if hostile := BuildMateBranchCommand("/var/www", "a'; rm -rf /; '"); strings.Contains(hostile, "; rm -rf /; git") {
		t.Errorf("branch name escaped its quoting:\n%s", hostile)
	}
}

// TestBuildDeliveryCommand_CommitsTheDeployedTreeAndSaysHowFarAhead is how a
// wired pair's work reaches its application with nobody saying how
// (2026-09-17, the owner: "no person is ever going to say this" of a prompt
// that had to name a git-push deploy): the tree as deployed is committed with
// the task's words, and the delivery says how far ahead of `main` that leaves
// it — what a change is opened for — before it is pushed to the change's
// branch; a dependency directory nobody ignored stops it before anything is
// staged; a second delivery of the same tree commits nothing new.
func TestBuildDeliveryCommand_CommitsTheDeployedTreeAndSaysHowFarAhead(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := mateBranchLab(t, nil)
	runShell(t, BuildMateBranchCommand(pair, labBranch))

	// Cut from main and nothing written: nothing to open a change for.
	out, err := exec.CommandContext(t.Context(), "sh", "-c", //nolint:gosec // test-only, the command under test against a t.TempDir repository
		BuildDeliveryCommand(pair, "Build a todo app", "", "")).CombinedOutput()
	if err != nil {
		t.Fatalf("a delivery of nothing: %v\n%s", err, out)
	}
	if ahead, found := DeliveryAhead(string(out)); !found || ahead != 0 {
		t.Fatalf("DeliveryAhead = %d, %v; want 0 — nothing differs from main\n%s", ahead, found, out)
	}

	writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
	if err := os.MkdirAll(filepath.Join(pair, "node_modules", "express"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeLabFile(t, filepath.Join(pair, "node_modules", "express", "index.js"), "a dependency\n")

	// No .gitignore: the dependencies would ride along, so nothing is staged.
	out, err = exec.CommandContext(t.Context(), "sh", "-c", //nolint:gosec // test-only, the command under test against a t.TempDir repository
		BuildDeliveryCommand(pair, "Build a todo app", "", "")).CombinedOutput()
	if err == nil {
		t.Fatalf("a tree with an unignored node_modules must not be delivered:\n%s", out)
	}
	if got := DeliveryUnignored(string(out)); got != "node_modules" {
		t.Fatalf("DeliveryUnignored = %q, want node_modules; output:\n%s", got, out)
	}
	if staged := runGit(t, pair, "diff", "--cached", "--name-only"); staged != "" {
		t.Fatalf("nothing may be staged before the refusal, got %q", staged)
	}
	if _, found := DeliveryAhead(string(out)); found {
		t.Errorf("a refused delivery must not say how far ahead it is:\n%s", out)
	}

	writeLabFile(t, filepath.Join(pair, ".gitignore"), "node_modules/\n")
	out, err = exec.CommandContext(t.Context(), "sh", "-c", //nolint:gosec // test-only, the command under test against a t.TempDir repository
		BuildDeliveryCommand(pair, "Build a todo app", "", "")).CombinedOutput()
	if err != nil {
		t.Fatalf("delivery: %v\n%s", err, out)
	}
	if ahead, found := DeliveryAhead(string(out)); !found || ahead != 1 {
		t.Fatalf("DeliveryAhead = %d, %v; want the one commit of the task\n%s", ahead, found, out)
	}
	remote := filepath.Join(filepath.Dir(pair), "remote.git")
	if branches := runGit(t, remote, "branch", "--list", "mate/*"); branches != "" {
		t.Fatalf("a delivery pushes nothing before its change is open, the remote has %q", branches)
	}
	runShell(t, BuildChangePushCommand(pair, labChange1))
	if got := runGit(t, remote, "log", "-1", "--format=%s", labChange1); got != "Build a todo app" {
		t.Errorf("the change's head commit is %q, want the task's words", got)
	}
	files := runGit(t, remote, "ls-tree", "-r", "--name-only", labChange1)
	if !strings.Contains(files, "index.js") || !strings.Contains(files, ".gitignore") || strings.Contains(files, "node_modules") {
		t.Errorf("the change carries %q; want the app and its .gitignore, never node_modules", files)
	}
	if err := exec.CommandContext(t.Context(), "git", "-C", remote, "merge-base", "--is-ancestor", "main", labChange1).Run(); err != nil {
		t.Errorf("the delivered branch must descend from main: %v", err)
	}
	if got := runGit(t, pair, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/"+labChange1 {
		t.Errorf("the local branch's upstream is %q, want the change's branch", got)
	}

	head := runGit(t, pair, "rev-parse", "HEAD")
	out, err = exec.CommandContext(t.Context(), "sh", "-c", //nolint:gosec // test-only, the command under test against a t.TempDir repository
		BuildDeliveryCommand(pair, "Build a todo app", "", "")).CombinedOutput()
	if err != nil {
		t.Fatalf("a second delivery of the same tree: %v\n%s", err, out)
	}
	if again := runGit(t, pair, "rev-parse", "HEAD"); again != head {
		t.Errorf("a clean tree must add no commit: %s → %s", head, again)
	}
	if ahead, _ := DeliveryAhead(string(out)); ahead != 1 {
		t.Errorf("DeliveryAhead = %d after a second delivery, want 1 still", ahead)
	}
}

// A second Mate merging first is the ordinary state of an application, and
// until 2026-09-18 it was fatal: a Mate's branch was cut from `main` when its
// repository was wired and never caught up, so the first merge stranded every
// other open change (the owner, on todo/appdev #3). A delivery takes `main`
// in before anything is pushed, so the change stays mergeable and the Mate's
// own tree carries everybody's work.
func TestBuildDeliveryCommand_TakesMainInBeforeAnythingIsPushed(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	for _, tc := range []struct {
		name string
		// onMain is what another Mate merged first, by file and content.
		onMain map[string]string
		// inTree is what this Mate wrote.
		inTree       map[string]string
		wantConflict string
	}{
		{
			name:   "another Mate's file comes in",
			onMain: map[string]string{"other.js": "somebody else's work\n"},
			inTree: map[string]string{"index.js": "the app\n"},
		},
		{
			name:   "the same file, different lines",
			onMain: map[string]string{"index.js": "the app\nand a footer\n"},
			inTree: map[string]string{"other.js": "mine\n"},
		},
		{
			name:         "the same line, two ways — the Mate is told, and nothing is pushed",
			onMain:       map[string]string{"index.js": "somebody else's line\n"},
			inTree:       map[string]string{"index.js": "my line\n"},
			wantConflict: "index.js",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pair := mateBranchLab(t, nil)
			root := filepath.Dir(pair)
			remote := filepath.Join(root, "remote.git")
			runShell(t, BuildMateBranchCommand(pair, labBranch))
			writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
			deliverInLab(t, pair)

			// This Mate's work is merged, and then another Mate lands its own
			// on top — the ordinary life of an application.
			other := filepath.Join(root, "other")
			runGit(t, root, "clone", "-q", remote, "other")
			runGit(t, other, "config", "user.email", "other@example.invalid")
			runGit(t, other, "config", "user.name", "other")
			runGit(t, other, "merge", "-q", "--no-edit", "origin/"+labChange1)
			runGit(t, other, "push", "-q", "origin", "main")
			for name, content := range tc.onMain {
				writeLabFile(t, filepath.Join(other, name), content)
			}
			runGit(t, other, "add", "-A")
			runGit(t, other, "commit", "-qm", "Another Mate's work")
			runGit(t, other, "push", "-q", "origin", "main")

			for name, content := range tc.inTree {
				writeLabFile(t, filepath.Join(pair, name), content)
			}
			out, err := exec.CommandContext(t.Context(), "sh", "-c", //nolint:gosec // test-only, the command under test against a t.TempDir repository
				BuildDeliveryCommand(pair, "Add a feature", "", "")).CombinedOutput()

			if tc.wantConflict != "" {
				if err == nil {
					t.Fatalf("a conflict must stop the delivery:\n%s", out)
				}
				if got := DeliveryConflict(string(out)); !strings.Contains(got, tc.wantConflict) {
					t.Fatalf("DeliveryConflict = %q, want %q; output:\n%s", got, tc.wantConflict, out)
				}
				if _, found := DeliveryAhead(string(out)); found {
					t.Errorf("a delivery stopped by a conflict must not say how far ahead it is:\n%s", out)
				}
				if state := runGit(t, pair, "status", "--porcelain=v1", "--untracked-files=no"); strings.Contains(state, "UU") {
					t.Errorf("the checkout must be left whole, not half-merged: %q", state)
				}
				return
			}

			if err != nil {
				t.Fatalf("delivery: %v\n%s", err, out)
			}
			if DeliveryConflict(string(out)) != "" {
				t.Fatalf("no conflict was expected:\n%s", out)
			}
			runShell(t, BuildChangePushCommand(pair, labChange2))
			// Mergeable again: the change now contains main.
			if err := exec.CommandContext(t.Context(), "git", "-C", remote,
				"merge-base", "--is-ancestor", "main", labChange2).Run(); err != nil {
				t.Errorf("the delivered change must contain main: %v", err)
			}
			// And the Mate's own checkout carries the other Mate's work, so
			// its next task is written against what is really on main.
			for name, content := range tc.onMain {
				got, readErr := os.ReadFile(filepath.Join(pair, name))
				if readErr != nil || string(got) != content {
					t.Errorf("%s in the Mate's tree = %q (%v), want %q", name, got, readErr, content)
				}
			}
		})
	}
}

// TestChangePushRefusal reads HQ's per-ref refusal out of git's report, and
// TestGitRemoteUnavailable a remote that could not serve the push now.
func TestChangePushRefusal(t *testing.T) {
	t.Parallel()
	for output, want := range map[string]string{
		"To https://hq.example/git/a1/appdev.git\n ! [remote rejected] HEAD -> mate/p-mate/3 (change_closed)\nerror: failed to push some refs": "change_closed",
		" ! [remote rejected] HEAD -> mate/p-mate/3 (unknown_change)":                                                                          "unknown_change",
		" ! [rejected]        HEAD -> mate/p-mate/3 (non-fast-forward)":                                                                        "non-fast-forward",
		"To https://hq.example/git/a1/appdev.git\n * [new branch]      HEAD -> mate/p-mate/3":                                                  "",
		"Everything up-to-date": "",
	} {
		if got := ChangePushRefusal(output); got != want {
			t.Errorf("ChangePushRefusal(%q) = %q, want %q", output, got, want)
		}
	}
}

func TestGitRemoteUnavailable(t *testing.T) {
	t.Parallel()
	for output, want := range map[string]bool{
		"fatal: unable to access 'https://hq.example/git/a1/appdev.git/': The requested URL returned error: 503":    true,
		"fatal: unable to access 'https://hq.example/git/a1/appdev.git/': Failed to connect to hq.example port 443": true,
		"fatal: unable to access 'https://hq.example/git/a1/appdev.git/': Could not resolve host: hq.example":       true,
		"fatal: unable to access 'https://hq.example/git/a1/appdev.git/': The requested URL returned error: 403":    false,
		"fatal: Authentication failed for 'https://hq.example/git/a1/appdev.git/'":                                  false,
		" ! [remote rejected] HEAD -> mate/p-mate/3 (change_closed)":                                                false,
	} {
		if got := GitRemoteUnavailable(output); got != want {
			t.Errorf("GitRemoteUnavailable(%q) = %v, want %v", output, got, want)
		}
	}
}

// TestBuildTakeChangeInCommand: HQ's branch of an open change may hold a
// commit the Mate's checkout lacks — a merge Core made there, a checkout that
// was lost and cloned again. HQ takes a change's branch only forward, so the
// Mate's push would be refused for good; the change's branch is taken in
// first, as `main` is, and a collision leaves the checkout exactly as it was.
func TestBuildTakeChangeInCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	for _, tc := range []struct {
		name string
		// atHQ is what lands on the change's branch at HQ after the Mate's
		// push, by file and content; nil leaves the branch as the Mate pushed it.
		atHQ map[string]string
		// branch is the change whose branch is taken in.
		branch string
		// inTree is what the Mate wrote since its push.
		inTree       map[string]string
		wantMerged   bool
		wantConflict string
	}{
		{
			name:       "a commit HQ added is taken in",
			atHQ:       map[string]string{"core.txt": "what Core wrote\n"},
			branch:     labChange1,
			inTree:     map[string]string{"todo.js": "todos\n"},
			wantMerged: true,
		},
		{
			name:   "a branch the checkout already holds is left as it is",
			branch: labChange1,
			inTree: map[string]string{"todo.js": "todos\n"},
		},
		{
			name:   "a change HQ has no branch of yet is nothing to take in",
			branch: labChange2,
			inTree: map[string]string{"todo.js": "todos\n"},
		},
		{
			name:         "the same line, two ways — the Mate is told, and nothing moves",
			atHQ:         map[string]string{"index.js": "what Core wrote\n"},
			branch:       labChange1,
			inTree:       map[string]string{"index.js": "the Mate's line\n"},
			wantConflict: "index.js",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pair := mateBranchLab(t, nil)
			root := filepath.Dir(pair)
			remote := filepath.Join(root, "remote.git")
			runGit(t, remote, "config", "receive.denyNonFastForwards", "true")
			runShell(t, BuildMateBranchCommand(pair, labBranch))
			writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
			deliverInLab(t, pair)

			if tc.atHQ != nil {
				hq := filepath.Join(root, "hq")
				runGit(t, root, "clone", "-q", "-b", labChange1, remote, "hq")
				runGit(t, hq, "config", "user.email", "hq@hq.invalid")
				runGit(t, hq, "config", "user.name", "HQ")
				for name, content := range tc.atHQ {
					writeLabFile(t, filepath.Join(hq, name), content)
				}
				commitAll(t, hq, "Core's own write")
				runGit(t, hq, "push", "-q", "origin", labChange1)
			}
			for name, content := range tc.inTree {
				writeLabFile(t, filepath.Join(pair, name), content)
			}
			commitAll(t, pair, "The Mate's next work")
			before := runGit(t, pair, "rev-parse", "HEAD")

			out, err := exec.CommandContext(t.Context(), "sh", "-c", //nolint:gosec // test-only, the command under test against a t.TempDir repository
				BuildTakeChangeInCommand(pair, tc.branch)).CombinedOutput()

			if tc.wantConflict != "" {
				if err == nil {
					t.Fatalf("a conflict must stop it:\n%s", out)
				}
				if got := DeliveryConflict(string(out)); !strings.Contains(got, tc.wantConflict) {
					t.Fatalf("DeliveryConflict = %q, want %q; output:\n%s", got, tc.wantConflict, out)
				}
				if got := runGit(t, pair, "rev-parse", "HEAD"); got != before {
					t.Errorf("HEAD moved: %s → %s", before, got)
				}
				if state := runGit(t, pair, "status", "--porcelain=v1", "--untracked-files=no"); state != "" {
					t.Errorf("the checkout must be left whole: %q", state)
				}
				return
			}
			if err != nil {
				t.Fatalf("take in: %v\n%s", err, out)
			}
			after := runGit(t, pair, "rev-parse", "HEAD")
			if moved := after != before; moved != tc.wantMerged {
				t.Fatalf("HEAD moved = %v, want %v:\n%s", moved, tc.wantMerged, out)
			}
			// Forward from HQ's branch: the push HQ refused before goes through.
			runShell(t, BuildChangePushCommand(pair, tc.branch))
			for name, content := range tc.atHQ {
				got, readErr := os.ReadFile(filepath.Join(pair, name))
				if readErr != nil || string(got) != content {
					t.Errorf("%s in the Mate's tree = %q (%v), want %q", name, got, readErr, content)
				}
			}
		})
	}
}
