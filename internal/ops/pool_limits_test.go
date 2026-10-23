// Tests for: ops/pool_limits.go — what zcp says and does at a pool's limits:
// a full pool, a pool that is not running, one pool of several that cannot
// be read.
package ops

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// recordLease writes zcp's record of a lease on testPoolHost's container
// c-<number>, as a successful acquire would.
func recordLease(t *testing.T, stateDir, pool, poolServiceID string, number int64, branch string) {
	t.Helper()
	rec := leaseRecord{
		PoolHostname: pool, PoolServiceID: poolServiceID,
		ContainerID: "c-" + strconv.FormatInt(number, 10), ContainerNumber: number,
		ContainerHostname: "node-id-" + strconv.FormatInt(number, 10) + ".runtime." + pool + ".zerops",
		Lease:             testLease, Branch: branch, AcquiredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeLeaseRecord(stateDir, rec); err != nil {
		t.Fatalf("write record %s: %v", rec.alias(), err)
	}
}

// reservedRange is containers c-1..c-n of testPoolHost, all reserved.
func reservedRange(n int64) []platform.SwarmContainer {
	out := make([]platform.SwarmContainer, 0, n)
	for i := int64(1); i <= n; i++ {
		out = append(out, reservedContainer("c-"+strconv.FormatInt(i, 10), i))
	}
	return out
}

// allReserved is the orchestrator's refusal of an acquire on a full pool
// (live body, 2026-10-23), with the platform's cause it passed on.
func allReserved(cause string) *platform.SwarmError {
	return &platform.SwarmError{
		Status: 400, Code: "containerAction", Reason: platform.SwarmReasonAllReserved, Retryable: true,
		Message: "all 10 container(s) of the pool that can serve the request are taken and another could not be created - retry once one is released",
		Cause:   cause,
	}
}

const causeAtMaximum = "[containerAction] Service is already at maximum number of containers"

// TestLeaseAcquire_FullPool_SaysHowFullAndWhatFreesOne: an acquire on a
// pool with no free container is refused at once and nothing is queued. The
// refusal reads the pool's limits and containers and says how full it is,
// which leases are zcp's own, and what frees a container — never that the
// same call may succeed in a moment.
func TestLeaseAcquire_FullPool_SaysHowFullAndWhatFreesOne(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		max         int
		containers  []platform.SwarmContainer
		yours       []int64
		cause       string
		limitsErr   error
		wantMessage []string
		wantSuggest []string
		banned      []string
	}{
		{
			name: "below the per-pool ceiling: release one of yours or raise the maximum",
			max:  4, containers: reservedRange(4), yours: []int64{1, 2}, cause: causeAtMaximum,
			wantMessage: []string{"Pool 'devpool' is full", "4 of at most 4", "yours: devpool-1, devpool-2", "2 held by other holders", "does not queue"},
			wantSuggest: []string{"zerops_lease action=release lease=devpool-1", `zerops_scale serviceHostname="devpool" maxContainers=`, "at most 10", "capped"},
			banned:      []string{"may succeed in a moment", "will not help"},
		},
		{
			name: "at the per-pool ceiling: raising the maximum will not help",
			max:  10, containers: reservedRange(10), yours: []int64{7}, cause: causeAtMaximum,
			wantMessage: []string{"is full", "10 of at most 10", "yours: devpool-7", "9 held by other holders"},
			wantSuggest: []string{"zerops_lease action=release lease=devpool-7", "Raising maxContainers will not help", "the most a pool holds"},
			banned:      []string{"may succeed in a moment", "zerops_scale"},
		},
		{
			name: "none of the leases is yours",
			max:  4, containers: reservedRange(4), cause: causeAtMaximum,
			wantMessage: []string{"is full", "4 of at most 4", "none of them yours"},
			wantSuggest: []string{"only their holders can release them", "zerops_scale"},
			banned:      []string{"may succeed in a moment", "lease=devpool-"},
		},
		{
			name: "another platform cause: quoted, and raising the maximum will not help",
			max:  4, containers: reservedRange(3), yours: []int64{2}, cause: "[containerAction] There is not enough free capacity on the hardware",
			wantMessage: []string{"There is not enough free capacity on the hardware", "3 of at most 4"},
			wantSuggest: []string{"Raising maxContainers will not help", "zerops_lease action=release lease=devpool-2"},
			banned:      []string{"may succeed in a moment", "zerops_scale"},
		},
		{
			name:       "the limits do not answer: the count without a maximum",
			containers: reservedRange(4), yours: []int64{3}, cause: causeAtMaximum, limitsErr: errors.New("limits: i/o timeout"),
			wantMessage: []string{"is full", "4 containers are leased", "yours: devpool-3"},
			wantSuggest: []string{"zerops_lease action=release lease=devpool-3", "if it is below 10"},
			banned:      []string{"may succeed in a moment"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			for _, n := range tt.yours {
				recordLease(t, fx.stateDir, testPoolHost, "svc-pool", n, "b"+strconv.FormatInt(n, 10))
			}
			fx.swarm.containers = tt.containers
			fx.swarm.limits = platform.SwarmPool{MaximumContainerCount: tt.max}
			fx.swarm.limitsErr = tt.limitsErr
			fx.swarm.acquireErr = allReserved(tt.cause)

			_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want a *PlatformError", err)
			}
			if pe.Code != platform.ErrAPIError || pe.APICode != platform.SwarmReasonAllReserved {
				t.Errorf("code = %s / apiCode %q, want %s / %s", pe.Code, pe.APICode, platform.ErrAPIError, platform.SwarmReasonAllReserved)
			}
			for _, want := range tt.wantMessage {
				if !strings.Contains(pe.Message, want) {
					t.Errorf("message %q lacks %q", pe.Message, want)
				}
			}
			for _, want := range tt.wantSuggest {
				if !strings.Contains(pe.Suggestion, want) {
					t.Errorf("suggestion %q lacks %q", pe.Suggestion, want)
				}
			}
			for _, banned := range tt.banned {
				if strings.Contains(pe.Message+" "+pe.Suggestion, banned) {
					t.Errorf("refusal %q / %q carries %q", pe.Message, pe.Suggestion, banned)
				}
			}
			if len(fx.swarm.acquireReqs) != 1 {
				t.Errorf("acquire asked %d times, want once (the orchestrator queues nothing, zcp retries nothing)", len(fx.swarm.acquireReqs))
			}
			records, rErr := readLeaseRecords(fx.stateDir)
			if rErr != nil || len(records) != len(tt.yours) {
				t.Errorf("records = %d (%v), want the %d there were: a refused acquire records nothing", len(records), rErr, len(tt.yours))
			}
		})
	}
}

// poolStatusFixture is a lease fixture whose pool reads searchStatus in the
// service search and directStatus in the by-id read (the search lags writes).
func poolStatusFixture(t *testing.T, searchStatus, directStatus string) *leaseFixture {
	t.Helper()
	fx := newLeaseFixture(t)
	pool := platform.ServiceStack{
		ID: "svc-pool", Name: testPoolHost, Status: searchStatus,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"},
	}
	direct := pool
	direct.Status = directStatus
	fx.deps.Client = platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "proj"}).
		WithServices([]platform.ServiceStack{pool}).
		WithService(&direct).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: testPoolToken, Sensitive: true}})
	return fx
}

// noRouteToHost is what an orchestrator of a stopped pool answers with
// (live, 2026-10-23: the address resolves, nothing listens).
func noRouteToHost() error {
	return platform.NewPlatformError(platform.ErrNetworkError,
		"the orchestrator at http://orch.devpool.zerops did not answer: dial tcp 10.1.92.5:80: connect: no route to host", "")
}

// TestLeaseOps_PoolNotRunning_SaysStartIt: when the orchestrator does not
// answer and the pool service is not running, the refusal says so and how to
// start it — what a stop keeps and what it loses — instead of sending the
// agent to the project VPN. The status is read by id: the search lags writes.
func TestLeaseOps_PoolNotRunning_SaysStartIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                       string
		searchStatus, directStatus string
		wantCode                   string
		want, banned               []string
	}{
		{
			name: "stopped", searchStatus: "STOPPED", directStatus: "STOPPED",
			wantCode: platform.ErrPrerequisiteMissing,
			want:     []string{"the pool is stopped (status STOPPED)", `zerops_manage action=start serviceHostname="devpool"`, "/var/www", "lease", "/tmp", "processes"},
			banned:   []string{"only inside the project", "VPN"},
		},
		{
			name: "stopped since the search was indexed", searchStatus: "ACTIVE", directStatus: "STOPPED",
			wantCode: platform.ErrPrerequisiteMissing,
			want:     []string{"the pool is stopped (status STOPPED)", "zerops_manage action=start"},
			banned:   []string{"only inside the project"},
		},
		{
			name: "starting", searchStatus: "STARTING", directStatus: "STARTING",
			wantCode: platform.ErrPrerequisiteMissing,
			want:     []string{"not running yet (status STARTING)", "ACTIVE"},
			banned:   []string{"only inside the project", "zerops_manage action=start"},
		},
		{
			name: "running: the orchestrator answers only inside the project", searchStatus: "ACTIVE", directStatus: "ACTIVE",
			wantCode: platform.ErrNetworkError,
			want:     []string{"only inside the project", "orch.devpool.zerops"},
			banned:   []string{"zerops_manage action=start", "stopped"},
		},
	}
	ops := []struct {
		name string
		run  func(t *testing.T, fx *leaseFixture) error
	}{
		{"acquire", func(_ *testing.T, fx *leaseFixture) error {
			fx.swarm.acquireErr = noRouteToHost()
			_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")
			return err
		}},
		{"status of the pool", func(_ *testing.T, fx *leaseFixture) error {
			fx.swarm.listErr = noRouteToHost()
			_, err := LeaseList(context.Background(), fx.deps, testPoolHost)
			return err
		}},
		{"release", func(t *testing.T, fx *leaseFixture) error {
			t.Helper()
			recordLease(t, fx.stateDir, testPoolHost, "svc-pool", 3, "b")
			fx.swarm.releaseErr = noRouteToHost()
			_, err := LeaseRelease(context.Background(), fx.deps, "devpool-3", true)
			return err
		}},
		{"rollout", func(_ *testing.T, fx *leaseFixture) error {
			fx.swarm.rolloutErr = noRouteToHost()
			_, err := LeaseRollout(context.Background(), fx.deps, testPoolHost)
			return err
		}},
	}
	for _, tt := range tests {
		for _, op := range ops {
			t.Run(tt.name+"/"+op.name, func(t *testing.T) {
				t.Parallel()
				fx := poolStatusFixture(t, tt.searchStatus, tt.directStatus)
				err := op.run(t, fx)
				var pe *platform.PlatformError
				if !errors.As(err, &pe) {
					t.Fatalf("err = %v, want a *PlatformError", err)
				}
				if pe.Code != tt.wantCode {
					t.Errorf("code = %s, want %s", pe.Code, tt.wantCode)
				}
				text := pe.Message + " " + pe.Suggestion
				for _, want := range tt.want {
					if !strings.Contains(text, want) {
						t.Errorf("refusal %q lacks %q", text, want)
					}
				}
				for _, banned := range tt.banned {
					if strings.Contains(text, banned) {
						t.Errorf("refusal %q carries %q", text, banned)
					}
				}
				if strings.Contains(text, testPoolToken) {
					t.Errorf("refusal leaks the pool token: %s", text)
				}
			})
		}
	}
}

// multiPoolFixture is a project of three pools: devpool answers, stoppool is
// stopped (its orchestrator unreachable), farpool runs but its orchestrator
// does not answer. zcp holds a lease on devpool and on stoppool.
func multiPoolFixture(t *testing.T) *leaseFixture {
	t.Helper()
	fx := newLeaseFixture(t)
	pool := func(id, name, status string) platform.ServiceStack {
		return platform.ServiceStack{ID: id, Name: name, Status: status,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}}
	}
	token := []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: testPoolToken, Sensitive: true}}
	fx.deps.Client = platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "proj"}).
		WithServices([]platform.ServiceStack{
			pool("svc-pool", testPoolHost, "ACTIVE"), pool("svc-stop", "stoppool", "STOPPED"), pool("svc-far", "farpool", "ACTIVE"),
		}).
		WithServiceEnv("svc-pool", token).WithServiceEnv("svc-stop", token).WithServiceEnv("svc-far", token)
	unreachable := &fakeSwarm{listErr: noRouteToHost()}
	fx.deps.Swarm = func(baseURL, _ string) SwarmAPI {
		if baseURL == orchestratorURL(testPoolHost) {
			return fx.swarm
		}
		return unreachable
	}
	fx.swarm.containers = []platform.SwarmContainer{reservedContainer("c-3", 3)}
	recordLease(t, fx.stateDir, testPoolHost, "svc-pool", 3, "feat-a")
	recordLease(t, fx.stateDir, "stoppool", "svc-stop", 2, "feat-b")
	return fx
}

// TestLeaseList_NoPool_OneStoppedPool_OthersStillVerified: a status of
// every pool never fails over one pool that cannot be read. That pool's
// leases are zcp's own records, marked unverified with why; a pool zcp holds
// no lease on is named as unread; every other pool is reconciled as usual.
func TestLeaseList_NoPool_OneStoppedPool_OthersStillVerified(t *testing.T) {
	t.Parallel()
	fx := multiPoolFixture(t)

	status, err := LeaseStatus(context.Background(), fx.deps, "")
	if err != nil {
		t.Fatalf("LeaseStatus: %v", err)
	}
	byAlias := map[string]LeaseView{}
	for _, v := range status.Leases {
		byAlias[v.Alias] = v
	}
	if len(byAlias) != 2 {
		t.Fatalf("leases = %+v, want devpool-3 and stoppool-2", status.Leases)
	}
	if v := byAlias["devpool-3"]; v.State != LeaseStateHeld || v.Unverified {
		t.Errorf("devpool-3 = %+v, want held and verified (its orchestrator answered)", v)
	}
	stopped := byAlias["stoppool-2"]
	if stopped.State != LeaseStateHeld || !stopped.Unverified || stopped.Branch != "feat-b" {
		t.Errorf("stoppool-2 = %+v, want zcp's record (held, branch feat-b) marked unverified", stopped)
	}
	for _, want := range []string{"stopped", `zerops_manage action=start serviceHostname="stoppool"`} {
		if !strings.Contains(stopped.Note, want) {
			t.Errorf("stoppool-2 note %q lacks %q", stopped.Note, want)
		}
	}
	unread := map[string]string{}
	for _, u := range status.Unread {
		unread[u.Pool] = u.Note
	}
	if len(unread) != 2 || !strings.Contains(unread["stoppool"], "stopped") || !strings.Contains(unread["farpool"], "only inside the project") {
		t.Errorf("unread pools = %v, want stoppool (stopped) and farpool (only inside the project)", unread)
	}

	views, err := LeaseList(context.Background(), fx.deps, "")
	if err != nil {
		t.Fatalf("LeaseList: %v", err)
	}
	if len(views) != 2 {
		t.Errorf("LeaseList = %+v, want the same two leases", views)
	}
}
