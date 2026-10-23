// Tests for: ops/lease.go — dev-pool leases: acquire, status, release.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

const (
	testPoolHost  = "devpool"
	testPoolToken = "orch-api-token-0001"
	testLease     = "0123456789abcdef0123456789abcdef"
	testRepo      = "https://github.com/acme/app.git"
)

// fakeSwarm is an orchestrator the tests control. It records what it is asked.
type fakeSwarm struct {
	containers []platform.SwarmContainer
	acquired   platform.SwarmAcquired
	acquireErr error
	listErr    error
	releaseErr error

	acquireReqs []platform.SwarmAcquireRequest
	releases    []fakeRelease
}

type fakeRelease struct {
	id, lease string
	reset     bool
}

func (f *fakeSwarm) Pool(context.Context) (platform.SwarmPool, error) {
	return platform.SwarmPool{MaximumContainerCount: 4}, nil
}

func (f *fakeSwarm) Containers(context.Context) ([]platform.SwarmContainer, error) {
	return f.containers, f.listErr
}

func (f *fakeSwarm) Acquire(_ context.Context, req platform.SwarmAcquireRequest) (platform.SwarmAcquired, error) {
	f.acquireReqs = append(f.acquireReqs, req)
	return f.acquired, f.acquireErr
}

func (f *fakeSwarm) Release(_ context.Context, id, lease string, reset bool) (platform.SwarmContainer, error) {
	f.releases = append(f.releases, fakeRelease{id, lease, reset})
	return platform.SwarmContainer{ID: id}, f.releaseErr
}

func (f *fakeSwarm) Renew(context.Context, string, string) (platform.SwarmContainer, error) {
	return platform.SwarmContainer{}, nil
}

func (f *fakeSwarm) Delete(context.Context, string, string) error { return nil }

// leaseFixture is a project holding one swarm pool, a plain runtime, and a
// state dir, with the fake orchestrator behind the factory.
type leaseFixture struct {
	deps     LeaseDeps
	swarm    *fakeSwarm
	stateDir string
	gotURL   string
	gotToken string
}

func newLeaseFixture(t *testing.T) *leaseFixture {
	t.Helper()
	fx := &leaseFixture{swarm: &fakeSwarm{}, stateDir: t.TempDir()}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "proj"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: testPoolHost, Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
			{ID: "svc-app", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: testPoolToken, Sensitive: true}})
	fx.deps = LeaseDeps{
		Client: mock, ProjectID: "p1", StateDir: fx.stateDir,
		Swarm: func(baseURL, token string) SwarmAPI {
			fx.gotURL, fx.gotToken = baseURL, token
			return fx.swarm
		},
	}
	return fx
}

func reservedContainer(id string, number int64) platform.SwarmContainer {
	return platform.SwarmContainer{
		ID: id, Number: number, Hostname: nodeHostname(number), Status: "ACTIVE",
		ServiceID: "svc-pool", LockState: "reserved",
	}
}

func nodeHostname(number int64) string {
	return "node-id-" + strconv.FormatInt(number, 10) + ".runtime." + testPoolHost + ".zerops"
}

func TestLeaseList_ReservedWithoutRecord_UnknownHolder(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	fx.swarm.containers = []platform.SwarmContainer{reservedContainer("c-7", 7)}

	got, err := LeaseList(context.Background(), fx.deps, testPoolHost)
	if err != nil {
		t.Fatalf("LeaseList: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("views = %+v, want exactly one", got)
	}
	v := got[0]
	if v.Alias != "devpool-7" || v.Pool != testPoolHost || v.ContainerHostname != nodeHostname(7) {
		t.Errorf("view = %+v, want alias devpool-7 of pool %s on %s", v, testPoolHost, nodeHostname(7))
	}
	if v.State != LeaseStateUnknownHolder {
		t.Errorf("State = %q, want %q (reserved container, no record: a lost acquire response)", v.State, LeaseStateUnknownHolder)
	}
	if fx.gotURL != "http://orch.devpool.zerops" || fx.gotToken != testPoolToken {
		t.Errorf("orchestrator reached at %q with token %q, want http://orch.devpool.zerops with the pool's API_TOKEN", fx.gotURL, fx.gotToken)
	}
}

func TestLeaseAcquire_NonPool_Refused(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)

	_, err := LeaseAcquire(context.Background(), fx.deps, "appdev", testRepo, "feature")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a *PlatformError", err)
	}
	if pe.Code != platform.ErrInvalidParameter {
		t.Errorf("Code = %q, want %q", pe.Code, platform.ErrInvalidParameter)
	}
	if !strings.Contains(pe.Message, "appdev") || !strings.Contains(pe.Message, "nodejs@22") {
		t.Errorf("message %q must name the service and its type", pe.Message)
	}
	if len(fx.swarm.acquireReqs) != 0 {
		t.Errorf("the orchestrator was asked to acquire %d time(s) for a non-pool", len(fx.swarm.acquireReqs))
	}
	if fx.gotURL != "" {
		t.Errorf("an orchestrator client was built (%s) for a non-pool", fx.gotURL)
	}
}

func acquiredAs(number int64) platform.SwarmAcquired {
	c := reservedContainer("c-abc", number)
	return platform.SwarmAcquired{Container: c, Lease: platform.SwarmLease(testLease)}
}

func TestLeaseAcquire_WritesRecord0600(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	fx.swarm.acquired = acquiredAs(3)

	view, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature-x")
	if err != nil {
		t.Fatalf("LeaseAcquire: %v", err)
	}

	if len(fx.swarm.acquireReqs) != 1 || fx.swarm.acquireReqs[0] != (platform.SwarmAcquireRequest{Start: true, Clean: true, LeaseTTLSeconds: 0}) {
		t.Errorf("acquire requests = %+v, want one {start:true, clean:true, leaseTtlSeconds:0}", fx.swarm.acquireReqs)
	}
	if view.Alias != "devpool-3" || view.ContainerHostname != nodeHostname(3) || view.Branch != "feature-x" || view.State != LeaseStateHeld {
		t.Errorf("view = %+v, want held devpool-3 on %s for branch feature-x", view, nodeHostname(3))
	}

	path := filepath.Join(fx.stateDir, "leases", "devpool-3.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("lease record not written at %s: %v", path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("record mode = %o, want 600 (it holds the lease)", perm)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("record is not JSON: %v", err)
	}
	want := map[string]any{
		"poolHostname": testPoolHost, "poolServiceId": "svc-pool",
		"containerId": "c-abc", "containerNumber": float64(3), "containerHostname": nodeHostname(3),
		"lease": testLease, "branch": "feature-x",
	}
	for k, v := range want {
		if rec[k] != v {
			t.Errorf("record[%q] = %v, want %v", k, rec[k], v)
		}
	}
	if at, _ := rec["acquiredAt"].(string); at == "" {
		t.Errorf("record has no acquiredAt")
	}

	// The view that reaches the agent never carries the lease.
	out, mErr := json.Marshal(view)
	if mErr != nil {
		t.Fatal(mErr)
	}
	if strings.Contains(string(out), testLease) {
		t.Errorf("view leaks the lease: %s", out)
	}
}

func TestLeaseList_MissingContainer_ReportedLost(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	for _, n := range []int64{3, 4, 5} {
		fx.swarm.acquired = acquiredAs(n)
		fx.swarm.acquired.Container.ID = "c-" + strconv.FormatInt(n, 10)
		if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "b"+strconv.FormatInt(n, 10)); err != nil {
			t.Fatalf("acquire #%d: %v", n, err)
		}
	}
	// #3 is gone from the orchestrator; #4 is free again (its lease expired or
	// was force-released); #5 is still reserved; #6 is reserved by nobody zcp knows.
	free4 := reservedContainer("c-4", 4)
	free4.LockState = ""
	fx.swarm.containers = []platform.SwarmContainer{free4, reservedContainer("c-5", 5), reservedContainer("c-6", 6)}

	got, err := LeaseList(context.Background(), fx.deps, testPoolHost)
	if err != nil {
		t.Fatalf("LeaseList: %v", err)
	}
	states := map[string]string{}
	for _, v := range got {
		states[v.Alias] = v.State
	}
	want := map[string]string{
		"devpool-3": LeaseStateLost,          // record, container missing
		"devpool-4": LeaseStateLost,          // record, container not reserved
		"devpool-5": LeaseStateHeld,          // record, container reserved
		"devpool-6": LeaseStateUnknownHolder, // reserved, no record
	}
	if len(states) != len(want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
	for alias, w := range want {
		if states[alias] != w {
			t.Errorf("%s = %q, want %q", alias, states[alias], w)
		}
	}
	for _, v := range got {
		if v.State == LeaseStateLost && v.Note == "" {
			t.Errorf("%s is lost but says nothing about what to do", v.Alias)
		}
		if v.Alias == "devpool-5" && v.Branch != "b5" {
			t.Errorf("held view lost its branch: %+v", v)
		}
	}
	// A lost record is reported, never re-leased: no further acquire happened.
	if len(fx.swarm.acquireReqs) != 3 {
		t.Errorf("acquire calls = %d, want only the 3 setup acquires", len(fx.swarm.acquireReqs))
	}
}

func TestLeaseRelease_ResetPassedAndRecordRemoved(t *testing.T) {
	t.Parallel()
	// Every release resets the container (DP-4); force only skips the guard.
	for _, force := range []bool{true, false} {
		t.Run("force="+strconv.FormatBool(force), func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			fx.swarm.acquired = acquiredAs(3)
			if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature"); err != nil {
				t.Fatalf("acquire: %v", err)
			}

			view, err := LeaseRelease(context.Background(), fx.deps, "devpool-3", force)
			if err != nil {
				t.Fatalf("LeaseRelease: %v", err)
			}

			if len(fx.swarm.releases) != 1 || fx.swarm.releases[0] != (fakeRelease{id: "c-abc", lease: testLease, reset: true}) {
				t.Errorf("releases = %+v, want one {c-abc, the lease, reset=true}", fx.swarm.releases)
			}
			if _, err := os.Stat(filepath.Join(fx.stateDir, "leases", "devpool-3.json")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("record still there after release: %v", err)
			}
			if view.Alias != "devpool-3" || view.State != LeaseStateReleased {
				t.Errorf("view = %+v, want devpool-3 released", view)
			}
			out, mErr := json.Marshal(view)
			if mErr != nil {
				t.Fatal(mErr)
			}
			if strings.Contains(string(out), testLease) {
				t.Errorf("view leaks the lease: %s", out)
			}
		})
	}
}

func TestLeaseAcquire_RecordWriteFails_ReleasesContainer(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	fx.swarm.acquired = acquiredAs(3)
	// A regular file where the state dir should be: the record cannot be written.
	blocker := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fx.deps.StateDir = blocker

	_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "")
	if err == nil {
		t.Fatal("want an error when the record cannot be written")
	}
	// With no record the lease would be unusable and, with TTL 0, the container
	// leaked for good: the acquire must be handed straight back.
	if len(fx.swarm.releases) != 1 || fx.swarm.releases[0] != (fakeRelease{id: "c-abc", lease: testLease, reset: false}) {
		t.Errorf("releases = %+v, want the fresh container handed back (no reset: it is clean)", fx.swarm.releases)
	}
	if strings.Contains(err.Error(), testLease) {
		t.Errorf("error leaks the lease: %v", err)
	}
}

func TestLeaseRelease_RefusedOrStale_RecordFate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		releaseErr error
		wantErr    bool
		wantState  string
		wantRecord bool
	}{
		{
			name:       "unknown lease: nothing left to release, record dropped",
			releaseErr: &platform.SwarmError{Status: 409, Code: "containerAction", Reason: "unknown-lease", Message: "the lease does not hold this container"},
			wantState:  LeaseStateLost, wantRecord: false,
		},
		{
			name:       "container gone: record dropped",
			releaseErr: &platform.SwarmError{Status: 404, Code: "containerAction", Reason: "container-not-found", Message: "no such container"},
			wantState:  LeaseStateLost, wantRecord: false,
		},
		{
			name:       "busy container: still leased, record kept",
			releaseErr: &platform.SwarmError{Status: 409, Code: "containerAction", Reason: "container-busy", Retryable: true, Message: "work in progress"},
			wantErr:    true, wantRecord: true,
		},
		{
			name:       "orchestrator unreachable: record kept",
			releaseErr: platform.NewPlatformError(platform.ErrNetworkError, "the orchestrator did not answer", ""),
			wantErr:    true, wantRecord: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			fx.swarm.acquired = acquiredAs(3)
			if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, ""); err != nil {
				t.Fatal(err)
			}
			fx.swarm.releaseErr = tt.releaseErr

			view, err := LeaseRelease(context.Background(), fx.deps, "devpool-3", true)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && view.State != tt.wantState {
				t.Errorf("State = %q, want %q", view.State, tt.wantState)
			}
			_, statErr := os.Stat(filepath.Join(fx.stateDir, "leases", "devpool-3.json"))
			if gotRecord := statErr == nil; gotRecord != tt.wantRecord {
				t.Errorf("record present = %v, want %v", gotRecord, tt.wantRecord)
			}
		})
	}
}

func TestLeaseRelease_BadAlias_Refused(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	for _, alias := range []string{"", "../etc/passwd", "devpool", "devpool-3/../../x", "Devpool-3", "devpool-3.json", "devpool-9"} {
		if _, err := LeaseRelease(context.Background(), fx.deps, alias, true); err == nil {
			t.Errorf("alias %q: want a refusal", alias)
		}
	}
	if len(fx.swarm.releases) != 0 {
		t.Errorf("the orchestrator was asked to release for a bad alias: %+v", fx.swarm.releases)
	}
}

func TestLeaseList_NoPool_CoversEveryPoolAndRecord(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	fx.swarm.acquired = acquiredAs(3)
	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "b"); err != nil {
		t.Fatal(err)
	}
	fx.swarm.containers = []platform.SwarmContainer{reservedContainer("c-abc", 3)}

	got, err := LeaseList(context.Background(), fx.deps, "")
	if err != nil {
		t.Fatalf("LeaseList: %v", err)
	}
	if len(got) != 1 || got[0].Alias != "devpool-3" || got[0].State != LeaseStateHeld || got[0].Branch != "b" {
		t.Errorf("views = %+v, want the one held devpool-3 on branch b (appdev is not a pool and is not asked)", got)
	}
}

// slowAcquireSwarm reserves on the orchestrator side and answers only when
// the test lets it, or gives up when its context ends — like a real request
// cut short after the container was reserved.
type slowAcquireSwarm struct {
	*fakeSwarm
	started chan struct{}
	answer  chan struct{}
}

func (s slowAcquireSwarm) Acquire(ctx context.Context, req platform.SwarmAcquireRequest) (platform.SwarmAcquired, error) {
	close(s.started)
	select {
	case <-ctx.Done():
		return platform.SwarmAcquired{}, fmt.Errorf("request: %w", ctx.Err())
	case <-s.answer:
		return s.fakeSwarm.Acquire(ctx, req)
	}
}

// An acquire whose caller goes away after the orchestrator reserved a
// container must still receive the lease: the container ends up recorded or
// handed back, never reserved with no record (it has no expiry).
func TestLeaseAcquire_CallerGoneMidAcquire_NoLeak(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	fx.swarm.acquired = acquiredAs(3)
	slow := slowAcquireSwarm{fakeSwarm: fx.swarm, started: make(chan struct{}), answer: make(chan struct{})}
	fx.deps.Swarm = func(string, string) SwarmAPI { return slow }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = LeaseAcquire(ctx, fx.deps, testPoolHost, testRepo, "feature")
	}()
	<-slow.started
	cancel()
	time.Sleep(50 * time.Millisecond) // the caller is gone before the orchestrator answers
	close(slow.answer)
	<-done

	_, statErr := os.Stat(filepath.Join(fx.stateDir, "leases", "devpool-3.json"))
	recorded := statErr == nil
	handedBack := len(fx.swarm.releases) == 1 && fx.swarm.releases[0].id == "c-abc"
	if !recorded && !handedBack {
		t.Errorf("the reserved container is neither recorded nor handed back (record: %v, releases: %+v)", statErr, fx.swarm.releases)
	}
}

// An acquire the orchestrator did not answer in time may still have reserved
// a container: the refusal says so and how status shows it, and does not send
// the agent off to run zcp inside the project it already runs in.
func TestLeaseAcquire_Timeout_SaysAContainerMayBeReserved(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	pe := platform.NewPlatformError(platform.ErrNetworkError, "the orchestrator at http://orch.devpool.zerops did not answer: context deadline exceeded", "")
	pe.Cause = context.DeadlineExceeded
	fx.swarm.acquireErr = pe

	_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")
	var got *platform.PlatformError
	if !errors.As(err, &got) {
		t.Fatalf("err = %v, want a platform error", err)
	}
	if !strings.Contains(got.Message, "may have reserved") || !strings.Contains(got.Suggestion, "unknown-holder") {
		t.Errorf("refusal %q / %q should say a container may be reserved and how status shows it", got.Message, got.Suggestion)
	}
	if strings.Contains(got.Suggestion, "answers only inside the project") {
		t.Errorf("a timeout is not an outside-the-project error: %q", got.Suggestion)
	}
}
