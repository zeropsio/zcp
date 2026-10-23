// Tests for: ops/lease_group.go — a group acquire leases one container per
// pool on one branch, all or none, and zcp holds one lease per repository,
// branch and pool (docs/spec-workflows.md §8 DP-17).
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

const (
	groupHQPool  = "hqpool"
	groupWebPool = "webpool"
	// groupBase is the main commit the fake origin answers a new branch was
	// cut from.
	groupBase = "9f8e7d6c5b4a39281706f5e4d3c2b1a098765432"
)

// poolOrch is a pool orchestrator the group tests control: every acquire
// reserves a fresh container, a release frees it. It is safe for concurrent
// use, and gate (when set) holds every acquire until it is closed.
type poolOrch struct {
	pool, svcID string

	mu         sync.Mutex
	next       int64
	containers []platform.SwarmContainer
	leases     map[string]string // container id → lease
	acquires   int
	acquireErr error
	releases   []fakeRelease
	releaseErr error
	gate       chan struct{}
	entered    chan struct{} // receives once per acquire that reached the orchestrator
}

func newPoolOrch(pool, svcID string, first int64) *poolOrch {
	return &poolOrch{pool: pool, svcID: svcID, next: first, leases: map[string]string{}}
}

func (o *poolOrch) Pool(context.Context) (platform.SwarmPool, error) {
	return platform.SwarmPool{MaximumContainerCount: 4}, nil
}

func (o *poolOrch) Containers(context.Context) ([]platform.SwarmContainer, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]platform.SwarmContainer(nil), o.containers...), nil
}

func (o *poolOrch) Acquire(context.Context, platform.SwarmAcquireRequest) (platform.SwarmAcquired, error) {
	o.mu.Lock()
	gate, entered := o.gate, o.entered
	o.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if gate != nil {
		<-gate
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.acquires++
	if o.acquireErr != nil {
		return platform.SwarmAcquired{}, o.acquireErr
	}
	n := o.next
	o.next++
	c := platform.SwarmContainer{
		ID: fmt.Sprintf("c-%s-%d", o.pool, n), Number: n, Status: "ACTIVE", ServiceID: o.svcID, LockState: leaseLockReserved,
		Hostname: "node-id-" + strconv.FormatInt(n, 10) + ".runtime." + o.pool + ".zerops",
	}
	lease := fmt.Sprintf("%s-lease-%d-0123456789abcdef", o.pool, n)
	o.containers = append(o.containers, c)
	o.leases[c.ID] = lease
	return platform.SwarmAcquired{Container: c, Lease: platform.SwarmLease(lease)}, nil
}

func (o *poolOrch) Release(_ context.Context, id, lease string, reset bool) (platform.SwarmContainer, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.releases = append(o.releases, fakeRelease{id, lease, reset})
	if o.releaseErr != nil {
		return platform.SwarmContainer{}, o.releaseErr
	}
	for i, c := range o.containers {
		if c.ID == id && o.leases[id] == lease {
			o.containers[i].LockState = ""
			delete(o.leases, id)
		}
	}
	return platform.SwarmContainer{ID: id}, nil
}

func (o *poolOrch) Renew(context.Context, string, string) (platform.SwarmContainer, error) {
	return platform.SwarmContainer{}, nil
}

func (o *poolOrch) Delete(context.Context, string, string) error { return nil }

func (o *poolOrch) Rollout(context.Context) (platform.SwarmRollout, error) {
	return platform.SwarmRollout{}, nil
}

func (o *poolOrch) acquireCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.acquires
}

func (o *poolOrch) releaseLog() []fakeRelease {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]fakeRelease(nil), o.releases...)
}

// groupFixture is a project holding two pools, hqpool and webpool, each with
// its own orchestrator, and ssh into the leased containers whose checkouts
// answer that a new branch was cut from groupBase.
type groupFixture struct {
	deps     LeaseDeps
	stateDir string
	hq, web  *poolOrch
	ssh      *leaseSSH
}

func poolService(id, name string) platform.ServiceStack {
	return platform.ServiceStack{ID: id, Name: name, Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}}
}

func newGroupFixture(t *testing.T) *groupFixture {
	t.Helper()
	gf := &groupFixture{stateDir: t.TempDir(), hq: newPoolOrch(groupHQPool, "svc-hq", 3), web: newPoolOrch(groupWebPool, "svc-web", 7), ssh: &leaseSSH{}}
	gf.ssh.setHandler(func(_, command string) ([]byte, error) {
		if strings.Contains(command, "fetch -q --prune origin") {
			return []byte("@@zcp-lease base " + groupBase + "\n"), nil
		}
		return nil, nil
	})
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "proj"}).
		WithServices([]platform.ServiceStack{
			poolService("svc-hq", groupHQPool), poolService("svc-web", groupWebPool),
			{ID: "svc-app", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-hq", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: "hq-token", Sensitive: true}}).
		WithServiceEnv("svc-web", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: "web-token", Sensitive: true}})
	orchs := map[string]SwarmAPI{orchestratorURL(groupHQPool): gf.hq, orchestratorURL(groupWebPool): gf.web}
	gf.deps = LeaseDeps{
		Client: mock, ProjectID: "p1", StateDir: gf.stateDir, SSH: gf.ssh,
		Swarm: func(baseURL, _ string) SwarmAPI { return orchs[baseURL] },
		Env:   func(string) (string, bool) { return "", false },
	}
	return gf
}

func (gf *groupFixture) records(t *testing.T) map[string]leaseRecord {
	t.Helper()
	recs, err := readLeaseRecords(gf.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]leaseRecord{}
	for _, r := range recs {
		out[r.alias()] = r
	}
	return out
}

// TestLeaseAcquireGroup_AllPoolsCheckedBeforeAnyReservation: every pool, the
// branch and the repository are checked before a single container is
// reserved, so a group acquire refused for one pool reserves none.
func TestLeaseAcquireGroup_AllPoolsCheckedBeforeAnyReservation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		pools    []string
		branch   string
		noBearer bool // webpool's bearer cannot be found
		wantCode string
		wantText string
	}{
		{name: "an unknown pool", pools: []string{groupHQPool, "nosuch"}, branch: "feat", wantCode: platform.ErrServiceNotFound, wantText: "nosuch"},
		{name: "a service that is not a pool", pools: []string{groupHQPool, "appdev"}, branch: "feat", wantCode: platform.ErrInvalidParameter, wantText: "appdev"},
		{name: "a pool without a bearer", pools: []string{groupHQPool, groupWebPool}, branch: "feat", noBearer: true, wantCode: platform.ErrPrerequisiteMissing, wantText: groupWebPool},
		{name: "a branch a lease does not take", pools: []string{groupHQPool, groupWebPool}, branch: "feat;x", wantCode: platform.ErrInvalidParameter, wantText: "branch"},
		{name: "a pool named twice", pools: []string{groupHQPool, groupWebPool, groupHQPool}, branch: "feat", wantCode: platform.ErrInvalidParameter, wantText: groupHQPool},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gf := newGroupFixture(t)
			if tt.noBearer {
				gf.deps.Client.(*platform.Mock).WithServiceEnv("svc-web", nil)
			}
			_, err := LeaseAcquireGroup(context.Background(), gf.deps, tt.pools, testRepo, tt.branch)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != tt.wantCode {
				t.Fatalf("err = %v, want a %s refusal", err, tt.wantCode)
			}
			if !strings.Contains(pe.Message, tt.wantText) {
				t.Errorf("refusal %q should name %q", pe.Message, tt.wantText)
			}
			if n := gf.hq.acquireCount() + gf.web.acquireCount(); n != 0 {
				t.Errorf("%d container(s) reserved before the refusal; a group acquire checks every pool first", n)
			}
			if len(gf.records(t)) != 0 {
				t.Errorf("a refused group acquire left records: %v", gf.records(t))
			}
		})
	}
}

// TestLeaseAcquireGroup_PoolsWithDifferentDefaultRepos_Refused: without repo
// the group's repository is the pools' default, which must be the same one.
func TestLeaseAcquireGroup_PoolsWithDifferentDefaultRepos_Refused(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	if err := storePoolDefaultRepo(gf.stateDir, groupHQPool, "https://github.com/acme/hq"); err != nil {
		t.Fatal(err)
	}
	if err := storePoolDefaultRepo(gf.stateDir, groupWebPool, "https://github.com/acme/web"); err != nil {
		t.Fatal(err)
	}
	_, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupHQPool, groupWebPool}, "", "feat")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter {
		t.Fatalf("err = %v, want an INVALID_PARAMETER refusal", err)
	}
	for _, want := range []string{groupHQPool, groupWebPool, "different repositories"} {
		if !strings.Contains(pe.Message, want) {
			t.Errorf("refusal %q lacks %q", pe.Message, want)
		}
	}
	if !strings.Contains(pe.Suggestion, "repo") {
		t.Errorf("suggestion %q should say to pass repo", pe.Suggestion)
	}
	if n := gf.hq.acquireCount() + gf.web.acquireCount(); n != 0 {
		t.Errorf("%d container(s) reserved for a group whose repository is ambiguous", n)
	}
}

// A pool with no default joins the group's repository, which the pools that
// have one agree on; the pool without one records it.
func TestLeaseAcquireGroup_OneDefaultRepo_IsTheGroups(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	if err := storePoolDefaultRepo(gf.stateDir, groupHQPool, testRepo); err != nil {
		t.Fatal(err)
	}
	views, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupHQPool, groupWebPool}, "", "feat")
	if err != nil {
		t.Fatalf("LeaseAcquireGroup: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("views = %+v, want two", views)
	}
	if got := readPoolDefaultRepo(gf.stateDir, groupWebPool); got != "https://github.com/acme/app" {
		t.Errorf("webpool's default = %q, want the group's repository", got)
	}
	for alias, r := range gf.records(t) {
		if r.Repo != "https://github.com/acme/app" {
			t.Errorf("%s repo = %q, want the canonical group repository", alias, r.Repo)
		}
	}
}

// TestLeaseAcquireGroup_MembersInPoolsOrder: the result lists one held lease
// per pool, in the order the pools were named, each with its acquiredAt and
// the record's repository and branch.
func TestLeaseAcquireGroup_MembersInPoolsOrder(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	views, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupWebPool, groupHQPool}, testRepo, "feat")
	if err != nil {
		t.Fatalf("LeaseAcquireGroup: %v", err)
	}
	if len(views) != 2 || views[0].Alias != "webpool-7" || views[1].Alias != "hqpool-3" {
		t.Fatalf("views = %+v, want webpool-7 then hqpool-3 (the pools' order)", views)
	}
	for _, v := range views {
		if v.State != LeaseStateHeld || v.Branch != "feat" || v.AcquiredAt == "" || v.Reused {
			t.Errorf("view = %+v, want a fresh held lease on feat with its acquiredAt", v)
		}
	}
	recs := gf.records(t)
	if len(recs) != 2 {
		t.Fatalf("records = %v, want two", recs)
	}
	for alias, r := range recs {
		if r.Repo != "https://github.com/acme/app" || r.Branch != "feat" {
			t.Errorf("%s = repo %q branch %q, want the canonical repository and feat", alias, r.Repo, r.Branch)
		}
	}
}

// TestLeaseAcquireGroup_ReservesInParallel: the members' reservations run at
// once — an acquire takes tens of seconds, so one after the other would add
// them up.
func TestLeaseAcquireGroup_ReservesInParallel(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gate := make(chan struct{})
	entered := make(chan struct{}, 2)
	for _, o := range []*poolOrch{gf.hq, gf.web} {
		o.gate, o.entered = gate, entered
	}
	done := make(chan error, 1)
	go func() {
		_, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupHQPool, groupWebPool}, testRepo, "feat")
		done <- err
	}()
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			close(gate)
			t.Fatal("the second reservation did not start while the first was still waiting: they run one after the other")
		}
	}
	close(gate)
	if err := <-done; err != nil {
		t.Fatalf("LeaseAcquireGroup: %v", err)
	}
}

// TestLeaseAcquireGroup_OneMemberFails_HandsBackThisCallsMembers: when one
// member cannot be leased, every container this call reserved goes back
// (reset), no record is left, and the refusal names the failing pool and
// where each other container went.
func TestLeaseAcquireGroup_OneMemberFails_HandsBackThisCallsMembers(t *testing.T) {
	t.Parallel()

	t.Run("a reservation is refused", func(t *testing.T) {
		t.Parallel()
		gf := newGroupFixture(t)
		gf.web.acquireErr = &platform.SwarmError{Status: 409, Code: "containerAction", Reason: platform.SwarmReasonAllReserved, Retryable: true, Message: "all containers are reserved"}

		_, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupHQPool, groupWebPool}, testRepo, "feat")
		var pe *platform.PlatformError
		if !errors.As(err, &pe) {
			t.Fatalf("err = %v, want a platform error", err)
		}
		if !strings.Contains(pe.Message, groupWebPool) || !strings.Contains(pe.Message, "full") {
			t.Errorf("message %q should carry webpool's own refusal", pe.Message)
		}
		if !strings.Contains(pe.Message, "hqpool-3") || !strings.Contains(pe.Message, "released with a reset") {
			t.Errorf("message %q should say hqpool-3 was handed back with a reset", pe.Message)
		}
		if rel := gf.hq.releaseLog(); len(rel) != 1 || rel[0].id != "c-hqpool-3" || !rel[0].reset {
			t.Errorf("hqpool releases = %+v, want c-hqpool-3 released with reset", rel)
		}
		if len(gf.records(t)) != 0 {
			t.Errorf("records left after a failed group acquire: %v", gf.records(t))
		}
		if strings.Contains(pe.Message+pe.Suggestion, "lease-3-") {
			t.Errorf("refusal leaks a lease: %q", pe.Message)
		}
	})

	t.Run("a checkout fails", func(t *testing.T) {
		t.Parallel()
		gf := newGroupFixture(t)
		gf.ssh.setHandler(func(host, command string) ([]byte, error) {
			if strings.Contains(command, "fetch -q --prune origin") {
				if host == "webpool-7" {
					return nil, sshFailure("fatal: repository not found", exitError(t, 128))
				}
				return []byte("@@zcp-lease base " + groupBase + "\n"), nil
			}
			return nil, nil
		})
		_, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupHQPool, groupWebPool}, testRepo, "feat")
		var pe *platform.PlatformError
		if !errors.As(err, &pe) {
			t.Fatalf("err = %v, want a platform error", err)
		}
		if !strings.Contains(pe.Message, "webpool-7") || !strings.Contains(pe.Message, "hqpool-3") {
			t.Errorf("message %q should name the failed checkout and the other member handed back", pe.Message)
		}
		for name, o := range map[string]*poolOrch{"hqpool": gf.hq, "webpool": gf.web} {
			if rel := o.releaseLog(); len(rel) != 1 || !rel[0].reset {
				t.Errorf("%s releases = %+v, want its container released with reset", name, rel)
			}
		}
		if len(gf.records(t)) != 0 {
			t.Errorf("records left after a failed group acquire: %v", gf.records(t))
		}
	})

	t.Run("a hand-back fails", func(t *testing.T) {
		t.Parallel()
		gf := newGroupFixture(t)
		gf.web.acquireErr = &platform.SwarmError{Status: 409, Code: "containerAction", Reason: platform.SwarmReasonAllReserved, Message: "all containers are reserved"}
		gf.hq.releaseErr = &platform.SwarmError{Status: 409, Code: "containerAction", Reason: "container-busy", Retryable: true, Message: "work in progress"}

		_, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupHQPool, groupWebPool}, testRepo, "feat")
		var pe *platform.PlatformError
		if !errors.As(err, &pe) {
			t.Fatalf("err = %v, want a platform error", err)
		}
		if !strings.Contains(pe.Message, "hqpool-3 stays reserved") {
			t.Errorf("message %q should say hqpool-3 stays reserved", pe.Message)
		}
		if !strings.Contains(pe.Suggestion, "zerops_lease action=release lease=hqpool-3 force=true") {
			t.Errorf("suggestion %q should name the forced release that frees hqpool-3", pe.Suggestion)
		}
		if _, kept := gf.records(t)["hqpool-3"]; !kept {
			t.Error("the record of a container that could not be handed back was dropped")
		}
	})

	t.Run("a member already held stays", func(t *testing.T) {
		t.Parallel()
		gf := newGroupFixture(t)
		if _, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupHQPool}, testRepo, "feat"); err != nil {
			t.Fatal(err)
		}
		gf.web.acquireErr = &platform.SwarmError{Status: 409, Code: "containerAction", Reason: platform.SwarmReasonAllReserved, Message: "all containers are reserved"}

		_, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupHQPool, groupWebPool}, testRepo, "feat")
		if err == nil {
			t.Fatal("want a refusal")
		}
		if rel := gf.hq.releaseLog(); len(rel) != 0 {
			t.Errorf("hqpool releases = %+v: the lease held before this call must stay", rel)
		}
		if _, kept := gf.records(t)["hqpool-3"]; !kept {
			t.Error("the record of the lease held before this call was dropped")
		}
		if !strings.Contains(err.Error(), "hqpool-3") {
			t.Errorf("refusal %q should say hqpool-3 stays held", err)
		}
	})
}

// TestLeaseAcquireGroup_NewBranch_EveryMemberCutFromOneBase: a branch new to
// origin is cut from one main commit for every member — the first member's
// checkout answers the commit, the others are cut from it — and each record
// keeps it, so a member joining later is cut from it too.
func TestLeaseAcquireGroup_NewBranch_EveryMemberCutFromOneBase(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	if _, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupHQPool, groupWebPool}, testRepo, "feat"); err != nil {
		t.Fatalf("LeaseAcquireGroup: %v", err)
	}
	byHost := map[string]string{}
	for _, c := range gf.ssh.checkouts() {
		byHost[c.hostname] = c.command
	}
	if strings.Contains(byHost["hqpool-3"], groupBase) {
		t.Errorf("the first member was handed a base before any checkout found one:\n%s", byHost["hqpool-3"])
	}
	if !strings.Contains(byHost["webpool-7"], shellQuote(groupBase)) {
		t.Errorf("webpool-7 was not cut from the first member's base %s:\n%s", groupBase, byHost["webpool-7"])
	}
	for alias, r := range gf.records(t) {
		if r.Base != groupBase {
			t.Errorf("%s base = %q, want %s", alias, r.Base, groupBase)
		}
	}

	// A third pool's lease joining later is cut from the recorded base.
	gf.ssh.reset()
	gf.deps.Client.(*platform.Mock).WithServices([]platform.ServiceStack{poolService("svc-hq", groupHQPool), poolService("svc-web", groupWebPool), poolService("svc-api", "apipool")}).
		WithServiceEnv("svc-api", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: "api-token", Sensitive: true}})
	api := newPoolOrch("apipool", "svc-api", 1)
	prev := gf.deps.Swarm
	gf.deps.Swarm = func(baseURL, token string) SwarmAPI {
		if baseURL == orchestratorURL("apipool") {
			return api
		}
		return prev(baseURL, token)
	}
	if _, err := LeaseAcquire(context.Background(), gf.deps, "apipool", testRepo, "feat"); err != nil {
		t.Fatalf("joining acquire: %v", err)
	}
	cmds := gf.ssh.checkouts()
	if len(cmds) != 1 || cmds[0].hostname != "apipool-1" || !strings.Contains(cmds[0].command, shellQuote(groupBase)) {
		t.Errorf("checkouts = %+v, want apipool-1 cut from the group's base %s", cmds, groupBase)
	}
}

// TestLeaseAcquire_HeldBranchOnPool_ReturnsHeldLeaseReused: zcp holds one
// lease per repository, branch and pool. A second acquire of the same branch
// on the same pool returns the held lease, marked reused, and reserves
// nothing, checks nothing out and starts nothing.
func TestLeaseAcquire_HeldBranchOnPool_ReturnsHeldLeaseReused(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	first, err := LeaseAcquire(context.Background(), gf.deps, groupHQPool, testRepo, "feat")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	gf.ssh.reset()

	again, err := LeaseAcquire(context.Background(), gf.deps, groupHQPool, "https://github.com/acme/app/", "feat")
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if again.Alias != first.Alias || again.State != LeaseStateHeld || !again.Reused || again.AcquiredAt != first.AcquiredAt {
		t.Errorf("second acquire = %+v, want %s held, reused, acquired at %s", again, first.Alias, first.AcquiredAt)
	}
	if first.Reused {
		t.Errorf("the first acquire is marked reused: %+v", first)
	}
	if n := gf.hq.acquireCount(); n != 1 {
		t.Errorf("the orchestrator reserved %d containers, want 1", n)
	}
	if cmds := gf.ssh.commands(); len(cmds) != 0 {
		t.Errorf("a reused lease ran %d command(s) in its container: %v", len(cmds), cmds)
	}

	// Another branch on the pool, or the same branch of another repository, is another lease.
	if v, err := LeaseAcquire(context.Background(), gf.deps, groupHQPool, testRepo, "other"); err != nil || v.Reused || v.Alias == first.Alias {
		t.Errorf("another branch = %+v, %v; want a lease of its own", v, err)
	}
	if v, err := LeaseAcquire(context.Background(), gf.deps, groupHQPool, "https://github.com/acme/other", "feat"); err != nil || v.Reused {
		t.Errorf("another repository = %+v, %v; want a lease of its own", v, err)
	}
	// A group naming the held pool keeps the lease and leases only the rest.
	views, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupHQPool, groupWebPool}, testRepo, "feat")
	if err != nil {
		t.Fatalf("group acquire: %v", err)
	}
	if len(views) != 2 || views[0].Alias != first.Alias || !views[0].Reused || views[1].Reused {
		t.Errorf("group = %+v, want %s reused then a fresh webpool lease", views, first.Alias)
	}
}

// A record whose lease ended (lost) is not a held lease of its branch: the
// acquire is refused naming it and the release that drops its record, and
// reserves nothing — zcp keeps one record per repository, branch and pool.
func TestLeaseAcquire_LostLeaseOfTheBranch_RefusedNamingTheRelease(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	if _, err := LeaseAcquire(context.Background(), gf.deps, groupHQPool, testRepo, "feat"); err != nil {
		t.Fatal(err)
	}
	gf.hq.mu.Lock()
	gf.hq.containers = nil // the container is gone from the pool
	gf.hq.mu.Unlock()

	_, err := LeaseAcquire(context.Background(), gf.deps, groupHQPool, testRepo, "feat")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
		t.Fatalf("err = %v, want a PREREQUISITE_MISSING refusal", err)
	}
	if !strings.Contains(pe.Message, "hqpool-3") || !strings.Contains(pe.Message, "lost") {
		t.Errorf("message %q should name the lost lease", pe.Message)
	}
	if !strings.Contains(pe.Suggestion, "zerops_lease action=release lease=hqpool-3") {
		t.Errorf("suggestion %q should name the release that drops its record", pe.Suggestion)
	}
	if n := gf.hq.acquireCount(); n != 1 {
		t.Errorf("the orchestrator reserved %d containers, want only the first", n)
	}
}

// TestLeaseAcquire_ConcurrentSameKey_ReservesOnce: two acquires of one
// repository, branch and pool at once reserve one container; the one that
// waited returns it, reused.
func TestLeaseAcquire_ConcurrentSameKey_ReservesOnce(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gate := make(chan struct{})
	gf.hq.gate, gf.hq.entered = gate, make(chan struct{}, 2)

	type outcome struct {
		view LeaseView
		err  error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			v, err := LeaseAcquire(context.Background(), gf.deps, groupHQPool, testRepo, "feat")
			results <- outcome{v, err}
		}()
	}
	<-gf.hq.entered
	// The other acquire waits for the key, never reaching the orchestrator.
	select {
	case <-gf.hq.entered:
		close(gate)
		t.Fatal("two acquires of one key reached the orchestrator at once")
	case <-time.After(200 * time.Millisecond):
	}
	close(gate)
	var got []outcome
	for range 2 {
		got = append(got, <-results)
	}
	for _, o := range got {
		if o.err != nil {
			t.Fatalf("acquire: %v", o.err)
		}
	}
	if got[0].view.Alias != got[1].view.Alias {
		t.Errorf("the acquires returned %s and %s, want one lease", got[0].view.Alias, got[1].view.Alias)
	}
	if got[0].view.Reused == got[1].view.Reused {
		t.Errorf("reused = %v and %v, want exactly the second one reused", got[0].view.Reused, got[1].view.Reused)
	}
	if n := gf.hq.acquireCount(); n != 1 {
		t.Errorf("the orchestrator reserved %d containers, want 1", n)
	}
}

// Acquires of different branches on one pool never wait for each other.
func TestLeaseAcquire_ConcurrentDifferentKeys_RunTogether(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gate := make(chan struct{})
	gf.hq.gate, gf.hq.entered = gate, make(chan struct{}, 2)
	done := make(chan error, 2)
	for _, branch := range []string{"feat-a", "feat-b"} {
		go func() {
			_, err := LeaseAcquire(context.Background(), gf.deps, groupHQPool, testRepo, branch)
			done <- err
		}()
	}
	for range 2 {
		select {
		case <-gf.hq.entered:
		case <-time.After(5 * time.Second):
			close(gate)
			t.Fatal("an acquire of another branch waited for the first one")
		}
	}
	close(gate)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("acquire: %v", err)
		}
	}
}

// TestLeaseGroup_HeldLeasesOfOneRepositoryAndBranch: a lease's group is the
// leases zcp holds of its repository and branch, one per pool, the lease
// itself included, earliest acquired first; a record older than keeping the
// repository groups with its pool's default repository.
func TestLeaseGroup_HeldLeasesOfOneRepositoryAndBranch(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	write := func(pool string, n int64, repo, branch, at string) {
		t.Helper()
		rec := leaseRecord{
			PoolHostname: pool, PoolServiceID: "svc-" + pool, ContainerID: fmt.Sprintf("c-%s-%d", pool, n), ContainerNumber: n,
			ContainerHostname: fmt.Sprintf("node-id-%d.runtime.%s.zerops", n, pool), Lease: testLease, Branch: branch, Repo: repo, AcquiredAt: at,
		}
		if err := writeLeaseRecord(stateDir, rec); err != nil {
			t.Fatal(err)
		}
	}
	if err := storePoolDefaultRepo(stateDir, "apipool", "https://github.com/acme/app.git"); err != nil {
		t.Fatal(err)
	}
	write("webpool", 7, "https://github.com/acme/app", "feat", "2026-10-23T12:00:05Z")
	write("hqpool", 3, "https://github.com/acme/app", "feat", "2026-10-23T12:00:01Z")
	write("apipool", 2, "", "feat", "2026-10-23T12:00:09Z") // predates keeping the repository
	write("hqpool", 4, "https://github.com/acme/app", "other", "2026-10-23T12:00:02Z")
	write("docpool", 5, "https://github.com/acme/docs", "feat", "2026-10-23T12:00:03Z")

	got, err := LeaseGroup(stateDir, "webpool-7")
	if err != nil {
		t.Fatalf("LeaseGroup: %v", err)
	}
	var aliases []string
	for _, m := range got {
		aliases = append(aliases, m.Alias)
	}
	if strings.Join(aliases, ",") != "hqpool-3,webpool-7,apipool-2" {
		t.Errorf("group = %v, want hqpool-3,webpool-7,apipool-2 (one repository and branch, earliest acquired first)", aliases)
	}
	for _, m := range got {
		if m.Alias == "hqpool-3" && (m.Pool != "hqpool" || m.ContainerHostname != "node-id-3.runtime.hqpool.zerops" || m.Branch != "feat") {
			t.Errorf("member = %+v, want hqpool's container on feat", m)
		}
	}
	if raw, _ := json.Marshal(got); strings.Contains(string(raw), testLease) {
		t.Errorf("the group carries a lease: %s", raw)
	}
	if _, err := LeaseGroup(stateDir, "nosuch-1"); err == nil {
		t.Error("the group of an alias zcp holds no lease for must be an error")
	}
}

// The record keeps the base only when the branch was cut new; a branch that
// already is on origin is tracked, and its records keep no base.
func TestLeaseAcquireGroup_BranchOnOrigin_RecordsNoBase(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gf.ssh.setHandler(func(string, string) ([]byte, error) { return nil, nil })
	if _, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupHQPool, groupWebPool}, testRepo, "feat"); err != nil {
		t.Fatal(err)
	}
	for alias, r := range gf.records(t) {
		if r.Base != "" {
			t.Errorf("%s base = %q, want none (the branch was tracked, not cut)", alias, r.Base)
		}
	}
}

// TestLeaseViews_CarryAcquiredAt: every view of a lease carries when it was
// acquired — the acquire's own, status and release alike — the key a client
// rebinds by when the bound lease of a branch ends.
func TestLeaseViews_CarryAcquiredAt(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gf.deps.SSH = nil
	acquired, err := LeaseAcquire(context.Background(), gf.deps, groupHQPool, testRepo, "feat")
	if err != nil {
		t.Fatal(err)
	}
	want := gf.records(t)["hqpool-3"].AcquiredAt
	if want == "" || acquired.AcquiredAt != want {
		t.Errorf("acquire view acquiredAt = %q, want the record's %q", acquired.AcquiredAt, want)
	}
	status, err := LeaseList(context.Background(), gf.deps, groupHQPool)
	if err != nil || len(status) != 1 || status[0].AcquiredAt != want {
		t.Errorf("status = %+v, %v; want hqpool-3 acquired at %q", status, err, want)
	}
	released, err := LeaseRelease(context.Background(), gf.deps, "hqpool-3", true)
	if err != nil || released.AcquiredAt != want {
		t.Errorf("release view = %+v, %v; want acquired at %q", released, err, want)
	}
}
