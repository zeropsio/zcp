package ops

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// mainHoldsFixture is a leased devpool-3 whose checkout counts one commit on
// no remote branch however often origin is fetched — its branch was merged as
// a squash, so main holds its change under another commit — and whose check
// of main answers held.
func mainHoldsFixture(t *testing.T, held bool) *hostFixture {
	t.Helper()
	hf := newHostFixture(t)
	var (
		mu    sync.Mutex
		armed bool
	)
	ssh := &leaseSSH{fn: func(_, command string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case !armed:
			return nil, nil
		case strings.Contains(command, "apply --cached --check --reverse"):
			if held {
				return []byte(leaseMainHoldsMark + "\n"), nil
			}
			return nil, nil
		case strings.Contains(command, "rev-list --count"):
			return []byte("1\n"), nil
		}
		return nil, nil
	}}
	hf.deps.SSH = ssh
	hf.swarm.acquired = acquiredAs(3)
	if _, err := LeaseAcquire(context.Background(), hf.deps, testPoolHost, testRepo, "feature"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	ssh.reset()
	hf.events = nil
	mu.Lock()
	armed = true
	mu.Unlock()
	return hf
}

// TestLeaseRelease_CommitsMainHoldsByContent_Released: a branch merged as a
// squash leaves its commits on no branch of origin while main holds what they
// change, so counted by commit the release refused forever, HQ's merge pass
// included. Where main holds every unpushed commit's change the release goes
// ahead and says why; where it does not, it is refused as before.
// witness: internal/ops/lease_checkout.go | if w.unpushed > 0 && mainHoldsUnpushed(ctx, d.SSH, rec.alias()) { → if false {
func TestLeaseRelease_CommitsMainHoldsByContent_Released(t *testing.T) {
	t.Parallel()
	t.Run("main holds their change: released, saying why", func(t *testing.T) {
		t.Parallel()
		hf := mainHoldsFixture(t, true)
		view, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)
		if err != nil {
			t.Fatalf("release of a lease whose change main holds: %v", err)
		}
		if len(hf.swarm.releases) != 1 {
			t.Errorf("releases = %+v, want the release", hf.swarm.releases)
		}
		if !strings.Contains(view.Note, "main already holds") {
			t.Errorf("note %q does not say main holds their change", view.Note)
		}
	})
	t.Run("main lacks their change: refused", func(t *testing.T) {
		t.Parallel()
		hf := mainHoldsFixture(t, false)
		_, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)
		_ = assertRefusedAndKept(t, hf, err)
	})
}
