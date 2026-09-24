// Tests for: ops/gitea_branch.go — a Mate's branch must descend from the
// repository's protected `main`. Run against a temp BARE repository standing
// in for Gitea: the broker's `main` is born with an initial commit, zcp
// git-initialises the pair with a history of its own, and a branch that does
// not descend from that `main` makes Gitea refuse `merge` and `squash` ("The
// merge head and base do not share a common history" — measured 2026-09-16),
// leaving `rebase` the only way a pull request can land.
package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// brokerSeed is what the broker's `auto_init` leaves on `main`: one
// parentless commit adding README.md, mode 100644.
func brokerSeed(t *testing.T, dir string) {
	t.Helper()
	writeLabFile(t, filepath.Join(dir, "README.md"), "the broker's README\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", "Initial commit")
}

// giteaBranchLab builds a bare "Gitea" whose `main` carries the broker's
// seeded README, plus a pair repository wired to it as `origin`. seedPair
// runs inside the pair and decides what local history it has.
func giteaBranchLab(t *testing.T, seedPair func(t *testing.T, dir string)) (pairDir string) {
	t.Helper()
	return giteaBranchLabOn(t, brokerSeed, seedPair)
}

// giteaBranchLabOn is giteaBranchLab with the base's history chosen too:
// seedBase runs in a clone whose `main` is then pushed as the remote's.
func giteaBranchLabOn(t *testing.T, seedBase, seedPair func(t *testing.T, dir string)) (pairDir string) {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	runGit(t, root, "init", "--bare", "-q", "-b", "main", "remote.git")

	seed := filepath.Join(root, "seed")
	runGit(t, root, "init", "-q", "-b", "main", "seed")
	runGit(t, seed, "config", "user.email", "broker@example.invalid")
	runGit(t, seed, "config", "user.name", "broker")
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

// commitAll commits the pair's whole tree; mode, when set, is forced on
// README.md in the index — a recipe authored through a mount that stamps +x
// adds its README as 100755 (nodejs-hello-world-app, measured 2026-09-24).
func commitAll(t *testing.T, dir, message, readmeMode string) {
	t.Helper()
	if readmeMode == "+x" {
		if err := os.Chmod(filepath.Join(dir, "README.md"), 0o755); err != nil {
			t.Fatalf("chmod README.md: %v", err)
		}
	}
	runGit(t, dir, "add", "-A")
	if readmeMode != "" {
		runGit(t, dir, "update-index", "--chmod="+readmeMode, "README.md")
	}
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

// giteaBranchOutcome is what a branch step row expects.
type giteaBranchOutcome int

const (
	// branchJoined: the pair's tree and working copy are untouched.
	branchJoined giteaBranchOutcome = iota
	// branchFromBase: a marker-only pair now holds the base's tree.
	branchFromBase
	// branchRefused: named in the output, and nothing moved.
	branchRefused
)

type giteaBranchCase struct {
	name string
	// base builds the remote's `main`; nil is the broker's seed.
	base func(t *testing.T, dir string)
	seed func(t *testing.T, dir string)
	want giteaBranchOutcome
	// refusal is the state a refused row names, with what it names.
	refusal string
	// wantFiles is path → content expected in the working tree after.
	wantFiles map[string]string
}

func giteaBranchCases() []giteaBranchCase {
	return []giteaBranchCase{
		{
			name: "local commits keep the pair's README over the seed's",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "README.md"), "the pair's README\n")
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "the first page", "")
			},
			want: branchJoined,
			wantFiles: map[string]string{
				"README.md": "the pair's README\n",
				"index.js":  "the app\n",
			},
		},
		{
			name: "a README added as 100755 against the seed's 100644",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "README.md"), "# hello world\n")
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "init", "+x")
			},
			want:      branchJoined,
			wantFiles: map[string]string{"README.md": "# hello world\n"},
		},
		{
			name: "the same README bytes, only the mode differs",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "README.md"), "the broker's README\n")
				commitAll(t, dir, "init", "+x")
			},
			want: branchJoined,
		},
		{
			name: "a two-root history with a hand-resolved merge",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "app.txt"), "base\n")
				commitAll(t, dir, "root one", "")
				runGit(t, dir, "checkout", "-q", "-b", "feat")
				writeLabFile(t, filepath.Join(dir, "app.txt"), "feat\n")
				commitAll(t, dir, "feat", "")
				runGit(t, dir, "checkout", "-q", "main")
				writeLabFile(t, filepath.Join(dir, "app.txt"), "main\n")
				commitAll(t, dir, "main", "")
				// The conflict is settled by hand, to neither side.
				cmd := exec.CommandContext(t.Context(), "git", "merge", "-q", "feat")
				cmd.Dir = dir
				_ = cmd.Run()
				writeLabFile(t, filepath.Join(dir, "app.txt"), "resolved\n")
				commitAll(t, dir, "merge feat", "")
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
				commitAll(t, dir, "init", "")
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
			want:      branchJoined,
			wantFiles: map[string]string{"README.md": "written, never committed\n"},
		},
		{
			name:      "the zcp-init marker alone",
			seed:      zcpInitMarker,
			want:      branchFromBase,
			wantFiles: map[string]string{"README.md": "the broker's README\n"},
		},
		{
			name:      "a pair with no local commits",
			seed:      nil,
			want:      branchFromBase,
			wantFiles: map[string]string{"README.md": "the broker's README\n"},
		},
		{
			name: "a branch an earlier failed pass left HEAD on is joined in place",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "init", "")
				runGit(t, dir, "checkout", "-q", "-b", "mate/mate-p1")
			},
			want:      branchJoined,
			wantFiles: map[string]string{"index.js": "the app\n"},
		},
		{
			name: "a staged change stays staged through the seed join",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "init", "")
				writeLabFile(t, filepath.Join(dir, "index.js"), "staged\n")
				runGit(t, dir, "add", "index.js")
			},
			want:      branchJoined,
			wantFiles: map[string]string{"index.js": "staged\n"},
		},
		{
			name: "a marker-only pair with a staged file is refused cleanly",
			base: func(t *testing.T, dir string) {
				t.Helper()
				brokerSeed(t, dir)
				writeLabFile(t, filepath.Join(dir, "app.js"), "the group's app\n")
				commitAll(t, dir, "landed", "")
			},
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
				commitAll(t, dir, "init", "")
				runGit(t, dir, "branch", "mate/mate-p1")
			},
			want:    branchRefused,
			refusal: "ZCP_BRANCH_ELSEWHERE",
		},
		{
			name: "a base with landed code and an unrelated pair history",
			base: func(t *testing.T, dir string) {
				t.Helper()
				brokerSeed(t, dir)
				writeLabFile(t, filepath.Join(dir, "app.js"), "the first Mate's work\n")
				commitAll(t, dir, "landed", "")
			},
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "app.js"), "this pair's work\n")
				commitAll(t, dir, "init", "")
			},
			want:    branchRefused,
			refusal: "ZCP_BASE_NOT_SEED",
		},
		{
			name: "a one-commit base that is more than a README",
			base: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "README.md"), "readme\n")
				writeLabFile(t, filepath.Join(dir, "app.js"), "code\n")
				commitAll(t, dir, "everything at once", "")
			},
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				commitAll(t, dir, "init", "")
			},
			want:    branchRefused,
			refusal: "ZCP_BASE_NOT_SEED",
		},
		{
			name: "a marker-only pair against a populated main branches from main",
			base: func(t *testing.T, dir string) {
				t.Helper()
				brokerSeed(t, dir)
				writeLabFile(t, filepath.Join(dir, "app.js"), "the group's app\n")
				commitAll(t, dir, "landed", "")
			},
			seed: func(t *testing.T, dir string) {
				t.Helper()
				zcpInitMarker(t, dir)
				writeLabFile(t, filepath.Join(dir, "notes.txt"), "untracked, not on main\n")
			},
			want: branchFromBase,
			wantFiles: map[string]string{
				"app.js":    "the group's app\n",
				"README.md": "the broker's README\n",
				"notes.txt": "untracked, not on main\n",
			},
		},
		{
			name: "a marker-only pair whose untracked file main would overwrite",
			base: func(t *testing.T, dir string) {
				t.Helper()
				brokerSeed(t, dir)
				writeLabFile(t, filepath.Join(dir, "app.js"), "the group's app\n")
				commitAll(t, dir, "landed", "")
			},
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

// TestBuildGiteaMateBranchCommand_DescendsFromMain is the table the wiring
// owes. Whatever local history the pair has, it ends on its own branch
// descending from `origin/main` — or it is refused by name and nothing moved.
// A pair joined onto the broker's seed keeps its tree and its working copy
// byte for byte: the seed is a placeholder, and the pair's code is not zcp's
// to rewrite (the rebase this replaced lost a hand-resolved merge in
// wasp-hello-world-app and could not settle a README mode clash in five
// recipes — measured 2026-09-24).
func TestBuildGiteaMateBranchCommand_DescendsFromMain(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	for _, tt := range giteaBranchCases() {
		t.Run(tt.name, func(t *testing.T) {
			base := tt.base
			if base == nil {
				base = brokerSeed
			}
			pair := giteaBranchLabOn(t, base, tt.seed)
			before := snapshotPair(t, pair)

			//nolint:gosec // test-only, the command under test against a t.TempDir repository
			out, err := exec.CommandContext(t.Context(), "sh", "-c",
				BuildGiteaMateBranchCommand(pair, "mate/mate-p1", "main")).CombinedOutput()

			assertLabFiles(t, pair, tt.wantFiles)
			switch tt.want {
			case branchRefused:
				assertRefusedNothingMoved(t, pair, before, tt.refusal, string(out), err)
			case branchFromBase:
				assertOnMateBranch(t, pair, string(out), err)
				if got, want := runGit(t, pair, "rev-parse", "HEAD"), runGit(t, pair, "rev-parse", "origin/main"); got != want {
					t.Errorf("a marker-only pair must branch AT main: HEAD %s, main %s", got, want)
				}
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
// broker's seed and real code on top.
func landedBase(t *testing.T, dir string) {
	t.Helper()
	brokerSeed(t, dir)
	writeLabFile(t, filepath.Join(dir, "app.js"), "the first Mate's work\n")
	commitAll(t, dir, "landed", "")
}

// runRemedyShell runs a remedy's commands in the pair, as the agent would.
func runRemedyShell(t *testing.T, pair, command string) {
	t.Helper()
	runShell(t, "cd "+shellQuote(pair)+" && "+command)
}

// TestGiteaBranchRefusalRemedy_TheNextAttemptWires: a refusal the agent is
// only told to wait out is a pair stuck for good — the push guard refuses
// every push until the wiring completes. Each refusal names one remedy, and
// once it is done the very next attempt puts the pair on its branch.
func TestGiteaBranchRefusalRemedy_TheNextAttemptWires(t *testing.T) {
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
				commitAll(t, dir, "init", "")
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
				commitAll(t, dir, "init", "")
				runGit(t, dir, "branch", "mate/mate-p1")
			},
			refusal: "ZCP_BRANCH_ELSEWHERE",
			remedy:  []string{"git checkout 'mate/mate-p1'"},
			apply: func(t *testing.T, pair string) {
				t.Helper()
				runRemedyShell(t, pair, "git checkout -q 'mate/mate-p1'")
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
				base = brokerSeed
			}
			pair := giteaBranchLabOn(t, base, tc.seed)
			command := BuildGiteaMateBranchCommand(pair, "mate/mate-p1", "main")

			out, err := exec.CommandContext(t.Context(), "sh", "-c", command).CombinedOutput()
			if err == nil {
				t.Fatalf("want a refusal, the command succeeded:\n%s", out)
			}
			refusal := GiteaBranchRefusal(string(out))
			if refusal != tc.refusal {
				t.Fatalf("GiteaBranchRefusal = %q, want %q; output:\n%s", refusal, tc.refusal, out)
			}
			remedy := GiteaBranchRefusalRemedy(refusal, "mate/mate-p1", "main")
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
		branch:  gitOr(t, pair, "rev-parse", "-q", "--verify", "refs/heads/mate/mate-p1"),
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
	if got := GiteaBranchRefusal(out); got != refusal {
		t.Errorf("GiteaBranchRefusal = %q, want %q; output:\n%s", got, refusal, out)
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
	if got := runGit(t, pair, "rev-parse", "--abbrev-ref", "HEAD"); got != "mate/mate-p1" {
		t.Errorf("HEAD is on %q, want mate/mate-p1", got)
	}
	// The whole point: Gitea's merge and squash need a shared history.
	if err := exec.CommandContext(t.Context(), "git", "-C", pair, "merge-base", "--is-ancestor", "origin/main", "HEAD").Run(); err != nil {
		t.Errorf("the branch does not descend from origin/main: %v\n%s",
			err, runGit(t, pair, "log", "--oneline", "--all"))
	}
}

// TestBuildGiteaMateBranchCommand_Idempotent pins the reconcile property: a
// second pass on a branch already based on `main` rewrites no commit — a
// rebase that ran unconditionally would change every sha under an open pull
// request on every pass.
func TestBuildGiteaMateBranchCommand_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := giteaBranchLab(t, func(t *testing.T, dir string) {
		t.Helper()
		writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
		runGit(t, dir, "add", "-A")
		runGit(t, dir, "commit", "-qm", "the first page")
	})
	cmd := BuildGiteaMateBranchCommand(pair, "mate/mate-p1", "main")
	runShell(t, cmd)
	first := runGit(t, pair, "rev-parse", "HEAD")
	runShell(t, cmd)
	if second := runGit(t, pair, "rev-parse", "HEAD"); second != first {
		t.Errorf("a second pass moved HEAD from %s to %s", first, second)
	}
}

// TestBuildGiteaMateBranchCommand_Shape pins the pieces that cannot be seen
// from the outcome: the fetch authenticates through the session credential
// helper (the bot token never reaches argv), the base is fetched BEFORE the
// branch is decided, and the join is plumbing — no porcelain that replays,
// rewrites or runs the pair's hooks.
func TestBuildGiteaMateBranchCommand_Shape(t *testing.T) {
	t.Parallel()

	cmd := BuildGiteaMateBranchCommand("/var/www", "mate/bot", "trunk")
	for _, want := range []string{
		"cd '/var/www'",
		"credential.helper=",
		"fetch --no-tags origin 'trunk'",
		"b='mate/bot'",
		"git merge-base --is-ancestor FETCH_HEAD HEAD",
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
	if hostile := BuildGiteaMateBranchCommand("/var/www", "a'; rm -rf /; '", "main"); strings.Contains(hostile, "; rm -rf /; git") {
		t.Errorf("branch name escaped its quoting:\n%s", hostile)
	}

	// An empty branch or base falls back to the protected default rather than
	// emitting a refspec git cannot parse.
	fallback := BuildGiteaMateBranchCommand("/var/www", "", "")
	if !strings.Contains(fallback, "origin 'main'") || !strings.Contains(fallback, "b='main'") {
		t.Errorf("empty branch/base must fall back to main:\n%s", fallback)
	}
}

// TestBuildGiteaDeliveryCommand_CommitsAndPushesTheDeployedTree is how a wired
// pair's work reaches its group with nobody saying how (2026-09-17, the owner:
// "no person is ever going to say this" of a prompt that had to name a
// git-push deploy): the tree as deployed is committed with the task's words and
// pushed to the Mate's own branch; a dependency directory nobody ignored stops
// it before anything is staged; a second delivery of the same tree sends
// nothing new.
func TestBuildGiteaDeliveryCommand_CommitsAndPushesTheDeployedTree(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := giteaBranchLab(t, nil)
	runShell(t, BuildGiteaMateBranchCommand(pair, "mate/mate-p1", "main"))
	writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
	if err := os.MkdirAll(filepath.Join(pair, "node_modules", "express"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeLabFile(t, filepath.Join(pair, "node_modules", "express", "index.js"), "a dependency\n")

	// No .gitignore: the dependencies would ride along, so nothing is staged.
	//nolint:gosec // test-only, the command under test against a t.TempDir repository
	out, err := exec.CommandContext(t.Context(), "sh", "-c",
		BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Build a todo app", "", "")).CombinedOutput()
	if err == nil {
		t.Fatalf("a tree with an unignored node_modules must not be delivered:\n%s", out)
	}
	if got := GiteaDeliveryUnignored(string(out)); got != "node_modules" {
		t.Fatalf("GiteaDeliveryUnignored = %q, want node_modules; output:\n%s", got, out)
	}
	if staged := runGit(t, pair, "diff", "--cached", "--name-only"); staged != "" {
		t.Fatalf("nothing may be staged before the refusal, got %q", staged)
	}

	writeLabFile(t, filepath.Join(pair, ".gitignore"), "node_modules/\n")
	runShell(t, BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Build a todo app", "", ""))
	remote := filepath.Join(filepath.Dir(pair), "remote.git")
	if got := runGit(t, remote, "log", "-1", "--format=%s", "mate/mate-p1"); got != "Build a todo app" {
		t.Errorf("the branch's head commit is %q, want the task's words", got)
	}
	files := runGit(t, remote, "ls-tree", "-r", "--name-only", "mate/mate-p1")
	if !strings.Contains(files, "index.js") || !strings.Contains(files, ".gitignore") || strings.Contains(files, "node_modules") {
		t.Errorf("the branch carries %q; want the app and its .gitignore, never node_modules", files)
	}
	if err := exec.CommandContext(t.Context(), "git", "-C", remote, "merge-base", "--is-ancestor", "main", "mate/mate-p1").Run(); err != nil {
		t.Errorf("the delivered branch must descend from main: %v", err)
	}

	head := runGit(t, remote, "rev-parse", "mate/mate-p1")
	//nolint:gosec // test-only, the command under test against a t.TempDir repository
	out, err = exec.CommandContext(t.Context(), "sh", "-c",
		BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Build a todo app", "", "")).CombinedOutput()
	if err != nil {
		t.Fatalf("a second delivery of the same tree: %v\n%s", err, out)
	}
	if again := runGit(t, remote, "rev-parse", "mate/mate-p1"); again != head {
		t.Errorf("a clean tree must add no commit: %s → %s", head, again)
	}
	if GiteaDeliveryUpToDate(string(out)) != true {
		t.Errorf("a second delivery must read as up to date:\n%s", out)
	}
}

// A second Mate merging first is the ordinary state of a group, and until
// 2026-09-18 it was fatal: a Mate's branch was cut from `main` when its
// repository was wired and never caught up, so the first merge killed every
// other open pull request — Gitea simply stopped offering Merge, with nothing
// said (the owner, on todo/appdev #3). A delivery now takes the base in before
// it pushes, so the branch stays mergeable and the Mate's own tree carries
// everybody's work.
func TestBuildGiteaDeliveryCommand_TakesTheBaseInBeforeItPushes(t *testing.T) {
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
			pair := giteaBranchLab(t, nil)
			root := filepath.Dir(pair)
			remote := filepath.Join(root, "remote.git")
			runShell(t, BuildGiteaMateBranchCommand(pair, "mate/mate-p1", "main"))
			writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
			runShell(t, BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Build the app", "", ""))

			// This Mate's work is merged, the way a person merges it, and then
			// another Mate lands its own on top — the ordinary life of a group.
			other := filepath.Join(root, "other")
			runGit(t, root, "clone", "-q", remote, "other")
			runGit(t, other, "config", "user.email", "other@example.invalid")
			runGit(t, other, "config", "user.name", "other")
			runGit(t, other, "merge", "-q", "--no-edit", "origin/mate/mate-p1")
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
			//nolint:gosec // test-only, the command under test against a t.TempDir repository
			out, err := exec.CommandContext(t.Context(), "sh", "-c",
				BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "main", "Add a feature", "", "")).CombinedOutput()

			if tc.wantConflict != "" {
				if err == nil {
					t.Fatalf("a conflict must stop the delivery:\n%s", out)
				}
				if got := GiteaDeliveryConflict(string(out)); !strings.Contains(got, tc.wantConflict) {
					t.Fatalf("GiteaDeliveryConflict = %q, want %q; output:\n%s", got, tc.wantConflict, out)
				}
				if state := runGit(t, pair, "status", "--porcelain=v1", "--untracked-files=no"); strings.Contains(state, "UU") {
					t.Errorf("the checkout must be left whole, not half-merged: %q", state)
				}
				return
			}

			if err != nil {
				t.Fatalf("delivery: %v\n%s", err, out)
			}
			if GiteaDeliveryConflict(string(out)) != "" {
				t.Fatalf("no conflict was expected:\n%s", out)
			}
			// Mergeable again: the branch now contains main.
			if err := exec.CommandContext(t.Context(), "git", "-C", remote,
				"merge-base", "--is-ancestor", "main", "mate/mate-p1").Run(); err != nil {
				t.Errorf("the delivered branch must contain main: %v", err)
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
