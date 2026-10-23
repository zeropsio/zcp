package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// poolContainerCeiling is the most containers a pool holds: the import
// schema's bound for both pool types (poolContainerBounds), and what the
// platform caps a higher maxContainers to (live, 2026-10-23: 11 is accepted
// and the pool still holds 10).
const poolContainerCeiling = 10

// poolStopKeeps is what a stop and start (or a restart) of a pool keeps and
// what it loses (live, 2026-10-23).
const poolStopKeeps = "A stop and start keep every container — its number, hostname and lease — and its /var/www and /home/zerops; the processes running in it and its /tmp are gone."

// poolLimits reads the pool's limits within the read budget.
func (d LeaseDeps) poolLimits(ctx context.Context, rp *resolvedPool) (platform.SwarmPool, error) {
	budget := d.ReadTimeout
	if budget == 0 {
		budget = leaseReadTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	return rp.api.Pool(ctx)
}

// poolRunStatus is the pool service's status, read by id (the service search
// lags writes, and a pool just stopped can still read ACTIVE there); the
// status the pool was resolved with stands in when that read fails.
func poolRunStatus(ctx context.Context, d LeaseDeps, rp *resolvedPool) (status string, running bool) {
	svc := rp.svc
	if d.Client != nil {
		if fresh, err := d.Client.GetService(ctx, rp.svc.ID); err == nil && fresh != nil {
			svc = fresh
		}
	}
	return svc.Status, svc.IsLive()
}

// poolNotRunningError says that an orchestrator does not answer because its
// pool service is not running, and what brings it back.
func poolNotRunningError(pool, op, status string, cause error) error {
	var msg, suggestion string
	switch status {
	case platform.ServiceStatusStopped, "STOPPING":
		msg = fmt.Sprintf("Cannot %s on pool '%s': the pool is stopped (status %s), so its orchestrator does not answer", op, pool, status)
		suggestion = fmt.Sprintf("Start it: zerops_manage action=start serviceHostname=%q. %s", pool, poolStopKeeps)
	case "CREATING", "STARTING", "RESTARTING", "RELOADING":
		msg = fmt.Sprintf("Cannot %s on pool '%s': the pool is not running yet (status %s), so its orchestrator does not answer", op, pool, status)
		suggestion = "Its orchestrator answers once the pool is ACTIVE (zerops_discover shows its status); retry then. " + poolStopKeeps
	default:
		msg = fmt.Sprintf("Cannot %s on pool '%s': the pool is not running (status %s), so its orchestrator does not answer", op, pool, status)
		suggestion = fmt.Sprintf("Its orchestrator answers only while the pool is ACTIVE: zerops_events serviceHostname=%q shows what happened to it.", pool)
	}
	out := platform.NewPlatformError(platform.ErrPrerequisiteMissing, msg, suggestion)
	out.Cause = cause
	return out
}

// poolUsage is a pool's containers as the orchestrator lists them, matched
// against zcp's own lease records.
type poolUsage struct {
	listed bool // the containers were listed
	max    int  // the pool's maximum; 0 when the limits were not read
	total  int  // containers listed
	leased int  // containers reserved
	yours  []string
}

// readPoolUsage reads the pool's limits and containers (both answer at once)
// and names zcp's own leases among the reserved ones. A read that fails
// leaves its part unknown; with no listing, zcp's records for the pool stand
// in for its own leases.
func readPoolUsage(ctx context.Context, d LeaseDeps, rp *resolvedPool) poolUsage {
	var u poolUsage
	if limits, err := d.poolLimits(ctx, rp); err == nil {
		u.max = limits.MaximumContainerCount
	}
	records, _ := readLeaseRecords(d.StateDir)
	own := map[string]leaseRecord{}
	for _, r := range records {
		if r.PoolHostname == rp.svc.Name && r.PoolServiceID == rp.svc.ID {
			own[r.ContainerID] = r
		}
	}
	containers, err := d.containers(ctx, rp)
	if err != nil {
		for _, r := range own {
			u.yours = append(u.yours, r.alias())
		}
	} else {
		u.listed, u.total = true, len(containers)
		for _, c := range containers {
			if c.LockState != leaseLockReserved {
				continue
			}
			u.leased++
			if r, ok := own[c.ID]; ok {
				u.yours = append(u.yours, r.alias())
			}
		}
	}
	slices.SortFunc(u.yours, func(a, b string) int { return cmp.Compare(leaseNumber(a), leaseNumber(b)) })
	return u
}

// leasePoolFullError is an acquire refused because every container that could
// serve it is reserved and another could not be created (all-reserved). The
// orchestrator queues nothing — an acquire succeeds only after a release — so
// the refusal says how full the pool is, which leases are zcp's own, and what
// frees a container. Raising the maximum is offered only while the
// platform's cause is the pool holding its maximum and that maximum is below
// the per-pool ceiling.
func leasePoolFullError(ctx context.Context, d LeaseDeps, rp *resolvedPool, se *platform.SwarmError) error {
	pool := rp.svc.Name
	u := readPoolUsage(ctx, d, rp)
	var msg string
	var advice []string
	release := ""
	switch {
	case len(u.yours) > 0:
		release = fmt.Sprintf("Release a lease of yours you no longer need (zerops_lease action=release lease=%s)", u.yours[0])
	case u.listed:
		release = "None of these leases is yours: only their holders can release them"
	}
	if se.Cause != "" && !se.AtServiceMaximum() {
		msg = fmt.Sprintf("Pool '%s' has no free container and the platform would not create another: %s%s", pool, se.Cause, u.countNote())
		advice = append(advice, "Raising maxContainers will not help: the platform refused for another reason than the pool's maximum")
		if release != "" {
			advice = append(advice, release+" to free a container")
		}
	} else {
		msg = fmt.Sprintf("Pool '%s' is full: %s%s. The orchestrator does not queue an acquire: one succeeds only after a lease is released", pool, u.leasedNote(), u.holdersNote())
		var raise string
		switch {
		case u.max >= poolContainerCeiling:
			raise = fmt.Sprintf("Raising maxContainers will not help: the pool's maximum is already %d, the most a pool holds (a higher value is capped)", poolContainerCeiling)
		case u.max > 0:
			raise = fmt.Sprintf("raise the pool's maxContainers (zerops_scale serviceHostname=%q maxContainers=<up to %d>): a pool holds at most %d containers, and a higher value is capped at %d", pool, poolContainerCeiling, poolContainerCeiling, poolContainerCeiling)
		default:
			raise = fmt.Sprintf("raise the pool's maxContainers (zerops_scale serviceHostname=%q) if it is below %d, the most a pool holds (a higher value is capped)", pool, poolContainerCeiling)
		}
		switch {
		case len(u.yours) > 0 && u.max < poolContainerCeiling:
			advice = append(advice, release+", or "+raise)
		case release != "":
			advice = append(advice, release, capitalize(raise))
		default:
			advice = append(advice, "zerops_lease action=status lists the pool's leases", capitalize(raise))
		}
	}
	out := platform.NewPlatformError(platform.ErrAPIError, msg, strings.Join(advice, ". ")+".")
	out.APICode = se.Reason
	out.Cause = se
	return out
}

// leasedNote says how many containers are leased, out of the maximum when it
// is known.
func (u poolUsage) leasedNote() string {
	switch {
	case !u.listed:
		return "every container that can serve an acquire is leased"
	case u.max > 0:
		return fmt.Sprintf("%d of at most %d containers are leased", u.leased, u.max)
	}
	return fmt.Sprintf("%d containers are leased", u.leased)
}

// holdersNote names zcp's own leases and counts the others'.
func (u poolUsage) holdersNote() string {
	others := u.leased - len(u.yours)
	yours := strings.Join(u.yours, ", ")
	switch {
	case !u.listed && len(u.yours) > 0:
		return " (yours, by zcp's records: " + yours + ")"
	case !u.listed:
		return ""
	case len(u.yours) == 0:
		return fmt.Sprintf(" (none of them yours: %d held by other holders)", u.leased)
	case others > 0:
		return fmt.Sprintf(" (yours: %s; %d held by other holders)", yours, others)
	}
	return " (all yours: " + yours + ")"
}

// countNote is the pool's fill as a sentence of its own, when it is known.
func (u poolUsage) countNote() string {
	if !u.listed {
		return ""
	}
	return ". " + capitalize(u.leasedNote()) + u.holdersNote()
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// LeasePoolUnread is a pool whose orchestrator a status of every pool could
// not read, and why.
type LeasePoolUnread struct {
	Pool string `json:"pool"`
	Note string `json:"note"`
}

// LeaseStatusView is a lease status: the leases, and — when every pool is
// listed — each pool whose orchestrator could not be read. Such a pool's
// leases are zcp's own records, marked unverified.
type LeaseStatusView struct {
	Leases []LeaseView
	Unread []LeasePoolUnread
}

// unverifiedViews are zcp's records of a pool its orchestrator could not
// confirm: the last known state (held — a record is written only by a
// successful acquire), marked unverified, with why.
func unverifiedViews(records []leaseRecord, why string) []LeaseView {
	views := make([]LeaseView, 0, len(records))
	for _, r := range records {
		v := r.view(LeaseStateHeld)
		v.AcquiredAt = r.AcquiredAt
		v.Unverified = true
		v.Note = "zcp's own record, unconfirmed: " + why
		views = append(views, v)
	}
	return views
}

// errorNote is an error as one line of a note: a platform error's message and
// what to do about it.
func errorNote(err error) string {
	var pe *platform.PlatformError
	if errors.As(err, &pe) {
		if pe.Suggestion != "" {
			return pe.Message + ". " + pe.Suggestion
		}
		return pe.Message
	}
	return err.Error()
}
