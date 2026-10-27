// Tests for: ops/lease.go — a release waits for its branch's lock only so
// long (docs/spec-workflows.md §8 DP-26).
package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestLeaseRelease_GroupLockHeld_RefusedWithinItsBound: another call holds the
// branch's lock (a sibling's release across its own reset, an acquire's
// rewire). A release waits for it only GroupLockWait, then refuses — nothing
// released, the record kept — instead of waiting as long as its caller does.
func TestLeaseRelease_GroupLockHeld_RefusedWithinItsBound(t *testing.T) {
	t.Parallel()
	hf, _ := unsavedFixture(t, "", 0)
	hf.deps.GroupLockWait = 100 * time.Millisecond
	unlock, err := lockLeaseGroup(context.Background(), hf.stateDir, topology.CanonicalRepoURL(testRepo), "feature")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	_, err = LeaseRelease(ctx, hf.deps, "devpool-3", false)

	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("the release waited %s for the branch's lock, want about its 100ms bound", took)
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if !strings.Contains(pe.Message, "branch feature") || !strings.Contains(pe.Suggestion, "action=release lease=devpool-3") {
		t.Errorf("refusal %q / %q should name the busy branch and the release to run again", pe.Message, pe.Suggestion)
	}
	if len(hf.swarm.releases) != 0 {
		t.Errorf("a refused release reached the orchestrator: %+v", hf.swarm.releases)
	}
	if _, statErr := os.Stat(filepath.Join(hf.stateDir, "leases", "devpool-3.json")); statErr != nil {
		t.Errorf("record gone after a refused release: %v", statErr)
	}
}
