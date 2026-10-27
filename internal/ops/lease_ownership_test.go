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
		Run: func(ctx context.Context, name string, args ...string) error {
			runMu.Lock()
			defer runMu.Unlock()
			return fakeAliasSudo(ctx, name, args...)
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
// with fields replacing or adding to its keys (raw JSON, as any zcp wrote it).
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

// writeOtherSoon is writeHQ for a record whose container another consumer
// reserved 20 s after the acquire.
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
			record: map[string]any{}, container: ptrTo(hqContainer(leaseLockReserved, ownOtherSince, "")),
			lease: ownHQLease, wantState: LeaseStateHeld,
		},
		{
			name:   "renewed and stopped: parked, whatever its lockSince",
			record: map[string]any{"parked": true, "parkedAt": "2026-10-24T11:01:30Z"}, container: ptrTo(hqContainer(leaseLockReserved, ownOtherSince, platform.SwarmContainerStopped)),
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
// reads it parked. Its record does not say parked, so a plain release's guard
// reaches for it over ssh, which a stopped container does not answer (255):
// asked again, the orchestrator still gives it the lease, so the release is
// refused as unchecked and the record kept; a forced release goes to the
// orchestrator with the lease. The record is never dropped while the lease
// still reserves the container.
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
	of.ssh.setHandler(func(host, _ string) ([]byte, error) {
		if host == ownHQAlias && of.hq.get(ownHQContainer).Status == platform.SwarmContainerStopped {
			return nil, &platform.SSHExecError{Hostname: host, Output: "ssh: connect to host " + host + " port 22: No route to host", Err: exitError(t, 255)}
		}
		return nil, nil
	})

	change, err := LeaseReleaseChange(context.Background(), of.deps, ownHQAlias, false)
	if err == nil {
		t.Fatalf("release = %+v, want it refused: the stopped container could not be checked and is still zcp's", change.Leases)
	}
	if of.rawRecord(t, ownHQAlias) == nil || len(of.hq.releaseLog()) != 0 {
		t.Fatalf("record %v, releases %+v: want the record kept and nothing released", of.rawRecord(t, ownHQAlias), of.hq.releaseLog())
	}
	v, err := LeaseRelease(context.Background(), of.deps, ownHQAlias, true)
	if err != nil || v.State != LeaseStateReleased {
		t.Fatalf("forced release = %+v, %v; want it released", v, err)
	}
	if got := of.hq.releaseLog(); len(got) != 1 || got[0].lease != lease {
		t.Errorf("releases = %+v, want the one release with zcp's lease", got)
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

// hookOrch is a poolOrch that runs before(call) ahead of each call zcp sends
// it — "renew", "stop", "start", "release", "delete" — and after(call) once
// one went through.
type hookOrch struct {
	*poolOrch
	before, after func(call string)
}

func (h hookOrch) hook(f func(string), call string) {
	if f != nil {
		f(call)
	}
}

func (h hookOrch) Renew(ctx context.Context, id, lease string) (platform.SwarmContainer, error) {
	h.hook(h.before, "renew")
	c, err := h.poolOrch.Renew(ctx, id, lease)
	if err == nil {
		h.hook(h.after, "renew")
	}
	return c, err
}

func (h hookOrch) Stop(ctx context.Context, id, lease string) (platform.SwarmContainer, error) {
	h.hook(h.before, "stop")
	c, err := h.poolOrch.Stop(ctx, id, lease)
	if err == nil {
		h.hook(h.after, "stop")
	}
	return c, err
}

func (h hookOrch) Start(ctx context.Context, id, lease string) (platform.SwarmContainer, error) {
	h.hook(h.before, "start")
	c, err := h.poolOrch.Start(ctx, id, lease)
	if err == nil {
		h.hook(h.after, "start")
	}
	return c, err
}

func (h hookOrch) Release(ctx context.Context, id, lease string, reset bool) (platform.SwarmContainer, error) {
	h.hook(h.before, "release")
	c, err := h.poolOrch.Release(ctx, id, lease, reset)
	if err == nil {
		h.hook(h.after, "release")
	}
	return c, err
}

func (h hookOrch) Delete(ctx context.Context, id, lease string) error {
	h.hook(h.before, "delete")
	err := h.poolOrch.Delete(ctx, id, lease)
	if err == nil {
		h.hook(h.after, "delete")
	}
	return err
}

// ownNewLease is the lease another zcp process took hqpool-3 again with.
const ownNewLease = "hqpool-lease-3-of-another-zcp-process"

// reacquireHQ is another zcp process sharing the state directory (an older
// zcp, maybe): it releases hqpool-3 — zcp's lease is its lease too — and
// acquires the container again, which the pool kept warm (DP-27), writing its
// own record (of another branch), ssh stanza and mount under the same alias.
func (of *ownFixture) reacquireHQ(t *testing.T) {
	t.Helper()
	of.reacquireHQOn(t, "feat-b")
}

// reacquireHQOn is reacquireHQ for a lease of branch.
func (of *ownFixture) reacquireHQOn(t *testing.T, branch string) {
	t.Helper()
	c := of.hq.get(ownHQContainer)
	c.LockState, c.Status = leaseLockReserved, platform.SwarmContainerActive
	of.hq.put(c, ownNewLease)
	rec, err := loadLeaseRecord(of.stateDir, ownHQAlias)
	if err != nil {
		t.Errorf("the other process reads the record: %v", err)
		return
	}
	rec.Lease, rec.Branch, rec.Parked, rec.ParkedAt = ownNewLease, branch, false, ""
	if err := writeLeaseRecord(of.stateDir, rec); err != nil {
		t.Errorf("the other process writes its record: %v", err)
	}
	of.mounter.mu.Lock()
	of.mounter.m.states["/var/www/"+ownHQAlias] = platform.MountStateActive
	of.mounter.mu.Unlock()
	if r := of.deps.Host.refreshAliases(context.Background(), of.deps); r != "" {
		t.Errorf("the other process's alias: %s", r)
	}
}

// unmountWatch is the fixture's mounter, noting every unmount once armed —
// once another lease's mount is in place at the alias.
type unmountWatch struct {
	*lockedMounter
	mu    sync.Mutex
	armed bool
	hits  []string
}

func (w *unmountWatch) arm() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.armed = true
}

func (w *unmountWatch) note(what string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.armed {
		w.hits = append(w.hits, what)
	}
}

func (w *unmountWatch) seen() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.hits...)
}

func (w *unmountWatch) Unmount(ctx context.Context, h, p string) error {
	w.note("unmount " + p)
	return w.lockedMounter.Unmount(ctx, h, p)
}

func (w *unmountWatch) ForceUnmount(ctx context.Context, h, p string) error {
	w.note("force unmount " + p)
	return w.lockedMounter.ForceUnmount(ctx, h, p)
}

func (w *unmountWatch) CleanupUnit(ctx context.Context, h string) error {
	w.note("unit remove " + h)
	return w.lockedMounter.CleanupUnit(ctx, h)
}

// TestLeaseOwnership_AliasReused_AnotherLeasesRecordLeftAlone: a record is
// dropped only while it is still the lease's own (DP-2). Another zcp process
// sharing the state directory can release hqpool-3 and take the container
// again, kept warm, under the same alias while this one parks or releases the
// old lease: the old lease is lost (or released), and the new lease's record,
// ssh stanza and mount stay as the other process left them — its mount is
// never unmounted, whenever it came — the result saying the alias now names
// another lease.
func TestLeaseOwnership_AliasReused_AnotherLeasesRecordLeftAlone(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		// When, in the calls zcp sends, the other process takes the container
		// again: before the renewN-th renew, before beforeCall, once
		// afterCall went through, or while the release's guard reads it.
		renewN     int
		beforeCall string
		afterCall  string
		guard      bool
		run        func(context.Context, LeaseDeps) (LeaseView, error)
		want       string
	}{
		{name: "a park, taken again right after it asked", afterCall: "renew", want: LeaseStateLost,
			run: func(ctx context.Context, d LeaseDeps) (LeaseView, error) { return LeasePark(ctx, d, ownHQAlias) }},
		{name: "a release, taken again while its guard reads", guard: true, want: LeaseStateLost,
			run: func(ctx context.Context, d LeaseDeps) (LeaseView, error) {
				return LeaseRelease(ctx, d, ownHQAlias, false)
			}},
		{name: "a park asking again before its stop", renewN: 2, want: LeaseStateLost,
			run: func(ctx context.Context, d LeaseDeps) (LeaseView, error) { return LeasePark(ctx, d, ownHQAlias) }},
		{name: "a park whose stop is refused held-by-other", beforeCall: "stop", want: LeaseStateLost,
			run: func(ctx context.Context, d LeaseDeps) (LeaseView, error) { return LeasePark(ctx, d, ownHQAlias) }},
		{name: "a forced release asking again before the release", renewN: 2, want: LeaseStateLost,
			run: func(ctx context.Context, d LeaseDeps) (LeaseView, error) {
				return LeaseRelease(ctx, d, ownHQAlias, true)
			}},
		{name: "a forced release that went, taken again before the record goes", afterCall: "release", want: LeaseStateReleased,
			run: func(ctx context.Context, d LeaseDeps) (LeaseView, error) {
				return LeaseRelease(ctx, d, ownHQAlias, true)
			}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			of.acquire(t, groupHQPool)
			watch := &unmountWatch{lockedMounter: of.mounter}
			of.deps.Host.Mounter = watch
			var mu sync.Mutex
			renews, taken := 0, false
			take := func() {
				mu.Lock()
				defer mu.Unlock()
				if !taken {
					taken = true
					of.reacquireHQ(t)
					watch.arm()
				}
			}
			of.ssh.setHandler(func(host, command string) ([]byte, error) {
				switch {
				case tt.guard && host == ownHQAlias && strings.Contains(command, "status --porcelain"):
					take()
				case strings.Contains(command, "rev-list --count"):
					return []byte("0\n"), nil
				}
				return nil, nil
			})
			of.routeHQ(hookOrch{poolOrch: of.hq,
				before: func(call string) {
					mu.Lock()
					if call == "renew" {
						renews++
					}
					now := (call == "renew" && renews == tt.renewN) || (tt.beforeCall != "" && call == tt.beforeCall)
					mu.Unlock()
					if now {
						take()
					}
				},
				after: func(call string) {
					if call == tt.afterCall {
						take()
					}
				},
			})

			v, err := tt.run(context.Background(), of.deps)
			if err != nil {
				t.Fatalf("err = %v, want the old lease %s", err, tt.want)
			}
			if !taken {
				t.Fatal("the other process never took the container again")
			}
			if v.State != tt.want || !strings.Contains(v.Note, "another lease") {
				t.Errorf("view = %+v, want %s, saying the alias now names another lease", v, tt.want)
			}
			if rec := of.rawRecord(t, ownHQAlias); rec == nil || rec["lease"] != ownNewLease {
				t.Errorf("record = %v, want the other process's, untouched", rec)
			}
			if !of.mounter.mounted(ownHQAlias) || !of.aliasStanza(ownHQAlias) {
				t.Errorf("mount %v, ssh stanza %v: want the other process's left in place", of.mounter.mounted(ownHQAlias), of.aliasStanza(ownHQAlias))
			}
			if got := watch.seen(); len(got) != 0 {
				t.Errorf("zcp took down the other process's mount: %v", got)
			}
			if of.hq.leaseOf(ownHQContainer) != ownNewLease {
				t.Errorf("the container's lease = %q, want the other process's", of.hq.leaseOf(ownHQContainer))
			}
		})
	}
}

// laggingSearch is a platform client whose service search does not list the
// pool yet (the search lags writes, §3.5) and whose read by id fails with
// byID.
type laggingSearch struct {
	platform.Client
	byID error
}

func (l laggingSearch) ListServices(ctx context.Context, projectID string) ([]platform.ServiceStack, error) {
	all, err := l.Client.ListServices(ctx, projectID)
	// A copy: the mock answers its own slice.
	return slices.DeleteFunc(slices.Clone(all), func(s platform.ServiceStack) bool { return s.Name == groupHQPool }), err
}

func (l laggingSearch) GetService(context.Context, string) (*platform.ServiceStack, error) {
	return nil, l.byID
}

// TestLeaseOwnership_LaggingSearch_ByIDReadFails_RefusalCarriesItsError: a
// pool the search misses is read by id before it counts as gone (DP-20); a
// read by id that fails for any other reason leaves the lease unverified, and
// the refusal carries that read's own failure — never the search's "no such
// service", which would say the pool is gone.
func TestLeaseOwnership_LaggingSearch_ByIDReadFails_RefusalCarriesItsError(t *testing.T) {
	t.Parallel()
	of := newOwnFixture(t)
	of.acquire(t, groupHQPool)
	of.deps.Client = laggingSearch{Client: of.deps.Client, byID: platform.NewPlatformError(platform.ErrNetworkError, "dial tcp api.zerops.io: i/o timeout", "")}

	_, err := LeaseRelease(context.Background(), of.deps, ownHQAlias, true)
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if pe.Code == platform.ErrServiceNotFound || !strings.Contains(pe.Message, "i/o timeout") {
		t.Errorf("refusal = %s: %s; want the read by id's own failure, never the search's", pe.Code, pe.Message)
	}
	if of.rawRecord(t, ownHQAlias) == nil || len(of.hq.releaseLog()) != 0 {
		t.Errorf("record %v, releases %v: want the record kept and nothing released", of.rawRecord(t, ownHQAlias), of.hq.releaseLog())
	}
}

// ownLeaseRefused is a poolOrch that answers zcp's call of kind on
// held-by-other although zcp's lease still holds the container: what the
// orchestrator answers its holder during a stop or start is not known, and
// held-by-other is live-verified on a release only.
type ownLeaseRefused struct {
	*poolOrch
	on string // "stop", "start" or "delete"
}

func (o ownLeaseRefused) Stop(ctx context.Context, id, lease string) (platform.SwarmContainer, error) {
	if o.on == "stop" {
		return platform.SwarmContainer{}, heldByOther()
	}
	return o.poolOrch.Stop(ctx, id, lease)
}

func (o ownLeaseRefused) Start(ctx context.Context, id, lease string) (platform.SwarmContainer, error) {
	if o.on == "start" {
		return platform.SwarmContainer{}, heldByOther()
	}
	return o.poolOrch.Start(ctx, id, lease)
}

func (o ownLeaseRefused) Delete(ctx context.Context, id, lease string) error {
	if o.on == "delete" {
		return heldByOther()
	}
	return o.poolOrch.Delete(ctx, id, lease)
}

// TestLeaseOwnership_HeldByOtherForItsOwnLease_KeptUnlessTheRenewAgrees: a
// held-by-other answer to a stop, start or delete drops the record only once
// a renew of the lease says it ended too (DP-2): while the orchestrator still
// gives the container to the lease, the call is refused as any other
// orchestrator failure and the record stays — a delete falls back to the
// reset release (DP-27).
func TestLeaseOwnership_HeldByOtherForItsOwnLease_KeptUnlessTheRenewAgrees(t *testing.T) {
	t.Parallel()
	unsaved := func(_, command string) ([]byte, error) {
		switch {
		case strings.Contains(command, "status --porcelain"):
			return []byte("?? notes.txt\n"), nil
		case strings.Contains(command, "rev-list --count"):
			return []byte("0\n"), nil
		}
		return nil, nil
	}
	for _, tt := range []struct {
		name, on   string
		parked     bool
		warm       bool
		ssh        func(host, command string) ([]byte, error)
		run        func(context.Context, LeaseDeps) (LeaseView, error)
		wantErr    bool
		wantParked bool
	}{
		{name: "a park's stop", on: "stop", wantErr: true,
			run: func(ctx context.Context, d LeaseDeps) (LeaseView, error) { return LeasePark(ctx, d, ownHQAlias) }},
		{name: "an unpark's start", on: "start", parked: true, wantErr: true, wantParked: true,
			run: func(ctx context.Context, d LeaseDeps) (LeaseView, error) { return LeaseUnpark(ctx, d, ownHQAlias) }},
		{name: "a release's start of a parked lease for its guard", on: "start", parked: true, wantErr: true, wantParked: true,
			run: func(ctx context.Context, d LeaseDeps) (LeaseView, error) {
				return LeaseRelease(ctx, d, ownHQAlias, false)
			}},
		{name: "the stop that re-parks a refused release's lease", on: "stop", parked: true, ssh: unsaved, wantErr: true,
			run: func(ctx context.Context, d LeaseDeps) (LeaseView, error) {
				return LeaseRelease(ctx, d, ownHQAlias, false)
			}},
		{name: "the warm decision's delete", on: "delete", warm: true,
			run: func(ctx context.Context, d LeaseDeps) (LeaseView, error) {
				return LeaseRelease(ctx, d, ownHQAlias, true)
			}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			of.acquire(t, groupHQPool)
			lease := of.hq.leaseOf(ownHQContainer)
			if tt.parked {
				if _, err := LeasePark(context.Background(), of.deps, ownHQAlias); err != nil {
					t.Fatal(err)
				}
			}
			if tt.ssh != nil {
				of.ssh.setHandler(tt.ssh)
			}
			of.deps.SSHReadyEvery, of.deps.StartedSSHWait = time.Millisecond, 200*time.Millisecond
			if tt.warm {
				mine := of.hq.get(ownHQContainer)
				mine.Snapshots, mine.Current = 1, true
				of.hq.put(mine, lease)
				of.hq.put(platform.SwarmContainer{ID: "c-hqpool-4", Number: 4, Hostname: nodeHostnameOf(groupHQPool, 4), Status: platform.SwarmContainerActive, Current: true, Snapshots: 1}, "")
				of.deps.Warm = &LeaseWarm{Target: 1, Go: func(fn func()) { fn() }}
			}
			of.routeHQ(ownLeaseRefused{poolOrch: of.hq, on: tt.on})

			v, err := tt.run(context.Background(), of.deps)
			if v.State == LeaseStateLost {
				t.Fatalf("view = %+v: a held-by-other the renew does not confirm dropped zcp's own lease", v)
			}
			if of.hq.leaseOf(ownHQContainer) == lease && of.rawRecord(t, ownHQAlias) == nil {
				t.Fatal("the record is gone while zcp's lease still reserves the container")
			}
			if tt.wantErr {
				if err == nil {
					t.Fatalf("view = %+v, want the call refused", v)
				}
				if rec := of.rawRecord(t, ownHQAlias); (rec["parked"] == true) != tt.wantParked {
					t.Errorf("record = %v, want parked %v", rec, tt.wantParked)
				}
				return
			}
			if err != nil || v.State != LeaseStateReleased {
				t.Fatalf("release = %+v, %v; want it released with the reset instead", v, err)
			}
			if got := of.hq.releaseLog(); len(got) != 1 || got[0].lease != lease {
				t.Errorf("releases = %+v, want the reset release with zcp's lease", got)
			}
		})
	}
}

// TestLeaseOwnership_ParkedWake_LeaseEndsDuringTheSSHWait_GuardNeverReads: a
// release starts a parked lease's container and waits for its ssh before the
// guard reads it (DP-28) — up to a minute — so it asks whose the container is
// again right before the guard (DP-2): a lease that ended meanwhile is
// dropped as lost, its container (another holder's now) never read, stopped
// or released.
func TestLeaseOwnership_ParkedWake_LeaseEndsDuringTheSSHWait_GuardNeverReads(t *testing.T) {
	t.Parallel()
	for _, branch := range []bool{false, true} {
		t.Run(fmt.Sprintf("branch=%v", branch), func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			pools := []string{groupHQPool}
			if branch {
				pools = append(pools, groupWebPool)
			}
			of.acquire(t, pools...)
			if _, err := LeasePark(context.Background(), of.deps, ownHQAlias); err != nil {
				t.Fatal(err)
			}
			of.deps.SSHReadyEvery, of.deps.StartedSSHWait = time.Millisecond, 200*time.Millisecond
			var once sync.Once
			of.ssh.setHandler(func(host, command string) ([]byte, error) {
				if host == ownHQAlias && command == "true" {
					once.Do(func() { of.hq.takeOver(ownHQContainer) })
				}
				if strings.Contains(command, "rev-list --count") {
					return []byte("0\n"), nil
				}
				return nil, nil
			})
			before := len(of.hq.powerLog())
			of.forgetSSH()

			var views []LeaseView
			var err error
			if branch {
				var change LeaseChange
				change, err = LeaseReleaseGroupChange(context.Background(), of.deps, "", "feat-x", false)
				views = change.Leases
			} else {
				var v LeaseView
				v, err = LeaseRelease(context.Background(), of.deps, ownHQAlias, false)
				views = []LeaseView{v}
			}
			if err != nil {
				t.Fatalf("release: %v", err)
			}
			for _, v := range views {
				if v.Alias == ownHQAlias && v.State != LeaseStateLost {
					t.Errorf("%s = %+v, want lost", ownHQAlias, v)
				}
			}
			of.ssh.mu.Lock()
			for _, c := range of.ssh.calls {
				if c.hostname == ownHQAlias && strings.Contains(c.command, "status --porcelain") {
					t.Errorf("the guard read %s, another holder's container now: %q", ownHQAlias, c.command)
				}
			}
			of.ssh.mu.Unlock()
			if got := of.hq.releaseLog(); len(got) != 0 {
				t.Errorf("releases = %+v, want none", got)
			}
			if got := of.hq.powerLog()[before:]; len(got) != 1 {
				t.Errorf("powers = %v, want only the wake's start", got)
			}
			if of.rawRecord(t, ownHQAlias) != nil {
				t.Errorf("record survives: %v", of.rawRecord(t, ownHQAlias))
			}
		})
	}
}

// TestLeaseOwnership_DroppedOutsideARelease_NamesTheSiblingsStillWired: a
// lease dropped as lost where no release rewires its branch — a refused
// branch release, the stop that re-parks a refused release's lease — names
// the branch's other leases that may still run wired to its container, and
// the status that names the restart (DP-26), as a park's or an unpark's drop
// does.
func TestLeaseOwnership_DroppedOutsideARelease_NamesTheSiblingsStillWired(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		on      string // the call another holder takes the container over before
		unsaved string // the alias holding work the release refuses for
		file    string
		run     func(context.Context, LeaseDeps) error
	}{
		{name: "a branch release refused for another lease's work", on: "start", unsaved: ownWebAlias, file: "scratch.txt",
			run: func(ctx context.Context, d LeaseDeps) error {
				_, err := LeaseReleaseGroupChange(ctx, d, "", "feat-x", false)
				return err
			}},
		{name: "the stop that re-parks a refused release's lease", on: "stop", unsaved: ownHQAlias, file: "notes.txt",
			run: func(ctx context.Context, d LeaseDeps) error {
				_, err := LeaseReleaseChange(ctx, d, ownHQAlias, false)
				return err
			}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			of.acquire(t, groupHQPool, groupWebPool)
			if _, err := LeasePark(context.Background(), of.deps, ownHQAlias); err != nil {
				t.Fatal(err)
			}
			of.ssh.setHandler(func(host, command string) ([]byte, error) {
				switch {
				case strings.Contains(command, "status --porcelain") && host == tt.unsaved:
					return []byte("?? " + tt.file + "\n"), nil
				case strings.Contains(command, "rev-list --count"):
					return []byte("0\n"), nil
				}
				return nil, nil
			})
			of.deps.SSHReadyEvery, of.deps.StartedSSHWait = time.Millisecond, 200*time.Millisecond
			of.routeHQ(takeOverFirst{poolOrch: of.hq, on: tt.on})

			err := tt.run(context.Background(), of.deps)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || !strings.Contains(pe.Message, tt.file) {
				t.Fatalf("err = %v, want the release refused for %s", err, tt.file)
			}
			if of.rawRecord(t, ownHQAlias) != nil {
				t.Fatalf("%s's record survives another holder's reservation", ownHQAlias)
			}
			if !strings.Contains(pe.Message, "may still run wired") || !strings.Contains(pe.Message, ownWebAlias) {
				t.Errorf("refusal %q must name %s, which may still run wired to %s's container", pe.Message, ownWebAlias, ownHQAlias)
			}
		})
	}
}

// takeOverThenRenewFails is a poolOrch on which another holder takes the
// container over right before zcp's stop reaches it — refused held-by-other
// — and from then on the orchestrator answers no renew.
type takeOverThenRenewFails struct{ *poolOrch }

func (t takeOverThenRenewFails) Stop(ctx context.Context, id, lease string) (platform.SwarmContainer, error) {
	t.takeOver(id)
	t.mu.Lock()
	t.renewErr = orchFailure()
	t.mu.Unlock()
	return t.poolOrch.Stop(ctx, id, lease)
}

// TestLeaseOwnership_StopHeldByOther_RenewUnanswered_NothingMountedOrWritten:
// a stop refused held-by-other whose confirming renew gets no answer leaves
// zcp unable to tell whose the container is (DP-2): the orchestrator just
// said another holder reserves it, so a park never mounts it again and a
// re-park writes nothing — the record stays as it was and the refusal names
// the status that asks again.
func TestLeaseOwnership_StopHeldByOther_RenewUnanswered_NothingMountedOrWritten(t *testing.T) {
	t.Parallel()
	t.Run("a park", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.acquire(t, groupHQPool)
		before := of.rawRecord(t, ownHQAlias)
		of.routeHQ(takeOverThenRenewFails{poolOrch: of.hq})

		_, err := LeasePark(context.Background(), of.deps, ownHQAlias)
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || !strings.Contains(pe.Suggestion, "zerops_lease action=status") {
			t.Fatalf("err = %v, want a refusal naming the status that asks again", err)
		}
		if of.mounter.mounted(ownHQAlias) {
			t.Errorf("zcp mounted %s again although the orchestrator said another holder reserves it", ownHQAlias)
		}
		if got := of.rawRecord(t, ownHQAlias); !maps.Equal(got, before) {
			t.Errorf("record = %v, want it as it was (%v)", got, before)
		}
	})
	t.Run("the stop that re-parks a refused release's lease", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.acquire(t, groupHQPool)
		if _, err := LeasePark(context.Background(), of.deps, ownHQAlias); err != nil {
			t.Fatal(err)
		}
		before := of.rawRecord(t, ownHQAlias)
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
		of.routeHQ(takeOverThenRenewFails{poolOrch: of.hq})

		_, err := LeaseReleaseChange(context.Background(), of.deps, ownHQAlias, false)
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || !strings.Contains(pe.Message, "notes.txt") {
			t.Fatalf("err = %v, want the release refused for notes.txt", err)
		}
		if strings.Contains(pe.Message, LeaseParkCall(ownHQAlias)) {
			t.Errorf("refusal %q tells the agent to park a container another holder may reserve", pe.Message)
		}
		if got := of.rawRecord(t, ownHQAlias); !maps.Equal(got, before) {
			t.Errorf("record = %v, want it as it was (%v)", got, before)
		}
	})
}

// laggingSearchByID is a platform client whose service search does not list
// the pool yet (the search lags writes, §3.5) while its read by id finds it.
type laggingSearchByID struct{ platform.Client }

func (l laggingSearchByID) ListServices(ctx context.Context, projectID string) ([]platform.ServiceStack, error) {
	all, err := l.Client.ListServices(ctx, projectID)
	// A copy: the mock answers its own slice.
	return slices.DeleteFunc(slices.Clone(all), func(s platform.ServiceStack) bool { return s.Name == groupHQPool }), err
}

// TestLeaseOwnership_LaggingSearch_TheReadByIDDecides: a search that misses
// the pool is no proof it is gone (§3.5): the read by id decides. Found, the
// release goes to the orchestrator with the lease and status reads the lease
// held; a read by id that fails otherwise leaves status reading it
// unverified — never lost, never a release that drops a live lease.
func TestLeaseOwnership_LaggingSearch_TheReadByIDDecides(t *testing.T) {
	t.Parallel()
	t.Run("found by id", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.acquire(t, groupHQPool)
		lease := of.hq.leaseOf(ownHQContainer)
		of.deps.Client = laggingSearchByID{Client: of.deps.Client}

		if v := hqView(t, of.deps); v.State != LeaseStateHeld || v.Unverified {
			t.Errorf("status = %+v, want held: the read by id finds the pool", v)
		}
		v, err := LeaseRelease(context.Background(), of.deps, ownHQAlias, true)
		if err != nil || v.State != LeaseStateReleased {
			t.Fatalf("release = %+v, %v; want it released", v, err)
		}
		if got := of.hq.releaseLog(); len(got) != 1 || got[0].lease != lease {
			t.Errorf("releases = %+v, want the one release with zcp's lease", got)
		}
	})
	t.Run("the read by id fails", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.acquire(t, groupHQPool)
		of.deps.Client = laggingSearch{Client: of.deps.Client, byID: platform.NewPlatformError(platform.ErrNetworkError, "dial tcp api.zerops.io: i/o timeout", "")}

		status, err := LeaseStatus(context.Background(), of.deps, "")
		if err != nil {
			t.Fatalf("LeaseStatus: %v", err)
		}
		for _, v := range status.Leases {
			if v.Alias == ownHQAlias && (v.State == LeaseStateLost || !v.Unverified) {
				t.Errorf("%s = %+v, want unverified, never lost", ownHQAlias, v)
			}
		}
	})
}

// TestLeaseOwnership_UnreadableRecordAtTheAlias_KeptAndSaid: a record at the
// alias that cannot be read by the time zcp would drop it is not known to be
// the lease's own: zcp keeps it and says why, with the way to clear it.
func TestLeaseOwnership_UnreadableRecordAtTheAlias_KeptAndSaid(t *testing.T) {
	t.Parallel()
	of := newOwnFixture(t)
	of.acquire(t, groupHQPool)
	path := filepath.Join(of.stateDir, "leases", ownHQAlias+".json")
	var once sync.Once
	of.routeHQ(hookOrch{poolOrch: of.hq, after: func(call string) {
		if call == "release" {
			once.Do(func() {
				if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
					t.Error(err)
				}
			})
		}
	}})

	v, err := LeaseRelease(context.Background(), of.deps, ownHQAlias, true)
	if err != nil || v.State != LeaseStateReleased {
		t.Fatalf("release = %+v, %v; want it released", v, err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "{not json" {
		t.Errorf("record = %q, %v; want the unreadable record kept", raw, err)
	}
	if !strings.Contains(v.Note, "could not be read") {
		t.Errorf("note = %q, want it to say the record could not be read and was kept", v.Note)
	}
}

// TestLeaseOwnership_AliasReusedByTheSameBranch_NoRewireWithoutIt: a lease
// found ended whose alias another zcp took again for a lease of the same
// branch rewires nothing: the alias still names a lease of the branch, so
// handing the siblings an overlay without it would cut them off from it
// (DP-26).
func TestLeaseOwnership_AliasReusedByTheSameBranch_NoRewireWithoutIt(t *testing.T) {
	t.Parallel()
	of := newOwnFixture(t)
	of.acquire(t, groupHQPool, groupWebPool)
	stale := of.records(t)[ownHQAlias]
	of.reacquireHQOn(t, "feat-x")

	change, err := releaseLost(context.Background(), of.deps, stale, "the lease ended outside zcp")
	if err != nil {
		t.Fatalf("releaseLost: %v", err)
	}
	if len(change.Rewired) != 0 {
		t.Errorf("rewired = %+v, want nothing: %s still names a lease of the branch", change.Rewired, ownHQAlias)
	}
	if rec := of.rawRecord(t, ownHQAlias); rec == nil || rec["lease"] != ownNewLease {
		t.Errorf("record = %v, want the other process's", rec)
	}
}

// TestLeaseOwnership_ParkedWake_RenewUnanswered_RefusalSaysItStartedIt: a
// release that started a parked lease's container and then gets no answer to
// its renew refuses — and says it started the container, never that it sent
// the container nothing.
func TestLeaseOwnership_ParkedWake_RenewUnanswered_RefusalSaysItStartedIt(t *testing.T) {
	t.Parallel()
	of := newOwnFixture(t)
	of.acquire(t, groupHQPool)
	if _, err := LeasePark(context.Background(), of.deps, ownHQAlias); err != nil {
		t.Fatal(err)
	}
	of.deps.SSHReadyEvery, of.deps.StartedSSHWait = time.Millisecond, 200*time.Millisecond
	var once sync.Once
	of.ssh.setHandler(func(host, command string) ([]byte, error) {
		if host == ownHQAlias && command == "true" {
			once.Do(func() { of.hqRenewFails(orchFailure()) })
		}
		return nil, nil
	})

	_, err := LeaseReleaseChange(context.Background(), of.deps, ownHQAlias, false)
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if strings.Contains(pe.Message, "sent nothing to it") || !strings.Contains(pe.Message, "started") {
		t.Errorf("refusal %q must say zcp started the container, never that it sent it nothing", pe.Message)
	}
}

// TestLeaseTargetChecked_TheOrchestratorVouchesOnce: a call that reaches a
// lease's container over ssh or sshfs (a dev server, a verify, a deploy from
// it, a mount, zcp init's remount) asks the pool's orchestrator once whether
// the container is still the lease's (DP-2): ours is returned — parked when
// its container is stopped, recorded so or not (DP-28); another holder's, a
// freed one and a gone one are refused as lost (IsLeaseLost), naming the
// release that drops the record; one the orchestrator does not answer about,
// or that this zcp cannot ask, is refused naming the status that asks again,
// and is not lost; a host that is no lease asks nothing.
func TestLeaseTargetChecked_TheOrchestratorVouchesOnce(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		host       string
		setup      func(t *testing.T, of *ownFixture)
		wantLease  bool
		wantParked bool
		wantErr    string
		// wantLost: the refusal is of a lease that is no longer zcp's
		// (IsLeaseLost), not of one the orchestrator did not answer about.
		wantLost bool
		renews   int
	}{
		{name: "ours, running", host: ownHQAlias, wantLease: true, renews: 1},
		{name: "ours, stopped outside zcp", host: ownHQAlias, wantLease: true, wantParked: true, renews: 1, setup: func(t *testing.T, of *ownFixture) {
			t.Helper()
			if _, err := of.hq.Stop(context.Background(), ownHQContainer, of.hq.leaseOf(ownHQContainer)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "another holder's", host: ownHQAlias, wantLease: true, wantErr: "zerops_lease action=release lease=" + ownHQAlias, wantLost: true, renews: 1,
			setup: func(_ *testing.T, of *ownFixture) { of.hq.takeOver(ownHQContainer) }},
		{name: "released elsewhere", host: ownHQAlias, wantLease: true, wantErr: "zerops_lease action=release lease=" + ownHQAlias, wantLost: true, renews: 1,
			setup: func(_ *testing.T, of *ownFixture) { of.hq.releaseOutside(ownHQContainer) }},
		{name: "gone", host: ownHQAlias, wantLease: true, wantErr: "zerops_lease action=release lease=" + ownHQAlias, wantLost: true, renews: 1,
			setup: func(_ *testing.T, of *ownFixture) { of.hq.remove(ownHQContainer) }},
		{name: "not answered", host: ownHQAlias, wantLease: true, wantErr: "zerops_lease action=status", renews: 1,
			setup: func(_ *testing.T, of *ownFixture) { of.hqRenewFails(orchFailure()) }},
		{name: "no orchestrator to ask", host: ownHQAlias, wantLease: true, wantErr: "zerops_lease action=status",
			setup: func(_ *testing.T, of *ownFixture) { of.deps.Swarm = nil }},
		{name: "no lease", host: "appdev"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			of.acquire(t, groupHQPool)
			before := len(of.hq.renewLog())
			if tt.setup != nil {
				tt.setup(t, of)
			}
			lease, ok, err := LeaseTargetChecked(context.Background(), of.deps, tt.host, "verified")
			if ok != tt.wantLease {
				t.Fatalf("ok = %v, want %v", ok, tt.wantLease)
			}
			var pe *platform.PlatformError
			switch {
			case tt.wantErr != "" && (!errors.As(err, &pe) || !strings.Contains(pe.Suggestion, tt.wantErr)):
				t.Errorf("err = %v, want a refusal naming %q", err, tt.wantErr)
			case tt.wantErr == "" && err != nil:
				t.Errorf("err = %v, want the lease", err)
			case tt.wantErr == "" && ok && (lease.Alias != ownHQAlias || lease.Parked != tt.wantParked):
				t.Errorf("lease = %+v, want %s, parked %v", lease, ownHQAlias, tt.wantParked)
			}
			if got := IsLeaseLost(err); got != tt.wantLost {
				t.Errorf("IsLeaseLost(%v) = %v, want %v", err, got, tt.wantLost)
			}
			if got := len(of.hq.renewLog()) - before; got != tt.renews {
				t.Errorf("renews = %d, want %d: one per call", got, tt.renews)
			}
		})
	}
}
