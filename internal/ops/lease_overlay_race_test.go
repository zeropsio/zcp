// Tests for: every overlay zcp sends a lease — a dev-server start, an
// acquire's own supervisor step, an unpark, a sync's take of a changed
// declaration — is computed from zcp's records and sent under its group's
// lock, so a sibling's release, which holds that lock from its rewire through
// the drop of its record, never drops the sibling between the computation and
// the send (docs/spec-workflows.md §8 DP-22, DP-26); and status names a
// container its unknown holder is parking or unparking (DP-2).
package ops

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// siblingReleaseRace is ssh into the rewire fixture's leases that, the first
// time a command sends webpool-7 an overlay naming hqpool-3's container,
// releases hqpool-3 first — forced, through the fixture's own deps — and waits
// for that release for up to 2 s before the send goes on. Without the group's
// lock the release rewires webpool-7 and drops hqpool-3 before the send lands;
// with it, the release waits for the send and then rewires webpool-7 itself.
type siblingReleaseRace struct {
	SSHDeployer
	rf   *rewireFixture
	once sync.Once
	done chan error

	mu    sync.Mutex
	fired bool
}

func newSiblingReleaseRace(rf *rewireFixture, ssh SSHDeployer) *siblingReleaseRace {
	return &siblingReleaseRace{SSHDeployer: ssh, rf: rf, done: make(chan error, 1)}
}

func (r *siblingReleaseRace) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	if host == ownWebAlias && strings.Contains(command, "ctl 'env'") && strings.Contains(command, "HQ_HOST="+rewireHQHost) {
		r.once.Do(func() {
			r.mu.Lock()
			r.fired = true
			r.mu.Unlock()
			go func() {
				// The release outlives the command that set it off.
				_, err := LeaseReleaseChange(context.WithoutCancel(ctx), r.rf.deps, ownHQAlias, true)
				r.done <- err
			}()
			select {
			case err := <-r.done:
				r.done <- err
			case <-time.After(2 * time.Second):
				// The release waits for the group's lock this send holds.
			}
		})
	}
	return r.SSHDeployer.ExecSSH(ctx, host, command)
}

// ExecSSHStdin and ExecSSHBackground go through ExecSSH, so every command is
// seen.
func (r *siblingReleaseRace) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return r.ExecSSH(ctx, host, command)
}

func (r *siblingReleaseRace) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return r.ExecSSH(ctx, host, command)
}

// settle waits for the release the race started and checks that webpool-7's
// servers are wired to nothing hqpool-3's container was.
func (r *siblingReleaseRace) settle(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	fired := r.fired
	r.mu.Unlock()
	if !fired {
		t.Fatal("no overlay naming hqpool-3's container was sent to webpool-7: the race never ran")
	}
	select {
	case err := <-r.done:
		if err != nil {
			t.Errorf("release %s: %v", ownHQAlias, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("release %s did not return", ownHQAlias)
	}
	for name, value := range r.rf.web.Overlay() {
		if value == rewireHQHost {
			t.Errorf("webpool-7's servers run with %s=%s, the container released with hqpool-3's lease (overlay %v)", name, value, r.rf.web.Overlay())
		}
	}
}

// TestDevServerStart_SiblingReleasedMeanwhile_NeverWiredToIt: a dev-server
// start on a lease computes its overlay from zcp's records and sends it before
// the start. A release of a sibling between the two — its rewire sent and its
// record dropped — must not be followed by the start's overlay naming the
// released container (DP-22, DP-26).
func TestDevServerStart_SiblingReleasedMeanwhile_NeverWiredToIt(t *testing.T) {
	t.Parallel()
	rf := newRewireFixture(t)
	rf.acquire(t, groupHQPool, groupWebPool)
	race := newSiblingReleaseRace(rf, rf.ssh)

	if _, err := ExecuteDevServer(context.Background(), race, rf.deps.Client, "p1",
		DevServerParams{Action: "start", Hostname: ownWebAlias, Name: "web", StateDir: rf.stateDir}); err != nil {
		t.Fatalf("start: %v", err)
	}
	race.settle(t)
}

// TestLeaseAcquire_SiblingReleasedDuringTheSupervisorStep_NeverWiredToIt: an
// acquire's supervisor step hands the new lease its group's overlay. A
// release of a sibling between the overlay's computation and its send — the
// release's rewire of the new lease sent, its record dropped — must not be
// followed by the step's overlay naming the released container (DP-22,
// DP-26).
func TestLeaseAcquire_SiblingReleasedDuringTheSupervisorStep_NeverWiredToIt(t *testing.T) {
	t.Parallel()
	rf := newRewireFixture(t)
	rf.acquire(t, groupHQPool)
	race := newSiblingReleaseRace(rf, rf.ssh)
	d := rf.deps
	d.SSH = race

	if _, err := LeaseAcquireGroupChange(context.Background(), d, []string{groupWebPool}, testRepo, "feat"); err != nil {
		t.Fatalf("acquire webpool: %v", err)
	}
	race.settle(t)
}

// TestLeaseUnpark_SiblingReleasedMeanwhile_NeverWiredToIt: an unpark hands
// the lease's supervisor its group's overlay as it stands. A release of a
// sibling between the overlay's computation and its send must not be followed
// by the unpark's overlay naming the released container (DP-26, DP-28).
func TestLeaseUnpark_SiblingReleasedMeanwhile_NeverWiredToIt(t *testing.T) {
	t.Parallel()
	rf := newRewireFixture(t)
	rf.acquire(t, groupHQPool, groupWebPool)
	if _, err := LeasePark(context.Background(), rf.deps, ownWebAlias); err != nil {
		t.Fatalf("park %s: %v", ownWebAlias, err)
	}
	race := newSiblingReleaseRace(rf, rf.ssh)
	d := rf.deps
	d.SSH = race

	if _, err := LeaseUnpark(context.Background(), d, ownWebAlias); err != nil {
		t.Fatalf("unpark %s: %v", ownWebAlias, err)
	}
	race.settle(t)
}

// syncScripted answers a sync's push and fast-forward as the sync tests'
// remote does — the fast-forward changing the declaration — and every other
// command through the leases' own ssh.
type syncScripted struct {
	SSHDeployer
}

func (s syncScripted) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	switch {
	case strings.Contains(command, "push --porcelain"):
		return []byte(syncPushed("feat", syncTo)), nil
	case strings.Contains(command, "merge --ff-only"):
		return []byte(syncMemberFastForwarded(syncFrom, syncTo) + "@@zcp-sync declaration\n"), nil
	}
	return s.SSHDeployer.ExecSSH(ctx, host, command)
}

// TestLeaseSync_SiblingReleasedDuringTheTake_NeverWiredToIt: a sync whose
// fast-forward changed a lease's declaration hands that lease's supervisor its
// group's overlay before the reload. A release of a sibling between the
// overlay's computation and its send must not be followed by the take's
// overlay naming the released container (DP-24, DP-26).
func TestLeaseSync_SiblingReleasedDuringTheTake_NeverWiredToIt(t *testing.T) {
	t.Parallel()
	rf := newRewireFixture(t)
	rf.acquire(t, groupHQPool, groupWebPool)
	// webpool-7's supervisor runs with another overlay, so the take sends one.
	if _, err := supervisorCtl(context.Background(), rf.web, ownWebAlias, "env", "--clear"); err != nil {
		t.Fatal(err)
	}
	race := newSiblingReleaseRace(rf, syncScripted{SSHDeployer: rf.ssh})
	d := rf.deps
	d.SSH = race

	v, err := LeaseSync(context.Background(), d, ownHQAlias)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if m := syncMemberOf(t, v, ownWebAlias); m.Outcome != LeaseSyncFastForwarded {
		t.Fatalf("member = %+v, want fast-forwarded", m)
	}
	race.settle(t)
}

// TestLeaseStatus_UnknownHolderParkingItsContainer_Named: a container listed
// stopping or starting — its holder parking or unparking it, under its
// reservation — that zcp holds no record of is another holder's: status
// names it unknown-holder, as it does a reserved one (DP-2).
func TestLeaseStatus_UnknownHolderParkingItsContainer_Named(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	parking, unparking := reservedContainer("c-7", 7), reservedContainer("c-8", 8)
	parking.LockState, unparking.LockState = leaseLockStopping, leaseLockStarting
	fx.swarm.containers = []platform.SwarmContainer{parking, unparking}

	got, err := LeaseList(context.Background(), fx.deps, testPoolHost)
	if err != nil {
		t.Fatalf("LeaseList: %v", err)
	}
	states := map[string]string{}
	for _, v := range got {
		states[v.Alias] = v.State
	}
	for _, alias := range []string{"devpool-7", "devpool-8"} {
		if states[alias] != LeaseStateUnknownHolder {
			t.Errorf("%s = %q, want unknown-holder (views %+v)", alias, states[alias], got)
		}
	}
}
