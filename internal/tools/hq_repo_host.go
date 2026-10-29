package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A Mate's agent runs every git command for a repository of its HQ in a dev
// service, never on the sshfs mount (docs/spec-mate.md §6.8): `zcp git
// <host>` in the dev service of the repository's pair, where zcp answers git
// for HQ and GitHub, and over ssh in a lease where a dev pool holds the
// repository's checkouts, as a lease's git always ran. The words zcp gives
// about a change name that place, read here from zcp's own records alone.

// repoRoute is where the agent runs git for one repository of this Mate's HQ
// (repoDevHost). The zero route is a repository no pair here is wired to.
type repoRoute struct {
	// host is the dev hostname of a plain pair wired to the repository.
	host string
	// pool is the dev pool of a pair keyed by one, when no plain pair is
	// wired to the repository: its checkouts are its leases.
	pool string
	// lease is the lease the words are about, "" for none known.
	lease string
}

// repoDevHost is where the agent runs git for appID's repo, by the records'
// repository rather than a hostname — a recipe can name a pair's repository
// otherwise: in the dev service of a plain pair wired to it, the first by
// hostname; in a lease of its dev pool when only pairs keyed by dev pools
// are — a pool's hostname reaches a random container, never a checkout; and
// with no pair here, in the dev service whose checkout the agent fetches it
// into, which zcp cannot name. An empty appID matches any application.
func repoDevHost(stateDir, appID, repo string) repoRoute {
	metas, _ := workflow.ListServiceMetas(stateDir)
	var route repoRoute
	for _, m := range metas {
		if !hqPairWired(m) || m.HQ.Repo != repo || (appID != "" && m.HQ.AppID != appID) {
			continue
		}
		if m.DevIsPool() {
			if route.pool == "" || m.Hostname < route.pool {
				route.pool = m.Hostname
			}
			continue
		}
		if route.host == "" || m.Hostname < route.host {
			route.host = m.Hostname
		}
	}
	if route.host != "" {
		route.pool = ""
	}
	return route
}

// changeRoute is where git runs for a change call's target: the lease it
// named, else its repository's place (repoDevHost).
func changeRoute(stateDir string, target changeTarget) repoRoute {
	route := repoDevHost(stateDir, target.AppID, target.Repo)
	if target.Lease == "" {
		return route
	}
	pool := route.pool
	if leases, err := ops.LeaseBranches(stateDir); err == nil {
		for _, l := range leases {
			if l.Alias == target.Lease {
				pool = l.Pool
			}
		}
	}
	return repoRoute{lease: target.Lease, pool: pool}
}

// inLease is r in the lease alias, which holds the branch the words are
// about.
func (r repoRoute) inLease(alias string) repoRoute { return repoRoute{lease: alias, pool: r.pool} }

// leased reports whether r's git runs in a lease, over ssh.
func (r repoRoute) leased() bool { return r.lease != "" || r.pool != "" }

// git is cmds — each a git command's arguments — run one after another
// where r runs git, as one command line in backticks that stops at the first
// that fails: `zcp git <host> …` for each in a dev service, one ssh to a
// lease's alias running them in its checkout.
func (r repoRoute) git(cmds ...string) string {
	if r.leased() {
		alias := r.lease
		if alias == "" {
			alias = "<alias>"
		}
		return fmt.Sprintf("`ssh %s \"cd %s && git %s\"`", alias, topology.CodeMountRoot, strings.Join(cmds, " && git "))
	}
	host := r.host
	if host == "" {
		host = "<host>"
	}
	prefix := "zcp git " + host + " "
	return "`" + prefix + strings.Join(cmds, " && "+prefix) + "`"
}

// where names the place r runs git.
func (r repoRoute) where() string {
	switch {
	case r.lease != "":
		return "in its lease " + r.lease
	case r.pool != "":
		return "in a lease of the dev pool " + r.pool
	case r.host != "":
		return "in its dev service " + r.host
	}
	return "in the dev service whose checkout you fetch it into"
}

// checkedOut names where branch is checked out for a fast-forward or a merge
// into it: in a dev service its checkout, or the worktree the dev service
// holds for it below /tmp/wt- (`-C` after the host); in a lease, the lease.
func (r repoRoute) checkedOut(branch string) string {
	if r.leased() {
		return r.where()
	}
	return fmt.Sprintf("where it is checked out %s (or in its /tmp/wt-%s worktree there, with `-C /tmp/wt-%s` after the host)", r.where(), branch, branch)
}

// take is how branch, already on origin, is put in hand: fetched and checked
// out in a dev service; in a dev pool, a lease acquired on it checks it out.
func (r repoRoute) take(branch string) string {
	if r.pool != "" {
		return leaseAcquire(r.pool, branch)
	}
	return r.git("fetch origin "+branch, "checkout "+branch)
}

// cut is how branch, which origin does not hold yet, is cut from origin's
// main: in a dev pool, a lease acquired on a branch that does not exist cuts
// it from main.
func (r repoRoute) cut(branch string) string {
	if r.pool != "" {
		return leaseAcquire(r.pool, branch)
	}
	return r.git("fetch origin "+hqBase, "checkout -b "+branch+" origin/"+hqBase)
}

// fromMain is how new work's branch is cut from an up-to-date main: main
// fast-forwarded first in a dev service; in a dev pool, by a lease acquired
// on it.
func (r repoRoute) fromMain(branch string) string {
	if r.pool != "" {
		return leaseAcquire(r.pool, branch)
	}
	return r.git("checkout "+hqBase, "fetch origin", "merge --ff-only origin/"+hqBase, "checkout -b "+branch)
}

// leaseAcquire is the call that checks branch out in a lease of pool, cut
// from main when origin does not hold it yet.
func leaseAcquire(pool, branch string) string {
	return fmt.Sprintf("`zerops_lease action=\"acquire\" pool=%q branch=%q`", pool, branch)
}
