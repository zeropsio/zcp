package ops

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// A branch that spans several app parts holds one lease per part's pool: a
// group (docs/spec-workflows.md §8 DP-17). The group is derived, never stored —
// the leases zcp holds of one repository and branch — and zcp holds one lease
// per repository, branch and pool, so an acquire of a key it already holds
// returns that lease, reused, instead of taking a second container. A group
// acquire checks every pool before it reserves any container, reserves the
// missing members at once, cuts a branch new to origin from one main commit
// for every member, and hands back every container it reserved when any
// member fails. Each member goes through the one lease's steps: the HQ
// credential (DP-16), the reservation (DP-3), the checkout (DP-6), then the
// repository's prepare and the supervisor (DP-13, DP-14). A group release
// (DP-23) reads every member before it releases any and releases each through
// the one lease's release step. A change of a group's membership first hands
// every other lease of it the recomputed sibling overlay (DP-25).

// leaseLocksDir is where the per-key acquire locks live, apart from the lease
// records (a record is replaced by rename on every write and cannot carry a
// lock itself).
const leaseLocksDir = "locks"

// groupMember is one pool of a group acquire.
type groupMember struct {
	pool string
	rp   *resolvedPool
	// held is the lease zcp held for the key before this call (returned,
	// reused); nil for a member this call leases.
	held *LeaseView
	// becomesDefault: the pool has no default repository yet; the group's
	// becomes it once the acquire worked.
	becomesDefault bool
	cred           poolCredentialState
	// rec is the member's record once reserved is set.
	rec      leaseRecord
	reserved bool
	view     LeaseView
	// err is the member's own failure; for a failed checkout it already says
	// where its container went (abandonLease).
	err error
}

// LeaseAcquireGroup leases one container per pool, every one on branch of one
// repository, and lists them in the pools' order. A pool zcp already holds
// the branch on keeps that lease (reused, never a second container); the rest
// are leased as LeaseAcquire leases one. Every pool, the branch and the
// repository are checked before any container is reserved; the missing
// members are reserved at once; a branch new to origin is cut for every member
// from one main commit — the group's recorded base, else what the first new
// member's checkout cut from. If any member fails, every container this call
// reserved is handed back, the leases held before it stay, and the refusal
// says where each container went. repo empty is the pools' default, which
// every pool that has one must agree on.
func LeaseAcquireGroup(ctx context.Context, d LeaseDeps, pools []string, repo, branch string) ([]LeaseView, error) {
	change, err := LeaseAcquireGroupChange(ctx, d, pools, repo, branch)
	return change.Leases, err
}

// LeaseAcquireGroupChange is LeaseAcquireGroup with the rewire it did: once
// the new leases hold the branch, every lease of the branch's group this call
// did not take — held before it, reused or not named — is handed the sibling
// overlay with the new ones in, while the new ones run their own supervisor
// step, which sends them theirs (DP-25). An acquire that took no lease changes
// no membership and rewires nothing.
func LeaseAcquireGroupChange(ctx context.Context, d LeaseDeps, pools []string, repo, branch string) (LeaseChange, error) {
	members, err := resolveGroupPools(ctx, d, pools)
	if err != nil {
		return LeaseChange{}, err
	}
	branch = cmp.Or(branch, leaseDefaultBranch)
	if err := validateLeaseBranch(branch); err != nil {
		return LeaseChange{}, err
	}
	repoURL, becomesDefault, err := resolveGroupRepo(d.StateDir, pools, repo)
	if err != nil {
		return LeaseChange{}, err
	}
	unlock, err := lockLeaseKeys(ctx, d.StateDir, repoURL, branch, pools)
	if err != nil {
		return LeaseChange{}, err
	}
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()
	group, err := leaseGroupRecords(d.StateDir, repoURL, branch)
	if err != nil {
		return LeaseChange{}, err
	}
	var fresh []*groupMember
	for _, m := range members {
		held, found, err := heldLease(ctx, d, m.pool, branch, group)
		if err != nil {
			return LeaseChange{}, err
		}
		if found {
			m.held = &held
			continue
		}
		m.becomesDefault = becomesDefault[m.pool]
		fresh = append(fresh, m)
	}
	// A repository on the Mate's HQ is fetched with the Mate credential, which
	// zcp puts on each pool's GIT_TOKEN before any container is taken (DP-16).
	for _, m := range fresh {
		if m.cred, err = ensurePoolHQCredential(ctx, d, m.rp.svc, repoURL); err != nil {
			return LeaseChange{}, err
		}
	}
	failed := func() bool { return slices.ContainsFunc(fresh, func(m *groupMember) bool { return m.err != nil }) }
	runMembers(fresh, func(m *groupMember) { reserveMember(ctx, d, m, repoURL, branch) })
	if !failed() && d.SSH != nil {
		checkoutMembers(ctx, d, fresh, repoURL, branch, recordedBase(group))
	}
	if failed() {
		return LeaseChange{}, groupRefusal(ctx, d, members)
	}
	// Every member holds the branch: a concurrent acquire of these keys now
	// finds them, while the prepare and the supervisor run.
	unlock()
	locked = false
	var rewired []LeaseRewire
	if d.SSH != nil {
		var rewire sync.WaitGroup
		rewire.Go(func() { rewired = rewireJoined(ctx, d, repoURL, branch, fresh) })
		runMembers(fresh, func(m *groupMember) { afterLeaseCheckout(ctx, d.SSH, d.StateDir, m.rec.alias(), &m.view) })
		rewire.Wait()
	}
	views := make([]LeaseView, 0, len(members))
	for _, m := range members {
		if m.held != nil {
			views = append(views, *m.held)
			continue
		}
		if m.becomesDefault {
			if err := storePoolDefaultRepo(d.StateDir, m.pool, repoURL); err != nil {
				m.view.Note = joinNotes(m.view.Note, fmt.Sprintf("the repository could not be recorded as the pool's default: %v", err))
			}
		}
		views = append(views, m.view)
	}
	return LeaseChange{Leases: views, Rewired: rewired}, nil
}

// rewireJoined hands every lease of the branch's group but the ones this
// acquire took (fresh) the overlay with them in; nil when it took none or the
// group has no other lease.
func rewireJoined(ctx context.Context, d LeaseDeps, repoURL, branch string, fresh []*groupMember) []LeaseRewire {
	if len(fresh) == 0 {
		return nil
	}
	group, err := leaseGroupRecords(d.StateDir, repoURL, branch)
	if err != nil {
		return nil
	}
	joined := map[string]bool{}
	names := make([]string, 0, len(fresh))
	for _, m := range fresh {
		joined[m.rec.alias()] = true
		names = append(names, m.rec.alias())
	}
	others := slices.DeleteFunc(group, func(r leaseRecord) bool { return joined[r.alias()] })
	if len(others) == 0 {
		return nil
	}
	return rewireLeases(ctx, d.SSH, d.StateDir, others, nil, "with "+strings.Join(names, ", "))
}

// resolveGroupPools finds every pool (one service listing), refusing an empty
// list, a pool named twice, a service that is not a pool and a pool zcp finds
// no bearer for — before anything is reserved.
func resolveGroupPools(ctx context.Context, d LeaseDeps, pools []string) ([]*groupMember, error) {
	if len(pools) == 0 {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			"acquire needs pool", "Pass the pool service's hostname as pool, or several as pools")
	}
	seen := map[string]bool{}
	for _, p := range pools {
		if seen[p] {
			return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("pools names '%s' twice", p), "Name each pool once: a group holds one lease per pool")
		}
		seen[p] = true
	}
	services, err := ListProjectServices(ctx, d.Client, d.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	members := make([]*groupMember, 0, len(pools))
	for _, p := range pools {
		svc, err := FindService(services, p)
		if err != nil {
			return nil, err
		}
		rp, err := orchestratorFor(ctx, d, svc)
		if err != nil {
			return nil, err
		}
		members = append(members, &groupMember{pool: p, rp: rp})
	}
	return members, nil
}

// resolveGroupRepo is the clone URL a group acquire clones, as it was given
// (DP-18) — repo, else the default every pool that has one agrees on (agreeing
// by identity, topology.SameRepoURL) — and, per pool, whether the pool has no
// default yet. One pool is resolveLeaseRepo exactly.
func resolveGroupRepo(stateDir string, pools []string, repo string) (string, map[string]bool, error) {
	if len(pools) == 1 {
		cloneURL, becomes, err := resolveLeaseRepo(stateDir, pools[0], repo)
		return cloneURL, map[string]bool{pools[0]: becomes}, err
	}
	defaults := map[string]string{}
	becomes := map[string]bool{}
	for _, p := range pools {
		if have := readPoolDefaultRepo(stateDir, p); have != "" {
			defaults[p] = have
		} else {
			becomes[p] = true
		}
	}
	if strings.TrimSpace(repo) != "" {
		cloneURL, err := validateLeaseRepo(repo)
		return cloneURL, becomes, err
	}
	var agreed string
	var named []string
	for _, p := range pools {
		have, ok := defaults[p]
		if !ok {
			continue
		}
		named = append(named, p+": "+have)
		if agreed == "" {
			agreed = have
		} else if !topology.SameRepoURL(agreed, have) {
			return "", nil, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Pools %s default to different repositories (%s)", strings.Join(pools, ", "), strings.Join(named, "; ")),
				"Pass repo, the https clone URL the branch is of: a group's leases are all of one repository")
		}
	}
	if agreed == "" {
		return "", nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("acquire needs repo: pools %s have no default repository yet", strings.Join(pools, ", ")),
			"Pass the clone URL as repo (https://host/owner/repo); the first acquire on a pool stores it as the pool's default")
	}
	cloneURL, err := validateLeaseRepo(agreed)
	return cloneURL, becomes, err
}

// leaseKeyLockPath is the lock file of one key — the canonical repository
// (case folded, as SameRepoURL compares), the branch and the pool — hashed, so
// a repository URL never names a path.
func leaseKeyLockPath(stateDir, repo, branch, pool string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(topology.CanonicalRepoURL(repo)) + "\x00" + branch + "\x00" + pool))
	return filepath.Join(stateDir, leaseLocksDir, "lease-"+hex.EncodeToString(sum[:16])+".lock")
}

// lockLeaseKeys holds the key of every pool (lockLeaseKey) — taken in the
// pools' sorted order, so two group acquires naming the same pools never wait
// on each other crosswise — and returns the release of them all. The locks
// span the held-lease check, the reservation, the record and the checkout:
// two acquires of one key cannot both reserve, and the one that waited finds
// the other's outcome, while acquires of other keys run on.
func lockLeaseKeys(ctx context.Context, stateDir, repo, branch string, pools []string) (func(), error) {
	if stateDir == "" {
		return nil, errors.New("lock lease keys: no state directory")
	}
	if err := os.MkdirAll(filepath.Join(stateDir, leaseLocksDir), 0o700); err != nil {
		return nil, fmt.Errorf("create lease lock directory: %w", err)
	}
	var releases []func()
	release := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	for _, p := range slices.Sorted(slices.Values(pools)) {
		rel, err := lockLeaseKey(ctx, leaseKeyLockPath(stateDir, repo, branch, p))
		if err != nil {
			release()
			return nil, fmt.Errorf("lock the lease of %s on pool %s: %w", branch, p, err)
		}
		releases = append(releases, rel)
	}
	return release, nil
}

// heldLease is the lease zcp already holds of branch on pool — one of the
// group's records there that the orchestrator still reserves, as status
// reports it (held, or unmounted with what it lacks), the earliest acquired
// when an older zcp left several — marked reused; false when there is none. A
// record of the key whose lease ended (lost), with none held beside it,
// refuses the acquire: zcp keeps one record per repository, branch and pool,
// and only a release drops a lost one (DP-2).
func heldLease(ctx context.Context, d LeaseDeps, pool, branch string, group []leaseRecord) (LeaseView, bool, error) {
	own := map[string]bool{}
	var records []leaseRecord
	for _, r := range group {
		if r.PoolHostname == pool {
			own[r.alias()] = true
			records = append(records, r)
		}
	}
	if len(records) == 0 {
		return LeaseView{}, false, nil
	}
	views, err := listPoolLeases(ctx, d, pool, records, true)
	if err != nil {
		return LeaseView{}, false, err
	}
	var held, lost *LeaseView
	for _, v := range views {
		if !own[v.Alias] {
			continue
		}
		switch v.State {
		case LeaseStateHeld, LeaseStateUnmounted:
			if held == nil || v.AcquiredAt < held.AcquiredAt {
				held = &v
			}
		case LeaseStateLost:
			if lost == nil {
				lost = &v
			}
		}
	}
	switch {
	case held != nil:
		held.Reused = true
		return *held, true, nil
	case lost != nil:
		return LeaseView{}, false, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("zcp's lease of branch %s on pool '%s', %s, is lost (%s), and zcp keeps one lease per repository, branch and pool", branch, pool, lost.Alias, lost.Note),
			fmt.Sprintf("Drop its record, then acquire again: zerops_lease action=release lease=%s", lost.Alias))
	}
	return LeaseView{}, false, nil
}

// recordedBase is the main commit the group's branch was cut from: the earliest
// acquired record's that keeps one; "" when none does (the branch was tracked
// from origin, or the records predate keeping it), so a new branch is cut from
// the current origin/main.
func recordedBase(group []leaseRecord) string {
	for _, r := range group {
		if r.Base != "" {
			return r.Base
		}
	}
	return ""
}

// runMembers runs fn for every member at once and waits for them all; each
// fn touches only its own member.
func runMembers[M any](members []M, fn func(M)) {
	var wg sync.WaitGroup
	for _, m := range members {
		wg.Go(func() { fn(m) })
	}
	wg.Wait()
}

// reserveMember reserves the member's container (DP-3: a clean, started
// container with no expiry, the request run to its own deadline), writes its
// record and gives it its alias and mount. A record that cannot be written
// hands the fresh container straight back.
func reserveMember(ctx context.Context, d LeaseDeps, m *groupMember, repoURL, branch string) {
	rp := m.rp
	// The orchestrator reserves the container before it answers, and an
	// acquire carries no idempotency key: one cut short by its caller would
	// leave a container reserved, with no expiry, for a lease zcp never
	// received. The request runs to its own deadline instead; a caller gone
	// meanwhile makes the steps below fail, which hands the container back.
	acqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), LeaseCallTimeout)
	got, err := rp.api.Acquire(acqCtx, platform.SwarmAcquireRequest{Start: true, Clean: true, LeaseTTLSeconds: 0})
	cancel()
	if err != nil {
		m.err = leaseAcquireError(ctx, d, rp, err)
		return
	}
	rec := leaseRecord{
		PoolHostname: m.pool, PoolServiceID: rp.svc.ID,
		ContainerID: got.Container.ID, ContainerNumber: got.Container.Number, ContainerHostname: got.Container.Hostname,
		Lease: got.Lease.Secret(), Branch: branch, Repo: topology.CanonicalRepoURL(repoURL), AcquiredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeLeaseRecord(d.StateDir, rec); err != nil {
		// Without a record the lease is unusable and, with no expiry, the
		// container would stay reserved for good: hand it straight back (it is
		// clean, so no reset).
		if _, relErr := rp.api.Release(context.WithoutCancel(ctx), rec.ContainerID, rec.Lease, false); relErr != nil {
			m.err = fmt.Errorf("record lease %s: %w (and handing the container back failed: %s stays reserved on %s)", rec.alias(), err, rec.ContainerHostname, m.pool)
			return
		}
		m.err = fmt.Errorf("record lease %s: %w (the container was handed back)", rec.alias(), err)
		return
	}
	m.rec, m.reserved = rec, true
	m.view = rec.view(LeaseStateHeld)
	if note := d.Host.attach(ctx, d, rec); note != "" {
		// The lease is real and stays recorded; zcp init (or the next status)
		// finds the alias or mount missing and restores it.
		m.view.State = LeaseStateUnmounted
		m.view.Note = note
	}
}

// checkoutMembers checks every new member out on the branch: the first alone,
// cut — when the branch is new to origin — from base, else from what its own
// checkout found on origin/main; then the others at once, from the commit the
// first was cut from, so every member starts from one main commit. A member
// cut new keeps that commit in its record. A failed checkout hands its own
// container back (abandonLease) and is the member's error.
func checkoutMembers(ctx context.Context, d LeaseDeps, fresh []*groupMember, repoURL, branch, base string) {
	if len(fresh) == 0 {
		return
	}
	checkout := func(m *groupMember, from string) string {
		cut, err := m.cred.checkout(ctx, d, m.rec, repoURL, branch, from)
		if err != nil {
			m.err = abandonLease(ctx, d, m.rec, err)
			m.reserved = false
			return ""
		}
		if cut != "" {
			m.rec.Base = cut
			if err := writeLeaseRecord(d.StateDir, m.rec); err != nil {
				m.view.Note = joinNotes(m.view.Note, fmt.Sprintf("the commit the branch was cut from could not be recorded (a lease of this branch acquired later is cut from origin/main): %v", err))
			}
		}
		return cut
	}
	if cut := checkout(fresh[0], base); cut != "" {
		base = cut
	}
	if fresh[0].err != nil {
		return
	}
	runMembers(fresh[1:], func(m *groupMember) { checkout(m, base) })
}

// groupRefusal is the refusal of a group acquire a member failed: the first
// failing member's own error (in the pools' order) carrying every other
// member's failure, where each other container this call reserved went once
// handed back, and which leases held before the call stay. A container that
// could not be handed back stays reserved and recorded, and the suggestion
// names the forced release that frees it. One pool is that pool's error.
func groupRefusal(ctx context.Context, d LeaseDeps, members []*groupMember) error {
	var primary error
	var extra, next []string
	for _, m := range members {
		if m.err == nil {
			continue
		}
		if primary == nil {
			primary = m.err
			continue
		}
		extra = append(extra, "also "+errorNote(m.err))
	}
	var back []*groupMember
	for _, m := range members {
		if m.reserved {
			back = append(back, m)
		}
	}
	outcomes := make([]struct{ what, next string }, len(back))
	var wg sync.WaitGroup
	for i, m := range back {
		wg.Go(func() {
			// The caller's context may be what ended the call; the container
			// must still go back.
			view, err := releaseAndDetach(context.WithoutCancel(ctx), d, m.rec, m.rec.alias())
			what, then := handBackOutcome(m.rec.alias(), view, err)
			outcomes[i].what = m.rec.alias() + ", which this call also reserved: " + what
			if err != nil {
				outcomes[i].next = then
			}
		})
	}
	wg.Wait()
	for _, o := range outcomes {
		extra = append(extra, o.what)
		if o.next != "" {
			next = append(next, o.next)
		}
	}
	for _, m := range members {
		if m.held != nil {
			extra = append(extra, m.held.Alias+" was held for this branch before this call and stays held")
		}
	}
	if len(extra) == 0 {
		return primary
	}
	var pe *platform.PlatformError
	if !errors.As(primary, &pe) {
		return fmt.Errorf("%w (%s)", primary, strings.Join(append(extra, next...), "; "))
	}
	out := *pe
	out.Message += "; " + strings.Join(extra, "; ")
	if len(next) > 0 {
		out.Suggestion = strings.TrimSuffix(strings.TrimSpace(out.Suggestion), ".")
		if out.Suggestion != "" {
			out.Suggestion += ". "
		}
		out.Suggestion += strings.Join(next, ". ")
	}
	return &out
}

// recordRepo is the canonical repository a record's lease is of: its own, or
// — for a record that predates keeping it — its pool's default; "" when zcp
// knows neither.
func recordRepo(stateDir string, r leaseRecord) string {
	return topology.CanonicalRepoURL(cmp.Or(r.Repo, readPoolDefaultRepo(stateDir, r.PoolHostname)))
}

// recordBranch is a record's branch; an empty one is main, as an acquire
// without a branch is.
func recordBranch(r leaseRecord) string { return cmp.Or(r.Branch, leaseDefaultBranch) }

// leaseGroupRecords is zcp's records of one repository and branch — the
// group, whatever state the orchestrator gives each — earliest acquired first,
// the alias breaking a tie.
func leaseGroupRecords(stateDir, repo, branch string) ([]leaseRecord, error) {
	records, err := readLeaseRecords(stateDir)
	if err != nil {
		return nil, err
	}
	var group []leaseRecord
	for _, r := range records {
		if recordBranch(r) == branch && topology.SameRepoURL(recordRepo(stateDir, r), repo) {
			group = append(group, r)
		}
	}
	slices.SortFunc(group, func(a, b leaseRecord) int {
		return cmp.Or(cmp.Compare(a.AcquiredAt, b.AcquiredAt), cmp.Compare(a.alias(), b.alias()))
	})
	return group, nil
}

// releaseMember is one lease of a branch's release.
type releaseMember struct {
	rec   leaseRecord
	guard leaseGuardResult
	// work is the unsaved work the guard found; err is why the guard could
	// not tell (a refusal of the branch).
	work unsavedWork
	err  error
	view LeaseView
}

// LeaseReleaseGroup releases every lease zcp holds of one repository and
// branch — the group (DP-17) — all or none unless forced (docs/spec-workflows.md
// §8 DP-23). Unless force is set it reads every lease first, changing nothing,
// and refuses the whole branch when any one holds work a release would lose or
// cannot be read, naming each lease's own state; nothing is released, no record
// dropped. Then each lease goes through the one lease's release step
// (releaseGuarded: a reset, or the container deleted when it cannot be reset,
// DP-15; a lease of a pool that is gone dropped as lost, DP-20). A lease the
// orchestrator will not release stays held and recorded and its view carries
// the failure, while the others go: the result is per lease, earliest acquired
// first, and the same call again releases what is left. repo empty is the one
// repository zcp leases the branch for.
func LeaseReleaseGroup(ctx context.Context, d LeaseDeps, repo, branch string, force bool) ([]LeaseView, error) {
	change, err := LeaseReleaseGroupChange(ctx, d, repo, branch, force)
	return change.Leases, err
}

// LeaseReleaseGroupChange is LeaseReleaseGroup with the rewire it did: the
// release takes every lease of the branch, so no other lease is left to
// rewire before it; a lease the orchestrator would not release stays held,
// and it is handed the overlay without the leases that went (DP-25).
func LeaseReleaseGroupChange(ctx context.Context, d LeaseDeps, repo, branch string, force bool) (LeaseChange, error) {
	if err := validateLeaseBranch(branch); err != nil {
		return LeaseChange{}, err
	}
	group, err := branchLeaseRecords(d.StateDir, repo, branch)
	if err != nil {
		return LeaseChange{}, err
	}
	members := make([]*releaseMember, 0, len(group))
	for _, r := range group {
		members = append(members, &releaseMember{rec: r})
	}
	if !force {
		// The guards reach each container through its alias: put back a
		// stanza another call or a redeploy lost before any of them runs.
		restoreNote := d.Host.refreshAliases(ctx, d)
		runMembers(members, func(m *releaseMember) {
			m.guard, m.work, m.err = inspectLeaseRelease(ctx, d, m.rec)
			m.guard.note = joinNotes(restoreNote, m.guard.note)
		})
		if err := branchReleaseRefusal(d, members, repo, branch); err != nil {
			return LeaseChange{}, err
		}
	}
	again := LeaseReleaseBranchCall(branch, repo, force)
	runMembers(members, func(m *releaseMember) {
		view, err := releaseGuarded(ctx, d, m.rec, m.guard)
		if err != nil {
			view = unreleasedView(ctx, d, m.rec, m.guard, err, again)
		}
		m.view = view
	})
	views := make([]LeaseView, 0, len(members))
	for _, m := range members {
		views = append(views, m.view)
	}
	return LeaseChange{Leases: views, Rewired: rewireLeftHeld(ctx, d, members)}, nil
}

// rewireLeftHeld hands every lease a branch's release could not release the
// overlay without the leases that went (their records are gone); nil when
// every lease went or none did. The caller's context may be what failed the
// releases; the leases left held are rewired all the same.
func rewireLeftHeld(ctx context.Context, d LeaseDeps, members []*releaseMember) []LeaseRewire {
	var held []leaseRecord
	var gone []string
	for _, m := range members {
		if m.view.Error != "" {
			held = append(held, m.rec)
		} else {
			gone = append(gone, m.rec.alias())
		}
	}
	if d.SSH == nil || len(held) == 0 || len(gone) == 0 {
		return nil
	}
	return rewireLeases(context.WithoutCancel(ctx), d.SSH, d.StateDir, held, nil, "without "+strings.Join(gone, ", "))
}

// branchLeaseRecords is what a release of branch releases: zcp's records of
// the branch of repo, earliest acquired first (leaseGroupRecords). repo empty
// is the one repository zcp leases the branch for; a branch it leases for
// several is refused naming each with its leases, and so is a branch it holds
// no lease of. A record whose repository zcp knows neither way is a group of
// one (DP-17), released by itself or by its alias.
func branchLeaseRecords(stateDir, repo, branch string) ([]leaseRecord, error) {
	if strings.TrimSpace(repo) != "" {
		cloneURL, err := validateLeaseRepo(repo)
		if err != nil {
			return nil, err
		}
		group, err := leaseGroupRecords(stateDir, cloneURL, branch)
		if err != nil {
			return nil, err
		}
		if len(group) == 0 {
			return nil, noLeaseOfBranch(branch, cloneURL)
		}
		return group, nil
	}
	records, err := readLeaseRecords(stateDir)
	if err != nil {
		return nil, err
	}
	aliases := map[string][]string{} // repository identity → its leases of the branch
	var keys, named []string
	var last leaseRecord
	for _, r := range records {
		if recordBranch(r) != branch {
			continue
		}
		of := recordRepo(stateDir, r)
		key := strings.ToLower(of)
		if of == "" {
			key, of = "\x00"+r.alias(), "a repository zcp has no record of"
		}
		if _, seen := aliases[key]; !seen {
			keys = append(keys, key)
			named = append(named, of)
		}
		aliases[key] = append(aliases[key], r.alias())
		last = r
	}
	switch len(keys) {
	case 0:
		return nil, noLeaseOfBranch(branch, "")
	case 1:
		if of := recordRepo(stateDir, last); of != "" {
			return leaseGroupRecords(stateDir, of, branch)
		}
		return []leaseRecord{last}, nil
	}
	parts := make([]string, 0, len(keys))
	for i, key := range keys {
		parts = append(parts, named[i]+": "+strings.Join(aliases[key], ", "))
	}
	return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("This zcp leases branch %s for several repositories (%s)", branch, strings.Join(parts, "; ")),
		"Pass repo, the clone URL whose leases of the branch to release, or release one lease by its alias as lease")
}

// noLeaseOfBranch is the refusal of a release of a branch (of repo, when one
// was named) zcp holds no lease of.
func noLeaseOfBranch(branch, repo string) error {
	of := ""
	if repo != "" {
		of = " of " + repo
	}
	return platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("This zcp holds no lease of branch %s%s", branch, of),
		"zerops_lease action=status lists the leases this zcp holds, each with its branch")
}

// branchReleaseRefusal is the refusal of a branch's release, nil when every
// lease's guard passed. A branch of one lease is refused as that lease's own
// release is; several name each lease's own state — the work it holds, why
// it could not be checked, or that it holds none — with how to save the work
// and the forced release of the branch.
func branchReleaseRefusal(d LeaseDeps, members []*releaseMember, repo, branch string) error {
	if !slices.ContainsFunc(members, func(m *releaseMember) bool { return m.err != nil || m.work.any() }) {
		return nil
	}
	if len(members) == 1 {
		if m := members[0]; m.err != nil {
			return m.err
		}
		m := members[0]
		return unsavedWorkRefusal(m.rec.alias(), m.work, IsHQRemote(m.rec.Repo, d.hqAddress()))
	}
	var states, fixes []string
	var unsaved, unchecked, onHQ bool
	for _, m := range members {
		alias := m.rec.alias()
		switch {
		case m.err != nil:
			unchecked = true
			states = append(states, guardFailureText(m.err))
			var pe *platform.PlatformError
			if errors.As(m.err, &pe) && pe.Suggestion != "" {
				fixes = append(fixes, "For "+alias+": "+strings.TrimSuffix(pe.Suggestion, "."))
			}
		case m.work.any():
			unsaved = true
			onHQ = onHQ || IsHQRemote(m.rec.Repo, d.hqAddress())
			states = append(states, alias+" holds "+m.work.describe())
		default:
			states = append(states, alias+" holds no unsaved work")
		}
	}
	var why string
	switch {
	case unsaved && unchecked:
		why = fmt.Sprintf("Branch %s's leases hold work that releasing would lose and not every one could be checked", branch)
	case unsaved:
		why = fmt.Sprintf("Branch %s's leases hold work that releasing would lose", branch)
	default:
		why = fmt.Sprintf("Not every lease of branch %s could be checked for unsaved work", branch)
	}
	if unsaved {
		fixes = append([]string{saveWorkHint(onHQ, "each container that holds it")}, fixes...)
	}
	fixes = append(fixes, "Or release with force=true to discard whatever every lease of the branch holds: "+LeaseReleaseBranchCall(branch, repo, true))
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		why+", so none of them was released: "+strings.Join(states, "; "),
		strings.Join(fixes, ". "))
}

// guardFailureText is a guard's refusal of one lease as a clause of its
// branch's refusal: its message, with what ssh or git answered (an
// SSHExecError keeps it apart from its Error()).
func guardFailureText(err error) string {
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		return err.Error()
	}
	if pe.Cause == nil {
		return pe.Message
	}
	return pe.Message + " (" + sshFailureText(pe.Cause) + ")"
}

// unreleasedView is a lease its branch's release could not release, as status
// reports it — held, or unmounted with what it lacks, since a release unmounts
// first — carrying the failure as its error and the call that releases what is
// left of the branch.
func unreleasedView(ctx context.Context, d LeaseDeps, rec leaseRecord, guard leaseGuardResult, err error, again string) LeaseView {
	v := rec.view(LeaseStateHeld)
	gap := d.Host.gap(ctx, rec.alias())
	if gap != "" {
		v.State = LeaseStateUnmounted
	}
	v.Error = errorNote(err)
	v.Note = joinNotes(guard.note, gap,
		fmt.Sprintf("the release failed, so %s stays reserved and its record is kept; %s releases what is left of the branch (a released lease has no record left)", rec.alias(), again))
	return v
}

// LeaseReleaseBranchCall is the zerops_lease call that releases branch — of
// repo, when one was named — forced or not.
func LeaseReleaseBranchCall(branch, repo string, force bool) string {
	call := "zerops_lease action=release branch=" + branch
	if repo = strings.TrimSpace(repo); repo != "" {
		call += " repo=" + repo
	}
	if force {
		call += " force=true"
	}
	return call
}
