package ops

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// LP-RETAKEN — A lease the orchestrator answers unknown-lease for while it
// lists the container reserved is another holder's, lost, never ours.
// witness: internal/ops/lease_ownership.go | return leaseOther, otherWhy → return leaseOurs, ""
func TestLease_LPRetaken_UnknownLeaseListedReserved_OtherHolders(t *testing.T) {
	t.Parallel()
	own, _ := renewedOwnership(platform.SwarmContainer{},
		&platform.SwarmError{Reason: platform.SwarmReasonUnknownLease}, reservedByOther)
	if own != leaseOther {
		t.Fatalf("ownership = %d, want leaseOther (%d)", own, leaseOther)
	}
	if !own.lost() {
		t.Fatalf("ownership %d is not lost", own)
	}
}

// LP-STOPPED — A lease whose container the orchestrator renews while stopping
// or starting it (a park, an unpark) stays ours.
// witness: internal/ops/lease_ownership.go | case c.LockState == leaseLockReserved || powerTransition(c): → case c.LockState == leaseLockReserved:
func TestLease_LPStopped_RenewedDuringPowerTransition_StaysOurs(t *testing.T) {
	t.Parallel()
	for _, lock := range []string{leaseLockStopping, leaseLockStarting} {
		own, _ := renewedOwnership(platform.SwarmContainer{LockState: lock}, nil, reservedUnread)
		if own != leaseOurs {
			t.Errorf("lockState %q: ownership = %d, want leaseOurs (%d)", lock, own, leaseOurs)
		}
	}
}

// LP-UNVERIFIED — A renew the orchestrator did not answer (any error that is
// not unknown-lease or container-not-found) is unverified: neither ours nor
// lost, so nothing reaches the container and its record stays.
// witness: internal/ops/lease_ownership.go | return o == leaseGone || o == leaseFree || o == leaseOther → return o == leaseGone || o == leaseFree || o == leaseOther || o == leaseUnverified
func TestLease_LPUnverified_RenewUnanswered_NeitherOursNorLost(t *testing.T) {
	t.Parallel()
	errs := []error{
		errors.New("orchestrator read timed out"),
		&platform.SwarmError{Reason: platform.SwarmReasonAllReserved},
	}
	for _, err := range errs {
		own, _ := renewedOwnership(platform.SwarmContainer{}, err, reservedByOther)
		if own != leaseUnverified {
			t.Errorf("renew error %v: ownership = %d, want leaseUnverified (%d)", err, own, leaseUnverified)
		}
		if own.lost() {
			t.Errorf("renew error %v: ownership %d reads lost", err, own)
		}
	}
}

// LP-DIRTY-BRANCH — Releasing a branch whose one lease holds uncommitted
// files or unpushed commits refuses the whole branch and releases none.
// witness: internal/ops/lease_group.go | return m.err != nil || m.work.any() → return m.err != nil
func TestLease_LPDirtyBranch_OneLeaseUnsaved_RefusesBranch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		work unsavedWork
	}{
		{name: "uncommitted files", work: unsavedWork{files: []string{" M main.go"}}},
		{name: "unpushed commits", work: unsavedWork{unpushed: 2}},
	} {
		members := []*releaseMember{
			{rec: leaseRecord{PoolHostname: "devpool", ContainerNumber: 1, Branch: "feature"}},
			{rec: leaseRecord{PoolHostname: "webpool", ContainerNumber: 2, Branch: "feature"}, work: tc.work},
		}
		assertBranchRefused(t, tc.name, branchReleaseRefusal(LeaseDeps{}, members, "", "feature"))
	}
}

// LP-UNCHECKED-BRANCH — Releasing a branch where one lease could not be
// checked for unsaved work refuses the whole branch.
// witness: internal/ops/lease_group.go | return m.err != nil || m.work.any() → return m.work.any()
func TestLease_LPUncheckedBranch_OneLeaseUnread_RefusesBranch(t *testing.T) {
	t.Parallel()
	members := []*releaseMember{
		{rec: leaseRecord{PoolHostname: "devpool", ContainerNumber: 1, Branch: "feature"}},
		{rec: leaseRecord{PoolHostname: "webpool", ContainerNumber: 2, Branch: "feature"}, err: errors.New("git status could not be read")},
	}
	assertBranchRefused(t, "unchecked lease", branchReleaseRefusal(LeaseDeps{}, members, "", "feature"))
}

func assertBranchRefused(t *testing.T, name string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: branch release refusal = nil, want a refusal of the whole branch", name)
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
		t.Fatalf("%s: branch release refusal = %v, want a *platform.PlatformError with %s", name, err, platform.ErrPrerequisiteMissing)
	}
}

// LP-MAIN — No lease is ever acquired on the repository's default branch; any
// other branch is accepted.
// witness: internal/ops/lease_group.go | if branch != leaseDefaultBranch { → if branch == leaseDefaultBranch {
func TestLease_LPMain_DefaultBranch_Refused(t *testing.T) {
	t.Parallel()
	if err := refuseMainLease("main"); err == nil {
		t.Errorf("refuseMainLease(main) = nil, want a refusal")
	}
	if err := refuseMainLease("feature"); err != nil {
		t.Errorf("refuseMainLease(feature) = %v, want nil", err)
	}
}

// LP-DUP-POOL — A group acquire that names one pool twice is refused before
// any lock: a group holds one lease per pool.
// witness: internal/ops/lease_group.go | seen[p] = true →
func TestLease_LPDupPool_PoolNamedTwice_Refused(t *testing.T) {
	t.Parallel()
	err := validateAcquirePools([]string{"devpool", "webpool", "devpool"})
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter {
		t.Fatalf("validateAcquirePools(devpool twice) = %v, want a *platform.PlatformError with %s", err, platform.ErrInvalidParameter)
	}
	if err := validateAcquirePools([]string{"devpool", "webpool"}); err != nil {
		t.Fatalf("validateAcquirePools(two pools) = %v, want nil", err)
	}
}

// previewPool is a dev pool opening the HTTP preview block 4001-4003 and
// running with maxContainers maxCount.
func previewPool(maxCount int32) *platform.ServiceStack {
	return &platform.ServiceStack{
		Name: "devpool",
		Ports: []platform.Port{
			platform.NewPort(4001, "tcp", "http", false, true),
			platform.NewPort(4002, "tcp", "http", false, true),
			platform.NewPort(4003, "tcp", "http", false, true),
		},
		CustomAutoscaling: &platform.CustomAutoscaling{HorizontalMaxCount: maxCount},
	}
}

// LP-PREVIEW-RAISE — A dev pool asked for more containers than its preview
// ports cover gets a shortfall naming each container past the block and its
// port; asked for exactly the block's count, none.
// witness: internal/ops/pool_preview_ports.go | if !ok || maxContainers <= opens { → if !ok || maxContainers <= opens+1 {
func TestLease_LPPreviewRaise_PastTheBlock_NamesContainerAndPort(t *testing.T) {
	t.Parallel()
	s, ok := readPoolPreviewShortfall(previewPool(3), 4)
	if !ok {
		t.Fatalf("maxContainers 4 over block 4001-4003: no shortfall, want container 4 on port 4004")
	}
	if len(s.containers) != 1 || s.containers[0] != "4" || len(s.ports) != 1 || s.ports[0] != "4004" {
		t.Fatalf("shortfall containers %v ports %v, want [4] [4004]", s.containers, s.ports)
	}
	if s.first != 4001 || s.last != 4003 {
		t.Fatalf("shortfall block %d-%d, want 4001-4003", s.first, s.last)
	}
	if _, ok := readPoolPreviewShortfall(previewPool(3), 3); ok {
		t.Fatalf("maxContainers 3 over block 4001-4003: a shortfall, want none")
	}
}

// LP-NO-RAISE — The same maxContainers again, or a lower one, is no raise; a
// higher one is.
// witness: internal/ops/pool_preview_ports.go | *params.MaxContainers > int(eff.HorizontalMaxCount) → *params.MaxContainers >= int(eff.HorizontalMaxCount)
func TestLease_LPNoRaise_SameOrLowerMax_NoRaise(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		max   int
		raise bool
	}{
		{max: 3, raise: false},
		{max: 2, raise: false},
		{max: 4, raise: true},
	} {
		got := poolRaises(previewPool(3), ScaleParams{MaxContainers: &tc.max})
		if got != tc.raise {
			t.Errorf("maxContainers %d over a pool at 3: raises = %v, want %v", tc.max, got, tc.raise)
		}
	}
}

// LP-PARKED — A reserved container that is stopped reads parked (as does one
// being stopped or started); a reserved running one does not.
// witness: internal/ops/lease_park.go | (c.LockState == leaseLockReserved && c.Status == platform.SwarmContainerStopped) → (c.LockState == leaseLockReserved && c.Status != platform.SwarmContainerStopped)
func TestLease_LPParked_ReservedStopped_ReadsParked(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		c      platform.SwarmContainer
		parked bool
	}{
		{c: platform.SwarmContainer{LockState: leaseLockReserved, Status: platform.SwarmContainerStopped}, parked: true},
		{c: platform.SwarmContainer{LockState: leaseLockStopping, Status: platform.SwarmContainerActive}, parked: true},
		{c: platform.SwarmContainer{LockState: leaseLockStarting, Status: platform.SwarmContainerStopped}, parked: true},
		{c: platform.SwarmContainer{LockState: leaseLockReserved, Status: platform.SwarmContainerActive}, parked: false},
	} {
		if got := parkedContainer(tc.c); got != tc.parked {
			t.Errorf("lockState %q status %q: parked = %v, want %v", tc.c.LockState, tc.c.Status, got, tc.parked)
		}
	}
}

// LP-UNKNOWN-POOL — An acquire naming a service that is not in the project, or
// not a Swarm pool, is refused naming that service, also when it passes no repo
// and none is recorded: it is never asked for a repo first.
// witness: internal/ops/lease_group.go | return poolErr → return repoErr
func TestLease_LPUnknownPool_NoRepoForANonPool_RefusedNamingIt(t *testing.T) {
	t.Parallel()
	services := []platform.ServiceStack{
		{ID: "svc-pool", Name: "devpool", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		{ID: "svc-app", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
	}
	repoErr := platform.NewPlatformError(platform.ErrInvalidParameter, "acquire needs repo: pool 'devpool' has no repository recorded", "")
	for _, tc := range []struct {
		pools     []string
		code      string
		names     string
		isRepoErr bool
	}{
		{pools: []string{"nosuch"}, code: platform.ErrServiceNotFound, names: "nosuch"},
		{pools: []string{"devpool", "nosuch"}, code: platform.ErrServiceNotFound, names: "nosuch"},
		{pools: []string{"devpool", "appdev"}, code: platform.ErrInvalidParameter, names: "appdev"},
		{pools: []string{"devpool"}, isRepoErr: true},
	} {
		err := unresolvedRepoRefusal(services, tc.pools, repoErr)
		if tc.isRepoErr {
			if !errors.Is(err, repoErr) {
				t.Errorf("pools %v: err = %v, want the repo refusal", tc.pools, err)
			}
			continue
		}
		var pe *platform.PlatformError
		if !errors.As(err, &pe) {
			t.Fatalf("pools %v: err = %v, want a PlatformError", tc.pools, err)
		}
		if pe.Code != tc.code || !strings.Contains(pe.Message, tc.names) || strings.Contains(pe.Message, "needs repo") {
			t.Errorf("pools %v: refusal = %s %q, want %s naming %q and not asking for a repo", tc.pools, pe.Code, pe.Message, tc.code, tc.names)
		}
	}
}

// LP-WARM-HQ — In a Mate, the clone of a pool's repository on the Mate's HQ
// into a free warm container is handed to a zcp process of its own, which
// holds its relayed session until the clone ends; the process that warmed the
// container, which may end first (a zcp lease call, an MCP server), never runs
// it.
// witness: internal/ops/lease_warm_clone.go | if IsHQRemote(repo, hqURL) { → if false {
func TestLease_LPWarmHQ_CloneHandedToAProcessOfItsOwn(t *testing.T) {
	t.Parallel()
	wf, ssh := newWarmCloneFixture(t, newWarmOrch(4))
	wf.deps.HQAddress = func() string { return leaseHQURL }
	wf.deps.HQGit = relayedThrough{deps: &wf.deps}
	withPoolRepository(&wf.deps, testPoolHost, leaseHQRepo, true)
	var (
		mu   sync.Mutex
		held []string
	)
	wf.deps.HoldWarmClone = func(pool, containerID string) error {
		mu.Lock()
		defer mu.Unlock()
		held = append(held, pool+"/"+containerID)
		return nil
	}
	wf.hold(t, 5, true)

	wf.release(t, "devpool-5")

	mu.Lock()
	defer mu.Unlock()
	if len(held) != 1 || held[0] != testPoolHost+"/c-5" {
		t.Errorf("clones handed to a process of their own = %v, want the container the release kept, %s/c-5", held, testPoolHost)
	}
	if launches := ssh.launches(); len(launches) != 0 {
		t.Errorf("the releasing process ran the clone itself, which dies with it: %+v", launches)
	}
}

// LP-WARM-HELD — A held clone's process records what the clone did: done once
// it finished, skipped when it left the work dir alone (a checkout took it, or
// it held a repository), failed naming the clone's exit status, and failed —
// never tried again into a skip — when the session ended while the clone ran.
// witness: internal/ops/lease_warm_clone.go | case exited && code == "0": → case exited && (code == "0" || code == leaseWarmCloneNothingExit):
func TestLease_LPWarmHeld_RecordsWhatTheCloneDid(t *testing.T) {
	t.Parallel()
	running := leaseWarmLaunchMarker + warmLaunchRunning + "\n"
	exit := func(code string) string { return running + leaseWarmLaunchMarker + warmLaunchExit + " " + code + "\n" }
	tests := []struct {
		name    string
		answer  string
		err     error
		outcome string
		note    string
	}{
		{"finished", exit("0"), nil, LeaseWarmCloneDone, ""},
		{"left the work dir alone", exit(leaseWarmCloneNothingExit), nil, LeaseWarmCloneSkipped, "already holds a repository"},
		{"the clone failed", exit("128"), nil, LeaseWarmCloneFailed, "exit status 128"},
		{"the session ended while it ran", running, sshFailure("", platform.ExitStatusError(leaseSSHUnreachableExit)), LeaseWarmCloneFailed, "ended while the clone ran"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			orch := newWarmOrch(4)
			orch.add(3, "", true, false, 1)
			wf, ssh := newWarmCloneFixture(t, orch)
			wf.deps.Warm.LaunchRetry = time.Nanosecond
			wf.deps.HQAddress = func() string { return leaseHQURL }
			wf.deps.HQGit = relayedThrough{deps: &wf.deps}
			withPoolRepository(&wf.deps, testPoolHost, leaseHQRepo, true)
			ssh.setHandler(func(string, string) ([]byte, error) { return []byte(tt.answer), tt.err })

			if err := LeaseWarmCloneHeld(context.Background(), wf.deps, testPoolHost, "c-3"); err != nil {
				t.Fatal(err)
			}

			if launches := ssh.launches(); len(launches) != 1 || !strings.HasPrefix(launches[0].command, relayedMark) {
				t.Fatalf("launches = %+v, want one relayed session, never tried again once the clone ran", launches)
			}
			if clone := warmCloneOf(t, wf); clone == nil || clone.Outcome != tt.outcome || !strings.Contains(clone.Note, tt.note) {
				t.Errorf("status clone = %+v, want %s noting %q", clone, tt.outcome, tt.note)
			}
		})
	}
}

// LP-WARM-LOST — A held clone that still reads running past its bound never
// reported back (the process holding its session ended first, as at a
// restart): status says it failed, never that it runs and an acquire waits
// for it.
// witness: internal/ops/lease_warm_clone.go | d.now().Sub(at) <= warmCloneBound(true) { → d.now().Sub(at) <= 2*warmCloneBound(true) {
func TestLease_LPWarmLost_RunningPastItsBoundReadsFailed(t *testing.T) {
	t.Parallel()
	wf, _ := newWarmCloneFixture(t, newWarmOrch(4))
	started := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	now := started
	wf.deps.Now = func() time.Time { return now }
	wf.deps.Warm.recordClone(wf.deps, testPoolHost, LeaseWarmClone{Container: nodeHostname(3), Repo: leaseHQRepo, Outcome: LeaseWarmCloneRunning, Note: warmCloneHeldNote})

	now = started.Add(time.Minute)
	if clone := warmCloneOf(t, wf); clone == nil || clone.Outcome != LeaseWarmCloneRunning {
		t.Errorf("a minute in: status clone = %+v, want running", clone)
	}
	now = started.Add(warmCloneBound(true) + time.Second)
	if clone := warmCloneOf(t, wf); clone == nil || clone.Outcome != LeaseWarmCloneFailed || !strings.Contains(clone.Note, "never reported back") {
		t.Errorf("past its bound: status clone = %+v, want failed, never reported back", clone)
	}
}
