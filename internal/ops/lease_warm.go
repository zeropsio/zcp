package ops

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// Each pool keeps a warm container, one an acquire takes at once where a
// create takes about 16 s on a container pool and minutes on a VM pool. Warm
// containers come from the orchestrator's create — never from minContainers,
// whose containers have no primary snapshot and cannot be reset (DP-15) — and
// from releases: a release keeps its container, reset, while the pool has
// fewer warm containers besides it than its target and the container runs the
// pool's current version; otherwise it removes the container with its own
// lease, so free containers never pile up and an outdated version leaves the
// pool. After an acquire or a release zcp tops the pool up through the
// orchestrator's create, in the background. The keep-or-remove decision and
// the top-up take one flock per pool and read the pool inside it; deciders
// outside it (another container's zcp, another consumer) can overshoot the
// target by one, which a later release removes. zcp deletes only the container
// it is releasing, with that lease — never a batch, never force, never by
// lowering maxContainers (docs/spec-workflows.md §8 DP-27).

// LeaseWarmTarget is how many warm containers each pool keeps (the owner's
// answer: one per pool).
const LeaseWarmTarget = 1

// leaseWarmLockWait is how long a release waits for its pool's warm lock — a
// concurrent release's reset takes 7–13 s, a create about 16 s on a container
// pool — before it keeps its container with a plain reset.
const leaseWarmLockWait = 20 * time.Second

// An acquire refused containers-preparing reserved nothing: it is retried
// every leasePreparingRetry, for up to leasePreparingWait on a container
// pool and to the acquire's own deadline on a VM pool.
const (
	leasePreparingRetry = 2 * time.Second
	leasePreparingWait  = time.Minute
)

// leaseWarmDir is where each pool's last warm outcome is kept, apart from the
// lease records (<stateDir>/warm/<pool>.json).
const leaseWarmDir = "warm"

// What zcp last did to keep a pool's warm containers (LeaseWarmOutcome.Outcome).
const (
	// LeaseWarmCreating: a create is running in the background.
	LeaseWarmCreating = "creating"
	// LeaseWarmCreated: the orchestrator created Count containers; an acquire
	// may have taken one before zcp could count it.
	LeaseWarmCreated = "created"
	// LeaseWarmReady: the pool had its warm containers; nothing was created.
	LeaseWarmReady = "ready"
	// LeaseWarmFull: nothing was created, the pool holds its maximum.
	LeaseWarmFull = "full"
	// LeaseWarmFailed: the create failed; Note says why.
	LeaseWarmFailed = "failed"
	// LeaseWarmUnread: the pool could not be read, so nothing was created.
	LeaseWarmUnread = "unread"
)

// LeaseWarm is the warm-container policy of the lease tool. A LeaseDeps
// without one (nil) keeps no warm container: every release resets its
// container and nothing is created — the envelope's lease reader, the scale
// gate and the verify check read pools only.
type LeaseWarm struct {
	// Target is how many warm containers each pool keeps (LeaseWarmTarget);
	// 0 removes every released container.
	Target int
	// Go runs a background create; nil runs it on a goroutine of its own.
	Go func(func())
	// LockWait bounds a release's wait for its pool's warm lock; zero means
	// leaseWarmLockWait.
	LockWait time.Duration
}

func (w *LeaseWarm) spawn(fn func()) {
	if w.Go != nil {
		w.Go(fn)
		return
	}
	go fn()
}

// LeaseWarmOutcome is what zcp last did to keep a pool's warm containers,
// kept per pool and reported by status, so a create that failed in the
// background is never invisible.
type LeaseWarmOutcome struct {
	At      string `json:"at"`              // when, RFC 3339
	Outcome string `json:"outcome"`         // LeaseWarmCreating, LeaseWarmCreated, ...
	Count   int    `json:"count,omitempty"` // containers being created, or created
	Note    string `json:"note,omitempty"`
}

// LeasePoolWarm is one pool's warm containers as a status read them.
type LeasePoolWarm struct {
	Pool   string `json:"pool"`
	Target int    `json:"target"`
	// Ready is the pool's warm containers, counted by id; nil when they could
	// not be counted (Note says why), never a guessed 0.
	Ready *int `json:"ready,omitempty"`
	// Preparing is what a create zcp started is making now.
	Preparing int `json:"preparing,omitempty"`
	// Last is what zcp last did for the pool; nil when it never did anything.
	Last *LeaseWarmOutcome `json:"last,omitempty"`
	Note string            `json:"note,omitempty"`
}

// Notes a release or an acquire carries.
const (
	keptWarmNote      = "kept as the pool's warm container (released with a reset)"
	warmPreparingNote = "a warm container is being prepared for the pool's next acquire (zerops_lease action=status reports it)"
	// leasePreparingSuggestion is what to do about an acquire still refused
	// containers-preparing once zcp stopped retrying.
	leasePreparingSuggestion = "A container is still being created: the pool's warm container, which zcp creates after an acquire or a release, or one another consumer of the pool asked for. " +
		"zcp retried the acquire for a minute (on a VM pool, to the acquire's own deadline) and reserved nothing; acquire again once it exists — zerops_lease action=status shows the pool's warm containers"
)

// errWarmLockBusy: another zcp call holds the pool's warm lock.
var errWarmLockBusy = errors.New("another zcp call holds the pool's warm lock")

// warmLockPath is the pool's warm lock, beside the per-key acquire locks.
func warmLockPath(stateDir, pool string) string {
	return filepath.Join(stateDir, leaseLocksDir, "warm-"+pool+".lock")
}

// lockPoolWarm takes the pool's warm lock (lockLeaseKey: a flock, across this
// process's calls and every zcp process of the container alike), waiting up
// to wait — 0 tries once — and errWarmLockBusy when it stays held. The caller
// MUST call the returned release function.
func lockPoolWarm(ctx context.Context, stateDir, pool string, wait time.Duration) (func(), error) {
	if stateDir == "" {
		return nil, errors.New("lock pool warm: no state directory")
	}
	if !isLeaseAliasShape(pool + "-0") {
		return nil, fmt.Errorf("lock pool warm: %q is not a pool hostname", pool)
	}
	if err := os.MkdirAll(filepath.Join(stateDir, leaseLocksDir), 0o700); err != nil {
		return nil, fmt.Errorf("create lease lock directory: %w", err)
	}
	lctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	unlock, err := lockLeaseKey(lctx, warmLockPath(stateDir, pool))
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return nil, errWarmLockBusy
		}
		return nil, fmt.Errorf("lock pool %s warm: %w", pool, err)
	}
	return unlock, nil
}

func warmOutcomePath(stateDir, pool string) string {
	return filepath.Join(stateDir, leaseWarmDir, pool+".json")
}

// writeWarmOutcome keeps the pool's last outcome, atomically.
func writeWarmOutcome(stateDir, pool string, o LeaseWarmOutcome) error {
	if stateDir == "" || !isLeaseAliasShape(pool+"-0") {
		return fmt.Errorf("warm outcome of %q: no state directory or not a pool hostname", pool)
	}
	dir := filepath.Join(stateDir, leaseWarmDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	data, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("marshal warm outcome: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".warm-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	defer os.Remove(tmp.Name()) // best effort: already gone after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write warm outcome: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close warm outcome: %w", err)
	}
	if err := os.Rename(tmp.Name(), warmOutcomePath(stateDir, pool)); err != nil {
		return fmt.Errorf("rename warm outcome: %w", err)
	}
	return nil
}

// readWarmOutcome is the pool's last outcome; nil when there is none or it
// cannot be read.
func readWarmOutcome(stateDir, pool string) *LeaseWarmOutcome {
	if stateDir == "" || !isLeaseAliasShape(pool+"-0") {
		return nil
	}
	raw, err := os.ReadFile(warmOutcomePath(stateDir, pool))
	if err != nil {
		return nil
	}
	var o LeaseWarmOutcome
	if json.Unmarshal(raw, &o) != nil || o.Outcome == "" {
		return nil
	}
	return &o
}

// record keeps what the warm step just did for the pool. A record that cannot
// be written only leaves status without it.
func (w *LeaseWarm) record(d LeaseDeps, pool, outcome string, count int, note string) {
	_ = writeWarmOutcome(d.StateDir, pool, LeaseWarmOutcome{At: d.now().UTC().Format(time.RFC3339), Outcome: outcome, Count: count, Note: note})
}

// warmCandidate is a container that is warm if a read by id finds a snapshot:
// free, ACTIVE, current, clean and reachable.
func warmCandidate(c platform.SwarmContainer) bool {
	return c.LockState == "" && c.Status == platform.SwarmContainerActive && c.Current && !c.Dirty && c.UnreachableSince == ""
}

// isWarm is a container an acquire can take at once and a reset restores: a
// candidate with a snapshot — the platform-made ones of minContainers have
// none and never count.
func isWarm(c platform.SwarmContainer) bool { return warmCandidate(c) && c.Snapshots >= 1 }

// poolWarmRead is a pool as the warm step read it.
type poolWarmRead struct {
	total int // containers in every state, deleting ones included
	ready int // warm containers, the released one aside
	// mine is the released container as listed (nil when it is not), and
	// mineCounted that its snapshots were read by id.
	mine        *platform.SwarmContainer
	mineCounted bool
}

// containerByID reads one container by id within the read budget.
func (d LeaseDeps) containerByID(ctx context.Context, rp *resolvedPool, id string) (platform.SwarmContainer, error) {
	ctx, cancel := context.WithTimeout(ctx, cmp.Or(d.ReadTimeout, leaseReadTimeout))
	defer cancel()
	return rp.api.Container(ctx, id)
}

// readPoolWarm lists the pool and reads its warm candidates by id (lists omit
// snapshots), counting up to need warm containers (0: all of them) — mineID,
// the container being released, aside and read by id for its own snapshots.
// A candidate whose read fails is not counted.
func (d LeaseDeps) readPoolWarm(ctx context.Context, rp *resolvedPool, mineID string, need int) (poolWarmRead, error) {
	list, err := d.containers(ctx, rp)
	if err != nil {
		return poolWarmRead{}, err
	}
	read := poolWarmRead{total: len(list)}
	for _, c := range list {
		if mineID != "" && c.ID == mineID {
			read.mine = &c
			continue
		}
		if !warmCandidate(c) || (need > 0 && read.ready >= need) {
			continue
		}
		if got, err := d.containerByID(ctx, rp, c.ID); err == nil && isWarm(got) {
			read.ready++
		}
	}
	if read.mine != nil {
		if got, err := d.containerByID(ctx, rp, mineID); err == nil {
			read.mine.Snapshots, read.mineCounted = got.Snapshots, true
		}
	}
	return read, nil
}

// releasedReset: the release handed the container back reset, so it stays in
// the pool (not lost, not deleted, not removed).
func releasedReset(v LeaseView) bool {
	return v.State == LeaseStateReleased && !v.containerDeleted && !v.containerRemoved
}

// keepOrRemove is a reset release under the warm policy, once the guard passed
// (or force) and the pool holds the lease's record: under the pool's warm lock
// it reads the pool, keeps the container with the reset release (reset) when
// the pool has fewer warm containers besides it than the target and it is
// current, and otherwise deletes it with its own lease; then it tops the pool
// up from the same read. A container without a primary snapshot goes the DP-15
// way first. A delete refused for any reason but a lease the orchestrator no
// longer knows falls back to the reset release; a lock it cannot take in time,
// or a pool it cannot read, keeps the container with the reset release.
func (w *LeaseWarm) keepOrRemove(ctx context.Context, d LeaseDeps, rp *resolvedPool, rec leaseRecord, reset func() (LeaseView, error)) (LeaseView, error) {
	unlock, err := lockPoolWarm(ctx, d.StateDir, rp.svc.Name, cmp.Or(w.LockWait, leaseWarmLockWait))
	if err != nil {
		// The wait may outlast the lease; the reset release asks the
		// orchestrator right before it (releaseOnOrchestrator), so a
		// container released outside zcp meanwhile is dropped, never reset.
		why := fmt.Sprintf("the pool's warm container could not be decided (%v)", err)
		if errors.Is(err, errWarmLockBusy) {
			why = "another zcp call was still deciding or creating the pool's warm container"
		}
		return keptWithReset(reset, why)
	}
	handed := false
	defer func() {
		if !handed {
			unlock()
		}
	}()
	read, err := d.readPoolWarm(ctx, rp, rec.ContainerID, w.Target)
	if err != nil {
		return keptWithReset(reset, "the pool's containers could not be read ("+err.Error()+")")
	}
	view, kept, err := w.decideRelease(ctx, d, rp, rec, read, reset)
	if err != nil {
		return LeaseView{}, err
	}
	if kept {
		read.ready++
	}
	var note string
	note, handed = w.topUp(ctx, d, rp, read, unlock)
	view.Note = joinNotes(view.Note, note)
	return view, nil
}

// keptWithReset releases with the reset when the warm decision could not be
// made: the container stays in the pool, and the view says why.
func keptWithReset(reset func() (LeaseView, error), why string) (LeaseView, error) {
	view, err := reset()
	if err != nil {
		return LeaseView{}, err
	}
	if releasedReset(view) {
		view.keptWarm = true
		view.Note = joinNotes(view.Note, "kept in the pool with a reset, since "+why+"; a later release removes a container beyond the pool's warm one")
	}
	return view, nil
}

// decideRelease keeps the released container (the reset release) or removes it
// with its own lease, reporting whether it stays in the pool as a warm one. A
// container the renew of its lease, under the warm lock, says is not this
// lease's (DP-2) is neither reset nor deleted: the orchestrator checks no
// lease on a free container, so the reset would take the pool's warm
// container from under another acquire; the record is dropped as lost. One
// the orchestrator does not answer about is left alone, the record kept. A
// delete refused held-by-other drops the record too.
func (w *LeaseWarm) decideRelease(ctx context.Context, d LeaseDeps, rp *resolvedPool, rec leaseRecord, read poolWarmRead, reset func() (LeaseView, error)) (LeaseView, bool, error) {
	own, why := leaseGone, recreatedWhy
	mine := read.mine
	if rec.PoolServiceID == rp.svc.ID {
		v := renewOwnership(ctx, d, rp, rec)
		if v.own == leaseUnverified {
			return LeaseView{}, false, unverifiedLeaseRefusal(rec.alias(), "released", v)
		}
		own, why = v.own, v.why
		if mine == nil {
			// The listing lacked the container the renew answered for:
			// decide on the renew's answer, its snapshots unread.
			mine = &v.container
		}
	}
	if own.lost() {
		view, err := dropLeaseRecord(d.StateDir, rec, LeaseStateLost, lostDropNote(why))
		return view, false, err
	}
	switch {
	case read.mineCounted && mine.Snapshots == 0:
		// No primary snapshot: the reset is refused and the container deleted
		// the DP-15 way, with its own wording, before any warm decision.
		view, err := reset()
		if err != nil {
			return LeaseView{}, false, err
		}
		kept := releasedReset(view) && mine.Current
		if kept {
			view.keptWarm = true
			view.Note = joinNotes(view.Note, keptWarmNote)
		}
		return view, kept, nil
	case mine.Current && read.ready < w.Target:
		view, err := reset()
		if err != nil {
			return LeaseView{}, false, err
		}
		if !releasedReset(view) {
			return view, false, nil
		}
		view.keptWarm = true
		view.Note = joinNotes(view.Note, keptWarmNote)
		return view, true, nil
	}
	why = removeWhy(mine.Current, w.Target)
	err := rp.api.Delete(ctx, rec.ContainerID, rec.Lease)
	if err == nil {
		view, dropErr := dropLeaseRecord(d.StateDir, rec, LeaseStateReleased, "the container was removed with this lease rather than reset: "+why)
		view.containerRemoved = true
		return view, false, dropErr
	}
	var se *platform.SwarmError
	if errors.As(err, &se) && (se.Reason == platform.SwarmReasonUnknownLease || se.Reason == platform.SwarmReasonContainerNotFound) {
		view, dropErr := dropLeaseRecord(d.StateDir, rec, LeaseStateLost, "the orchestrator no longer held this container for this lease, so there was nothing to release; the record is dropped")
		return view, false, dropErr
	}
	if isHeldByOther(err) {
		view, dropErr := dropLeaseRecord(d.StateDir, rec, LeaseStateLost, lostDropNote(heldByOtherWhy))
		return view, false, dropErr
	}
	view, rerr := reset()
	if rerr != nil {
		return LeaseView{}, false, rerr
	}
	if !releasedReset(view) {
		return view, false, nil
	}
	refusal := err.Error()
	if se != nil && se.Message != "" {
		refusal = se.Message
	}
	kept := mine.Current
	view.keptWarm = kept
	view.Note = joinNotes(view.Note, fmt.Sprintf("%s, but the orchestrator refused to remove it (%s), so it was released with a reset instead and stays in the pool", why, refusal))
	return view, kept, nil
}

// removeWhy is why a released container is removed rather than kept.
func removeWhy(current bool, target int) string {
	switch {
	case !current:
		return "it ran an outdated version of the pool's app (the pool's new containers run the current one)"
	case target == 0:
		return "the pool keeps no warm container"
	case target == 1:
		return "the pool already has its warm container"
	}
	return fmt.Sprintf("the pool already has its %d warm containers", target)
}

// afterAcquire tops the pool up after an acquire took a container: under the
// pool's warm lock — or, while another call holds it, in the background once
// it is free — and says when a warm container is being prepared.
func (w *LeaseWarm) afterAcquire(ctx context.Context, d LeaseDeps, rp *resolvedPool) string {
	pool := rp.svc.Name
	unlock, err := lockPoolWarm(ctx, d.StateDir, pool, 0)
	switch {
	case errors.Is(err, errWarmLockBusy):
		w.spawn(func() {
			bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), LeaseCallTimeout)
			defer cancel()
			unlock, err := lockPoolWarm(bctx, d.StateDir, pool, LeaseCallTimeout)
			if err != nil {
				return
			}
			w.topUpRead(bctx, d, rp, unlock)
		})
		return ""
	case err != nil:
		return ""
	}
	return w.topUpRead(ctx, d, rp, unlock)
}

// topUpRead reads the pool and tops it up, under the warm lock unlock
// releases.
func (w *LeaseWarm) topUpRead(ctx context.Context, d LeaseDeps, rp *resolvedPool, unlock func()) string {
	read, err := d.readPoolWarm(ctx, rp, "", w.Target)
	if err != nil {
		w.record(d, rp.svc.Name, LeaseWarmUnread, 0, "the pool's containers could not be read, so nothing was created: "+err.Error())
		unlock()
		return ""
	}
	note, handed := w.topUp(ctx, d, rp, read, unlock)
	if !handed {
		unlock()
	}
	return note
}

// topUp creates the pool's shortfall — min(target − warm, maximum − total),
// containers in every state counted — through the orchestrator's create, in
// the background, and records what it did. read is the pool as read under the
// warm lock, which unlock releases: when a create starts, the background
// create takes the lock over (handed) and releases it once the orchestrator
// answered, so no other decision reads the pool meanwhile.
func (w *LeaseWarm) topUp(ctx context.Context, d LeaseDeps, rp *resolvedPool, read poolWarmRead, unlock func()) (note string, handed bool) {
	pool := rp.svc.Name
	short := w.Target - read.ready
	if short <= 0 {
		w.record(d, pool, LeaseWarmReady, 0, fmt.Sprintf("the pool had %d warm container(s), its target being %d", read.ready, w.Target))
		return "", false
	}
	limits, err := d.poolLimits(ctx, rp)
	if err != nil {
		w.record(d, pool, LeaseWarmUnread, 0, "the pool's limits could not be read, so nothing was created: "+err.Error())
		return "", false
	}
	n := min(short, limits.MaximumContainerCount-read.total)
	if n <= 0 {
		w.record(d, pool, LeaseWarmFull, 0, fmt.Sprintf("nothing was created: the pool holds its maximum of %d containers", limits.MaximumContainerCount))
		return "", false
	}
	w.record(d, pool, LeaseWarmCreating, n, "")
	w.spawn(func() {
		defer unlock()
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), LeaseCallTimeout)
		defer cancel()
		created, err := rp.api.Create(cctx, n)
		var se *platform.SwarmError
		switch {
		case err == nil:
			w.record(d, pool, LeaseWarmCreated, len(created), "an acquire may take it at once; zcp never treats it as its own")
		case errors.As(err, &se) && se.Reason == platform.SwarmReasonPoolCapacityExceeded:
			w.record(d, pool, LeaseWarmFull, 0, "the orchestrator refused the create: "+se.Message)
		case errors.As(err, &se) && se.Message != "":
			w.record(d, pool, LeaseWarmFailed, 0, "the orchestrator refused the create: "+se.Message)
		default:
			w.record(d, pool, LeaseWarmFailed, 0, "the create failed: "+err.Error())
		}
	})
	return warmPreparingNote, true
}

// acquireThroughPreparing is the acquire of a container, retried while the
// orchestrator answers containers-preparing — a refusal that reserved nothing
// — every PreparingRetry, for up to leasePreparingWait on a container pool and
// to ctx's deadline on a VM pool. note says how long it waited, "" when it did
// not.
func acquireThroughPreparing(ctx context.Context, d LeaseDeps, rp *resolvedPool) (platform.SwarmAcquired, string, error) {
	req := platform.SwarmAcquireRequest{Start: true, Clean: true, LeaseTTLSeconds: 0}
	every := cmp.Or(d.PreparingRetry, leasePreparingRetry)
	vm := topology.IsPoolVMType(rp.svc.ServiceStackTypeInfo.ServiceStackTypeVersionName)
	start := d.now()
	var waited time.Duration
	retried := false
	for {
		got, err := rp.api.Acquire(ctx, req)
		var se *platform.SwarmError
		if err == nil || !errors.As(err, &se) || se.Reason != platform.SwarmReasonContainersPreparing {
			note := ""
			if retried && err == nil {
				note = fmt.Sprintf("the pool was still preparing a container (most likely its warm container being created), so the acquire waited %s for it", waited.Round(time.Second))
			}
			return got, note, err
		}
		waited = d.now().Sub(start)
		if !vm && waited >= leasePreparingWait {
			return got, "", err
		}
		timer := time.NewTimer(every)
		select {
		case <-ctx.Done():
			timer.Stop()
			return got, "", err
		case <-timer.C:
		}
		retried = true
	}
}

// leaseStatusWarm is each of pools' warm containers, for a status under the
// warm policy (nil without it): its target, its warm containers counted by id,
// what a create zcp started is making now, and what zcp last did. It only
// reads.
func (d LeaseDeps) leaseStatusWarm(ctx context.Context, pools []string) []LeasePoolWarm {
	if d.Warm == nil || len(pools) == 0 {
		return nil
	}
	services, err := ListProjectServices(ctx, d.Client, d.ProjectID)
	if err != nil {
		return nil
	}
	out := make([]LeasePoolWarm, 0, len(pools))
	for _, name := range pools {
		svc, err := FindService(services, name)
		if err != nil {
			continue // gone: it keeps nothing warm
		}
		pw := LeasePoolWarm{Pool: name, Target: d.Warm.Target}
		pw.Last, pw.Preparing = d.warmOutcomeView(name)
		rp, err := orchestratorFor(ctx, d, svc)
		if err == nil {
			var read poolWarmRead
			if read, err = d.readPoolWarm(ctx, rp, "", 0); err == nil {
				ready := read.ready
				pw.Ready = &ready
			}
		}
		if err != nil {
			pw.Note = "its warm containers could not be counted: " + errorNote(err)
		}
		out = append(out, pw)
	}
	return out
}

// warmOutcomeView is the pool's last outcome as status reports it, and what a
// create zcp started is making now: one still within its own deadline. A
// create past it never answered — the zcp process ended first — and is said
// so, never counted.
func (d LeaseDeps) warmOutcomeView(pool string) (*LeaseWarmOutcome, int) {
	o := readWarmOutcome(d.StateDir, pool)
	if o == nil || o.Outcome != LeaseWarmCreating {
		return o, 0
	}
	if at, err := time.Parse(time.RFC3339, o.At); err == nil && d.now().Sub(at) <= LeaseCallTimeout {
		return o, o.Count
	}
	o.Note = joinNotes(o.Note, fmt.Sprintf("the create of %d container(s) zcp started at %s never reported back (its zcp process ended before the orchestrator answered); the pool may hold what it created", o.Count, o.At))
	return o, 0
}
