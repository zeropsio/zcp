// Tests for: originFromHQ (origin.go) — the command that takes a connected
// origin's code from HQ into the pair's working copy, run against git.
package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
)

// gitOut runs git in dir and returns its trimmed output; t fails on an error.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	//nolint:gosec // test-only, inputs are t.TempDir paths / literal args
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// takeFromHQ runs, in pair, what originFromHQ tells the agent to run there.
func takeFromHQ(t *testing.T, pair string) (string, error) {
	t.Helper()
	text := originFromHQ(pair)
	_, command, ok := strings.Cut(text, "`")
	command, _, closed := strings.Cut(command, "`")
	if !ok || !closed || !strings.HasPrefix(command, "cd "+pair+" && ") {
		t.Fatalf("not a command run in the checkout: %s", text)
	}
	out, err := exec.CommandContext(context.Background(), "sh", "-c", command).CombinedOutput()
	return string(out), err
}

// HQ's main and a pair wired to its seed: an HQ repository whose main is
// first HQ's seed and then, after it took the origin, either the origin's own
// tip (untouched) or a merge of the seed and that tip; and the pair's working
// copy on `main` as the wiring cut it from the seed, holding work of its own
// when own is set.
func originFixture(t *testing.T, replaced, own bool) (pair, hqMain, originTip string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	gitOut(t, root, "init", "-q", origin)
	if err := os.WriteFile(filepath.Join(origin, "app.txt"), []byte("the person's app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitOut(t, origin, "add", "app.txt")
	gitOut(t, origin, "commit", "-q", "-m", "The person's commit")
	originTip = gitOut(t, origin, "rev-parse", "HEAD")

	hqRepo := filepath.Join(root, "hq.git")
	gitOut(t, root, "init", "-q", "--bare", hqRepo)
	empty := gitOut(t, hqRepo, "hash-object", "-t", "tree", "/dev/null")
	seed := gitOut(t, hqRepo, "commit-tree", empty, "-m", "Initial commit")
	gitOut(t, hqRepo, "fetch", "-q", origin, "HEAD")
	hqMain = originTip
	if !replaced {
		tree := gitOut(t, hqRepo, "rev-parse", originTip+"^{tree}")
		hqMain = gitOut(t, hqRepo, "commit-tree", tree, "-p", seed, "-p", originTip, "-m", "Adopt remote history")
	}
	gitOut(t, hqRepo, "update-ref", "refs/heads/main", hqMain)

	pair = filepath.Join(root, "pair")
	gitOut(t, root, "init", "-q", pair)
	gitOut(t, pair, "remote", "add", "origin", hqRepo)
	gitOut(t, pair, "fetch", "-q", "origin", seed)
	gitOut(t, pair, "checkout", "-q", "-B", "main", seed)
	// The wiring fills the checkout's own identity when it has none
	// (ops.BuildBaseBranchCommand), so the take's merge commits as it does,
	// never as whoever the host's git config names.
	gitOut(t, pair, "config", "user.email", ops.DeployGitIdentity.Email)
	gitOut(t, pair, "config", "user.name", ops.DeployGitIdentity.Name)
	if own {
		if err := os.WriteFile(filepath.Join(pair, "notes.md"), []byte("the Mate's notes\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitOut(t, pair, "add", "notes.md")
		gitOut(t, pair, "commit", "-q", "-m", "The Mate's work")
	}
	return pair, hqMain, originTip
}

// Taking a repository's code from HQ moves a checkout holding only HQ's seed
// onto HQ's main, so it carries the origin's own commits and no commit of
// HQ's; a checkout with work of its own merges main in when they share
// history, and keeps work sharing none on a branch of its own while it moves
// onto main — never two unrelated histories joined.
func TestOriginFromHQ_CheckoutHoldingOnlyTheSeedMovesOntoHQMain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		replaced bool
	}{
		{name: "HQ's main is the origin's own history", replaced: true},
		{name: "HQ's main merged the origin into its seed", replaced: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pair, hqMain, _ := originFixture(t, tt.replaced, false)
			if out, err := takeFromHQ(t, pair); err != nil {
				t.Errorf("taking the code from HQ failed: %v\n%s", err, out)
			}
			if got := gitOut(t, pair, "rev-parse", "HEAD"); got != hqMain {
				t.Errorf("HEAD = %s, want HQ's main %s", got, hqMain)
			}
			if got := gitOut(t, pair, "branch", "--show-current"); got != "main" {
				t.Errorf("branch = %q, want the checkout's own kept", got)
			}
		})
	}
	t.Run("work of its own sharing HQ's history merges the origin's in", func(t *testing.T) {
		t.Parallel()
		pair, _, originTip := originFixture(t, false, true)
		if out, err := takeFromHQ(t, pair); err != nil {
			t.Errorf("taking the code from HQ failed: %v\n%s", err, out)
		}
		if got := gitOut(t, pair, "ls-files"); got != "app.txt\nnotes.md" {
			t.Errorf("files = %q, want the origin's beside the Mate's", got)
		}
		gitOut(t, pair, "merge-base", "--is-ancestor", originTip, "HEAD")
	})
	t.Run("work of its own sharing no history with HQ's main stays on its own branch", func(t *testing.T) {
		t.Parallel()
		pair, hqMain, _ := originFixture(t, true, true)
		own := gitOut(t, pair, "rev-parse", "HEAD")
		for range 2 { // a take run again changes nothing
			if out, err := takeFromHQ(t, pair); err != nil {
				t.Fatalf("taking the code from HQ failed: %v\n%s", err, out)
			}
		}
		if got := gitOut(t, pair, "rev-parse", "HEAD"); got != hqMain {
			t.Errorf("HEAD = %s, want HQ's main %s", got, hqMain)
		}
		if got := gitOut(t, pair, "rev-parse", "own-history-"+own[:12]); got != own {
			t.Errorf("own-history-%s = %s, want the Mate's work %s kept", own[:12], got, own)
		}
		if err := exec.CommandContext(context.Background(), "git", "-C", pair, "merge-base", "HEAD", own).Run(); err == nil {
			t.Errorf("HEAD shares history with the Mate's work: two unrelated histories were joined")
		}
	})
}
