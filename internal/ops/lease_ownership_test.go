// Tests for: whether the container a lease record names is still that
// record's reservation (docs/spec-workflows.md §8 DP-2) — gone, free, reserved
// by zcp's lease, reserved by another holder, or not verifiable — and what
// status, release (one lease, a branch, forced), the warm decision, park,
// unpark, sync, the acquire's reuse check and the rewire do with each verdict.
// A lease zcp no longer holds is never read or written over ssh, and its
// container is never reset, deleted, stopped or started.
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

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// The live values of the 2026-10-24 drive (lease-ledger 4g): zcp acquired
// lvapi-3 at 11:01:00 (its reservation's lockSince 11:01:00.888), it was
// released outside zcp, and another consumer reserved the container at
// 11:02:04.783.
const (
	ownAcquiredAt    = "2026-10-24T11:01:00Z"
	ownLockSince     = "2026-10-24T11:01:00.88865963Z"
	ownOtherSince    = "2026-10-24T11:02:04.783475573Z"
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

// TestLeaseOwnership_Status_EachVerdict: status decides from the orchestrator
// whether the container is still this record's reservation. The record keeps
// the reservation's lockSince; the same lockSince is zcp's lease (held), a
// later one is another holder's (lost, saying so), and one zcp cannot place —
// a stop or start of zcp's own whose outcome it never learned, a record older
// than keeping lockSince whose reservation moved since the acquire — is
// unverified, never lost. A lease that is not held, or unverified, is never
// read over ssh.
func TestLeaseOwnership_Status_EachVerdict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		record         map[string]any
		container      *platform.SwarmContainer // nil: gone from the pool
		lease          string                   // the lease holding it
		wantState      string
		wantUnverified bool
		wantNote       string
	}{
		{
			name:   "reserved since the record's own lockSince: held",
			record: map[string]any{"lockSince": ownLockSince}, container: ptrTo(hqContainer(leaseLockReserved, ownLockSince, "")),
			lease: ownHQLease, wantState: LeaseStateHeld,
		},
		{
			name:   "the same reservation written at another precision: held",
			record: map[string]any{"lockSince": "2026-10-24T11:01:00.888Z"}, container: ptrTo(hqContainer(leaseLockReserved, ownLockSince, "")),
			lease: ownHQLease, wantState: LeaseStateHeld,
		},
		{
			name:   "reserved since later than the record's lockSince: another holder's, lost",
			record: map[string]any{"lockSince": ownLockSince}, container: ptrTo(hqContainer(leaseLockReserved, ownOtherSince, "")),
			lease: ownOtherLease, wantState: LeaseStateLost, wantNote: "another holder",
		},
		{
			name:   "stopped by another holder since: lost, not parked",
			record: map[string]any{"lockSince": ownLockSince}, container: ptrTo(hqContainer(leaseLockReserved, ownOtherSince, platform.SwarmContainerStopped)),
			lease: ownOtherLease, wantState: LeaseStateLost, wantNote: "another holder",
		},
		{
			name:   "later while zcp's own stop or start of it was in flight: unverified, never lost",
			record: map[string]any{"lockSince": ownLockSince, "lockChanging": "2026-10-24T11:02:00Z"}, container: ptrTo(hqContainer(leaseLockReserved, ownOtherSince, platform.SwarmContainerStopped)),
			lease: ownHQLease, wantState: LeaseStateParked, wantUnverified: true, wantNote: "cannot verify",
		},
		{
			name:   "free: lost",
			record: map[string]any{"lockSince": ownLockSince}, container: ptrTo(hqContainer("", "", "")),
			wantState: LeaseStateLost, wantNote: "no longer reserves",
		},
		{
			name:   "being deleted: lost",
			record: map[string]any{"lockSince": ownLockSince}, container: ptrTo(hqContainer("deleting", ownOtherSince, "DELETING")),
			wantState: LeaseStateLost, wantNote: "deleting",
		},
		{
			name:   "gone from the pool: lost",
			record: map[string]any{"lockSince": ownLockSince}, container: nil,
			wantState: LeaseStateLost, wantNote: "gone",
		},
		{
			name:   "an older record reserved within its acquire: held",
			record: map[string]any{}, container: ptrTo(hqContainer(leaseLockReserved, ownLockSince, "")),
			lease: ownHQLease, wantState: LeaseStateHeld,
		},
		{
			name:   "an older record whose container was reserved a minute after its acquire (the live 4g case): unverified, never lost",
			record: map[string]any{}, container: ptrTo(hqContainer(leaseLockReserved, ownOtherSince, "")),
			lease: ownOtherLease, wantState: LeaseStateHeld, wantUnverified: true, wantNote: "cannot verify",
		},
		{
			name:   "an older record zcp parked, stopped at its park: parked",
			record: map[string]any{"parked": true, "parkedAt": "2026-10-24T11:07:01Z"}, container: ptrTo(hqContainer(leaseLockReserved, "2026-10-24T11:07:01.738476823Z", platform.SwarmContainerStopped)),
			lease: ownHQLease, wantState: LeaseStateParked,
		},
		{
			name:   "reserved, the orchestrator naming no lockSince: held",
			record: map[string]any{"lockSince": ownLockSince}, container: ptrTo(hqContainer(leaseLockReserved, "", "")),
			lease: ownHQLease, wantState: LeaseStateHeld,
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
			if !readable && v.Activity != nil && v.Activity.ReadAt != "" {
				t.Errorf("activity %+v was read from a container that is not verified as this lease's", v.Activity)
			}
		})
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

// TestLeaseOwnership_Release_NotOurs_DropsTheRecordAndTouchesNothing: a
// release — plain or forced — of a lease whose container is gone, free, or
// reserved by another holder drops the record as lost, saying why, and takes
// down the alias and the mount. It never runs the guard over ssh and never
// calls the orchestrator's release or delete: a release of a free container
// resets it whatever lease it carries (live, 2026-10-24).
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
				if of.rawRecord(t, ownHQAlias) != nil {
					t.Error("the record is still there")
				}
				if of.mounter.mounted(ownHQAlias) {
					t.Error("the mount is still there")
				}
				if of.aliasStanza(ownHQAlias) {
					t.Error("the alias stanza is still there")
				}
			})
		}
	}
}

// writeUnverified writes an older record of hqpool-3 (no lockSince, acquired
// at ownAcquiredAt) whose container the orchestrator lists reserved since a
// minute later, held by lease, mounted and aliased.
func (of *ownFixture) writeUnverified(t *testing.T, lease string, fields map[string]any, status string) {
	t.Helper()
	of.writeRecord(t, groupHQPool, "svc-hq", 3, fields)
	of.hq.put(hqContainer(leaseLockReserved, ownOtherSince, status), lease)
	if status != platform.SwarmContainerStopped {
		of.mounter.m.states["/var/www/"+ownHQAlias] = platform.MountStateActive
	}
	if r := of.deps.Host.refreshAliases(context.Background(), of.deps); r != "" {
		t.Fatal(r)
	}
}

// TestLeaseOwnership_Release_Unverified: a lease zcp cannot verify is never
// read over ssh. A plain release refuses, naming the park that asks the
// orchestrator with the lease and the forced release; a forced one asks the
// orchestrator, and its held-by-other answer drops the record as lost — never
// "retry in a moment" — while a lease that turns out zcp's own is released.
func TestLeaseOwnership_Release_Unverified(t *testing.T) {
	t.Parallel()

	t.Run("plain: refused, nothing read or changed", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.writeUnverified(t, ownOtherLease, map[string]any{}, "")

		_, err := LeaseReleaseChange(context.Background(), of.deps, ownHQAlias, false)
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
			t.Fatalf("err = %v, want a PREREQUISITE_MISSING refusal", err)
		}
		if !strings.Contains(pe.Message, ownHQAlias) || !strings.Contains(pe.Message, "cannot verify") {
			t.Errorf("message %q must name the lease and that zcp cannot verify it", pe.Message)
		}
		for _, want := range []string{"zerops_lease action=park lease=" + ownHQAlias, "force=true"} {
			if !strings.Contains(pe.Suggestion, want) {
				t.Errorf("suggestion %q lacks %q", pe.Suggestion, want)
			}
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

	t.Run("forced: held-by-other drops the record as lost", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.writeUnverified(t, ownOtherLease, map[string]any{}, "")

		change, err := LeaseReleaseChange(context.Background(), of.deps, ownHQAlias, true)
		if err != nil {
			t.Fatalf("LeaseReleaseChange: %v", err)
		}
		v := change.Leases[0]
		if v.State != LeaseStateLost || !strings.Contains(v.Note, "another holder") || strings.Contains(v.Note, ownRemovedReason) {
			t.Errorf("view = %+v, want lost, naming another holder, never a retry", v)
		}
		if of.rawRecord(t, ownHQAlias) != nil || of.aliasStanza(ownHQAlias) || of.mounter.mounted(ownHQAlias) {
			t.Error("the record, alias stanza or mount survived")
		}
		if c := of.hq.get(ownHQContainer); c.LockState != leaseLockReserved || of.hq.leaseOf(ownHQContainer) != ownOtherLease {
			t.Errorf("the other holder's reservation changed: %+v", c)
		}
		if got := of.hq.freeResetLog(); len(got) != 0 {
			t.Errorf("free containers reset: %v", got)
		}
	})

	t.Run("forced: zcp's own after all is released", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.writeUnverified(t, ownHQLease, map[string]any{}, "")

		change, err := LeaseReleaseChange(context.Background(), of.deps, ownHQAlias, true)
		if err != nil {
			t.Fatalf("LeaseReleaseChange: %v", err)
		}
		if v := change.Leases[0]; v.State != LeaseStateReleased {
			t.Errorf("view = %+v, want released", v)
		}
		if got := of.hq.releaseLog(); len(got) != 1 || got[0] != (fakeRelease{ownHQContainer, ownHQLease, true}) {
			t.Errorf("releases = %+v, want one reset release with its lease", got)
		}
	})
}

// TestLeaseOwnership_HeldByOther_FromAnyMutatingCall_DropsTheRecord: the
// orchestrator's held-by-other answer to any call carrying the lease — the
// warm decision's delete, a park's stop, an unpark's start — means the
// container is no longer this lease's: the record is dropped as lost with its
// alias and mount, never retried as "in a moment".
func TestLeaseOwnership_HeldByOther_FromAnyMutatingCall_DropsTheRecord(t *testing.T) {
	t.Parallel()

	t.Run("the warm decision's delete", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.writeUnverified(t, ownOtherLease, map[string]any{}, "")
		// The pool already has its warm container, so the release deletes.
		of.hq.put(platform.SwarmContainer{ID: "c-hqpool-4", Number: 4, Hostname: nodeHostnameOf(groupHQPool, 4), Status: platform.SwarmContainerActive, Current: true, Snapshots: 1}, "")
		of.deps.Warm = &LeaseWarm{Target: 1, Go: func(fn func()) { fn() }}

		change, err := LeaseReleaseChange(context.Background(), of.deps, ownHQAlias, true)
		if err != nil {
			t.Fatalf("LeaseReleaseChange: %v", err)
		}
		if v := change.Leases[0]; v.State != LeaseStateLost || !strings.Contains(v.Note, "another holder") || strings.Contains(v.Note, ownRemovedReason) {
			t.Errorf("view = %+v, want lost naming another holder", v)
		}
		if got := of.hq.releaseLog(); len(got) != 0 {
			t.Errorf("releases after the refused delete = %+v, want none", got)
		}
		if of.rawRecord(t, ownHQAlias) != nil {
			t.Error("the record survived")
		}
	})

	t.Run("a park's stop", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.writeUnverified(t, ownOtherLease, map[string]any{}, "")

		v, err := LeasePark(context.Background(), of.deps, ownHQAlias)
		if err != nil {
			t.Fatalf("LeasePark: %v", err)
		}
		if v.State != LeaseStateLost || !strings.Contains(v.Note, "another holder") || !strings.Contains(v.Note, "record is dropped") {
			t.Errorf("view = %+v, want lost naming another holder and the dropped record", v)
		}
		if of.rawRecord(t, ownHQAlias) != nil || of.aliasStanza(ownHQAlias) || of.mounter.mounted(ownHQAlias) {
			t.Error("the record, alias stanza or mount survived")
		}
		if c := of.hq.get(ownHQContainer); c.Status != platform.SwarmContainerActive {
			t.Errorf("the other holder's container = %+v, want it running as it was", c)
		}
	})

	t.Run("an unpark's start", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.writeUnverified(t, ownOtherLease, map[string]any{"parked": true, "parkedAt": "2026-10-24T09:00:00Z"}, platform.SwarmContainerStopped)

		v, err := LeaseUnpark(context.Background(), of.deps, ownHQAlias)
		if err != nil {
			t.Fatalf("LeaseUnpark: %v", err)
		}
		if v.State != LeaseStateLost || !strings.Contains(v.Note, "another holder") {
			t.Errorf("view = %+v, want lost naming another holder", v)
		}
		if of.rawRecord(t, ownHQAlias) != nil || of.aliasStanza(ownHQAlias) {
			t.Error("the record or alias stanza survived")
		}
		if of.reached(ownHQAlias) {
			t.Error("ssh reached the other holder's container")
		}
	})
}

// TestLeaseOwnership_WarmDecision_NeverResetsOrDeletesNotOurs: the warm
// decision (DP-27) acts on the pool as it read it under the warm lock: a
// container that is gone, free, or another holder's reservation there is
// never reset (the orchestrator checks no lease on a free container) nor
// deleted; the record is dropped as lost.
func TestLeaseOwnership_WarmDecision_NeverResetsOrDeletesNotOurs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mine *platform.SwarmContainer
		want string
	}{
		{name: "gone from the pool", mine: nil, want: "gone"},
		{name: "free", mine: ptrTo(hqContainer("", "", "")), want: "no longer reserves"},
		{name: "another holder's", mine: ptrTo(hqContainer(leaseLockReserved, ownOtherSince, "")), want: "another holder"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			of.writeRecord(t, groupHQPool, "svc-hq", 3, map[string]any{"lockSince": ownLockSince})
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
			if err != nil {
				t.Fatalf("decideRelease: %v", err)
			}
			if resets != 0 || len(of.hq.deleteLog()) != 0 {
				t.Errorf("resets %d, deletes %v: want neither for a container that is not this lease's", resets, of.hq.deleteLog())
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

// TestLeaseOwnership_ReleaseBranch: a branch's release drops a member zcp no
// longer holds as lost — no guard over ssh, no orchestrator call — and
// releases the rest; a member zcp cannot verify refuses the branch without
// forcing, nothing released and nothing read from it.
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
		if len(of.records(t)) != 0 {
			t.Errorf("records left: %v", of.records(t))
		}
		if of.mounter.mounted(ownWebAlias) || of.aliasStanza(ownWebAlias) || of.rawRecord(t, ownWebAlias) != nil {
			t.Errorf("%s's mount, alias stanza or record survived", ownWebAlias)
		}
	})

	t.Run("an unverified member refuses the branch", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.acquire(t, groupWebPool)
		of.writeUnverified(t, ownOtherLease, map[string]any{}, "")

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

// TestLeaseOwnership_Park_KeepsTheReservationsLockSince: zcp's own stop and
// start restart the reservation's lockSince, so park and unpark keep the one
// the orchestrator answered in the record and the lease stays verified; a
// stop whose answer was lost leaves the record saying so, and status reads
// the lease unverified, never another holder's.
func TestLeaseOwnership_Park_KeepsTheReservationsLockSince(t *testing.T) {
	t.Parallel()

	t.Run("park and unpark", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.acquire(t, groupHQPool)
		ctx := context.Background()
		if _, err := LeasePark(ctx, of.deps, ownHQAlias); err != nil {
			t.Fatalf("LeasePark: %v", err)
		}
		c := of.hq.get(ownHQContainer)
		if got := of.rawRecord(t, ownHQAlias)["lockSince"]; got != c.LockSince {
			t.Errorf("record lockSince after the park = %v, want the stop's %s", got, c.LockSince)
		}
		if v := hqView(t, of.deps); v.State != LeaseStateParked || v.Unverified {
			t.Errorf("status after the park = %+v, want parked, verified", v)
		}
		if _, err := LeaseUnpark(ctx, of.deps, ownHQAlias); err != nil {
			t.Fatalf("LeaseUnpark: %v", err)
		}
		c = of.hq.get(ownHQContainer)
		if got := of.rawRecord(t, ownHQAlias)["lockSince"]; got != c.LockSince {
			t.Errorf("record lockSince after the unpark = %v, want the start's %s", got, c.LockSince)
		}
		if v := hqView(t, of.deps); v.Unverified || (v.State != LeaseStateHeld && v.State != LeaseStateUnmounted) {
			t.Errorf("status after the unpark = %+v, want held, verified", v)
		}
	})

	t.Run("a stop whose answer was lost", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.acquire(t, groupHQPool)
		of.hq.mu.Lock()
		of.hq.powerCut = true
		of.hq.mu.Unlock()
		if _, err := LeasePark(context.Background(), of.deps, ownHQAlias); err == nil {
			t.Fatal("LeasePark: want the lost answer's error")
		}
		v := hqView(t, of.deps)
		if v.State == LeaseStateLost || !v.Unverified || !strings.Contains(v.Note, "cannot verify") {
			t.Errorf("status = %+v, want unverified (zcp's own stop may have moved it), never lost", v)
		}
		if of.reached(ownHQAlias) {
			t.Error("status read a lease it cannot verify")
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
// member zcp no longer holds is "lost" and one it cannot verify "unverified",
// neither fetched nor fast-forwarded over ssh.
func TestLeaseOwnership_Sync_LeasesNoLongerOursAreNeverReached(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (*groupFixture, *syncRemote) {
		t.Helper()
		gf, remote := newSyncFixture(t, syncBranch)
		gf.hq.put(platform.SwarmContainer{ID: "c-hqpool", Number: 3, Hostname: nodeHostnameOf(groupHQPool, 3), Status: platform.SwarmContainerActive, LockState: leaseLockReserved, LockSince: "2026-10-24T10:00:00.5Z"}, groupHQPool+"-lease-0123456789abcdef")
		gf.web.put(platform.SwarmContainer{ID: "c-webpool", Number: 7, Hostname: nodeHostnameOf(groupWebPool, 7), Status: platform.SwarmContainerActive, LockState: leaseLockReserved, LockSince: "2026-10-24T10:00:01.5Z"}, groupWebPool+"-lease-0123456789abcdef")
		return gf, remote
	}

	t.Run("the source", func(t *testing.T) {
		t.Parallel()
		gf, remote := setup(t)
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
		{name: "a member released outside zcp", want: "lost", end: func(o *poolOrch) { o.releaseOutside("c-webpool") }},
		{name: "a member zcp cannot verify", want: "unverified", end: func(o *poolOrch) { o.takeOver("c-webpool") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gf, remote := setup(t)
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
// that drops it (lost), and one zcp cannot verify is refused naming the park
// that verifies it — never returned reused.
func TestLeaseOwnership_Acquire_ReuseCheck(t *testing.T) {
	t.Parallel()

	t.Run("another holder's", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.acquire(t, groupHQPool)
		of.hq.takeOver(ownHQContainer)

		_, err := LeaseAcquire(context.Background(), of.deps, groupHQPool, testRepo, "feat-x")
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing || !strings.Contains(pe.Suggestion, "zerops_lease action=release lease="+ownHQAlias) {
			t.Fatalf("err = %v, want a refusal naming the release", err)
		}
		if of.hq.acquireCount() != 1 {
			t.Errorf("acquires = %d, want only the first", of.hq.acquireCount())
		}
	})

	t.Run("unverified", func(t *testing.T) {
		t.Parallel()
		of := newOwnFixture(t)
		of.writeUnverified(t, ownOtherLease, map[string]any{}, "")

		_, err := LeaseAcquire(context.Background(), of.deps, groupHQPool, testRepo, "feat-x")
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing || !strings.Contains(pe.Suggestion, "zerops_lease action=park lease="+ownHQAlias) {
			t.Fatalf("err = %v, want a refusal naming the park", err)
		}
		if of.hq.acquireCount() != 0 || of.reached(ownHQAlias) {
			t.Errorf("acquires = %d, ssh reached %v: want neither", of.hq.acquireCount(), of.reached(ownHQAlias))
		}
	})
}

// TestLeaseOwnership_Rewire_SiblingNoLongerOursIsNeverReached: the rewire of
// a release hands the overlay only to siblings zcp still holds; one another
// holder took is named with the release that drops it, nothing sent to it.
func TestLeaseOwnership_Rewire_SiblingNoLongerOursIsNeverReached(t *testing.T) {
	t.Parallel()
	of := newOwnFixture(t)
	of.acquire(t, groupHQPool, groupWebPool)
	of.web.takeOver(ownWebContainer)

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

// TestLeaseOwnership_Acquire_RecordsTheReservationsLockSince: the record
// keeps the reservation's lockSince — from the acquire's answer, or a read by
// id right after it when the answer names none.
func TestLeaseOwnership_Acquire_RecordsTheReservationsLockSince(t *testing.T) {
	t.Parallel()
	for _, bare := range []bool{false, true} {
		t.Run(fmt.Sprintf("answer without lockSince=%v", bare), func(t *testing.T) {
			t.Parallel()
			of := newOwnFixture(t)
			of.hq.bareAcquire = bare
			of.acquire(t, groupHQPool)
			c := of.hq.get(ownHQContainer)
			if got := of.rawRecord(t, ownHQAlias)["lockSince"]; c.LockSince == "" || got != c.LockSince {
				t.Errorf("record lockSince = %v, want the reservation's %s", got, c.LockSince)
			}
		})
	}
}

// TestLeaseOwnership_FullPool_NamesNoLeaseAnotherHolderTook: a full pool's
// refusal counts as zcp's own only the leases it still holds, and reads none
// it does not over ssh.
func TestLeaseOwnership_FullPool_NamesNoLeaseAnotherHolderTook(t *testing.T) {
	t.Parallel()
	of := newOwnFixture(t)
	of.acquire(t, groupHQPool)
	of.hq.takeOver(ownHQContainer)
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

// ptrTo is a pointer to a copy of v.
func ptrTo[T any](v T) *T { return &v }
