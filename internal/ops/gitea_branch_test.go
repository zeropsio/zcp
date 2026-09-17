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

// giteaBranchLab builds a bare "Gitea" whose `main` carries the broker's
// seeded README, plus a pair repository wired to it as `origin`. seedPair
// runs inside the pair and decides what local history it has.
func giteaBranchLab(t *testing.T, seedPair func(t *testing.T, dir string)) (pairDir string) {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	runGit(t, root, "init", "--bare", "-q", "-b", "main", "remote.git")

	seed := filepath.Join(root, "seed")
	runGit(t, root, "init", "-q", "-b", "main", "seed")
	runGit(t, seed, "config", "user.email", "broker@example.invalid")
	runGit(t, seed, "config", "user.name", "broker")
	writeLabFile(t, filepath.Join(seed, "README.md"), "the broker's README\n")
	runGit(t, seed, "add", "-A")
	runGit(t, seed, "commit", "-qm", "Initial commit")
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

func writeLabFile(t *testing.T, path, body string) {
	t.Helper()
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

// TestBuildGiteaMateBranchCommand_DescendsFromMain is the table the fix owes:
// whatever local history the pair has, the branch it ends on descends from
// `origin/main`, and the pair's own file wins where the broker's seed
// collides with it.
func TestBuildGiteaMateBranchCommand_DescendsFromMain(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	tests := []struct {
		name string
		seed func(t *testing.T, dir string)
		// wantFiles is path → content expected in the working tree after.
		wantFiles map[string]string
	}{
		{
			name: "local commits are replayed on top of main",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "README.md"), "the pair's README\n")
				writeLabFile(t, filepath.Join(dir, "index.js"), "the app\n")
				runGit(t, dir, "add", "-A")
				runGit(t, dir, "commit", "-qm", "the first page")
			},
			// The pair's tree wins the README the broker seeded, and nothing
			// the pair wrote is lost.
			wantFiles: map[string]string{
				"README.md": "the pair's README\n",
				"index.js":  "the app\n",
			},
		},
		{
			name: "the zcp-init empty commit replays too",
			seed: func(t *testing.T, dir string) {
				t.Helper()
				tree := runGit(t, dir, "mktree")
				sha := runGit(t, dir, "commit-tree", tree, "-m", "zcp init")
				runGit(t, dir, "update-ref", "HEAD", sha)
			},
			wantFiles: map[string]string{"README.md": "the broker's README\n"},
		},
		{
			name:      "a pair with no local commits branches from main",
			seed:      nil,
			wantFiles: map[string]string{"README.md": "the broker's README\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pair := giteaBranchLab(t, tt.seed)
			runShell(t, BuildGiteaMateBranchCommand(pair, "mate/mate-p1", "main"))

			if got := runGit(t, pair, "rev-parse", "--abbrev-ref", "HEAD"); got != "mate/mate-p1" {
				t.Errorf("HEAD is on %q, want mate/mate-p1", got)
			}
			// The whole point: Gitea's merge and squash need a shared history.
			if err := exec.CommandContext(t.Context(), "git", "-C", pair, "merge-base", "--is-ancestor", "origin/main", "HEAD").Run(); err != nil {
				t.Errorf("the branch does not descend from origin/main: %v\n%s",
					err, runGit(t, pair, "log", "--oneline", "--all"))
			}
			for path, want := range tt.wantFiles {
				body, err := os.ReadFile(filepath.Join(pair, path))
				if err != nil {
					t.Errorf("read %s: %v", path, err)
					continue
				}
				if string(body) != want {
					t.Errorf("%s = %q, want %q", path, body, want)
				}
			}
		})
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
// branch is decided, and a rebase that cannot resolve leaves no half-rebased
// repository behind.
func TestBuildGiteaMateBranchCommand_Shape(t *testing.T) {
	t.Parallel()

	cmd := BuildGiteaMateBranchCommand("/var/www", "mate/bot", "trunk")
	for _, want := range []string{
		"cd '/var/www'",
		"credential.helper=",
		"fetch --no-tags origin 'trunk'",
		"git checkout -b 'mate/bot'",
		"git merge-base --is-ancestor FETCH_HEAD HEAD",
		"git rebase -X theirs FETCH_HEAD",
		"git rebase --abort",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command missing %q:\n%s", want, cmd)
		}
	}
	if fetch, rebase := strings.Index(cmd, "fetch --no-tags"), strings.Index(cmd, "rebase -X theirs"); fetch > rebase {
		t.Errorf("the base must be fetched before the rebase reads FETCH_HEAD:\n%s", cmd)
	}
	// Never `-B`, never a force, never a reset: moving or resetting an
	// existing branch would discard work the Mate has already pushed.
	for _, absent := range []string{"checkout -B", "--force", "reset --hard"} {
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
	if !strings.Contains(fallback, "origin 'main'") || !strings.Contains(fallback, "-b 'main'") {
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
	out, err := exec.CommandContext(t.Context(), "sh", "-c",
		BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "Build a todo app")).CombinedOutput()
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
	runShell(t, BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "Build a todo app"))
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
	out, err = exec.CommandContext(t.Context(), "sh", "-c",
		BuildGiteaDeliveryCommand(pair, "mate/mate-p1", "Build a todo app")).CombinedOutput()
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
