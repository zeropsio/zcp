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
// every other lease of it the recomputed sibling overlay (DP-26).

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
	// supervised: after the checkout a supervisor answers on the lease, so the
	// acquire reads its dev servers at its end.
	supervised bool
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
// step, which sends them theirs (DP-26). An acquire that took no lease changes
// no membership and rewires nothing.
func LeaseAcquireGroupChange(ctx context.Context, d LeaseDeps, pools []string, repo, branch string) (LeaseChange, error) {
	return LeaseAcquireGroupChangeFrom(ctx, d, pools, repo, branch, "")
}

// LeaseAcquireGroupChangeFrom is LeaseAcquireGroupChange with from, the branch
// a branch new to origin is cut from instead of main ("" for main): every new
// member of the group from the one commit the first was cut from (DP-17). A
// from origin does not have is refused and the containers go back; a branch
// origin has, one the group cut before (its recorded base) and one a lease
// already holds are never rewritten — each lease says from was not applied
// (DP-6). from names a branch a lease takes, never the branch itself, and
// never comes with main, which no acquire cuts.
func LeaseAcquireGroupChangeFrom(ctx context.Context, d LeaseDeps, pools []string, repo, branch, from string) (LeaseChange, error) {
	members, err := resolveGroupPools(ctx, d, pools)
	if err != nil {
		return LeaseChange{}, err
	}
	branch = cmp.Or(branch, leaseDefaultBranch)
	if err := validateLeaseBranch(branch); err != nil {
		return LeaseChange{}, err
	}
	if err := validateLeaseFrom(branch, from); err != nil {
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
	// zcp puts on each pool before any container is taken (DP-16).
	for _, m := range fresh {
		if m.cred, err = ensurePoolHQCredential(ctx, d, m.rp.svc, repoURL); err != nil {
			return LeaseChange{}, err
		}
	}
	failed := func() bool { return slices.ContainsFunc(fresh, func(m *groupMember) bool { return m.err != nil }) }
	runMembers(fresh, func(m *groupMember) { reserveMember(ctx, d, m, repoURL, branch) })
	if !failed() && d.SSH != nil {
		checkoutMembers(ctx, d, fresh, repoURL, branch, recordedBase(group), from)
	}
	if failed() {
		return LeaseChange{}, groupRefusal(ctx, d, members)
	}
	if d.Warm != nil {
		// The repository a pool's warm containers get is the one its last
		// lease checked out, as given (DP-32), recorded before the top-up.
		for _, m := range fresh {
			_ = storeWarmCloneRepo(d.StateDir, m.pool, repoURL)
		}
	}
	// Every member holds the branch: a concurrent acquire of these keys now
	// finds them, while the prepare and the supervisor run.
	unlock()
	locked = false
	if d.Warm != nil {
		// Each pool an acquire took a container of is topped up to its warm
		// target, the create in the background (DP-27).
		runMembers(fresh, func(m *groupMember) { m.view.Note = joinNotes(m.view.Note, d.Warm.afterAcquire(ctx, d, m.rp)) })
	}
	var rewired []LeaseRewire
	if d.SSH != nil {
		var rewire sync.WaitGroup
		rewire.Go(func() { rewired = rewireJoined(ctx, d, repoURL, branch, fresh) })
		runMembers(fresh, func(m *groupMember) {
			m.supervised = afterLeaseCheckout(ctx, d, m.rec.alias(), &m.view)
		})
		rewire.Wait()
		// Each lease taken reports its dev servers as the acquire leaves
		// them: one read, nothing waited for (DP-30) — after its pool's
		// public access is on, so the preview server's URL answers.
		runMembers(fresh, func(m *groupMember) {
			if !m.supervised {
				return
			}
			on := false
			if d.PublicAccess != nil {
				var note string
				on, note = d.PublicAccess(ctx, m.rec.alias())
				m.view.Note = joinNotes(m.view.Note, note)
			}
			m.view.DevServers = acquiredDevServers(ctx, d, m.rec, on)
		})
	}
	views := make([]LeaseView, 0, len(members))
	for _, m := range members {
		if m.held != nil {
			if from != "" {
				m.held.Note = joinNotes(m.held.Note, fmt.Sprintf("this lease already held branch %s, so from=%s changed nothing in it", branch, from))
			}
			views = append(views, *m.held)
			continue
		}
		if m.becomesDefault {
			if err := storePoolDefaultRepoOn(d.StateDir, m.pool, repoURL, IsHQRemote(repoURL, d.hqAddress())); err != nil {
				m.view.Note = joinNotes(m.view.Note, fmt.Sprintf("the repository could not be recorded as the pool's default: %v", err))
			}
		}
		views = append(views, m.view)
	}
	return LeaseChange{Leases: views, Rewired: rewired}, nil
}

// rewireJoined hands every lease of the branch's group but the ones this
// acquire took (fresh) the overlay with them in, under the group's lock
// (lockLeaseGroup); nil when it took none or the group has no other lease.
func rewireJoined(ctx context.Context, d LeaseDeps, repoURL, branch string, fresh []*groupMember) []LeaseRewire {
	if len(fresh) == 0 {
		return nil
	}
	unlock, lockErr := lockLeaseGroup(ctx, d.StateDir, topology.CanonicalRepoURL(repoURL), branch)
	if lockErr == nil {
		defer unlock()
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
	why := "with " + strings.Join(names, ", ")
	if lockErr != nil {
		out := make([]LeaseRewire, 0, len(others))
		for _, r := range others {
			out = append(out, LeaseRewire{Alias: r.alias(), Note: fmt.Sprintf("zcp sent %s no overlay %s (%v); %s sends it again", r.alias(), why, lockErr, resendCall(r.alias(), nil))})
		}
		return out
	}
	return rewireLeases(ctx, d, others, nil, why)
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

// leaseGroupLockPath is the lock file of one group (DP-17) — the canonical
// repository, case folded as SameRepoURL compares, and the branch — hashed,
// so a repository URL never names a path.
func leaseGroupLockPath(stateDir, repo, branch string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(topology.CanonicalRepoURL(repo)) + "\x00" + branch))
	return filepath.Join(stateDir, leaseLocksDir, "group-"+hex.EncodeToString(sum[:16])+".lock")
}

// lockLeaseGroup holds the lock of the group of repo and branch (a flock,
// waited for until ctx ends, across this process's calls and every zcp
// process sharing the state directory). A change of the group's membership
// and every rewire of its leases hold it: a release from the rewire of the
// lease's siblings through the drop of its record, an acquire's rewire from
// reading zcp's records through the sends. So a rewire computed while a lease
// is still recorded is never sent after a release dropped it, and two
// releases of one group never each leave a sibling wired to the other's
// container (DP-26). A repository zcp knows neither way ("") is a group of
// one: nothing is held. The caller MUST call the returned release function.
func lockLeaseGroup(ctx context.Context, stateDir, repo, branch string) (func(), error) {
	if stateDir == "" || repo == "" {
		return func() {}, nil
	}
	if err := os.MkdirAll(filepath.Join(stateDir, leaseLocksDir), 0o700); err != nil {
		return nil, fmt.Errorf("create lease lock directory: %w", err)
	}
	unlock, err := lockLeaseKey(ctx, leaseGroupLockPath(stateDir, repo, branch))
	if err != nil {
		return nil, fmt.Errorf("lock the leases of branch %s: %w", branch, err)
	}
	return unlock, nil
}

// leaseReleaseGroupWait bounds a release's wait for its branch's lock: a
// sibling's release holds it across its rewire and its own reset, and the
// release that waited reads its container again once it holds it, so a
// longer wait buys nothing but a call that hangs.
const leaseReleaseGroupWait = 2 * time.Minute

// lockReleaseGroup is lockRecordGroup for a release, waiting at most
// d.GroupLockWait (leaseReleaseGroupWait): a lock still held then refuses the
// release, nothing released, naming again — the call that releases it once
// the other call answered.
func lockReleaseGroup(ctx context.Context, d LeaseDeps, rec leaseRecord, again string) (func(), error) {
	wait := cmp.Or(d.GroupLockWait, leaseReleaseGroupWait)
	lctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	unlock, err := lockRecordGroup(lctx, d.StateDir, rec)
	if err != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		return nil, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("Nothing was released: another zcp call held the leases of branch %s for over %s — a release of another of its leases, across its reset, or an acquire's rewire", recordBranch(rec), wait),
			"Release again once that call has answered: "+again)
	}
	return unlock, err
}

// lockRecordGroup is lockLeaseGroup of rec's group.
func lockRecordGroup(ctx context.Context, stateDir string, rec leaseRecord) (func(), error) {
	return lockLeaseGroup(ctx, stateDir, recordRepo(stateDir, rec), recordBranch(rec))
}

// lockLeaseTargetGroup is lockLeaseGroup of the group of the lease a target
// names (its record's repository, or its pool's default, and its branch).
func lockLeaseTargetGroup(ctx context.Context, stateDir string, lease LeaseTargetInfo) (func(), error) {
	repo := topology.CanonicalRepoURL(cmp.Or(lease.Repo, readPoolDefaultRepo(stateDir, lease.Pool)))
	return lockLeaseGroup(ctx, stateDir, repo, cmp.Or(lease.Branch, leaseDefaultBranch))
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
	pending := map[string]bool{}
	var records []leaseRecord
	for _, r := range group {
		if r.PoolHostname == pool {
			own[r.alias()] = true
			pending[r.alias()] = r.CheckoutPending
			records = append(records, r)
		}
	}
	if len(records) == 0 {
		return LeaseView{}, false, nil
	}
	views, err := listPoolLeases(ctx, d, listingResolver(d), pool, records, true)
	if err != nil {
		return LeaseView{}, false, err
	}
	var held, unverified, unfinished, lost *LeaseView
	for _, v := range views {
		if !own[v.Alias] {
			continue
		}
		switch v.State {
		case LeaseStateHeld, LeaseStateUnmounted, LeaseStateParked:
			switch {
			case v.Unverified:
				if unverified == nil {
					unverified = &v
				}
			case pending[v.Alias]:
				if unfinished == nil {
					unfinished = &v
				}
			case held == nil || v.AcquiredAt < held.AcquiredAt:
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
	case unverified != nil:
		// Maybe another holder's container: never handed back reused.
		return LeaseView{}, false, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("zcp's lease of branch %s on pool '%s', %s, cannot be verified as still zcp's (%s), and zcp keeps one lease per repository, branch and pool", branch, pool, unverified.Alias, unverified.Note),
			fmt.Sprintf("Acquire again once the pool's orchestrator answers: zerops_lease action=status pool=%s asks it again — a lease that is still zcp's is reused, one that is not is reported lost", pool))
	case unfinished != nil:
		// Reserved and recorded, but its checkout never finished: its
		// /var/www is not on the branch, so it is never handed out reused.
		return LeaseView{}, false, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("zcp's lease of branch %s on pool '%s', %s, is still reserved, but its checkout never finished (zcp stopped mid-checkout, or the container could not be handed back after a failed one), so its /var/www is not on the branch; zcp keeps one lease per repository, branch and pool", branch, pool, unfinished.Alias),
			fmt.Sprintf("Release it, then acquire again: zerops_lease action=release lease=%s — it holds nothing of the branch, so force=true is right only when its guard names nothing but what the unfinished checkout left", unfinished.Alias))
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
	// A create in progress refuses it containers-preparing, reserving nothing:
	// it is retried within its own deadline (DP-27).
	acqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), LeaseCallTimeout)
	got, waitNote, err := acquireThroughPreparing(acqCtx, d, rp)
	cancel()
	if err != nil {
		m.err = leaseAcquireError(ctx, d, rp, err)
		return
	}
	rec := leaseRecord{
		PoolHostname: m.pool, PoolServiceID: rp.svc.ID,
		ContainerID: got.Container.ID, ContainerNumber: got.Container.Number, ContainerHostname: got.Container.Hostname,
		Lease: got.Lease.Secret(), Branch: branch, Repo: topology.CanonicalRepoURL(repoURL), AcquiredAt: time.Now().UTC().Format(time.RFC3339),
		// The checkout runs over ssh after the record is written.
		CheckoutPending: d.SSH != nil,
	}
	// The alias file is root-read ssh config whose render refuses the whole
	// set over one record outside the shapes: such a container is never
	// recorded, so it never costs every other lease its stanza.
	switch {
	case !isLeaseAliasShape(rec.alias()):
		err = errors.New("the container's number makes no <pool>-<number> alias")
	case platform.ValidateContainerHostname(rec.ContainerHostname) != nil:
		err = errors.New("its container hostname is not node-id-<n>.runtime.<pool>.zerops")
	default:
		err = writeLeaseRecord(d.StateDir, rec)
	}
	if err != nil {
		// Without a record the lease is unusable and, with no expiry, the
		// container would stay reserved for good: hand it straight back (it is
		// clean, so no reset).
		if _, relErr := rp.api.Release(context.WithoutCancel(ctx), rec.ContainerID, rec.Lease, false); relErr != nil {
			m.err = fmt.Errorf("record lease %s: %w (and handing the container back failed: container %s stays reserved on %s)", rec.alias(), err, rec.ContainerID, m.pool)
			return
		}
		m.err = fmt.Errorf("record lease %s: %w (the container was handed back)", rec.alias(), err)
		return
	}
	m.rec, m.reserved = rec, true
	m.view = rec.view(LeaseStateHeld)
	m.view.Note = waitNote
	if note := d.Host.attach(ctx, d, rec); note != "" {
		// The lease is real and stays recorded; zcp init (or the next status)
		// finds the alias or mount missing and restores it.
		m.view.State = LeaseStateUnmounted
		m.view.Note = joinNotes(m.view.Note, note)
	}
}

// checkoutMembers checks every new member out on the branch: the first alone,
// cut — when the branch is new to origin — from base, else from what its own
// checkout found on origin/main, or on origin/<from> when from names a branch;
// then the others at once, from the commit the first was cut from, so every
// member starts from one commit. A member from did not cut says so
// (fromNotAppliedNote). A member
// cut new keeps that commit in its record. A failed checkout hands its own
// container back (abandonLease) and is the member's error. A member waits for
// the others' reservations and the first checkout, minutes on a pool still
// preparing a container, so the orchestrator is asked right before each
// checkout and before its record is written again whether the container is
// still the member's (DP-2): one that is not is never checked out or written
// over — its record is dropped while it is still its own, the container never
// handed back — and one the orchestrator does not answer about is not checked
// out; either is the member's error.
func checkoutMembers(ctx context.Context, d LeaseDeps, fresh []*groupMember, repoURL, branch, base, from string) {
	if len(fresh) == 0 {
		return
	}
	groupBase := base
	checkout := func(m *groupMember, cutFrom string) string {
		switch v := renewOwnership(ctx, d, m.rp, m.rec); {
		case v.own.lost():
			m.err, m.reserved = memberEnded(ctx, d, m.rec, v.why, "before its checkout, so nothing was checked out in it"), false
			return ""
		case v.own == leaseUnverified:
			m.err = unverifiedLeaseRefusal(m.rec.alias(), "checked out", v)
			return ""
		}
		got, err := m.cred.checkout(ctx, d, m.rec, repoURL, branch, cutFrom, from)
		if err != nil {
			m.err = abandonLease(ctx, d, m.rec, err)
			m.reserved = false
			return ""
		}
		if got.warm == leaseWarmDiscarded {
			m.view.Note = joinNotes(m.view.Note, discardedWarmNote)
		}
		m.view.Note = joinNotes(m.view.Note, fromNotAppliedNote(branch, from, groupBase, got.base))
		if got.base != "" || got.stableNumber != 0 || m.rec.CheckoutPending {
			// Recorded before the keys are let go and before any rewire, so
			// every overlay computed from here on finds the new lease's
			// preview port, and the next acquire of the key finds the
			// checkout finished.
			if v := renewOwnership(ctx, d, m.rp, m.rec); v.own.lost() {
				m.err, m.reserved = memberEnded(ctx, d, m.rec, v.why, "during its checkout, which may have reached the container's next holder"), false
				return ""
			}
			m.rec.Base, m.rec.StableNumber, m.rec.CheckoutPending = got.base, got.stableNumber, false
			switch kept, err := rewriteLeaseRecord(d.StateDir, m.rec); {
			case err != nil:
				m.view.Note = joinNotes(m.view.Note, fmt.Sprintf("the finished checkout, the commit the branch was cut from and the container's stable number could not be recorded (a lease of this branch acquired later is cut from origin/main, no sibling gets this lease's preview URL, and the next acquire of this branch on the pool is refused until this lease is released): %v", err))
			case kept != "":
				// Still the member's by the renew: handed back with the
				// group, released only once a renew agrees again.
				m.err = platform.NewPlatformError(platform.ErrPrerequisiteMissing,
					fmt.Sprintf("Lease %s was checked out, but zcp did not write its record again: %s", m.rec.alias(), kept),
					"zerops_lease action=status shows which lease the alias names now")
				return ""
			}
		}
		return got.base
	}
	if cut := checkout(fresh[0], base); cut != "" {
		base = cut
	}
	if fresh[0].err != nil {
		return
	}
	runMembers(fresh[1:], func(m *groupMember) { checkout(m, base) })
}

// validateLeaseFrom refuses a from that is not a branch a lease takes, that
// is the branch itself, or that comes with main, which no acquire cuts; ""
// (cut from main) passes.
func validateLeaseFrom(branch, from string) error {
	switch {
	case from == "":
		return nil
	case validateLeaseBranch(from) != nil:
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			"from is not a branch name a lease takes",
			"Name the branch a new branch is cut from: letters, digits and . _ / - only, starting with a letter or digit (e.g. from=pool-previews)")
	case branch == leaseDefaultBranch:
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("from cuts a new branch, and the acquire names %s, which no acquire cuts", leaseDefaultBranch),
			fmt.Sprintf("Pass the new branch as branch (zerops_lease action=acquire branch=<new branch> from=%s), or leave from out to lease %s", from, leaseDefaultBranch))
	case from == branch:
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			"from names the branch itself, and a branch is never cut from itself",
			"Leave from out to check the branch out as origin has it, or name the branch a new one is cut from")
	}
	return nil
}

// fromNotAppliedNote says why a lease checked out with from was not cut from
// it — the branch is on origin (tracked: cut is "") or the group cut it
// before (groupBase) — and "" when it was, or when there was no from.
func fromNotAppliedNote(branch, from, groupBase, cut string) string {
	switch {
	case from == "":
		return ""
	case groupBase != "":
		return fmt.Sprintf("branch %s was cut before for this branch's other leases (from commit %s), so this lease starts from that commit too; from=%s was not applied", branch, groupBase, from)
	case cut == "":
		return fmt.Sprintf("branch %s is already on origin, so it was checked out as it is there; from=%s cuts only a branch origin does not have yet", branch, from)
	}
	return ""
}

// memberEnded is the error of a group acquire's member whose lease ended
// outside zcp (why) — when says when, and what it means — once its record is
// dropped while it is still its own, with its mount and alias stanza
// (dropLostLease). The container is free or another holder's, so it is never
// handed back.
func memberEnded(ctx context.Context, d LeaseDeps, rec leaseRecord, why, when string) error {
	msg := fmt.Sprintf("Lease %s ended outside zcp %s (%s)", rec.alias(), when, why)
	switch v, err := dropLostLease(context.WithoutCancel(ctx), d, rec, why); {
	case err != nil:
		msg += fmt.Sprintf("; its record could not be dropped: %v", err)
	case v.recordKept != "":
		msg += "; " + v.recordKept
	default:
		msg += "; its record is dropped"
	}
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing, msg, leaseGoneNext)
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
	// wake is what the release did to read a parked lease (DP-28).
	wake leaseWake
	// verdict is whose its container is, as the orchestrator answered before
	// anything else (readLeaseOwnership). lost is why the lease is no longer
	// zcp's — its verdict, or what the release's own steps found (endedOnCall:
	// a start refused held-by-other that a renew confirms, or the renew right
	// before its guard) — "" while it may still be.
	verdict     leaseVerdict
	lost        string
	endedOnCall bool
}

// LeaseReleaseGroup releases every lease zcp holds of one repository and
// branch — the group (DP-17) — all or none unless forced (docs/spec-workflows.md
// §8 DP-23). It first reads whose each container is (readLeaseOwnership,
// DP-2): a lease that is no longer zcp's is dropped as lost, never read and
// never released, and holds nothing back; one the orchestrator does not
// answer about refuses the branch unless forced, and is not released even
// then. Unless force is set it reads every lease first,
// changing nothing, and refuses the whole branch when any one holds work a
// release would lose or cannot be read, naming each lease's own state;
// nothing is released, no record dropped. Then each lease goes through the one lease's release step
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
// and it is handed the overlay without the leases that went (DP-26).
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
	// Check before mutate (O3): whose each container is, all at once, before
	// any is read or released. A lease that is no longer zcp's is dropped
	// below as lost — never read, never released — and does not hold the
	// branch back.
	runMembers(members, func(m *releaseMember) {
		if m.verdict = readLeaseOwnership(ctx, d, m.rec); m.verdict.own.lost() {
			m.lost = m.verdict.why
		}
	})
	if !force {
		// The guards reach each container through its alias: put back a
		// stanza another call or a redeploy lost before any of them runs.
		restoreNote := d.Host.refreshAliases(ctx, d)
		runMembers(members, func(m *releaseMember) {
			switch {
			case m.lost != "":
				return
			case m.verdict.own == leaseUnverified:
				m.err = unverifiedLeaseRefusal(m.rec.alias(), "released", m.verdict)
				return
			}
			// A parked lease is started first so its guard can read it
			// (DP-28).
			if m.wake, m.err = wakeParkedForGuard(ctx, d, m.rec, m.verdict); m.err != nil {
				if why, ended := leaseEnded(m.err); ended {
					// Not zcp's container any more: never re-parked.
					m.lost, m.endedOnCall, m.err, m.wake = why, true, nil, leaseWake{}
				}
				return
			}
			m.guard, m.work, m.err = inspectLeaseRelease(ctx, d, m.rec)
			m.guard.note = joinNotes(restoreNote, m.wake.note(), m.guard.note)
			m.guard.woken = m.wake.started
		})
		if err := branchReleaseRefusal(d, members, repo, branch); err != nil {
			// A lease the release's own steps found ended is lost whatever
			// becomes of the branch: those records go now, with their alias
			// and mount.
			return LeaseChange{}, unsavedRefusal(ctx, d, members, dropEndedOnCall(ctx, d, members, reparkMembers(ctx, d, members, err)))
		}
	}
	// The releases and the rewire of what stays held run under the group's
	// lock (lockLeaseGroup): no overlay naming a lease that goes is sent after
	// it went.
	again := LeaseReleaseBranchCall(branch, repo, force)
	unlock, err := lockReleaseGroup(ctx, d, group[0], again)
	if err != nil {
		return LeaseChange{}, dropEndedOnCall(ctx, d, members, reparkMembers(ctx, d, members, err))
	}
	defer unlock()
	if !force {
		// The guards read the containers before the wait for the lock, which
		// can outlast work landing in one (a conversation sharing a lease):
		// every lease still to be released is read again before any is
		// released, and work in one refuses the branch (DP-4).
		runMembers(members, func(m *releaseMember) {
			if m.lost != "" || m.guard.lost != "" {
				return
			}
			var again leaseGuardResult
			again, m.work, m.err = inspectLeaseRelease(ctx, d, m.rec)
			m.guard.lost, m.guard.note = again.lost, joinNotes(m.guard.note, again.note)
		})
		if err := branchReleaseRefusal(d, members, repo, branch); err != nil {
			return LeaseChange{}, unsavedRefusal(ctx, d, members, dropEndedOnCall(ctx, d, members, reparkMembers(ctx, d, members, err)))
		}
	}
	runMembers(members, func(m *releaseMember) {
		var view LeaseView
		var err error
		switch {
		case m.verdict.own == leaseUnverified:
			// Forced, but the orchestrator did not say whose the container
			// is: nothing is released (a free container's release resets it).
			err = unverifiedLeaseRefusal(m.rec.alias(), "released", m.verdict)
		case m.lost != "":
			view, err = dropLostLease(ctx, d, m.rec, m.lost)
		default:
			view, err = releaseGuarded(ctx, d, m.rec, m.guard)
		}
		if err != nil {
			view = unreleasedView(ctx, d, m.rec, m.guard, err, again)
			if m.wake.started {
				// Not released: the lease it started stays parked.
				text, parked := repark(context.WithoutCancel(ctx), d, m.rec)
				view.Note = joinNotes(view.Note, text)
				if parked {
					view.State, view.ParkedAt = LeaseStateParked, m.rec.ParkedAt
				}
			}
		}
		m.view = view
	})
	views := make([]LeaseView, 0, len(members))
	for _, m := range members {
		views = append(views, m.view)
	}
	return LeaseChange{Leases: views, Rewired: rewireLeftHeld(ctx, d, members)}, nil
}

// dropEndedOnCall drops, with its alias and mount, the record of every member
// of a refused branch release whose lease the release's own steps found ended
// outside zcp (endedOnCall: final whatever becomes of the branch), and adds to
// cause what failed to go, an alias that names another lease by now, and —
// since no release rewires the branch — the leases of the branch that may
// still run wired to the dropped container (DP-26).
func dropEndedOnCall(ctx context.Context, d LeaseDeps, members []*releaseMember, cause error) error {
	var notes []string
	for _, m := range members {
		if !m.endedOnCall {
			continue
		}
		alias := m.rec.alias()
		switch v, err := dropLostLease(context.WithoutCancel(ctx), d, m.rec, m.lost); {
		case err != nil:
			notes = append(notes, fmt.Sprintf("%s's record could not be dropped: %v", alias, err))
		case v.recordKept != "":
			notes = append(notes, v.recordKept)
		}
		if wired := siblingsMayBeWired(d.StateDir, m.rec); wired != "" {
			notes = append(notes, alias+": "+wired)
		}
	}
	return withNote(cause, strings.Join(notes, "; "))
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
	return rewireLeases(context.WithoutCancel(ctx), d, held, nil, "without "+strings.Join(gone, ", "))
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

// ErrNoLeaseOfBranch is the cause of a branch release's refusal when zcp
// holds no lease of the branch: a caller to whom that means the branch is
// released already (`zcp lease release`, DP-35) tells it by errors.Is.
var ErrNoLeaseOfBranch = errors.New("no lease of the branch")

// noLeaseOfBranch is the refusal of a release of a branch (of repo, when one
// was named) zcp holds no lease of.
func noLeaseOfBranch(branch, repo string) error {
	of := ""
	if repo != "" {
		of = " of " + repo
	}
	pe := platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("This zcp holds no lease of branch %s%s", branch, of),
		"zerops_lease action=status lists the leases this zcp holds, each with its branch")
	pe.Cause = ErrNoLeaseOfBranch
	return pe
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
		case m.endedOnCall:
			states = append(states, alias+" is no longer zcp's ("+m.lost+"), so its record is dropped now")
		case m.lost != "":
			states = append(states, alias+" is no longer zcp's ("+m.lost+") and goes with the branch's release, unread")
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
	gap := ""
	switch {
	case rec.Parked && !guard.woken:
		// Released as it stood, stopped, and the release failed: still
		// parked (DP-28).
		v.State, v.ParkedAt = LeaseStateParked, rec.ParkedAt
	default:
		if gap = d.Host.gap(ctx, rec.alias()); gap != "" {
			v.State = LeaseStateUnmounted
		}
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

// leaseReleaseCall is the zerops_lease call that releases the lease at alias,
// forced or not.
func leaseReleaseCall(alias string, force bool) string {
	call := "zerops_lease action=release lease=" + alias
	if force {
		call += " force=true"
	}
	return call
}
