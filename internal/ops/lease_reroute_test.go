// Tests for: ops/lease_reroute.go — an unparked lease's preview is routed
// again (docs/spec-workflows.md §8 DP-28): the platform's balancer drops a
// pool container from the pool's public routes when the orchestrator stops it
// and does not add it back when the orchestrator starts it again; it rebuilds
// the routes only when the pool's containers change (live-verified
// 2026-10-25, plans/unpark-502). So once an unpark's container runs again,
// zcp changes the pool's containers: it replaces the pool's warm container,
// or creates it when the pool has none, within maxContainers.
package ops

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// routeOrch is warmOrch recording each start, removal and create in order;
// a removal finishes at once (the live one takes ~9 s), so the container is
// gone from the pool's listing when the replacement is asked for.
type routeOrch struct {
	*warmOrch
	mu     sync.Mutex
	events []string
}

func (o *routeOrch) note(e string) {
	o.mu.Lock()
	o.events = append(o.events, e)
	o.mu.Unlock()
}

func (o *routeOrch) log() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.events)
}

func (o *routeOrch) Start(ctx context.Context, id, lease string) (platform.SwarmContainer, error) {
	o.note("start " + id)
	return o.warmOrch.Start(ctx, id, lease)
}

func (o *routeOrch) Delete(ctx context.Context, id, lease string) error {
	if err := o.warmOrch.Delete(ctx, id, lease); err != nil {
		o.note("remove refused " + id)
		return err
	}
	o.note("remove " + id)
	o.warmOrch.mu.Lock()
	o.boxes = slices.DeleteFunc(o.boxes, func(b *warmBox) bool { return b.c.ID == id })
	o.warmOrch.mu.Unlock()
	return nil
}

func (o *routeOrch) Create(ctx context.Context, count int) ([]platform.SwarmContainer, error) {
	created, err := o.warmOrch.Create(ctx, count)
	if err == nil {
		o.note("create")
	}
	return created, err
}

// newRerouteFixture is newParkFixture's parked devpool-3 behind a routeOrch
// of at most maxContainers, the warm policy at one warm container run in
// place, and the pool service declaring ports.
func newRerouteFixture(t *testing.T, maxContainers int, ports []platform.Port) (*parkFixture, *routeOrch) {
	t.Helper()
	pf := newParkFixture(t, true)
	orch := &routeOrch{warmOrch: newWarmOrch(maxContainers)}
	mine := orch.add(3, leaseLockReserved, true, false, 1)
	mine.lease, mine.c.Status = testLease, platform.SwarmContainerStopped
	pf.deps.Swarm = func(string, string) SwarmAPI { return orch }
	pf.deps.Warm = &LeaseWarm{Target: 1, Go: func(fn func()) { fn() }}
	pf.deps.Client = platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "proj"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: testPoolHost, Status: "ACTIVE", Ports: ports, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: testPoolToken, Sensitive: true}})
	return pf, orch
}

// previewPorts is a pool serving lease previews on 8001 and 8002.
var previewPorts = []platform.Port{{Port: 8001, Protocol: "tcp", Scheme: "http"}, {Port: 8002, Protocol: "tcp", Scheme: "http"}}

// TestLeaseUnpark_PublicPool_ReplacesTheWarmContainer: the pool has its warm
// container, so the unpark removes it — free, without a lease or force —
// once its own container runs again, and creates its replacement once it is
// gone; the lease's own container is never removed, and the note says the
// preview is routed again.
func TestLeaseUnpark_PublicPool_ReplacesTheWarmContainer(t *testing.T) {
	t.Parallel()
	pf, orch := newRerouteFixture(t, 3, previewPorts)
	orch.add(4, "", true, false, 1) // the pool's warm container

	v, err := LeaseUnpark(context.Background(), pf.deps, "devpool-3")
	if err != nil {
		t.Fatalf("LeaseUnpark: %v", err)
	}
	if got, want := orch.log(), []string{"start c-3", "remove c-4", "create"}; !slices.Equal(got, want) {
		t.Errorf("orchestrator = %v, want %v: the warm container replaced after the start", got, want)
	}
	_, deletes, _ := orch.warmOrch.log()
	if len(deletes) != 1 || deletes[0] != (fakeRelease{id: "c-4"}) {
		t.Errorf("deletes = %+v, want only the free c-4, with no lease", deletes)
	}
	if v.State != LeaseStateHeld || !strings.Contains(v.Note, "routed again") {
		t.Errorf("view = %+v, want a held lease whose note says its preview is routed again", v)
	}
}

// TestLeaseUnpark_PublicPool_NoWarmContainer_CreatesIt: a pool without a warm
// container but with room gets one created after the start, nothing removed.
func TestLeaseUnpark_PublicPool_NoWarmContainer_CreatesIt(t *testing.T) {
	t.Parallel()
	pf, orch := newRerouteFixture(t, 3, previewPorts)

	v, err := LeaseUnpark(context.Background(), pf.deps, "devpool-3")
	if err != nil {
		t.Fatalf("LeaseUnpark: %v", err)
	}
	if got, want := orch.log(), []string{"start c-3", "create"}; !slices.Equal(got, want) {
		t.Errorf("orchestrator = %v, want %v", got, want)
	}
	if !strings.Contains(v.Note, "routed again") {
		t.Errorf("note = %q, want it to say the preview is routed again", v.Note)
	}
}

// TestLeaseUnpark_PublicPool_FullNoneFree_SaysItIsNotRouted: a pool at its
// maximum with every other container held changes nothing — no removal of
// another holder's container, no create past the maximum — and the note says
// the preview answers 502 until the pool's containers change.
func TestLeaseUnpark_PublicPool_FullNoneFree_SaysItIsNotRouted(t *testing.T) {
	t.Parallel()
	pf, orch := newRerouteFixture(t, 2, previewPorts)
	orch.add(4, leaseLockReserved, true, false, 1) // another holder's lease

	v, err := LeaseUnpark(context.Background(), pf.deps, "devpool-3")
	if err != nil {
		t.Fatalf("LeaseUnpark: %v", err)
	}
	if got, want := orch.log(), []string{"start c-3"}; !slices.Equal(got, want) {
		t.Errorf("orchestrator = %v, want only the start", got)
	}
	if v.State != LeaseStateHeld || !strings.Contains(v.Note, "502") || !strings.Contains(v.Note, "until the pool's containers change") {
		t.Errorf("view = %+v, want a held lease whose note says its preview answers 502 until the pool's containers change", v)
	}
}

// TestLeaseUnpark_PoolWithoutHTTPPort_ChangesNoContainer: a pool with no HTTP
// port has no public route to restore: nothing is removed or created and
// nothing is said of it.
func TestLeaseUnpark_PoolWithoutHTTPPort_ChangesNoContainer(t *testing.T) {
	t.Parallel()
	pf, orch := newRerouteFixture(t, 3, []platform.Port{{Port: 5432, Protocol: "tcp", Scheme: "tcp"}})
	orch.add(4, "", true, false, 1)

	v, err := LeaseUnpark(context.Background(), pf.deps, "devpool-3")
	if err != nil {
		t.Fatalf("LeaseUnpark: %v", err)
	}
	if got, want := orch.log(), []string{"start c-3"}; !slices.Equal(got, want) {
		t.Errorf("orchestrator = %v, want only the start", got)
	}
	if v.Note != "" {
		t.Errorf("note = %q, want none", v.Note)
	}
}

// TestLeaseUnpark_RunningUnparkedLease_ChangesNoContainer: a lease that is not
// parked and whose container runs was never stopped since its route was made:
// the unpark changes none of the pool's containers.
func TestLeaseUnpark_RunningUnparkedLease_ChangesNoContainer(t *testing.T) {
	t.Parallel()
	pf, orch := newRerouteFixture(t, 3, previewPorts)
	rec := pf.record(t)
	rec.Parked, rec.ParkedAt = false, ""
	if err := writeLeaseRecord(pf.stateDir, rec); err != nil {
		t.Fatal(err)
	}
	orch.find("c-3").c.Status = platform.SwarmContainerActive
	orch.add(4, "", true, false, 1)

	if _, err := LeaseUnpark(context.Background(), pf.deps, "devpool-3"); err != nil {
		t.Fatalf("LeaseUnpark: %v", err)
	}
	if got := orch.log(); len(got) != 0 {
		t.Errorf("orchestrator = %v, want nothing started, removed or created", got)
	}
}
