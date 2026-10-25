// Tests for: ops/lease.go — a held-by-other answer to a release drops the
// record only once a renew agrees, as for a stop, a start and a delete
// (docs/spec-workflows.md §8 DP-2, DP-4).
package ops

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestLeaseRelease_HeldByOtherButRenewSaysOurs_RecordKept: the record holds
// the only copy of the lease string, so a drop on a held-by-other answer the
// renew does not confirm — what the orchestrator answers its own holder while
// a concurrent park stops the container is not live-verified — would leave the
// container reserved with no expiry and no one able to release it. The renew
// still gives zcp the container: the release is refused and the record kept.
func TestLeaseRelease_HeldByOtherButRenewSaysOurs_RecordKept(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	fx.swarm.acquired = acquiredAs(3)
	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	fx.swarm.containers = []platform.SwarmContainer{reservedContainer("c-abc", 3)}
	fx.swarm.releaseErr = &platform.SwarmError{Status: 409, Code: "containerAction", Reason: platform.SwarmReasonHeldByOther,
		Message: "the container is reserved by another holder"}

	v, err := LeaseRelease(context.Background(), fx.deps, "devpool-3", true)

	if err == nil {
		t.Errorf("release = %+v, want a refusal: the renew still gives zcp the container", v)
	}
	if _, statErr := os.Stat(filepath.Join(fx.stateDir, "leases", "devpool-3.json")); statErr != nil {
		t.Errorf("the record — the only copy of the lease — was dropped (%v)", statErr)
	}
}
