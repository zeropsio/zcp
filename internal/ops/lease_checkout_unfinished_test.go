// Tests for: ops/lease_group.go — a lease whose checkout never finished is
// never handed out as reused (docs/spec-workflows.md §8 DP-17).
package ops

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestLeaseAcquire_UnfinishedCheckoutKept_NotReused: an acquire's checkout
// fails and the orchestrator refuses the hand-back, so the record stays — on
// a container whose /var/www never reached the branch. The next acquire of
// the same key finds that lease reserved; it is not on the branch, so it is
// never answered reused (an agent told to bind nothing would work off the
// wrong tree): the acquire is refused, naming the release that ends it, and
// nothing is reserved anew.
func TestLeaseAcquire_UnfinishedCheckoutKept_NotReused(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	fx.swarm.acquired = acquiredAs(3)
	fx.swarm.releaseErr = &platform.SwarmError{Status: http.StatusConflict, Code: "containerAction", Message: "busy", Reason: "container-busy", Retryable: true}
	var mu sync.Mutex
	failCheckout := true
	fx.deps.SSH = &leaseSSH{fn: func(_ string, cmd string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if failCheckout && strings.Contains(cmd, "fetch -q --prune origin") {
			out := "fatal: could not read Username for 'https://github.com': terminal prompts disabled"
			return []byte(out), sshFailure(out, exitError(t, 128))
		}
		return nil, nil
	}}
	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature"); err == nil {
		t.Fatal("first acquire: want the checkout's refusal")
	}
	// The container stays reserved and its record kept.
	fx.swarm.containers = []platform.SwarmContainer{reservedContainer("c-abc", 3)}
	fx.swarm.releaseErr = nil
	mu.Lock()
	failCheckout = false
	mu.Unlock()
	reqs := len(fx.swarm.acquireReqs)

	v, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")

	if err == nil {
		t.Fatalf("second acquire = %+v (reused=%v), want a refusal: its checkout never finished", v, v.Reused)
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
		t.Fatalf("second acquire error = %v, want PREREQUISITE_MISSING", err)
	}
	if !strings.Contains(pe.Suggestion, "action=release lease=devpool-3") {
		t.Errorf("suggestion %q does not name the release of devpool-3", pe.Suggestion)
	}
	if got := len(fx.swarm.acquireReqs); got != reqs {
		t.Errorf("acquire requests = %d after the refusal, want %d: nothing is reserved anew", got, reqs)
	}
}
