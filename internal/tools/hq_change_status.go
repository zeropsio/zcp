package tools

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// What status, zerops_lease status and the release handoff say of a Mate's
// changes: zcp's own ledger and lease records — a change's standing as the
// last pass told it — and, for status, each change's standing as the Mate's
// state names it now, read once (statusHQChanges).

// changeOldAfter is how long a change no lease holds may go untouched in HQ
// before status labels it old. A label only: nothing closes or releases on
// idleness without a person.
const changeOldAfter = 72 * time.Hour

// changeLabels is a change's standing as told, as status and develop's start
// label it: ready or a draft, behind main or conflicting with it.
func changeLabels(t *workflow.HQChangeTold) []string {
	if t == nil {
		return nil
	}
	var labels []string
	if t.Ready != nil {
		if *t.Ready {
			labels = append(labels, "ready")
		} else {
			labels = append(labels, "draft")
		}
	}
	switch {
	case t.Mergeability == hq.MergeConflict:
		labels = append(labels, "conflicts with main")
	case t.Behind:
		labels = append(labels, "behind main")
	}
	return labels
}

// hqLeaseBranch is a lease of an HQ repository: its alias, and the
// repository and branch it holds.
type hqLeaseBranch struct {
	alias, appID, repo, branch string
}

// hqLeaseBranches is every lease zcp holds of a repository on this Mate's HQ
// (address), by alias.
func hqLeaseBranches(stateDir, address string) []hqLeaseBranch {
	leases, _ := ops.LeaseBranches(stateDir)
	var out []hqLeaseBranch
	for _, l := range leases {
		if appID, repo, ok := hq.RepoOfURL(address, l.Repo); ok {
			out = append(out, hqLeaseBranch{alias: l.Alias, appID: appID, repo: repo, branch: l.Branch})
		}
	}
	return out
}

// hqChangesReadBound bounds status's one read of the Mate's state for the
// changes it names: status answers promptly whether HQ does or not.
const hqChangesReadBound = 5 * time.Second

// hqStateNow is the Mate's own state in HQ, read once and bounded; nil when
// this zcp holds no enrollment or HQ did not answer.
func hqStateNow(ctx context.Context, httpClient ops.HTTPDoer) *hq.MateState {
	hqc, enrolled := openHQ(httpClient)
	if !enrolled {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, hqChangesReadBound)
	defer cancel()
	state, err := hqc.Once().Self(ctx)
	if err != nil {
		return nil
	}
	return &state
}

// statusHQChanges is status's Changes section with each change's standing as
// the Mate's state names it now: a merge since the last pass reads merged,
// never open, while the pair backoff holds the pass off. The state is read
// once, inside a Mate, only when the section has a change or a leased branch
// to name — an idle Mate costs HQ nothing — and nothing is written: the pass
// still says a merge or a standing once, with what to do. asLastRead: HQ did
// not answer, and the section is the ledger's, as last told.
func statusHQChanges(ctx context.Context, httpClient ops.HTTPDoer, rt runtime.Info, stateDir, address string, now time.Time) (changes []workflow.HQRepoChanges, asLastRead bool) {
	kept := hqChangesStatus(stateDir, address, now, nil)
	if len(kept) == 0 || !rt.MateEnabled {
		return kept, false
	}
	state := hqStateNow(ctx, httpClient)
	if state == nil {
		return kept, true
	}
	return hqChangesStatus(stateDir, address, now, state), false
}

// hqChangesStatus is status's Changes section: per repository, each change
// the ledger keeps — its standing as told, or as state (nil: unread) names it
// now, the leases on its branch, and an old one's age — and each branch
// leases hold with no change yet, `main` aside.
func hqChangesStatus(stateDir, address string, now time.Time, state *hq.MateState) []workflow.HQRepoChanges {
	changes := hqChanges(stateDir)
	leases := hqLeaseBranches(stateDir, address)
	type repoKey struct{ appID, repo string }
	var keys []repoKey
	add := func(k repoKey) {
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	for _, c := range changes {
		add(repoKey{c.AppID, c.Repo})
	}
	for _, l := range leases {
		if l.branch != hqBase {
			add(repoKey{l.appID, l.repo})
		}
	}
	slices.SortFunc(keys, func(a, b repoKey) int { return cmp.Or(cmp.Compare(a.repo, b.repo), cmp.Compare(a.appID, b.appID)) })
	held := func(k repoKey, branch string) []string {
		var aliases []string
		for _, l := range leases {
			if l.appID == k.appID && l.repo == k.repo && l.branch == branch {
				aliases = append(aliases, l.alias)
			}
		}
		return aliases
	}
	out := make([]workflow.HQRepoChanges, 0, len(keys))
	for _, k := range keys {
		repo := workflow.HQRepoChanges{Repo: k.repo}
		var branches []string
		for _, c := range changesIn(changes, k.appID, k.repo) {
			line, told := changeLineNow(c, state)
			line.Leases = held(k, c.Branch)
			if age := changeAge(told, now); told != nil && len(line.Leases) == 0 && age >= changeOldAfter {
				line.Labels = append(line.Labels, fmt.Sprintf("no lease holds it, untouched in HQ for %d days", int(age/(24*time.Hour))))
			}
			repo.Changes = append(repo.Changes, line)
			branches = append(branches, c.Branch)
		}
		for _, l := range leases {
			if l.appID != k.appID || l.repo != k.repo || l.branch == hqBase || slices.Contains(branches, l.branch) {
				continue
			}
			branches = append(branches, l.branch)
			repo.Branches = append(repo.Branches, workflow.HQBranchLine{Branch: l.branch, Leases: held(k, l.branch)})
		}
		out = append(out, repo)
	}
	return out
}

// changeLineNow is c's status line by what state (nil: unread) names it now —
// still open, its standing now; merged; closed or no longer listed, said so —
// or, unread, unknown or of another application, its standing as told. told
// is its standing as an open change, nil once it is not one.
func changeLineNow(c workflow.HQChange, state *hq.MateState) (workflow.HQChangeLine, *workflow.HQChangeTold) {
	line := workflow.HQChangeLine{Number: c.Number, Branch: c.Branch, Labels: changeLabels(c.Told)}
	if state == nil || state.AppID == nil || *state.AppID != c.AppID {
		return line, c.Told
	}
	outcome, s := outcomeOf(*state, c)
	switch outcome {
	case changeStillOpen:
		now := standingOf(*s)
		line.Labels = changeLabels(&now)
		return line, &now
	case changeMerged:
		line.Labels, line.Merged = nil, true
	case changeClosed:
		line.Labels = []string{"closed without merging"}
	case changeUnlisted:
		line.Labels = []string{"no longer open"}
	case changeUnknown:
		return line, c.Told
	}
	return line, nil
}

// changeAge is how long ago HQ last changed a change as last told, 0 when
// unknown.
func changeAge(t *workflow.HQChangeTold, now time.Time) time.Duration {
	if t == nil || t.UpdatedAt == "" {
		return 0
	}
	at, err := time.Parse(time.RFC3339, t.UpdatedAt)
	if err != nil {
		return 0
	}
	return now.Sub(at)
}

// leaseGroupChanges is what zerops_lease status says of each lease group's
// change among views: per repository and branch, once however many leases
// the group has, the change the ledger keeps on it with its standing as told,
// or that the branch has none yet; "" when no lease of an HQ repository is
// among them.
func leaseGroupChanges(stateDir, address string, views []ops.LeaseView) string {
	leases := hqLeaseBranches(stateDir, address)
	changes := hqChanges(stateDir)
	var groups, seen []string
	for _, v := range views {
		i := slices.IndexFunc(leases, func(l hqLeaseBranch) bool { return l.alias == v.Alias })
		if i < 0 || leases[i].branch == hqBase {
			continue
		}
		l := leases[i]
		group := l.repo + "@" + l.branch
		if slices.Contains(seen, group) {
			continue
		}
		seen = append(seen, group)
		words := "no change yet"
		for _, c := range changesIn(changes, l.appID, l.repo) {
			if c.Branch != l.branch {
				continue
			}
			words = fmt.Sprintf("#%d", c.Number)
			if labels := changeLabels(c.Told); len(labels) > 0 {
				words += " (" + strings.Join(labels, " · ") + ")"
			}
		}
		groups = append(groups, "group "+group+": "+words)
	}
	if len(groups) == 0 {
		return ""
	}
	return "Each branch's change in HQ, as last read: " + strings.Join(groups, "; ") + "."
}

// releaseOpenChangesWords is what the release handoff says of the open
// changes the ledger keeps, per repository: a release takes main as it is,
// so none of them is in it; "" for none.
func releaseOpenChangesWords(stateDir string) string {
	byRepo := map[string][]int{}
	var repos []string
	for _, c := range hqChanges(stateDir) {
		if _, ok := byRepo[c.Repo]; !ok {
			repos = append(repos, c.Repo)
		}
		byRepo[c.Repo] = append(byRepo[c.Repo], c.Number)
	}
	if len(repos) == 0 {
		return ""
	}
	slices.Sort(repos)
	named := make([]string, 0, len(repos))
	for _, repo := range repos {
		numbers := byRepo[repo]
		slices.Sort(numbers)
		named = append(named, repo+" "+changeNumbersWords(numbers))
	}
	return "A release takes main as it is, so the open changes " + strings.Join(named, ", and ") +
		" are not in it: the person merges them first, or releases main without them."
}
