// Tests for: ops/pool_limits.go — what zcp says and does at a pool's limits:
// a full pool, a pool that is not running, one pool of several that cannot
// be read.
package ops

import (
	"context"
	"errors"
	"slices"
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

// shrinkPoolClient is a project holding devpool with maxContainers curMax,
// capturing what Scale sends.
func shrinkPoolClient(curMax int32) *scaleCaptureClient {
	return &scaleCaptureClient{Mock: platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "proj"}).
		WithServices([]platform.ServiceStack{{
			ID: "svc-pool", Name: testPoolHost, ProjectID: "p1", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"},
			CustomAutoscaling:    &platform.CustomAutoscaling{HorizontalMinCount: 0, HorizontalMaxCount: curMax},
		}}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: testPoolToken, Sensitive: true}})}
}

// freeContainer is container c-<number> of testPoolHost, not leased.
func freeContainer(number int64) platform.SwarmContainer {
	c := reservedContainer("c-"+strconv.FormatInt(number, 10), number)
	c.LockState = ""
	return c
}

func leasedAt(numbers ...int64) func(int64) bool {
	return func(n int64) bool { return slices.Contains(numbers, n) }
}

// containersAt are containers c-<n> for each number, reserved where leased says.
func containersAt(leased func(int64) bool, numbers ...int64) []platform.SwarmContainer {
	out := make([]platform.SwarmContainer, 0, len(numbers))
	for _, n := range numbers {
		if leased(n) {
			out = append(out, reservedContainer("c-"+strconv.FormatInt(n, 10), n))
			continue
		}
		out = append(out, freeContainer(n))
	}
	return out
}

// TestScale_PoolLowering_ReadsTheOrchestratorFirst: lowering a pool's
// maxContainers below the containers its orchestrator holds deletes the
// highest-numbered ones, leased ones included. Before it mutates, Scale reads
// the orchestrator's containers (check-before-mutate): a lowering over free
// containers only proceeds and names them; one that would delete a leased
// container — or that zcp cannot check — is put to the guard, whose refusal
// stops the scale before anything is sent. A raise reads nothing.
func TestScale_PoolLowering_ReadsTheOrchestratorFirst(t *testing.T) {
	t.Parallel()
	errRefused := errors.New("refused by the guard")

	tests := []struct {
		name           string
		curMax, newMax int32
		containers     []platform.SwarmContainer
		yours          []int64
		listErr        error
		noSwarm        bool
		approveErr     error
		wantLists      int
		wantAsked      bool
		wantLeased     []string // aliases of the leased containers that go, yours marked "*"
		wantUnread     string
		wantSent       bool
		wantNotice     []string
		bannedNotice   []string
	}{
		{
			name: "a raise reads nothing and asks nothing", curMax: 5, newMax: 8,
			containers: containersAt(leasedAt(1), 1, 2, 3, 4, 5),
			wantLists:  0, wantSent: true, bannedNotice: []string{"deletes devpool-"},
		},
		{
			name: "a lowering over free containers proceeds and names them", curMax: 5, newMax: 3,
			containers: containersAt(leasedAt(1, 2), 1, 2, 3, 4, 5),
			wantLists:  1, wantSent: true,
			wantNotice:   []string{"deletes devpool-5 and devpool-4", "free"},
			bannedNotice: []string{"devpool-3"},
		},
		{
			name: "the highest-numbered go first, whatever the listing order", curMax: 5, newMax: 2,
			containers: containersAt(leasedAt(1), 11, 1, 2),
			wantLists:  1, wantSent: true,
			wantNotice:   []string{"deletes devpool-11"},
			bannedNotice: []string{"devpool-2", "devpool-1 "},
		},
		{
			name: "a lowering over your lease asks the guard, and its refusal sends nothing", curMax: 5, newMax: 4,
			containers: containersAt(leasedAt(5), 1, 2, 3, 4, 5), yours: []int64{5}, approveErr: errRefused,
			wantLists: 1, wantAsked: true, wantLeased: []string{"devpool-5*"}, wantSent: false,
		},
		{
			name: "an approved lowering over your lease proceeds and says what goes with it", curMax: 5, newMax: 4,
			containers: containersAt(leasedAt(5), 1, 2, 3, 4, 5), yours: []int64{5},
			wantLists: 1, wantAsked: true, wantLeased: []string{"devpool-5*"}, wantSent: true,
			wantNotice: []string{"deletes devpool-5 (your lease, branch b5)", "/var/www", "lost"},
		},
		{
			name: "another holder's lease is put to the guard too", curMax: 5, newMax: 3,
			containers: containersAt(leasedAt(4), 1, 2, 3, 4, 5), approveErr: errRefused,
			wantLists: 1, wantAsked: true, wantLeased: []string{"devpool-4"}, wantSent: false,
		},
		{
			name: "an orchestrator that does not answer is put to the guard", curMax: 5, newMax: 3,
			listErr: noRouteToHost(), approveErr: errRefused,
			wantLists: 1, wantAsked: true, wantUnread: "only inside the project", wantSent: false,
		},
		{
			name: "no orchestrator client is put to the guard", curMax: 5, newMax: 3, noSwarm: true, approveErr: errRefused,
			wantLists: 0, wantAsked: true, wantUnread: "cannot ask", wantSent: false,
		},
		{
			name: "a lowering to the containers held deletes nothing", curMax: 8, newMax: 5,
			containers: containersAt(leasedAt(1, 2, 3, 4, 5), 1, 2, 3, 4, 5),
			wantLists:  1, wantSent: true, bannedNotice: []string{"deletes devpool-"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := shrinkPoolClient(tt.curMax)
			stateDir := t.TempDir()
			for _, n := range tt.yours {
				recordLease(t, stateDir, testPoolHost, "svc-pool", n, "b"+strconv.FormatInt(n, 10))
			}
			swarm := &fakeSwarm{containers: tt.containers, listErr: tt.listErr}
			deps := LeaseDeps{Client: client, ProjectID: "p1", StateDir: stateDir,
				Swarm: func(string, string) SwarmAPI { return swarm },
				Env:   func(string) (string, bool) { return "", false }}
			if tt.noSwarm {
				deps.Swarm = nil
			}
			var asked *PoolShrink
			guard := WithPoolShrinkGuard(deps, func(s PoolShrink) error {
				asked = &s
				return tt.approveErr
			})

			res, err := Scale(context.Background(), client, "p1", testPoolHost, ScaleParams{MaxContainers: scaleIntPtr(int(tt.newMax))}, guard)
			if tt.approveErr != nil {
				if !errors.Is(err, tt.approveErr) {
					t.Fatalf("err = %v, want the guard's refusal", err)
				}
			} else if err != nil {
				t.Fatalf("Scale: %v", err)
			}
			if swarm.lists != tt.wantLists {
				t.Errorf("container listings = %d, want %d", swarm.lists, tt.wantLists)
			}
			if (asked != nil) != tt.wantAsked {
				t.Fatalf("guard asked = %v, want %v (plan %+v)", asked != nil, tt.wantAsked, asked)
			}
			if asked != nil {
				leased := make([]string, 0, len(asked.LeasedDeletes()))
				for _, c := range asked.LeasedDeletes() {
					a := c.Alias
					if c.Yours {
						a += "*"
					}
					leased = append(leased, a)
				}
				if strings.Join(leased, ",") != strings.Join(tt.wantLeased, ",") {
					t.Errorf("leased containers that go = %v, want %v", leased, tt.wantLeased)
				}
				if tt.wantUnread == "" && asked.Unread != "" || !strings.Contains(asked.Unread, tt.wantUnread) {
					t.Errorf("Unread = %q, want it to contain %q", asked.Unread, tt.wantUnread)
				}
				if asked.Pool != testPoolHost || asked.To != int(tt.newMax) {
					t.Errorf("plan = %+v, want pool %s lowered to %d", asked, testPoolHost, tt.newMax)
				}
			}
			if sent := client.sent != nil; sent != tt.wantSent {
				t.Fatalf("sent to the platform = %v, want %v", sent, tt.wantSent)
			}
			if !tt.wantSent {
				return
			}
			if got := client.sent.HorizontalMaxCount; got == nil || *got != tt.newMax {
				t.Errorf("maxContainers sent = %v, want %d", got, tt.newMax)
			}
			for _, want := range tt.wantNotice {
				if !strings.Contains(res.Notice, want) {
					t.Errorf("notice %q lacks %q", res.Notice, want)
				}
			}
			for _, banned := range tt.bannedNotice {
				if strings.Contains(res.Notice, banned) {
					t.Errorf("notice %q carries %q", res.Notice, banned)
				}
			}
		})
	}
}

// TestLeaseAcquire_FullPool_ListsYourLeasesOldestActivityFirst: a full pool's
// refusal reads zcp's own leases on it and lists them oldest activity first —
// idle ones marked so, then the more recent, then those in use now, then those
// whose activity could not be read — each with what a release would lose. The
// release it names is the oldest lease holding nothing unsaved and not in use,
// after asking the person when this conversation did not take it; it never
// names a forced release, and the refusal changes nothing.
func TestLeaseAcquire_FullPool_ListsYourLeasesOldestActivityFirst(t *testing.T) {
	t.Parallel()
	answers := map[string]string{
		"devpool-1": idleAnswer(before(2), nil, 0, time.Time{}, 0),
		"devpool-2": idleAnswer(before(72), nil, 1, time.Time{}, 0),
		"devpool-3": idleAnswer(before(120), nil, 0, before(120), 1),
		"devpool-6": idleAnswer(before(30), []string{" M a.txt", "?? b.txt"}, 0, time.Time{}, 0),
		"devpool-7": idleAnswer(before(40), nil, 0, before(40), 0),
	}
	fx := newLeaseFixture(t)
	for _, n := range []int64{1, 2, 3, 5, 6, 7} {
		recordLeaseAt(t, fx.stateDir, testPoolHost, "svc-pool", n, "b"+strconv.FormatInt(n, 10), testRepo, before(200))
	}
	fx.swarm.containers = reservedRange(8)
	fx.swarm.limits = platform.SwarmPool{MaximumContainerCount: 8}
	fx.swarm.acquireErr = allReserved(causeAtMaximum)
	fx.deps.Now = func() time.Time { return idleNow }
	fx.deps.SSH = &ctxSSH{fn: func(_ context.Context, host, _ string) ([]byte, error) {
		if a, ok := answers[host]; ok {
			return []byte(a), nil
		}
		return nil, sshFailure("ssh: connect to host "+host+" port 22: Connection refused", exitError(t, 255))
	}}

	_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.APICode != platform.SwarmReasonAllReserved {
		t.Fatalf("err = %v, want the full pool's refusal", err)
	}

	order := []string{
		"devpool-2 — idle, last active 3 d ago, 1 commit(s) on no remote branch",
		"devpool-7 — idle, last active 40 h ago, nothing unsaved",
		"devpool-6 — idle, last active 30 h ago, 2 uncommitted or untracked file(s)",
		"devpool-1 — last active 2 h ago, nothing unsaved",
		"devpool-3 — in use now (a preview connection is open), nothing unsaved",
		"devpool-5 — activity not read",
	}
	at := -1
	for _, want := range order {
		i := strings.Index(pe.Message, want)
		if i < 0 {
			t.Errorf("message %q lacks %q", pe.Message, want)
			continue
		}
		if i < at {
			t.Errorf("message %q lists %q out of order: oldest activity first", pe.Message, want)
		}
		at = i
	}
	for _, want := range []string{"Pool 'devpool' is full", "8 of at most 8", "2 held by other holders"} {
		if !strings.Contains(pe.Message, want) {
			t.Errorf("message %q lacks %q", pe.Message, want)
		}
	}
	for _, want := range []string{"zerops_lease action=release lease=devpool-7", "ask the person"} {
		if !strings.Contains(pe.Suggestion, want) {
			t.Errorf("suggestion %q lacks %q", pe.Suggestion, want)
		}
	}
	if strings.Contains(pe.Message+" "+pe.Suggestion, "force") {
		t.Errorf("refusal %q / %q offers force", pe.Message, pe.Suggestion)
	}
	if len(fx.swarm.acquireReqs) != 1 || len(fx.swarm.releases)+len(fx.swarm.deletes) != 0 {
		t.Errorf("acquires %d, releases %v, deletes %v: want the one acquire and nothing else", len(fx.swarm.acquireReqs), fx.swarm.releases, fx.swarm.deletes)
	}
}
