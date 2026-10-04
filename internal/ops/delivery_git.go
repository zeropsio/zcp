package ops

import (
	"fmt"
	"strconv"
	"strings"
)

// The git a Mate's delivery runs in its pair's checkout (SPEC §3.2a). The Mate
// works on a local branch of its own that descends from HQ's `main`; a
// delivery commits the tree as deployed, takes `main` in — a squash of this
// Mate's own earlier change absorbed first (delivery_absorb.go) — and reports
// how far ahead of `main` that leaves it. HQ takes a Mate's push only on the
// branch of its own open change, `mate/<project id>/<number>`. HQ decides from
// the candidate tree hash whether a change is needed before zcp pushes HEAD there
// (BuildChangePushCommand). `main` moves only by HQ's merge: nothing here ever
// pushes it.

// deliveryBase is the branch a Mate's change lands on: `main`, which only HQ's
// merge moves.
const deliveryBase = "main"

// BuildMateBranchCommand puts the pair's working copy on the Mate's own local
// branch, SHARING its history with the repository's `main`.
//
// HQ makes a repository with one commit of an empty tree on `main`, while zcp
// git-initialises the pair with a history of its own. A change whose history
// shares nothing with `main` is unrelated to it, and HQ cannot squash it. So
// `main` is fetched first and the two histories are joined.
//
// The pair's code is never rewritten to do it. A rebase onto the base refused
// a dirty working copy and linearised a recipe's hand-resolved merges into
// different code (5 and 1 of the recipes, measured 2026-09-24). The states,
// each decided from facts and each with one action:
//
//   - HEAD shares history with the base — descends from it, or is a history
//     the base has since moved past →
//     only the branch is named, and the delivery takes the base in;
//   - HEAD is only the `zcp init` marker (a parentless empty tree) → the
//     branch is cut from the base, so whatever the base carries stays in the
//     branch and the working copy. Refused by name if a change is staged (the
//     checkout would carry it onto the base); if the checkout would overwrite
//     an untracked file, a seed base is joined as below instead, any other
//     base refused by name;
//   - the base is provably HQ's seed — one parentless commit of the empty
//     tree → a commit with the pair's own tree and both histories as parents
//     (`commit-tree`, plumbing: no index, working copy or hook is touched), so
//     the pair's tree stays byte-identical;
//   - anything else — real code on both sides, unrelated → refused by name
//     (ZCP_BASE_NOT_SEED): no mechanical answer keeps both.
//
// The Mate's branch existing while HEAD is elsewhere is refused by name too.
// Every refusal changes nothing; MateBranchRefusal reads which one it was and
// MateBranchRefusalRemedy what to do about it.
//
// Idempotent: a branch that already shares history with the base is left
// exactly where it is, so a later pass cannot rewrite the shas under an open
// change.
//
// Auth rides HQ's session credential helper, like every other remote-reading
// command here: the Mate credential reaches git over an anonymous pipe, never
// argv and never a file.
func BuildMateBranchCommand(workingDir, branch string) string {
	refuse := func(state string) string {
		return fmt.Sprintf(`{ echo "%s%s"; exit 5; }`, mateBranchRefusalMarker, state)
	}
	joinIdentity := fmt.Sprintf("-c user.email=%s -c user.name=%s",
		shellQuote(DeployGitIdentity.Email), shellQuote(DeployGitIdentity.Name))
	join := `c=$(git ` + joinIdentity + ` commit-tree "HEAD^{tree}" -p HEAD -p FETCH_HEAD -m "Join the repository's base") && ` +
		`git update-ref "$ref" "$c" "$old" && git symbolic-ref HEAD "$ref"; `
	return strings.Join([]string{
		"cd " + shellQuote(workingDir),
		gitIdentityEnsureFragment(),
		fmt.Sprintf("GIT_TERMINAL_PROMPT=0 git %s fetch --no-tags origin %s",
			hqCredentialHelperArgs(), shellQuote(deliveryBase)),
		gitHeadEnsureFragment(),
		"{ b=" + shellQuote(branch) + `; ref="refs/heads/$b"; cur=$(git symbolic-ref -q HEAD || true); ` +
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
			`else echo "` + mateBranchRefusalMarker + `ZCP_UNTRACKED_COLLISION $collide"; exit 5; fi; ` +
			`elif [ -n "$isseed" ]; then ` + join +
			`else ` + refuse("ZCP_BASE_NOT_SEED") + `; fi; }`,
	}, " && ")
}

// deliveryUnignoredMarker prefixes the dependency directories a delivery
// refused to commit, so the caller can name them.
const deliveryUnignoredMarker = "ZCP_UNIGNORED:"

// deliveryConflictMarker prefixes the files `main` and the Mate's work both
// changed, when taking `main` in could not be done without a decision.
const deliveryConflictMarker = "ZCP_MERGE_CONFLICT:"

// deliveryAheadMarker prefixes how many commits the checkout's HEAD has that
// `main` does not, once `main` is taken in; this is not a content verdict.
const deliveryAheadMarker = "ZCP_AHEAD:"

// BuildDeliveryCommand readies a wired pair's working tree, as it was deployed,
// to be delivered: it commits everything with the task's words, takes `main`
// in and reports how far ahead of it HEAD is (DeliveryAhead). It pushes
// nothing: the change it pushes to is opened first (BuildChangePushCommand).
//
// A dependency directory the repository does not ignore stops it before
// anything is staged: committing node_modules is never what a person meant,
// and the .gitignore stays the agent's to write (InitServiceGit). A clean tree
// commits nothing.
//
// landedCommit and landedHead are the landing of this pair's own earlier
// change the delivery is catching up to — workflow.HQRepoRef.Landed, "" when
// none is recorded — absorbed (BuildAbsorbLandedChangeCommand) BEFORE the
// ordinary take-`main`-in merge, so a squash of this Mate's own history does
// not read as two unrelated histories that both add the same files (MB-26).
func BuildDeliveryCommand(workingDir, message, landedCommit, landedHead string) string {
	steps := []string{ //nolint:prealloc // clearer as a literal; one known append follows, not a growth loop
		"cd " + shellQuote(workingDir),
		gitIdentityEnsureFragment(),
		`{ unignored=""; for d in node_modules vendor .venv; do if [ -d "$d" ] && ! git check-ignore -q "$d"; then unignored="$unignored $d"; fi; done; ` +
			`if [ -n "$unignored" ]; then echo "` + deliveryUnignoredMarker + `$unignored"; exit 3; fi; }`,
		"git add -A",
		fmt.Sprintf("(git diff --cached --quiet || git commit -q -m %s)", shellQuote(message)),
	}
	steps = append(steps, deliverySyncSteps(landedCommit, landedHead, message)...)
	return strings.Join(steps, " && ")
}

// BuildDeliverySyncCommand catches a pair's own checkout up with `main` — and
// a landing of its own change — WITHOUT committing anything of the agent's:
// the exact fetch/absorb/take-`main`-in sequence BuildDeliveryCommand runs
// after its commit, with the same report of how far ahead HEAD is. A push of
// committed work runs it before asking HQ for a change; an owed delivery runs
// the same steps when it finishes. A
// conflict aborts and leaves the checkout exactly as it was.
func BuildDeliverySyncCommand(workingDir, landedCommit, landedHead string) string {
	steps := append([]string{"cd " + shellQuote(workingDir)}, deliverySyncSteps(landedCommit, landedHead, "Work after the landed change")...)
	return strings.Join(steps, " && ")
}

// deliverySyncSteps is the shared tail of BuildDeliveryCommand and
// BuildDeliverySyncCommand once the working directory is current: fetch the
// remote, absorb a known-lossless landing as a real merge, take in whatever
// else `main` carries that this branch does not, and say how far ahead HEAD
// is then.
//
// An application has more than one Mate and they land in turn, so a branch is
// behind the moment somebody else merges. A merge and not a rebase: history
// only moves forward, so a push built on this stays an ordinary one, HQ takes
// a change's branch only forward, and no force can lose a commit.
func deliverySyncSteps(landedCommit, landedHead, message string) []string {
	remoteBase := shellQuote("origin/" + deliveryBase)
	return []string{
		fmt.Sprintf("GIT_TERMINAL_PROMPT=0 git %s fetch --no-tags -q origin", hqCredentialHelperArgs()),
		BuildAbsorbLandedChangeCommand(landedCommit, landedHead),
		// Already contains `main` → nothing to do. Otherwise merge it, and a
		// collision only a person or the agent can settle leaves the checkout
		// exactly as it was, named in the output.
		fmt.Sprintf("(git merge-base --is-ancestor %s HEAD 2>/dev/null"+
			" || ! git rev-parse -q --verify %s >/dev/null 2>&1"+
			" || git merge --no-edit -q %s"+
			" || (conflicts=$(git diff --name-only --diff-filter=U | tr '\n' ' ');"+
			" git merge --abort >/dev/null 2>&1;"+
			` echo "%s$conflicts"; exit 4))`,
			remoteBase, remoteBase, remoteBase, deliveryConflictMarker),
		freshDeliveryBase(landedCommit, landedHead, message),
		fmt.Sprintf(`{ ahead=$(git rev-list --count %s 2>/dev/null) || ahead=$(git rev-list --count HEAD); echo "%s$ahead"; }`,
			shellQuote("origin/"+deliveryBase+"..HEAD"), deliveryAheadMarker),
	}
}

// BuildChangePushCommand pushes the checkout's HEAD to branch, the branch of
// the Mate's open change — never to `main`, which only HQ's merge moves —
// and makes it the local branch's upstream.
func BuildChangePushCommand(workingDir, branch string) string {
	return fmt.Sprintf("cd %s && GIT_TERMINAL_PROMPT=0 git %s push -u origin %s 2>&1",
		shellQuote(workingDir), hqCredentialHelperArgs(), shellQuote("HEAD:refs/heads/"+branch))
}

// BuildTakeChangeInCommand takes the branch of the Mate's open change, as HQ
// holds it now, into the checkout before it is pushed there. HQ takes a
// change's branch only forward, and the branch can hold a commit the checkout
// lacks — one Core wrote there, or a checkout lost and made again — which
// would refuse every later push for good. A merge, as with `main`: nothing is
// rewritten, and a collision only a person or the agent can settle leaves the
// checkout exactly as it was, named by the delivery's conflict marker
// (DeliveryConflict). A branch HQ does not have yet, or one HEAD already
// holds, is nothing to take in.
func BuildTakeChangeInCommand(workingDir, branch string) string {
	ref := shellQuote("refs/heads/" + branch)
	tracking := shellQuote("origin/" + branch)
	return strings.Join([]string{
		"cd " + shellQuote(workingDir),
		fmt.Sprintf("{ GIT_TERMINAL_PROMPT=0 git %s ls-remote --exit-code --heads origin %s >/dev/null; held=$?;"+
			" if [ $held -eq 2 ]; then exit 0; fi; [ $held -eq 0 ]; }", hqCredentialHelperArgs(), ref),
		fmt.Sprintf("GIT_TERMINAL_PROMPT=0 git %s fetch --no-tags -q origin %s",
			hqCredentialHelperArgs(), shellQuote("+refs/heads/"+branch+":refs/remotes/origin/"+branch)),
		fmt.Sprintf("(git merge-base --is-ancestor %s HEAD"+
			" || git merge --no-edit -q %s"+
			" || (conflicts=$(git diff --name-only --diff-filter=U | tr '\\n' ' ');"+
			" git merge --abort >/dev/null 2>&1;"+
			` echo "%s$conflicts"; exit 4))`,
			tracking, tracking, deliveryConflictMarker),
	}, " && ")
}

// DeliveryUnignored reads the dependency directories a delivery refused to
// commit out of its output, space-separated; empty when it refused none.
func DeliveryUnignored(output string) string { return markedLine(output, deliveryUnignoredMarker) }

// DeliveryConflict reads the files `main` and the Mate's work both changed out
// of a delivery's output, space-separated; empty when `main` came in cleanly
// or there was none to take in.
func DeliveryConflict(output string) string { return markedLine(output, deliveryConflictMarker) }

// DeliveryAhead reads how many commits HEAD has that `main` does not out of a
// delivery's output; found is false when the delivery stopped before it said.
func DeliveryAhead(output string) (ahead int, found bool) {
	for line := range strings.SplitSeq(output, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), deliveryAheadMarker); ok {
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			return n, err == nil
		}
	}
	return 0, false
}

// ChangePushRefusal reads why HQ refused a change's branch out of a push's
// output — git reports a refused ref as `! [remote rejected] HEAD -> <ref>
// (<reason>)`, with HQ's own reason (unknown_change, change_closed,
// not_your_ref, ...), or `! [rejected]` for a push that is not forward; empty
// when no ref was refused.
func ChangePushRefusal(output string) string {
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "! [remote rejected]") && !strings.HasPrefix(line, "! [rejected]") {
			continue
		}
		if opening, closing := strings.LastIndex(line, "("), strings.LastIndex(line, ")"); opening >= 0 && closing > opening {
			return line[opening+1 : closing]
		}
		return "refused"
	}
	return ""
}

// GitRemoteUnavailable reports whether a git command's output says the remote
// could not serve it now — not reached at all, or a 5xx such as HQ's standby
// 503 (git prints the status without the Retry-After) — rather than refused
// it: a delivery stopped by it is pending, never failed.
func GitRemoteUnavailable(output string) bool {
	return strings.Contains(output, "returned error: 5") ||
		(strings.Contains(output, "unable to access") && !strings.Contains(output, "returned error: 4"))
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

// mateBranchRefusalMarker prefixes the named state a branch step refused, so
// the caller can say which one without parsing git's own words.
const mateBranchRefusalMarker = "ZCP_BRANCH_REFUSED:"

// MateBranchRefusal reads the state BuildMateBranchCommand refused out of its
// output, with anything it names ("ZCP_BASE_NOT_SEED",
// "ZCP_UNTRACKED_COLLISION app.js"); empty when it refused none.
func MateBranchRefusal(output string) string { return markedLine(output, mateBranchRefusalMarker) }

// MateBranchRefusalRemedy is the one thing to do about a refusal
// MateBranchRefusal read, run in the pair's working copy; the next attempt
// then finds the state it can wire. Empty for a refusal it does not know.
func MateBranchRefusalRemedy(refusal, branch string) string {
	state, named, _ := strings.Cut(refusal, " ")
	switch state {
	case "ZCP_BASE_NOT_SEED":
		return fmt.Sprintf("%q already holds code and this history is unrelated to it, so take it in: run `git fetch origin %s && git merge --allow-unrelated-histories --no-edit FETCH_HEAD`, resolve any conflicts and commit, then deploy again — the base is then part of the history and the branch is cut from it.",
			deliveryBase, shellQuote(deliveryBase))
	case "ZCP_BRANCH_ELSEWHERE":
		return fmt.Sprintf("%q already exists and the working copy is on another branch: run `git checkout %s`, then deploy again.",
			branch, shellQuote(branch))
	case "ZCP_STAGED_CHANGES":
		return fmt.Sprintf("the working copy is about to take %q's files and has changes staged: run `git restore --staged .` (the files stay in the working copy), then deploy again.", deliveryBase)
	case "ZCP_UNTRACKED_COLLISION":
		return fmt.Sprintf("%q carries files the working copy has untracked (%s): move or delete them, then deploy again.",
			deliveryBase, strings.TrimSpace(named))
	}
	return ""
}
