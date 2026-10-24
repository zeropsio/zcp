package ops

import (
	"context"
	"errors"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
)

// A container the platform created for the pool's minContainers has no
// primary snapshot, so the orchestrator refuses its reset (no-primary-snapshot)
// and keeps the reservation. Releasing it without the reset is no way out: a
// clean acquire resets only a container marked dirty, which writes over ssh
// never mark, so the next holder would get this holder's /var/www, $HOME, /tmp
// and /opt. zcp deletes the container instead, with the lease, once the
// release guard passed (or force); the pool creates fresh containers — with a
// snapshot — on later acquires (docs/spec-workflows.md §8 DP-15).

// unresettableAdvice is what to do about a pool whose containers cannot be
// reset: zcp keeps the pool's warm container itself (DP-27), so a container
// the agent creates by hand only competes with it.
const unresettableAdvice = "Set the pool's minContainers to 0: zcp keeps the pool's warm container itself, one the orchestrator creates and can reset"

// unresettableNote is what a release that deleted its container says.
const unresettableNote = "the container could not be reset (the platform created it for the pool's minContainers, so it has no primary snapshot), so it was deleted rather than handed to the next holder with this lease's files. " + unresettableAdvice

// deleteUnresettable deletes the leased container whose reset the
// orchestrator refused and drops the record. A delete the orchestrator refuses
// keeps the reservation and the record; a lease it no longer knows has nothing
// left to delete.
func deleteUnresettable(ctx context.Context, d LeaseDeps, rp *resolvedPool, rec leaseRecord, alias string) (LeaseView, error) {
	err := rp.api.Delete(ctx, rec.ContainerID, rec.Lease)
	if err == nil {
		v, dropErr := dropLeaseRecord(d.StateDir, rec, LeaseStateReleased, unresettableNote)
		v.containerDeleted = true
		return v, dropErr
	}
	var se *platform.SwarmError
	if errors.As(err, &se) && (se.Reason == platform.SwarmReasonUnknownLease || se.Reason == platform.SwarmReasonContainerNotFound) {
		return dropLeaseRecord(d.StateDir, rec, LeaseStateLost, "the orchestrator no longer held this container for this lease, so there was nothing to release; the record is dropped")
	}
	if v, held := heldByOtherAsked(ctx, d, rp, rec, err); held && v.own.lost() {
		return dropLeaseRecord(d.StateDir, rec, LeaseStateLost, lostDropNote(v.why))
	}
	failed := leaseOrchestratorError(ctx, d, rp, "delete "+alias, err)
	var pe *platform.PlatformError
	if !errors.As(failed, &pe) {
		return LeaseView{}, fmt.Errorf("lease %s could not be reset, and deleting it failed: %w", alias, failed)
	}
	out := *pe
	out.Message = fmt.Sprintf("Lease %s could not be reset (the platform created its container for the pool's minContainers, so it has no primary snapshot), and deleting it instead failed, so it stays reserved: %s", alias, pe.Message)
	out.Suggestion = joinNotes(pe.Suggestion, fmt.Sprintf("Retry zerops_lease action=release lease=%s; zcp never releases it unreset, which would hand its files to the next holder", alias))
	return LeaseView{}, &out
}
