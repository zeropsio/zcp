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
	command := originFromHQ("appdev")
	start := strings.Index(command, "cd <repo dir> && ")
	if start < 0 || !strings.HasSuffix(command, "`") {
		t.Fatalf("not a command run in the repo dir: %s", command)
	}
	inner := strings.ReplaceAll(command[start:len(command)-2], "<repo dir>", pair)
	out, err := exec.CommandContext(context.Background(), "sh", "-c", inner).CombinedOutput()
	return string(out), err
}

// HQ's main and a pair wired to its seed: an HQ repository whose main is
// first HQ's seed and then, after adoption, either the origin's own tip
// (untouched) or a merge of the seed and that tip; and the pair's working
// copy on the Mate's branch cut from the seed, holding work of its own when
// own is set.
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
	gitOut(t, pair, "checkout", "-q", "-b", "mate/ada/1", seed)
	if own {
		if err := os.WriteFile(filepath.Join(pair, "notes.md"), []byte("the Mate's notes\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitOut(t, pair, "add", "notes.md")
		gitOut(t, pair, "commit", "-q", "-m", "The Mate's work")
	}
	return pair, hqMain, originTip
}

// Connecting an origin takes its history into a pair whose branch holds only
// HQ's seed: the branch moves onto HQ's main, so it carries the origin's own
// commits and no commit of HQ's; a branch with work of its own merges main in.
func TestOriginFromHQ_BranchHoldingOnlyTheSeedMovesOntoHQMain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		replaced bool
		own      bool
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
			if got := gitOut(t, pair, "branch", "--show-current"); got != "mate/ada/1" {
				t.Errorf("branch = %q, want the Mate's branch kept", got)
			}
		})
	}
	t.Run("a branch with work of its own merges the origin's history in", func(t *testing.T) {
		t.Parallel()
		pair, _, originTip := originFixture(t, true, true)
		if out, err := takeFromHQ(t, pair); err != nil {
			t.Errorf("taking the code from HQ failed: %v\n%s", err, out)
		}
		if got := gitOut(t, pair, "ls-files"); got != "app.txt\nnotes.md" {
			t.Errorf("files = %q, want the origin's beside the Mate's", got)
		}
		gitOut(t, pair, "merge-base", "--is-ancestor", originTip, "HEAD")
	})
}
