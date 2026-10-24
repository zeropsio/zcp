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
// lease) would move. The orchestrator's held-by-other answer to a release
// carrying the lease is final too (live-verified): another holder reserves
// the container, and the record is dropped as lost (dropLostLease), never
// retried "in a moment". To a delete, stop or start it drops the record only
// once a renew agrees the lease ended (heldByOtherAsked); a renew that does
// not answer leaves the container maybe another holder's, sent, mounted and
// written nothing more. A record is dropped only while it is still the
// lease's own (dropLeaseRecord).

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
	rp, gone, err := recordPool(ctx, d, rec)
	switch {
	case gone != "":
		return leaseVerdict{own: leaseGone, why: gone}
	case err != nil:
		return leaseVerdict{own: leaseUnverified, why: unverifiedLead + ": " + errorNote(err), err: err}
	}
	return renewOwnership(ctx, d, rp, rec)
}

// recordPool resolves the pool rec names: through the service search, and —
// when the search misses it — by id (poolOfRecords), since the search lags
// writes (§3.5) and its miss is no proof the pool is gone; a search that
// fails otherwise is followed by a read by id for a pool gone or being
// deleted (DP-20). gone says why the pool holds nothing of the lease (gone,
// being deleted, re-created since the lease was taken); err why zcp cannot
// tell — the read by id's own failure when the search missed.
func recordPool(ctx context.Context, d LeaseDeps, rec leaseRecord) (rp *resolvedPool, gone string, err error) {
	rp, err = resolvePool(ctx, d, rec.PoolHostname)
	if err != nil {
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || pe.Code != platform.ErrServiceNotFound {
			if why := leasePoolGoneWhy(ctx, d, rec); why != "" {
				return nil, why, nil
			}
			return nil, "", err
		}
		if rp, gone, err = poolOfRecords(ctx, d, []leaseRecord{rec}); rp == nil {
			return nil, gone, err
		}
	}
	if rp.svc.ID != rec.PoolServiceID {
		return nil, recreatedWhy, nil
	}
	if state := poolDeletionState(rp.svc.Status); state != "" {
		return nil, fmt.Sprintf("the pool service %s (status %s)", state, rp.svc.Status), nil
	}
	return rp, "", nil
}

// poolOfRecords reads by id the pool service records name, once the service
// search missed their pool: the first still there (rp), why none is (gone),
// or a read that failed otherwise and leaves it untold (err, the read's own
// failure — never the search's "no such service").
func poolOfRecords(ctx context.Context, d LeaseDeps, records []leaseRecord) (rp *resolvedPool, gone string, err error) {
	gone = "the pool service no longer exists"
	seen := map[string]bool{}
	for _, r := range records {
		if seen[r.PoolServiceID] {
			continue
		}
		seen[r.PoolServiceID] = true
		svc, gerr := d.Client.GetService(ctx, r.PoolServiceID)
		switch {
		case gerr != nil && IsServiceGone(gerr):
			continue
		case gerr != nil:
			err = gerr
			continue
		}
		if state := poolDeletionState(svc.Status); state != "" {
			gone = fmt.Sprintf("the pool service %s (status %s)", state, svc.Status)
			continue
		}
		found, oerr := orchestratorFor(ctx, d, svc)
		if oerr != nil {
			err = oerr
			continue
		}
		return found, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	return nil, gone, nil
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

// leaseEndedError is a step that found, while it ran, that the lease had
// ended outside zcp (why): its caller drops the record as lost and sends the
// container nothing more.
type leaseEndedError struct{ why string }

func (e *leaseEndedError) Error() string { return "the lease ended outside zcp: " + e.why }

// leaseEnded is why err says the lease ended outside zcp; ok is false for any
// other error.
func leaseEnded(err error) (why string, ok bool) {
	var e *leaseEndedError
	if errors.As(err, &e) {
		return e.why, true
	}
	return "", false
}

// heldByOtherAsked is, for a stop, start or delete the orchestrator refused
// (err), whether it refused it held-by-other (held) and, then, what a renew
// of the lease answers (v). held-by-other is live-verified on a release only,
// and what the orchestrator answers its own holder while it stops or starts
// the container is not known, so it alone never drops a record (DP-2): a
// renew that agrees the lease ended does; one that does not answer leaves the
// container maybe another holder's — nothing more is sent to it, mounted or
// written (heldByOtherUnverifiedRefusal); one that gives the container to the
// lease makes the refusal an orchestrator failure like any other.
func heldByOtherAsked(ctx context.Context, d LeaseDeps, rp *resolvedPool, rec leaseRecord, err error) (v leaseVerdict, held bool) {
	if !isHeldByOther(err) {
		return leaseVerdict{}, false
	}
	return renewOwnership(ctx, d, rp, rec), true
}

// heldByOtherUnverifiedRefusal refuses what (a park, an unpark, a release) of
// a lease whose call the orchestrator refused held-by-other and whose renew
// went unanswered (v): the container may be another holder's, so zcp left it,
// its mount and the record as they were.
func heldByOtherUnverifiedRefusal(alias, what string, v leaseVerdict) error {
	return unverifiedRefusal(fmt.Sprintf("Lease %s was not %s: the orchestrator refused zcp's call on its container, saying another holder reserves it, and did not answer whether this lease still holds it, so zcp mounted nothing, sent nothing more and left the record as it was", alias, what), v)
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
// container resets it whatever lease it carries. A record at the alias that
// names another lease by now — a zcp sharing the state directory took the
// container again — is that lease's, with its mount and stanza: all left
// alone (dropLeaseRecord).
func dropLostLease(ctx context.Context, d LeaseDeps, rec leaseRecord, why string) (LeaseView, error) {
	if leaseRecordReplaced(d.StateDir, rec) {
		return dropLeaseRecord(d.StateDir, rec, LeaseStateLost, lostDropNote(why))
	}
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
	return unverifiedRefusal(fmt.Sprintf("Lease %s was not %s: zcp could not ask the pool's orchestrator whether its container is still this lease's, so it read nothing from it and sent nothing to it", alias, what), v)
}

// unverifiedRefusal is a refusal saying msg of a lease the orchestrator did
// not answer about (v), keeping the code and suggestion of the
// orchestrator's own failure and naming the status that asks again.
func unverifiedRefusal(msg string, v leaseVerdict) error {
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
