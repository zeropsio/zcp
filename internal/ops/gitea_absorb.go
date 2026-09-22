package ops

import (
	"fmt"
	"strings"
)

// giteaAbsorbNoopCommand is POSIX `true` — the shell no-op
// BuildAbsorbLandedPullRequestCommand returns when there is nothing recorded
// to absorb.
const giteaAbsorbNoopCommand = "true"

// BuildAbsorbLandedPullRequestCommand folds a pull request's landing on the
// repository's protected base into HEAD as a REAL merge, before the ordinary
// take-the-base-in step (BuildGiteaDeliveryCommand) runs — so a squash (or an
// ordinary merge-commit merge) of a Mate's own earlier work stops looking, to
// a plain 3-way merge, like two unrelated histories that both add the same
// files. Gitea squashes by default (MB-26): the squash commit shares no
// history with the branch that became it, so `git merge origin/<base>`
// diffs the base's common ancestor against BOTH sides independently and
// reports an add/add conflict on every file the branch ever touched — even
// though the content is identical. Measured live 2026-09-22: a Mate's very
// next task, on its own checkout, hit this on its first delivery after any
// squash merge.
//
// commit is S — the merge/squash commit Gitea reports as merge_commit_sha.
// head is H — the branch tip Gitea merged (pulls API head.sha), i.e. what
// the Mate's checkout was at the moment of the landing. Both come from
// ReadGiteaPullRequestOutcome and are recorded on the pair
// (workflow.GiteaRepoRef.Landed) between the moment a pass learns the
// request merged and the delivery that absorbs it.
//
// Never rebases, never force-pushes, never fails the delivery on its own —
// every unprovable or already-settled shape is a silent no-op that leaves
// the ordinary merge step to run as it always did:
//
//   - commit == "" → nothing recorded, no-op.
//   - S is already an ancestor of HEAD → already absorbed (or never needed
//     to be — a merge-commit merge that only fast-forwarded), no-op.
//   - H is not an ancestor of HEAD → the checkout has moved since, in a way
//     this fast track cannot reason about (a rewritten history, or a
//     landing of some OTHER branch state); no-op, ordinary merge decides.
//   - `git merge-tree --write-tree S^1 H` cannot prove the squash carries
//     EXACTLY H's tree — true for a squash or an ordinary merge commit,
//     unprovable for a rebase-merge, or for a container git older than
//     2.38 (no `merge-tree --write-tree`); no-op either way.
//
// Once proven lossless: `git merge --no-edit -q S^1` brings the base as it
// stood the moment BEFORE the landing (a real merge — a genuine conflict
// here is real, and aborts exactly like the ordinary merge step, marker and
// all), then `git merge -s ours --no-edit -q S` records S itself as merged
// without touching the tree, since its content is already proven present.
// The ordinary `merge origin/<base>` step that follows brings in only
// whatever landed on the base after S.
func BuildAbsorbLandedPullRequestCommand(commit, head string) string {
	if commit == "" {
		return giteaAbsorbNoopCommand
	}
	qCommit := shellQuote(commit)
	qHead := shellQuote(head)
	qCommitParent := shellQuote(commit + "^1")
	qCommitTree := shellQuote(commit + "^{tree}")
	steps := []string{
		fmt.Sprintf("git rev-parse -q --verify %s >/dev/null 2>&1 || exit 0", qCommit),
		fmt.Sprintf("git merge-base --is-ancestor %s HEAD 2>/dev/null && exit 0", qCommit),
		fmt.Sprintf("git rev-parse -q --verify %s >/dev/null 2>&1 || exit 0", qHead),
		fmt.Sprintf("git merge-base --is-ancestor %s HEAD 2>/dev/null || exit 0", qHead),
		fmt.Sprintf("provenTree=$(git merge-tree --write-tree %s %s 2>/dev/null) || exit 0", qCommitParent, qHead),
		fmt.Sprintf("wantTree=$(git rev-parse -q --verify %s 2>/dev/null) || exit 0", qCommitTree),
		`[ -n "$provenTree" ] && [ "$provenTree" = "$wantTree" ] || exit 0`,
		fmt.Sprintf(`git merge --no-edit -q %s || (conflicts=$(git diff --name-only --diff-filter=U | tr '\n' ' '); git merge --abort >/dev/null 2>&1; echo "%s$conflicts"; exit 4)`,
			qCommitParent, giteaDeliveryConflictMarker),
		fmt.Sprintf("git merge -s ours --no-edit -q %s", qCommit),
	}
	// `; `, not `&&`/`||`: each step above already decides its own early
	// exit (`|| exit 0`, `|| (...; exit 4)`), so the next one must run
	// unconditionally unless a previous one exited the subshell outright.
	return "(" + strings.Join(steps, "; ") + ")"
}
