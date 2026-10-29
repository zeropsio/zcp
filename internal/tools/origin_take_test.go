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

// takeFromHQ runs what originFromHQ tells the agent to run in appdev's dev
// service, with zcp git standing in for the ssh to it: the checkout is pair.
func takeFromHQ(t *testing.T, pair string) (string, error) {
	t.Helper()
	return takeThroughZcpGit(t, pair, "")
}

// takeThroughZcpGit runs originFromHQ's command for appdev as the agent's
// shell would, with `zcp git appdev <args>` standing in as git run in pair —
// or failing as ssh does, exit 255 and nothing run, for the git subcommand
// failing names ("" for none).
func takeThroughZcpGit(t *testing.T, pair, failing string) (string, error) {
	t.Helper()
	text := originFromHQ("appdev")
	_, command, ok := strings.Cut(text, "`")
	command, _, closed := strings.Cut(command, "`")
	if !ok || !closed || !strings.HasPrefix(command, "zcp git appdev ") {
		t.Fatalf("not a command run in appdev's dev service through zcp git: %s", text)
	}
	stub := `zcp() { [ "$1" = git ] && [ "$2" = appdev ] || return 99; shift 2; [ "$1" = ` + ops.ShellQuote(failing) + ` ] && return 255; git -C ` + ops.ShellQuote(pair) + ` "$@"; }; `
	out, err := exec.CommandContext(context.Background(), "sh", "-c", stub+command).CombinedOutput() //nolint:gosec // test-only: the command under test, run against a t.TempDir checkout
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

// TestOriginTake_SubstitutionsFailClosedThroughZcpGit: the take reads the
// checkout through `zcp git` into a variable before it decides anything, and
// an assignment chained with && stops the chain when zcp git fails — ssh down,
// the service restarting — so no decision is made on an empty answer: a
// checkout holding only the seed is never reset as if it were, and work of its
// own is never moved off without its branch.
func TestOriginTake_SubstitutionsFailClosedThroughZcpGit(t *testing.T) {
	t.Parallel()
	text := originFromHQ("appdev")
	for _, want := range []string{
		"n=$(zcp git appdev rev-list --count HEAD) && t=$(zcp git appdev ls-tree HEAD) && if [ \"$n\" = 1 ] && [ -z \"$t\" ]; then",
		"h=$(zcp git appdev rev-parse --short=12 HEAD) && zcp git appdev branch -f \"own-history-$h\" && zcp git appdev reset --keep FETCH_HEAD",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the take lacks %q: %s", want, text)
		}
	}
	if strings.Contains(text, "$(git ") || strings.Contains(text, "\"$(") {
		t.Errorf("the take decides on a substitution that fails open: %s", text)
	}
	tests := []struct {
		name    string
		own     bool
		failing string
	}{
		{name: "commit count unread, seed only", failing: "rev-list"},
		{name: "tree unread, seed only", failing: "ls-tree"},
		{name: "commit count unread, work of its own", own: true, failing: "rev-list"},
		{name: "its commit unread, work of its own", own: true, failing: "rev-parse"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pair, _, _ := originFixture(t, true, tt.own)
			before := gitOut(t, pair, "rev-parse", "HEAD")

			out, err := takeThroughZcpGit(t, pair, tt.failing)

			if err == nil {
				t.Errorf("the take succeeded though zcp git failed for %s:\n%s", tt.failing, out)
			}
			if got := gitOut(t, pair, "rev-parse", "HEAD"); got != before {
				t.Errorf("HEAD moved to %s, want %s kept: a decision was made on an unread checkout", got, before)
			}
			if branches := gitOut(t, pair, "branch", "--list", "own-history-*"); branches != "" {
				t.Errorf("a branch was made on an unread commit: %s", branches)
			}
		})
	}
}
