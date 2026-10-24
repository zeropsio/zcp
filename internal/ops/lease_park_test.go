// Tests for: ops/lease_park.go — park and unpark of a lease
// (docs/spec-workflows.md §8 DP-28): park unmounts the lease, then stops its
// container through the orchestrator with the lease; unpark starts it, waits
// for its ssh, mounts it and makes sure its supervisor runs; a release of a
// parked lease starts it for its guard and stops it again when refused.
package ops

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
)

// parkNow is zcp's clock in the park tests.
var parkNow = time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC)

// powerSwarm is orderedSwarm that also records a holder's stop and start.
type powerSwarm struct{ orderedSwarm }

func (p powerSwarm) Stop(ctx context.Context, id, lease string) (platform.SwarmContainer, error) {
	*p.events = append(*p.events, "orchestrator-stop")
	return p.fakeSwarm.Stop(ctx, id, lease)
}

func (p powerSwarm) Start(ctx context.Context, id, lease string) (platform.SwarmContainer, error) {
	*p.events = append(*p.events, "orchestrator-start")
	return p.fakeSwarm.Start(ctx, id, lease)
}

// parkSSH is the leased container over ssh: the readiness probe (`true`),
// failing its first unready answers; the release guard's two reads,
// answering porcelain and unpushed; everything else — the declaration read,
// the supervisor's ensure — the fake dev container answers. It records each
// kind of command it answered in events.
type parkSSH struct {
	mu        sync.Mutex
	c         *devservertest.Container
	events    *[]string
	unready   int
	porcelain string
	unpushed  string
	hosts     []string
}

func (s *parkSSH) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	s.mu.Lock()
	s.hosts = append(s.hosts, host)
	switch {
	case command == "true":
		*s.events = append(*s.events, "ssh-ready")
		if s.unready > 0 {
			s.unready--
			s.mu.Unlock()
			return nil, &platform.SSHExecError{Hostname: host, Output: "ssh: connect to host node-id-3.runtime.devpool.zerops port 22: No route to host", Err: errors.New("exit status 255")}
		}
		s.mu.Unlock()
		return nil, nil
	case strings.Contains(command, "status --porcelain"):
		*s.events = append(*s.events, "guard")
		out := s.porcelain
		s.mu.Unlock()
		return []byte(out), nil
	case strings.Contains(command, "rev-list --count"):
		out := s.unpushed
		s.mu.Unlock()
		return []byte(out), nil
	case strings.Contains(command, "@@zcp-devsup check"):
		*s.events = append(*s.events, "supervisor")
	}
	s.mu.Unlock()
	return s.c.ExecSSH(ctx, host, command)
}

func (s *parkSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *parkSSH) sentTo() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.hosts)
}

// parkFixture is a hostFixture holding devpool-3 (container c-3) of testRepo's
// feat-x, its orchestrator recording stops and starts and its container
// answering ssh as parkSSH does.
type parkFixture struct {
	*hostFixture
	ssh *parkSSH
}

// newParkFixture holds devpool-3 parked (its record says so, its container
// is stopped and it is not mounted) or held (running and mounted).
func newParkFixture(t *testing.T, parked bool) *parkFixture {
	t.Helper()
	hf := newHostFixture(t)
	recordLeaseAt(t, hf.stateDir, testPoolHost, "svc-pool", 3, "feat-x", testRepo, parkNow.Add(-48*time.Hour))
	c := reservedContainer("c-3", 3)
	if parked {
		markParked(t, hf.stateDir, "devpool-3", parkNow.Add(-time.Hour))
		c.Status = platform.SwarmContainerStopped
	} else {
		hf.mounter.states["/var/www/devpool-3"] = platform.MountStateActive
	}
	hf.swarm.containers = []platform.SwarmContainer{c}
	swarm := powerSwarm{orderedSwarm{fakeSwarm: hf.swarm, events: &hf.events}}
	hf.deps.Swarm = func(string, string) SwarmAPI { return swarm }
	hf.deps.Now = func() time.Time { return parkNow }
	ssh := &parkSSH{
		c:      &devservertest.Container{Installed: true, SupervisorVersion: "v9.300.0", Formats: []int{1}, Capabilities: slices.Clone(devsup.Capabilities)},
		events: &hf.events, unpushed: "0\n",
	}
	hf.deps.SSH = ssh
	hf.deps.SSHReadyEvery = time.Millisecond
	hf.deps.StartedSSHWait = 200 * time.Millisecond
	return &parkFixture{hostFixture: hf, ssh: ssh}
}

// markParked rewrites alias's record parked since at.
func markParked(t *testing.T, stateDir, alias string, at time.Time) {
	t.Helper()
	rec, err := loadLeaseRecord(stateDir, alias)
	if err != nil {
		t.Fatal(err)
	}
	rec.Parked, rec.ParkedAt = true, at.UTC().Format(time.RFC3339)
	if err := writeLeaseRecord(stateDir, rec); err != nil {
		t.Fatal(err)
	}
}

// poolLeaseView is alias's view in a status of testPoolHost.
func poolLeaseView(t *testing.T, d LeaseDeps, alias string) LeaseView {
	t.Helper()
	status, err := LeaseStatus(context.Background(), d, testPoolHost)
	if err != nil {
		t.Fatalf("LeaseStatus: %v", err)
	}
	for _, v := range status.Leases {
		if v.Alias == alias {
			return v
		}
	}
	t.Fatalf("leases = %+v, want %s", status.Leases, alias)
	return LeaseView{}
}

func (pf *parkFixture) record(t *testing.T) leaseRecord {
	t.Helper()
	rec, err := loadLeaseRecord(pf.stateDir, "devpool-3")
	if err != nil {
		t.Fatalf("devpool-3's record: %v", err)
	}
	return rec
}

// orderOf is each named event's first index in events, -1 when absent.
func orderOf(events []string, names ...string) []int {
	out := make([]int, len(names))
	for i, n := range names {
		out[i] = slices.Index(events, n)
	}
	return out
}

// inOrder reports whether every named event happened, in the given order.
func inOrder(events []string, names ...string) bool {
	at := orderOf(events, names...)
	for i, n := range at {
		if n < 0 || (i > 0 && n < at[i-1]) {
			return false
		}
	}
	return true
}

// TestLeasePark_UnmountsThenStopsWithItsLease: park unmounts the lease first,
// then stops its container through the orchestrator with its own lease, and
// records it parked since zcp's clock; it sends nothing over ssh, releases
// nothing and says how to start it again.
func TestLeasePark_UnmountsThenStopsWithItsLease(t *testing.T) {
	t.Parallel()
	pf := newParkFixture(t, false)

	v, err := LeasePark(context.Background(), pf.deps, "devpool-3")
	if err != nil {
		t.Fatalf("LeasePark: %v", err)
	}
	if !inOrder(pf.events, "unmount", "orchestrator-stop") {
		t.Errorf("events = %v, want the unmount before the stop", pf.events)
	}
	if len(pf.swarm.stops) != 1 || pf.swarm.stops[0] != (fakeRelease{id: "c-3", lease: testLease}) {
		t.Errorf("stops = %+v, want c-3 stopped once with its own lease", pf.swarm.stops)
	}
	if len(pf.swarm.starts) != 0 || len(pf.swarm.releases) != 0 || len(pf.swarm.deletes) != 0 {
		t.Errorf("starts %+v, releases %+v, deletes %+v: a park only stops", pf.swarm.starts, pf.swarm.releases, pf.swarm.deletes)
	}
	if _, mounted := pf.mounter.states["/var/www/devpool-3"]; mounted {
		t.Error("/var/www/devpool-3 is still mounted")
	}
	if hosts := pf.ssh.sentTo(); len(hosts) != 0 {
		t.Errorf("park sent ssh to %v; it needs none", hosts)
	}
	rec := pf.record(t)
	if !rec.Parked || rec.ParkedAt != parkNow.Format(time.RFC3339) {
		t.Errorf("record parked %v since %q, want parked since %s", rec.Parked, rec.ParkedAt, parkNow.Format(time.RFC3339))
	}
	if v.State != LeaseStateParked || v.ParkedAt != parkNow.Format(time.RFC3339) {
		t.Errorf("view = %+v, want state parked since %s", v, parkNow.Format(time.RFC3339))
	}
	for _, want := range []string{"zerops_lease action=unpark lease=devpool-3", "/var/www and $HOME", "an Ubuntu base empties /tmp", "unmounted"} {
		if !strings.Contains(v.Note, want) {
			t.Errorf("note %q lacks %q", v.Note, want)
		}
	}
	// /tmp survives a stop only where the base keeps it: an Ubuntu base
	// empties it at every start (live-verified 2026-10-24).
	if strings.Contains(v.Note, "$HOME and /tmp") {
		t.Errorf("note %q promises /tmp unconditionally", v.Note)
	}
	if strings.Contains(v.Note, testLease) {
		t.Errorf("note leaks the lease: %q", v.Note)
	}
}

// TestLeasePark_UnmountFails_NothingStopped: a mount that will not unmount
// refuses the park before the container is stopped — a stopped container's
// mount hangs whatever reads it — naming how to unmount it.
func TestLeasePark_UnmountFails_NothingStopped(t *testing.T) {
	t.Parallel()
	pf := newParkFixture(t, false)
	pf.mounter.umountErr = errors.New("target is busy")

	_, err := LeasePark(context.Background(), pf.deps, "devpool-3")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrUnmountFailed {
		t.Fatalf("err = %v, want an UNMOUNT_FAILED refusal", err)
	}
	if !strings.Contains(pe.Message, "was not parked") || !strings.Contains(pe.Message, "target is busy") {
		t.Errorf("message %q should say the lease was not parked and why", pe.Message)
	}
	if !strings.Contains(pe.Suggestion, "fusermount -uz /var/www/devpool-3") || !strings.Contains(pe.Suggestion, "zerops_lease action=park lease=devpool-3") {
		t.Errorf("suggestion %q should name the unmount and the park again", pe.Suggestion)
	}
	if len(pf.swarm.stops) != 0 {
		t.Errorf("stops = %+v, want none", pf.swarm.stops)
	}
	if pf.record(t).Parked {
		t.Error("the record says parked after a refused park")
	}
}

// TestLeasePark_StopRefused_StaysHeldAndMounted: a stop the orchestrator
// refuses leaves the lease held, running and mounted again, and the refusal
// says so; the record never says parked.
func TestLeasePark_StopRefused_StaysHeldAndMounted(t *testing.T) {
	t.Parallel()
	pf := newParkFixture(t, false)
	pf.swarm.stopErr = &platform.SwarmError{Status: 400, Code: "containerAction", Message: "the container has work in progress", Reason: "container-busy"}

	_, err := LeasePark(context.Background(), pf.deps, "devpool-3")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a *PlatformError", err)
	}
	for _, want := range []string{"work in progress", "stays held and running", "mount is back"} {
		if !strings.Contains(pe.Message, want) {
			t.Errorf("message %q lacks %q", pe.Message, want)
		}
	}
	if pf.mounter.states["/var/www/devpool-3"] != platform.MountStateActive {
		t.Error("the mount was not put back after the refused stop")
	}
	if pf.record(t).Parked {
		t.Error("the record says parked after a refused stop")
	}
}

// TestLeasePark_LeaseNotHeld_Refused: a lease the orchestrator no longer
// holds, or whose container it is stopping or starting right now, is not
// parked: nothing is unmounted or stopped, and the refusal says why.
func TestLeasePark_LeaseNotHeld_Refused(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		containers func(c platform.SwarmContainer) []platform.SwarmContainer
		want       string
	}{
		{"released elsewhere", func(c platform.SwarmContainer) []platform.SwarmContainer {
			c.LockState = ""
			return []platform.SwarmContainer{c}
		},
			"no longer reserves"},
		{"gone from the pool", func(platform.SwarmContainer) []platform.SwarmContainer { return nil }, "gone from the pool"},
		{"being stopped", func(c platform.SwarmContainer) []platform.SwarmContainer {
			c.LockState = "stopping"
			return []platform.SwarmContainer{c}
		},
			"stopping its container right now"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pf := newParkFixture(t, false)
			pf.swarm.containers = tt.containers(pf.swarm.containers[0])

			_, err := LeasePark(context.Background(), pf.deps, "devpool-3")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want a refusal saying %q", err, tt.want)
			}
			if len(pf.swarm.stops) != 0 || pf.mounter.states["/var/www/devpool-3"] != platform.MountStateActive {
				t.Errorf("stops %+v, mounts %v: a refused park changes nothing", pf.swarm.stops, pf.mounter.states)
			}
		})
	}
}

// TestLeasePark_AlreadyStopped_RecordsWithoutAStop: a container the
// orchestrator lists stopped and reserved — stopped outside zcp — is not
// stopped again; the lease is recorded parked.
func TestLeasePark_AlreadyStopped_RecordsWithoutAStop(t *testing.T) {
	t.Parallel()
	pf := newParkFixture(t, false)
	pf.swarm.containers[0].Status = platform.SwarmContainerStopped

	v, err := LeasePark(context.Background(), pf.deps, "devpool-3")
	if err != nil {
		t.Fatalf("LeasePark: %v", err)
	}
	if len(pf.swarm.stops) != 0 {
		t.Errorf("stops = %+v, want none for a stopped container", pf.swarm.stops)
	}
	if !pf.record(t).Parked || v.State != LeaseStateParked || !strings.Contains(v.Note, "stopped already") {
		t.Errorf("view = %+v, record parked %v: want it recorded parked, saying it was stopped already", v, pf.record(t).Parked)
	}
}

// TestLeaseUnpark_StartsWaitsForSSHMountsAndSupervises: unpark starts the
// container with its lease, records it running, waits for its ssh, mounts it,
// then makes sure its supervisor runs — started only in a reset container,
// whose run.start does not run again; a container never reset ran its
// run.start at the start — and hands it the branch's sibling overlay from
// zcp's records either way.
func TestLeaseUnpark_StartsWaitsForSSHMountsAndSupervises(t *testing.T) {
	t.Parallel()
	const decl = `{"version":1,"siblingEnv":{"API_HOST":"webpool"},"servers":[{"name":"web","port":3000,"command":"pnpm dev"}]}`
	for _, tt := range []struct {
		name       string
		runStart   bool
		wantStarts int
	}{
		{name: "a reset container: zcp starts its supervisor", wantStarts: 1},
		{name: "a container never reset: run.start started it", runStart: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pf := newParkFixture(t, true)
			// The branch's lease on webpool, whose container the overlay names.
			recordLeaseAt(t, pf.stateDir, "webpool", "svc-web", 5, "feat-x", testRepo, parkNow.Add(-47*time.Hour))
			pf.ssh.c.Declaration = decl
			if tt.runStart {
				pf.ssh.c.Running = true
				pf.ssh.c.Env = map[string]string{"API_HOST": "node-id-1.runtime.webpool.zerops"} // kept from before the park
			}

			v, err := LeaseUnpark(context.Background(), pf.deps, "devpool-3")
			if err != nil {
				t.Fatalf("LeaseUnpark: %v", err)
			}
			if !inOrder(pf.events, "orchestrator-start", "ssh-ready", "mount", "supervisor") {
				t.Errorf("events = %v, want start, then ssh, then the mount, then the supervisor", pf.events)
			}
			if len(pf.swarm.starts) != 1 || pf.swarm.starts[0] != (fakeRelease{id: "c-3", lease: testLease}) {
				t.Errorf("starts = %+v, want c-3 started once with its own lease", pf.swarm.starts)
			}
			if pf.ssh.c.SupervisorStarts != tt.wantStarts {
				t.Errorf("supervisors started = %d, want %d", pf.ssh.c.SupervisorStarts, tt.wantStarts)
			}
			if got := pf.ssh.c.Overlay()["API_HOST"]; got != "node-id-5.runtime.webpool.zerops" {
				t.Errorf("overlay API_HOST = %q, want the branch's webpool lease node-id-5.runtime.webpool.zerops", got)
			}
			if pf.mounter.states["/var/www/devpool-3"] != platform.MountStateActive {
				t.Error("/var/www/devpool-3 is not mounted")
			}
			if rec := pf.record(t); rec.Parked || rec.ParkedAt != "" {
				t.Errorf("record parked %v since %q, want it running", rec.Parked, rec.ParkedAt)
			}
			if v.State != LeaseStateHeld || v.ParkedAt != "" || v.Note != "" {
				t.Errorf("view = %+v, want a plain held lease", v)
			}
		})
	}
}

// TestLeaseUnpark_SSHAnswersLate_WaitsForIt: a started container whose ssh
// does not answer at once is tried again until it does.
func TestLeaseUnpark_SSHAnswersLate_WaitsForIt(t *testing.T) {
	t.Parallel()
	pf := newParkFixture(t, true)
	pf.ssh.unready = 2

	v, err := LeaseUnpark(context.Background(), pf.deps, "devpool-3")
	if err != nil {
		t.Fatalf("LeaseUnpark: %v", err)
	}
	ready := 0
	for _, e := range pf.events {
		if e == "ssh-ready" {
			ready++
		}
	}
	if ready != 3 || v.State != LeaseStateHeld {
		t.Errorf("ssh tried %d times, view %+v: want three tries and a held lease", ready, v)
	}
}

// TestLeaseUnpark_SSHNeverAnswers_RunsUnmountedWithNote: once the container
// runs, an ssh that never answers fails nothing: the lease is recorded
// running and reported unmounted, its note naming the unpark that finishes.
func TestLeaseUnpark_SSHNeverAnswers_RunsUnmountedWithNote(t *testing.T) {
	t.Parallel()
	pf := newParkFixture(t, true)
	pf.ssh.unready = 1 << 30

	v, err := LeaseUnpark(context.Background(), pf.deps, "devpool-3")
	if err != nil {
		t.Fatalf("LeaseUnpark: %v", err)
	}
	if v.State != LeaseStateUnmounted || !strings.Contains(v.Note, "did not answer ssh") || !strings.Contains(v.Note, "zerops_lease action=unpark lease=devpool-3") {
		t.Errorf("view = %+v, want unmounted with the ssh wait and the unpark again in its note", v)
	}
	if slices.Contains(pf.events, "mount") || slices.Contains(pf.events, "supervisor") {
		t.Errorf("events = %v: nothing is mounted or supervised before ssh answers", pf.events)
	}
	if pf.record(t).Parked {
		t.Error("the record still says parked though the container runs")
	}
}

// TestLeaseUnpark_StartRefused_StaysParked: a start the orchestrator refuses
// fails the unpark; the lease stays parked and unmounted.
func TestLeaseUnpark_StartRefused_StaysParked(t *testing.T) {
	t.Parallel()
	pf := newParkFixture(t, true)
	pf.swarm.startErr = &platform.SwarmError{Status: 400, Code: "containerAction", Message: "the platform could not start the container"}

	_, err := LeaseUnpark(context.Background(), pf.deps, "devpool-3")
	if err == nil || !strings.Contains(err.Error(), "could not start the container") {
		t.Fatalf("err = %v, want the orchestrator's refusal", err)
	}
	if !pf.record(t).Parked || slices.Contains(pf.events, "mount") {
		t.Errorf("record parked %v, events %v: want it still parked and unmounted", pf.record(t).Parked, pf.events)
	}
}

// TestLeaseUnpark_RunningLease_StartsNothingRestoresTheRest: unpark of a lease
// whose container runs starts nothing and still mounts it and makes sure its
// supervisor runs.
func TestLeaseUnpark_RunningLease_StartsNothingRestoresTheRest(t *testing.T) {
	t.Parallel()
	pf := newParkFixture(t, false)
	delete(pf.mounter.states, "/var/www/devpool-3")

	v, err := LeaseUnpark(context.Background(), pf.deps, "devpool-3")
	if err != nil {
		t.Fatalf("LeaseUnpark: %v", err)
	}
	if len(pf.swarm.starts) != 0 {
		t.Errorf("starts = %+v, want none for a running container", pf.swarm.starts)
	}
	if pf.mounter.states["/var/www/devpool-3"] != platform.MountStateActive || pf.ssh.c.SupervisorStarts != 1 {
		t.Errorf("mounts %v, supervisors started %d: want it mounted and supervised", pf.mounter.states, pf.ssh.c.SupervisorStarts)
	}
	if v.State != LeaseStateHeld || !strings.Contains(v.Note, "running already") {
		t.Errorf("view = %+v, want held, saying it was running already", v)
	}
}

// TestLeaseStatus_StoppedReservedIsParked: status reports a recorded lease
// whose container its orchestrator lists stopped (or being stopped or
// started) and reserved as parked — before any check of its mount — with
// parkedAt when zcp parked it and the unpark that starts it; one zcp parked
// whose container runs again is held, and says so. Status starts or stops
// nothing.
func TestLeaseStatus_StoppedReservedIsParked(t *testing.T) {
	t.Parallel()
	parkedAt := parkNow.Add(-time.Hour).Format(time.RFC3339)
	for _, tt := range []struct {
		name         string
		parked       bool
		status, lock string
		wantState    string
		wantParkedAt string
		want         []string
		banned       []string
	}{
		{name: "parked by zcp", parked: true, status: "STOPPED", lock: "reserved", wantState: LeaseStateParked, wantParkedAt: parkedAt,
			want: []string{"parked", "zerops_lease action=unpark lease=devpool-3"}, banned: []string{"zcp init restores"}},
		{name: "stopped outside zcp", status: "STOPPED", lock: "reserved", wantState: LeaseStateParked,
			want: []string{"did not park it", "zerops_lease action=unpark lease=devpool-3"}},
		{name: "being stopped", parked: true, status: "ACTIVE", lock: "stopping", wantState: LeaseStateParked, wantParkedAt: parkedAt,
			want: []string{"stopping its container right now"}},
		{name: "parked, then started outside zcp", parked: true, status: "ACTIVE", lock: "reserved", wantState: LeaseStateUnmounted,
			want: []string{"runs again (started outside zcp)", "zerops_lease action=unpark lease=devpool-3"}},
		{name: "parked, then released elsewhere", parked: true, status: "STOPPED", lock: "", wantState: LeaseStateLost},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pf := newParkFixture(t, tt.parked)
			delete(pf.mounter.states, "/var/www/devpool-3")
			pf.swarm.containers[0].Status, pf.swarm.containers[0].LockState = tt.status, tt.lock
			pf.deps.SSH = nil // the checkout read is DP-21's; this is the reconcile

			v := poolLeaseView(t, pf.deps, "devpool-3")

			if v.State != tt.wantState || v.ParkedAt != tt.wantParkedAt {
				t.Errorf("view = %+v, want state %s parked at %q", v, tt.wantState, tt.wantParkedAt)
			}
			for _, want := range tt.want {
				if !strings.Contains(v.Note, want) {
					t.Errorf("note %q lacks %q", v.Note, want)
				}
			}
			for _, banned := range tt.banned {
				if strings.Contains(v.Note, banned) {
					t.Errorf("note %q carries %q", v.Note, banned)
				}
			}
			if len(pf.swarm.stops)+len(pf.swarm.starts)+len(pf.swarm.releases) != 0 {
				t.Errorf("status changed the pool: stops %+v starts %+v releases %+v", pf.swarm.stops, pf.swarm.starts, pf.swarm.releases)
			}
		})
	}
}

// TestLeaseStatus_UnreadPool_ParkedRecordStaysParked: when the pool's
// orchestrator cannot be read, a lease zcp's record says is parked is listed
// parked, unverified.
func TestLeaseStatus_UnreadPool_ParkedRecordStaysParked(t *testing.T) {
	t.Parallel()
	pf := newParkFixture(t, true)
	pf.swarm.listErr = errors.New("connection refused")
	pf.deps.SSH = nil

	status, err := LeaseStatus(context.Background(), pf.deps, "")
	if err != nil {
		t.Fatalf("LeaseStatus: %v", err)
	}
	if len(status.Leases) != 1 || status.Leases[0].State != LeaseStateParked || !status.Leases[0].Unverified || status.Leases[0].ParkedAt == "" {
		t.Errorf("leases = %+v, want devpool-3 parked, unverified, with its parkedAt", status.Leases)
	}
}

// TestLeaseRelease_Parked_StartsThenGuards: a release of a parked lease
// starts its container with its lease, waits for its ssh, runs the guard on
// it and, the guard passing, releases it with a reset as any release does.
func TestLeaseRelease_Parked_StartsThenGuards(t *testing.T) {
	t.Parallel()
	pf := newParkFixture(t, true)

	v, err := LeaseRelease(context.Background(), pf.deps, "devpool-3", false)
	if err != nil {
		t.Fatalf("LeaseRelease: %v", err)
	}
	if !inOrder(pf.events, "orchestrator-start", "ssh-ready", "guard", "orchestrator-release") {
		t.Errorf("events = %v, want the start, ssh, the guard, then the release", pf.events)
	}
	if len(pf.swarm.releases) != 1 || !pf.swarm.releases[0].reset || len(pf.swarm.stops) != 0 {
		t.Errorf("releases %+v, stops %+v: want one reset release and no stop", pf.swarm.releases, pf.swarm.stops)
	}
	if v.State != LeaseStateReleased || !strings.Contains(v.Note, "started its parked container") {
		t.Errorf("view = %+v, want released, saying zcp started it to check it", v)
	}
}

// TestLeaseRelease_Parked_RefusedThenStoppedAgain: a release that started a
// parked lease and is then refused — the guard finds work a release would
// lose, or the container's ssh never answers — stops it again with its lease,
// so the lease stays parked, and the refusal says so. A container zcp cannot
// stop again runs on, and its record says so.
func TestLeaseRelease_Parked_RefusedThenStoppedAgain(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		porcelain  string
		unready    int
		stopErr    error
		wantParked bool
		want       []string
	}{
		{name: "the guard finds unsaved work", porcelain: " M src/App.tsx\n", wantParked: true,
			want: []string{"src/App.tsx", "stays parked"}},
		{name: "its ssh never answers", unready: 1 << 30, wantParked: true,
			want: []string{"did not answer ssh", "stays parked"}},
		{name: "the stop again is refused", porcelain: " M src/App.tsx\n",
			stopErr: &platform.SwarmError{Status: 400, Code: "containerAction", Message: "the container has work in progress"},
			want:    []string{"could not stop its container again", "runs now", "zerops_lease action=park lease=devpool-3"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pf := newParkFixture(t, true)
			pf.ssh.porcelain, pf.ssh.unready = tt.porcelain, tt.unready
			pf.swarm.stopErr = tt.stopErr

			_, err := LeaseRelease(context.Background(), pf.deps, "devpool-3", false)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
				t.Fatalf("err = %v, want a PREREQUISITE_MISSING refusal", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(pe.Message, want) {
					t.Errorf("message %q lacks %q", pe.Message, want)
				}
			}
			if !inOrder(pf.events, "orchestrator-start", "orchestrator-stop") {
				t.Errorf("events = %v, want the start, then the stop again", pf.events)
			}
			if len(pf.swarm.releases) != 0 {
				t.Errorf("releases = %+v, want none", pf.swarm.releases)
			}
			if got := pf.record(t).Parked; got != tt.wantParked {
				t.Errorf("record parked = %v, want %v", got, tt.wantParked)
			}
		})
	}
}

// TestLeaseRelease_Parked_StartRefused_NothingReleased: a parked lease whose
// container the orchestrator will not start is not released and stays
// parked; the refusal names the unpark and the forced release.
func TestLeaseRelease_Parked_StartRefused_NothingReleased(t *testing.T) {
	t.Parallel()
	pf := newParkFixture(t, true)
	pf.swarm.startErr = &platform.SwarmError{Status: 400, Code: "containerAction", Message: "the platform could not start the container"}

	_, err := LeaseRelease(context.Background(), pf.deps, "devpool-3", false)
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a *PlatformError", err)
	}
	if !strings.Contains(pe.Message, "is parked") || !strings.Contains(pe.Message, "could not start") {
		t.Errorf("message %q should say the lease is parked and could not be started", pe.Message)
	}
	if !strings.Contains(pe.Suggestion, "zerops_lease action=unpark lease=devpool-3") || !strings.Contains(pe.Suggestion, "force=true") {
		t.Errorf("suggestion %q should name the unpark and the forced release", pe.Suggestion)
	}
	if slices.Contains(pf.events, "guard") || len(pf.swarm.releases)+len(pf.swarm.stops) != 0 || !pf.record(t).Parked {
		t.Errorf("events %v, releases %+v, stops %+v: want nothing read, released or stopped, and the lease still parked", pf.events, pf.swarm.releases, pf.swarm.stops)
	}
}

// TestLeaseRelease_ParkedForce_ReleasedStoppedWithoutAStart: a forced release
// of a parked lease starts nothing — a reset of a stopped container returns
// it to the pool clean and stopped — and says so.
func TestLeaseRelease_ParkedForce_ReleasedStoppedWithoutAStart(t *testing.T) {
	t.Parallel()
	pf := newParkFixture(t, true)

	v, err := LeaseRelease(context.Background(), pf.deps, "devpool-3", true)
	if err != nil {
		t.Fatalf("LeaseRelease: %v", err)
	}
	if len(pf.swarm.starts) != 0 || len(pf.swarm.releases) != 1 || !pf.swarm.releases[0].reset {
		t.Errorf("starts %+v, releases %+v: want no start and one reset release", pf.swarm.starts, pf.swarm.releases)
	}
	if v.State != LeaseStateReleased || !strings.Contains(v.Note, "a reset leaves a stopped container stopped") {
		t.Errorf("view = %+v, want released, saying the container stays stopped", v)
	}
}

// TestLeaseReleaseGroup_ParkedMember_StartedAndStoppedAgainOnRefusal: a
// release of a branch starts its parked lease for the guard, and when the
// branch is refused stops it again, so it stays parked.
func TestLeaseReleaseGroup_ParkedMember_StartedAndStoppedAgainOnRefusal(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gf.heldGroup(t)
	if _, err := LeasePark(context.Background(), gf.deps, "webpool-7"); err != nil {
		t.Fatalf("park webpool-7: %v", err)
	}
	gf.answerGuard(nil, map[string]int{"hqpool-3": 1})

	_, err := LeaseReleaseGroup(context.Background(), gf.deps, "", "feat", false)
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || !strings.Contains(pe.Message, "webpool-7: zcp stopped its container again, so it stays parked") {
		t.Fatalf("err = %v, want the branch refused, saying webpool-7 stays parked", err)
	}
	if got := gf.guardReadHosts(); strings.Join(got, ",") != "hqpool-3,webpool-7" {
		t.Errorf("guard read %v, want both leases read, the parked one once started", got)
	}
	gf.web.mu.Lock()
	powers := slices.Clone(gf.web.powers)
	gf.web.mu.Unlock()
	if want := []string{"STOPPED c-webpool-7", "ACTIVE c-webpool-7", "STOPPED c-webpool-7"}; !slices.Equal(powers, want) {
		t.Errorf("webpool stops and starts = %v, want %v (park, start for the guard, stop again)", powers, want)
	}
	if rec := gf.records(t)["webpool-7"]; !rec.Parked {
		t.Errorf("webpool-7's record = %+v, want it still parked", rec)
	}
}

// TestLeaseAcquire_ParkedLeaseOfTheKey_ReusedNotASecond: an acquire of a
// repository, branch and pool zcp holds a parked lease of returns that lease,
// reused and parked — never a second container.
func TestLeaseAcquire_ParkedLeaseOfTheKey_ReusedNotASecond(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gf.heldGroup(t)
	if _, err := LeasePark(context.Background(), gf.deps, "webpool-7"); err != nil {
		t.Fatalf("park webpool-7: %v", err)
	}
	before := gf.web.acquireCount()

	views, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupWebPool}, testRepo, "feat")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if gf.web.acquireCount() != before {
		t.Errorf("acquires = %d, want none beyond the %d before", gf.web.acquireCount(), before)
	}
	if len(views) != 1 || views[0].Alias != "webpool-7" || !views[0].Reused || views[0].State != LeaseStateParked {
		t.Errorf("views = %+v, want webpool-7 reused, parked", views)
	}
}

// TestLeaseRelease_ParkedSibling_NotRewiredNamed: a release that rewires the
// branch's other leases (DP-26) sends a parked one nothing — nothing answers
// on it — and names it with the unpark, which sends the overlay as it then
// stands.
func TestLeaseRelease_ParkedSibling_NotRewiredNamed(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gf.heldGroup(t)
	if _, err := LeasePark(context.Background(), gf.deps, "webpool-7"); err != nil {
		t.Fatalf("park webpool-7: %v", err)
	}

	change, err := LeaseReleaseChange(context.Background(), gf.deps, "hqpool-3", false)
	if err != nil {
		t.Fatalf("release hqpool-3: %v", err)
	}
	if len(change.Rewired) != 1 || change.Rewired[0].Alias != "webpool-7" || !strings.Contains(change.Rewired[0].Note, "is parked") ||
		!strings.Contains(change.Rewired[0].Note, "zerops_lease action=unpark lease=webpool-7") {
		t.Errorf("rewired = %+v, want webpool-7 named parked with its unpark", change.Rewired)
	}
	gf.ssh.mu.Lock()
	defer gf.ssh.mu.Unlock()
	for _, c := range gf.ssh.calls {
		if c.hostname == "webpool-7" {
			t.Errorf("ssh sent to the parked webpool-7: %s", c.command)
		}
	}
}

// TestLeaseSync_ParkedLease_RefusedOrNamedParked: a sync from a parked lease
// is refused naming its unpark; a parked member is not tried and is named
// parked with the unpark and the sync again.
func TestLeaseSync_ParkedLease_RefusedOrNamedParked(t *testing.T) {
	t.Parallel()

	t.Run("the source", func(t *testing.T) {
		t.Parallel()
		gf, remote := newSyncFixture(t, syncBranch)
		markParked(t, gf.stateDir, "hqpool-3", parkNow)

		_, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || !strings.Contains(pe.Message, "is parked") || !strings.Contains(pe.Suggestion, "zerops_lease action=unpark lease=hqpool-3") {
			t.Fatalf("err = %v, want a refusal naming the unpark", err)
		}
		if sent := remote.all(); len(sent) != 0 {
			t.Errorf("sent %v; a parked source is refused before anything runs", sent)
		}
	})

	t.Run("a member", func(t *testing.T) {
		t.Parallel()
		gf, remote := newSyncFixture(t, syncBranch)
		markParked(t, gf.stateDir, "webpool-7", parkNow)

		v, err := LeaseSync(context.Background(), gf.deps, "hqpool-3")
		if err != nil {
			t.Fatalf("LeaseSync: %v", err)
		}
		m := syncMemberOf(t, v, "webpool-7")
		if m.Outcome != LeaseSyncParked || !strings.Contains(m.Note, "zerops_lease action=unpark lease=webpool-7") || !strings.Contains(m.Note, "zerops_lease action=sync lease=hqpool-3") {
			t.Errorf("member = %+v, want parked, naming the unpark and the sync again", m)
		}
		if sent := remote.sent("webpool-7"); len(sent) != 0 {
			t.Errorf("sent the parked member %v", sent)
		}
	})
}

// TestLeaseStatus_IdleWithUnsaved_OffersPark: an idle lease holding work a
// release would lose is offered the park beside the push — never a release.
func TestLeaseStatus_IdleWithUnsaved_OffersPark(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	recordLeaseAt(t, fx.stateDir, testPoolHost, "svc-pool", 3, "feat-a", testRepo, before(200))
	fx.swarm.containers = []platform.SwarmContainer{reservedContainer("c-3", 3)}
	fx.deps.Now = func() time.Time { return idleNow }
	answer := idleAnswer(before(50), []string{" M src/App.tsx"}, 1, time.Time{}, 0)
	fx.deps.SSH = &ctxSSH{fn: func(context.Context, string, string) ([]byte, error) { return []byte(answer), nil }}

	v := poolLeaseView(t, fx.deps, "devpool-3")

	if v.Idle == nil || !strings.Contains(v.Idle.Next, "zerops_lease action=park lease=devpool-3") {
		t.Fatalf("idle = %+v, want its next to offer zerops_lease action=park lease=devpool-3", v.Idle)
	}
	for _, banned := range []string{"force", "action=release"} {
		if strings.Contains(v.Idle.Next, banned) {
			t.Errorf("idle next %q carries %q", v.Idle.Next, banned)
		}
	}
}
