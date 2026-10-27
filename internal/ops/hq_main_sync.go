package ops

import (
	"fmt"
	"strings"
)

// BuildMainSyncCommand checks a wired pair's local `main` against the
// repository's `main` in HQ, fetched from repoURL — HQ's address, never
// `origin`: in a checkout kept on GitHub, origin is GitHub, which only the
// Mate's own shell can answer. HQ's `main` follows GitHub's there.
//
// A checkout wired before branches of the agent's own kept zcp's seed — one
// parentless commit of the empty tree, unrelated to HQ's history — as its
// `main`, the agent working on another branch, so `git merge --ff-only
// origin/main` fails on it. The states, each with one action:
//
//   - no `main`, or a `main` HQ's has or descends from → nothing: the agent's
//     fast-forward takes HQ's in;
//   - a seed-only `main`, nothing of the agent's on it → `main` is put on
//     HQ's (MainFromSeed): moved by ref when the checkout is elsewhere, checked
//     out when it is on `main` — refused by git, nothing moved, when an
//     uncommitted file is in the way (MainSeedInTheWay); a `main` that moves
//     while it is switched, a commit of the person's landing on it, is theirs
//     and stays checked out where they put it;
//   - commits HQ's `main` does not have that another ref holds too — a
//     branch, or the branch of a change pushed from `main` → nothing: they are
//     kept, and the agent's guidance names the way onto HQ's `main`;
//   - a history of its own sharing nothing with HQ's unwritten root — one
//     parentless commit of the empty tree, as a repository is born — that only
//     `main` holds → left where it is (MainFirstHistory): it is the
//     repository's first history, pushed as it is on a branch, whose change
//     HQ makes `main`; never carried onto that root;
//   - commits only `main` holds → left where it is (MainOwnCommits), the step
//     that keeps them named: `main` moves only by a merge, and no commit of
//     the agent's is ever discarded.
//
// Run again, it changes nothing more. Auth rides HQ's session credential
// helper, as BuildBaseBranchCommand's does.
func BuildMainSyncCommand(workingDir, repoURL string) string {
	mark := func(state string) string { return `echo "` + mainSyncMarker + state + `"` }
	return strings.Join([]string{
		"cd " + shellQuote(workingDir),
		hqGit(fmt.Sprintf("%s fetch -q --no-tags %s %s", hqCredentialHelperArgs(), shellQuote(repoURL), shellQuote(baseBranch))),
		`{ ref="refs/heads/` + baseBranch + `"; new=$(git rev-parse FETCH_HEAD); old=$(git rev-parse -q --verify "$ref" || true); ` +
			`if [ -z "$old" ] || git merge-base --is-ancestor "$old" "$new"; then exit 0; fi; ` +
			`if [ "$(git rev-list --count "$old")" = 1 ] && [ "$(git rev-parse "$old^{tree}")" = "$(git hash-object -t tree /dev/null)" ]; then ` +
			`if [ "$(git symbolic-ref -q HEAD)" != "$ref" ]; then git update-ref "$ref" "$new" "$old" && ` + mark(MainFromSeed+` $new`) + `; exit; fi; ` +
			`if git checkout -q --detach "$new" 2>/dev/null; then if git update-ref "$ref" "$new" "$old" 2>/dev/null; then git symbolic-ref HEAD "$ref" && ` + mark(MainFromSeed+` $new`) + `; exit; fi; ` +
			`git checkout -q ` + shellQuote(baseBranch) + `; exit; fi; ` +
			mark(MainSeedInTheWay) + `; exit 0; fi; ` +
			`if git for-each-ref --contains "$old" --format='%(refname)' | grep -qvx "$ref"; then exit 0; fi; ` +
			`if [ "$(git rev-list --count "$new")" = 1 ] && [ "$(git rev-parse "$new^{tree}")" = "$(git hash-object -t tree /dev/null)" ] && ! git merge-base "$old" "$new" >/dev/null; then ` +
			mark(MainFirstHistory) + `; exit 0; fi; ` +
			mark(MainOwnCommits+` $(git rev-list --count "$new..$old")`) + `; }`,
	}, " && ")
}

// mainSyncMarker prefixes the state BuildMainSyncCommand found, so the caller
// reads it without parsing git's own words.
const mainSyncMarker = "ZCP_MAIN:"

// The states BuildMainSyncCommand reports; it reports none when there is
// nothing to say.
const (
	// MainFromSeed: a seed-only `main` is HQ's now; the detail is its commit.
	MainFromSeed = "FROM_SEED"
	// MainSeedInTheWay: a seed-only `main` is checked out and an uncommitted
	// file is where HQ's `main` puts one; nothing moved.
	MainSeedInTheWay = "SEED_IN_THE_WAY"
	// MainOwnCommits: `main` holds commits HQ's does not, and no other ref
	// holds them; the detail is how many. Nothing moved.
	MainOwnCommits = "OWN_COMMITS"
	// MainFirstHistory: HQ's `main` is its unwritten root and `main` is a
	// history of its own, sharing nothing with it, that no other ref holds —
	// the repository's first history, to push as it is. Nothing moved.
	MainFirstHistory = "FIRST_HISTORY"
)

// MainSync reads the state BuildMainSyncCommand found out of its output, with
// its detail; "" when it had nothing to say.
func MainSync(output string) (state, detail string) {
	state, detail, _ = strings.Cut(markedLine(output, mainSyncMarker), " ")
	return state, strings.TrimSpace(detail)
}
