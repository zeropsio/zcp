package tools

import (
	"cmp"
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// What status, zerops_lease status and release, and the release handoff say
// of a Mate's changes: zcp's own ledger and lease records — a change's
// standing as the last pass told it — and, for status, lease status, a lease
// release and the release handoff, each change's standing as the Mate's state
// names it now, read once (statusHQChanges, leaseGroupChanges,
// leaseReleaseChanges, releaseOpenChangesWords), with a change merged on a
// leased branch named so.

// changeOldAfter is how long a change no lease holds may go untouched in HQ
// before status labels it old. A label only: nothing closes or releases on
// idleness without a person.
const changeOldAfter = 72 * time.Hour

// changeLabels is a change's standing as told, as status and develop's start
// label it: ready means described, with HQ's mergeability named separately
// so it cannot imply permission to merge before HQ has judged it.
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
		labels = append(labels, "not mergeable: conflicts with main")
	case t.Mergeability == "empty":
		labels = append(labels, "not mergeable: no changes against main")
	case t.Mergeability == "already_merged":
		labels = append(labels, "not mergeable: already merged into main")
	case t.Mergeability == "unrelated":
		labels = append(labels, "not mergeable: unrelated to main")
	case t.Mergeability != hq.MergeClean && t.Ready != nil && *t.Ready:
		labels = append(labels, "checking mergeability")
	}
	if t.Behind && t.Mergeability != hq.MergeConflict {
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

// hqChangesReadBound bounds status's and lease status's one read of the
// Mate's state for the changes they name: each answers promptly whether HQ
// does or not.
const hqChangesReadBound = 5 * time.Second

// hqStateNow is the Mate's own state in HQ, read once and bounded, from the
// HQ the container rt presents itself to (hqIdentity: its environment's
// origin, else the one the installed Mate declares); nil when the container
// cannot address an HQ or HQ did not answer.
func hqStateNow(ctx context.Context, httpClient ops.HTTPDoer, rt runtime.Info) *hq.MateState {
	hqc, linked := openHQ(httpClient, rt)
	if !linked {
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

// statusHQChanges reads the Mate's state once for changes and unresolved app
// findings, even without open changes, unless the pairs already record an
// HQ older than repositories. Outside a Mate or while HQ cannot answer,
// only the ledger's changes remain, labelled as last read.
func statusHQChanges(ctx context.Context, httpClient ops.HTTPDoer, rt runtime.Info, stateDir, address string, now time.Time, includeProblems bool) (changes []workflow.HQRepoChanges, asLastRead bool, problems []hq.Problem) {
	kept := hqChangesStatus(stateDir, address, now, nil)
	// Environment observation consumes only changes and retains its no-read
	// behavior when empty. Lifecycle status also asks for app findings.
	if len(kept) == 0 && !includeProblems {
		return kept, false, nil
	}
	if len(kept) == 0 {
		// The origin lookup has already recorded an old HQ's unknown mode.
		// Any pair naming a mode (or still unrecorded) keeps status fresh,
		// whether or not that HQ includes the optional problems field.
		metas, _ := workflow.ListServiceMetas(stateDir)
		olderHQ := false
		for _, m := range metas {
			if !hqPairWired(m) {
				continue
			}
			if m.HQ.Origin == nil || m.HQ.Origin.Mode != "" {
				olderHQ = false
				break
			}
			olderHQ = true
		}
		if olderHQ {
			return kept, false, nil
		}
	}
	if !rt.MateEnabled {
		return kept, len(kept) > 0, nil
	}
	state := hqStateNow(ctx, httpClient, rt)
	if state == nil {
		return kept, len(kept) > 0, nil
	}
	return hqChangesStatus(stateDir, address, now, state), false, hqStateProblems(*state)
}

// hqStateProblems forwards every HQ finding from the existing self read.
// The same finding can occur at app, change and validation level; render an
// identical finding once, without choosing a verdict or dropping distinct ones.
func hqStateProblems(state hq.MateState) []hq.Problem {
	var problems []hq.Problem
	add := func(findings []hq.Problem) {
		for _, finding := range findings {
			if !slices.ContainsFunc(problems, func(known hq.Problem) bool { return reflect.DeepEqual(known, finding) }) {
				problems = append(problems, finding)
			}
		}
	}
	add(state.Problems)
	for _, change := range state.Changes {
		add(change.Problems)
		if change.RecipeValidation != nil {
			add(change.RecipeValidation.Problems)
		}
	}
	return problems
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
			if number := mergedOn(state, k.appID, k.repo, l.branch); number > 0 {
				repo.Changes = append(repo.Changes, workflow.HQChangeLine{Number: number, Branch: l.branch, Leases: held(k, l.branch), Merged: true})
				continue
			}
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

// mergedOn is the number of the change merged on branch of appID's repo, as
// state (nil: unread) names it — the newest of the Mate's changes on that
// branch, when merged; 0 for none, or a newer one not merged. A lease on that
// branch holds merged work: that is said, never "no change yet", which
// invites a new change on it.
func mergedOn(state *hq.MateState, appID, repo, branch string) int {
	if state == nil || state.AppID == nil || *state.AppID != appID {
		return 0
	}
	newest := -1
	for i, c := range state.Changes {
		if c.Repo == repo && c.Branch == branch && (newest < 0 || c.Number > state.Changes[newest].Number) {
			newest = i
		}
	}
	if newest < 0 || state.Changes[newest].State != hq.ChangeMerged {
		return 0
	}
	return state.Changes[newest].Number
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
// the group has, the change the ledger keeps on it with its standing, a change
// merged on it — release its leases, or start new work from main — or that
// the branch has none yet; "" when no lease of an HQ repository is among
// them. read (nil: none) reads the Mate's state once, only when there is a
// group to name; its standing now is said then, the ledger's as last read
// when it answers nil.
func leaseGroupChanges(stateDir, address string, views []ops.LeaseView, read func() *hq.MateState) string {
	leases := hqLeaseBranches(stateDir, address)
	var groups []hqLeaseBranch
	for _, v := range views {
		i := slices.IndexFunc(leases, func(l hqLeaseBranch) bool { return l.alias == v.Alias })
		if i < 0 || leases[i].branch == hqBase {
			continue
		}
		l := leases[i]
		if !slices.ContainsFunc(groups, func(g hqLeaseBranch) bool { return g.appID == l.appID && g.repo == l.repo && g.branch == l.branch }) {
			groups = append(groups, l)
		}
	}
	if len(groups) == 0 {
		return ""
	}
	var state *hq.MateState
	if read != nil {
		state = read()
	}
	changes := hqChanges(stateDir)
	named := make([]string, 0, len(groups))
	for _, g := range groups {
		named = append(named, "group "+g.repo+"@"+g.branch+": "+groupChangeWords(changes, leases, state, g))
	}
	prefix := "Each branch's change in HQ: "
	if state == nil {
		prefix = "Each branch's change in HQ, as last read: "
	}
	return prefix + strings.Join(named, "; ") + "."
}

// groupChangeWords is what lease status says of the change on group g's
// branch: the ledger's, with its standing as state (nil: unread) names it or
// as told — HQ's own merge commit at its head said as the pass says it, with
// the fast-forward in each of the group's leases; one merged there, by the
// ledger or by state; or none yet. leases are every HQ lease zcp holds.
func groupChangeWords(changes []workflow.HQChange, leases []hqLeaseBranch, state *hq.MateState, g hqLeaseBranch) string {
	words, merged, open := groupChange(changes, state, g)
	if merged == 0 {
		if open != nil && open.Told != nil && open.Told.CoreHead != "" {
			words += " — " + corePushWords(open.Number, open.Told.CoreLanded, g.branch, repoRoute{}.inLease(g.alias), groupAliases(leases, g), false)
		}
		return words
	}
	which := "this lease"
	if len(groupAliases(leases, g)) > 1 {
		which = "these leases"
	}
	return fmt.Sprintf("change #%d merged — release %s (`%s`), or start new work from %s", merged, which, ops.LeaseReleaseBranchCall(g.branch, "", false), hqBase)
}

// groupChange is the change on group g's branch: words for the ledger's open
// one, with its standing as state (nil: unread) names it or as told, or "no
// change yet"; merged is the number of one merged there, by the ledger or by
// state (0: none), which words then do not name. open is the ledger's open
// change words name, its Told the standing they name it with; nil for none.
func groupChange(changes []workflow.HQChange, state *hq.MateState, g hqLeaseBranch) (words string, merged int, open *workflow.HQChange) {
	words, merged = "no change yet", mergedOn(state, g.appID, g.repo, g.branch)
	for _, c := range changesIn(changes, g.appID, g.repo) {
		if c.Branch != g.branch {
			continue
		}
		line, told := changeLineNow(c, state)
		if line.Merged {
			merged, open = c.Number, nil
			continue
		}
		merged, words = 0, fmt.Sprintf("#%d", c.Number)
		if len(line.Labels) > 0 {
			words += " (" + strings.Join(line.Labels, " · ") + ")"
		}
		named := c
		named.Told = told
		open = &named
	}
	return words, merged, open
}

// groupAliases are the aliases of group g's leases among leases.
func groupAliases(leases []hqLeaseBranch, g hqLeaseBranch) []string {
	var out []string
	for _, l := range leases {
		if l.appID == g.appID && l.repo == g.repo && l.branch == g.branch {
			out = append(out, l.alias)
		}
	}
	return out
}

// leaseReleaseChanges is what a lease release's result says of the change on
// each released lease's branch (released: the release's views, whose records
// are gone), as lease status says it (leaseGroupChanges): once per repository
// and branch, read through read (nil: none) once, only when a lease of an HQ
// repository off main is among them — the ledger's as last read when it
// answers nil. A merged change names the branch's leases still held, with the
// branch release, and the repository's stages, which track main, with the
// call that puts main on one — the merged note may have reached the agent
// before the last merge only; "" when no released lease is of an HQ
// repository.
func leaseReleaseChanges(stateDir, address string, released []ops.LeaseView, read func() *hq.MateState) string {
	var groups []hqLeaseBranch
	for _, v := range released {
		appID, repo, ok := hq.RepoOfURL(address, v.Repo)
		branch := cmp.Or(v.Branch, hqBase)
		if !ok || branch == hqBase {
			continue
		}
		g := hqLeaseBranch{appID: appID, repo: repo, branch: branch}
		if !slices.Contains(groups, g) {
			groups = append(groups, g)
		}
	}
	if len(groups) == 0 {
		return ""
	}
	var state *hq.MateState
	if read != nil {
		state = read()
	}
	changes, leases := hqChanges(stateDir), hqLeaseBranches(stateDir, address)
	named := make([]string, 0, len(groups))
	for _, g := range groups {
		words, merged, _ := groupChange(changes, state, g)
		if merged > 0 {
			words = fmt.Sprintf("change #%d merged", merged)
			if held := groupAliases(leases, g); len(held) > 0 {
				words += fmt.Sprintf(" — the branch's leases still held (%s) hold merged work: release them (`%s`), or start new work from %s",
					strings.Join(held, ", "), ops.LeaseReleaseBranchCall(g.branch, "", false), hqBase)
			}
			if stages := stagesTrackMain(stateDir, g.appID, g.repo); stages != "" {
				words += " — " + stages
			}
		}
		named = append(named, "group "+g.repo+"@"+g.branch+": "+words)
	}
	prefix := "The released branch's change in HQ: "
	if len(groups) > 1 {
		prefix = "Each released branch's change in HQ: "
	}
	if state == nil {
		prefix = strings.TrimSuffix(prefix, ": ") + ", as last read: "
	}
	return prefix + strings.Join(named, "; ") + "."
}

// releaseOpenChangesWords is what the release handoff says of the open
// changes the ledger keeps, per repository: a release takes main as it is,
// so none of them is in it; "" for none. Inside a Mate, with a change to
// name, the Mate's state is read once, bounded, and a change it names merged,
// closed or no longer listed is left out — a merge since the last pass is
// never named as left out. The read writes nothing: the pass still says the
// merge once, with what to do. Unread — outside a Mate, or HQ not answering —
// the ledger's are named as last read.
func releaseOpenChangesWords(ctx context.Context, httpClient ops.HTTPDoer, rt runtime.Info, stateDir string) string {
	kept := hqChanges(stateDir)
	if len(kept) == 0 {
		return ""
	}
	var state *hq.MateState
	if rt.MateEnabled {
		state = hqStateNow(ctx, httpClient, rt)
	}
	byRepo := map[string][]int{}
	var repos []string
	for _, c := range kept {
		if state != nil && state.AppID != nil && *state.AppID == c.AppID {
			if outcome, _ := outcomeOf(*state, c); outcome != changeStillOpen && outcome != changeUnknown {
				continue
			}
		}
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
	asLastRead := ""
	if state == nil {
		asLastRead = " (as last read)"
	}
	return "A release takes main as it is, so the open changes " + strings.Join(named, ", and ") + asLastRead +
		" are not in it: the person merges them first, or releases main without them."
}
