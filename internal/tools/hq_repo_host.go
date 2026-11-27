package tools

import (
	"fmt"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A Mate's agent runs every git command for a repository of its HQ in a dev
// service, never on the sshfs mount (docs/spec-mate.md §6.8): `zcp git
// <host>` in the dev service of the repository's pair, where zcp answers git
// for HQ and, to read, for GitHub, and `zcp git <alias>` in a lease where a
// dev pool holds the repository's checkouts. The words zcp gives
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
	// copy is the dev service's checkout of a plain pair as this call reads
	// it (readingCopy); nil when the call reads none.
	copy *copyPlan
	// own is the branches whose work the words carry as the Mate's own
	// (carrying).
	own []workflow.OwnBranch
}

// copyReader reads the checkout of repo in host, a plain pair's dev service,
// for this call (classicCopyRead).
type copyReader func(host, repo string) workflow.WhereCopy

// copyPlan is a plain pair's dev-service checkout as one call reads it: read
// the first time words would switch its branch, and that read answers every
// word of the call after it.
type copyPlan struct {
	read func() workflow.WhereCopy
	got  *workflow.WhereCopy
}

func (p *copyPlan) plan() *workflow.WhereCopy {
	if p.got == nil {
		read := p.read()
		p.got = &read
	}
	return p.got
}

// readingCopy is r with its dev service's checkout of repo read by read, once,
// when words first would switch its branch (mayMove). A lease, a dev pool and
// a repository no pair here is wired to have no such checkout.
func (r repoRoute) readingCopy(read copyReader, repo string) repoRoute {
	if read == nil || r.host == "" || r.leased() {
		return r
	}
	host := r.host
	r.copy = &copyPlan{read: func() workflow.WhereCopy { return read(host, repo) }}
	return r
}

// carrying is r for words that carry own's work as the Mate's own when they
// move its checkout (workflow.OwnBranch), over the same read of it.
func (r repoRoute) carrying(own ...workflow.OwnBranch) repoRoute {
	r.own = append(slices.Clip(r.own), own...)
	return r
}

// mayMove is whether words may give the git that switches r's checkout to
// branch, and the planner's words when they may not. A plain pair's dev
// service checkout is shared by every conversation of the Mate: its branch
// moves only when workflow.MissingEditCopyStep answers checkout over this
// call's read of it (readingCopy) and the work the words carry (carrying), a
// checkout this call did not read being unknown. A lease is the caller's
// own, and a repository no pair here is wired to goes into a checkout the
// agent fetches it into: their git moves no one else's work.
func (r repoRoute) mayMove(branch string) (string, bool) {
	if r.host == "" || r.leased() {
		return "", true
	}
	classic := &workflow.WhereCopy{Name: r.host, WorkUnread: "checkout was not read"}
	if r.copy != nil {
		classic = r.copy.plan()
	}
	step := workflow.MissingEditCopyStep(branch, classic, r.own...)
	if step.Action == "checkout "+branch {
		return "", true
	}
	return step.Words(), false
}

// moved is cmds, git that switches r's checkout to branch, run where r runs
// git (git) — or the planner's words when that checkout may not move
// (mayMove).
func (r repoRoute) moved(branch string, cmds ...string) string {
	if words, ok := r.mayMove(branch); !ok {
		return words
	}
	return r.git(cmds...)
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
// named, else place, its repository's (repoDevHost).
func changeRoute(stateDir string, target changeTarget, place repoRoute) repoRoute {
	if target.Lease == "" {
		return place
	}
	pool := place.pool
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

// leased reports whether r's git runs in a lease.
func (r repoRoute) leased() bool { return r.lease != "" || r.pool != "" }

// git is cmds — each a git command's arguments — run one after another
// where r runs git, as one command line in backticks that stops at the first
// that fails: `zcp git <host> …` for each in a dev service, `zcp git <alias> …`
// in a lease. Always through zcp, which answers HQ with the Mate's credential:
// a lease's own git over ssh holds none, and its blobless checkout fetches
// old file contents from HQ on demand.
func (r repoRoute) git(cmds ...string) string {
	at := r.host
	switch {
	case r.leased():
		at = r.lease
		if at == "" {
			at = "<alias>"
		}
	case at == "":
		at = "<host>"
	}
	prefix := "zcp git " + at + " "
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
// out in a dev service whose checkout may move (moved); in a dev pool, a
// lease acquired on it checks it out.
func (r repoRoute) take(branch string) string {
	if r.pool != "" {
		return leaseAcquire(r.pool, branch)
	}
	return r.moved(branch, "fetch origin "+branch, "checkout "+branch)
}

// cut is how branch, which origin does not hold yet, is cut from origin's
// main, in a dev service whose checkout may move (moved): in a dev pool, a
// lease acquired on a branch that does not exist cuts it from main.
func (r repoRoute) cut(branch string) string {
	if r.pool != "" {
		return leaseAcquire(r.pool, branch)
	}
	return r.moved(branch, "fetch origin "+hqBase, "checkout -b "+branch+" origin/"+hqBase)
}

// fromMain is how new work's branch is cut from an up-to-date main: main
// fast-forwarded first in a dev service whose checkout may move (moved); in
// a dev pool, by a lease acquired on it.
func (r repoRoute) fromMain(branch string) string {
	if r.pool != "" {
		return leaseAcquire(r.pool, branch)
	}
	return r.moved(branch, "checkout "+hqBase, "fetch origin", "merge --ff-only origin/"+hqBase, "checkout -b "+branch)
}

// plainGit is cmds — each a git command's arguments — as a project without a
// Mate runs them, on its mounted checkout, reading HQ through `zcp hq git`
// (owner decision Q1, docs/spec-mate.md §6.7): `zcp git` has no Mate identity
// to answer its git with, so its words stay as they were before `zcp git`.
func plainGit(cmds ...string) string { return "`git " + strings.Join(cmds, " && git ") + "`" }

// leaseAcquire is the call that checks branch out in a lease of pool, cut
// from main when origin does not hold it yet.
func leaseAcquire(pool, branch string) string {
	return fmt.Sprintf("`zerops_lease action=\"acquire\" pool=%q branch=%q`", pool, branch)
}
