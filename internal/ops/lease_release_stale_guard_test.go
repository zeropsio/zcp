// Tests for: ops/lease.go, ops/lease_group.go — a release reads the
// container for unsaved work again once it holds its branch's lock, right
// before the reset, and waits for that lock only so long
// (docs/spec-workflows.md §8 DP-4, DP-26).
package ops

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// lateWorkHandler answers the guard's git status clean on its first read of
// host and with late.txt untracked from then on — work that landed after the
// first guard read, while the release waited for its branch's lock and rewired
// the siblings. Every other host, and the unpushed count, reads clean.
func lateWorkHandler(host string) func(string, string) ([]byte, error) {
	var mu sync.Mutex
	reads := 0
	return func(h, command string) ([]byte, error) {
		switch {
		case strings.Contains(command, "status --porcelain"):
			if h != host {
				return nil, nil
			}
			mu.Lock()
			defer mu.Unlock()
			reads++
			if reads == 1 {
				return nil, nil
			}
			return []byte("?? late.txt\n"), nil
		case strings.Contains(command, "rev-list --count"):
			return []byte("0\n"), nil
		}
		return nil, nil
	}
}

// TestLeaseRelease_WorkLandsAfterTheGuard_Refused: a second conversation
// sharing the lease (reused), or the agent's own parallel call, writes in the
// container after the guard read it clean. The release reads it again under
// its branch's lock, right before the reset, and refuses: the record, the
// mount and the reservation stay.
func TestLeaseRelease_WorkLandsAfterTheGuard_Refused(t *testing.T) {
	t.Parallel()
	hf, ssh := unsavedFixture(t, "", 0)
	ssh.setHandler(lateWorkHandler("devpool-3"))

	_, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)

	pe := assertRefusedAndKept(t, hf, err)
	if !strings.Contains(pe.Message, "late.txt") {
		t.Errorf("message %q should name the file that landed after the first read", pe.Message)
	}
}

// TestLeaseReleaseGroup_WorkLandsAfterTheGuards_ReleasesNone: a branch's
// release reads every lease again once it holds the branch's lock, before any
// is released; one that holds work by then refuses the whole branch.
func TestLeaseReleaseGroup_WorkLandsAfterTheGuards_ReleasesNone(t *testing.T) {
	t.Parallel()
	gf := newGroupFixture(t)
	gf.heldGroup(t)
	gf.ssh.setHandler(lateWorkHandler("webpool-7"))

	views, err := LeaseReleaseGroup(context.Background(), gf.deps, "", "feat", false)

	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
		t.Fatalf("views %+v, err = %v; want a PREREQUISITE_MISSING refusal", views, err)
	}
	if !strings.Contains(pe.Message, "late.txt") {
		t.Errorf("message %q should name the file that landed after the first read", pe.Message)
	}
	for name, o := range map[string]*poolOrch{"hqpool": gf.hq, "webpool": gf.web} {
		if rel := o.releaseLog(); len(rel) != 0 {
			t.Errorf("%s releases = %+v: a refused branch releases nothing", name, rel)
		}
	}
	if recs := gf.records(t); len(recs) != 2 {
		t.Errorf("records = %v, want both kept", recs)
	}
}
