package ops

import (
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
// it over ssh, releases, deletes, stops or starts its container — it decides
// whether the container the record names is still this record's reservation
// (docs/spec-workflows.md §8 DP-2): gone, free, reserved by this lease,
// reserved by another holder, or not verifiable. The verdict comes from one
// orchestrator read — by id (readLeaseOwnership), or the pool's listing a
// status already holds (leaseOwnershipOf) — and every caller takes it from
// there: status, every release, the warm decision, park, unpark, sync, an
// acquire's reuse check and the rewire.
//
// A read never answers with the lease, so zcp tells its own reservation by
// when it began. The record keeps the reservation's lockSince as the
// orchestrator answered the acquire (or a read by id right after it), and
// again after each stop or start zcp makes with the lease, since each restarts
// it (live-verified 2026-10-23). A lockSince within a second of the recorded
// one is this lease (an answer and a read may write it at other precisions);
// a later one is a reservation made since, which only another holder can have
// made — zcp alone holds the lease. While zcp has a stop or start in flight, or
// sent one whose answer it lost, the record says so (LockChanging) and a moved
// lockSince is unverified, never another holder's. A record older than keeping
// lockSince is this lease while the reservation began no later than its
// acquire, or its park, give or take leaseLockSinceSlack; past that it is
// unverified: a stop and start zcp made then reads exactly like another
// holder's reservation. Unverified is never lost: Mate unbinds a lost lease
// (docs/spec-mate.md §6.3), and a lost record is dropped without asking the
// orchestrator, which would leak this lease's container for good.
//
// What decides a container is no longer this lease's from a call rather than
// a read is the orchestrator's own answer: held-by-other to any call carrying
// the lease — release, delete, stop, start — is final, and the record is
// dropped as lost (dropLostLease); it is never retried "in a moment".

// leaseOwnership is what the orchestrator's read says of the container a
// lease record names.
type leaseOwnership int

const (
	// leaseOurs: reserved by this lease.
	leaseOurs leaseOwnership = iota
	// leaseUnverified: reserved, and zcp cannot tell by whom.
	leaseUnverified
	// leaseGone: the pool or the container is gone, or being deleted.
	leaseGone
	// leaseFree: in the pool, reserved by nobody.
	leaseFree
	// leaseOther: reserved by another holder.
	leaseOther
)

// lost: the container is no longer this lease's — gone, free or another
// holder's.
func (o leaseOwnership) lost() bool { return o == leaseGone || o == leaseFree || o == leaseOther }

// leaseLockDeleting is the orchestrator's lockState of a container it is
// deleting.
const leaseLockDeleting = "deleting"

// leaseLockSinceSame is how far apart two writings of one reservation's
// lockSince may be: an answer and a read may give it at other precisions,
// while another holder's reservation follows this one's end by far more.
const leaseLockSinceSame = time.Second

// leaseLockSinceSlack is how much later than an older record's acquire (or
// park) its reservation's lockSince may read and still be this lease's: the
// record's times are zcp's clock in whole seconds, lockSince the
// orchestrator's.
const leaseLockSinceSlack = 30 * time.Second

// unverifiedLead opens what a status, a refusal and a note say of a lease zcp
// cannot verify.
const unverifiedLead = "zcp cannot verify that this container is still this lease's"

// heldByOtherWhy is why a record is dropped once the orchestrator refused a
// call with its lease held-by-other.
const heldByOtherWhy = "another holder reserves this container now: the orchestrator refused this lease's call on it (held-by-other), so the lease ended outside zcp and the pool handed the container on"

// leaseVerdict is a lease's ownership as one read by id gave it: why says
// what it is when it is not plainly this lease's; rp and container are the
// pool and the container as read (nil and zero when the pool is gone, the
// zero container when the container is).
type leaseVerdict struct {
	own       leaseOwnership
	why       string
	rp        *resolvedPool
	container platform.SwarmContainer
}

// leaseOwnershipOf decides, from a read of the pool whose service id is
// poolServiceID, whether the container rec names is still rec's reservation
// (c nil: the read did not find it), and why when it is not plainly so. A
// reservation the orchestrator gives no lockSince for is this lease: nothing
// tells otherwise.
func leaseOwnershipOf(rec leaseRecord, poolServiceID string, c *platform.SwarmContainer) (leaseOwnership, string) {
	switch {
	case rec.PoolServiceID != poolServiceID:
		return leaseGone, "the pool was re-created since this lease was taken"
	case c == nil:
		return leaseGone, "the container is gone from the pool"
	case c.LockState == leaseLockDeleting:
		return leaseGone, "the orchestrator is deleting this container"
	case c.LockState != leaseLockReserved && !powerTransition(*c):
		return leaseFree, "the orchestrator no longer reserves this container (the lease ended or was released elsewhere)"
	case c.LockSince == "":
		return leaseOurs, ""
	}
	return reservationOwnership(rec, c.LockSince)
}

// reservationOwnership decides, by rec, whose the reservation that began at
// since is.
func reservationOwnership(rec leaseRecord, since string) (leaseOwnership, string) {
	inFlight := func() (leaseOwnership, string) {
		return leaseUnverified, fmt.Sprintf("%s: the orchestrator lists it reserved since %s, and zcp sent a stop or a start of it at %s whose outcome it never learned (each restarts the reservation's time)", unverifiedLead, since, rec.LockChanging)
	}
	if rec.LockSince != "" {
		delta, ok := lockSinceDelta(since, rec.LockSince)
		switch {
		case since == rec.LockSince || (ok && delta.Abs() < leaseLockSinceSame):
			return leaseOurs, ""
		case rec.LockChanging != "":
			return inFlight()
		case ok && delta > 0:
			return leaseOther, fmt.Sprintf("another holder reserves this container now: the orchestrator lists it reserved since %s, after this lease's own reservation (%s), so the lease ended outside zcp and the pool handed the container on", since, rec.LockSince)
		}
		return leaseUnverified, fmt.Sprintf("%s: the orchestrator lists it reserved since %s, which does not follow this lease's own reservation (%s)", unverifiedLead, since, rec.LockSince)
	}
	at, err := time.Parse(time.RFC3339Nano, since)
	if ref := recordReservedBy(rec); err == nil && !ref.IsZero() && !at.After(ref.Add(leaseLockSinceSlack)) {
		return leaseOurs, ""
	}
	if rec.LockChanging != "" {
		return inFlight()
	}
	return leaseUnverified, fmt.Sprintf("%s: the orchestrator lists it reserved since %s, later than this lease's acquire (%s), and the record is older than zcp keeping the reservation's own time — a stop and a start zcp made and another holder's reservation read alike", unverifiedLead, since, rec.AcquiredAt)
}

// lockSinceDelta is a − b, both RFC 3339 times; false when either does not
// parse.
func lockSinceDelta(a, b string) (time.Duration, bool) {
	ta, errA := time.Parse(time.RFC3339Nano, a)
	tb, errB := time.Parse(time.RFC3339Nano, b)
	if errA != nil || errB != nil {
		return 0, false
	}
	return ta.Sub(tb), true
}

// recordReservedBy is the latest moment an older record says zcp reserved or
// changed the reservation itself — its acquire or its park; zero when neither
// parses.
func recordReservedBy(rec leaseRecord) time.Time {
	var latest time.Time
	for _, s := range []string{rec.AcquiredAt, rec.ParkedAt} {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil && t.After(latest) {
			latest = t
		}
	}
	return latest
}

// readLeaseOwnership reads, by id, whether the container rec names is still
// rec's reservation. A pool that is gone, re-created or being deleted, and a
// container the orchestrator does not find, are gone, with no container read.
// The service search lags writes (§3.5), so a pool it misses is read by id
// before it counts as gone, and so is the pool of an orchestrator that cannot
// be reached (DP-20); one still there whose orchestrator does not answer is an
// error — zcp then cannot tell, and acts on nothing.
func readLeaseOwnership(ctx context.Context, d LeaseDeps, rec leaseRecord) (leaseVerdict, error) {
	gone := func(why string) (leaseVerdict, error) { return leaseVerdict{own: leaseGone, why: why}, nil }
	rp, err := resolvePool(ctx, d, rec.PoolHostname)
	if err != nil {
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || pe.Code != platform.ErrServiceNotFound {
			if why := leasePoolGoneWhy(ctx, d, rec); why != "" {
				return gone(why)
			}
			return leaseVerdict{}, err
		}
		svc, gerr := d.Client.GetService(ctx, rec.PoolServiceID)
		switch {
		case gerr != nil && IsServiceGone(gerr):
			return gone("the pool service no longer exists")
		case gerr != nil:
			return leaseVerdict{}, err
		}
		if rp, err = orchestratorFor(ctx, d, svc); err != nil {
			return leaseVerdict{}, err
		}
	}
	if rp.svc.ID != rec.PoolServiceID {
		return gone("the pool was re-created since this lease was taken")
	}
	if state := poolDeletionState(rp.svc.Status); state != "" {
		return gone(fmt.Sprintf("the pool service %s (status %s)", state, rp.svc.Status))
	}
	c, err := d.containerByID(ctx, rp, rec.ContainerID)
	if err != nil {
		var se *platform.SwarmError
		if errors.As(err, &se) && se.Reason == platform.SwarmReasonContainerNotFound {
			own, why := leaseOwnershipOf(rec, rp.svc.ID, nil)
			return leaseVerdict{own: own, why: why, rp: rp}, nil
		}
		if !errors.As(err, &se) {
			if why := leasePoolGoneWhy(ctx, d, rec); why != "" {
				return gone(why)
			}
		}
		return leaseVerdict{}, leaseOrchestratorError(ctx, d, rp, "check "+rec.alias()+"'s container", err)
	}
	own, why := leaseOwnershipOf(rec, rp.svc.ID, &c)
	return leaseVerdict{own: own, why: why, rp: rp, container: c}, nil
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

// leaseVerifyCall is the call that asks the orchestrator, with alias's lease,
// whether its container c is still this lease's: a park of a running one, an
// unpark of a stopped one — each a call the orchestrator answers held-by-other
// when another holder has it.
func leaseVerifyCall(alias string, c platform.SwarmContainer) string {
	if c.Status == platform.SwarmContainerStopped || c.LockState == leaseLockStopping {
		return LeaseUnparkCall(alias)
	}
	return LeaseParkCall(alias)
}

// unverifiedRemedy is how the agent settles a lease zcp cannot verify.
func unverifiedRemedy(alias string, c platform.SwarmContainer) string {
	return fmt.Sprintf("%s asks the orchestrator with the lease: another holder's reservation drops the record, this lease's verifies it. A forced release asks it too and, if the container is this lease's, releases it with whatever it holds: zerops_lease action=release lease=%s force=true", leaseVerifyCall(alias, c), alias)
}

// unverifiedLeaseRefusal refuses what (a release, a sync) of a lease zcp
// cannot verify: nothing is read from its container or sent to it.
func unverifiedLeaseRefusal(alias, what string, v leaseVerdict) error {
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		fmt.Sprintf("Lease %s was not %s: %s, so zcp reads nothing from it and sends nothing to it", alias, what, v.why),
		unverifiedRemedy(alias, v.container))
}

// reservedLockSince is c's lockSince when c is a settled reservation (not a
// stop or start in progress, whose end restarts it again); "" otherwise.
func reservedLockSince(c platform.SwarmContainer) string {
	if c.LockState != leaseLockReserved {
		return ""
	}
	return c.LockSince
}

// changeLock runs call — a stop or a start zcp makes with rec's lease — and
// keeps what it does to the reservation's lockSince in rec and its record:
// before the call the record says a change is in flight (LockChanging), so a
// status that reads the new lockSince meanwhile, or after a call whose answer
// was lost, says unverified, never another holder's; once the orchestrator
// answered, the record keeps the lockSince it answered — or a read by id
// right after gives — and the flag goes. A refusal the orchestrator answered
// changed nothing, and the flag goes too; held-by-other is returned as it is,
// for the caller to drop the record. A record that cannot be marked first
// refuses the call.
func changeLock(ctx context.Context, d LeaseDeps, rp *resolvedPool, rec *leaseRecord, verb string, call func(context.Context) (platform.SwarmContainer, error)) error {
	rec.LockChanging = d.now().UTC().Format(time.RFC3339)
	if err := writeLeaseRecord(d.StateDir, *rec); err != nil {
		rec.LockChanging = ""
		return fmt.Errorf("%s %s: its record could not be written before the call, so nothing was sent: %w", verb, rec.alias(), err)
	}
	c, err := call(ctx)
	var se *platform.SwarmError
	switch {
	case err == nil:
		since := reservedLockSince(c)
		if since == "" {
			if got, rerr := d.containerByID(context.WithoutCancel(ctx), rp, rec.ContainerID); rerr == nil {
				since = reservedLockSince(got)
			}
		}
		if since != "" {
			rec.LockSince, rec.LockChanging = since, ""
		}
	case isHeldByOther(err):
		return err
	case errors.As(err, &se) && se.Status >= 400 && se.Status < 500:
		rec.LockChanging = ""
	}
	// A record that cannot be written keeps the flag: unverified, never lost.
	_ = writeLeaseRecord(d.StateDir, *rec)
	return err
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
