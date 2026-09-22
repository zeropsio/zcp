package ops

import (
	"fmt"
	"strings"
)

// giteaAbsorbNoopCommand is POSIX `true` — the shell no-op
// BuildAbsorbLandedPullRequestCommand returns when there is nothing recorded
// to absorb.
const giteaAbsorbNoopCommand = "true"

// giteaAbsorbConflictMarker prefixes the files a REAL conflict inside the
// absorb's own S^1 merge touched — distinct from giteaDeliveryConflictMarker
// (the ordinary take-the-base-in step's conflict), because the recovery is
// different: an absorb conflict happens BEFORE anything from the base
// landed, so the fix is the manual absorb sequence (merge S^1, resolve,
// merge -s ours S, then merge the base); the ordinary step's conflict is a
// plain "take the base in" collision, resolved with a plain fetch+merge.
const giteaAbsorbConflictMarker = "ZCP_ABSORB_CONFLICT:"

// giteaAbsorbUnprovableMarker marks a landing that WAS recorded but could
// not be proven lossless even after the portable plumbing fallback below —
// a rebase-merge, or a merge commit a person resolved by hand differently
// from a mechanical 3-way merge. Either way the absorb falls through as a
// silent no-op and the ordinary take-the-base-in step runs unabsorbed —
// which, for a genuine squash of this Mate's own earlier work, reproduces
// the very false add/add conflict this whole mechanism exists to prevent.
// The marker lets a caller that then sees an ordinary conflict tell the
// difference and give the right advice instead of sending the agent in a
// circle.
const giteaAbsorbUnprovableMarker = "ZCP_ABSORB_UNPROVABLE"

// giteaAbsorbDirtyMarker marks a landing that WAS recorded and provably
// safe to absorb, but the S^1 merge itself never ran: uncommitted changes
// in the checkout touch what it would merge, and git refuses to even start
// rather than overwrite them. Distinct from giteaAbsorbConflictMarker
// (which the S^1 merge's OWN abort emits, with a real conflicting-file
// list) — this fires before any merge attempt, so there is no unmerged
// index entry to name; a caller must not read the resulting bare marker as
// "no conflict" (empty file list from GiteaAbsorbConflict) and push
// anyway.
const giteaAbsorbDirtyMarker = "ZCP_ABSORB_DIRTY"

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
//   - Neither `git merge-tree --write-tree S^1 H` (fast path, git ≥2.38)
//     NOR the portable plumbing fallback (a temporary index: `read-tree -m
//     -i --aggressive <merge-base> S^1 H && write-tree`, which works on any
//     git and is tried whenever the fast path fails for ANY reason —
//     unsupported subcommand, or a real conflict the merge itself hit) can
//     prove the squash carries EXACTLY S's tree — true for a squash or an
//     ordinary merge commit computed mechanically, unprovable for a
//     rebase-merge or a merge commit a person resolved by hand differently;
//     no-op, but marked (giteaAbsorbUnprovableMarker) — a landing WAS
//     recorded, so a caller that then sees the ordinary step conflict can
//     tell it may be this same false shape, unresolved. Soundness of the
//     fallback does not depend on which algorithm computed the candidate
//     tree: either candidate is accepted ONLY on an exact match against
//     S^{tree}, a direct byte-for-byte proof, never an inference from one
//     merge strategy agreeing with another.
//
// Once proven lossless: the checkout must be clean first — uncommitted
// changes touching what S^1 would merge make git refuse to even start, and
// that is marked too (giteaAbsorbDirtyMarker), distinctly from a real
// conflict (there is no unmerged index entry to name; a caller must not
// read the resulting bare marker as "no conflict" and push anyway). Then
// `git merge --no-edit -q S^1` brings the base as it stood the moment
// BEFORE the landing (a real merge — a genuine conflict here is real:
// aborted, marked giteaAbsorbConflictMarker, and the chain stopped there —
// never falls through to the ordinary step, which would otherwise push a
// history that records S as merged while the checkout still lacks whatever
// else was on the base before S), then `git merge -s ours --no-edit -q S`
// records S itself as merged without touching the tree, since its content
// is already proven present. The ordinary `merge origin/<base>` step that
// follows brings in only whatever landed on the base after S.
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
		fmt.Sprintf(`wantTree=$(git rev-parse -q --verify %s 2>/dev/null) || { echo "%s"; exit 0; }`,
			qCommitTree, giteaAbsorbUnprovableMarker),
		// Fast path (git >=2.38): a pure plumbing merge, touches neither the
		// working tree nor the index.
		fmt.Sprintf(`provenTree=$(git merge-tree --write-tree %s %s 2>/dev/null)`, qCommitParent, qHead),
		// Portable fallback, tried whenever the fast path did not already
		// prove the match: a real 3-way merge into a TEMPORARY index (never
		// the real one — GIT_INDEX_FILE + -i), using the actual merge-base
		// of S^1 and H as the common ancestor, same as merge-tree computes
		// internally. `--aggressive` auto-resolves the trivial cases (a
		// file added/deleted/modified on only one side); unmerged entries
		// after that are a REAL conflict the plumbing could not resolve —
		// write-tree then refuses (conservative: reads as unprovable, never
		// as a false match). `mktemp` itself creates the file (0 bytes,
		// which `read-tree` rejects as "index file smaller than expected"
		// — it is not the same as "does not exist yet"), so it is removed
		// again right before use, leaving only the PATH; `read-tree` then
		// initializes a fresh index there. Always cleaned up after.
		fmt.Sprintf(`if [ -z "$provenTree" ] || [ "$provenTree" != "$wantTree" ]; then tmpidx=$(mktemp) && rm -f "$tmpidx" && base=$(git merge-base %s %s 2>/dev/null) && GIT_INDEX_FILE="$tmpidx" git read-tree -m -i --aggressive "$base" %s %s 2>/dev/null && provenTree=$(GIT_INDEX_FILE="$tmpidx" git write-tree 2>/dev/null); rm -f "$tmpidx"; fi`,
			qCommitParent, qHead, qCommitParent, qHead),
		fmt.Sprintf(`[ -n "$provenTree" ] && [ "$provenTree" = "$wantTree" ] || { echo "%s"; exit 0; }`,
			giteaAbsorbUnprovableMarker),
		// The checkout must be clean before the real merge below touches
		// it — uncommitted changes to a path S^1 would merge make git
		// refuse outright, with no unmerged index entry to name (unlike a
		// real merge conflict), so this is its own marker, checked BEFORE
		// the merge is attempted rather than inferred from its failure.
		fmt.Sprintf(`[ -z "$(git status --porcelain 2>/dev/null)" ] || { echo "%s"; exit 6; }`, giteaAbsorbDirtyMarker),
		// A brace group, not `(...)`: `exit 4` inside a `(...)` subshell only
		// terminates THAT subshell — the `; `-joined chain below would still
		// run `git merge -s ours`, the whole absorb block would exit 0, and
		// the ordinary step would then see origin/<base> as already an
		// ancestor and skip, pushing a history that records S as merged
		// while a real conflict inside S^1 was silently dropped (measured
		// live 2026-09-23). `{ ...; }` runs in THIS subshell, so `exit 4`
		// here ends the whole absorb block immediately, exactly like every
		// `exit 0` above.
		fmt.Sprintf(`git merge --no-edit -q %s || { conflicts=$(git diff --name-only --diff-filter=U | tr '\n' ' '); git merge --abort >/dev/null 2>&1; echo "%s$conflicts"; exit 4; }`,
			qCommitParent, giteaAbsorbConflictMarker),
		fmt.Sprintf("git merge -s ours --no-edit -q %s", qCommit),
	}
	// `; `, not `&&`/`||`: each step above already decides its own early
	// exit (`|| exit 0`, `|| { ...; exit N; }`), so the next one must run
	// unconditionally unless a previous one exited the subshell outright.
	return "(" + strings.Join(steps, "; ") + ")"
}

// GiteaAbsorbConflict reads the files a REAL conflict inside the absorb's
// own S^1 merge touched out of its output, space-separated; empty when
// there was none. Distinct from GiteaDeliveryConflict (the ordinary
// take-the-base-in step) — a caller sees at most one of the two per run,
// since a chain that hits this one stops before the ordinary step ever
// runs.
func GiteaAbsorbConflict(output string) string {
	for line := range strings.SplitSeq(output, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), giteaAbsorbConflictMarker); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// GiteaAbsorbDirty reports whether a recorded landing, proven safe to
// absorb, could not actually be merged because the checkout was not clean
// — never reported alongside GiteaAbsorbConflict (whose file list would be
// empty here, since no merge ever started to leave an unmerged entry to
// name; a caller must not read that empty list as "no conflict" and push
// anyway).
func GiteaAbsorbDirty(output string) bool {
	return strings.Contains(output, giteaAbsorbDirtyMarker)
}

// GiteaAbsorbUnprovable reports whether a recorded landing fell through
// unabsorbed because it could not be proven lossless (a rebase-merge, or a
// container git too old for `merge-tree --write-tree`) — a caller that then
// sees the ordinary step's conflict marker can tell the difference between
// an unrelated collision and this Mate's own unabsorbed squash coming back.
func GiteaAbsorbUnprovable(output string) bool {
	return strings.Contains(output, giteaAbsorbUnprovableMarker)
}
