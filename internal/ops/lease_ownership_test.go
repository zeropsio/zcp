// Tests for: whether the container a lease record names is still that
// record's reservation (docs/spec-workflows.md §8 DP-2) — asked of the
// orchestrator with the record's own lease (POST /container/{id}/renew, which
// changes nothing for a lease with no expiry): ours, gone, another holder's
// or free (lost), or not answered (unverified) — and what status, release
// (one lease, a branch, forced), the warm decision, park, unpark, sync, the
// acquire's reuse check, the rewire and a full pool's refusal do with each
// verdict. A lease zcp no longer holds is never read or written over ssh, its
// container never reset, deleted, stopped or started, and zcp's own lease is
// never called lost because its reservation's lockSince moved.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// The live values of the 2026-10-24 drive (lease-ledger 4g): zcp acquired
// lvapi-3 at 11:01:00 (its reservation's lockSince 11:01:00.888), it was
// released outside zcp, and another consumer reserved the container at
// 11:02:04.783. ownSoonSince is another consumer's reservation 20 s after the
// acquire: by the reservation's time alone it reads like zcp's own.
const (
	ownAcquiredAt    = "2026-10-24T11:01:00Z"
	ownLockSince     = "2026-10-24T11:01:00.88865963Z"
	ownOtherSince    = "2026-10-24T11:02:04.783475573Z"
	ownSoonSince     = "2026-10-24T11:01:20.5Z"
	ownOtherLease    = "another-consumers-lease-0123456789"
	ownHQContainer   = "c-hqpool-3"
	ownWebContainer  = "c-webpool-7"
	ownHQAlias       = "hqpool-3"
	ownWebAlias      = "webpool-7"
	ownRemovedReason = "in a moment"
	// ownHQLease and ownLeaseOfWeb are the leases zcp's records of
	// hqpool-3 and webpool-7 hold.
	ownHQLease    = "hqpool-lease-3-0123456789abcdef"
	ownLeaseOfWeb = "webpool-lease-7-0123456789abcdef"
)

// lockedMounter is mockMounter safe for a branch's members unmounting at once.
type lockedMounter struct {
	mu sync.Mutex
	m  *mockMounter
}

func (l *lockedMounter) CheckMount(ctx context.Context, p string) (platform.MountState, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.m.CheckMount(ctx, p)
}

func (l *lockedMounter) Mount(ctx context.Context, h, p string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.m.Mount(ctx, h, p)
}

func (l *lockedMounter) Unmount(ctx context.Context, h, p string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.m.Unmount(ctx, h, p)
}

func (l *lockedMounter) ForceUnmount(ctx context.Context, h, p string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.m.ForceUnmount(ctx, h, p)
}

func (l *lockedMounter) IsWritable(ctx context.Context, p string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.m.IsWritable(ctx, p)
}

func (l *lockedMounter) ListMountDirs(ctx context.Context, p string) ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.m.ListMountDirs(ctx, p)
}

func (l *lockedMounter) HasUnit(ctx context.Context, h string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.m.HasUnit(ctx, h)
}

func (l *lockedMounter) CleanupUnit(ctx context.Context, h string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.m.CleanupUnit(ctx, h)
}

func (l *lockedMounter) mounted(alias string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.m.states["/var/www/"+alias] == platform.MountStateActive
}

// ownFixture is groupFixture (hqpool and webpool, each a poolOrch) with the
// host side — an alias file and a mounter — and ssh whose checkouts are clean.
type ownFixture struct {
	*groupFixture
	mounter   *lockedMounter
	aliasFile string
}

func newOwnFixture(t *testing.T) *ownFixture {
	t.Helper()
	gf := newGroupFixture(t)
	gf.ssh.setHandler(func(_, command string) ([]byte, error) {
		switch {
		case strings.Contains(command, "fetch -q --prune origin"):
			return []byte("@@zcp-lease base " + groupBase + "\n"), nil
		case strings.Contains(command, "rev-list --count"):
			return []byte("0\n"), nil
		}
		return nil, nil
	})
	of := &ownFixture{groupFixture: gf, mounter: &lockedMounter{m: newMockMounter()}, aliasFile: filepath.Join(t.TempDir(), "zcp-leases.conf")}
	var runMu sync.Mutex
	gf.deps.Host = LeaseHost{
		Mounter:   of.mounter,
		AliasFile: of.aliasFile,
		Run: func(_ context.Context, _ string, args ...string) error {
			runMu.Lock()
			defer runMu.Unlock()
			raw, err := os.ReadFile(args[len(args)-2])
			if err != nil {
				return err
			}
			return os.WriteFile(args[len(args)-1], raw, 0o644)
		},
	}
	return of
}

// acquire leases feat-x on pools through zcp, then forgets the ssh calls the
// acquire made.
func (of *ownFixture) acquire(t *testing.T, pools ...string) {
	t.Helper()
	if _, err := LeaseAcquireGroup(context.Background(), of.deps, pools, testRepo, "feat-x"); err != nil {
		t.Fatalf("acquire feat-x on %v: %v", pools, err)
	}
	of.forgetSSH()
}

func (of *ownFixture) forgetSSH() {
	of.ssh.mu.Lock()
	defer of.ssh.mu.Unlock()
	of.ssh.calls = nil
}

// reached reports whether ssh reached host since the last forgetSSH.
func (of *ownFixture) reached(host string) bool {
	of.ssh.mu.Lock()
	defer of.ssh.mu.Unlock()
	return slices.ContainsFunc(of.ssh.calls, func(c sshCall) bool { return c.hostname == host })
}

// writeRecord writes a record of pool's container #number as zcp writes it,
// with fields replacing or adding to its keys (raw JSON: a record written
// before zcp kept the reservation's lockSince has no such key).
func (of *ownFixture) writeRecord(t *testing.T, pool, svcID string, number int64, fields map[string]any) {
	t.Helper()
	rec := map[string]any{
		"poolHostname": pool, "poolServiceId": svcID,
		"containerId": fmt.Sprintf("c-%s-%d", pool, number), "containerNumber": number,
		"containerHostname": nodeHostnameOf(pool, number),
		"lease":             fmt.Sprintf("%s-lease-%d-0123456789abcdef", pool, number),
		"branch":            "feat-x", "repo": topology.CanonicalRepoURL(testRepo), "acquiredAt": ownAcquiredAt,
	}
	maps.Copy(rec, fields)
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(of.stateDir, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.json", pool, number)), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// rawRecord is alias's record as JSON keys; nil when there is none.
func (of *ownFixture) rawRecord(t *testing.T, alias string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(of.stateDir, "leases", alias+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("%s's record is not JSON: %v", alias, err)
	}
	return rec
}

func (of *ownFixture) aliasStanza(alias string) bool {
	raw, err := os.ReadFile(of.aliasFile)
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), "Host "+alias+"\n")
}

// hqContainer is hqpool's #3, reserved by lease since lockSince (status
// ACTIVE unless given).
func hqContainer(lockState, lockSince, status string) platform.SwarmContainer {
	if status == "" {
		status = platform.SwarmContainerActive
	}
	return platform.SwarmContainer{
		ID: ownHQContainer, Number: 3, Hostname: nodeHostnameOf(groupHQPool, 3), Status: status, ServiceID: "svc-hq",
		Current: true, LockState: lockState, LockSince: lockSince, Snapshots: 1,
	}
}

// hqView is hqpool-3's view in a status of hqpool.
func hqView(t *testing.T, d LeaseDeps) LeaseView {
	t.Helper()
	status, err := LeaseStatus(context.Background(), d, groupHQPool)
	if err != nil {
		t.Fatalf("LeaseStatus: %v", err)
	}
	for _, v := range status.Leases {
		if v.Alias == ownHQAlias {
			return v
		}
	}
	t.Fatalf("leases = %+v, want %s", status.Leases, ownHQAlias)
	return LeaseView{}
}

// ptrTo is a pointer to a copy of v.
func ptrTo[T any](v T) *T { return &v }

// writeHQ writes a record of hqpool-3 (fields replacing or adding to its keys)
// and lists its container held by lease since lockSince — status ACTIVE unless
// given — mounted (unless stopped) and aliased.
func (of *ownFixture) writeHQ(t *testing.T, fields map[string]any, lease, lockSince, status string) {
	t.Helper()
	of.writeRecord(t, groupHQPool, "svc-hq", 3, fields)
	of.hq.put(hqContainer(leaseLockReserved, lockSince, status), lease)
	if status != platform.SwarmContainerStopped {
		of.mounter.mu.Lock()
		of.mounter.m.states["/var/www/"+ownHQAlias] = platform.MountStateActive
		of.mounter.mu.Unlock()
	}
	if r := of.deps.Host.refreshAliases(context.Background(), of.deps); r != "" {
		t.Fatal(r)
	}
}

// writeOtherSoon is writeHQ for a record older than any lockSince keeping,
// whose container another consumer reserved 20 s after the acquire.
func (of *ownFixture) writeOtherSoon(t *testing.T, fields map[string]any, status string) {
	t.Helper()
	of.writeHQ(t, fields, ownOtherLease, ownSoonSince, status)
}

// hqRenewFails makes every renew of hqpool's orchestrator fail with err.
func (of *ownFixture) hqRenewFails(err error) {
	of.hq.mu.Lock()
	defer of.hq.mu.Unlock()
	of.hq.renewErr = err
}

// routeHQ sends hqpool's orchestrator calls to api, webpool's to its own.
func (of *ownFixture) routeHQ(api SwarmAPI) {
	web := of.web
	of.deps.Swarm = func(baseURL, _ string) SwarmAPI {
		if baseURL == orchestratorURL(groupWebPool) {
			return web
		}
		return api
	}
}

// orchFailure is an orchestrator that refuses a renew for its own reasons.
func orchFailure() error {
	return &platform.SwarmError{Status: 500, Code: "internalServerError", Message: "the orchestrator could not read its reservations"}
}

// orchSilent is an orchestrator that does not answer.
func orchSilent() error {
	return platform.NewPlatformError(platform.ErrNetworkError, "the orchestrator at http://orch.hqpool.zerops did not answer: i/o timeout", "")
}

// takeOverFirst is a poolOrch on which another consumer takes the container
// over right before zcp's call of kind on reaches it — a race no read can
// see — so the call is refused held-by-other.
type takeOverFirst struct {
	*poolOrch
	on string // "stop", "start", "release" or "delete"
}

func (t takeOverFirst) Stop(ctx context.Context, id, lease string) (platform.SwarmContainer, error) {
	if t.on == "stop" {
		t.takeOver(id)
	}
	return t.poolOrch.Stop(ctx, id, lease)
}

func (t takeOverFirst) Start(ctx context.Context, id, lease string) (platform.SwarmContainer, error) {
	if t.on == "start" {
		t.takeOver(id)
	}
	return t.poolOrch.Start(ctx, id, lease)
}

func (t takeOverFirst) Release(ctx context.Context, id, lease string, reset bool) (platform.SwarmContainer, error) {
	if t.on == "release" {
		t.takeOver(id)
	}
	return t.poolOrch.Release(ctx, id, lease, reset)
}

func (t takeOverFirst) Delete(ctx context.Context, id, lease string) error {
	if t.on == "delete" {
		t.takeOver(id)
	}
	return t.poolOrch.Delete(ctx, id, lease)
}

// stopRefused is a poolOrch that refuses every stop 4xx, changing nothing.
type stopRefused struct{ *poolOrch }

func (s stopRefused) Stop(context.Context, string, string) (platform.SwarmContainer, error) {
	return platform.SwarmContainer{}, &platform.SwarmError{Status: 401, Code: "notAuthorized", Message: "invalid token"}
}

// TestLeaseOwnership_Status_RenewDecides: status asks the orchestrator, with
// the record's own lease, whether the container is still the record's
// reservation. Renewed: held — or parked when stopped (DP-28) — however the
// reservation's lockSince moved; refused unknown-lease: another holder's,
// lost, even one that reserved it seconds after zcp's acquire; free, being
// deleted, or gone: lost; not answered: unverified, never lost. Only a lease
// zcp holds, verified, is read over ssh.
func TestLeaseOwnership_Status_RenewDecides(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		record         map[string]any
		container      *platform.SwarmContainer // nil: gone from the pool
		lease          string                   // the lease holding it
		renewErr       error
		wantState      string
		wantUnverified bool
		wantNote       string
	}{
		{
			name:   "renewed, its lockSince moved by a stop and start outside zcp: held",
			record: map[string]any{"lockSince": ownLockSince}, container: ptrTo(hqContainer(leaseLockReserved, ownOtherSince, "")),
			lease: ownHQLease, wantState: LeaseStateHeld,
		},
		{
			name:   "renewed and stopped: parked, whatever its lockSince",
			record: map[string]any{"lockSince": ownLockSince, "parked": true, "parkedAt": "2026-10-24T11:01:30Z"}, container: ptrTo(hqContainer(leaseLockReserved, ownOtherSince, platform.SwarmContainerStopped)),
			lease: ownHQLease, wantState: LeaseStateParked,
		},
		{
			name:   "another holder reserved it 20 s after zcp's acquire: lost",
			record: map[string]any{}, container: ptrTo(hqContainer(leaseLockReserved, ownSoonSince, "")),
			lease: ownOtherLease, wantState: LeaseStateLost, wantNote: "another holder",
		},
		{
			name:   "another holder reserved and stopped it: lost, not parked",
			record: map[string]any{}, container: ptrTo(hqContainer(leaseLockReserved, ownSoonSince, platform.SwarmContainerStopped)),
			lease: ownOtherLease, wantState: LeaseStateLost, wantNote: "another holder",
		},
		{
			name:   "free: lost",
			record: map[string]any{}, container: ptrTo(hqContainer("", "", "")),
			wantState: LeaseStateLost, wantNote: "no longer reserves",
		},
		{
			name:   "being deleted: lost",
			record: map[string]any{}, container: ptrTo(hqContainer("deleting", ownOtherSince, "DELETING")),
			lease: ownHQLease, wantState: LeaseStateLost, wantNote: "deleting",
		},
		{
			name:   "gone from the pool: lost",
			record: map[string]any{}, container: nil,
			wantState: LeaseStateLost, wantNote: "gone",
		},
		{
			name:   "the renew fails on the orchestrator's side: unverified, never lost",
			record: map[string]any{}, container: ptrTo(hqContainer(leaseLockReserved, ownLockSince, "")),
			lease: ownHQLease, renewErr: orchFailure(), wantState: LeaseStateHeld, wantUnverified: true, wantNote: "cannot verify",
		},
		{
			name:   "the renew gets no answer: unverified, never lost",
			record: map[string]any{}, container: ptrTo(hqContainer(leaseLockReserved, ownLockSince, platform.SwarmContainerStopped)),
			lease: ownHQLease, renewErr: orchSilent(), wantState: LeaseStateParked, wantUnverified: true, wantNote: "cannot verify",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			of.writeRecord(t, groupHQPool, "svc-hq", 3, tt.record)
			if tt.container != nil {
				of.hq.put(*tt.container, tt.lease)
			}
			of.hqRenewFails(tt.renewErr)
			of.mounter.m.states["/var/www/"+ownHQAlias] = platform.MountStateActive
			if r := of.deps.Host.refreshAliases(context.Background(), of.deps); r != "" {
				t.Fatal(r)
			}
			v := hqView(t, of.deps)
			if v.State != tt.wantState || v.Unverified != tt.wantUnverified {
				t.Errorf("view = state %q unverified %v (%s), want state %q unverified %v", v.State, v.Unverified, v.Note, tt.wantState, tt.wantUnverified)
			}
			if !strings.Contains(v.Note, tt.wantNote) {
				t.Errorf("note %q lacks %q", v.Note, tt.wantNote)
			}
			readable := tt.wantState == LeaseStateHeld && !tt.wantUnverified
			if of.reached(ownHQAlias) != readable {
				t.Errorf("ssh reached %s = %v, want %v (only a lease zcp holds, verified, is read)", ownHQAlias, of.reached(ownHQAlias), readable)
			}
		})
	}
}

// TestLeaseOwnership_StopStartOutsideZcp_StaysOurs: a stop of zcp's lease made
// outside zcp — an older zcp sharing the state directory, anyone holding the
// lease — restarts the reservation's lockSince but leaves it zcp's: status
// reads it parked, and a release goes to the orchestrator with the lease; the
// record is never dropped while the lease still reserves the container.
func TestLeaseOwnership_StopStartOutsideZcp_StaysOurs(t *testing.T) {
	t.Parallel()
	of := newOwnFixture(t)
	of.acquire(t, groupHQPool)
	lease := of.hq.leaseOf(ownHQContainer)
	if _, err := of.hq.Stop(context.Background(), ownHQContainer, lease); err != nil {
		t.Fatal(err)
	}
	if v := hqView(t, of.deps); v.State != LeaseStateParked || v.Unverified {
		t.Errorf("status = %+v, want parked, verified: the container is still zcp's", v)
	}
	if len(of.hq.renewLog()) == 0 {
		t.Error("status never asked the orchestrator with the lease")
	}
	change, err := LeaseReleaseChange(context.Background(), of.deps, ownHQAlias, false)
	if of.rawRecord(t, ownHQAlias) == nil && of.hq.leaseOf(ownHQContainer) == lease {
		t.Fatalf("release (%+v, %v) dropped the record while zcp's own lease still reserves the container", change.Leases, err)
	}
	if err == nil {
		if got := of.hq.releaseLog(); len(got) != 1 || got[0].lease != lease {
			t.Errorf("releases = %+v, want the one release with zcp's lease", got)
		}
	}
}

// TestLeaseOwnership_LostAnswerThenRefusal_StaysOurs: an unpark whose answer
// was lost, then a park the orchestrator refuses 4xx, leave the container
// zcp's: status reads it held and verified, never lost.
func TestLeaseOwnership_LostAnswerThenRefusal_StaysOurs(t *testing.T) {
	t.Parallel()
	of := newOwnFixture(t)
	of.acquire(t, groupHQPool)
	ctx := context.Background()
	if _, err := LeasePark(ctx, of.deps, ownHQAlias); err != nil {
		t.Fatal(err)
	}
	of.hq.mu.Lock()
	of.hq.powerCut = true
	of.hq.mu.Unlock()
	_, _ = LeaseUnpark(ctx, of.deps, ownHQAlias)
	of.hq.mu.Lock()
	of.hq.powerCut = false
	of.hq.mu.Unlock()
	of.routeHQ(stopRefused{of.hq})
	if _, err := LeasePark(ctx, of.deps, ownHQAlias); err == nil {
		t.Fatal("LeasePark: want the orchestrator's refusal")
	}
	if v := hqView(t, of.deps); v.State == LeaseStateLost || v.Unverified {
		t.Errorf("status = %+v, want zcp's own lease held and verified", v)
	}
}

// TestLeaseOwnership_ConcurrentParkDuringRelease_NeverDropsALiveLease: one
// zcp process releases hqpool-3 while another parks it during the release's
// guard; the guard's ssh then fails against the stopped container, and the
// release asks the orchestrator again — the lease is still zcp's, so the
// release is refused and the record kept.
func TestLeaseOwnership_ConcurrentParkDuringRelease_NeverDropsALiveLease(t *testing.T) {
	t.Parallel()
	of := newOwnFixture(t)
	of.acquire(t, groupHQPool)
	lease := of.hq.leaseOf(ownHQContainer)
	var once sync.Once
	of.ssh.setHandler(func(host, _ string) ([]byte, error) {
		if host != ownHQAlias {
			return nil, nil
		}
		once.Do(func() {
			if _, err := LeasePark(context.Background(), of.deps, ownHQAlias); err != nil {
				t.Errorf("the other process's park: %v", err)
			}
		})
		return nil, &platform.SSHExecError{Hostname: host, Output: "ssh: connect to host: No route to host", Err: exitError(t, 255)}
	})
	change, err := LeaseReleaseChange(context.Background(), of.deps, ownHQAlias, false)
	if of.rawRecord(t, ownHQAlias) == nil && of.hq.leaseOf(ownHQContainer) == lease {
		t.Fatalf("release (%+v, %v) dropped the record while zcp's own lease still reserves the container", change.Leases, err)
	}
	if err == nil {
		t.Errorf("release = %+v, want a refusal: the container was unreachable and is still zcp's", change.Leases)
	}
}

// TestLeaseOwnership_Unpark_OnlyWhenOurs: an unpark acts only on a container
// the orchestrator gives the lease: another holder's is refused and one zcp
// cannot verify is refused too — nothing started, mounted or sent over ssh —
// while one whose start answer was lost earlier is zcp's again at the next
// status, and the next unpark finishes it.
func TestLeaseOwnership_Unpark_OnlyWhenOurs(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, of *ownFixture)
	}{
		{name: "another holder's, running", setup: func(t *testing.T, of *ownFixture) {
			t.Helper()
			of.writeHQ(t, map[string]any{"parked": true, "parkedAt": "2026-10-24T09:00:00Z"}, ownOtherLease, ownOtherSince, "")
		}},
		{name: "not verifiable", setup: func(t *testing.T, of *ownFixture) {
			t.Helper()
			of.writeHQ(t, map[string]any{"parked": true, "parkedAt": "2026-10-24T09:00:00Z"}, ownHQLease, ownLockSince, "")
			of.hqRenewFails(orchFailure())
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			tt.setup(t, of)
			of.mounter.mu.Lock()
			delete(of.mounter.m.states, "/var/www/"+ownHQAlias)
			of.mounter.mu.Unlock()
			of.forgetSSH()

			if _, err := LeaseUnpark(context.Background(), of.deps, ownHQAlias); err == nil {
				t.Error("LeaseUnpark: want a refusal")
			}
			if of.reached(ownHQAlias) || of.mounter.mounted(ownHQAlias) || len(of.hq.powerLog()) != 0 {
				t.Errorf("unpark reached the container (ssh %v, mounted %v, powers %v)", of.reached(ownHQAlias), of.mounter.mounted(ownHQAlias), of.hq.powerLog())
			}
		})
	}

	t.Run("a start whose answer was lost", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.acquire(t, groupHQPool)
		ctx := context.Background()
		if _, err := LeasePark(ctx, of.deps, ownHQAlias); err != nil {
			t.Fatal(err)
		}
		of.hq.mu.Lock()
		of.hq.powerCut = true
		of.hq.mu.Unlock()
		_, _ = LeaseUnpark(ctx, of.deps, ownHQAlias)
		of.hq.mu.Lock()
		of.hq.powerCut = false
		of.hq.mu.Unlock()
		if v := hqView(t, of.deps); v.Unverified || v.State == LeaseStateLost {
			t.Errorf("status = %+v, want zcp's own running lease, verified", v)
		}
		of.forgetSSH()
		if v, err := LeaseUnpark(ctx, of.deps, ownHQAlias); err != nil || v.State == LeaseStateLost {
			t.Errorf("unpark again = %+v, %v; want it to finish the unpark", v, err)
		}
		if !of.mounter.mounted(ownHQAlias) || !of.reached(ownHQAlias) {
			t.Errorf("the second unpark did not mount it (%v) or make sure of its supervisor (%v)", of.mounter.mounted(ownHQAlias), of.reached(ownHQAlias))
		}
	})
}

// TestLeaseOwnership_Park_OnlyWhenOurs: a park records a lease parked only when
// the orchestrator gives the container to the lease: one another holder
// stopped is refused naming the release, nothing written; one zcp cannot
// verify is refused, nothing changed.
func TestLeaseOwnership_Park_OnlyWhenOurs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, of *ownFixture)
		want  string
	}{
		{name: "another holder stopped it", want: "zerops_lease action=release lease=" + ownHQAlias, setup: func(t *testing.T, of *ownFixture) {
			t.Helper()
			of.writeHQ(t, map[string]any{}, ownOtherLease, ownOtherSince, platform.SwarmContainerStopped)
		}},
		{name: "another holder took it seconds after the acquire", want: "zerops_lease action=release lease=" + ownHQAlias, setup: func(t *testing.T, of *ownFixture) {
			t.Helper()
			of.writeOtherSoon(t, map[string]any{}, "")
		}},
		{name: "not verifiable", want: "", setup: func(t *testing.T, of *ownFixture) {
			t.Helper()
			of.writeHQ(t, map[string]any{}, ownHQLease, ownLockSince, "")
			of.hqRenewFails(orchFailure())
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			tt.setup(t, of)
			wasMounted := of.mounter.mounted(ownHQAlias)

			_, err := LeasePark(context.Background(), of.deps, ownHQAlias)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want a refusal", err)
			}
			if !strings.Contains(pe.Suggestion, tt.want) {
				t.Errorf("suggestion %q lacks %q", pe.Suggestion, tt.want)
			}
			if rec := of.rawRecord(t, ownHQAlias); rec == nil || rec["parked"] != nil {
				t.Errorf("record = %v, want it as it was", rec)
			}
			if len(of.hq.powerLog()) != 0 || of.mounter.mounted(ownHQAlias) != wasMounted {
				t.Errorf("park changed something: powers %v, mounted %v (was %v)", of.hq.powerLog(), of.mounter.mounted(ownHQAlias), wasMounted)
			}
		})
	}
}

// endsOnUnmount is the fixture's mounter, running end once, as the first
// unmount goes: a lease that ends while a park unmounts it.
type endsOnUnmount struct {
	*lockedMounter
	once sync.Once
	end  func()
}

func (e *endsOnUnmount) Unmount(ctx context.Context, h, p string) error {
	e.once.Do(e.end)
	return e.lockedMounter.Unmount(ctx, h, p)
}

func (e *endsOnUnmount) CleanupUnit(ctx context.Context, h string) error {
	e.once.Do(e.end)
	return e.lockedMounter.CleanupUnit(ctx, h)
}

// TestLeaseOwnership_Park_LeaseEndsDuringTheUnmount_NeverStopped: a park asks
// whose the container is again right before its stop (DP-2, DP-28) — the
// unmount before it takes a while, and the orchestrator checks no lease on a
// free container — so a lease that ended meanwhile sends no stop: its record
// is dropped as lost, the result saying it was not parked; one the
// orchestrator then does not answer about is refused, the mount back.
func TestLeaseOwnership_Park_LeaseEndsDuringTheUnmount_NeverStopped(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		end  func(of *ownFixture)
		lost bool
	}{
		{name: "released outside zcp, free", lost: true, end: func(of *ownFixture) { of.hq.releaseOutside(ownHQContainer) }},
		{name: "another holder took it", lost: true, end: func(of *ownFixture) { of.hq.takeOver(ownHQContainer) }},
		{name: "the orchestrator stops answering", end: func(of *ownFixture) { of.hqRenewFails(orchFailure()) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			of.acquire(t, groupHQPool)
			of.deps.Host.Mounter = &endsOnUnmount{lockedMounter: of.mounter, end: func() { tt.end(of) }}

			v, err := LeasePark(context.Background(), of.deps, ownHQAlias)
			if got := of.hq.powerLog(); len(got) != 0 {
				t.Errorf("the park sent %v to a container that is no longer the lease's", got)
			}
			if tt.lost {
				if err != nil || v.State != LeaseStateLost || !strings.Contains(v.Note, "not parked") {
					t.Fatalf("park = %+v, %v; want lost, not parked", v, err)
				}
				if rec := of.rawRecord(t, ownHQAlias); rec != nil {
					t.Errorf("record = %v, want it dropped", rec)
				}
				return
			}
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("park = %+v, %v; want a refusal", v, err)
			}
			if rec := of.rawRecord(t, ownHQAlias); rec == nil || rec["parked"] != nil {
				t.Errorf("record = %v, want it as it was", rec)
			}
			if !of.mounter.mounted(ownHQAlias) {
				t.Errorf("the refused park left the lease unmounted")
			}
		})
	}
}

// TestLeaseOwnership_HeldByOther_DropsTheRecord: the orchestrator's
// held-by-other answer to any call carrying the lease is final — the record
// is dropped as lost with its alias stanza and mount, never retried "in a
// moment" — also in a branch's release refused for another lease's unsaved
// work, and in the stop that re-parks a lease a refused release started.
func TestLeaseOwnership_HeldByOther_DropsTheRecord(t *testing.T) {
	t.Parallel()

	assertDropped := func(t *testing.T, of *ownFixture) {
		t.Helper()
		if of.rawRecord(t, ownHQAlias) != nil || of.aliasStanza(ownHQAlias) || of.mounter.mounted(ownHQAlias) {
			t.Errorf("record %v, alias stanza %v, mount %v: want all gone", of.rawRecord(t, ownHQAlias), of.aliasStanza(ownHQAlias), of.mounter.mounted(ownHQAlias))
		}
	}

	t.Run("a branch release refused for another lease's work", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.acquire(t, groupHQPool, groupWebPool)
		if _, err := LeasePark(context.Background(), of.deps, ownHQAlias); err != nil {
			t.Fatal(err)
		}
		of.ssh.setHandler(func(host, command string) ([]byte, error) {
			switch {
			case strings.Contains(command, "status --porcelain") && host == ownWebAlias:
				return []byte("?? scratch.txt\n"), nil
			case strings.Contains(command, "rev-list --count"):
				return []byte("0\n"), nil
			}
			return nil, nil
		})
		of.routeHQ(takeOverFirst{poolOrch: of.hq, on: "start"})

		_, err := LeaseReleaseGroupChange(context.Background(), of.deps, "", "feat-x", false)
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || !strings.Contains(pe.Message, "scratch.txt") {
			t.Fatalf("err = %v, want the branch refused for webpool-7's work", err)
		}
		if !strings.Contains(pe.Message, ownHQAlias) || !strings.Contains(pe.Message, "another holder") {
			t.Errorf("refusal %q must say %s's record was dropped: another holder has it", pe.Message, ownHQAlias)
		}
		assertDropped(t, of)
		if of.rawRecord(t, ownWebAlias) == nil {
			t.Error("the refused member's record was dropped")
		}
	})

	t.Run("the stop that re-parks a refused release's lease", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.acquire(t, groupHQPool)
		if _, err := LeasePark(context.Background(), of.deps, ownHQAlias); err != nil {
			t.Fatal(err)
		}
		of.ssh.setHandler(func(_, command string) ([]byte, error) {
			switch {
			case strings.Contains(command, "status --porcelain"):
				return []byte("?? notes.txt\n"), nil
			case strings.Contains(command, "rev-list --count"):
				return []byte("0\n"), nil
			}
			return nil, nil
		})
		of.deps.SSHReadyEvery, of.deps.StartedSSHWait = time.Millisecond, 200*time.Millisecond
		of.routeHQ(takeOverFirst{poolOrch: of.hq, on: "stop"})

		_, err := LeaseReleaseChange(context.Background(), of.deps, ownHQAlias, false)
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || !strings.Contains(pe.Message, "notes.txt") {
			t.Fatalf("err = %v, want the release refused for notes.txt", err)
		}
		if !strings.Contains(pe.Message, "another holder") {
			t.Errorf("refusal %q must say the record was dropped: another holder has it", pe.Message)
		}
		assertDropped(t, of)
	})

	for _, tt := range []struct {
		name, on string
		run      func(context.Context, LeaseDeps) (LeaseView, error)
		parked   bool
		warm     bool
	}{
		{name: "a park's stop", on: "stop", run: func(ctx context.Context, d LeaseDeps) (LeaseView, error) { return LeasePark(ctx, d, ownHQAlias) }},
		{name: "an unpark's start", on: "start", parked: true, run: func(ctx context.Context, d LeaseDeps) (LeaseView, error) { return LeaseUnpark(ctx, d, ownHQAlias) }},
		{name: "a forced release", on: "release", run: func(ctx context.Context, d LeaseDeps) (LeaseView, error) {
			return LeaseRelease(ctx, d, ownHQAlias, true)
		}},
		{name: "the warm decision's delete", on: "delete", warm: true, run: func(ctx context.Context, d LeaseDeps) (LeaseView, error) {
			return LeaseRelease(ctx, d, ownHQAlias, true)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			of.acquire(t, groupHQPool)
			if tt.parked {
				if _, err := LeasePark(context.Background(), of.deps, ownHQAlias); err != nil {
					t.Fatal(err)
				}
			}
			if tt.warm {
				// hqpool-3 can be reset (it has its primary snapshot) and the
				// pool already has its warm container, so the release deletes.
				mine := of.hq.get(ownHQContainer)
				mine.Snapshots, mine.Current = 1, true
				of.hq.put(mine, of.hq.leaseOf(ownHQContainer))
				of.hq.put(platform.SwarmContainer{ID: "c-hqpool-4", Number: 4, Hostname: nodeHostnameOf(groupHQPool, 4), Status: platform.SwarmContainerActive, Current: true, Snapshots: 1}, "")
				of.deps.Warm = &LeaseWarm{Target: 1, Go: func(fn func()) { fn() }}
			}
			of.routeHQ(takeOverFirst{poolOrch: of.hq, on: tt.on})

			v, err := tt.run(context.Background(), of.deps)
			if err != nil {
				t.Fatalf("err = %v, want a lost lease", err)
			}
			if v.State != LeaseStateLost || !strings.Contains(v.Note, "another holder") || strings.Contains(v.Note, ownRemovedReason) {
				t.Errorf("view = %+v, want lost naming another holder, never a retry", v)
			}
			assertDropped(t, of)
			if got := of.hq.freeResetLog(); len(got) != 0 {
				t.Errorf("free containers reset: %v", got)
			}
		})
	}
}

// TestLeaseOwnership_Release_NotOurs_DropsTheRecordAndTouchesNothing: a
// release — plain or forced — of a lease whose container is gone, being
// deleted, free, or reserved by another holder drops the record as lost,
// saying why, and takes down the alias and the mount. It never runs the guard
// over ssh and never calls the orchestrator's release or delete: a release of
// a free container resets it whatever lease it carries (live, 2026-10-24).
func TestLeaseOwnership_Release_NotOurs_DropsTheRecordAndTouchesNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		end      func(o *poolOrch)
		wantNote string
	}{
		{name: "another holder reserved it", end: func(o *poolOrch) { o.takeOver(ownHQContainer) }, wantNote: "another holder"},
		{name: "released outside zcp, free", end: func(o *poolOrch) { o.releaseOutside(ownHQContainer) }, wantNote: "no longer reserves"},
		{name: "gone from the pool", end: func(o *poolOrch) { o.remove(ownHQContainer) }, wantNote: "gone"},
		{name: "being deleted", end: func(o *poolOrch) {
			c := o.get(ownHQContainer)
			c.LockState, c.Status = "deleting", "DELETING"
			o.put(c, o.leaseOf(ownHQContainer))
		}, wantNote: "deleting"},
	}
	for _, tt := range tests {
		for _, force := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, force=%v", tt.name, force), func(t *testing.T) {
				t.Parallel()
				of := newOwnFixture(t)
				of.acquire(t, groupHQPool)
				if !of.mounter.mounted(ownHQAlias) || !of.aliasStanza(ownHQAlias) {
					t.Fatalf("setup: %s is not mounted and aliased", ownHQAlias)
				}
				tt.end(of.hq)

				change, err := LeaseReleaseChange(context.Background(), of.deps, ownHQAlias, force)
				if err != nil {
					t.Fatalf("LeaseReleaseChange: %v", err)
				}
				v := change.Leases[0]
				if v.State != LeaseStateLost || !strings.Contains(v.Note, tt.wantNote) || !strings.Contains(v.Note, "record is dropped") {
					t.Errorf("view = %+v, want lost, saying why (%q) and that the record is dropped", v, tt.wantNote)
				}
				if strings.Contains(v.Note, ownRemovedReason) {
					t.Errorf("note %q tells the agent to retry", v.Note)
				}
				if of.reached(ownHQAlias) {
					t.Errorf("ssh reached %s, a container zcp no longer holds", ownHQAlias)
				}
				if got := of.hq.releaseLog(); len(got) != 0 {
					t.Errorf("orchestrator releases = %+v, want none (a free container is reset whatever the lease)", got)
				}
				if got := of.hq.deleteLog(); len(got) != 0 {
					t.Errorf("orchestrator deletes = %v, want none", got)
				}
				if got := of.hq.freePowerLog(); len(got) != 0 {
					t.Errorf("free containers stopped or started: %v", got)
				}
				if of.rawRecord(t, ownHQAlias) != nil || of.mounter.mounted(ownHQAlias) || of.aliasStanza(ownHQAlias) {
					t.Error("the record, mount or alias stanza is still there")
				}
			})
		}
	}
}

// TestLeaseOwnership_Release_Unverified_Refused: a release — plain or forced —
// of a lease the orchestrator does not answer about is refused with why:
// nothing is read over ssh, released, deleted or dropped.
func TestLeaseOwnership_Release_Unverified_Refused(t *testing.T) {
	t.Parallel()
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("force=%v", force), func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			of.acquire(t, groupHQPool)
			of.hqRenewFails(orchFailure())

			_, err := LeaseReleaseChange(context.Background(), of.deps, ownHQAlias, force)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || !strings.Contains(pe.Message, ownHQAlias) || !strings.Contains(pe.Message, "could not read its reservations") {
				t.Fatalf("err = %v, want a refusal naming the lease and the orchestrator's answer", err)
			}
			if of.reached(ownHQAlias) {
				t.Error("ssh reached a container zcp cannot verify as its own")
			}
			if len(of.hq.releaseLog()) != 0 || len(of.hq.deleteLog()) != 0 || len(of.hq.powerLog()) != 0 {
				t.Errorf("orchestrator mutated: releases %v deletes %v powers %v", of.hq.releaseLog(), of.hq.deleteLog(), of.hq.powerLog())
			}
			if of.rawRecord(t, ownHQAlias) == nil || !of.mounter.mounted(ownHQAlias) {
				t.Error("a refused release dropped the record or the mount")
			}
		})
	}
}

// TestLeaseOwnership_WarmDecision_NeverResetsOrDeletesNotOurs: the warm
// decision (DP-27) acts on the pool as it read it under the warm lock and on a
// renew right before its call: a container gone, free, or another holder's is
// neither reset nor deleted (the orchestrator checks no lease on a free
// container) and the record is dropped as lost; one the orchestrator does not
// answer about is left alone, the record kept.
func TestLeaseOwnership_WarmDecision_NeverResetsOrDeletesNotOurs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		mine     *platform.SwarmContainer
		lease    string
		renewErr error
		want     string // "" when the record is kept and an error returned
	}{
		{name: "gone from the pool", mine: nil, want: "gone"},
		{name: "free", mine: ptrTo(hqContainer("", "", "")), want: "no longer reserves"},
		{name: "another holder's, reserved seconds after the acquire", mine: ptrTo(hqContainer(leaseLockReserved, ownSoonSince, "")), lease: ownOtherLease, want: "another holder"},
		{name: "not answered", mine: ptrTo(hqContainer(leaseLockReserved, ownLockSince, "")), lease: ownHQLease, renewErr: orchFailure()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			of.writeRecord(t, groupHQPool, "svc-hq", 3, map[string]any{})
			if tt.mine != nil {
				of.hq.put(*tt.mine, tt.lease)
			}
			of.hqRenewFails(tt.renewErr)
			rec, err := loadLeaseRecord(of.stateDir, ownHQAlias)
			if err != nil {
				t.Fatal(err)
			}
			rp := &resolvedPool{svc: ptrTo(poolService("svc-hq", groupHQPool)), api: of.hq}
			resets := 0
			reset := func() (LeaseView, error) {
				resets++
				return dropLeaseRecord(of.stateDir, rec, LeaseStateReleased, "")
			}
			read := poolWarmRead{total: 1, mine: tt.mine, mineCounted: tt.mine != nil}

			v, kept, err := (&LeaseWarm{Target: 1}).decideRelease(context.Background(), of.deps, rp, rec, read, reset)
			if resets != 0 || len(of.hq.deleteLog()) != 0 {
				t.Errorf("resets %d, deletes %v: want neither for a container that is not this lease's", resets, of.hq.deleteLog())
			}
			if tt.want == "" {
				if err == nil || of.rawRecord(t, ownHQAlias) == nil {
					t.Errorf("err = %v, record %v: want an error and the record kept", err, of.rawRecord(t, ownHQAlias))
				}
				return
			}
			if err != nil {
				t.Fatalf("decideRelease: %v", err)
			}
			if kept || v.State != LeaseStateLost || !strings.Contains(v.Note, tt.want) {
				t.Errorf("view = %+v (kept %v), want lost naming %q", v, kept, tt.want)
			}
			if of.rawRecord(t, ownHQAlias) != nil {
				t.Error("the record survived")
			}
		})
	}
}

// freedWhileWaiting is a poolOrch whose container is released outside zcp
// right after zcp first asks whose it is — by id or by renew.
type freedWhileWaiting struct {
	*poolOrch
	once sync.Once
}

func (f *freedWhileWaiting) Container(ctx context.Context, id string) (platform.SwarmContainer, error) {
	c, err := f.poolOrch.Container(ctx, id)
	f.once.Do(func() { f.releaseOutside(id) })
	return c, err
}

func (f *freedWhileWaiting) Renew(ctx context.Context, id, lease string) (platform.SwarmContainer, error) {
	c, err := f.poolOrch.Renew(ctx, id, lease)
	f.once.Do(func() { f.releaseOutside(id) })
	return c, err
}

// TestLeaseOwnership_WarmLockBusy_NeverResetsAContainerFreedMeanwhile: a
// release that waited for the pool's warm lock in vain keeps its container
// with a reset (DP-27) only once the orchestrator still gives the container to
// the lease: one released outside zcp while the release waited is free, and a
// reset would take it from under the pool's next acquire.
func TestLeaseOwnership_WarmLockBusy_NeverResetsAContainerFreedMeanwhile(t *testing.T) {
	t.Parallel()
	of := newOwnFixture(t)
	of.acquire(t, groupHQPool)
	of.routeHQ(&freedWhileWaiting{poolOrch: of.hq})
	of.deps.Warm = &LeaseWarm{Target: 1, LockWait: 20 * time.Millisecond, Go: func(fn func()) { fn() }}
	unlock, err := lockPoolWarm(context.Background(), of.stateDir, groupHQPool, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	change, err := LeaseReleaseChange(context.Background(), of.deps, ownHQAlias, true)
	if err != nil {
		t.Fatalf("LeaseReleaseChange: %v", err)
	}
	if v := change.Leases[0]; v.State != LeaseStateLost || !strings.Contains(v.Note, "no longer reserves") {
		t.Errorf("view = %+v, want lost: the container was released outside zcp meanwhile", v)
	}
	if got := of.hq.freeResetLog(); len(got) != 0 {
		t.Errorf("free containers reset: %v", got)
	}
	if got := of.hq.releaseLog(); len(got) != 0 {
		t.Errorf("releases = %+v, want none", got)
	}
}

// TestLeaseOwnership_ReleaseBranch: a branch's release drops a member zcp no
// longer holds as lost — no guard over ssh, no orchestrator call — and
// releases the rest; a member the orchestrator does not answer about refuses
// the branch, nothing released and nothing read from it.
func TestLeaseOwnership_ReleaseBranch(t *testing.T) {
	t.Parallel()

	t.Run("a member another holder took is dropped, the rest released", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.acquire(t, groupHQPool, groupWebPool)
		of.web.takeOver(ownWebContainer)

		change, err := LeaseReleaseGroupChange(context.Background(), of.deps, "", "feat-x", false)
		if err != nil {
			t.Fatalf("LeaseReleaseGroupChange: %v", err)
		}
		states := map[string]LeaseView{}
		for _, v := range change.Leases {
			states[v.Alias] = v
		}
		if v := states[ownHQAlias]; v.State != LeaseStateReleased {
			t.Errorf("%s = %+v, want released", ownHQAlias, v)
		}
		if v := states[ownWebAlias]; v.State != LeaseStateLost || !strings.Contains(v.Note, "another holder") || v.Error != "" {
			t.Errorf("%s = %+v, want lost naming another holder, no error", ownWebAlias, v)
		}
		if of.reached(ownWebAlias) {
			t.Error("ssh reached the other holder's container")
		}
		if got := of.web.releaseLog(); len(got) != 0 {
			t.Errorf("webpool releases = %+v, want none", got)
		}
		if c := of.web.get(ownWebContainer); c.LockState != leaseLockReserved || of.web.leaseOf(ownWebContainer) == ownLeaseOfWeb {
			t.Errorf("the other holder's container = %+v, want its reservation as it was", c)
		}
		if of.mounter.mounted(ownWebAlias) || of.aliasStanza(ownWebAlias) || of.rawRecord(t, ownWebAlias) != nil || len(of.records(t)) != 0 {
			t.Errorf("records left %v, or %s's mount or alias stanza survived", of.records(t), ownWebAlias)
		}
	})

	t.Run("a member the orchestrator does not answer about refuses the branch", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.acquire(t, groupHQPool, groupWebPool)
		of.hqRenewFails(orchFailure())

		_, err := LeaseReleaseGroupChange(context.Background(), of.deps, "", "feat-x", false)
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing || !strings.Contains(pe.Message, ownHQAlias) {
			t.Fatalf("err = %v, want a refusal naming %s", err, ownHQAlias)
		}
		if of.reached(ownHQAlias) {
			t.Error("ssh reached a container zcp cannot verify as its own")
		}
		if len(of.web.releaseLog()) != 0 || len(of.hq.releaseLog()) != 0 || len(of.records(t)) != 2 {
			t.Errorf("a refused branch released something: web %v hq %v, records %d", of.web.releaseLog(), of.hq.releaseLog(), len(of.records(t)))
		}
	})
}

// TestLeaseOwnership_Park_NotOurs_RefusedNothingChanged: a park or an unpark
// of a lease whose container another holder reserves is refused naming the
// release that drops its record — nothing stopped, started or unmounted.
func TestLeaseOwnership_Park_NotOurs_RefusedNothingChanged(t *testing.T) {
	t.Parallel()
	for _, step := range []struct {
		name string
		fn   func(context.Context, LeaseDeps, string) (LeaseView, error)
	}{{"park", LeasePark}, {"unpark", LeaseUnpark}} {
		t.Run(step.name, func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			of.acquire(t, groupHQPool)
			of.hq.takeOver(ownHQContainer)

			_, err := step.fn(context.Background(), of.deps, ownHQAlias)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
				t.Fatalf("err = %v, want a PREREQUISITE_MISSING refusal", err)
			}
			if !strings.Contains(pe.Message, "another holder") || !strings.Contains(pe.Suggestion, "zerops_lease action=release lease="+ownHQAlias) {
				t.Errorf("refusal = %q / %q, want another holder named and the release", pe.Message, pe.Suggestion)
			}
			if got := of.hq.powerLog(); len(got) != 0 {
				t.Errorf("powers = %v, want none", got)
			}
			if !of.mounter.mounted(ownHQAlias) || of.rawRecord(t, ownHQAlias) == nil {
				t.Error("a refused park unmounted the lease or dropped its record")
			}
		})
	}
}

// TestLeaseOwnership_Sync_LeasesNoLongerOursAreNeverReached: a sync from a
// lease zcp no longer holds is refused naming the release, nothing sent; a
// member zcp no longer holds is "lost" and one the orchestrator does not
// answer about "unverified", neither fetched nor fast-forwarded over ssh.
func TestLeaseOwnership_Sync_LeasesNoLongerOursAreNeverReached(t *testing.T) {
	t.Parallel()

	t.Run("the source", func(t *testing.T) {
		t.Parallel()
		gf, remote := newSyncFixture(t, syncBranch)
		gf.hq.releaseOutside("c-hqpool")

		_, err := LeaseSync(context.Background(), gf.deps, ownHQAlias)
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing || !strings.Contains(pe.Suggestion, "zerops_lease action=release lease="+ownHQAlias) {
			t.Fatalf("err = %v, want a refusal naming the release", err)
		}
		if got := remote.all(); len(got) != 0 {
			t.Errorf("ssh sent %v, want nothing", got)
		}
	})

	for _, tt := range []struct {
		name, want string
		end        func(o *poolOrch)
	}{
		{name: "a member released outside zcp", want: LeaseSyncLost, end: func(o *poolOrch) { o.releaseOutside("c-webpool") }},
		{name: "a member another holder took seconds after its acquire", want: LeaseSyncLost, end: func(o *poolOrch) {
			c := o.get("c-webpool")
			c.LockSince = "2026-10-24T10:00:10Z"
			o.put(c, ownOtherLease)
		}},
		{name: "a member the orchestrator does not answer about", want: LeaseSyncUnverified, end: func(o *poolOrch) {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.renewErr = orchFailure()
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gf, remote := newSyncFixture(t, syncBranch)
			tt.end(gf.web)

			v, err := LeaseSync(context.Background(), gf.deps, ownHQAlias)
			if err != nil {
				t.Fatalf("LeaseSync: %v", err)
			}
			if m := syncMemberOf(t, v, ownWebAlias); m.Outcome != tt.want || m.Note == "" {
				t.Errorf("member = %+v, want outcome %q with a note", m, tt.want)
			}
			if got := remote.sent(ownWebAlias); len(got) != 0 {
				t.Errorf("ssh sent %s %v, want nothing", ownWebAlias, got)
			}
		})
	}
}

// TestLeaseOwnership_Acquire_ReuseCheck: an acquire of a key whose record
// names a container another holder reserves is refused naming the release
// that drops it (lost), and one the orchestrator does not answer about is
// refused too — never returned reused.
func TestLeaseOwnership_Acquire_ReuseCheck(t *testing.T) {
	t.Parallel()

	t.Run("another holder's, reserved seconds after the acquire", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.writeOtherSoon(t, map[string]any{}, "")

		_, err := LeaseAcquire(context.Background(), of.deps, groupHQPool, testRepo, "feat-x")
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing || !strings.Contains(pe.Suggestion, "zerops_lease action=release lease="+ownHQAlias) {
			t.Fatalf("err = %v, want a refusal naming the release", err)
		}
		if of.hq.acquireCount() != 0 || of.reached(ownHQAlias) {
			t.Errorf("acquires = %d, ssh reached %v: want neither", of.hq.acquireCount(), of.reached(ownHQAlias))
		}
	})

	t.Run("not answered", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.acquire(t, groupHQPool)
		of.hqRenewFails(orchFailure())

		v, err := LeaseAcquire(context.Background(), of.deps, groupHQPool, testRepo, "feat-x")
		if err == nil {
			t.Fatalf("acquire = %+v, want a refusal: zcp cannot tell whether the lease is still its own", v)
		}
		if of.hq.acquireCount() != 1 || of.reached(ownHQAlias) {
			t.Errorf("acquires = %d, ssh reached %v: want only the first acquire, nothing read", of.hq.acquireCount(), of.reached(ownHQAlias))
		}
	})
}

// TestLeaseOwnership_Rewire_SiblingNoLongerOursIsNeverReached: the rewire of
// a release hands the overlay only to siblings zcp still holds; one another
// holder took — seconds after its acquire — is named with the release that
// drops it, nothing sent to it.
func TestLeaseOwnership_Rewire_SiblingNoLongerOursIsNeverReached(t *testing.T) {
	t.Parallel()
	of := newOwnFixture(t)
	of.acquire(t, groupHQPool)
	of.writeRecord(t, groupWebPool, "svc-web", 7, map[string]any{})
	of.web.put(platform.SwarmContainer{ID: ownWebContainer, Number: 7, Hostname: nodeHostnameOf(groupWebPool, 7), Status: platform.SwarmContainerActive, LockState: leaseLockReserved, LockSince: ownSoonSince}, ownOtherLease)
	of.forgetSSH()

	change, err := LeaseReleaseChange(context.Background(), of.deps, ownHQAlias, true)
	if err != nil {
		t.Fatalf("LeaseReleaseChange: %v", err)
	}
	if of.reached(ownWebAlias) {
		t.Error("the rewire reached the other holder's container")
	}
	i := slices.IndexFunc(change.Rewired, func(r LeaseRewire) bool { return r.Alias == ownWebAlias })
	if i < 0 || !strings.Contains(change.Rewired[i].Note, "zerops_lease action=release lease="+ownWebAlias) {
		t.Errorf("rewired = %+v, want %s named with the release that drops it", change.Rewired, ownWebAlias)
	}
	if of.rawRecord(t, ownWebAlias) == nil {
		t.Errorf("%s's record was dropped by another lease's release", ownWebAlias)
	}
}

// TestLeaseOwnership_FullPool_NamesNoLeaseAnotherHolderTook: a full pool's
// refusal counts as zcp's own only the leases the orchestrator gives it, and
// reads none it does not over ssh.
func TestLeaseOwnership_FullPool_NamesNoLeaseAnotherHolderTook(t *testing.T) {
	t.Parallel()
	of := newOwnFixture(t)
	of.writeOtherSoon(t, map[string]any{}, "")
	of.hq.mu.Lock()
	of.hq.acquireErr = &platform.SwarmError{Status: 400, Code: "containerAction", Reason: platform.SwarmReasonAllReserved, Message: "all containers are reserved"}
	of.hq.mu.Unlock()

	_, err := LeaseAcquire(context.Background(), of.deps, groupHQPool, testRepo, "feat-y")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want the full pool's refusal", err)
	}
	if strings.Contains(pe.Message+pe.Suggestion, "release lease="+ownHQAlias) {
		t.Errorf("refusal %q / %q names the release of a lease another holder took as zcp's own", pe.Message, pe.Suggestion)
	}
	if of.reached(ownHQAlias) {
		t.Error("the refusal read the other holder's container over ssh")
	}
}
