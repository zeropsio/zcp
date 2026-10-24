// Tests for: ops/lease_wiring.go, the rewire in lease.go and lease_group.go —
// when a lease joins or leaves its branch's group, zcp first hands every other
// lease of the group the overlay recomputed without (or with) it, a failure
// there a note and never a refusal; a release that fails afterwards hands it
// back with the lease still held; status names wiring that points at a
// container no longer of the group and fixes nothing (docs/spec-workflows.md
// §8 "Dev pools (DP)").
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
)

// rewireDeclaration has each part reach the other's lease of the branch and
// runs one server, named after the part, without a watch.
func rewireDeclaration(server string, port string) string {
	return `{"version":1,"siblingEnv":{"HQ_HOST":"hqpool","WEB_HOST":"webpool"},"servers":[{"name":"` + server + `","port":` + port + `,"command":"run ` + server + `"}]}`
}

// The hostnames of the containers the group fixture's orchestrators hand out
// first: hqpool #3 and webpool #7.
const (
	rewireHQHost  = "node-id-3.runtime.hqpool.zerops"
	rewireWebHost = "node-id-7.runtime.webpool.zerops"
)

// rewireSSH is the leased containers zcp reaches: each alias with a zcp-devsup
// is a devservertest.Container answering its supervisor's commands; the
// checkout (a new branch, cut from groupBase) and the release guard's reads (a
// clean checkout) are answered here; an unreachable alias fails every command
// as ssh does. log keeps every command, "host: command", in the order sent.
type rewireSSH struct {
	mu          sync.Mutex
	containers  map[string]*devservertest.Container
	unreachable map[string]error
	log         []string
}

func (s *rewireSSH) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	s.mu.Lock()
	s.log = append(s.log, host+": "+command)
	c, down := s.containers[host], s.unreachable[host]
	s.mu.Unlock()
	switch {
	case down != nil:
		return nil, &platform.SSHExecError{Hostname: host, Output: "ssh: connect to host " + host + " port 22: Connection refused", Err: down}
	case strings.Contains(command, "fetch -q --prune origin"):
		return []byte("@@zcp-lease base " + groupBase + "\n"), nil
	case strings.Contains(command, "status --porcelain"):
		return nil, nil
	case strings.Contains(command, "rev-list --count"):
		return []byte("0\n"), nil
	case c != nil:
		return c.ExecSSH(ctx, host, command)
	}
	return nil, nil
}

func (s *rewireSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *rewireSSH) setUnreachable(host string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unreachable[host] = err
}

// sent are the commands sent to host containing part, in order.
func (s *rewireSSH) sent(host, part string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, l := range s.log {
		if cmd, ok := strings.CutPrefix(l, host+": "); ok && strings.Contains(cmd, part) {
			out = append(out, cmd)
		}
	}
	return out
}

// rewireFixture is the group fixture whose leases of hqpool and webpool each
// run zcp-devsup with rewireDeclaration: hqpool's container runs api, webpool's
// runs web.
type rewireFixture struct {
	*groupFixture
	ssh     *rewireSSH
	hq, web *devservertest.Container
}

func newRewireFixture(t *testing.T) *rewireFixture {
	t.Helper()
	gf := newGroupFixture(t)
	rf := &rewireFixture{
		groupFixture: gf,
		hq:           devservertest.NewSupervised(rewireDeclaration("api", "8080")),
		web:          devservertest.NewSupervised(rewireDeclaration("web", "3000")),
	}
	rf.ssh = &rewireSSH{containers: map[string]*devservertest.Container{"hqpool-3": rf.hq, "webpool-7": rf.web}, unreachable: map[string]error{}}
	gf.deps.SSH = rf.ssh
	return rf
}

// acquire leases pools on branch feat and fails the test on a refusal.
func (rf *rewireFixture) acquire(t *testing.T, pools ...string) LeaseChange {
	t.Helper()
	change, err := LeaseAcquireGroupChange(context.Background(), rf.deps, pools, testRepo, "feat")
	if err != nil {
		t.Fatalf("acquire %v: %v", pools, err)
	}
	return change
}

func rewireOf(t *testing.T, rewired []LeaseRewire, alias string) LeaseRewire {
	t.Helper()
	for _, r := range rewired {
		if r.Alias == alias {
			return r
		}
	}
	t.Fatalf("rewired %+v lacks %s", rewired, alias)
	return LeaseRewire{}
}

// releaseHook runs before at the start of every release the orchestrator is
// asked for.
type releaseHook struct {
	SwarmAPI
	before func()
}

func (h releaseHook) Release(ctx context.Context, id, lease string, reset bool) (platform.SwarmContainer, error) {
	h.before()
	return h.SwarmAPI.Release(ctx, id, lease, reset)
}

// onHQRelease runs before at the start of every release hqpool's orchestrator
// is asked for.
func (rf *rewireFixture) onHQRelease(before func()) {
	orchs := map[string]SwarmAPI{orchestratorURL(groupHQPool): releaseHook{SwarmAPI: rf.groupFixture.hq, before: before}, orchestratorURL(groupWebPool): rf.groupFixture.web}
	rf.deps.Swarm = func(baseURL, _ string) SwarmAPI { return orchs[baseURL] }
}

// TestLeaseAcquire_JoiningGroupRewiresMembers: an acquire that adds a lease to
// its branch's group hands every lease of the group it did not take — one
// leased by an earlier call, one this call returned reused — the overlay
// recomputed with the new member, after the checkout; the supervisor restarts
// the servers running under the old one, and the result names each lease it
// rewired with the servers restarted and how long it took. A lease this call
// took gets the overlay in its own supervisor step, never a rewire.
func TestLeaseAcquire_JoiningGroupRewiresMembers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// second is the call that adds webpool to the branch hqpool holds.
		second []string
	}{
		{name: "a single acquire joins the group", second: []string{groupWebPool}},
		{name: "a group acquire returns hqpool reused and adds webpool", second: []string{groupHQPool, groupWebPool}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rf := newRewireFixture(t)
			if first := rf.acquire(t, groupHQPool); len(first.Rewired) != 0 {
				t.Fatalf("the first lease of a branch rewired %+v: it has no other lease", first.Rewired)
			}
			if got := rf.hq.Overlay(); !maps.Equal(got, map[string]string{"HQ_HOST": rewireHQHost}) {
				t.Fatalf("hqpool-3's overlay after its acquire = %v, want only its own HQ_HOST", got)
			}
			before, _ := rf.hq.Restarts("api")

			change := rf.acquire(t, tt.second...)

			want := map[string]string{"HQ_HOST": rewireHQHost, "WEB_HOST": rewireWebHost}
			if got := rf.hq.Overlay(); !maps.Equal(got, want) {
				t.Errorf("hqpool-3's overlay = %v, want %v: WEB_HOST names the lease that joined", got, want)
			}
			if n, why := rf.hq.Restarts("api"); n != before+1 || why != "env changed" {
				t.Errorf("api restarts = %d (%q), want %d: one more, under the new overlay", n, why, before+1)
			}
			if len(change.Rewired) != 1 {
				t.Fatalf("rewired = %+v, want hqpool-3 only (webpool-7 got its overlay in its own supervisor step)", change.Rewired)
			}
			r := rewireOf(t, change.Rewired, "hqpool-3")
			if r.SiblingEnv == nil || !maps.Equal(r.SiblingEnv.Set, want) || !slices.Equal(r.Restarted, []string{"api"}) || r.Note != "" || r.Millis < 0 {
				t.Errorf("rewire = %+v, want the overlay %v, api restarted, no note", r, want)
			}
			if got := rf.web.Overlay(); !maps.Equal(got, want) {
				t.Errorf("webpool-7's overlay = %v, want %v from its own supervisor step", got, want)
			}
			if n := len(rf.ssh.sent("webpool-7", "@@zcp-devsup ctl")); n != 0 {
				t.Errorf("webpool-7 was sent %d ctl call(s) outside its supervisor step: %v", n, rf.ssh.sent("webpool-7", "@@zcp-devsup ctl"))
			}
		})
	}
}

// TestLeaseAcquire_NewGroup_RewiresNothing: a group acquire whose every lease
// is new rewires nothing — each gets the group's overlay in its own
// supervisor step — and an acquire that only returns held leases changes no
// membership, so it rewires nothing either.
func TestLeaseAcquire_NewGroup_RewiresNothing(t *testing.T) {
	t.Parallel()
	rf := newRewireFixture(t)
	if change := rf.acquire(t, groupHQPool, groupWebPool); len(change.Rewired) != 0 {
		t.Errorf("a new group rewired %+v", change.Rewired)
	}
	envs := len(rf.ssh.sent("hqpool-3", "ctl 'env'"))
	if change := rf.acquire(t, groupWebPool, groupHQPool); len(change.Rewired) != 0 {
		t.Errorf("an acquire of held leases rewired %+v", change.Rewired)
	}
	if n := len(rf.ssh.sent("hqpool-3", "ctl 'env'")); n != envs {
		t.Errorf("an acquire of held leases sent hqpool-3 %d more ctl env", n-envs)
	}
}

// TestLeaseRelease_RewiresSiblingsBeforeRelease: a released container goes to
// the next acquirer under the same hostname, so before hqpool-3 is released
// every other lease of its branch gets the overlay without it — HQ_HOST falls
// back to the container's own value — and the restarts it sets off are
// awaited: when the orchestrator is asked to release, web already runs without
// HQ_HOST. The result names the lease rewired and what restarted.
func TestLeaseRelease_RewiresSiblingsBeforeRelease(t *testing.T) {
	t.Parallel()
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "guarded", true: "forced"}[force], func(t *testing.T) {
			t.Parallel()
			rf := newRewireFixture(t)
			rf.acquire(t, groupHQPool, groupWebPool)
			before, _ := rf.web.Restarts("web")
			var atRelease map[string]string
			var restartsAtRelease int
			rf.onHQRelease(func() {
				atRelease = rf.web.Overlay()
				restartsAtRelease, _ = rf.web.Restarts("web")
			})

			change, err := LeaseReleaseChange(context.Background(), rf.deps, "hqpool-3", force)
			if err != nil {
				t.Fatalf("release: %v", err)
			}

			want := map[string]string{"WEB_HOST": rewireWebHost}
			if !maps.Equal(atRelease, want) {
				t.Errorf("webpool-7's overlay when hqpool-3 was released = %v, want %v: the rewire runs before the release", atRelease, want)
			}
			if restartsAtRelease != before+1 {
				t.Errorf("web had restarted %d times when hqpool-3 was released, want %d: its restart under the new overlay awaited", restartsAtRelease, before+1)
			}
			if len(change.Leases) != 1 || change.Leases[0].Alias != "hqpool-3" || change.Leases[0].State != LeaseStateReleased {
				t.Fatalf("leases = %+v, want hqpool-3 released", change.Leases)
			}
			r := rewireOf(t, change.Rewired, "webpool-7")
			if r.SiblingEnv == nil || !maps.Equal(r.SiblingEnv.Set, want) || !slices.Equal(r.SiblingEnv.LeftToContainerEnv, []string{"HQ_HOST"}) {
				t.Errorf("rewire siblingEnv = %+v, want %v with HQ_HOST left to the container's env", r.SiblingEnv, want)
			}
			if !slices.Equal(r.Restarted, []string{"web"}) || r.Note != "" {
				t.Errorf("rewire = %+v, want web restarted and no note", r)
			}
			if n := len(rf.ssh.sent("hqpool-3", "ctl 'env'")); n != 1 {
				t.Errorf("hqpool-3 was sent %d ctl env, want only its acquire's: the lease leaving is reset, never rewired", n)
			}
		})
	}
}

// TestLeaseRelease_RewireFails_NoteNotBlock: another lease's health never
// holds a release up: a sibling ssh cannot reach, one whose supervisor does
// not answer, or one that refuses the overlay is a note naming it and the call
// that sends the overlay again, and the release goes ahead.
func TestLeaseRelease_RewireFails_NoteNotBlock(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		breakIt  func(t *testing.T, rf *rewireFixture)
		wantNote []string
	}{
		{name: "unreachable", breakIt: func(t *testing.T, rf *rewireFixture) {
			t.Helper()
			rf.ssh.setUnreachable("webpool-7", exitError(t, 255))
		},
			wantNote: []string{"webpool-7", "Connection refused", "hqpool-3", `zerops_dev_server action=restart hostname="webpool-7"`}},
		{name: "supervisor unresponsive", breakIt: func(_ *testing.T, rf *rewireFixture) { rf.web.Unresponsive = true },
			wantNote: []string{"webpool-7", "did not answer", `zerops_dev_server action=restart hostname="webpool-7"`}},
		{name: "overlay refused", breakIt: func(_ *testing.T, rf *rewireFixture) { rf.web.EnvRefusal = "env: the state directory is read-only" },
			wantNote: []string{"webpool-7", "read-only", `zerops_dev_server action=restart hostname="webpool-7" name="web"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rf := newRewireFixture(t)
			rf.acquire(t, groupHQPool, groupWebPool)
			tt.breakIt(t, rf)

			change, err := LeaseReleaseChange(context.Background(), rf.deps, "hqpool-3", true)
			if err != nil {
				t.Fatalf("a sibling's failure blocked the release: %v", err)
			}
			if len(change.Leases) != 1 || change.Leases[0].State != LeaseStateReleased {
				t.Errorf("leases = %+v, want hqpool-3 released", change.Leases)
			}
			if len(rf.releaseLog(groupHQPool)) != 1 {
				t.Errorf("hqpool's orchestrator was asked %d times, want the release", len(rf.releaseLog(groupHQPool)))
			}
			r := rewireOf(t, change.Rewired, "webpool-7")
			for _, want := range tt.wantNote {
				if !strings.Contains(r.Note, want) {
					t.Errorf("rewire note %q lacks %q", r.Note, want)
				}
			}
		})
	}
}

// releaseLog is the releases the pool's orchestrator was asked for.
func (rf *rewireFixture) releaseLog(pool string) []fakeRelease {
	if pool == groupHQPool {
		return rf.groupFixture.hq.releaseLog()
	}
	return rf.groupFixture.web.releaseLog()
}

// TestLeaseRelease_FailsAfterRewire_ResendsWithMemberBackIn: when the
// orchestrator will not release hqpool-3 after its siblings were rewired
// without it, hqpool-3 stays held and recorded, so the call hands every
// sibling it rewired the overlay with hqpool-3 back in, and the refusal says
// so.
func TestLeaseRelease_FailsAfterRewire_ResendsWithMemberBackIn(t *testing.T) {
	t.Parallel()
	rf := newRewireFixture(t)
	rf.acquire(t, groupHQPool, groupWebPool)
	rf.groupFixture.hq.releaseErr = &platform.SwarmError{Status: 409, Code: "containerAction", Reason: "container-busy", Retryable: true, Message: "work in progress"}
	var atRelease map[string]string
	rf.onHQRelease(func() { atRelease = rf.web.Overlay() })

	_, err := LeaseReleaseChange(context.Background(), rf.deps, "hqpool-3", true)

	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want the orchestrator's refusal", err)
	}
	if _, ok := atRelease["HQ_HOST"]; ok || atRelease == nil {
		t.Errorf("webpool-7's overlay at the release = %v, want it rewired without hqpool-3 first", atRelease)
	}
	want := map[string]string{"HQ_HOST": rewireHQHost, "WEB_HOST": rewireWebHost}
	if got := rf.web.Overlay(); !maps.Equal(got, want) {
		t.Errorf("webpool-7's overlay after the failed release = %v, want %v: hqpool-3 is still held", got, want)
	}
	if _, ok := rf.records(t)["hqpool-3"]; !ok {
		t.Errorf("hqpool-3's record is gone after a failed release")
	}
	for _, want := range []string{"work in progress", "webpool-7", "hqpool-3", "back"} {
		if !strings.Contains(pe.Message, want) {
			t.Errorf("refusal %q lacks %q", pe.Message, want)
		}
	}
}

// TestLeaseReleaseGroup_OrchestratorFailsOne_RewiresTheLeaseLeftHeld: a
// release of the branch takes every lease of it, so nothing is rewired before
// it; a lease the orchestrator will not release stays held, and the call hands
// it the overlay without the leases that went.
func TestLeaseReleaseGroup_OrchestratorFailsOne_RewiresTheLeaseLeftHeld(t *testing.T) {
	t.Parallel()
	rf := newRewireFixture(t)
	rf.acquire(t, groupHQPool, groupWebPool)
	rf.groupFixture.web.releaseErr = &platform.SwarmError{Status: 409, Code: "containerAction", Reason: "container-busy", Retryable: true, Message: "work in progress"}

	change, err := LeaseReleaseGroupChange(context.Background(), rf.deps, "", "feat", true)
	if err != nil {
		t.Fatalf("release branch: %v", err)
	}
	states := map[string]string{}
	for _, v := range change.Leases {
		states[v.Alias] = v.State
	}
	if states["hqpool-3"] != LeaseStateReleased || states["webpool-7"] != LeaseStateHeld {
		t.Fatalf("leases = %+v, want hqpool-3 released and webpool-7 held", change.Leases)
	}
	want := map[string]string{"WEB_HOST": rewireWebHost}
	if got := rf.web.Overlay(); !maps.Equal(got, want) {
		t.Errorf("webpool-7's overlay = %v, want %v: hqpool-3 went", got, want)
	}
	if r := rewireOf(t, change.Rewired, "webpool-7"); !slices.Equal(r.Restarted, []string{"web"}) {
		t.Errorf("rewire = %+v, want web restarted", r)
	}
}

// TestLeaseReleaseGroup_EveryLeaseGoes_RewiresNothing: a branch whose every
// lease is released leaves no lease to rewire.
func TestLeaseReleaseGroup_EveryLeaseGoes_RewiresNothing(t *testing.T) {
	t.Parallel()
	rf := newRewireFixture(t)
	rf.acquire(t, groupHQPool, groupWebPool)
	envs := len(rf.ssh.sent("webpool-7", "ctl 'env'")) + len(rf.ssh.sent("hqpool-3", "ctl 'env'"))

	change, err := LeaseReleaseGroupChange(context.Background(), rf.deps, "", "feat", true)
	if err != nil {
		t.Fatalf("release branch: %v", err)
	}
	if len(change.Rewired) != 0 {
		t.Errorf("rewired = %+v, want none", change.Rewired)
	}
	if n := len(rf.ssh.sent("webpool-7", "ctl 'env'")) + len(rf.ssh.sent("hqpool-3", "ctl 'env'")); n != envs {
		t.Errorf("%d ctl env sent to leases being released", n-envs)
	}
}

// staleStatusFixture holds feat-x of syncRepo on hqpool-7 and webpool-3:
// webpool-3 is reserved for zcp, and hqpool-7 is reserved too unless lost —
// then its container is gone from the pool, as when its lease ended outside
// zcp. webpool-3's checkout answers its activity read with the supervisor part
// given.
func staleStatusFixture(t *testing.T, hqLost bool, preview string) (*groupFixture, *ctxSSH) {
	t.Helper()
	gf := newGroupFixture(t)
	for _, r := range []leaseRecord{syncRecord(groupHQPool, "svc-hq", 7, syncBranch, "2026-10-24T10:00:00Z"), syncRecord(groupWebPool, "svc-web", 3, syncBranch, "2026-10-24T10:00:01Z")} {
		if err := writeLeaseRecord(gf.stateDir, r); err != nil {
			t.Fatal(err)
		}
	}
	gf.web.containers = []platform.SwarmContainer{{ID: "c-webpool", Number: 3, Hostname: nodeHostnameOf(groupWebPool, 3), LockState: leaseLockReserved}}
	if !hqLost {
		gf.hq.containers = []platform.SwarmContainer{{ID: "c-hqpool", Number: 7, Hostname: nodeHostnameOf(groupHQPool, 7), LockState: leaseLockReserved}}
	}
	ssh := &ctxSSH{fn: func(_ context.Context, host, _ string) ([]byte, error) {
		answer := string(checkoutAnswer(1792756800, "", nil, 0))
		if host == "webpool-3" {
			answer += preview
		}
		return []byte(answer), nil
	}}
	gf.deps.SSH = ssh
	return gf, ssh
}

// overlayState is a `ctl status --json` answer whose supervisor holds env as
// its overlay and runs web (running when live, else stopped).
func overlayState(env string, live bool) string {
	status, desired := "running", "running"
	if !live {
		status, desired = "stopped", "stopped"
	}
	return `{"format":1,"supervisor":{"version":"v9.400.0","pid":412},"declaration":{"version":1,"error":""},"env":` + env +
		`,"servers":{"web":{"source":"declared","desired":"` + desired + `","status":"` + status + `","pid":901,"restarts":0}}}`
}

// TestLeaseStatus_StaleWiring_NamedNotFixed: a lease whose dev servers run
// with an overlay naming a container no lease of its group holds any more —
// its lease ended outside zcp — is marked wiring: stale, with the variable,
// the container, the lease it was and the call that sends the overlay again;
// status sends nothing. Wiring naming a held lease, an overlay no server runs
// under, and a supervisor that predates the overlay are not stale.
func TestLeaseStatus_StaleWiring_NamedNotFixed(t *testing.T) {
	t.Parallel()
	hq7 := `{"HQ_HOST":"` + nodeHostnameOf(groupHQPool, 7) + `","WEB_HOST":"` + nodeHostnameOf(groupWebPool, 3) + `"}`
	tests := []struct {
		name      string
		hqLost    bool
		preview   string
		wantStale bool
	}{
		{name: "hqpool-7 lost: stale", hqLost: true, preview: previewPart(envOnlyVersion, 0, overlayState(hq7, true), 0), wantStale: true},
		{name: "hqpool-7 held: wired", preview: previewPart(envOnlyVersion, 0, overlayState(hq7, true), 0)},
		{name: "a container no lease of the group ever was: stale", preview: previewPart(envOnlyVersion, 0,
			overlayState(`{"HQ_HOST":"`+nodeHostnameOf(groupHQPool, 9)+`"}`, true), 0), wantStale: true},
		{name: "no server runs under it", hqLost: true, preview: previewPart(envOnlyVersion, 0, overlayState(hq7, false), 0)},
		{name: "a supervisor without the overlay", hqLost: true, preview: previewPart(olderVersion, 0, overlayState(hq7, true), 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, pool := range []string{"", groupWebPool} {
				gf, ssh := staleStatusFixture(t, tt.hqLost, tt.preview)

				status, err := LeaseStatus(context.Background(), gf.deps, pool)
				if err != nil {
					t.Fatalf("status pool=%q: %v", pool, err)
				}

				var web LeaseView
				for _, v := range status.Leases {
					if v.Alias == "webpool-3" {
						web = v
					}
				}
				if web.State != LeaseStateHeld {
					t.Fatalf("pool=%q: webpool-3 = %+v, want held", pool, web)
				}
				// A status of webpool alone does not read hqpool's orchestrator:
				// only a container no record of the group names is known stale.
				wantStale := tt.wantStale && (pool == "" || !tt.hqLost)
				if got := web.Wiring == LeaseWiringStale; got != wantStale {
					t.Errorf("pool=%q: wiring = %q, want stale=%v (note %q)", pool, web.Wiring, wantStale, web.Note)
				}
				if wantStale {
					for _, want := range []string{"HQ_HOST", "web", `zerops_dev_server action=restart hostname="webpool-3" name="web"`} {
						if !strings.Contains(web.Note, want) {
							t.Errorf("pool=%q: note %q lacks %q", pool, web.Note, want)
						}
					}
					if tt.hqLost && !strings.Contains(web.Note, "hqpool-7") {
						t.Errorf("pool=%q: note %q does not name the lease the container was", pool, web.Note)
					}
				}
				ssh.mu.Lock()
				for _, c := range ssh.calls {
					if c.command != leaseActivityCommand() {
						t.Errorf("pool=%q: status sent %q to %s; it reads and fixes nothing", pool, c.command, c.hostname)
					}
				}
				ssh.mu.Unlock()
				if n := len(gf.hq.releaseLog()) + len(gf.web.releaseLog()); n != 0 {
					t.Errorf("pool=%q: status released %d container(s)", pool, n)
				}
			}
		})
	}
}

// TestLeaseRelease_SiblingDeclarationUnreadable_DropsOnlyTheLeavingContainer:
// a sibling whose declaration cannot be read still never stays wired to the
// container that leaves: zcp keeps the variables naming a container of the
// group and drops the rest, and says why.
func TestLeaseRelease_SiblingDeclarationUnreadable_DropsOnlyTheLeavingContainer(t *testing.T) {
	t.Parallel()
	rf := newRewireFixture(t)
	rf.acquire(t, groupHQPool, groupWebPool)
	rf.web.Declaration = `{"version":1,"servers":[{"name":"web","port":3000}]}` // an edit broke it

	change, err := LeaseReleaseChange(context.Background(), rf.deps, "hqpool-3", true)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if got, want := rf.web.Overlay(), map[string]string{"WEB_HOST": rewireWebHost}; !maps.Equal(got, want) {
		t.Errorf("webpool-7's overlay = %v, want %v: only hqpool-3's container dropped", got, want)
	}
	if r := rewireOf(t, change.Rewired, "webpool-7"); !strings.Contains(r.Note, "cannot be read") || !strings.Contains(r.Note, "webpool-7") {
		t.Errorf("rewire note %q should say webpool-7's declaration cannot be read", r.Note)
	}
}

// TestLeaseAcquire_JoiningGroup_SupervisorWithoutEnv_NotedNotSent: a lease
// whose supervisor does not list env never gets ctl env from a rewire; the
// rewire notes what it cannot set and the way to a newer supervisor.
func TestLeaseAcquire_JoiningGroup_SupervisorWithoutEnv_NotedNotSent(t *testing.T) {
	t.Parallel()
	rf := newRewireFixture(t)
	rf.hq.Capabilities, rf.hq.SupervisorVersion = nil, "v9.200.0"
	rf.acquire(t, groupHQPool)

	change := rf.acquire(t, groupWebPool)

	if sent := rf.ssh.sent("hqpool-3", "ctl 'env'"); len(sent) != 0 {
		t.Errorf("ctl env sent to a supervisor that does not list it: %v", sent)
	}
	r := rewireOf(t, change.Rewired, "hqpool-3")
	for _, want := range []string{"hqpool-3", "v9.200.0", "WEB_HOST", "zerops_lease action=rollout"} {
		if !strings.Contains(r.Note, want) {
			t.Errorf("rewire note %q lacks %q", r.Note, want)
		}
	}
}
