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
// Mate). So the base is fetched first and the two histories are joined.
//
// The pair's code is never rewritten to do it. A rebase onto the base could
// not settle a README the seed and a recipe both add with different modes,
// refused a dirty working copy, and linearised a recipe's hand-resolved merges
// into different code (5 and 1 of the recipes, measured 2026-09-24). The
// states, each decided from facts and each with one action:
//
//   - HEAD already descends from the base → only the branch is named;
//   - HEAD is only the `zcp init` marker (a parentless empty tree) → the
//     branch is cut from the base, so whatever the base carries — the seed's
//     README included — stays in the branch and the working copy. Refused by
//     name if a change is staged (the checkout would carry it onto the base);
//     if the checkout would overwrite an untracked file, a seed base is
//     joined as below instead, any other base refused by name;
//   - the base is provably the broker's seed — one parentless commit whose
//     tree is empty or only README.md → a commit with the pair's own tree and
//     both histories as parents (`commit-tree`, plumbing: no index, working
//     copy or hook is touched), so the pair's tree stays byte-identical and
//     only the placeholder is discarded;
//   - anything else — real code on both sides, unrelated → refused by name
//     (ZCP_BASE_NOT_SEED): no mechanical answer keeps both.
//
// The Mate's branch existing while HEAD is elsewhere is refused by name too.
// Every refusal changes nothing; GiteaBranchRefusal reads which one it was and
// GiteaBranchRefusalRemedy what to do about it.
//
// Idempotent: a branch that already descends from the base is left exactly
// where it is, so a later reconcile pass cannot rewrite the shas under an open
// pull request.
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
	refuse := func(state string) string {
		return fmt.Sprintf(`{ echo "%s%s"; exit 5; }`, giteaBranchRefusalMarker, state)
	}
	joinIdentity := fmt.Sprintf("-c user.email=%s -c user.name=%s",
		shellQuote(DeployGitIdentity.Email), shellQuote(DeployGitIdentity.Name))
	join := `c=$(git ` + joinIdentity + ` commit-tree "HEAD^{tree}" -p HEAD -p FETCH_HEAD -m "Join the repository's base") && ` +
		`git update-ref "$ref" "$c" "$old" && git symbolic-ref HEAD "$ref"; `
	return strings.Join([]string{
		"cd " + shellQuote(workingDir),
		gitIdentityEnsureFragment(),
		fmt.Sprintf("GIT_TERMINAL_PROMPT=0 git %s fetch --no-tags origin %s",
			gitCredentialHelperArgs(), shellQuote(base)),
		gitHeadEnsureFragment(),
		"{ b=" + shellQuote(branch) + `; ref="refs/heads/$b"; cur=$(git symbolic-ref -q HEAD || true); ` +
			`old=$(git rev-parse -q --verify "$ref" || true); ` +
			`if [ "$cur" != "$ref" ] && [ -n "$old" ]; then ` + refuse("ZCP_BRANCH_ELSEWHERE") + `; fi; ` +
			`isseed=; if [ "$(git rev-list --count FETCH_HEAD)" = 1 ] && seed=$(git ls-tree --name-only FETCH_HEAD) && { [ -z "$seed" ] || [ "$seed" = README.md ]; }; then isseed=1; fi; ` +
			`if git merge-base --is-ancestor FETCH_HEAD HEAD 2>/dev/null; then ` +
			`[ "$cur" = "$ref" ] || git checkout -q -b "$b"; ` +
			`elif [ "$(git rev-parse "HEAD^{tree}")" = "$(git hash-object -t tree /dev/null)" ] && ! git rev-parse -q --verify "HEAD^" >/dev/null; then ` +
			`if ! git diff --cached --quiet; then ` + refuse("ZCP_STAGED_CHANGES") + `; fi; ` +
			`collide=$(git ls-tree -r --name-only FETCH_HEAD | while IFS= read -r f; do if [ -e "$f" ] || [ -L "$f" ]; then printf '%s ' "$f"; fi; done); ` +
			`if [ -z "$collide" ]; then ` +
			`git checkout -q --detach FETCH_HEAD && git update-ref "$ref" "$(git rev-parse FETCH_HEAD)" "$old" && git symbolic-ref HEAD "$ref"; ` +
			`elif [ -n "$isseed" ]; then ` + join +
			`else echo "` + giteaBranchRefusalMarker + `ZCP_UNTRACKED_COLLISION $collide"; exit 5; fi; ` +
			`elif [ -n "$isseed" ]; then ` + join +
			`else ` + refuse("ZCP_BASE_NOT_SEED") + `; fi; }`,
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

// giteaBranchRefusalMarker prefixes the named state a branch step refused,
// so the caller can say which one without parsing git's own words.
const giteaBranchRefusalMarker = "ZCP_BRANCH_REFUSED:"

// GiteaBranchRefusal reads the state BuildGiteaMateBranchCommand refused out
// of its output, with anything it names ("ZCP_BASE_NOT_SEED",
// "ZCP_UNTRACKED_COLLISION app.js"); empty when it refused none.
func GiteaBranchRefusal(output string) string {
	for line := range strings.SplitSeq(output, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), giteaBranchRefusalMarker); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// GiteaBranchRefusalRemedy is the one thing to do about a refusal
// GiteaBranchRefusal read, run in the pair's working copy; the next attempt
// then finds the state it can wire. Empty for a refusal it does not know.
func GiteaBranchRefusalRemedy(refusal, branch, base string) string {
	if branch == "" {
		branch = defaultBranch
	}
	if base == "" {
		base = defaultBranch
	}
	state, named, _ := strings.Cut(refusal, " ")
	switch state {
	case "ZCP_BASE_NOT_SEED":
		return fmt.Sprintf("%q already holds code and this history is unrelated to it, so take it in: run `git fetch origin %s && git merge --allow-unrelated-histories --no-edit FETCH_HEAD`, resolve any conflicts and commit, then deploy again — the base is then part of the history and the branch is cut from it.",
			base, shellQuote(base))
	case "ZCP_BRANCH_ELSEWHERE":
		return fmt.Sprintf("%q already exists and the working copy is on another branch: run `git checkout %s`, then deploy again.",
			branch, shellQuote(branch))
	case "ZCP_STAGED_CHANGES":
		return fmt.Sprintf("the working copy is about to take %q's files and has changes staged: run `git restore --staged .` (the files stay in the working copy), then deploy again.", base)
	case "ZCP_UNTRACKED_COLLISION":
		return fmt.Sprintf("%q carries files the working copy has untracked (%s): move or delete them, then deploy again.",
			base, strings.TrimSpace(named))
	}
	return ""
}
