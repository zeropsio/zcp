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
	// limits is what GET /pool answers (a maximum of 4 when unset);
	// limitsErr fails it.
	limits    platform.SwarmPool
	limitsErr error
	lists     int // container listings asked for

	acquireReqs []platform.SwarmAcquireRequest
	releases    []fakeRelease

	rollout    platform.SwarmRollout
	rolloutErr error
	rollouts   int
}

type fakeRelease struct {
	id, lease string
	reset     bool
}

func (f *fakeSwarm) Pool(context.Context) (platform.SwarmPool, error) {
	if f.limitsErr != nil {
		return platform.SwarmPool{}, f.limitsErr
	}
	if f.limits.MaximumContainerCount == 0 {
		return platform.SwarmPool{MaximumContainerCount: 4}, nil
	}
	return f.limits, nil
}

func (f *fakeSwarm) Containers(context.Context) ([]platform.SwarmContainer, error) {
	f.lists++
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

func (f *fakeSwarm) Rollout(context.Context) (platform.SwarmRollout, error) {
	f.rollouts++
	return f.rollout, f.rolloutErr
}

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
		// Hermetic: the machine's own env never supplies a pool token.
		Env: func(string) (string, bool) { return "", false },
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

// maskedPoolFixture is a lease fixture whose API token sees the pool's
// API_TOKEN masked — what a Mate's own token gets (live, 2026-10-23).
func maskedPoolFixture(t *testing.T) *leaseFixture {
	t.Helper()
	fx := newLeaseFixture(t)
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "proj"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: testPoolHost, Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Sensitive: true, Masked: true}})
	fx.deps.Client = mock
	fx.swarm.acquired = acquiredAs(3)
	return fx
}

// A Mate's token sees the pool's API_TOKEN masked, but the container's live
// env carries it as <pool>_API_TOKEN (envIsolation none): the orchestrator is
// reached with that.
func TestLeaseAcquire_MaskedToken_UsesTheContainersLiveEnv(t *testing.T) {
	t.Parallel()
	fx := maskedPoolFixture(t)
	fx.deps.Env = func(key string) (string, bool) {
		if key == testPoolHost+"_API_TOKEN" {
			return "live-env-token", true
		}
		return "", false
	}
	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if fx.gotToken != "live-env-token" {
		t.Errorf("orchestrator bearer = %q, want the live env's %s_API_TOKEN", fx.gotToken, testPoolHost)
	}
}

// With neither source the refusal says what to add, never just "masked".
func TestLeaseAcquire_MaskedTokenNoLiveEnv_SaysWhatToAdd(t *testing.T) {
	t.Parallel()
	fx := maskedPoolFixture(t)
	_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a platform error", err)
	}
	for _, want := range []string{testPoolHost + "_API_TOKEN", "${" + testPoolHost + "_API_TOKEN}"} {
		if !strings.Contains(pe.Message+" "+pe.Suggestion, want) {
			t.Errorf("refusal %q / %q lacks %q", pe.Message, pe.Suggestion, want)
		}
	}
	if len(fx.swarm.acquireReqs) != 0 {
		t.Error("acquired without a token")
	}
}

// TestLeaseAcquire_PoolBearer_LiveEnvSources: the orchestrator's bearer
// comes from the container's live env as <pool>_API_TOKEN, else as the
// different-name <pool>_LEASE_TOKEN (a service variable
// `<pool>_LEASE_TOKEN=${<pool>_API_TOKEN}` resolves under every isolation),
// else from the env API. A value that is still the unresolved text of a
// reference (`${...}`) is never a bearer (live, 2026-10-23: a variable named
// like the reference it holds stays literal, and turns every other reference
// to that name on the service literal too).
func TestLeaseAcquire_PoolBearer_LiveEnvSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		live      map[string]string
		masked    bool // the env API reads the pool's API_TOKEN masked
		wantToken string
	}{
		{name: "the API token name", live: map[string]string{"devpool_API_TOKEN": "live-api"}, masked: true, wantToken: "live-api"},
		{name: "the different-name lease token", live: map[string]string{"devpool_LEASE_TOKEN": "live-lease"}, masked: true, wantToken: "live-lease"},
		{name: "an unresolved API token name falls through to the lease token",
			live: map[string]string{"devpool_API_TOKEN": "${devpool_API_TOKEN}", "devpool_LEASE_TOKEN": "live-lease"}, masked: true, wantToken: "live-lease"},
		{name: "unresolved live values fall through to the env API",
			live: map[string]string{"devpool_API_TOKEN": "${devpool_API_TOKEN}", "devpool_LEASE_TOKEN": "${devpool_API_TOKEN}"}, wantToken: testPoolToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			if tt.masked {
				fx = maskedPoolFixture(t)
			}
			fx.swarm.acquired = acquiredAs(3)
			fx.deps.Env = func(key string) (string, bool) {
				v, ok := tt.live[key]
				return v, ok
			}
			if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature"); err != nil {
				t.Fatalf("acquire: %v", err)
			}
			if fx.gotToken != tt.wantToken {
				t.Errorf("orchestrator bearer = %q, want %q", fx.gotToken, tt.wantToken)
			}
		})
	}
}

// TestLeaseAcquire_NoUsableBearer_SaysAddADifferentNameReference: with no
// usable bearer the refusal tells the person to add the different-name
// reference and never to name it like the reference it holds; a live value
// that is an unresolved reference is named as such, and the same-name
// variable as the thing to delete.
func TestLeaseAcquire_NoUsableBearer_SaysAddADifferentNameReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		live   map[string]string
		want   []string
		banned []string
	}{
		{
			name: "nothing in the live env",
			want: []string{"devpool_LEASE_TOKEN=${devpool_API_TOKEN}", "Never name it devpool_API_TOKEN"},
		},
		{
			name: "the same-name variable left the API token name unresolved",
			live: map[string]string{"devpool_API_TOKEN": "${devpool_API_TOKEN}"},
			want: []string{"devpool_API_TOKEN is the unresolved text ${devpool_API_TOKEN}", "delete", "devpool_LEASE_TOKEN=${devpool_API_TOKEN}"},
		},
		{
			name: "the lease token's reference did not resolve",
			live: map[string]string{"devpool_LEASE_TOKEN": "${devpool_API_TOKEN}"},
			want: []string{"devpool_LEASE_TOKEN is the unresolved text ${devpool_API_TOKEN}"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := maskedPoolFixture(t)
			fx.deps.Env = func(key string) (string, bool) {
				v, ok := tt.live[key]
				return v, ok
			}
			_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
				t.Fatalf("err = %v, want a %s refusal", err, platform.ErrPrerequisiteMissing)
			}
			text := pe.Message + " " + pe.Suggestion
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Errorf("refusal %q lacks %q", text, want)
				}
			}
			if strings.Contains(text, "devpool_API_TOKEN=${devpool_API_TOKEN}") {
				t.Errorf("refusal still advises the same-name reference, which never resolves: %q", text)
			}
			if fx.gotToken != "" || len(fx.swarm.acquireReqs) != 0 {
				t.Errorf("the orchestrator was reached with bearer %q (%d acquires): an unresolved reference is no bearer", fx.gotToken, len(fx.swarm.acquireReqs))
			}
		})
	}
}

// The live env is the container's current one: a file Zerops rewrites as
// services change, read fresh, so a pool created after zcp started is seen.
func TestLiveContainerEnv_ReadsTheFileFresh(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "env.json")
	if err := os.WriteFile(path, []byte(`{"devpool_API_TOKEN":"one"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if v, ok := liveContainerEnvFrom(path, "devpool_API_TOKEN"); !ok || v != "one" {
		t.Errorf("first read = %q %v, want one", v, ok)
	}
	if err := os.WriteFile(path, []byte(`{"devpool_API_TOKEN":"two"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if v, ok := liveContainerEnvFrom(path, "devpool_API_TOKEN"); !ok || v != "two" {
		t.Errorf("second read = %q %v, want two (read fresh)", v, ok)
	}
	if _, ok := liveContainerEnvFrom(filepath.Join(t.TempDir(), "absent.json"), "zcp_test_no_such_key_12345"); ok {
		t.Error("an absent file and an unset process env must read as absent")
	}
}

// TestLiveContainerEnv_FileWinsOverAStaleProcessEnv: where the live env file
// exists it is the container's current env; the process env, a snapshot from
// the process's start, answers only where there is no file (a dev machine).
func TestLiveContainerEnv_FileWinsOverAStaleProcessEnv(t *testing.T) {
	// Not parallel — sets a process env variable.
	t.Setenv("zcppool_API_TOKEN", "stale")
	dir := t.TempDir()
	path := filepath.Join(dir, "env.json")
	if err := os.WriteFile(path, []byte(`{"other_KEY":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if v, ok := liveContainerEnvFrom(path, "zcppool_API_TOKEN"); ok {
		t.Errorf("a key the live file lacks read as %q from the stale process env", v)
	}
	if v, ok := liveContainerEnvFrom(filepath.Join(dir, "absent.json"), "zcppool_API_TOKEN"); !ok || v != "stale" {
		t.Errorf("without a live file = %q %v, want the process env's value", v, ok)
	}
}

// hangingListSwarm is an orchestrator that accepts a container listing and
// never answers it.
type hangingListSwarm struct{ *fakeSwarm }

func (hangingListSwarm) Containers(ctx context.Context) ([]platform.SwarmContainer, error) {
	<-ctx.Done()
	return nil, fmt.Errorf("list: %w", ctx.Err())
}

// A status read gives up on an orchestrator that does not answer within the
// read budget, however long the client allows a start or a reset to take.
func TestLeaseList_HangingOrchestrator_BoundedByTheReadBudget(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	fx.deps.Swarm = func(string, string) SwarmAPI { return hangingListSwarm{fx.swarm} }
	fx.deps.ReadTimeout = 50 * time.Millisecond
	done := make(chan error, 1)
	go func() {
		_, err := LeaseList(context.Background(), fx.deps, testPoolHost)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a listing the orchestrator never answered reported no error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("status still waiting on the orchestrator after 5s")
	}
}

// TestPoolOrchestratorCheck: a pool is never probed by its hostname (PS-3);
// its verify reaches what it serves through the orchestrator — with the
// bearer zcp finds — and counts the containers leased.
func TestPoolOrchestratorCheck(t *testing.T) {
	t.Parallel()

	fx := newLeaseFixture(t)
	fx.swarm.containers = []platform.SwarmContainer{reservedContainer("c-1", 1), {ID: "c-2", Number: 2}}
	got := PoolOrchestratorCheck(context.Background(), fx.deps, testPoolHost)
	if got.Name != "orchestrator" || got.Status != "pass" || !strings.Contains(got.Detail, "2 container") || !strings.Contains(got.Detail, "1 leased") {
		t.Errorf("check = %+v, want orchestrator pass naming 2 containers, 1 leased", got)
	}

	masked := maskedPoolFixture(t)
	got = PoolOrchestratorCheck(context.Background(), masked.deps, testPoolHost)
	if got.Status != "fail" || !strings.Contains(got.Detail, testPoolHost+"_API_TOKEN") {
		t.Errorf("check = %+v, want a failure naming %s_API_TOKEN", got, testPoolHost)
	}
	if strings.Contains(got.Detail, testPoolToken) {
		t.Errorf("the check leaks the pool token: %s", got.Detail)
	}

	fx.swarm.listErr = errors.New("dial tcp: i/o timeout")
	if got = PoolOrchestratorCheck(context.Background(), fx.deps, testPoolHost); got.Status != "fail" {
		t.Errorf("an orchestrator that does not answer reads %+v, want fail", got)
	}
}
