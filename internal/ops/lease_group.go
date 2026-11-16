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
	// missing is why zcp read no bearer for the pool before the keys were
	// held; acquireBearers writes one and fills rp's client. nil once it has
	// one.
	missing *missingBearer
	// held is the lease zcp held for the key before this call (returned,
	// reused); nil for a member this call leases.
	held *LeaseView
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
	// checkedOut: a lease zcp held whose checkout had been cut short, checked
	// out by heldLease already (resumeHeldCheckout).
	checkedOut bool
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
// says where each container went. repo empty is the pools' recorded
// repository, which every pool that records one must agree on.
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
// (DP-6). from names a branch a lease takes, never the branch itself. The
// repository's default branch — also the branch of an acquire that names none
// — is refused at once (refuseMainLease); a lease of it held from before is
// still released, parked and listed.
func LeaseAcquireGroupChangeFrom(ctx context.Context, d LeaseDeps, pools []string, repo, branch, from string) (LeaseChange, error) {
	branch = cmp.Or(branch, leaseDefaultBranch)
	if err := refuseMainLease(branch); err != nil {
		return LeaseChange{}, err
	}
	members, err := resolveGroupPools(ctx, d, pools)
	if err != nil {
		return LeaseChange{}, err
	}
	if err := validateLeaseBranch(branch); err != nil {
		return LeaseChange{}, err
	}
	if err := validateLeaseFrom(branch, from); err != nil {
		return LeaseChange{}, err
	}
	repoURL, err := resolveGroupRepo(d, pools, repo)
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
	if err := acquireBearers(ctx, d, members); err != nil {
		return LeaseChange{}, err
	}
	group, err := leaseGroupRecords(d.StateDir, repoURL, branch)
	if err != nil {
		return LeaseChange{}, err
	}
	var fresh []*groupMember
	base := recordedBase(group)
	for _, m := range members {
		held, found, checkedOut, err := heldLease(ctx, d, m.pool, repoURL, branch, base, from, group)
		if err != nil {
			return LeaseChange{}, err
		}
		if found {
			m.held = &held
			continue
		}
		if checkedOut != nil {
			// Its checkout never cut the branch, and it ran now: the lease is
			// finished as one this call takes, and handed back as one if
			// another member fails.
			m.rec, m.reserved, m.checkedOut = *checkedOut, true, true
			m.view = checkedOut.view(LeaseStateHeld)
			m.view.Note = "zcp held this lease, but its checkout had never put it on the branch (zcp stopped mid-checkout), so this acquire ran the checkout"
			base = cmp.Or(base, checkedOut.Base)
		}
		fresh = append(fresh, m)
	}
	failed := func() bool { return slices.ContainsFunc(fresh, func(m *groupMember) bool { return m.err != nil }) }
	runMembers(fresh, func(m *groupMember) {
		if !m.reserved {
			reserveMember(ctx, d, m, repoURL, branch)
		}
	})
	if !failed() && d.SSH != nil {
		checkoutMembers(ctx, d, slices.DeleteFunc(slices.Clone(fresh), func(m *groupMember) bool { return m.checkedOut }), repoURL, branch, base, from)
	}
	if failed() {
		return LeaseChange{}, groupRefusal(ctx, d, members)
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
		// No sibling is known, so none was sent the overlay: each lease
		// taken says so, rather than a rewire that silently did not run.
		out := make([]LeaseRewire, 0, len(fresh))
		for _, m := range fresh {
			out = append(out, LeaseRewire{Alias: m.rec.alias(), Note: fmt.Sprintf("zcp could not read the leases of branch %s (%v), so no other lease of it was handed an overlay with %s", branch, err, m.rec.alias())})
		}
		return out
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
// list, a pool named twice and a service that is not a pool before anything is
// reserved; a pool zcp reads no bearer for is marked missing, for
// acquireBearers to write under the keys' locks.
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
		if err := checkPoolType(svc); err != nil {
			return nil, err
		}
		token, missing, err := readPoolBearer(ctx, d, svc)
		if err != nil {
			return nil, err
		}
		m := &groupMember{pool: p, rp: &resolvedPool{svc: svc}, missing: missing}
		if missing == nil {
			m.rp = orchestratorClient(d, svc, token)
		}
		members = append(members, m)
	}
	return members, nil
}

// acquireBearers writes the bearer reference of every member zcp read none
// for (EnsureBearer, the one writer), called with the keys' locks held —
// the lock order is key, then bearer; nothing takes them the other way. The
// writer's own error, with its class, refuses the acquire before anything is
// reserved; a writer that answers no bearer (written, not resolved) refuses
// it as a pool with no image yet (noImageYetError).
func acquireBearers(ctx context.Context, d LeaseDeps, members []*groupMember) error {
	for _, m := range members {
		if m.missing == nil {
			continue
		}
		token, err := EnsureBearer(ctx, d, m.pool)
		if err != nil {
			return err
		}
		if token == "" {
			return noImageYetError(m.pool)
		}
		m.rp, m.missing = orchestratorClient(d, m.rp.svc, token), nil
	}
	return nil
}

// resolveGroupRepo is the clone URL a group acquire clones, as it was given
// (DP-18) — repo, else the repository every pool that records one agrees on
// (agreeing by identity, topology.SameRepoURL). One pool is resolveLeaseRepo
// exactly.
func resolveGroupRepo(d LeaseDeps, pools []string, repo string) (string, error) {
	if len(pools) == 1 || strings.TrimSpace(repo) != "" {
		return resolveLeaseRepo(d, pools[0], repo)
	}
	var agreed string
	var named []string
	for _, p := range pools {
		have := d.poolRepository(p).Repo
		if have == "" {
			continue
		}
		named = append(named, p+": "+have)
		if agreed == "" {
			agreed = have
		} else if !topology.SameRepoURL(agreed, have) {
			return "", platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Pools %s record different repositories (%s)", strings.Join(pools, ", "), strings.Join(named, "; ")),
				"Pass repo, the https clone URL the branch is of: a group's leases are all of one repository")
		}
	}
	if agreed == "" {
		return "", platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("acquire needs repo: pools %s have no repository recorded", strings.Join(pools, ", ")),
			"Pass the clone URL as repo (https://host/owner/repo). A pool's repository is the one its stand-up records: zerops_standup records it for a pool that has none")
	}
	return validateLeaseRepo(agreed)
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

// lockReleaseGroup is lockRecordGroup for a release of recs (records of one
// group), and inside it each record's key lock (lockReleaseKeys) — order group,
// then key: an acquire lets go of its keys before it takes the group's lock
// (rewireJoined), so no cycle forms, and an acquire that would answer a lease
// reused waits until the release has dropped it or refused. Both waits share
// one bound, d.GroupLockWait (leaseReleaseGroupWait): a lock still held then
// refuses the release, nothing released, naming again — the call that
// releases it once the other call answered.
func lockReleaseGroup(ctx context.Context, d LeaseDeps, recs []leaseRecord, again string) (func(), error) {
	wait := cmp.Or(d.GroupLockWait, leaseReleaseGroupWait)
	lctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	branch := recordBranch(recs[0])
	unlockGroup, err := lockRecordGroup(lctx, d.StateDir, recs[0])
	if err != nil {
		return nil, releaseLockRefusal(ctx, err, fmt.Sprintf("another zcp call held the leases of branch %s for over %s — a release of another of its leases, across its reset, or an acquire's rewire", branch, wait), again)
	}
	unlockKeys, err := lockReleaseKeys(lctx, d.StateDir, recs)
	if err != nil {
		unlockGroup()
		return nil, releaseLockRefusal(ctx, err, fmt.Sprintf("an acquire of branch %s held its lease's key for over %s — across its reservation and checkout", branch, wait), again)
	}
	return func() {
		unlockKeys()
		unlockGroup()
	}, nil
}

// releaseLockRefusal is a release's refusal of a lock wait that ran out (held
// says by whom); any other error of the lock is err itself.
func releaseLockRefusal(ctx context.Context, err error, held, again string) error {
	if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		pe := platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			"Nothing was released: "+held,
			"Release again once that call has answered: "+again)
		pe.Cause = err
		return pe
	}
	return err
}

// lockReleaseKeys holds the key lock of every record in recs — its
// repository, branch and pool, as an acquire of it takes it (leaseKeyLockPath)
// — each once, in lockLeaseKeys' order (the pools sorted), so a release and a
// group acquire never wait on each other crosswise; none for a record that
// names no repository, which the release refused before. It returns the
// release of them all.
func lockReleaseKeys(ctx context.Context, stateDir string, recs []leaseRecord) (func(), error) {
	type key struct{ repo, branch, pool string }
	var keys []key
	for _, r := range recs {
		if repo := recordRepo(r); repo != "" {
			keys = append(keys, key{repo, recordBranch(r), r.PoolHostname})
		}
	}
	slices.SortFunc(keys, func(a, b key) int {
		return cmp.Or(strings.Compare(a.repo, b.repo), strings.Compare(a.branch, b.branch), strings.Compare(a.pool, b.pool))
	})
	var releases []func()
	release := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	if len(keys) > 0 {
		if err := os.MkdirAll(filepath.Join(stateDir, leaseLocksDir), 0o700); err != nil {
			return nil, fmt.Errorf("create lease lock directory: %w", err)
		}
	}
	// Two records can name one key in different spellings of the repository
	// (case): one lock path is taken once — flock on a second open of it
	// would wait on this call's own hold.
	locked := map[string]bool{}
	for _, k := range keys {
		path := leaseKeyLockPath(stateDir, k.repo, k.branch, k.pool)
		if locked[path] {
			continue
		}
		locked[path] = true
		rel, err := lockLeaseKey(ctx, path)
		if err != nil {
			release()
			return nil, fmt.Errorf("lock the lease of %s on pool %s: %w", k.branch, k.pool, err)
		}
		releases = append(releases, rel)
	}
	return release, nil
}

// lockRecordGroup is lockLeaseGroup of rec's group.
func lockRecordGroup(ctx context.Context, stateDir string, rec leaseRecord) (func(), error) {
	if recordRepo(rec) == "" {
		return nil, recordWithoutRepo(rec.alias())
	}
	return lockLeaseGroup(ctx, stateDir, recordRepo(rec), recordBranch(rec))
}

// lockLeaseTargetGroup is lockLeaseGroup of the group of the lease a target
// names (its record's repository and its branch).
func lockLeaseTargetGroup(ctx context.Context, stateDir string, lease LeaseTargetInfo) (func(), error) {
	if topology.CanonicalRepoURL(lease.Repo) == "" {
		return nil, recordWithoutRepo(lease.Alias)
	}
	return lockLeaseGroup(ctx, stateDir, topology.CanonicalRepoURL(lease.Repo), cmp.Or(lease.Branch, leaseDefaultBranch))
}

// heldLease is the lease zcp already holds of branch on pool — one of the
// group's records there that the orchestrator still reserves, as status
// reports it (held, or unmounted with what it lacks), the earliest acquired
// when an older zcp left several — marked reused; false when there is none. A
// record of the key whose lease ended (lost), with none held beside it,
// refuses the acquire: zcp keeps one record per repository, branch and pool,
// and only a release drops a lost one (DP-2).
//
// Whether the held lease's checkout finished is the container's fact, never
// the record's (leaseRecord): it runs under the acquire's key lock, so the
// container decides (resumeHeldCheckout). One whose branch was cut is reused,
// only the checkout's steps that change no work run in it again; one holding
// nothing of the agent's — zcp stopped mid-checkout — gets the whole checkout
// now and is returned as checkedOut, for the acquire to finish as a lease it
// takes (prepare, supervisor, dev servers); one holding anything else is
// refused. A parked lease, and a zcp with no ssh, cannot be asked: the lease
// is reused as it stands.
func heldLease(ctx context.Context, d LeaseDeps, pool, repoURL, branch, base, from string, group []leaseRecord) (held LeaseView, found bool, checkedOut *leaseRecord, err error) {
	own := map[string]leaseRecord{}
	var records []leaseRecord
	for _, r := range group {
		if r.PoolHostname == pool {
			own[r.alias()] = r
			records = append(records, r)
		}
	}
	if len(records) == 0 {
		return LeaseView{}, false, nil, nil
	}
	views, err := listPoolLeases(ctx, d, listingResolver(d), pool, records, true)
	if err != nil {
		return LeaseView{}, false, nil, err
	}
	var heldView, unverified, lost *LeaseView
	for _, v := range views {
		if _, ok := own[v.Alias]; !ok {
			continue
		}
		switch v.State {
		case LeaseStateHeld, LeaseStateUnmounted, LeaseStateParked:
			switch {
			case v.Unverified:
				if unverified == nil {
					unverified = &v
				}
			case heldView == nil || v.AcquiredAt < heldView.AcquiredAt:
				heldView = &v
			}
		case LeaseStateLost:
			if lost == nil {
				lost = &v
			}
		}
	}
	switch {
	case heldView != nil:
		if d.SSH != nil && heldView.State != LeaseStateParked {
			rec, got, note, err := resumeHeldCheckout(ctx, d, own[heldView.Alias], repoURL, branch, base, from, heldView.State == LeaseStateUnmounted)
			switch {
			case err != nil:
				return LeaseView{}, false, nil, err
			case !got.resumed:
				return LeaseView{}, false, &rec, nil
			}
			heldView.Note = joinNotes(heldView.Note, note)
		}
		heldView.Reused = true
		return *heldView, true, nil, nil
	case unverified != nil:
		// Maybe another holder's container: never handed back reused.
		return LeaseView{}, false, nil, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("zcp's lease of branch %s on pool '%s', %s, cannot be verified as still zcp's (%s), and zcp keeps one lease per repository, branch and pool", branch, pool, unverified.Alias, unverified.Note),
			fmt.Sprintf("Acquire again once the pool's orchestrator answers: zerops_lease action=status pool=%s asks it again — a lease that is still zcp's is reused, one that is not is reported lost", pool))
	case lost != nil:
		return LeaseView{}, false, nil, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("zcp's lease of branch %s on pool '%s', %s, is lost (%s), and zcp keeps one lease per repository, branch and pool", branch, pool, lost.Alias, lost.Note),
			fmt.Sprintf("Drop its record, then acquire again: zerops_lease action=release lease=%s", lost.Alias))
	}
	return LeaseView{}, false, nil, nil
}

// resumeHeldCheckout runs the checkout of rec, a lease of branch zcp holds,
// under the acquire's key lock as one script that decides in the container
// (resumeLeaseCheckout): the branch cut already — only the steps that change
// no work ran (resumed) — or nothing of the agent's there, so the whole
// checkout ran, cut from base. What it learnt that the record lacks — the
// base a new branch was cut from, the container's stable number — is
// recorded. fixAlias puts back the lease's ssh alias first (a lease status
// reports unmounted may lack it). Any failure refuses the acquire, the lease
// and its record as they were: an ssh that does not answer is never read as
// a checkout that did not happen.
func resumeHeldCheckout(ctx context.Context, d LeaseDeps, rec leaseRecord, repoURL, branch, base, from string, fixAlias bool) (leaseRecord, leaseCheckedOut, string, error) {
	note := ""
	if fixAlias {
		note = d.Host.refreshAliases(ctx, d)
	}
	got, err := resumeLeaseCheckout(ctx, d, rec, repoURL, branch, base, from)
	if err != nil {
		return rec, got, note, err
	}
	if got.warm == leaseWarmDiscarded {
		note = joinNotes(note, discardedWarmNote)
	}
	note = joinNotes(note, leaseUnsetNote(got.unset))
	changed := false
	if rec.Base == "" && got.base != "" {
		rec.Base, changed = got.base, true
	}
	if rec.StableNumber == 0 && got.stableNumber != 0 {
		rec.StableNumber, changed = got.stableNumber, true
	}
	if changed {
		if kept, werr := rewriteLeaseRecord(d.StateDir, rec); werr != nil || kept != "" {
			note = joinNotes(note, fmt.Sprintf("the commit the branch was cut from and the container's stable number could not be recorded (%s)", cmp.Or(kept, errorNote(werr))))
		}
	}
	return rec, got, note, nil
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
		got, err := checkoutLease(ctx, d, m.rec, repoURL, branch, cutFrom, from)
		if err != nil {
			m.err = abandonLease(ctx, d, m.rec, err)
			m.reserved = false
			return ""
		}
		if got.warm == leaseWarmDiscarded {
			m.view.Note = joinNotes(m.view.Note, discardedWarmNote)
		}
		m.view.Note = joinNotes(m.view.Note, leaseUnsetNote(got.unset))
		m.view.Note = joinNotes(m.view.Note, fromNotAppliedNote(branch, from, groupBase, got.base))
		if got.base != "" || got.stableNumber != 0 {
			// Recorded before the keys are let go and before any rewire, so
			// every overlay computed from here on finds the new lease's
			// preview port.
			if v := renewOwnership(ctx, d, m.rp, m.rec); v.own.lost() {
				m.err, m.reserved = memberEnded(ctx, d, m.rec, v.why, "during its checkout, which may have reached the container's next holder"), false
				return ""
			}
			m.rec.Base, m.rec.StableNumber = got.base, got.stableNumber
			switch kept, err := rewriteLeaseRecord(d.StateDir, m.rec); {
			case err != nil:
				m.view.Note = joinNotes(m.view.Note, fmt.Sprintf("the commit the branch was cut from and the container's stable number could not be recorded (a lease of this branch acquired later is cut from origin/main, and no sibling gets this lease's preview URL): %v", err))
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

// refuseMainLease refuses an acquire of the repository's default branch
// (leaseDefaultBranch): no lease holds main — work happens on a branch of the
// agent's own, and the stage gets main through the end-of-work check.
func refuseMainLease(branch string) error {
	if branch != leaseDefaultBranch {
		return nil
	}
	return platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("A lease never holds %s, the repository's default branch: nothing was acquired", leaseDefaultBranch),
		fmt.Sprintf("Work on a branch of your own: zerops_lease action=acquire pool=<pool> branch=<new branch> cuts it from %s. The stage is deployed from %s through the end-of-work check, never through a lease", leaseDefaultBranch, leaseDefaultBranch))
}

// validateLeaseFrom refuses a from that is not a branch a lease takes or that
// is the branch itself; "" (cut from main) passes.
func validateLeaseFrom(branch, from string) error {
	switch {
	case from == "":
		return nil
	case validateLeaseBranch(from) != nil:
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			"from is not a branch name a lease takes",
			"Name the branch a new branch is cut from: letters, digits and . _ / - only, starting with a letter or digit (e.g. from=pool-previews)")
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

// recordRepo is the canonical repository a record's lease is of; "" for a
// record that predates keeping it, which every verb that keys by it refuses
// (recordWithoutRepo).
func recordRepo(r leaseRecord) string {
	return topology.CanonicalRepoURL(r.Repo)
}

// recordWithoutRepo is the refusal of a verb that keys a lease by its
// repository when the lease's record names none: it predates zcp keeping one,
// and keying it by "" would take a second container of its branch or skip it
// on a release.
func recordWithoutRepo(alias string) error {
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		fmt.Sprintf("Lease %s has a record that names no repository (it predates zcp keeping one), so zcp cannot tell which repository's lease of its branch it is", alias),
		"Tell the person: the record .zcp/state/leases/"+alias+".json needs its repository (the pool's ServiceMeta remoteUrl, canonical form) written by hand before zcp acquires or releases on its branch. Nothing was changed")
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
		if recordBranch(r) == branch && recordRepo(r) == "" {
			return nil, recordWithoutRepo(r.alias())
		}
		if recordBranch(r) == branch && topology.SameRepoURL(recordRepo(r), repo) {
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

// LeaseReleaseGroupChange is LeaseReleaseGroup with the rewire it did. A
// lease joining after the group was read is rewired before the captured
// members leave; one the orchestrator would not release is rewired after
// those that went (DP-26).
func LeaseReleaseGroupChange(ctx context.Context, d LeaseDeps, repo, branch string, force bool) (LeaseChange, error) {
	return leaseReleaseGroupChange(ctx, d, repo, branch, force, nil)
}

// LeaseReleaseGroupConfirmed captures the branch's records before confirm
// reads the evidence for releasing them. A lease joining during that read is
// not among the records released. A declined confirmation changes nothing.
func LeaseReleaseGroupConfirmed(ctx context.Context, d LeaseDeps, repo, branch string, confirm func([]LeaseTargetInfo) bool) (LeaseChange, error) {
	if confirm == nil {
		return LeaseChange{}, errors.New("confirm branch release: confirmation is required")
	}
	return leaseReleaseGroupChange(ctx, d, repo, branch, false, confirm)
}

func leaseReleaseGroupChange(ctx context.Context, d LeaseDeps, repo, branch string, force bool, confirm func([]LeaseTargetInfo) bool) (LeaseChange, error) {
	if err := validateLeaseBranch(branch); err != nil {
		return LeaseChange{}, err
	}
	group, err := branchLeaseRecords(d.StateDir, repo, branch)
	if err != nil {
		return LeaseChange{}, err
	}
	if confirm != nil {
		targets := make([]LeaseTargetInfo, 0, len(group))
		for _, r := range group {
			targets = append(targets, r.targetInfo())
		}
		if !confirm(targets) {
			return LeaseChange{}, nil
		}
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
			m.guard, m.work, m.err = inspectUnsavedWork(ctx, d, m.rec)
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
	unlock, err := lockReleaseGroup(ctx, d, group, again)
	if err != nil {
		return LeaseChange{}, dropEndedOnCall(ctx, d, members, reparkMembers(ctx, d, members, err))
	}
	defer unlock()
	// A lease can join after the group was read. It is not being released,
	// so remove the departing members from its overlay before they go.
	siblings, rewired, err := rewireGroupSurvivors(ctx, d, members)
	if err != nil {
		return LeaseChange{}, dropEndedOnCall(ctx, d, members, reparkMembers(ctx, d, members, err))
	}
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
			again, m.work, m.err = inspectUnsavedWork(ctx, d, m.rec)
			m.guard.lost, m.guard.note = again.lost, joinNotes(m.guard.note, again.note)
		})
		if err := branchReleaseRefusal(d, members, repo, branch); err != nil {
			err = dropEndedOnCall(ctx, d, members, reparkMembers(ctx, d, members, err))
			after, note := rewireGroupAfterFailure(ctx, d, members, siblings, rewired, false)
			err = withNote(err, note)
			return LeaseChange{Rewired: mergeGroupRewires(rewired, after)}, unsavedRefusal(ctx, d, members, err)
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
			view, err = releaseGuarded(ctx, d, m.rec, m.guard, parkedContainer(m.verdict.container))
		}
		if err != nil {
			view = unreleasedView(ctx, d, m.rec, m.guard, parkedContainer(m.verdict.container), err, again)
			if m.wake.started {
				// Not released: the lease it started stays parked.
				text, parked := repark(context.WithoutCancel(ctx), d, m.rec)
				view.Note = joinNotes(view.Note, text)
				if parked {
					view.State = LeaseStateParked
				}
			}
		}
		m.view = view
	})
	failed := false
	for _, m := range members {
		failed = failed || m.view.Error != ""
	}
	if failed {
		after, note := rewireGroupAfterFailure(ctx, d, members, siblings, rewired, true)
		rewired = mergeGroupRewires(rewired, after)
		if note != "" {
			for _, m := range members {
				if m.view.Error != "" {
					m.view.Error += "; " + note
					break
				}
			}
		}
	}
	views := make([]LeaseView, 0, len(members))
	for _, m := range members {
		views = append(views, m.view)
	}
	return LeaseChange{Leases: views, Rewired: rewired}, nil
}

// rewireGroupSurvivors updates leases that joined after the release read the
// group. Both the record's lease and container must match before its alias is
// treated as leaving: an alias reused by another lease stays in the overlay.
// The caller holds the group lock through the rewire and release.
func rewireGroupSurvivors(ctx context.Context, d LeaseDeps, members []*releaseMember) ([]leaseRecord, []LeaseRewire, error) {
	if d.SSH == nil || len(members) == 0 {
		return nil, nil, nil
	}
	first := members[0].rec
	group, err := leaseGroupRecords(d.StateDir, recordRepo(first), recordBranch(first))
	if err != nil {
		return nil, nil, fmt.Errorf("read branch leases for rewire: %w", err)
	}
	candidates := make(map[string]leaseRecord, len(members))
	for _, m := range members {
		candidates[m.rec.alias()] = m.rec
	}
	leaving := make(map[string]bool, len(members))
	var siblings []leaseRecord
	for _, r := range group {
		candidate, ok := candidates[r.alias()]
		if ok && candidate.ContainerID == r.ContainerID && candidate.Lease == r.Lease {
			leaving[r.alias()] = true
		} else {
			siblings = append(siblings, r)
		}
	}
	if len(siblings) == 0 || len(leaving) == 0 {
		return nil, nil, nil
	}
	names := make([]string, 0, len(leaving))
	for _, m := range members {
		if leaving[m.rec.alias()] {
			names = append(names, m.rec.alias())
		}
	}
	return siblings, rewireLeases(ctx, d, siblings, leaving, "without "+strings.Join(names, ", ")), nil
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

// rewireGroupAfterFailure reconciles the survivors after a refused or partial
// group release. A retained record is included in an overlay only when its
// captured lease is still confirmed: a release can succeed while its record
// remains, and its container may already belong to somebody else.
func rewireGroupAfterFailure(ctx context.Context, d LeaseDeps, members []*releaseMember, siblings []leaseRecord, before []LeaseRewire, afterRelease bool) ([]LeaseRewire, string) {
	if d.SSH == nil || len(members) == 0 {
		return nil, ""
	}
	ctx = context.WithoutCancel(ctx)
	first := members[0].rec
	group, err := leaseGroupRecords(d.StateDir, recordRepo(first), recordBranch(first))
	if err != nil {
		note := fmt.Sprintf("zcp could not read branch leases to restore sibling wiring (%v)", err)
		for _, r := range siblings {
			note += "; " + resendCall(r.alias(), nil) + " sends it again"
		}
		if len(siblings) > 0 {
			return []LeaseRewire{{Alias: siblings[0].alias(), Note: note}}, note
		}
		return nil, note
	}
	current := make(map[string]leaseRecord, len(group))
	for _, r := range group {
		current[r.alias()] = r
	}
	verdicts := make([]leaseVerdict, len(members))
	indexes := make([]int, len(members))
	for i := range indexes {
		indexes[i] = i
	}
	runMembers(indexes, func(i int) {
		r := members[i].rec
		if now, ok := current[r.alias()]; ok && now.ContainerID == r.ContainerID && now.Lease == r.Lease {
			verdicts[i] = readLeaseOwnership(ctx, d, r)
		}
	})
	leaving := map[string]bool{}
	ours := map[string]bool{}
	changedMembership := false
	for i, m := range members {
		r := m.rec
		now, ok := current[r.alias()]
		if !ok || now.ContainerID != r.ContainerID || now.Lease != r.Lease {
			changedMembership = true
			continue
		}
		if verdicts[i].own == leaseOurs {
			ours[r.alias()] = true
		} else {
			leaving[r.alias()] = true
			changedMembership = true
		}
	}
	changed := map[string]bool{}
	for _, r := range before {
		if r.changed {
			changed[r.Alias] = true
		}
	}
	var targets []leaseRecord
	for _, r := range group {
		if changed[r.alias()] || (afterRelease && changedMembership && ours[r.alias()]) {
			targets = append(targets, r)
		}
	}
	if len(targets) == 0 {
		return nil, ""
	}
	after := rewireLeases(ctx, d, targets, leaving, "with verified held leases")
	var problems []string
	for _, r := range after {
		if r.Note != "" {
			problems = append(problems, r.Note)
		}
	}
	note := "zcp handed it back to " + strings.Join(aliasesOf(targets), ", ") + " with verified held leases"
	if len(problems) > 0 {
		note = "zcp could not hand it back to every sibling: " + strings.Join(problems, "; ")
	}
	return after, note
}

// mergeGroupRewires reports each sibling's final overlay and retains notes
// from both the pre-release rewire and its reconciliation.
func mergeGroupRewires(before, after []LeaseRewire) []LeaseRewire {
	for _, r := range after {
		i := slices.IndexFunc(before, func(v LeaseRewire) bool { return v.Alias == r.Alias })
		if i < 0 {
			before = append(before, r)
			continue
		}
		r.Note = joinNotes(before[i].Note, r.Note)
		r.Millis += before[i].Millis
		before[i] = r
	}
	return before
}

// branchLeaseRecords is what a release of branch releases: zcp's records of
// the branch of repo, earliest acquired first (leaseGroupRecords). repo empty
// is the one repository zcp leases the branch for; a branch it leases for
// several is refused naming each with its leases, and so is a branch it holds
// no lease of. A record of the branch that names no repository refuses it
// (recordWithoutRepo).
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
		of := recordRepo(r)
		if of == "" {
			return nil, recordWithoutRepo(r.alias())
		}
		key := strings.ToLower(of)
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
		return leaseGroupRecords(stateDir, recordRepo(last), branch)
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
// left of the branch. parked is whether the orchestrator listed its container
// stopped when the release asked it (parkedContainer).
func unreleasedView(ctx context.Context, d LeaseDeps, rec leaseRecord, guard leaseGuardResult, parked bool, err error, again string) LeaseView {
	v := rec.view(LeaseStateHeld)
	gap := ""
	switch {
	case parked && !guard.woken:
		// Released as it stood, stopped, and the release failed: still
		// parked (DP-28).
		v.State = LeaseStateParked
	default:
		if gap = d.Host.gap(ctx, rec.alias()); gap != "" {
			v.State = LeaseStateUnmounted
		}
	}
	v.Error, v.Failure = errorNote(err), err
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
