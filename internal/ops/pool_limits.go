package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

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
	leased int  // containers reserved
	// yours are zcp's own leases among them, oldest activity first when it
	// was read (byOldestActivity), by container number otherwise.
	yours []LeaseView
	// read: zcp read its leases' activity (it reaches them over ssh), at now.
	read bool
	now  time.Time
}

// readPoolUsage reads the pool's limits and containers (both answer at once)
// and names zcp's own leases among the reserved ones. A read that fails
// leaves its part unknown; with no listing, zcp's records for the pool stand
// in for its own leases. With ssh it reads each of them as status does
// (readLeaseActivities, DP-21) — all at once, changing nothing — and orders
// them oldest activity first (DP-9).
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
	var mine []leaseRecord
	containers, err := d.containers(ctx, rp)
	if err != nil {
		for _, r := range own {
			mine = append(mine, r)
		}
	} else {
		u.listed = true
		for _, c := range containers {
			if c.LockState != leaseLockReserved {
				continue
			}
			u.leased++
			if r, ok := own[c.ID]; ok {
				mine = append(mine, r)
			}
		}
	}
	for _, r := range mine {
		u.yours = append(u.yours, r.view(LeaseStateHeld))
	}
	slices.SortFunc(u.yours, func(a, b LeaseView) int { return cmp.Compare(leaseNumber(a.Alias), leaseNumber(b.Alias)) })
	if d.SSH != nil && len(u.yours) > 0 {
		readLeaseActivities(ctx, d, mine, u.yours)
		u.read, u.now = true, d.now()
		slices.SortStableFunc(u.yours, byOldestActivity)
	}
	return u
}

// releaseCandidate is the lease of yours a full pool's refusal names the
// release of: the first, oldest activity first, that a release would free
// without losing what the guard reads and that is not in use now; the first
// of all when none is.
func (u poolUsage) releaseCandidate() string {
	for _, v := range u.yours {
		if u.read && releasable(v) {
			return v.Alias
		}
	}
	return u.yours[0].Alias
}

// leasePoolFullError is an acquire refused because every container that could
// serve it is reserved and another could not be created (all-reserved). The
// orchestrator queues nothing — an acquire succeeds only after a release — so
// the refusal says how full the pool is, which leases are zcp's own — with ssh,
// oldest activity first, each with what a release would lose — and what frees
// a container: the release of one of them, after asking the person when this
// conversation did not take it, never forced. Raising the maximum is offered
// only while the platform's cause is the pool holding its maximum and that
// maximum is below the per-pool ceiling.
func leasePoolFullError(ctx context.Context, d LeaseDeps, rp *resolvedPool, se *platform.SwarmError) error {
	pool := rp.svc.Name
	u := readPoolUsage(ctx, d, rp)
	var msg string
	var advice []string
	release := ""
	switch {
	case len(u.yours) > 0:
		release = fmt.Sprintf("Release a lease of yours you no longer need — ask the person first if this conversation did not take it (zerops_lease action=release lease=%s)", u.releaseCandidate())
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

// holdersNote names zcp's own leases and counts the others'. Once their
// activity was read it lists them oldest activity first, each with how
// recently it was active and what a release would lose (activitySummary).
func (u poolUsage) holdersNote() string {
	others := u.leased - len(u.yours)
	names := make([]string, 0, len(u.yours))
	for _, v := range u.yours {
		names = append(names, v.Alias)
	}
	yours := strings.Join(names, ", ")
	if u.read {
		summaries := make([]string, 0, len(u.yours))
		for _, v := range u.yours {
			summaries = append(summaries, activitySummary(v, u.now))
		}
		yours = strings.Join(summaries, "; ")
		switch {
		case !u.listed && len(u.yours) > 0:
			return " (yours, by zcp's records, oldest activity first: " + yours + ")"
		case u.listed && len(u.yours) > 0 && others > 0:
			return fmt.Sprintf(" (%d held by other holders; yours, oldest activity first: %s)", others, yours)
		case u.listed && len(u.yours) > 0:
			return " (all yours, oldest activity first: " + yours + ")"
		}
	}
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

// PoolShrink is what lowering a pool's maxContainers to To deletes: the pool
// deletes its highest-numbered containers above the new maximum within about
// 30 s, leased ones included, each with its /var/www, and their leases then
// answer container-not-found (live, 2026-10-23).
type PoolShrink struct {
	Pool string
	To   int
	// Holds is how many containers the orchestrator listed.
	Holds int
	// Deletes are the containers the lowering deletes, highest number first.
	Deletes []PoolShrinkContainer
	// Unread is why the containers could not be listed; empty when they were,
	// or when the lowering cannot delete any (a raise).
	Unread string
	// LowestSafe is the lowest maxContainers that deletes no leased container
	// (0 when none is leased); meaningful only when the containers were listed.
	LowestSafe int
}

// PoolShrinkContainer is one container a lowering deletes.
type PoolShrinkContainer struct {
	Alias  string
	Leased bool
	// Yours marks a lease zcp holds a record for; Branch is that lease's.
	Yours  bool
	Branch string
}

// LeasedDeletes are the leased containers the lowering deletes.
func (s PoolShrink) LeasedDeletes() []PoolShrinkContainer {
	var out []PoolShrinkContainer
	for _, c := range s.Deletes {
		if c.Leased {
			out = append(out, c)
		}
	}
	return out
}

// NeedsApproval reports a lowering that would delete a leased container, or
// whose deletions zcp could not check.
func (s PoolShrink) NeedsApproval() bool {
	return s.Unread != "" || len(s.LeasedDeletes()) > 0
}

// notice says what the lowering deletes, for the scale result.
func (s PoolShrink) notice() string {
	switch {
	case s.Unread != "":
		return fmt.Sprintf("zcp could not list the pool's containers (%s), so it cannot name the ones above maxContainers %d this lowering deletes.", s.Unread, s.To)
	case len(s.Deletes) == 0:
		return ""
	case len(s.LeasedDeletes()) == 0:
		names := make([]string, 0, len(s.Deletes))
		for _, c := range s.Deletes {
			names = append(names, c.Alias)
		}
		return fmt.Sprintf("This lowering deletes %s within about 30 s — free containers, leased by no one.", joinAnd(names))
	}
	names := make([]string, 0, len(s.Deletes))
	for _, c := range s.Deletes {
		names = append(names, c.Alias+" ("+c.Whose()+")")
	}
	return fmt.Sprintf("This lowering deletes %s within about 30 s. A deleted container takes its /var/www with it; zerops_lease action=status lists a lease of yours on one as lost, and its release drops the record.", joinAnd(names))
}

// Whose says whose the container is: your lease (with its branch), another
// holder's, or free.
func (c PoolShrinkContainer) Whose() string {
	switch {
	case c.Yours && c.Branch != "":
		return "your lease, branch " + c.Branch
	case c.Yours:
		return "your lease"
	case c.Leased:
		return "another holder's lease"
	}
	return "free"
}

// joinAnd joins names as "a", "a and b", "a, b and c".
func joinAnd(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// planPoolShrink reads which containers lowering svc's maxContainers to `to`
// deletes. A raise (to at or above the maximum the service runs with) can
// delete nothing and reads nothing. Otherwise the orchestrator lists the
// containers (check-before-mutate); one that cannot be asked leaves Unread.
func planPoolShrink(ctx context.Context, d LeaseDeps, svc, current *platform.ServiceStack, to int) PoolShrink {
	s := PoolShrink{Pool: svc.Name, To: to}
	if eff := effectiveAutoscaling(current); eff != nil && eff.HorizontalMaxCount > 0 && int(eff.HorizontalMaxCount) <= to {
		return s
	}
	if d.Swarm == nil {
		s.Unread = "this zcp cannot ask the pool's orchestrator here"
		return s
	}
	rp, err := orchestratorFor(ctx, d, svc)
	if err != nil {
		s.Unread = errorNote(err)
		return s
	}
	containers, err := d.containers(ctx, rp)
	if err != nil {
		s.Unread = errorNote(leaseOrchestratorError(ctx, d, rp, "list containers", err))
		return s
	}
	s.Holds = len(containers)
	sorted := slices.Clone(containers)
	slices.SortFunc(sorted, func(a, b platform.SwarmContainer) int { return cmp.Compare(b.Number, a.Number) })
	records, _ := readLeaseRecords(d.StateDir)
	own := map[string]leaseRecord{}
	for _, r := range records {
		if r.PoolHostname == svc.Name && r.PoolServiceID == svc.ID {
			own[r.ContainerID] = r
		}
	}
	for i, c := range sorted {
		leased := c.LockState == leaseLockReserved
		if leased && s.LowestSafe == 0 {
			s.LowestSafe = len(sorted) - i
		}
		if i >= len(sorted)-to {
			continue
		}
		pc := PoolShrinkContainer{Alias: leaseAlias(svc.Name, c.Number), Leased: leased}
		if r, ok := own[c.ID]; ok && leased {
			pc.Yours, pc.Branch = true, r.Branch
		}
		s.Deletes = append(s.Deletes, pc)
	}
	return s
}
