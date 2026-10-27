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
// name each such branch with the open-change call.

// unproposedBranch is a branch a lease of this Mate's HQ repository holds that
// no change proposes, by the lease holding it.
type unproposedBranch struct {
	alias, branch string
}

// unproposedLeaseBranches names, inside a Mate, each branch zcp's leases of
// this Mate's HQ repositories hold — never main, never a mate/ branch (an
// open change's own) — that the change ledger keeps no change on: one per
// repository and branch, by the first lease holding it. pools limits it to
// leases of those pools (nil: every lease). It reads zcp's own records only.
func unproposedLeaseBranches(stateDir string, rt runtime.Info, pools []string) []unproposedBranch {
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
		if slices.ContainsFunc(changesIn(changes, appID, repo), func(c workflow.HQChange) bool { return c.Branch == l.Branch }) {
			continue
		}
		out = append(out, unproposedBranch{alias: l.Alias, branch: l.Branch})
	}
	slices.SortFunc(out, func(a, b unproposedBranch) int { return strings.Compare(a.branch+a.alias, b.branch+b.alias) })
	return out
}

// unproposedNote is the sentence naming each unproposed branch with the
// open-change call that proposes it, "" for none.
func unproposedNote(branches []unproposedBranch) string {
	if len(branches) == 0 {
		return ""
	}
	calls := make([]string, 0, len(branches))
	for _, b := range branches {
		calls = append(calls, fmt.Sprintf("%q (lease %s): zerops_workflow action=\"open-change\" service=%q title=\"<what the work does>\"", b.branch, b.alias, b.alias))
	}
	return "No change in HQ proposes the work on " + strings.Join(calls, "; ") +
		" — open it once the work is ready for the person to review; it is how the work reaches main."
}
