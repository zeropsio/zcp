package tools

import (
	"fmt"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A Mate's work ends in a change a person reviews (docs/spec-mate.md §10.10),
// and a branch a lease holds is that work on its way. Nothing proposes it
// until the agent opens its change, and a finished pool-migrate left Odin's
// 206-commit `dev-pools` with none until the person asked (2026-10-27). So the
// answers that close a stretch of work — the retire's, the develop close —
// name each such branch with the open-change call. Where the Mate holds open
// changes of its own in that repository, they name those too: AGENTS.md's
// "What one change holds" may put the work on one of them instead
// (docs/spec-mate.md §10.10).

// unproposedBranch is a branch a lease of this Mate's HQ repository holds that
// no change proposes, by the lease holding it, and the Mate's own open changes
// in that repository the work may be added to instead.
type unproposedBranch struct {
	alias, branch string
	addTo         []workflow.HQChange
}

// unproposedLeaseBranches names, inside a Mate, each branch zcp's leases of
// this Mate's HQ repositories hold — never main, never a mate/ branch (an
// open change's own) — that the change ledger keeps no change on: one per
// repository and branch, by the first lease holding it. pools limits it to
// leases of those pools (nil: every lease). With offer, each names the
// ledger's open changes of the Mate's own in its repository (ownOpenChanges)
// — the develop close's note; pool-migrate's retire offers none, since a
// migration branch carries the pool's setup, deploy configuration, which
// always gets a change of its own. It reads zcp's own records only.
func unproposedLeaseBranches(stateDir string, rt runtime.Info, pools []string, offer bool) []unproposedBranch {
	if !rt.MateEnabled || stateDir == "" {
		return nil
	}
	address := hqAddress(rt)
	if address == "" {
		return nil
	}
	leases, err := ops.LeaseBranches(stateDir)
	if err != nil {
		return nil
	}
	changes, _ := workflow.PeekHQChanges(stateDir)
	seen := map[string]bool{}
	var out []unproposedBranch
	for _, l := range leases {
		if pools != nil && !slices.Contains(pools, l.Pool) {
			continue
		}
		if l.Branch == "" || l.Branch == hqBase || strings.HasPrefix(l.Branch, "mate/") {
			continue
		}
		appID, repo, ok := hq.RepoOfURL(address, l.Repo)
		if !ok {
			continue
		}
		key := appID + "/" + repo + "/" + l.Branch
		if seen[key] {
			continue
		}
		seen[key] = true
		inRepo := changesIn(changes, appID, repo)
		if slices.ContainsFunc(inRepo, func(c workflow.HQChange) bool { return c.Branch == l.Branch }) {
			continue
		}
		b := unproposedBranch{alias: l.Alias, branch: l.Branch}
		if offer {
			b.addTo = ownOpenChanges(inRepo)
		}
		out = append(out, b)
	}
	slices.SortFunc(out, func(a, b unproposedBranch) int { return strings.Compare(a.branch+a.alias, b.branch+b.alias) })
	return out
}

// ownOpenChanges is, of a repository's changes the ledger keeps, those the
// work of another branch may be added to: the Mate's own — never one a person
// handed it — on a branch HQ named, and not told settled.
func ownOpenChanges(inRepo []workflow.HQChange) []workflow.HQChange {
	var own []workflow.HQChange
	for _, c := range inRepo {
		if c.Source == workflow.HQChangeFromHanded || c.Branch == "" || (c.Told != nil && c.Told.State != "" && c.Told.State != hq.ChangeOpen) {
			continue
		}
		own = append(own, c)
	}
	return own
}

// unproposedNote is the sentence naming each unproposed branch with the
// open-change call that proposes it and, where the Mate holds open changes of
// its own in the repository, adding the work to one of them instead; "" for
// none.
func unproposedNote(branches []unproposedBranch) string {
	if len(branches) == 0 {
		return ""
	}
	calls := make([]string, 0, len(branches))
	adds := false
	for _, b := range branches {
		call := fmt.Sprintf("%q (lease %s): zerops_workflow action=\"open-change\" service=%q title=\"<what the work does>\"", b.branch, b.alias, b.alias)
		if len(b.addTo) > 0 {
			adds = true
			call += addToOpenChangeWords(b)
		}
		calls = append(calls, call)
	}
	end := " — open it once the work is ready for the person to review; it is how the work reaches main."
	if adds {
		end = " — do either once the work is ready for the person to review; it is how the work reaches main."
	}
	return "No change in HQ proposes the work on " + strings.Join(calls, "; ") + end
}

// addToOpenChangeWords offer b's work to the Mate's own open changes in its
// repository, as AGENTS.md's "What one change holds" allows. A lease checkout
// holds only its own branch, so the branch is pushed from its lease and
// merged as origin/<branch> where the open change's branch is checked out;
// then that change is described again and b's branch released — every lease
// of its group, and only once pushed, which a release otherwise refuses — so
// it is no longer named as unproposed.
func addToOpenChangeWords(b unproposedBranch) string {
	named := make([]string, 0, len(b.addTo))
	for _, c := range b.addTo {
		named = append(named, fmt.Sprintf("#%d on %q", c.Number, c.Branch))
	}
	which := "your open change " + named[0]
	if len(named) > 1 {
		which = "one of your open changes (" + strings.Join(named, ", ") + ")"
	}
	return fmt.Sprintf(", or, if AGENTS.md's \"What one change holds\" says so, add it to %s: push it from its lease (zcp git %s push origin %s); where that change's branch is checked out, zcp git <host> fetch origin && zcp git <host> merge origin/%s, then push; describe that change again; then release the branch (%s)",
		which, b.alias, b.branch, b.branch, ops.LeaseReleaseBranchCall(b.branch, "", false))
}
