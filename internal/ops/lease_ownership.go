package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// A lease can end outside zcp: released with its lease by someone who read the
// record, or the container deleted, and the pool hands the container to its
// next acquirer under the same hostname. So before zcp acts on a lease — reads
// it over ssh, releases, deletes, stops or starts its container — it asks the
// orchestrator whether the container the record names is still this record's
// reservation (docs/spec-workflows.md §8 DP-2), and asks again right before
// each call that changes it. The question is the record's own lease: a renew
// (POST /container/{id}/renew with X-Swarm-Lease) changes nothing for a lease
// with no expiry — every lease zcp takes — and answers exactly (live-verified
// 2026-10-24):
//
//   - 200, lockState reserved (or stopping, starting): ours — running or
//     stopped, however the reservation's lockSince moved;
//   - 200, lockState deleting: lost — the container is being deleted;
//   - 400 unknown-lease: lost — this lease holds no reservation of the
//     container any more (released, reset, deleted and re-made, or another
//     holder's now); a lease is tied to one container id, and a lease with no
//     expiry ends only by a release, a reset or a delete;
//   - 400 container-not-found (or the bare "Container not found" the
//     orchestrator passes through right after a delete): lost — gone;
//   - anything else — no answer, a 5xx, a 401/403, a pool zcp cannot reach —
//     unverified: zcp cannot tell, so it acts on nothing and says why.
//
// A pool that is gone, re-created or being deleted holds nothing of the lease
// (DP-20) and is lost without a renew. The record carries nothing but the
// lease for this: no reservation time to compare, which a stop or start zcp
// did not record (an older zcp sharing the state directory, anyone holding the
// lease) would move. The orchestrator's held-by-other answer to a release,
// delete, stop or start carrying the lease is final too: another holder
// reserves the container, and the record is dropped as lost (dropLostLease),
// never retried "in a moment".

// leaseOwnership is what the orchestrator says of the container a lease
// record names.
type leaseOwnership int

const (
	// leaseUnverified: the orchestrator did not answer whose it is — also the
	// zero value, so a verdict nobody asked for never reads as ours.
	leaseUnverified leaseOwnership = iota
	// leaseOurs: reserved by this lease.
	leaseOurs
	// leaseGone: the pool or the container is gone, or being deleted.
	leaseGone
	// leaseFree: this lease holds no reservation of it, and nobody else does.
	leaseFree
	// leaseOther: this lease holds no reservation of it; another holder does.
	leaseOther
)

// lost: the container is no longer this lease's — gone, free or another
// holder's.
func (o leaseOwnership) lost() bool { return o == leaseGone || o == leaseFree || o == leaseOther }

// leaseLockDeleting is the orchestrator's lockState of a container it is
// deleting.
const leaseLockDeleting = "deleting"

// unverifiedLead opens what a status, a refusal and a note say of a lease the
// orchestrator did not answer about.
const unverifiedLead = "zcp cannot verify that this container is still this lease's"

// heldByOtherWhy is why a record is dropped once the orchestrator refused a
// call with its lease held-by-other.
const heldByOtherWhy = "another holder reserves this container now: the orchestrator refused this lease's call on it (held-by-other), so the lease ended outside zcp and the pool handed the container on"

// Why a lost lease is lost, as the orchestrator answered.
const (
	freeWhy  = "the orchestrator no longer reserves this container (the lease ended or was released elsewhere)"
	otherWhy = "another holder reserves this container now: the orchestrator holds no reservation of it for this lease (unknown-lease), so the lease ended outside zcp and the pool handed the container on"
	endedWhy = "the orchestrator holds no reservation of this container for this lease (unknown-lease): the lease ended outside zcp"
	goneWhy  = "the container is gone from the pool"
	delWhy   = "the orchestrator is deleting this container"
)

// leaseVerdict is a lease's ownership as the orchestrator gave it: why says
// what it is when it is not plainly this lease's; err is what the
// orchestrator answered when it did not say (unverified), to refuse with; rp
// and container are the pool and the container as answered (nil and zero when
// the pool is gone, the zero container when the container is).
type leaseVerdict struct {
	own       leaseOwnership
	why       string
	err       error
	rp        *resolvedPool
	container platform.SwarmContainer
}

// readTimeout is the budget of one orchestrator read.
func (d LeaseDeps) readTimeout() time.Duration { return cmp.Or(d.ReadTimeout, leaseReadTimeout) }

// renewLease asks the orchestrator, with rec's lease, whether it still holds
// rec's container, within the read budget.
func (d LeaseDeps) renewLease(ctx context.Context, rp *resolvedPool, rec leaseRecord) (platform.SwarmContainer, error) {
	ctx, cancel := context.WithTimeout(ctx, d.readTimeout())
	defer cancel()
	return rp.api.Renew(ctx, rec.ContainerID, rec.Lease)
}

// recreatedWhy is why a lease taken on a pool since re-created is lost: the
// pool's orchestrator and containers are new, and hold nothing of it.
const recreatedWhy = "the pool was re-created since this lease was taken"

// listedReserved is what a listing says of a container (c nil: the listing
// lacks it) whose lease the orchestrator holds no reservation of: another
// holder's when it is listed reserved, or being stopped or started.
func listedReserved(c *platform.SwarmContainer) reservedNow {
	if c != nil && (c.LockState == leaseLockReserved || powerTransition(*c)) {
		return reservedByOther
	}
	return reservedByNobody
}

// reservedNow is what a listing says of a container whose lease the
// orchestrator holds no reservation of: another holder reserves it, nobody
// does, or it was not read.
type reservedNow int

const (
	reservedUnread reservedNow = iota
	reservedByOther
	reservedByNobody
)

// renewedOwnership maps a renew's answer (c, err) to the lease's ownership;
// other tells a lease the orchestrator holds no reservation of apart.
func renewedOwnership(c platform.SwarmContainer, err error, other reservedNow) (leaseOwnership, string) {
	if err == nil {
		switch {
		case c.LockState == leaseLockDeleting:
			return leaseGone, delWhy
		case c.LockState == leaseLockReserved || powerTransition(c):
			return leaseOurs, ""
		}
		return leaseUnverified, fmt.Sprintf("%s: the orchestrator renewed the lease yet lists the container %q", unverifiedLead, c.LockState)
	}
	var se *platform.SwarmError
	if errors.As(err, &se) {
		switch se.Reason {
		case platform.SwarmReasonUnknownLease:
			switch other {
			case reservedByOther:
				return leaseOther, otherWhy
			case reservedByNobody:
				return leaseFree, freeWhy
			case reservedUnread:
			}
			return leaseFree, endedWhy
		case platform.SwarmReasonContainerNotFound:
			return leaseGone, goneWhy
		}
	}
	return leaseUnverified, unverifiedLead + ": the orchestrator did not say whether the lease still holds it (" + errorNote(err) + ")"
}

// readLeaseOwnership asks the orchestrator, fresh, whether the container rec
// names is still rec's reservation: a pool that is gone, re-created or being
// deleted is lost without asking (the service search lags writes, §3.5, so a
// pool it misses is read by id first, and so is the pool of an orchestrator
// that cannot be reached, DP-20); otherwise the renew of rec's lease decides,
// and a lease it no longer holds is told apart — free or another holder's —
// by a read of the container by id. A pool or an orchestrator zcp cannot ask
// is unverified, carrying the error to refuse with.
func readLeaseOwnership(ctx context.Context, d LeaseDeps, rec leaseRecord) leaseVerdict {
	gone := func(why string) leaseVerdict { return leaseVerdict{own: leaseGone, why: why} }
	unverified := func(err error) leaseVerdict {
		return leaseVerdict{own: leaseUnverified, why: unverifiedLead + ": " + errorNote(err), err: err}
	}
	rp, err := resolvePool(ctx, d, rec.PoolHostname)
	if err != nil {
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || pe.Code != platform.ErrServiceNotFound {
			if why := leasePoolGoneWhy(ctx, d, rec); why != "" {
				return gone(why)
			}
			return unverified(err)
		}
		svc, gerr := d.Client.GetService(ctx, rec.PoolServiceID)
		switch {
		case gerr != nil && IsServiceGone(gerr):
			return gone("the pool service no longer exists")
		case gerr != nil:
			return unverified(err)
		}
		if rp, err = orchestratorFor(ctx, d, svc); err != nil {
			return unverified(err)
		}
	}
	if rp.svc.ID != rec.PoolServiceID {
		return gone(recreatedWhy)
	}
	if state := poolDeletionState(rp.svc.Status); state != "" {
		return gone(fmt.Sprintf("the pool service %s (status %s)", state, rp.svc.Status))
	}
	return renewOwnership(ctx, d, rp, rec)
}

// renewOwnership is readLeaseOwnership once the pool is in hand: the renew of
// rec's lease, an orchestrator that cannot be reached followed by a read of
// the pool by id (DP-20), and a lease the orchestrator holds no reservation
// of told apart — free or another holder's — by a read of the container.
func renewOwnership(ctx context.Context, d LeaseDeps, rp *resolvedPool, rec leaseRecord) leaseVerdict {
	c, err := d.renewLease(ctx, rp, rec)
	var se *platform.SwarmError
	if err != nil && !errors.As(err, &se) {
		if why := leasePoolGoneWhy(ctx, d, rec); why != "" {
			return leaseVerdict{own: leaseGone, why: why}
		}
	}
	other := reservedUnread
	if errors.As(err, &se) && se.Reason == platform.SwarmReasonUnknownLease {
		if listed, rerr := d.containerByID(ctx, rp, rec.ContainerID); rerr == nil {
			other = listedReserved(&listed)
		}
	}
	own, why := renewedOwnership(c, err, other)
	v := leaseVerdict{own: own, why: why, rp: rp, container: c}
	if own == leaseUnverified {
		v.err = leaseOrchestratorError(ctx, d, rp, "check "+rec.alias()+"'s container", err)
		if err == nil {
			v.err = errors.New(why)
		}
	}
	return v
}

// isHeldByOther: the orchestrator refused a call carrying the lease because
// another holder reserves the container.
func isHeldByOther(err error) bool {
	var se *platform.SwarmError
	return errors.As(err, &se) && se.Reason == platform.SwarmReasonHeldByOther
}

// lostDropNote is what a view of a dropped lost record says: why the lease is
// no longer this zcp's, and that nothing was released.
func lostDropNote(why string) string {
	return why + ", so there was nothing to release; the record is dropped"
}

// dropLostLease drops the record of a lease zcp no longer holds — why says
// so — with what the record kept on this host: its mount first, then (the
// record gone) its alias stanza. Nothing is read from or sent to the
// container, and the orchestrator is not called: a release of a free
// container resets it whatever lease it carries.
func dropLostLease(ctx context.Context, d LeaseDeps, rec leaseRecord, why string) (LeaseView, error) {
	unmountNote := d.Host.detachMount(ctx, d, rec)
	view, err := dropLeaseRecord(d.StateDir, rec, LeaseStateLost, lostDropNote(why))
	if err != nil {
		return LeaseView{}, err
	}
	view.Note = joinNotes(view.Note, unmountNote, d.Host.refreshAliases(ctx, d))
	return view, nil
}

// unverifiedRemedy is what settles a lease the orchestrator did not answer
// about.
const unverifiedRemedy = "zerops_lease action=status asks again once the pool's orchestrator answers; until then zcp reads nothing from the container and sends nothing to it"

// unverifiedLeaseRefusal refuses what (a release, a park, a sync) of a lease
// the orchestrator did not answer about: nothing was read from its container
// or sent to it. It keeps the code and suggestion of the orchestrator's own
// failure (a stopped pool says to start it, DP-10).
func unverifiedLeaseRefusal(alias, what string, v leaseVerdict) error {
	msg := fmt.Sprintf("Lease %s was not %s: zcp could not ask the pool's orchestrator whether its container is still this lease's, so it read nothing from it and sent nothing to it", alias, what)
	var pe *platform.PlatformError
	if errors.As(v.err, &pe) {
		out := *pe
		out.Message = msg + " — " + pe.Message
		out.Suggestion = joinNotes(pe.Suggestion, unverifiedRemedy)
		return &out
	}
	detail := v.why
	if v.err != nil {
		detail = v.err.Error()
	}
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing, msg+" — "+detail, unverifiedRemedy)
}

// lostLeaseNote is what a member of a sync or a rewire zcp no longer holds
// says, with the release that drops its record.
func lostLeaseNote(alias, why, what string) string {
	return fmt.Sprintf("%s's lease is lost (%s), so zcp %s; zerops_lease action=release lease=%s drops its record", alias, why, what, alias)
}

// siblingsMayBeWired names, for a lease dropped outside a release, the other
// leases of its branch that may still run wired to its container (DP-26);
// "" when it has none.
func siblingsMayBeWired(stateDir string, rec leaseRecord) string {
	siblings := siblingRecords(stateDir, rec)
	if len(siblings) == 0 {
		return ""
	}
	return fmt.Sprintf("the branch's other leases (%s) may still run wired to its container: zerops_lease action=status names any stale wiring and the restart that fixes it", strings.Join(aliasesOf(siblings), ", "))
}

// reservedOwnership renews, all at once, each container of containers that
// is listed reserved (or being stopped or started) and that one of own (by
// container id) records, and answers its ownership by container id. A
// container listed otherwise is no lease's: it is left out.
func reservedOwnership(ctx context.Context, d LeaseDeps, rp *resolvedPool, own map[string]leaseRecord, containers []platform.SwarmContainer) map[string]leaseOwnership {
	type ask struct {
		rec leaseRecord
		own leaseOwnership
	}
	var asks []*ask
	for _, c := range containers {
		r, ok := own[c.ID]
		if !ok {
			continue
		}
		if r.PoolServiceID == rp.svc.ID && listedReserved(&c) == reservedByOther {
			asks = append(asks, &ask{rec: r})
		}
	}
	runMembers(asks, func(a *ask) {
		c, err := d.renewLease(ctx, rp, a.rec)
		a.own, _ = renewedOwnership(c, err, reservedByOther)
	})
	out := make(map[string]leaseOwnership, len(asks))
	for _, a := range asks {
		out[a.rec.ContainerID] = a.own
	}
	return out
}
