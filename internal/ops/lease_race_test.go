// Tests for: the windows between two zcp calls, or two zcp processes sharing
// a state directory, in which a lease changes hands (docs/spec-workflows.md
// §8 DP-2, DP-17, DP-26, DP-27, DP-28): a group acquire's member that ends
// while another member waits is never checked out or written over; two
// releases of one group never leave a sibling wired to either; a container
// its holder is parking or unparking is leased; a sync fast-forwards only the
// leases of its own branch; park and unpark write the record only while it is
// still the lease's own; a warm container zcp could not read is never counted
// as missing; a listing that fails never turns a known loss into unverified.
package ops

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// raceOtherRepo is the repository another zcp process leases the same branch
// name of.
const raceOtherRepo = "https://github.com/acme/other"

// waitForRecord waits until zcp has written alias's record.
func waitForRecord(t *testing.T, stateDir, alias string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := loadLeaseRecord(stateDir, alias); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no record of %s within 5 s", alias)
}

// takenByAnotherZcp is another zcp process sharing the state directory that
// took hqpool's container #3 again — released elsewhere, then acquired for
// another repository — and recorded its own lease under the same alias.
func takenByAnotherZcp(t *testing.T, gf *groupFixture) {
	t.Helper()
	gf.hq.takeOver(ownHQContainer)
	other := leaseRecord{
		PoolHostname: groupHQPool, PoolServiceID: "svc-hq", ContainerID: ownHQContainer, ContainerNumber: 3,
		ContainerHostname: nodeHostnameOf(groupHQPool, 3), Lease: ownOtherLease, Branch: "feat-b",
		Repo: topology.CanonicalRepoURL(raceOtherRepo), AcquiredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeLeaseRecord(gf.stateDir, other); err != nil {
		t.Errorf("the other process writes its record: %v", err)
	}
}

// recordLeaseOf is the lease string alias's record holds ("" for none).
func recordLeaseOf(t *testing.T, stateDir, alias string) string {
	t.Helper()
	rec, err := loadLeaseRecord(stateDir, alias)
	if err != nil {
		return ""
	}
	return rec.Lease
}

// TestLeaseAcquireGroup_MemberEndsWhileAnotherWaits_NeverCheckedOutOrWrittenOver:
// a group acquire reserves its members at once and checks them out only once
// every one is reserved — a pool still preparing a container holds that up
// for minutes. A member whose lease ends meanwhile, its container taken again
// under the same alias by another zcp process, is asked about right before
// its checkout (DP-2): nothing is checked out in the other lease's /var/www,
// its record is left as the other process wrote it, the container is never
// released with zcp's lease, and the acquire hands back the members it
// reserved.
func TestLeaseAcquireGroup_MemberEndsWhileAnotherWaits_NeverCheckedOutOrWrittenOver(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gate, entered := make(chan struct{}), make(chan struct{}, 1)
	gf.web.mu.Lock()
	gf.web.gate, gf.web.entered = gate, entered
	gf.web.mu.Unlock()
	type result struct {
		change LeaseChange
		err    error
	}
	done := make(chan result, 1)
	go func() {
		change, err := LeaseAcquireGroupChange(context.Background(), gf.deps, []string{groupHQPool, groupWebPool}, testRepo, "feat")
		done <- result{change, err}
	}()
	<-entered // webpool's acquire waits at its orchestrator
	waitForRecord(t, gf.stateDir, ownHQAlias)
	takenByAnotherZcp(t, gf)
	close(gate)

	var res result
	select {
	case res = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the group acquire did not return")
	}
	if res.err == nil {
		t.Fatalf("acquire = %+v, want a refusal: hqpool-3's lease ended before its checkout", res.change.Leases)
	}
	if !strings.Contains(res.err.Error(), ownHQAlias) {
		t.Errorf("refusal %q should name %s", res.err, ownHQAlias)
	}
	for _, c := range gf.ssh.checkouts() {
		if c.hostname == ownHQAlias {
			t.Errorf("checked out in %s, another lease's container: %s", ownHQAlias, c.command)
		}
	}
	if got := recordLeaseOf(t, gf.stateDir, ownHQAlias); got != ownOtherLease {
		t.Errorf("%s's record holds lease %q, want the other process's, untouched", ownHQAlias, got)
	}
	if got := gf.hq.leaseOf(ownHQContainer); got != ownOtherLease {
		t.Errorf("the container's lease = %q, want the other process's", got)
	}
	if resets := gf.hq.freeResetLog(); len(resets) != 0 {
		t.Errorf("free containers reset: %v", resets)
	}
	if got := recordLeaseOf(t, gf.stateDir, ownWebAlias); got != "" {
		t.Errorf("webpool-7's record survived the refusal (lease %q): the acquire hands back what it reserved", got)
	}
	if rels := gf.web.releaseLog(); len(rels) != 1 || rels[0].id != ownWebContainer {
		t.Errorf("webpool releases = %+v, want webpool-7 handed back", rels)
	}
}

// TestLeaseAcquire_RecordTakenDuringTheCheckout_NeverWrittenOver: the
// checkout records the commit it cut the branch from and the container's
// stable number in the lease's record. A record another zcp process wrote
// under the alias meanwhile — the lease ended and the container was taken
// again — is never written over (DP-2), and the acquire is refused, since the
// lease it would return is no longer zcp's.
func TestLeaseAcquire_RecordTakenDuringTheCheckout_NeverWrittenOver(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	var once sync.Once
	gf.ssh.setHandler(func(host, command string) ([]byte, error) {
		if strings.Contains(command, "fetch -q --prune origin") {
			if host == ownHQAlias {
				once.Do(func() { takenByAnotherZcp(t, gf) })
			}
			return []byte("@@zcp-lease base " + groupBase + "\n@@zcp-lease slot 3\n"), nil
		}
		return nil, nil
	})

	change, err := LeaseAcquireGroupChange(context.Background(), gf.deps, []string{groupHQPool}, testRepo, "feat")
	if err == nil {
		t.Errorf("acquire = %+v, want a refusal: the lease ended during its checkout", change.Leases)
	}
	if got := recordLeaseOf(t, gf.stateDir, ownHQAlias); got != ownOtherLease {
		t.Errorf("%s's record holds lease %q, want the other process's, never written over", ownHQAlias, got)
	}
	if got := gf.hq.leaseOf(ownHQContainer); got != ownOtherLease {
		t.Errorf("the container's lease = %q, want the other process's", got)
	}
}

// raceWorkerDeclaration is the third part's: it reaches the hqpool and the
// webpool lease of its branch.
const raceWorkerDeclaration = `{"version":1,"siblingEnv":{"HQ_HOST":"hqpool","WEB_HOST":"webpool"},"servers":[{"name":"worker","port":9000,"command":"run worker"}]}`

// newRewireTrio is the rewire fixture with a third pool, apipool, whose
// container #5 runs zcp-devsup with raceWorkerDeclaration (worker); release
// gate the hqpool orchestrator's releases when set.
func newRewireTrio(t *testing.T, hqRelease func()) (*rewireFixture, *devservertest.Container) {
	t.Helper()
	rf := newRewireFixture(t)
	mock, ok := rf.deps.Client.(*platform.Mock)
	if !ok {
		t.Fatal("the fixture's client is not a *platform.Mock")
	}
	services, err := mock.ListServices(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	mock.WithServices(append(services, poolService("svc-api", "apipool"))).
		WithServiceEnv("svc-api", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: "api-token", Sensitive: true}})
	var hq SwarmAPI = rf.groupFixture.hq
	if hqRelease != nil {
		hq = releaseHook{SwarmAPI: rf.groupFixture.hq, before: hqRelease}
	}
	orchs := map[string]SwarmAPI{
		orchestratorURL(groupHQPool): hq, orchestratorURL(groupWebPool): rf.groupFixture.web,
		orchestratorURL("apipool"): newPoolOrch("apipool", "svc-api", 5),
	}
	rf.deps.Swarm = func(baseURL, _ string) SwarmAPI { return orchs[baseURL] }
	worker := devservertest.NewSupervised(raceWorkerDeclaration)
	rf.ssh.mu.Lock()
	rf.ssh.containers["apipool-5"] = worker
	rf.ssh.mu.Unlock()
	return rf, worker
}

// TestLeaseRelease_TwoSiblingsAtOnce_NoOverlayNamesEither: two releases of one
// group's leases — hqpool-3 and webpool-7, with apipool-5 staying — each hand
// apipool-5 the overlay recomputed without their own lease. Neither may
// compute it while the other's lease is still recorded and send it after that
// lease went: apipool-5's worker would reach the container's next holder
// (DP-26). Whatever the order, once both are released apipool-5 is wired to
// neither container.
func TestLeaseRelease_TwoSiblingsAtOnce_NoOverlayNamesEither(t *testing.T) {
	t.Parallel()
	atRelease, proceed := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	rf, worker := newRewireTrio(t, func() {
		once.Do(func() {
			atRelease <- struct{}{}
			<-proceed
		})
	})
	rf.acquire(t, groupHQPool, groupWebPool, "apipool")
	if got, want := worker.Overlay(), map[string]string{"HQ_HOST": rewireHQHost, "WEB_HOST": rewireWebHost}; !maps.Equal(got, want) {
		t.Fatalf("apipool-5's overlay after the acquire = %v, want %v", got, want)
	}

	doneHQ, doneWeb := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := LeaseReleaseChange(context.Background(), rf.deps, ownHQAlias, true)
		doneHQ <- err
	}()
	<-atRelease // hqpool-3's siblings are rewired without it; its release waits at the orchestrator
	go func() {
		_, err := LeaseReleaseChange(context.Background(), rf.deps, ownWebAlias, true)
		doneWeb <- err
	}()
	select {
	case err := <-doneWeb:
		doneWeb <- err
	case <-time.After(2 * time.Second):
		// webpool-7's release waits for hqpool-3's to finish.
	}
	close(proceed)
	for name, ch := range map[string]chan error{ownHQAlias: doneHQ, ownWebAlias: doneWeb} {
		select {
		case err := <-ch:
			if err != nil {
				t.Errorf("release %s: %v", name, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("release %s did not return", name)
		}
	}

	for name, value := range worker.Overlay() {
		if value == rewireHQHost || value == rewireWebHost {
			t.Errorf("apipool-5's worker runs with %s=%s, a container released with its lease (overlay %v)", name, value, worker.Overlay())
		}
	}
}

// TestScale_PoolLowering_ContainerBeingParkedIsLeased: a container its holder
// is stopping or starting (a park or an unpark, DP-28) is under its
// reservation, lockState stopping or starting: a lowering that deletes it
// deletes a leased container, so it is put to the guard (PS-7), and the lowest
// safe maximum counts it.
func TestScale_PoolLowering_ContainerBeingParkedIsLeased(t *testing.T) {
	t.Parallel()
	errRefused := errors.New("refused by the guard")
	containers := containersAt(leasedAt(), 1, 2, 3, 4, 5)
	containers[3].LockState = leaseLockStarting // devpool-4, being unparked
	containers[4].LockState = leaseLockStopping // devpool-5, being parked
	client := shrinkPoolClient(5)
	stateDir := t.TempDir()
	recordLease(t, stateDir, testPoolHost, "svc-pool", 5, "b5")
	swarm := &fakeSwarm{containers: containers}
	deps := LeaseDeps{Client: client, ProjectID: "p1", StateDir: stateDir,
		Swarm: func(string, string) SwarmAPI { return swarm },
		Env:   func(string) (string, bool) { return "", false }}
	var asked *PoolShrink
	guard := WithPoolShrinkGuard(deps, func(s PoolShrink) error {
		asked = &s
		return errRefused
	})

	_, err := Scale(context.Background(), client, "p1", testPoolHost, ScaleParams{MaxContainers: scaleIntPtr(3)}, guard)
	if !errors.Is(err, errRefused) {
		t.Fatalf("err = %v, want the guard's refusal: the lowering deletes two leased containers", err)
	}
	if asked == nil {
		t.Fatal("the guard was not asked")
	}
	leased := make([]string, 0, len(asked.LeasedDeletes()))
	for _, c := range asked.LeasedDeletes() {
		a := c.Alias
		if c.Yours {
			a += "*"
		}
		leased = append(leased, a)
	}
	if want := []string{"devpool-5*", "devpool-4"}; !slices.Equal(leased, want) {
		t.Errorf("leased containers that go = %v, want %v", leased, want)
	}
	if asked.LowestSafe != 5 {
		t.Errorf("lowest safe maximum = %d, want 5", asked.LowestSafe)
	}
	if client.sent != nil {
		t.Error("the lowering was sent to the platform")
	}
}

// TestLeaseAcquire_FullPool_ContainerBeingParkedIsLeased: a full pool's
// refusal counts a container its holder is stopping or starting as leased,
// and names it as yours when it is zcp's lease.
func TestLeaseAcquire_FullPool_ContainerBeingParkedIsLeased(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	parking := reservedContainer("c-2", 2)
	parking.LockState = leaseLockStopping
	fx.swarm.containers = []platform.SwarmContainer{reservedContainer("c-1", 1), parking}
	fx.swarm.foreign = map[string]bool{"c-1": true}
	fx.swarm.limits = platform.SwarmPool{MaximumContainerCount: 2}
	fx.swarm.acquireErr = allReserved(causeAtMaximum)
	recordLease(t, fx.stateDir, testPoolHost, "svc-pool", 2, "b2")

	_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feat")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want the full pool's refusal", err)
	}
	if !strings.Contains(pe.Message, "2 of at most 2 containers are leased") || !strings.Contains(pe.Message, "devpool-2") {
		t.Errorf("refusal %q, want 2 of 2 leased, devpool-2 named yours", pe.Message)
	}
}

// hookSSH is ssh that runs before ahead of every command it sends.
type hookSSH struct {
	SSHDeployer
	before func(host, command string)
}

func (h hookSSH) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	h.before(host, command)
	return h.SSHDeployer.ExecSSH(ctx, host, command)
}

// TestLeaseSync_MemberAliasRetakenForAnotherRepository_NotFastForwarded: a
// sync reads its group before the push. A member released meanwhile whose
// container another zcp process took again under the same alias — a lease of
// another repository's branch of the same name — is no member of the group:
// it is never fetched or fast-forwarded, however the orchestrator answers the
// lease its alias names now.
func TestLeaseSync_MemberAliasRetakenForAnotherRepository_NotFastForwarded(t *testing.T) {
	t.Parallel()
	gf, remote := newSyncFixture(t, syncBranch)
	var once sync.Once
	gf.deps.SSH = hookSSH{SSHDeployer: remote, before: func(host, command string) {
		if host != ownHQAlias || !strings.Contains(command, "push --porcelain") {
			return
		}
		once.Do(func() {
			rec := syncRecord(groupWebPool, "svc-web", 7, syncBranch, "2026-10-24T10:05:00Z")
			rec.Repo, rec.Lease = raceOtherRepo, "webpool-lease-of-another-repository-0123456789"
			gf.web.put(gf.web.get(rec.ContainerID), rec.Lease)
			if err := writeLeaseRecord(gf.stateDir, rec); err != nil {
				t.Errorf("the other process writes its record: %v", err)
			}
		})
	}}

	v, err := LeaseSync(context.Background(), gf.deps, ownHQAlias)
	if err != nil {
		t.Fatalf("LeaseSync: %v", err)
	}
	if m := syncMemberOf(t, v, ownWebAlias); m.Outcome != LeaseSyncLost || m.Note == "" {
		t.Errorf("member = %+v, want lost, saying the alias names another lease now", m)
	}
	for _, cmd := range remote.sent(ownWebAlias) {
		if strings.Contains(cmd, "merge --ff-only") {
			t.Errorf("fast-forwarded %s, another repository's lease: %s", ownWebAlias, cmd)
		}
	}
}

// TestLeaseOwnership_ParkUnpark_RecordTakenMeanwhile_NeverWrittenOver: park
// and unpark record the lease parked or running once its container stopped
// or started. The record is written only while it is still the lease's own
// (DP-2): another zcp process that took the container again under the alias
// meanwhile — after the stop or start went, or while park unmounted a
// container stopped outside zcp — keeps its record, and the call says the
// lease is lost.
func TestLeaseOwnership_ParkUnpark_RecordTakenMeanwhile_NeverWrittenOver(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		// stopped: the container is stopped outside zcp before the park, and
		// the other process takes it during the park's unmount.
		stopped bool
		// afterCall: the other process takes it once this call went through.
		afterCall string
		unpark    bool
	}{
		{name: "a park, taken right after its stop", afterCall: "stop"},
		{name: "an unpark, taken right after its start", afterCall: "start", unpark: true},
		{name: "a park of a container stopped outside zcp, taken during the unmount", stopped: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			of.acquire(t, groupHQPool)
			if tt.unpark {
				if _, err := LeasePark(context.Background(), of.deps, ownHQAlias); err != nil {
					t.Fatalf("park before the unpark: %v", err)
				}
			}
			if tt.stopped {
				c := of.hq.get(ownHQContainer)
				c.Status = platform.SwarmContainerStopped
				of.hq.put(c, of.hq.leaseOf(ownHQContainer))
			}
			var once sync.Once
			take := func() { once.Do(func() { of.reacquireHQ(t) }) }
			if tt.stopped {
				of.deps.Host.Mounter = &takeOnUnmount{lockedMounter: of.mounter, take: take}
			}
			of.routeHQ(hookOrch{poolOrch: of.hq, after: func(call string) {
				if call == tt.afterCall {
					take()
				}
			}})

			var v LeaseView
			var err error
			if tt.unpark {
				v, err = LeaseUnpark(context.Background(), of.deps, ownHQAlias)
			} else {
				v, err = LeasePark(context.Background(), of.deps, ownHQAlias)
			}
			if rec := of.rawRecord(t, ownHQAlias); rec == nil || rec["lease"] != ownNewLease {
				t.Errorf("record = %v, want the other process's, never written over", rec)
			}
			if err == nil && v.State != LeaseStateLost {
				t.Errorf("view = %+v, want the old lease lost (or a refusal)", v)
			}
		})
	}
}

// takeOnUnmount is the fixture's mounter, running take at the park's unmount.
type takeOnUnmount struct {
	*lockedMounter
	take func()
}

func (m *takeOnUnmount) Unmount(ctx context.Context, h, p string) error {
	m.take()
	return m.lockedMounter.Unmount(ctx, h, p)
}

// readFails is a pool orchestrator whose read by id of one container fails.
type readFails struct {
	*warmOrch
	id string
}

func (r readFails) Container(ctx context.Context, id string) (platform.SwarmContainer, error) {
	if id == r.id {
		return platform.SwarmContainer{}, platform.NewPlatformError(platform.ErrNetworkError, "the orchestrator did not answer: i/o timeout", "")
	}
	return r.warmOrch.Container(ctx, id)
}

// TestLeaseStatus_WarmCandidateUnread_ReadyAbsent: a warm candidate — free,
// ACTIVE, current, clean — is warm only once its read by id finds a snapshot.
// One whose read fails is not counted, so the pool's warm containers are
// unknown: status reports ready absent with why, never a guessed 0 (DP-27).
func TestLeaseStatus_WarmCandidateUnread_ReadyAbsent(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	orch.add(3, "", true, false, 1)
	wf := newWarmFixture(t, orch)
	wf.deps.Swarm = func(string, string) SwarmAPI { return readFails{warmOrch: orch, id: "c-3"} }

	status, err := LeaseStatus(context.Background(), wf.deps, testPoolHost)
	if err != nil {
		t.Fatal(err)
	}
	if w := poolWarmOf(t, status); w.Ready != nil || w.Note == "" {
		t.Errorf("warm = %+v (ready %v), want ready absent with a note: c-3 could not be read", w, w.Ready)
	}
}

// TestEnsureWarm_CandidateUnread_CreatesNothing: the top-up after an acquire
// counts the pool's warm containers by id. A candidate whose read fails may
// be the warm container the pool already has, so nothing is created — never
// a container beyond the target — and the outcome says the pool was unread.
func TestEnsureWarm_CandidateUnread_CreatesNothing(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(4)
	orch.add(3, "", true, false, 1) // the acquire takes it
	orch.add(4, "", true, false, 1) // warm, its read fails
	wf := newWarmFixture(t, orch)
	wf.deps.Swarm = func(string, string) SwarmAPI { return readFails{warmOrch: orch, id: "c-4"} }

	wf.acquire(t, "feat-a")

	if _, _, creates := orch.log(); len(creates) != 0 {
		t.Errorf("creates = %v, want none: the pool may have its warm container", creates)
	}
	if o := readWarmOutcome(wf.stateDir, testPoolHost); o == nil || o.Outcome != LeaseWarmUnread {
		t.Errorf("outcome = %+v, want unread", o)
	}
}

// goneByID is a platform client whose search still lists the pool (it lags
// writes) while the read by id no longer finds it.
type goneByID struct {
	platform.Client
}

func (goneByID) GetService(context.Context, string) (*platform.ServiceStack, error) {
	return nil, platform.NewPlatformError(platform.ErrServiceNotFound, "Service not found", "")
}

// TestLeaseStatus_ListingFails_KnownLossesAreLost: a lease of a pool that is
// gone, being deleted or re-created since it was taken is lost without asking
// its orchestrator (DP-2, DP-20) — also when the orchestrator does not answer
// the listing, as such a pool's does not: status reports it lost, saying why,
// never unverified, whether the pool was named or every pool listed.
func TestLeaseStatus_ListingFails_KnownLossesAreLost(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		setup    func(fx *leaseFixture)
		recordOf string // the record's pool service id
		want     string
	}{
		{name: "the pool was re-created", recordOf: "svc-old", want: "re-created",
			setup: func(*leaseFixture) {}},
		{name: "the pool is being deleted", recordOf: "svc-pool", want: "being deleted",
			setup: func(fx *leaseFixture) {
				mock := fx.deps.Client.(*platform.Mock)
				svc, _ := mock.GetService(context.Background(), "svc-pool")
				deleting := *svc
				deleting.Status = platform.ServiceStatusDeleting
				mock.WithServices([]platform.ServiceStack{deleting})
			}},
		{name: "the pool is gone, the search lagging", recordOf: "svc-pool", want: "no longer exists",
			setup: func(fx *leaseFixture) { fx.deps.Client = goneByID{Client: fx.deps.Client} }},
	} {
		for _, named := range []bool{true, false} {
			t.Run(tt.name+map[bool]string{true: ", pool named", false: ", every pool"}[named], func(t *testing.T) {
				t.Parallel()
				fx := newLeaseFixture(t)
				fx.swarm.listErr = noRouteToHost()
				recordLease(t, fx.stateDir, testPoolHost, tt.recordOf, 3, "feat")
				tt.setup(fx)
				pool := ""
				if named {
					pool = testPoolHost
				}

				status, err := LeaseStatus(context.Background(), fx.deps, pool)
				if err != nil {
					t.Fatalf("LeaseStatus: %v", err)
				}
				i := slices.IndexFunc(status.Leases, func(v LeaseView) bool { return v.Alias == "devpool-3" })
				if i < 0 {
					t.Fatalf("leases = %+v, want devpool-3", status.Leases)
				}
				if v := status.Leases[i]; v.State != LeaseStateLost || v.Unverified || !strings.Contains(v.Note, tt.want) {
					t.Errorf("devpool-3 = %+v, want lost, saying %q", v, tt.want)
				}
			})
		}
	}
}

// TestLeaseStatus_ListingFails_RecreatedLeaseLostBesideUnread: a status of
// every pool whose orchestrator does not list its containers reports each
// lease only it can decide unverified, with why — and a lease taken on the
// pool before it was re-created lost all the same, since the new pool holds
// nothing of it (DP-2, DP-20).
func TestLeaseStatus_ListingFails_RecreatedLeaseLostBesideUnread(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	fx.swarm.listErr = noRouteToHost()
	recordLease(t, fx.stateDir, testPoolHost, "svc-old", 3, "feat")
	recordLease(t, fx.stateDir, testPoolHost, "svc-pool", 4, "feat")

	status, err := LeaseStatus(context.Background(), fx.deps, "")
	if err != nil {
		t.Fatalf("LeaseStatus: %v", err)
	}
	byAlias := map[string]LeaseView{}
	for _, v := range status.Leases {
		byAlias[v.Alias] = v
	}
	if v := byAlias["devpool-3"]; v.State != LeaseStateLost || v.Unverified || !strings.Contains(v.Note, "re-created") {
		t.Errorf("devpool-3 = %+v, want lost: the pool was re-created since it was taken", v)
	}
	if v := byAlias["devpool-4"]; !v.Unverified {
		t.Errorf("devpool-4 = %+v, want unverified: only the orchestrator decides it", v)
	}
	if len(status.Unread) != 1 || status.Unread[0].Pool != testPoolHost {
		t.Errorf("unread = %+v, want the pool named", status.Unread)
	}
}
