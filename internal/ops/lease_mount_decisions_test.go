package ops

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// These are compatibility controls for W2: ownership and read-only status
// remain separate from the mounter's bounded observation of usability.
func TestLeaseMount_Decisions(t *testing.T) {
	t.Parallel()
	t.Run("Decision: A reservation lost during a wake wait is never mounted or supervised.", func(t *testing.T) {
		t.Parallel()
		for _, loss := range []string{"another holder", "free", "gone"} {
			t.Run(loss, func(t *testing.T) {
				t.Parallel()
				pf := newParkFixture(t, true)
				pf.deps.SSH = &handedOverOnReady{parkSSH: pf.ssh, handOff: func() {
					switch loss {
					case "another holder":
						pf.swarm.foreign = map[string]bool{"c-3": true}
					case "free":
						pf.swarm.containers[0].LockState = ""
					case "gone":
						pf.swarm.containers = nil
					}
				}}

				view, err := LeaseUnpark(context.Background(), pf.deps, "devpool-3")
				if err != nil {
					t.Fatalf("LeaseUnpark: %v", err)
				}
				if len(pf.swarm.starts) != 1 || view.State != LeaseStateLost {
					t.Fatalf("starts = %v, state = %s; want one wake followed by lost ownership", pf.swarm.starts, view.State)
				}
				if slices.Contains(pf.events, "mount") || slices.Contains(pf.events, "supervisor") || pf.ssh.c.SupervisorStarts != 0 {
					t.Errorf("events = %v, supervisor starts = %d; want no mount or supervisor effect after the wake receipt", pf.events, pf.ssh.c.SupervisorStarts)
				}
				if pf.mounter.states["/var/www/devpool-3"] == platform.MountStateActive {
					t.Error("lost reservation has an active mount")
				}
			})
		}
	})
	t.Run("Decision: Reading a lease's mount status never writes through that mount.", func(t *testing.T) {
		t.Parallel()
		for _, state := range []string{"active", "stale", "absent", "pending", "unreadable"} {
			t.Run(state, func(t *testing.T) {
				t.Parallel()
				for _, hostname := range []string{"apppool-3", ""} {
					mock := platform.NewMock().WithServices(testServices())
					mounter := &probeWatch{mockMounter: newMockMounter()}
					mounter.mountDirs = []string{"app", "apppool-3"}
					mounter.states["/var/www/app"] = platform.MountStateActive
					switch state {
					case "active":
						mounter.states["/var/www/apppool-3"] = platform.MountStateActive
					case "stale":
						mounter.states["/var/www/apppool-3"] = platform.MountStateStale
					case "pending":
						mounter.units["apppool-3"] = true
					case "unreadable":
						mounter.checkErr = errors.New("mount observation did not answer")
					}
					_, err := MountStatus(context.Background(), mock, "proj-1", mounter, hostname)
					if state == "unreadable" {
						if !errors.Is(err, mounter.checkErr) {
							t.Errorf("MountStatus(%q) error = %v, want unreadable observation", hostname, err)
						}
					} else if err != nil {
						t.Fatalf("MountStatus(%q): %v", hostname, err)
					}
					if slices.Contains(mounter.probed, "/var/www/apppool-3") {
						t.Errorf("MountStatus(%q) wrote through a lease mount: %v", hostname, mounter.probed)
					}
					if hostname == "" && state != "unreadable" && !slices.Contains(mounter.probed, "/var/www/app") {
						t.Errorf("service control was not write-probed: %v", mounter.probed)
					}
				}
			})
		}
	})
}
