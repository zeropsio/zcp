// Tests for: ops/lease_group.go — a group acquire leases one container per
// pool on one branch, all or none, and zcp holds one lease per repository,
// branch and pool (docs/spec-workflows.md §8 DP-17); a group release reads
// every lease of a branch before it releases any, and releases all or none
// unless forced (DP-21).
package ops

import (
	"context"
	"errors"
	"fmt"
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
	deletes    []string // container ids deleted (DELETE /container/{id})
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

func (o *poolOrch) Delete(_ context.Context, id, lease string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.deletes = append(o.deletes, id)
	for i, c := range o.containers {
		if c.ID == id && o.leases[id] == lease {
			o.containers = append(o.containers[:i], o.containers[i+1:]...)
			delete(o.leases, id)
			break
		}
	}
	return nil
}

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

func (o *poolOrch) deleteLog() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.deletes...)
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
	// The default is a clone URL: kept as given (DP-18), the record's repo is
	// the identity.
	if got := readPoolDefaultRepo(gf.stateDir, groupWebPool); got != testRepo {
		t.Errorf("webpool's default = %q, want the group's repository as given, %q", got, testRepo)
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
	got := make([]outcome, 0, 2)
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
	aliases := make([]string, 0, len(got))
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
	if dump := fmt.Sprintf("%+v", got); strings.Contains(dump, testLease) {
		t.Errorf("the group carries a lease: %s", dump)
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

// A state directory that cannot be written refuses the acquire before any
// container is taken: the key's lock lives there, ahead of the record.
func TestLeaseAcquire_StateDirUnwritable_TakesNoContainer(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	// A regular file where the state directory should be.
	blocker := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	gf.deps.StateDir = blocker
	if _, err := LeaseAcquire(context.Background(), gf.deps, groupHQPool, testRepo, "feat"); err == nil {
		t.Fatal("want a refusal when the state directory cannot be written")
	}
	if n := gf.hq.acquireCount(); n != 0 {
		t.Errorf("the orchestrator reserved %d container(s) for an acquire that could not record one", n)
	}
}

// TestLeaseAcquireGroup_RepoAsGiven_EveryMemberClonesIt: a group acquire of
// `…/repo.git` clones that URL in every member's container, never its
// canonical `…/repo`, which a plain git server refuses; every member's record
// keeps the canonical identity (DP-18).
func TestLeaseAcquireGroup_RepoAsGiven_EveryMemberClonesIt(t *testing.T) {
	t.Parallel()
	const given = "https://git.example.com/team/repo.git"
	gf := newGroupFixture(t)
	if _, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupHQPool, groupWebPool}, given, "feat"); err != nil {
		t.Fatalf("LeaseAcquireGroup: %v", err)
	}
	cmds := gf.ssh.checkouts()
	if len(cmds) != 2 {
		t.Fatalf("checkouts = %d, want one per member", len(cmds))
	}
	for _, c := range cmds {
		if !strings.Contains(c.command, "git remote add origin "+shellQuote(given)) || strings.Contains(c.command, shellQuote("https://git.example.com/team/repo")) {
			t.Errorf("%s does not clone %s as given:\n%s", c.hostname, given, c.command)
		}
	}
	for alias, r := range gf.records(t) {
		if r.Repo != "https://git.example.com/team/repo" {
			t.Errorf("%s repo = %q, want the canonical identity", alias, r.Repo)
		}
	}
}

// heldGroup leases hqpool-3 and webpool-7 on feat in one call, then makes the
// containers answer the release guard's reads (answerGuard) — every one
// clean — and forgets the acquire's commands.
func (gf *groupFixture) heldGroup(t *testing.T) {
	t.Helper()
	if _, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupHQPool, groupWebPool}, testRepo, "feat"); err != nil {
		t.Fatalf("group acquire: %v", err)
	}
	gf.answerGuard(nil, nil)
	gf.ssh.reset()
}

// answerGuard makes each container answer the release guard's two reads:
// files are its `git status --porcelain` lines and unpushed its commits on no
// remote branch, by alias; an alias named in neither is clean.
func (gf *groupFixture) answerGuard(files map[string]string, unpushed map[string]int) {
	gf.ssh.setHandler(func(host, command string) ([]byte, error) {
		switch {
		case strings.Contains(command, "status --porcelain"):
			return []byte(files[host]), nil
		case strings.Contains(command, "rev-list --count"):
			return []byte(strconv.Itoa(unpushed[host]) + "\n"), nil
		}
		return nil, nil
	})
}

// guardReadHosts are the aliases the release guard read (git status), sorted.
func (gf *groupFixture) guardReadHosts() []string {
	gf.ssh.mu.Lock()
	defer gf.ssh.mu.Unlock()
	var hosts []string
	for _, c := range gf.ssh.calls {
		if strings.Contains(c.command, "status --porcelain") {
			hosts = append(hosts, c.hostname)
		}
	}
	slices.Sort(hosts)
	return hosts
}

// TestLeaseReleaseGroup_OneMemberUnsaved_ReleasesNone: a release of a branch
// reads every lease of it first; one lease holding work a release would lose —
// or one zcp cannot read — refuses the whole branch: nothing is released,
// every record stays, and the refusal names each lease's own state and the
// forced release of the branch.
func TestLeaseReleaseGroup_OneMemberUnsaved_ReleasesNone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		files    map[string]string
		unpushed map[string]int
		readErr  string // the alias whose git status fails (not a git repository)
		want     []string
	}{
		{
			name:  "uncommitted files in one",
			files: map[string]string{"webpool-7": "?? notes.txt\n M src/app.ts\n"},
			want:  []string{"webpool-7", "notes.txt", "src/app.ts", "hqpool-3 holds no unsaved work"},
		},
		{
			name:     "unpushed commits in one",
			unpushed: map[string]int{"hqpool-3": 2},
			want:     []string{"hqpool-3", "2 commit(s) on no remote branch", "webpool-7 holds no unsaved work"},
		},
		{
			name:     "work in both",
			files:    map[string]string{"webpool-7": "?? notes.txt\n"},
			unpushed: map[string]int{"hqpool-3": 1},
			want:     []string{"hqpool-3", "1 commit(s)", "webpool-7", "notes.txt"},
		},
		{
			name:    "one cannot be read",
			readErr: "webpool-7",
			want:    []string{"webpool-7 could not be checked for unsaved work", "not a git repository", "hqpool-3 holds no unsaved work"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gf := newGroupFixture(t)
			gf.heldGroup(t)
			gf.answerGuard(tt.files, tt.unpushed)
			if tt.readErr != "" {
				clean := gf.ssh.fn
				gf.ssh.setHandler(func(host, command string) ([]byte, error) {
					if host == tt.readErr && strings.Contains(command, "status --porcelain") {
						out := "fatal: not a git repository (or any of the parent directories): .git"
						return []byte(out), sshFailure(out, exitError(t, 128))
					}
					return clean(host, command)
				})
			}

			views, err := LeaseReleaseGroup(context.Background(), gf.deps, "", "feat", false)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
				t.Fatalf("views %+v, err = %v; want a PREREQUISITE_MISSING refusal", views, err)
			}
			for _, want := range tt.want {
				if !strings.Contains(pe.Message, want) {
					t.Errorf("message %q lacks %q", pe.Message, want)
				}
			}
			if !strings.Contains(pe.Message, "none") {
				t.Errorf("message %q should say no lease of the branch was released", pe.Message)
			}
			if !strings.Contains(pe.Suggestion, "zerops_lease action=release branch=feat force=true") {
				t.Errorf("suggestion %q should name the forced release of the branch", pe.Suggestion)
			}
			if got := gf.guardReadHosts(); strings.Join(got, ",") != "hqpool-3,webpool-7" {
				t.Errorf("guard read %v, want every lease of the branch read before anything is released", got)
			}
			for name, o := range map[string]*poolOrch{"hqpool": gf.hq, "webpool": gf.web} {
				if rel := o.releaseLog(); len(rel) != 0 {
					t.Errorf("%s releases = %+v: a refused branch releases nothing", name, rel)
				}
			}
			if recs := gf.records(t); len(recs) != 2 {
				t.Errorf("records = %v, want both kept", recs)
			}
			if strings.Contains(pe.Message+pe.Suggestion, "-lease-") {
				t.Errorf("refusal leaks a lease: %q", pe.Message)
			}
		})
	}
}

// A branch held by one lease refuses its release as that lease's own release
// refuses it.
func TestLeaseReleaseGroup_OneMember_RefusedAsItsOwnRelease(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	if _, err := LeaseAcquire(context.Background(), gf.deps, groupHQPool, testRepo, "feat"); err != nil {
		t.Fatal(err)
	}
	gf.answerGuard(map[string]string{"hqpool-3": "?? notes.txt\n"}, nil)

	_, groupErr := LeaseReleaseGroup(context.Background(), gf.deps, "", "feat", false)
	_, ownErr := LeaseRelease(context.Background(), gf.deps, "hqpool-3", false)
	if groupErr == nil || ownErr == nil || groupErr.Error() != ownErr.Error() {
		t.Errorf("group refusal %v, want the lease's own %v", groupErr, ownErr)
	}
	if rel := gf.hq.releaseLog(); len(rel) != 0 {
		t.Errorf("releases = %+v, want none", rel)
	}
}

// orderedOrch is a poolOrch whose releases are logged beside the guard's reads.
type orderedOrch struct {
	*poolOrch
	log func(string)
}

func (o orderedOrch) Release(ctx context.Context, id, lease string, reset bool) (platform.SwarmContainer, error) {
	o.log("release " + id)
	return o.poolOrch.Release(ctx, id, lease, reset)
}

// TestLeaseReleaseGroup_ReadsEveryMemberBeforeReleasingAny: a clean branch is
// released lease by lease only once every lease of it was read; each goes back
// with a reset, its record dropped, and the views list them earliest acquired
// first, with their acquiredAt.
func TestLeaseReleaseGroup_ReadsEveryMemberBeforeReleasingAny(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gf.heldGroup(t)
	var mu sync.Mutex
	var events []string
	logEvent := func(e string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}
	clean := gf.ssh.fn
	gf.ssh.setHandler(func(host, command string) ([]byte, error) {
		if strings.Contains(command, "status --porcelain") {
			logEvent("read " + host)
		}
		return clean(host, command)
	})
	orchs := map[string]SwarmAPI{
		orchestratorURL(groupHQPool):  orderedOrch{gf.hq, logEvent},
		orchestratorURL(groupWebPool): orderedOrch{gf.web, logEvent},
	}
	gf.deps.Swarm = func(baseURL, _ string) SwarmAPI { return orchs[baseURL] }
	recs := gf.records(t)

	views, err := LeaseReleaseGroup(context.Background(), gf.deps, "", "feat", false)
	if err != nil {
		t.Fatalf("LeaseReleaseGroup: %v", err)
	}
	mu.Lock()
	got := slices.Clone(events)
	mu.Unlock()
	firstRelease, lastRead := -1, -1
	for i, e := range got {
		switch {
		case strings.HasPrefix(e, "read "):
			lastRead = i
		case strings.HasPrefix(e, "release ") && firstRelease < 0:
			firstRelease = i
		}
	}
	if len(got) != 4 || firstRelease < 0 || lastRead > firstRelease {
		t.Errorf("events = %v, want both reads before the first release", got)
	}
	if len(views) != 2 || views[0].Alias != "hqpool-3" || views[1].Alias != "webpool-7" {
		t.Fatalf("views = %+v, want hqpool-3 then webpool-7", views)
	}
	for _, v := range views {
		if v.State != LeaseStateReleased || v.Branch != "feat" || v.AcquiredAt != recs[v.Alias].AcquiredAt || v.Error != "" {
			t.Errorf("view = %+v, want released on feat with its acquiredAt", v)
		}
	}
	for name, o := range map[string]*poolOrch{"hqpool": gf.hq, "webpool": gf.web} {
		if rel := o.releaseLog(); len(rel) != 1 || !rel[0].reset {
			t.Errorf("%s releases = %+v, want its container released with reset", name, rel)
		}
	}
	if left := gf.records(t); len(left) != 0 {
		t.Errorf("records left: %v", left)
	}
}

// TestLeaseReleaseGroup_Force_ReleasesEvery: force releases every lease of the
// branch, whatever it holds, reading none of them, each with a reset.
func TestLeaseReleaseGroup_Force_ReleasesEvery(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gf.heldGroup(t)
	gf.answerGuard(map[string]string{"hqpool-3": "?? a.txt\n", "webpool-7": " M b.go\n"}, map[string]int{"webpool-7": 3})

	views, err := LeaseReleaseGroup(context.Background(), gf.deps, "", "feat", true)
	if err != nil {
		t.Fatalf("a forced release must release every lease: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("views = %+v, want both leases", views)
	}
	for _, v := range views {
		if v.State != LeaseStateReleased {
			t.Errorf("view = %+v, want released", v)
		}
	}
	for name, o := range map[string]*poolOrch{"hqpool": gf.hq, "webpool": gf.web} {
		if rel := o.releaseLog(); len(rel) != 1 || !rel[0].reset {
			t.Errorf("%s releases = %+v, want its container released with reset", name, rel)
		}
	}
	if cmds := gf.ssh.commands(); len(cmds) != 0 {
		t.Errorf("a forced release read the containers: %v", cmds)
	}
	if left := gf.records(t); len(left) != 0 {
		t.Errorf("records left: %v", left)
	}
}

// TestLeaseReleaseGroup_OrchestratorFailsOne_PerMemberResult: once every lease
// passed the guard, one the orchestrator will not release stays held and
// recorded while the others go; the result is per lease — the failed one
// carries its error and the call that finishes the branch.
func TestLeaseReleaseGroup_OrchestratorFailsOne_PerMemberResult(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gf.heldGroup(t)
	gf.web.releaseErr = &platform.SwarmError{Status: 409, Code: "containerAction", Reason: "container-busy", Retryable: true, Message: "work in progress"}

	views, err := LeaseReleaseGroup(context.Background(), gf.deps, "", "feat", false)
	if err != nil {
		t.Fatalf("an orchestrator failure on one lease is that lease's result, not the call's: %v", err)
	}
	if len(views) != 2 || views[0].Alias != "hqpool-3" || views[1].Alias != "webpool-7" {
		t.Fatalf("views = %+v, want hqpool-3 then webpool-7", views)
	}
	if hq := views[0]; hq.State != LeaseStateReleased || hq.Error != "" {
		t.Errorf("hqpool-3 = %+v, want released", hq)
	}
	web := views[1]
	if web.State != LeaseStateHeld || !strings.Contains(web.Error, "work in progress") {
		t.Errorf("webpool-7 = %+v, want held with the orchestrator's refusal as its error", web)
	}
	if !strings.Contains(web.Note, "zerops_lease action=release branch=feat") {
		t.Errorf("webpool-7 note %q should name the call that finishes the branch", web.Note)
	}
	recs := gf.records(t)
	if _, kept := recs["webpool-7"]; !kept || len(recs) != 1 {
		t.Errorf("records = %v, want webpool-7's alone kept", recs)
	}
	if strings.Contains(web.Error+web.Note, "-lease-") {
		t.Errorf("the result leaks a lease: %+v", web)
	}
}

// TestLeaseReleaseGroup_Again_FinishesTheGroup: the same call again releases
// what is left of the branch — the leases already released have no record
// left — and once nothing is left it says zcp holds no lease of the branch.
func TestLeaseReleaseGroup_Again_FinishesTheGroup(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gf.heldGroup(t)
	gf.web.releaseErr = &platform.SwarmError{Status: 409, Code: "containerAction", Reason: "container-busy", Retryable: true, Message: "work in progress"}
	if _, err := LeaseReleaseGroup(context.Background(), gf.deps, "", "feat", false); err != nil {
		t.Fatal(err)
	}
	gf.web.mu.Lock()
	gf.web.releaseErr = nil
	gf.web.mu.Unlock()
	gf.ssh.reset()

	views, err := LeaseReleaseGroup(context.Background(), gf.deps, "", "feat", false)
	if err != nil {
		t.Fatalf("the second call: %v", err)
	}
	if len(views) != 1 || views[0].Alias != "webpool-7" || views[0].State != LeaseStateReleased {
		t.Errorf("views = %+v, want webpool-7 alone, released", views)
	}
	if got := gf.guardReadHosts(); strings.Join(got, ",") != "webpool-7" {
		t.Errorf("guard read %v, want only what is left of the branch", got)
	}
	if rel := gf.hq.releaseLog(); len(rel) != 1 {
		t.Errorf("hqpool releases = %+v, want only the first call's", rel)
	}
	if left := gf.records(t); len(left) != 0 {
		t.Errorf("records left: %v", left)
	}

	_, err = LeaseReleaseGroup(context.Background(), gf.deps, "", "feat", false)
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter || !strings.Contains(pe.Message, "feat") {
		t.Errorf("err = %v, want an INVALID_PARAMETER refusal naming the branch zcp holds no lease of", err)
	}
}

// TestLeaseReleaseGroup_EachMemberTakesTheOneLeasesPath: each lease of the
// branch is released as one lease is — a container that cannot be reset is
// deleted with its lease (DP-15), a lease of a pool being deleted is dropped as
// lost (DP-20) — never by a second copy of those steps.
func TestLeaseReleaseGroup_EachMemberTakesTheOneLeasesPath(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gf.heldGroup(t)
	gf.web.releaseErr = noPrimarySnapshot()
	hq := poolService("svc-hq", groupHQPool)
	hq.Status = "DELETING"
	gf.deps.Client.(*platform.Mock).WithServices([]platform.ServiceStack{hq, poolService("svc-web", groupWebPool)})

	views, err := LeaseReleaseGroup(context.Background(), gf.deps, "", "feat", true)
	if err != nil {
		t.Fatalf("LeaseReleaseGroup: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("views = %+v, want both leases", views)
	}
	if v := views[0]; v.Alias != "hqpool-3" || v.State != LeaseStateLost || !strings.Contains(v.Note, "being deleted") {
		t.Errorf("hqpool-3 = %+v, want lost: its pool is being deleted", v)
	}
	if v := views[1]; v.Alias != "webpool-7" || v.State != LeaseStateReleased || !strings.Contains(v.Note, "could not be reset") {
		t.Errorf("webpool-7 = %+v, want released by deleting the container it could not reset", v)
	}
	if del := gf.web.deleteLog(); len(del) != 1 || del[0] != "c-webpool-7" {
		t.Errorf("webpool deletes = %v, want c-webpool-7", del)
	}
	if rel := gf.hq.releaseLog(); len(rel) != 0 {
		t.Errorf("hqpool releases = %+v: a pool being deleted is not asked", rel)
	}
	if left := gf.records(t); len(left) != 0 {
		t.Errorf("records left: %v", left)
	}
}

// TestLeaseReleaseGroup_SeveralRepositories_RefusedWithoutRepo: a branch zcp
// leases for two repositories is two groups: a release of it without repo is
// refused naming each repository's leases; with repo it releases only that
// repository's.
func TestLeaseReleaseGroup_SeveralRepositories_RefusedWithoutRepo(t *testing.T) {
	t.Parallel()
	const docs = "https://github.com/acme/docs"
	gf := newGroupFixture(t)
	if _, err := LeaseAcquire(context.Background(), gf.deps, groupHQPool, testRepo, "feat"); err != nil {
		t.Fatal(err)
	}
	if _, err := LeaseAcquire(context.Background(), gf.deps, groupWebPool, docs, "feat"); err != nil {
		t.Fatal(err)
	}
	gf.answerGuard(nil, nil)

	_, err := LeaseReleaseGroup(context.Background(), gf.deps, "", "feat", false)
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter {
		t.Fatalf("err = %v, want an INVALID_PARAMETER refusal", err)
	}
	for _, want := range []string{"https://github.com/acme/app", "hqpool-3", docs, "webpool-7"} {
		if !strings.Contains(pe.Message, want) {
			t.Errorf("message %q lacks %q", pe.Message, want)
		}
	}
	if !strings.Contains(pe.Suggestion, "repo") {
		t.Errorf("suggestion %q should say to pass repo", pe.Suggestion)
	}
	if len(gf.hq.releaseLog())+len(gf.web.releaseLog()) != 0 {
		t.Error("a refused release reached an orchestrator")
	}

	views, err := LeaseReleaseGroup(context.Background(), gf.deps, docs+".git", "feat", false)
	if err != nil {
		t.Fatalf("release of one repository's branch: %v", err)
	}
	if len(views) != 1 || views[0].Alias != "webpool-7" || views[0].State != LeaseStateReleased {
		t.Errorf("views = %+v, want webpool-7 alone, released", views)
	}
	if _, kept := gf.records(t)["hqpool-3"]; !kept {
		t.Error("the other repository's lease of the branch was released")
	}
}

// A branch zcp holds no lease of, a repository it holds none of the branch
// for, or a branch name a lease never takes, is refused before anything is
// read or released.
func TestLeaseReleaseGroup_NothingToRelease_Refused(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gf.heldGroup(t)
	for _, tt := range []struct {
		name, repo, branch, want string
	}{
		{"another branch", "", "other", "other"},
		{"another repository", "https://github.com/acme/docs", "feat", "https://github.com/acme/docs"},
		{"a repository that is no clone URL", "git@github.com:acme/app.git", "feat", "clone URL"},
		{"no branch", "", "", "branch"},
		{"a branch a lease never takes", "", "../feat", "branch"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LeaseReleaseGroup(context.Background(), gf.deps, tt.repo, tt.branch, true)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter || !strings.Contains(pe.Message, tt.want) {
				t.Errorf("err = %v, want an INVALID_PARAMETER refusal mentioning %q", err, tt.want)
			}
		})
	}
	if len(gf.hq.releaseLog())+len(gf.web.releaseLog()) != 0 {
		t.Error("a refused release reached an orchestrator")
	}
	if cmds := gf.ssh.commands(); len(cmds) != 0 {
		t.Errorf("a refused release read the containers: %v", cmds)
	}
}
