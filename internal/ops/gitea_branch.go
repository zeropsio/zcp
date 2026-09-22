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

// giteaDeliveryUnignoredMarker prefixes the dependency directories a delivery
// refused to commit, so the caller can name them.
const giteaDeliveryUnignoredMarker = "ZCP_UNIGNORED:"

// giteaDeliveryConflictMarker prefixes the files the base and the Mate's work
// both changed, when taking the base in could not be done without a decision.
const giteaDeliveryConflictMarker = "ZCP_MERGE_CONFLICT:"

// BuildGiteaDeliveryCommand delivers a wired pair's working tree as it was
// deployed: it commits everything with the task's words and pushes the commit
// to the Mate's own branch — never to `main`, which takes no direct push.
//
// A dependency directory the repository does not ignore stops it before
// anything is staged: committing node_modules is never what a person meant,
// and the .gitignore stays the agent's to write (InitServiceGit). A clean
// tree commits nothing and the push reports the branch up to date.
//
// landedCommit and landedHead are the pull request landing this pair's own
// earlier delivery is catching up to — workflow.GiteaRepoRef.Landed, "" when
// none is recorded. Absorbed (BuildAbsorbLandedPullRequestCommand) BEFORE the
// ordinary take-the-base-in merge below, so a squash of this Mate's own
// history does not read as two unrelated histories that both add the same
// files (MB-26).
func BuildGiteaDeliveryCommand(workingDir, branch, base, message, landedCommit, landedHead string) string {
	if branch == "" {
		branch = defaultBranch
	}
	if base == "" {
		base = defaultBranch
	}
	steps := []string{ //nolint:prealloc // clearer as a literal; two known appends follow, not a growth loop
		"cd " + shellQuote(workingDir),
		gitIdentityEnsureFragment(),
		`{ unignored=""; for d in node_modules vendor .venv; do if [ -d "$d" ] && ! git check-ignore -q "$d"; then unignored="$unignored $d"; fi; done; ` +
			`if [ -n "$unignored" ]; then echo "` + giteaDeliveryUnignoredMarker + `$unignored"; exit 3; fi; }`,
		"git add -A",
		fmt.Sprintf("(git diff --cached --quiet || git commit -q -m %s)", shellQuote(message)),
	}
	steps = append(steps, giteaFetchAbsorbAndTakeBaseInSteps(base, landedCommit, landedHead)...)
	steps = append(steps, fmt.Sprintf("GIT_TERMINAL_PROMPT=0 git %s push -u origin %s 2>&1",
		gitCredentialHelperArgs(), shellQuote("HEAD:refs/heads/"+branch)))
	return strings.Join(steps, " && ")
}

// BuildGiteaAbsorbAndSyncCommand catches a pair's own checkout up with a
// landing WITHOUT delivering anything of the agent's — no add, no commit, no
// push. It runs the exact fetch/absorb/take-base-in sequence
// BuildGiteaDeliveryCommand runs before its push, so a reconcile pass that
// just learned a pull request merged can fold that landing into the Mate's
// history right there (MB-26: the Mate's next task starts on current code,
// not on whatever its checkout happened to be at). A conflict aborts and
// leaves the checkout exactly as it was, same marker as a delivery's — the
// caller treats this as best-effort and never surfaces the marker itself;
// the next real delivery is the authoritative report.
func BuildGiteaAbsorbAndSyncCommand(workingDir, base, landedCommit, landedHead string) string {
	steps := append([]string{"cd " + shellQuote(workingDir)},
		giteaFetchAbsorbAndTakeBaseInSteps(base, landedCommit, landedHead)...)
	return strings.Join(steps, " && ")
}

// giteaFetchAbsorbAndTakeBaseInSteps is the shared tail BuildGiteaDeliveryCommand
// and BuildGiteaAbsorbAndSyncCommand both run once the working directory is
// current: fetch the remote, absorb a known-lossless landing as a real merge,
// then take in whatever else the base carries that this branch doesn't.
//
// A group has more than one Mate and they land in turn, so a branch cut when
// the repository was wired — or last delivered — is behind the moment
// somebody else merges, and Gitea then simply stops offering Merge (the
// owner, 2026-09-18, on a second Mate's request). A merge and not a rebase:
// history only moves forward, so a push built on this stays an ordinary one
// and no force can lose a commit.
func giteaFetchAbsorbAndTakeBaseInSteps(base, landedCommit, landedHead string) []string {
	remoteBase := shellQuote("origin/" + base)
	return []string{
		fmt.Sprintf("GIT_TERMINAL_PROMPT=0 git %s fetch --no-tags -q origin", gitCredentialHelperArgs()),
		BuildAbsorbLandedPullRequestCommand(landedCommit, landedHead),
		fmt.Sprintf("(git rev-parse -q --verify %s >/dev/null 2>&1 || true)", remoteBase),
		// Already contains the base → nothing to do. Otherwise merge it, and a
		// collision only a person or the agent can settle leaves the checkout
		// exactly as it was, named in the output.
		fmt.Sprintf("(git merge-base --is-ancestor %s HEAD 2>/dev/null"+
			" || ! git rev-parse -q --verify %s >/dev/null 2>&1"+
			" || git merge --no-edit -q %s"+
			" || (conflicts=$(git diff --name-only --diff-filter=U | tr '\n' ' ');"+
			" git merge --abort >/dev/null 2>&1;"+
			` echo "%s$conflicts"; exit 4))`,
			remoteBase, remoteBase, remoteBase, giteaDeliveryConflictMarker),
	}
}

// GiteaDeliveryUnignored reads the dependency directories a delivery refused
// to commit out of its output, space-separated; empty when it refused none.
func GiteaDeliveryUnignored(output string) string {
	for line := range strings.SplitSeq(output, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), giteaDeliveryUnignoredMarker); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// GiteaDeliveryConflict reads the files the base and the Mate's work both
// changed out of a delivery's output, space-separated; empty when the base
// came in cleanly or there was none to take in.
func GiteaDeliveryConflict(output string) string {
	for line := range strings.SplitSeq(output, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), giteaDeliveryConflictMarker); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// GiteaDeliveryUpToDate reports whether a delivery's push found the branch
// already carrying the commit.
func GiteaDeliveryUpToDate(output string) bool {
	return strings.Contains(output, "Everything up-to-date")
}
