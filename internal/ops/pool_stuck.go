package ops

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// A container no lease holds still counts toward its pool's maximum while it
// is not one an acquire takes: a platform action on it failed (live,
// 2026-10-25: webpool #5 stood ACTION_FAILED after a restart its exhausted
// process table could not run, and every acquire after was refused "full: 2 of
// at most 3 containers are leased"), or the orchestrator is removing it. A
// full pool's refusal names each one (docs/spec-workflows.md §8 DP-9).
//
// zcp cannot remove a failed one: the orchestrator removes only a container
// its holder leases (DELETE /container/{id} with the lease; force takes the
// pool's ADMIN_TOKEN, which zcp never uses, PS-6), no lease holds this one, and
// the platform's API has no container delete of its own. Kara's run showed
// even the holder's own delete leave a failed container standing: zcp's
// release removed webpool-5 with its lease, and five minutes later it was
// still listed ACTION_FAILED. What frees a place is the person's call.

// swarmLockDeleting is the orchestrator's lockState of a container it is
// removing; it still counts toward the pool's total (DP-27).
const swarmLockDeleting = "deleting"

// stuckContainer: a listed container no lease holds that no acquire takes —
// one the orchestrator is removing, or one in any state but running or
// stopped (a platform action on it failed).
func stuckContainer(c platform.SwarmContainer) bool {
	if leasedContainer(c) {
		return false
	}
	return c.LockState == swarmLockDeleting ||
		(c.Status != platform.SwarmContainerActive && c.Status != platform.SwarmContainerStopped)
}

// stuckState is what a stuck container is, as a refusal names it.
func stuckState(c platform.SwarmContainer) string {
	if c.LockState == swarmLockDeleting {
		return "being removed"
	}
	return "status " + c.Status
}

// stuckNote is the message's clause naming the stuck containers among a full
// pool's places, "" when there is none.
func (u poolUsage) stuckNote(pool string) string {
	if len(u.stuck) == 0 {
		return ""
	}
	names := make([]string, 0, len(u.stuck))
	for _, c := range u.stuck {
		names = append(names, fmt.Sprintf("%s-%d (%s)", pool, c.Number, stuckState(c)))
	}
	if len(names) == 1 {
		return fmt.Sprintf(", and %s still takes a place of the pool though no acquire takes it", names[0])
	}
	return fmt.Sprintf(", and %s still take places of the pool though no acquire takes them", joinAnd(names))
}

// stuckAdvice says, for each stuck container, what holds its place and what
// frees it: one being removed frees its place within about half a minute; a
// failed one stays until the platform removes it, which zcp cannot ask — so
// the person decides whether to raise the pool's maximum by one, never zcp.
func (u poolUsage) stuckAdvice(pool string) string {
	var removing, failed []string
	for _, c := range u.stuck {
		alias := fmt.Sprintf("%s-%d", pool, c.Number)
		if c.LockState == swarmLockDeleting {
			removing = append(removing, alias)
			continue
		}
		failed = append(failed, fmt.Sprintf("%s (status %s)", alias, c.Status))
	}
	var advice []string
	if len(removing) > 0 {
		advice = append(advice, fmt.Sprintf("%s: the orchestrator is removing it, which frees its place within about half a minute; acquire again then", joinAnd(removing)))
	}
	if len(failed) > 0 {
		line := fmt.Sprintf("%s: a platform action on it failed (zerops_events serviceHostname=%q says which), so no acquire takes it, yet it counts toward the pool's maximum. zcp cannot remove it — the orchestrator removes only a container its holder leases, and no lease holds it", joinAnd(failed), pool)
		if u.max > 0 && u.max < poolContainerCeiling {
			line += fmt.Sprintf(". To lease beside it, ask the person whether to raise maxContainers by one (zerops_scale serviceHostname=%q maxContainers=%d); the failed container keeps its place until the platform removes it", pool, min(u.max+len(failed), poolContainerCeiling))
		}
		advice = append(advice, line)
	}
	return strings.Join(advice, ". ")
}
