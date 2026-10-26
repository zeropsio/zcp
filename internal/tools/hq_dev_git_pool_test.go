// Tests for: tools/hq_dev_git.go and hq_wiring.go — keeping a wired pair on
// this Mate's HQ when its dev half is a dev pool (docs/spec-workflows.md §8
// PS-3, §13 PM-10): no checkout of its own to move, its HQ credential renewed
// on the pool.
package tools

import (
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

// TestDevGit_PoolKeyingAPairWiredAtARetiredHQ_MovesOnlyTheRecord: a pair
// keyed by its dev pool whose record names its repository on an HQ the Mate
// no longer links to is moved by the Mate's start and by the repository pass
// as a dev service's pair is — but the pool's hostname reaches a random
// container of it, possibly someone's lease, and holds no checkout: nothing
// runs over ssh there, and only the record follows to the current HQ.
func TestDevGit_PoolKeyingAPairWiredAtARetiredHQ_MovesOnlyTheRecord(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		run  func(l *hqLab, ssh *hostLogSSH)
	}{
		{name: "the Mate's start", run: func(l *hqLab, ssh *hostLogSSH) {
			startDevGit(l.t.Context(), l.mock, l.hq.srv.Client(), ssh, l.rt, l.stateDir)
		}},
		{name: "the repository pass", run: func(l *hqLab, ssh *hostLogSSH) {
			reconcileHQRepositories(l.t.Context(), l.mock, l.hq.srv.Client(), ssh, l.rt, l.stateDir, hqPassNow)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lab := newHQLab(t)
			lab.wire()
			current := lab.git("remote", "get-url", "origin")
			ssh := lab.keyPairOnPool()
			retired := retiredHQ + "/git/" + labApp + "/appdev.git"
			if err := workflow.UpdateServiceMeta(lab.stateDir, testPool, func(m *workflow.ServiceMeta) error {
				m.RemoteURL = retired
				return nil
			}); err != nil {
				t.Fatal(err)
			}

			tc.run(lab, ssh)

			if n := ssh.reached(testPool); n != 0 {
				t.Errorf("the move reached the pool %s over ssh %d time(s)", testPool, n)
			}
			if got := lab.poolMeta().RemoteURL; got != current {
				t.Errorf("the pool-keyed pair's record names %q, want its repository on the current HQ %q", got, current)
			}
		})
	}
}
