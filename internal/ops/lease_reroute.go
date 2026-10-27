package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// A pool container the orchestrator stops leaves the pool's public routes,
// and the platform's balancer does not put it back when the orchestrator
// starts it again: it rebuilds the pool's routes only when the pool's
// containers change — a create (the route answered ~1 s after it began) or a
// removal (~7 s) — while re-enabling or toggling the subdomain changes
// nothing (live-verified 2026-10-25, docs/spec-workflows.md §8 DP-28). An
// unparked lease's dev server answered over the project network while its
// preview answered 502 for good. So once an unpark's container runs again,
// zcp changes the pool's containers under the warm policy's lock (DP-27): it
// replaces the pool's warm container — removed (free, so without a lease or
// force), and a new one created once it has left the pool — or, when the
// pool has none, creates it within maxContainers. A pool with no HTTP port
// has no public route; a full pool whose other containers are all held
// leaves nothing zcp may change, and the unpark says the preview is not
// routed until the pool's containers change — which the park of a lease in
// such a pool says up front (parkedUnroutedNote), parking it all the same.
//
// A reset stops and starts its container too, so the one a release keeps as
// the pool's warm container is off the routes until the next acquire's
// top-up creates a container — which a pool holding its maximum cannot do:
// the acquire that took a reset-kept container in a full pool answered 502
// (live-verified 2026-10-25). So in a full pool serving previews a release
// removes its container rather than keep it warm (poolFullOfRoutes, DP-27),
// and the pool's next acquire creates one, whose create re-renders the
// routes: a cold start, and no warm container while the pool is full.

// leaseRerouteGoneEvery and leaseRerouteGoneWait pace the wait for a removed
// warm container to leave the pool before its replacement is created: the
// orchestrator counts a container being removed toward maxContainers, and a
// removal took ~9 s (live, 2026-10-25).
const (
	leaseRerouteGoneEvery = time.Second
	leaseRerouteGoneWait  = 2 * time.Minute
)

// leaseRouteWhy is why an unpark changes the pool's containers.
const leaseRouteWhy = "the platform routes the pool's subdomain to a container started again only once the pool's containers change"

// rerouteUnparked changes the pool's containers so the platform routes the
// pool's subdomain to the lease's container again, which the orchestrator
// just started (or which started outside zcp after zcp parked it), and says
// what it did: "" for a pool that serves no HTTP port.
func rerouteUnparked(ctx context.Context, d LeaseDeps, rp *resolvedPool, rec leaseRecord) string {
	if !poolServesHTTP(ctx, d, rp) {
		return ""
	}
	w := d.Warm
	if w == nil {
		return unroutedNote("zcp keeps no warm container for this pool")
	}
	unlock, err := lockPoolWarm(ctx, d.StateDir, rp.svc.Name, cmp.Or(w.LockWait, leaseWarmLockWait))
	if err != nil {
		if errors.Is(err, errWarmLockBusy) {
			return unroutedNote("another zcp call was deciding the pool's warm container")
		}
		return unroutedNote(fmt.Sprintf("the pool's warm container could not be decided (%v)", err))
	}
	handed := false
	defer func() {
		if !handed {
			unlock()
		}
	}()
	read, err := d.readPoolWarm(ctx, rp, rec.ContainerID, 1)
	if err != nil {
		return unroutedNote("the pool's containers could not be read (" + err.Error() + ")")
	}
	if len(read.warm) > 0 {
		id := read.warm[0]
		if rp.api.Delete(ctx, id, "") == nil {
			unlock()
			handed = true
			w.spawn(func() { w.replaceRemoved(context.WithoutCancel(ctx), d, rp, id) })
			return "zcp replaced the pool's warm container, so its preview is routed again (" + leaseRouteWhy + "); a new warm container is being prepared"
		}
		// A refused removal changed nothing: an acquire may have taken the
		// container meanwhile, so a create is tried as for a pool with none.
		read.ready, read.warm = 0, nil
	}
	if _, handed = w.topUp(ctx, d, rp, read, unlock); handed {
		return "zcp is creating the pool's warm container, so its preview is routed again (" + leaseRouteWhy + ")"
	}
	why := "nothing was created"
	if o := readWarmOutcome(d.StateDir, rp.svc.Name); o != nil && o.Note != "" {
		why = o.Note
	}
	return unroutedNote(why)
}

// unroutedNote says an unparked lease's preview is not routed yet, and why
// zcp changed none of the pool's containers.
func unroutedNote(why string) string {
	return "its preview may answer 502 until the pool's containers change: " + leaseRouteWhy + ", and zcp changed none (" + why +
		"); an acquire that creates a container or a release that removes one routes it again"
}

// parkedUnroutedNote is what a park says up front of a pool its unpark
// cannot route the lease's preview in again (rerouteUnparked): one serving an
// HTTP port that holds its maximum (poolFullOfRoutes) with no free warm
// container an unpark could replace. The park is never refused for it; the
// note suggests a release instead — in a full pool it removes the container,
// and the branch's next acquire creates a routed one — or a raise of the
// pool's maximum, the person's call. "" for any other pool, and for one whose
// containers or limits cannot be read.
func parkedUnroutedNote(ctx context.Context, d LeaseDeps, rp *resolvedPool, rec leaseRecord) string {
	read, err := d.readPoolWarm(ctx, rp, rec.ContainerID, 1)
	if err != nil || read.ready > 0 {
		return ""
	}
	limit, full := poolFullOfRoutes(ctx, d, rp, read)
	if !full {
		return ""
	}
	alias := rec.alias()
	note := fmt.Sprintf("once unparked, its preview answers 502 until the pool's containers change (an acquire that creates a container or a release that removes one): "+
		"the platform routes a container started again only then, and the pool holds its maximum of %d containers with none free, so the unpark can neither replace a warm container nor create one; "+
		"if its preview must answer when it runs again, release it instead once its work is pushed (zerops_lease action=release lease=%s) — in a full pool the release removes its container, and the branch's next acquire creates one whose preview is routed",
		limit, alias)
	if limit < poolContainerCeiling {
		note += fmt.Sprintf(" — or raise the pool's maxContainers (zerops_scale serviceHostname=%q maxContainers=<up to %d>), which gives the unpark room to create one%s", rp.svc.Name, poolContainerCeiling, poolRaiseNote)
	}
	return note
}

// poolFullOfRoutes reports the pool's maximum, and whether a release must
// remove the container it would keep as the pool's warm one: the pool holds
// its maximum — every listed container counted as the top-up counts them,
// the released one, a stuck one and one being removed included
// (stuckContainer) — so no container can be created after this release, and
// it serves an HTTP port, whose route the container's reset would lose.
// Limits that cannot be read keep the container, as before; so does a pool
// with no HTTP port, which has no route to lose.
func poolFullOfRoutes(ctx context.Context, d LeaseDeps, rp *resolvedPool, read poolWarmRead) (int, bool) {
	limits, err := d.poolLimits(ctx, rp)
	if err != nil || limits.MaximumContainerCount <= 0 {
		return 0, false
	}
	total := read.total
	if read.mine == nil {
		total++ // the listing lacked the released container, which is in the pool all the same
	}
	if total < limits.MaximumContainerCount || !poolServesHTTP(ctx, d, rp) {
		return 0, false
	}
	return limits.MaximumContainerCount, true
}

// fullPoolRemoveWhy is why a release removed, in a full pool of maximum limit
// serving previews, a container it would otherwise keep warm.
func fullPoolRemoveWhy(limit int) string {
	return fmt.Sprintf("the pool holds its maximum of %d containers, so none can be created after this release, and %s — kept as the warm one, the container would leave the pool's routes in its reset and the next lease on it would get a preview answering 502; "+
		"the pool's next acquire creates a container instead (a cold start), whose preview is routed, and the pool has no warm container until it has room for one", limit, leaseRouteWhy)
}

// fullPoolWarmNote is what status says of a pool a release left without a
// warm container because the pool was full.
func fullPoolWarmNote(limit int) string {
	return fmt.Sprintf("no warm container while the pool is full: a release removed its container rather than keep it warm, since the pool held its maximum of %d containers and a container kept warm would leave the pool's routes in its reset (%s); "+
		"the pool's next acquire creates a container (a cold start), and an acquire or release that finds room tops the pool up", limit, leaseRouteWhy)
}

// poolServesHTTP reports whether the pool declares an HTTP port, read by id
// (the listing the pool was resolved from stands in when the read fails).
func poolServesHTTP(ctx context.Context, d LeaseDeps, rp *resolvedPool) bool {
	svc := rp.svc
	if d.Client != nil {
		if fresh, err := d.Client.GetService(ctx, rp.svc.ID); err == nil && fresh != nil {
			svc = fresh
		}
	}
	return slices.ContainsFunc(svc.Ports, func(p platform.Port) bool { return p.HTTPSupport || isHTTPScheme(p) })
}

// replaceRemoved creates the pool's warm container again once the one an
// unpark removed (id) has left the pool's listing, under the warm lock; one
// still listed after leaseRerouteGoneWait is recorded, and the pool's next
// acquire or release tops it up.
func (w *LeaseWarm) replaceRemoved(ctx context.Context, d LeaseDeps, rp *resolvedPool, id string) {
	wctx, cancel := context.WithTimeout(ctx, leaseRerouteGoneWait)
	defer cancel()
	for {
		list, err := d.containers(wctx, rp)
		if err == nil && !slices.ContainsFunc(list, func(c platform.SwarmContainer) bool { return c.ID == id }) {
			break
		}
		select {
		case <-wctx.Done():
			w.record(d, rp.svc.Name, LeaseWarmFailed, 0, fmt.Sprintf("the warm container zcp removed to route an unparked lease again was still in the pool after %s, so no replacement was created; the pool's next acquire or release creates it", leaseRerouteGoneWait))
			return
		case <-time.After(leaseRerouteGoneEvery):
		}
	}
	unlock, err := lockPoolWarm(ctx, d.StateDir, rp.svc.Name, LeaseCallTimeout)
	if err != nil {
		return
	}
	w.topUpRead(ctx, d, rp, unlock)
}
