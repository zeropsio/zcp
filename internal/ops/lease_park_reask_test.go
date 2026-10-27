// Tests for: ops/lease_park.go — park and unpark ask the orchestrator again
// before they mount a container or start its supervisor after a wait or a
// refused stop (docs/spec-workflows.md §8 DP-2, DP-28).
package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// handedOverOnReady is parkSSH whose first readiness answer comes once the
// pool handed the container to another holder: the lease ended during the
// unpark's ssh wait.
type handedOverOnReady struct {
	*parkSSH
	once    sync.Once
	handOff func()
}

func (s *handedOverOnReady) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	if command == "true" {
		s.once.Do(s.handOff)
	}
	return s.parkSSH.ExecSSH(ctx, host, command)
}

func (s *handedOverOnReady) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *handedOverOnReady) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

// TestLeaseUnpark_LeaseEndsDuringTheSSHWait_NeitherMountedNorSupervised: the
// unpark waits up to a minute for the started container's ssh, which can
// outlast the lease. It asks the orchestrator again before it mounts the
// container or starts a supervisor there: another holder's container is
// never mounted or supervised, and the record is dropped as lost.
func TestLeaseUnpark_LeaseEndsDuringTheSSHWait_NeitherMountedNorSupervised(t *testing.T) {
	t.Parallel()
	pf := newParkFixture(t, true)
	pf.swarm.foreign = map[string]bool{}
	pf.deps.SSH = &handedOverOnReady{parkSSH: pf.ssh, handOff: func() { pf.swarm.foreign["c-3"] = true }}

	v, err := LeaseUnpark(context.Background(), pf.deps, "devpool-3")
	if err != nil {
		t.Fatalf("LeaseUnpark: %v", err)
	}

	if slices.Contains(pf.events, "mount") || pf.mounter.states["/var/www/devpool-3"] == platform.MountStateActive {
		t.Errorf("events = %v: another holder's container was mounted", pf.events)
	}
	if slices.Contains(pf.events, "supervisor") {
		t.Errorf("events = %v: a supervisor was ensured in another holder's container", pf.events)
	}
	if v.State != LeaseStateLost {
		t.Errorf("view = %+v, want the lease reported lost", v)
	}
	if _, statErr := os.Stat(filepath.Join(pf.stateDir, "leases", "devpool-3.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("the record of a lease that ended survives (%v)", statErr)
	}
}

// raceStopSwarm refuses the stop unknown-lease, as the orchestrator does when
// a release of the lease raced the renew before it: the container is no
// longer the lease's.
type raceStopSwarm struct{ powerSwarm }

func (s raceStopSwarm) Stop(ctx context.Context, id, lease string) (platform.SwarmContainer, error) {
	s.foreign[id] = true
	return s.powerSwarm.Stop(ctx, id, lease)
}

// TestLeasePark_StopRefusedUnknownLease_NeverRemounted: a stop refused for
// another reason than held-by-other says nothing of whose the container is.
// Before the park puts the mount back it asks again; a lease that ended is
// dropped as lost, its container never mounted again.
func TestLeasePark_StopRefusedUnknownLease_NeverRemounted(t *testing.T) {
	t.Parallel()
	pf := newParkFixture(t, false)
	pf.swarm.foreign = map[string]bool{}
	pf.swarm.stopErr = &platform.SwarmError{Status: 400, Code: "containerAction", Reason: platform.SwarmReasonUnknownLease,
		Message: "no reservation of this container is held by that lease - acquire it again"}
	swarm := raceStopSwarm{powerSwarm{orderedSwarm{fakeSwarm: pf.swarm, events: &pf.events}}}
	pf.deps.Swarm = func(string, string) SwarmAPI { return swarm }

	v, err := LeasePark(context.Background(), pf.deps, "devpool-3")

	if pf.mounter.states["/var/www/devpool-3"] == platform.MountStateActive {
		t.Errorf("events = %v (view %+v, err %v): a container no longer the lease's was mounted again", pf.events, v, err)
	}
	if err != nil || v.State != LeaseStateLost {
		t.Errorf("view = %+v, err = %v; want the lease reported lost", v, err)
	}
}
