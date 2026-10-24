// Tests for: ops/lease_warm.go, the release and acquire steps of lease.go and
// lease_group.go that use it — each pool keeps a warm container: a release
// keeps its container (reset) when the pool has none besides it and the
// container is current, and otherwise removes it with its own lease; an
// acquire or a release tops the pool up through the orchestrator's create,
// in the background, under one flock per pool; status reports each pool's
// warm containers and what zcp last did for them; an acquire waits out a
// create in progress (docs/spec-workflows.md §8 DP-27).
package ops

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// warmBox is one container of warmOrch's pool.
type warmBox struct {
	c         platform.SwarmContainer
	snapshots int
	lease     string
}

// warmOrch models one pool's orchestrator as the live one answers (I-P1,
// 2026-10-23): a list omits snapshots and only a read by id counts them; an
// acquire takes a free container first and creates one when there is room; a
// reset release frees the container clean, and refuses one without a primary
// snapshot; a delete with the holder's lease leaves the container listed as
// deleting (it still counts toward the maximum); a create adds free, current
// containers with a primary snapshot, and answers them with snapshots null.
// It is safe for concurrent use.
type warmOrch struct {
	mu    sync.Mutex
	max   int
	next  int64
	boxes []*warmBox
	// acquireErrs answer the next acquires, in order, before any works.
	acquireErrs []error
	createErr   error
	deleteErr   error
	// onCreate runs inside a create, before it answers (a racing acquire).
	onCreate func(o *warmOrch, created []*warmBox)

	acquires int
	releases []fakeRelease
	deletes  []fakeRelease
	creates  []int
	reads    []string // ids read by id
}

// warmStatusDeleting is how a list shows a container being removed.
const warmStatusDeleting = "DELETING"

func newWarmOrch(maxContainers int) *warmOrch {
	return &warmOrch{max: maxContainers, next: 3}
}

func warmLeaseOf(id string) string { return "lease-of-" + id + "-0123456789abcdef" }

// add puts a container in the pool: lock "" (free) or "reserved" (held by a
// holder zcp does not know unless it acquired it).
func (o *warmOrch) add(number int64, lock string, current, dirty bool, snapshots int) *warmBox {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.addLocked(number, lock, current, dirty, snapshots)
}

func (o *warmOrch) addLocked(number int64, lock string, current, dirty bool, snapshots int) *warmBox {
	id := "c-" + strconv.FormatInt(number, 10)
	b := &warmBox{c: platform.SwarmContainer{
		ID: id, Number: number, Hostname: nodeHostname(number), Status: platform.SwarmContainerActive, ServiceID: "svc-pool",
		Current: current, LockState: lock, Dirty: dirty,
	}, snapshots: snapshots}
	if lock == leaseLockReserved {
		b.lease = warmLeaseOf(id)
	}
	o.boxes = append(o.boxes, b)
	if number >= o.next {
		o.next = number + 1
	}
	return b
}

func (o *warmOrch) find(id string) *warmBox {
	for _, b := range o.boxes {
		if b.c.ID == id {
			return b
		}
	}
	return nil
}

func (o *warmOrch) Pool(context.Context) (platform.SwarmPool, error) {
	return platform.SwarmPool{MaximumContainerCount: o.max}, nil
}

func (o *warmOrch) Containers(context.Context) ([]platform.SwarmContainer, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]platform.SwarmContainer, 0, len(o.boxes))
	for _, b := range o.boxes {
		out = append(out, b.c) // lists never count snapshots
	}
	return out, nil
}

func (o *warmOrch) Container(_ context.Context, id string) (platform.SwarmContainer, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.reads = append(o.reads, id)
	b := o.find(id)
	if b == nil {
		return platform.SwarmContainer{}, &platform.SwarmError{Status: http.StatusNotFound, Code: "containerAction", Message: "no such container", Reason: platform.SwarmReasonContainerNotFound}
	}
	c := b.c
	c.Snapshots = b.snapshots
	return c, nil
}

func (o *warmOrch) Acquire(context.Context, platform.SwarmAcquireRequest) (platform.SwarmAcquired, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.acquires++
	if len(o.acquireErrs) > 0 {
		err := o.acquireErrs[0]
		o.acquireErrs = o.acquireErrs[1:]
		return platform.SwarmAcquired{}, err
	}
	var got *warmBox
	for _, b := range o.boxes {
		if b.c.LockState == "" && b.c.Status == platform.SwarmContainerActive {
			got = b
			break
		}
	}
	if got == nil {
		if len(o.boxes) >= o.max {
			return platform.SwarmAcquired{}, &platform.SwarmError{Status: http.StatusBadRequest, Code: "containerAction", Message: "all taken", Reason: platform.SwarmReasonAllReserved, Retryable: true}
		}
		got = o.addLocked(o.next, "", true, false, 1)
	}
	got.c.LockState, got.lease = leaseLockReserved, warmLeaseOf(got.c.ID)
	return platform.SwarmAcquired{Container: got.c, Lease: platform.SwarmLease(got.lease)}, nil
}

func (o *warmOrch) Release(_ context.Context, id, lease string, reset bool) (platform.SwarmContainer, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.releases = append(o.releases, fakeRelease{id, lease, reset})
	b := o.find(id)
	if b == nil || b.lease != lease {
		return platform.SwarmContainer{}, &platform.SwarmError{Status: http.StatusConflict, Code: "containerAction", Message: "unknown lease", Reason: platform.SwarmReasonUnknownLease}
	}
	if reset && b.snapshots == 0 {
		return platform.SwarmContainer{}, noPrimarySnapshot()
	}
	b.c.LockState, b.lease = "", ""
	if reset {
		b.c.Dirty = false
	}
	return b.c, nil
}

// Renew is the live renew of a TTL-0 lease: the holder's own lease answers its
// container unchanged (lockState deleting while a delete runs); any other
// lease, or any on a free container, is refused unknown-lease.
func (o *warmOrch) Renew(_ context.Context, id, lease string) (platform.SwarmContainer, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	b := o.find(id)
	switch {
	case b == nil:
		return platform.SwarmContainer{}, &platform.SwarmError{Status: http.StatusBadRequest, Code: "containerAction", Message: "no container " + id + " in the pool", Reason: platform.SwarmReasonContainerNotFound}
	case b.c.LockState == "deleting":
		return b.c, nil
	case b.c.LockState == "" || b.lease != lease:
		return platform.SwarmContainer{}, &platform.SwarmError{Status: http.StatusBadRequest, Code: "containerAction", Message: "no reservation of this container is held by that lease - acquire it again", Reason: platform.SwarmReasonUnknownLease}
	}
	return b.c, nil
}

func (o *warmOrch) Delete(_ context.Context, id, lease string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.deletes = append(o.deletes, fakeRelease{id: id, lease: lease})
	if o.deleteErr != nil {
		return o.deleteErr
	}
	b := o.find(id)
	if b == nil {
		return nil // a container not in the pool is answered 202
	}
	if b.lease != lease {
		return &platform.SwarmError{Status: http.StatusConflict, Code: "containerAction", Message: "held by another", Reason: "held-by-other"}
	}
	b.c.Status, b.c.LockState, b.lease = warmStatusDeleting, "deleting", ""
	return nil
}

func (o *warmOrch) Create(_ context.Context, count int) ([]platform.SwarmContainer, error) {
	o.mu.Lock()
	o.creates = append(o.creates, count)
	if o.createErr != nil {
		err := o.createErr
		o.mu.Unlock()
		return nil, err
	}
	if len(o.boxes)+count > o.max {
		o.mu.Unlock()
		return nil, &platform.SwarmError{Status: http.StatusBadRequest, Code: "containerAction", Message: "the requested containers do not fit under the pool maximum", Reason: platform.SwarmReasonPoolCapacityExceeded}
	}
	created := make([]*warmBox, 0, count)
	for range count {
		created = append(created, o.addLocked(o.next, "", true, false, 1))
	}
	hook := o.onCreate
	o.mu.Unlock()
	if hook != nil {
		hook(o, created)
	}
	out := make([]platform.SwarmContainer, 0, len(created))
	for _, b := range created {
		out = append(out, b.c) // the create answers snapshots null
	}
	return out, nil
}

// Stop and Start are a holder's: the container takes the status, its
// reservation kept; another lease is refused.
func (o *warmOrch) Stop(_ context.Context, id, lease string) (platform.SwarmContainer, error) {
	return o.power(id, lease, platform.SwarmContainerStopped)
}

func (o *warmOrch) Start(_ context.Context, id, lease string) (platform.SwarmContainer, error) {
	return o.power(id, lease, platform.SwarmContainerActive)
}

func (o *warmOrch) power(id, lease, status string) (platform.SwarmContainer, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	b := o.find(id)
	if b == nil || b.lease != lease {
		return platform.SwarmContainer{}, &platform.SwarmError{Status: http.StatusConflict, Code: "containerAction", Message: "unknown lease", Reason: platform.SwarmReasonUnknownLease}
	}
	b.c.Status = status
	return b.c, nil
}

func (o *warmOrch) Rollout(context.Context) (platform.SwarmRollout, error) {
	return platform.SwarmRollout{}, nil
}

func (o *warmOrch) log() (releases, deletes []fakeRelease, creates []int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.releases), slices.Clone(o.deletes), slices.Clone(o.creates)
}

func (o *warmOrch) state(id string) (lock, status string, present bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if b := o.find(id); b != nil {
		return b.c.LockState, b.c.Status, true
	}
	return "", "", false
}

// warmFixture is devpool behind warmOrch, with the warm policy at one warm
// container and its background step run in place (spawned collects it
// instead when collect is set).
type warmFixture struct {
	deps     LeaseDeps
	orch     *warmOrch
	stateDir string

	mu      sync.Mutex
	collect bool
	spawned []func()
}

func newWarmFixture(t *testing.T, orch *warmOrch) *warmFixture {
	t.Helper()
	fx := newLeaseFixture(t)
	wf := &warmFixture{deps: fx.deps, orch: orch, stateDir: fx.stateDir}
	wf.deps.Swarm = func(string, string) SwarmAPI { return orch }
	wf.deps.Warm = &LeaseWarm{Target: 1, Go: func(fn func()) {
		wf.mu.Lock()
		if wf.collect {
			wf.spawned = append(wf.spawned, fn)
			wf.mu.Unlock()
			return
		}
		wf.mu.Unlock()
		fn()
	}}
	return wf
}

// runSpawned runs what the warm policy put in the background since the last
// call, in order.
func (wf *warmFixture) runSpawned() int {
	wf.mu.Lock()
	fns := wf.spawned
	wf.spawned = nil
	wf.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
	return len(fns)
}

func (wf *warmFixture) acquire(t *testing.T, branch string) LeaseView {
	t.Helper()
	view, err := LeaseAcquire(context.Background(), wf.deps, testPoolHost, testRepo, branch)
	if err != nil {
		t.Fatalf("acquire %s: %v", branch, err)
	}
	return view
}

// hold gives zcp a lease of container number, reserved in the pool and
// recorded, without an acquire (whose own top-up would change the pool).
func (wf *warmFixture) hold(t *testing.T, number int64, current bool) {
	t.Helper()
	b := wf.orch.add(number, leaseLockReserved, current, false, 1)
	if err := writeLeaseRecord(wf.stateDir, leaseRecord{PoolHostname: testPoolHost, PoolServiceID: "svc-pool", ContainerID: b.c.ID,
		ContainerNumber: number, ContainerHostname: nodeHostname(number), Lease: b.lease, Branch: "b" + strconv.FormatInt(number, 10),
		AcquiredAt: "2026-10-24T08:00:00Z"}); err != nil {
		t.Fatal(err)
	}
}

func (wf *warmFixture) release(t *testing.T, alias string) LeaseView {
	t.Helper()
	view, err := LeaseRelease(context.Background(), wf.deps, alias, false)
	if err != nil {
		t.Fatalf("release %s: %v", alias, err)
	}
	return view
}

func (wf *warmFixture) recordGone(t *testing.T, alias string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(wf.stateDir, "leases", alias+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the record of %s survived its release: %v", alias, err)
	}
}

// TestLeaseRelease_WarmBelowTarget_ResetsAndKeeps: a pool with no warm
// container besides the released one keeps it — a reset release, no delete,
// no create — and the result says it was kept warm.
func TestLeaseRelease_WarmBelowTarget_ResetsAndKeeps(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	orch.add(4, leaseLockReserved, true, false, 1) // another holder's
	orch.add(6, "", true, false, 0)                // platform-made: never warm
	wf := newWarmFixture(t, orch)
	wf.hold(t, 5, true)

	view := wf.release(t, "devpool-5")

	releases, deletes, creates := orch.log()
	if len(releases) != 1 || releases[0] != (fakeRelease{id: "c-5", lease: warmLeaseOf("c-5"), reset: true}) {
		t.Errorf("releases = %+v, want c-5 released with its lease and a reset", releases)
	}
	if len(deletes) != 0 || len(creates) != 0 {
		t.Errorf("deletes %+v, creates %v: want neither (the kept container is the warm one)", deletes, creates)
	}
	if view.State != LeaseStateReleased || !strings.Contains(view.Note, "kept as the pool's warm container") {
		t.Errorf("view = %+v, want released and kept as the pool's warm container", view)
	}
	wf.recordGone(t, "devpool-5")
}

// TestLeaseRelease_WarmAtTarget_DeletesWithItsOwnLease: a pool that already
// has its warm container removes the released one with that lease's own
// lease — never a reset, never a create — and the result says it was removed.
func TestLeaseRelease_WarmAtTarget_DeletesWithItsOwnLease(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	wf := newWarmFixture(t, orch)
	wf.hold(t, 3, true)
	orch.add(6, "", true, false, 1) // the pool's warm container

	view := wf.release(t, "devpool-3")

	releases, deletes, creates := orch.log()
	if len(deletes) != 1 || deletes[0] != (fakeRelease{id: "c-3", lease: warmLeaseOf("c-3")}) {
		t.Errorf("deletes = %+v, want c-3 deleted with its own lease", deletes)
	}
	if len(releases) != 0 {
		t.Errorf("releases = %+v, want none: a removed container is not also reset", releases)
	}
	if len(creates) != 0 {
		t.Errorf("creates = %v, want none: the pool is at its target", creates)
	}
	if view.State != LeaseStateReleased || !strings.Contains(view.Note, "removed") || !strings.Contains(view.Note, "warm container") {
		t.Errorf("view = %+v, want released, the container removed because the pool has its warm container", view)
	}
	if strings.Contains(view.Note, "kept") {
		t.Errorf("note %q says kept", view.Note)
	}
	wf.recordGone(t, "devpool-3")
}

// TestLeaseRelease_Outdated_Deletes: a container that runs an outdated version
// is removed even when the pool has no warm container (the orchestrator never
// replaces a held outdated one), and the pool is topped up with a current one.
func TestLeaseRelease_Outdated_Deletes(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	wf := newWarmFixture(t, orch)
	wf.hold(t, 3, false)                           // a deploy since: #3 is outdated
	orch.add(4, leaseLockReserved, true, false, 1) // another holder's

	view := wf.release(t, "devpool-3")

	releases, deletes, creates := orch.log()
	if len(deletes) != 1 || deletes[0] != (fakeRelease{id: "c-3", lease: warmLeaseOf("c-3")}) || len(releases) != 0 {
		t.Errorf("deletes %+v, releases %+v: want the outdated c-3 deleted with its lease, never reset", deletes, releases)
	}
	if len(creates) != 1 || creates[0] != 1 {
		t.Errorf("creates = %v, want one current container created for the pool", creates)
	}
	for _, want := range []string{"removed", "outdated", "being prepared"} {
		if !strings.Contains(view.Note, want) {
			t.Errorf("note %q lacks %q", view.Note, want)
		}
	}
}

// TestLeaseRelease_DeleteRefused_FallsBackToReset: the orchestrator refuses a
// removal with no reason when another removal runs or the pool is at its
// minimum (live I-P1: metadata null, only the message differs); any such
// refusal falls back to the reset release, and the result names it.
func TestLeaseRelease_DeleteRefused_FallsBackToReset(t *testing.T) {
	t.Parallel()
	for _, msg := range []string{
		"another container removal for this service is still in progress - retry in a moment",
		"the service is already at its minimum container count",
	} {
		t.Run(msg, func(t *testing.T) {
			t.Parallel()
			orch := newWarmOrch(4)
			wf := newWarmFixture(t, orch)
			wf.hold(t, 3, true)
			orch.add(6, "", true, false, 1)
			orch.deleteErr = &platform.SwarmError{Status: http.StatusBadRequest, Code: "containerAction", Message: msg}

			view := wf.release(t, "devpool-3")

			releases, deletes, _ := orch.log()
			if len(deletes) != 1 || deletes[0] != (fakeRelease{id: "c-3", lease: warmLeaseOf("c-3")}) {
				t.Errorf("deletes = %+v, want the one refused delete of c-3 with its lease", deletes)
			}
			if len(releases) != 1 || releases[0] != (fakeRelease{id: "c-3", lease: warmLeaseOf("c-3"), reset: true}) {
				t.Errorf("releases = %+v, want c-3 released with a reset after the refusal", releases)
			}
			if view.State != LeaseStateReleased || !strings.Contains(view.Note, "refused to remove") || !strings.Contains(view.Note, msg) {
				t.Errorf("view = %+v, want released, naming the refused removal and why", view)
			}
			wf.recordGone(t, "devpool-3")
		})
	}
}

// TestLeaseWarm_DeletesOnlyTheReleasedContainer: whatever the pool holds —
// warm containers beyond the target, dirty, outdated or platform-made free
// ones, other holders' leases, zcp's own other lease — a release deletes only
// its own container, with its own lease, and nothing else is deleted,
// released or created.
func TestLeaseWarm_DeletesOnlyTheReleasedContainer(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(10)
	wf := newWarmFixture(t, orch)
	wf.hold(t, 3, true)
	wf.hold(t, 4, true)                            // zcp's other lease
	orch.add(5, "", true, false, 1)                // warm
	orch.add(10, "", true, false, 1)               // warm, beyond the target
	orch.add(6, "", true, true, 1)                 // dirty
	orch.add(7, "", false, false, 1)               // outdated
	orch.add(8, "", true, false, 0)                // platform-made: no snapshot
	orch.add(9, leaseLockReserved, true, false, 1) // another holder's

	wf.release(t, "devpool-3")

	releases, deletes, creates := orch.log()
	if len(deletes) != 1 || deletes[0] != (fakeRelease{id: "c-3", lease: warmLeaseOf("c-3")}) {
		t.Fatalf("deletes = %+v, want exactly c-3 with its own lease", deletes)
	}
	if len(releases) != 0 || len(creates) != 0 {
		t.Errorf("releases %+v, creates %v: want none", releases, creates)
	}
	for _, id := range []string{"c-4", "c-5", "c-6", "c-7", "c-8", "c-9", "c-10"} {
		if lock, status, present := orch.state(id); !present || status == warmStatusDeleting {
			t.Errorf("%s is %s/%s (present %v): zcp touched a container it was not releasing", id, status, lock, present)
		}
	}
}

// TestLeaseWarm_KeepThenRemoveThenPrepare is the policy end to end at one warm
// container: releasing one of two leases in a pool with no free container
// keeps it warm; releasing the other removes it; an acquire that takes the
// warm container leaves one being prepared.
func TestLeaseWarm_KeepThenRemoveThenPrepare(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(3)
	orch.add(3, leaseLockReserved, true, false, 1)
	orch.add(4, leaseLockReserved, true, false, 1)
	wf := newWarmFixture(t, orch)
	for _, n := range []int64{3, 4} {
		id := "c-" + strconv.FormatInt(n, 10)
		if err := writeLeaseRecord(wf.stateDir, leaseRecord{PoolHostname: testPoolHost, PoolServiceID: "svc-pool", ContainerID: id,
			ContainerNumber: n, ContainerHostname: nodeHostname(n), Lease: warmLeaseOf(id), Branch: "b" + strconv.FormatInt(n, 10), AcquiredAt: "2026-10-24T08:00:00Z"}); err != nil {
			t.Fatal(err)
		}
	}

	first := wf.release(t, "devpool-3")
	second := wf.release(t, "devpool-4")
	if !strings.Contains(first.Note, "kept as the pool's warm container") {
		t.Errorf("first release note %q: want kept warm", first.Note)
	}
	if !strings.Contains(second.Note, "removed") {
		t.Errorf("second release note %q: want removed", second.Note)
	}
	if lock, _, _ := orch.state("c-3"); lock != "" {
		t.Errorf("c-3 lock = %q, want free (the warm one)", lock)
	}
	if _, status, _ := orch.state("c-4"); status != warmStatusDeleting {
		t.Errorf("c-4 status = %q, want DELETING", status)
	}

	orch.mu.Lock()
	orch.boxes = slices.DeleteFunc(orch.boxes, func(b *warmBox) bool { return b.c.ID == "c-4" }) // gone
	orch.mu.Unlock()
	got := wf.acquire(t, "feat-c")
	if got.Alias != "devpool-3" {
		t.Fatalf("acquire took %s, want the warm devpool-3", got.Alias)
	}
	if _, _, creates := orch.log(); len(creates) != 1 || creates[0] != 1 {
		t.Errorf("creates = %v, want one warm container created after the acquire", creates)
	}
	if !strings.Contains(got.Note, "being prepared") {
		t.Errorf("acquire note %q: want a warm container being prepared", got.Note)
	}
}

// TestEnsureWarm_AfterAcquire_CreatesTheShortfall: an acquire that leaves the
// pool without a warm container creates one, and records it.
func TestEnsureWarm_AfterAcquire_CreatesTheShortfall(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	orch.add(3, "", true, false, 1)
	wf := newWarmFixture(t, orch)

	view := wf.acquire(t, "feat-a")

	if _, _, creates := orch.log(); len(creates) != 1 || creates[0] != 1 {
		t.Fatalf("creates = %v, want one container created", creates)
	}
	if !strings.Contains(view.Note, "being prepared") {
		t.Errorf("note %q: want a warm container being prepared", view.Note)
	}
	status, err := LeaseStatus(context.Background(), wf.deps, testPoolHost)
	if err != nil {
		t.Fatal(err)
	}
	w := poolWarmOf(t, status)
	if w.Target != 1 || w.Ready == nil || *w.Ready != 1 || w.Last == nil || w.Last.Outcome != LeaseWarmCreated || w.Last.Count != 1 {
		t.Errorf("warm = %+v (last %+v), want target 1, ready 1, last created 1", w, w.Last)
	}
}

func poolWarmOf(t *testing.T, status LeaseStatusView) LeasePoolWarm {
	t.Helper()
	for _, w := range status.Warm {
		if w.Pool == testPoolHost {
			return w
		}
	}
	t.Fatalf("status warm = %+v lacks pool %s", status.Warm, testPoolHost)
	return LeasePoolWarm{}
}

// TestEnsureWarm_WhatCountsWarm: only a free, ACTIVE, current, clean,
// reachable container with a snapshot (read by id) counts — never a
// platform-made one (no snapshot), a dirty, outdated or unreachable one.
func TestEnsureWarm_WhatCountsWarm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		setup      func(o *warmOrch)
		wantCreate bool
	}{
		{"a warm container", func(o *warmOrch) { o.add(6, "", true, false, 1) }, false},
		{"platform-made: no snapshot", func(o *warmOrch) { o.add(6, "", true, false, 0) }, true},
		{"dirty", func(o *warmOrch) { o.add(6, "", true, true, 1) }, true},
		{"outdated", func(o *warmOrch) { o.add(6, "", false, false, 1) }, true},
		{"unreachable", func(o *warmOrch) { o.add(6, "", true, false, 1).c.UnreachableSince = "2026-10-24T08:00:00Z" }, true},
		{"stopped", func(o *warmOrch) { o.add(6, "", true, false, 1).c.Status = "STOPPED" }, true},
		{"another holder's", func(o *warmOrch) { o.add(6, leaseLockReserved, true, false, 1) }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			orch := newWarmOrch(10)
			orch.add(5, "", true, false, 1) // the one the acquire takes
			tt.setup(orch)
			wf := newWarmFixture(t, orch)

			wf.acquire(t, "feat-b") // takes #5, then tops up

			_, _, creates := orch.log()
			if got := len(creates) > 0; got != tt.wantCreate {
				t.Errorf("created %v, want a create: %v", creates, tt.wantCreate)
			}
		})
	}
}

// TestEnsureWarm_CappedAtThePoolMaximum: the top-up creates min(target −
// warm, maximum − total), counting containers in every state, deleting ones
// included; a pool at its maximum creates nothing and records why.
func TestEnsureWarm_CappedAtThePoolMaximum(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(3)
	orch.add(3, "", true, false, 1)
	orch.add(4, leaseLockReserved, true, false, 1)
	orch.add(5, "deleting", true, false, 1).c.Status = warmStatusDeleting
	wf := newWarmFixture(t, orch)

	view := wf.acquire(t, "feat-a") // takes #3: three containers, the maximum

	if _, _, creates := orch.log(); len(creates) != 0 {
		t.Errorf("creates = %v, want none at the maximum", creates)
	}
	if strings.Contains(view.Note, "being prepared") {
		t.Errorf("note %q promises a warm container the pool has no room for", view.Note)
	}
	status, err := LeaseStatus(context.Background(), wf.deps, testPoolHost)
	if err != nil {
		t.Fatal(err)
	}
	if w := poolWarmOf(t, status); w.Last == nil || w.Last.Outcome != LeaseWarmFull || !strings.Contains(w.Last.Note, "3") {
		t.Errorf("warm last = %+v, want full, naming the maximum of 3", w.Last)
	}
}

// TestEnsureWarm_CreateRefused_RecordedAndStatusReportsIt: a create that fails
// in the background is never invisible — its outcome is recorded and status
// reports it, a pool-capacity-exceeded refusal included.
func TestEnsureWarm_CreateRefused_RecordedAndStatusReportsIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		err     error
		outcome string
		want    string
	}{
		{"capacity", &platform.SwarmError{Status: http.StatusBadRequest, Code: "containerAction", Message: "the requested containers do not fit under the pool maximum", Reason: platform.SwarmReasonPoolCapacityExceeded}, LeaseWarmFull, "do not fit"},
		{"platform", &platform.SwarmError{Status: http.StatusBadRequest, Code: "containerAction", Message: "snapshot limit reached"}, LeaseWarmFailed, "snapshot limit reached"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			orch := newWarmOrch(4)
			orch.createErr = tt.err
			wf := newWarmFixture(t, orch)
			wf.acquire(t, "feat-a")

			status, err := LeaseStatus(context.Background(), wf.deps, testPoolHost)
			if err != nil {
				t.Fatal(err)
			}
			w := poolWarmOf(t, status)
			if w.Last == nil || w.Last.Outcome != tt.outcome || !strings.Contains(w.Last.Note, tt.want) || w.Last.At == "" {
				t.Errorf("warm last = %+v, want %s naming %q, with when", w.Last, tt.outcome, tt.want)
			}
			if w.Ready == nil || *w.Ready != 0 || w.Preparing != 0 {
				t.Errorf("warm = %+v, want 0 ready, none preparing", w)
			}
		})
	}
}

// TestEnsureWarm_InTheBackground_UnderThePoolLock: the create runs in the
// background, never on the caller, and holds the pool's flock until the
// orchestrator answers; status meanwhile reports it preparing; a release
// decision that cannot take the lock in time keeps its container with a
// reset and says so.
func TestEnsureWarm_InTheBackground_UnderThePoolLock(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	orch.add(3, "", true, false, 1)
	wf := newWarmFixture(t, orch)
	wf.deps.Warm.LockWait = 50 * time.Millisecond
	wf.collect = true

	acq := wf.acquire(t, "feat-a")
	if _, _, creates := orch.log(); len(creates) != 0 {
		t.Fatalf("creates = %v before the background ran: the create must not run on the caller", creates)
	}
	if !strings.Contains(acq.Note, "being prepared") {
		t.Errorf("acquire note %q: want a warm container being prepared", acq.Note)
	}
	status, err := LeaseStatus(context.Background(), wf.deps, testPoolHost)
	if err != nil {
		t.Fatal(err)
	}
	if w := poolWarmOf(t, status); w.Preparing != 1 || w.Last == nil || w.Last.Outcome != LeaseWarmCreating {
		t.Errorf("warm = %+v (last %+v) while the create runs, want 1 preparing, last creating", w, w.Last)
	}

	view := wf.release(t, "devpool-3") // the create still holds the lock
	releases, deletes, _ := orch.log()
	if len(deletes) != 0 || len(releases) != 1 || !releases[0].reset {
		t.Errorf("releases %+v, deletes %+v: want a reset release, no delete, while the lock is held", releases, deletes)
	}
	if !strings.Contains(view.Note, "kept") {
		t.Errorf("note %q: want the container kept with a reset", view.Note)
	}

	if n := wf.runSpawned(); n != 1 {
		t.Fatalf("background steps = %d, want the one create", n)
	}
	if _, _, creates := orch.log(); len(creates) != 1 {
		t.Errorf("creates = %v, want the background create", creates)
	}
	unlock, err := lockPoolWarm(context.Background(), wf.stateDir, testPoolHost, 0)
	if err != nil {
		t.Fatalf("the pool lock is still held after the create answered: %v", err)
	}
	unlock()
}

// TestEnsureWarm_ContainerItCreated_IsNotItsOwn: a racing acquire can take the
// container a create makes before the create answers (live I-P1), so zcp
// never treats it as its own: no record, no release or delete of it, and
// status counts warm containers from the pool, not from the create's answer.
func TestEnsureWarm_ContainerItCreated_IsNotItsOwn(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	orch.add(3, "", true, false, 1)
	orch.onCreate = func(o *warmOrch, created []*warmBox) {
		o.mu.Lock()
		defer o.mu.Unlock()
		for _, b := range created {
			b.c.LockState, b.lease = leaseLockReserved, "someone-elses-lease"
		}
	}
	wf := newWarmFixture(t, orch)
	wf.acquire(t, "feat-a")

	recs, err := readLeaseRecords(wf.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].ContainerID != "c-3" {
		t.Errorf("records = %+v, want only the acquired c-3", recs)
	}
	status, err := LeaseStatus(context.Background(), wf.deps, testPoolHost)
	if err != nil {
		t.Fatal(err)
	}
	if w := poolWarmOf(t, status); w.Ready == nil || *w.Ready != 0 || w.Last == nil || w.Last.Outcome != LeaseWarmCreated {
		t.Errorf("warm = %+v (last %+v), want 0 ready though the last create made one", w, w.Last)
	}
	releases, deletes, _ := orch.log()
	if len(releases) != 0 || len(deletes) != 0 {
		t.Errorf("releases %+v, deletes %+v: zcp touched the container it created", releases, deletes)
	}
}

// TestLeaseRelease_Unresettable_DeletedAsDP15BeforeTheWarmDecision: a
// container without a primary snapshot is deleted the DP-15 way, with its
// wording, whatever the pool's warm containers.
func TestLeaseRelease_Unresettable_DeletedAsDP15BeforeTheWarmDecision(t *testing.T) {
	t.Parallel()
	for _, warm := range []bool{false, true} {
		t.Run(fmt.Sprintf("warm=%v", warm), func(t *testing.T) {
			t.Parallel()
			orch := newWarmOrch(4)
			orch.add(3, "", true, false, 0) // platform-made
			if warm {
				orch.add(6, "", true, false, 1)
			}
			wf := newWarmFixture(t, orch)
			wf.acquire(t, "feat-a") // takes #3

			view := wf.release(t, "devpool-3")

			_, deletes, _ := orch.log()
			if len(deletes) != 1 || deletes[0] != (fakeRelease{id: "c-3", lease: warmLeaseOf("c-3")}) {
				t.Errorf("deletes = %+v, want c-3 deleted with its lease", deletes)
			}
			if !strings.Contains(view.Note, "could not be reset") || !strings.Contains(view.Note, "minContainers") {
				t.Errorf("note %q: want the DP-15 wording", view.Note)
			}
		})
	}
}

// TestLeaseAcquire_ContainersPreparing_RetriesWithinDeadline: an acquire
// refused containers-preparing reserved nothing, so zcp retries it — for up to
// a minute on a container pool, to the acquire's own deadline on a VM pool —
// and the note or the refusal names the create it waited for.
func TestLeaseAcquire_ContainersPreparing_RetriesWithinDeadline(t *testing.T) {
	t.Parallel()
	preparing := &platform.SwarmError{Status: http.StatusBadRequest, Code: "containerAction",
		Message: "1 container(s) are still being prepared - retry in a moment", Reason: platform.SwarmReasonContainersPreparing, Retryable: true}
	tests := []struct {
		name      string
		poolType  string
		refusals  int // containers-preparing answers before an acquire works
		wantOK    bool
		wantTries int
	}{
		{"container pool, the create ends in time", "swarm@1", 2, true, 3},
		{"container pool, still preparing after a minute", "swarm@1", 100, false, 30},
		{"VM pool, past a minute", "swarm-vm@1", 40, true, 41},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			orch := newWarmOrch(4)
			for range tt.refusals {
				orch.acquireErrs = append(orch.acquireErrs, preparing)
			}
			wf := newWarmFixture(t, orch)
			wf.deps.Client.(*platform.Mock).WithServices([]platform.ServiceStack{
				{ID: "svc-pool", Name: testPoolHost, Status: platform.SwarmContainerActive, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: tt.poolType, ServiceStackTypeCategoryName: "USER"}},
			})
			var mu sync.Mutex
			clock := time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
			wf.deps.Now = func() time.Time { // two seconds per look
				mu.Lock()
				defer mu.Unlock()
				clock = clock.Add(2 * time.Second)
				return clock
			}
			wf.deps.PreparingRetry = time.Millisecond

			view, err := LeaseAcquire(context.Background(), wf.deps, testPoolHost, testRepo, "feat-a")

			orch.mu.Lock()
			tries := orch.acquires
			orch.mu.Unlock()
			if tries != tt.wantTries {
				t.Errorf("acquire attempts = %d, want %d", tries, tt.wantTries)
			}
			if !tt.wantOK {
				var pe *platform.PlatformError
				if !errors.As(err, &pe) {
					t.Fatalf("err = %v, want the containers-preparing refusal", err)
				}
				if !strings.Contains(pe.Message+pe.Suggestion, "warm container") || !strings.Contains(pe.Suggestion, "zcp retried") {
					t.Errorf("refusal %q / %q should name the warm create and the retry", pe.Message, pe.Suggestion)
				}
				return
			}
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}
			if !strings.Contains(view.Note, "still preparing") || !strings.Contains(view.Note, "warm container") {
				t.Errorf("note %q should say the acquire waited for a container being prepared", view.Note)
			}
		})
	}
}

// TestLeaseStatus_ReportsEachPoolsWarm: status reports each pool's target,
// its warm containers counted by id, the ones zcp is preparing and what it
// last did — and changes nothing: no orchestrator mutation, no record written.
func TestLeaseStatus_ReportsEachPoolsWarm(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(10)
	orch.add(3, "", true, false, 1)                // warm
	orch.add(4, "", true, false, 1)                // warm
	orch.add(5, "", true, false, 0)                // platform-made
	orch.add(6, leaseLockReserved, true, false, 1) // another holder's
	wf := newWarmFixture(t, orch)
	at := time.Now().UTC().Format(time.RFC3339)
	if err := writeWarmOutcome(wf.stateDir, testPoolHost, LeaseWarmOutcome{At: at, Outcome: LeaseWarmCreating, Count: 1}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(wf.stateDir, "warm", testPoolHost+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the outcome record is not at %s: %v", path, err)
	}

	status, err := LeaseStatus(context.Background(), wf.deps, "")
	if err != nil {
		t.Fatal(err)
	}

	w := poolWarmOf(t, status)
	if w.Target != 1 || w.Ready == nil || *w.Ready != 2 || w.Preparing != 1 || w.Last == nil || w.Last.Outcome != LeaseWarmCreating || w.Last.At != at {
		t.Errorf("warm = %+v (last %+v), want target 1, 2 ready, 1 preparing, last creating at %s", w, w.Last, at)
	}
	releases, deletes, creates := orch.log()
	if len(releases)+len(deletes)+len(creates) != 0 || orch.acquires != 0 {
		t.Errorf("status mutated the pool: releases %+v, deletes %+v, creates %v", releases, deletes, creates)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Errorf("status rewrote the outcome record:\n%s\nwas\n%s", after, before)
	}
}

// TestLeaseStatus_CreateThatNeverAnswered_SaysSo: a create whose zcp ended
// before the orchestrator answered leaves "creating" behind; once the
// create's own deadline has passed, status no longer counts it preparing and
// says it never reported back.
func TestLeaseStatus_CreateThatNeverAnswered_SaysSo(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	wf := newWarmFixture(t, orch)
	at := time.Now().Add(-LeaseCallTimeout - time.Minute).UTC().Format(time.RFC3339)
	if err := writeWarmOutcome(wf.stateDir, testPoolHost, LeaseWarmOutcome{At: at, Outcome: LeaseWarmCreating, Count: 1}); err != nil {
		t.Fatal(err)
	}

	status, err := LeaseStatus(context.Background(), wf.deps, testPoolHost)
	if err != nil {
		t.Fatal(err)
	}
	w := poolWarmOf(t, status)
	if w.Preparing != 0 || w.Last == nil || !strings.Contains(w.Last.Note, "never reported back") {
		t.Errorf("warm = %+v (last %+v), want none preparing and the create named as never answered", w, w.Last)
	}
}

// TestLeaseStatus_WithoutWarmPolicy_ReportsNoWarm: a zcp without the warm
// policy (the envelope's lease reader, tests) reads no warm containers.
func TestLeaseStatus_WithoutWarmPolicy_ReportsNoWarm(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	orch.add(3, "", true, false, 1)
	wf := newWarmFixture(t, orch)
	wf.deps.Warm = nil

	status, err := LeaseStatus(context.Background(), wf.deps, testPoolHost)
	if err != nil {
		t.Fatal(err)
	}
	orch.mu.Lock()
	reads := len(orch.reads)
	orch.mu.Unlock()
	if status.Warm != nil || reads != 0 {
		t.Errorf("warm = %+v, reads by id %d: want neither without the policy", status.Warm, reads)
	}
}

// TestHandBackOutcome_WarmKeptOrRemoved: a failed checkout's hand-back says
// where the container went with the warm policy too — kept as the pool's warm
// container, or removed — in the message and the suggestion alike.
func TestHandBackOutcome_WarmKeptOrRemoved(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		view   LeaseView
		want   string
		banned []string
	}{
		{"kept", LeaseView{State: LeaseStateReleased, keptWarm: true}, "kept as the pool's warm container", []string{"removed", "stays reserved"}},
		{"removed", LeaseView{State: LeaseStateReleased, containerRemoved: true}, "removed", []string{"kept", "stays reserved", "could not be reset"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			what, next := handBackOutcome("devpool-3", tt.view, nil)
			if !strings.Contains(what, tt.want) {
				t.Errorf("what %q lacks %q", what, tt.want)
			}
			for _, b := range tt.banned {
				if strings.Contains(what+next, b) {
					t.Errorf("what %q / next %q say %q", what, next, b)
				}
			}
			if !strings.Contains(next, "acquire again") {
				t.Errorf("next %q should say to acquire again", next)
			}
		})
	}
}

// TestLeaseStatus_ListsTheServicesOnce: status — the envelope's lease reader
// too, after every tool result in a project with a pool — lists the
// project's services once (an ES-backed search) and resolves every pool, and
// the warm counts, from that one list: never a search per pool.
func TestLeaseStatus_ListsTheServicesOnce(t *testing.T) {
	t.Parallel()
	for _, named := range []string{"", testPoolHost} {
		t.Run("pool="+named, func(t *testing.T) {
			t.Parallel()
			orch := newWarmOrch(4)
			orch.add(3, "", true, false, 1)
			wf := newWarmFixture(t, orch)
			mock, ok := wf.deps.Client.(*platform.Mock)
			if !ok {
				t.Fatalf("client %T is not the mock", wf.deps.Client)
			}
			mock.WithServices([]platform.ServiceStack{
				{ID: "svc-pool", Name: testPoolHost, Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
				{ID: "svc-web", Name: "webpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
				{ID: "svc-app", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
			}).WithServiceEnv("svc-web", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: testPoolToken, Sensitive: true}})
			wf.hold(t, 5, true)
			before := mock.CallCount("ListServices")

			status, err := LeaseStatus(context.Background(), wf.deps, named)
			if err != nil {
				t.Fatal(err)
			}
			if len(status.Leases) == 0 || len(status.Warm) == 0 {
				t.Fatalf("status = %+v, want the lease and the warm counts read", status)
			}
			if n := mock.CallCount("ListServices") - before; n != 1 {
				t.Errorf("ListServices calls = %d, want 1: every pool resolved from the one list", n)
			}
		})
	}
}

// TestLeaseRelease_ParkedForce_StoppedContainerNeverKeptWarm: a forced
// release of a parked lease sends the container back as it stands, stopped,
// and a reset leaves a stopped container stopped — while a warm container is
// an ACTIVE one an acquire can take at once. So the stopped container is not
// kept as the pool's warm one: it is removed with its own lease and the pool
// is topped up with a container that runs, and status reads that one ready.
func TestLeaseRelease_ParkedForce_StoppedContainerNeverKeptWarm(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	wf := newWarmFixture(t, orch)
	wf.hold(t, 5, true)
	orch.mu.Lock()
	orch.find("c-5").c.Status = platform.SwarmContainerStopped // parked
	orch.mu.Unlock()

	view, err := LeaseRelease(context.Background(), wf.deps, "devpool-5", true)
	if err != nil {
		t.Fatalf("release: %v", err)
	}

	releases, deletes, creates := orch.log()
	if len(deletes) != 1 || deletes[0] != (fakeRelease{id: "c-5", lease: warmLeaseOf("c-5")}) {
		t.Errorf("deletes = %+v, want the stopped c-5 removed with its own lease", deletes)
	}
	if len(releases) != 0 {
		t.Errorf("releases = %+v, want none: a stopped container is not reset and kept", releases)
	}
	if !slices.Equal(creates, []int{1}) {
		t.Errorf("creates = %v, want one container created: the pool has no warm one that runs", creates)
	}
	if view.State != LeaseStateReleased || strings.Contains(view.Note, "kept as the pool's warm container") || !strings.Contains(view.Note, "stopped") {
		t.Errorf("view = %+v, want released, removed because it is stopped, never kept warm", view)
	}
	status, err := LeaseStatus(context.Background(), wf.deps, testPoolHost)
	if err != nil {
		t.Fatal(err)
	}
	if w := poolWarmOf(t, status); w.Ready == nil || *w.Ready != 1 {
		t.Errorf("warm = %+v, want 1 ready: the created container", w)
	}
}
