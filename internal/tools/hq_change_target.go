package tools

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// Which change a change call means. An open change is (application,
// repository, branch): every call names its change — by branch, or by a lease
// alias or a pair that implies it — and zcp infers one only where exactly one
// candidate exists, never "the newest", and never a change a session opened
// last: subagents share one zcp, and every conversation the container's
// leases (docs/spec-mate.md §10.10).

// The verbs a change call is for, as its words name them.
const (
	changeVerbOpen     = "open"
	changeVerbDescribe = "describe"
	changeVerbClose    = "close"
)

// changeAsk is what a change call names: a pair or a lease alias (service),
// a branch, and what it is to do.
type changeAsk struct {
	Service, Branch, Verb string
}

// changeTarget is the repository a change call is about, and the branch it
// names or implies.
type changeTarget struct {
	AppID, Repo string
	// Branch is the branch the call named, or the lease's it named; "" for
	// none.
	Branch string
	// Lease is the lease alias the call named, "" for none.
	Lease string
	// Pair is the pair the call named, else the first wired pair of the
	// repository by hostname; nil when only leases name it.
	Pair *workflow.ServiceMeta
}

// resolveChangeTarget is the repository, and the branch, a change call
// means, from zcp's own records alone — before anything asks HQ:
//
//   - service a lease alias: its record's repository, which must be on this
//     Mate's HQ (address), and its branch, never `main`;
//   - service a pair: its repository;
//   - neither: the one repository of the Mate's wired pairs and HQ leases —
//     pairs sharing a repository count once — else the one whose leases hold
//     the branch named; otherwise a refusal naming a pair or lease of each.
//
// open-change needs a branch, named or a lease's.
func resolveChangeTarget(stateDir, address string, ask changeAsk) (changeTarget, error) {
	named := strings.TrimSpace(ask.Branch)
	if named != "" {
		branch, refusal := changeBranchOf(named)
		if refusal != nil {
			return changeTarget{}, refusal
		}
		named = branch
	}
	leases, err := ops.LeaseBranches(stateDir)
	if err != nil {
		return changeTarget{}, fmt.Errorf("read this Mate's leases: %w", err)
	}
	var target changeTarget
	switch service := strings.TrimSpace(ask.Service); {
	case service != "":
		target, err = changeTargetOfService(stateDir, address, service, named, ask.Verb, leases)
	default:
		target, err = changeTargetOfRepository(stateDir, address, named, ask.Verb, leases)
	}
	if err != nil {
		return changeTarget{}, err
	}
	if target.Branch == "" {
		target.Branch = named
	}
	if ask.Verb == changeVerbOpen && target.Branch == "" {
		return changeTarget{}, platform.NewPlatformError(platform.ErrInvalidParameter,
			`open-change opens the change of a branch of your own: pass branch="<branch>", or service="<lease alias>" for the branch a lease holds.`,
			"Cut a branch from main, commit, push it, and name it here.")
	}
	return target, nil
}

// changeTargetOfService is the repository a pair or lease alias names.
func changeTargetOfService(stateDir, address, service, named, verb string, leases []ops.LeaseTargetInfo) (changeTarget, error) {
	if i := slices.IndexFunc(leases, func(l ops.LeaseTargetInfo) bool { return l.Alias == service }); i >= 0 {
		lease := leases[i]
		appID, repo, ok := hq.RepoOfURL(address, lease.Repo)
		switch {
		case !ok:
			return changeTarget{}, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("The lease %s holds %s, which is not this Mate's HQ: it has no change to %s here.", service, topology.RedactRepoURLCredentials(cmp.Or(lease.Repo, "a repository zcp does not know")), verb),
				"Name a lease or pair of a repository in this Mate's HQ.")
		case lease.Branch == hqBase:
			return changeTarget{}, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("The lease %s holds %q, and nothing is opened from %q: it moves only by a merge.", service, hqBase, hqBase),
				"Acquire a lease of a branch of your own (zerops_lease action=acquire branch=\"<branch>\"), push the work there, and name that lease.")
		case named != "" && named != lease.Branch:
			return changeTarget{}, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("The lease %s holds the branch %q, not %q.", service, lease.Branch, named),
				fmt.Sprintf("Name the lease alone (service=%q), or the branch alone.", service))
		}
		return changeTarget{AppID: appID, Repo: repo, Branch: lease.Branch, Lease: service, Pair: firstPairOf(stateDir, appID, repo)}, nil
	}
	meta, err := workflow.FindServiceMeta(stateDir, service)
	if err != nil {
		return changeTarget{}, fmt.Errorf("read the pair %q: %w", service, err)
	}
	if meta == nil {
		return changeTarget{}, platform.NewPlatformError(platform.ErrServiceNotFound,
			fmt.Sprintf("No pair or lease of this Mate is named %q.", service),
			fmt.Sprintf("Pass the dev or the stage hostname of a pair, or a lease's alias (zerops_lease action=status lists them), whose change this %ss.", verb))
	}
	if !hqPairWired(meta) {
		return changeTarget{}, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("%s has no repository in HQ yet, so it has no change to %s.", meta.Hostname, verb),
			noRepositoryYet)
	}
	return changeTarget{AppID: meta.HQ.AppID, Repo: meta.HQ.Repo, Pair: meta}, nil
}

// changeRepository is one HQ repository the Mate works in, with the pairs and
// leases that name it.
type changeRepository struct {
	appID, repo string
	pairs       []*workflow.ServiceMeta
	leases      []ops.LeaseTargetInfo
}

// changeRepositories is every HQ repository the Mate's wired pairs and HQ
// leases name, each once, by name.
func changeRepositories(stateDir, address string, leases []ops.LeaseTargetInfo) []*changeRepository {
	byKey := map[string]*changeRepository{}
	of := func(appID, repo string) *changeRepository {
		key := appID + "/" + repo
		if byKey[key] == nil {
			byKey[key] = &changeRepository{appID: appID, repo: repo}
		}
		return byKey[key]
	}
	metas, _ := workflow.ListServiceMetas(stateDir)
	for _, m := range metas {
		if hqPairWired(m) {
			r := of(m.HQ.AppID, m.HQ.Repo)
			r.pairs = append(r.pairs, m)
		}
	}
	for _, l := range leases {
		if appID, repo, ok := hq.RepoOfURL(address, l.Repo); ok {
			r := of(appID, repo)
			r.leases = append(r.leases, l)
		}
	}
	out := make([]*changeRepository, 0, len(byKey))
	for _, r := range byKey {
		slices.SortFunc(r.pairs, func(a, b *workflow.ServiceMeta) int { return cmp.Compare(a.Hostname, b.Hostname) })
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b *changeRepository) int {
		return cmp.Or(cmp.Compare(a.repo, b.repo), cmp.Compare(a.appID, b.appID))
	})
	return out
}

// target is the repository as a change target.
func (r *changeRepository) target() changeTarget {
	t := changeTarget{AppID: r.appID, Repo: r.repo}
	if len(r.pairs) > 0 {
		t.Pair = r.pairs[0]
	}
	return t
}

// changeTargetOfRepository is the one repository a call naming no service
// can mean.
func changeTargetOfRepository(stateDir, address, named, verb string, leases []ops.LeaseTargetInfo) (changeTarget, error) {
	repos := changeRepositories(stateDir, address, leases)
	switch len(repos) {
	case 0:
		return changeTarget{}, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			"No pair of this Mate has its repository in HQ yet, so there is no change to "+verb+".",
			noRepositoryYet)
	case 1:
		return repos[0].target(), nil
	}
	if named != "" {
		var holding []*changeRepository
		for _, r := range repos {
			if slices.ContainsFunc(r.leases, func(l ops.LeaseTargetInfo) bool { return l.Branch == named }) {
				holding = append(holding, r)
			}
		}
		if len(holding) == 1 {
			return holding[0].target(), nil
		}
	}
	names := make([]string, 0, len(repos))
	for _, r := range repos {
		service := ""
		switch {
		case len(r.pairs) > 0:
			service = r.pairs[0].Hostname
		case len(r.leases) > 0:
			service = r.leases[0].Alias
		}
		names = append(names, fmt.Sprintf("service=%q (repository %q)", service, r.repo))
	}
	return changeTarget{}, platform.NewPlatformError(platform.ErrServiceRequired,
		fmt.Sprintf("This Mate works in several repositories in HQ: say which repository's change this %ss.", verb),
		"Pass service= a lease or pair of the repository: "+strings.Join(names, " or ")+".")
}

// firstPairOf is the first wired pair of appID's repo by hostname, nil for
// none.
func firstPairOf(stateDir, appID, repo string) *workflow.ServiceMeta {
	metas, _ := workflow.ListServiceMetas(stateDir)
	var first *workflow.ServiceMeta
	for _, m := range metas {
		if hqPairWired(m) && m.HQ.AppID == appID && m.HQ.Repo == repo && (first == nil || m.Hostname < first.Hostname) {
			first = m
		}
	}
	return first
}

// changePick is the change a call means within its repository.
type changePick struct {
	Number int
	Branch string
	Title  string
	// InState: the Mate's own state names it open; else only the ledger
	// does (the state was not read).
	InState bool
}

// pick is the open change the call means in t's repository: the one on t's
// branch, or — no branch named, for a describe or a close — the only open one
// there. state is the Mate's own state, nil when it could not be read; ledger
// is zcp's record of its changes, read instead. Several open and no branch is
// a refusal listing each; none is the zero pick.
func (t changeTarget) pick(state *hq.MateState, ledger []workflow.HQChange, verb string) (changePick, error) {
	candidates := t.openCandidates(state, ledger)
	if t.Branch != "" {
		for _, c := range candidates {
			if c.Branch == t.Branch {
				return c, nil
			}
		}
		return changePick{}, nil
	}
	switch len(candidates) {
	case 0:
		return changePick{}, nil
	case 1:
		return candidates[0], nil
	}
	named := make([]string, 0, len(candidates))
	for _, c := range candidates {
		name := fmt.Sprintf("branch=%q (#%d", c.Branch, c.Number)
		if c.Title != "" {
			name += fmt.Sprintf(" %q", c.Title)
		}
		named = append(named, name+")")
	}
	return changePick{}, platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("This Mate has %d open changes in %q: say which one this %ss.", len(candidates), t.Repo, verb),
		"Pass "+strings.Join(named, " or ")+".")
}

// openCandidates is the repository's open changes, by number: as the Mate's
// own state names them when it was read — each one's branch HQ's, or the
// ledger's for an HQ older than branches in the state — else as the ledger
// keeps them.
func (t changeTarget) openCandidates(state *hq.MateState, ledger []workflow.HQChange) []changePick {
	kept := func(number int) *workflow.HQChange {
		for i := range ledger {
			if c := &ledger[i]; c.AppID == t.AppID && c.Repo == t.Repo && c.Number == number {
				return c
			}
		}
		return nil
	}
	var out []changePick
	if state != nil {
		if state.AppID == nil || *state.AppID != t.AppID {
			return nil
		}
		for _, c := range state.Changes {
			if c.Repo != t.Repo || c.State != hq.ChangeOpen {
				continue
			}
			pick := changePick{Number: c.Number, Branch: strings.TrimPrefix(c.Branch, "refs/heads/"), InState: true}
			if c.Title != nil {
				pick.Title = *c.Title
			}
			if k := kept(c.Number); k != nil {
				pick.Branch = cmp.Or(pick.Branch, k.Branch)
				pick.Title = cmp.Or(pick.Title, k.Title)
			}
			out = append(out, pick)
		}
	} else {
		for _, c := range ledger {
			if c.AppID == t.AppID && c.Repo == t.Repo {
				out = append(out, changePick{Number: c.Number, Branch: c.Branch, Title: c.Title})
			}
		}
	}
	slices.SortFunc(out, func(a, b changePick) int { return cmp.Compare(a.Number, b.Number) })
	return out
}
