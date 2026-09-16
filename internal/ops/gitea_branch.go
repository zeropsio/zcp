package ops

import (
	"fmt"
	"strings"
)

// BuildGiteaMateBranchCommand puts the pair's working copy on the Mate's own
// branch, DESCENDING from the repository's protected base.
//
// The broker creates `{slug}/{name}` with an initial commit on a protected
// `main` (gitea-mate docs/vocabulary.md), while zcp git-initialises the pair
// with a history of its own. A branch pushed from that history shares no
// commit with `main`, and Gitea then refuses both `merge` and `squash` on the
// pull request — "The merge head and base do not share a common history" —
// leaving `rebase` the only button that works (measured 2026-09-16 on a live
// Mate). So the base is fetched first and the pair's local commits are
// replayed on top of it.
//
// `-X theirs` resolves a collision in favour of the side being replayed —
// the PAIR's tree. The only file the broker seeds is the README, and a
// Mate's own README is the one worth keeping; nothing else can collide.
//
// The three shapes it has to cover:
//
//   - local commits → branch at HEAD, rebase onto the fetched base;
//   - only the `zcp init` marker commit → the same, and the marker replays
//     as an empty commit on top of the base;
//   - no commits at all (unborn HEAD) → the branch is cut straight from the
//     fetched base.
//
// Idempotent: a branch that already descends from the base is left exactly
// where it is (`merge-base --is-ancestor` short-circuits), so a later
// reconcile pass cannot rewrite the shas under an open pull request. A rebase
// that cannot resolve aborts itself rather than leaving a half-rebased
// repository the next pass would trip over.
//
// Auth rides the session credential helper, like every other remote-reading
// command here: the bot token reaches git over an anonymous pipe, never argv
// and never a file.
func BuildGiteaMateBranchCommand(workingDir, branch, base string) string {
	if branch == "" {
		branch = defaultBranch
	}
	if base == "" {
		base = defaultBranch
	}
	quotedBranch := shellQuote(branch)
	return strings.Join([]string{
		"cd " + shellQuote(workingDir),
		gitIdentityEnsureFragment(),
		fmt.Sprintf("GIT_TERMINAL_PROMPT=0 git %s fetch --no-tags origin %s",
			gitCredentialHelperArgs(), shellQuote(base)),
		// An unborn HEAD has nothing to replay: cut the branch from the base.
		fmt.Sprintf("(git rev-parse -q --verify HEAD >/dev/null || git checkout -q -b %s FETCH_HEAD)", quotedBranch),
		fmt.Sprintf("(git checkout %s 2>/dev/null || git checkout -b %s)", quotedBranch, quotedBranch),
		"(git merge-base --is-ancestor FETCH_HEAD HEAD 2>/dev/null" +
			" || git rebase -X theirs FETCH_HEAD" +
			" || (git rebase --abort >/dev/null 2>&1; false))",
	}, " && ")
}
