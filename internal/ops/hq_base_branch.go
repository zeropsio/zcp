package ops

import (
	"fmt"
	"strings"
	"time"
)

// The git zcp runs against a Mate's HQ in a pair's checkout: the wiring puts
// the checkout on `main`, its history descending from the repository's `main`
// in HQ (BuildBaseBranchCommand). Everything after that is the agent's plain
// git: a branch of its own, pushed; `main` moves only by a merge, and nothing
// here ever pushes.

// baseBranch is the repository's `main` in HQ, which only a merge moves.
const baseBranch = "main"

// HQGitBound is the safety cap on one git command against HQ in all: a
// transfer HQ keeps feeding is legitimate work however long it takes, so what
// bounds it is the stall (hqGitStallSeconds). The cap catches the rest, a
// connect HQ never answers among them, which curl would wait 300 s for; it
// stays under the SSH runner's 5 min, so git
// ends itself before the session is cut. A container without `timeout` runs
// git bounded by the stall alone. A var so tests narrow it.
//
//nolint:gochecknoglobals // tuning knob, initialized; test-narrowed
var HQGitBound = 4 * time.Minute

// hqGitStallSeconds ends a transfer HQ stops feeding, with git's own words:
// below 1 byte/s for this long.
const hqGitStallSeconds = 10

// hqGit is `git <args>` against HQ — args already shell-quoted, the
// credential helper among them — bounded by HQGitBound and the stall bound,
// keeping git's exit status. Git speaks
// English (LC_ALL=C): its words are read, and a container's locale would
// translate them.
func hqGit(args string) string {
	seconds := max(1, int(HQGitBound/time.Second))
	return fmt.Sprintf(`{ if command -v timeout >/dev/null 2>&1; then bound="timeout %d"; else bound=; fi; `+
		`GIT_TERMINAL_PROMPT=0 LC_ALL=C $bound git -c http.lowSpeedLimit=1 -c http.lowSpeedTime=%d %s; }`,
		seconds, hqGitStallSeconds, args)
}

// BuildBaseBranchCommand puts the pair's working copy on `main`, SHARING its
// history with the repository's `main` in HQ, so every branch the agent cuts
// from it can land there.
//
// HQ makes a repository with one commit of an empty tree on `main`, while zcp
// git-initialises the pair with a history of its own. A change whose history
// shares nothing with `main` is unrelated to it, and HQ cannot merge it. So
// `main` is fetched first and the two histories are joined.
//
// The pair's code is never rewritten to do it. A rebase onto the base refused
// a dirty working copy and linearised a recipe's hand-resolved merges into
// different code (5 and 1 of the recipes, measured 2026-09-24). The states,
// each decided from facts and each with one action:
//
//   - HEAD shares history with the base — descends from it, or is a history
//     the base has since moved past →
//     only `main` is named, and the agent takes the base in with git;
//   - HEAD is only the `zcp init` marker (a parentless empty tree) → `main`
//     is put on the base, so whatever the base carries is in the working
//     copy. Refused by name if a change is staged (the
//     checkout would carry it onto the base); if the checkout would overwrite
//     an untracked file, a seed base is joined as below instead, any other
//     base refused by name;
//   - the base is provably HQ's seed — one parentless commit of the empty
//     tree → a commit with the pair's own tree and both histories as parents
//     (`commit-tree`, plumbing: no index, working copy or hook is touched), so
//     the pair's tree stays byte-identical;
//   - anything else — real code on both sides, unrelated → refused by name
//     (ZCP_BASE_NOT_SEED): two unrelated histories are never joined.
//
// `main` existing while HEAD is elsewhere is refused by name too.
// Every refusal changes nothing; BaseBranchRefusal reads which one it was and
// BaseBranchRefusalRemedy what to do about it.
//
// Idempotent: a `main` that already shares history with the base is left
// exactly where it is, so a later pass cannot rewrite the shas under an open
// change.
//
// Auth rides HQ's session credential helper, like every other remote-reading
// command here: the Mate credential reaches git over an anonymous pipe, never
// argv and never a file.
func BuildBaseBranchCommand(workingDir string) string {
	refuse := func(state string) string {
		return fmt.Sprintf(`{ echo "%s%s"; exit 5; }`, baseBranchRefusalMarker, state)
	}
	joinIdentity := fmt.Sprintf("-c user.email=%s -c user.name=%s",
		shellQuote(DeployGitIdentity.Email), shellQuote(DeployGitIdentity.Name))
	join := `c=$(git ` + joinIdentity + ` commit-tree "HEAD^{tree}" -p HEAD -p FETCH_HEAD -m "Join the repository's base") && ` +
		`git update-ref "$ref" "$c" "$old" && git symbolic-ref HEAD "$ref"; `
	return strings.Join([]string{
		"cd " + shellQuote(workingDir),
		gitIdentityEnsureFragment(),
		hqGit(fmt.Sprintf("%s fetch --no-tags origin %s", hqCredentialHelperArgs(), shellQuote(baseBranch))),
		gitHeadEnsureFragment(),
		"{ b=" + shellQuote(baseBranch) + `; ref="refs/heads/$b"; cur=$(git symbolic-ref -q HEAD || true); ` +
			`old=$(git rev-parse -q --verify "$ref" || true); ` +
			`if [ "$cur" != "$ref" ] && [ -n "$old" ]; then ` + refuse("ZCP_BRANCH_ELSEWHERE") + `; fi; ` +
			`isseed=; if [ "$(git rev-list --count FETCH_HEAD)" = 1 ] && [ "$(git rev-parse "FETCH_HEAD^{tree}")" = "$(git hash-object -t tree /dev/null)" ]; then isseed=1; fi; ` +
			`if git merge-base FETCH_HEAD HEAD >/dev/null 2>&1; then ` +
			`[ "$cur" = "$ref" ] || git checkout -q -b "$b"; ` +
			`elif [ "$(git rev-parse "HEAD^{tree}")" = "$(git hash-object -t tree /dev/null)" ] && ! git rev-parse -q --verify "HEAD^" >/dev/null; then ` +
			`if ! git diff --cached --quiet; then ` + refuse("ZCP_STAGED_CHANGES") + `; fi; ` +
			`collide=$(git ls-tree -r --name-only FETCH_HEAD | while IFS= read -r f; do if [ -e "$f" ] || [ -L "$f" ]; then printf '%s ' "$f"; fi; done); ` +
			`if [ -z "$collide" ]; then ` +
			`git checkout -q --detach FETCH_HEAD && git update-ref "$ref" "$(git rev-parse FETCH_HEAD)" "$old" && git symbolic-ref HEAD "$ref"; ` +
			`elif [ -n "$isseed" ]; then ` + join +
			`else echo "` + baseBranchRefusalMarker + `ZCP_UNTRACKED_COLLISION $collide"; exit 5; fi; ` +
			`elif [ -n "$isseed" ]; then ` + join +
			`else ` + refuse("ZCP_BASE_NOT_SEED") + `; fi; }`,
	}, " && ")
}

// markedLine is the rest of the first line of output that begins with marker,
// trimmed; empty when there is none.
func markedLine(output, marker string) string {
	for line := range strings.SplitSeq(output, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), marker); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// baseBranchRefusalMarker prefixes the named state a branch step refused, so
// the caller can say which one without parsing git's own words.
const baseBranchRefusalMarker = "ZCP_BRANCH_REFUSED:"

// BaseBranchRefusal reads the state BuildBaseBranchCommand refused out of its
// output, with anything it names ("ZCP_BASE_NOT_SEED",
// "ZCP_UNTRACKED_COLLISION app.js"); empty when it refused none.
func BaseBranchRefusal(output string) string { return markedLine(output, baseBranchRefusalMarker) }

// KeepOwnHistory keeps a checkout's history on a branch named after its
// commit before the checkout moves onto a `main` it shares nothing with: run
// again, it names the same commit again, and a commit made in between gets a
// branch of its own.
const KeepOwnHistory = `git branch -f "own-history-$(git rev-parse --short=12 HEAD)"`

// BaseBranchRefusalRemedy is the one thing to do about a refusal
// BaseBranchRefusal read, run in the pair's working copy; the next wiring
// then finds the state it can wire. Empty for a refusal it does not know.
func BaseBranchRefusalRemedy(refusal string) string {
	state, named, _ := strings.Cut(refusal, " ")
	switch state {
	case "ZCP_BASE_NOT_SEED":
		return fmt.Sprintf("%q already holds code this history shares nothing with, and two unrelated histories are never joined: keep this history on a branch of its own and put the working copy on %q — run `%s && git fetch origin %s && git reset --keep FETCH_HEAD`, which can be run again as it is — then carry what it needs over with `git cherry-pick` onto a branch cut from %q. That replaces the dev service's files with %q's code: when that code is not this project's own, as after the Mate moved to another application, ask the person first.",
			baseBranch, baseBranch, KeepOwnHistory, shellQuote(baseBranch), baseBranch, baseBranch)
	case "ZCP_BRANCH_ELSEWHERE":
		return fmt.Sprintf("%q already exists and the working copy is on another branch: run `git checkout %s`.",
			baseBranch, shellQuote(baseBranch))
	case "ZCP_STAGED_CHANGES":
		return fmt.Sprintf("the working copy is about to take %q's files and has changes staged: run `git restore --staged .` (the files stay in the working copy).", baseBranch)
	case "ZCP_UNTRACKED_COLLISION":
		return fmt.Sprintf("%q carries files the working copy has untracked (%s): move or delete them.",
			baseBranch, strings.TrimSpace(named))
	}
	return ""
}
