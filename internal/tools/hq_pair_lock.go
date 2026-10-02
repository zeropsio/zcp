package tools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/workflow"
)

// One process at a time runs git on a pair's checkout (workflow.LockPair):
// the zcp of an agent delivering, pushing or standing a pair up waits for it
// (holdPairCheckout); a pass and `zcp service mate` finishing an owed
// delivery leave a pair held elsewhere to the next round. Each takes it once,
// at its outer step, keyed by the pair's record (its dev hostname) — never
// again beneath, where it would wait on itself.

// hqPairLockWait bounds how long an agent's step waits for its pair's
// checkout; a var so tests shorten it.
var hqPairLockWait = 2 * time.Minute

// holdPairCheckout is an agent's step taking its pair's checkout, waiting for
// another process to let go of it up to hqPairLockWait.
func holdPairCheckout(ctx context.Context, stateDir, hostname string) (release func(), err error) {
	return workflow.LockPair(ctx, stateDir, hostname, hqPairLockWait)
}

// pairHeldReason is why an agent's step did not get its pair's checkout.
func pairHeldReason(hostname string, err error) string {
	if errors.Is(err, workflow.ErrPairBusy) {
		return fmt.Sprintf("another delivery of %s held its checkout for %s (a pass, or zcp service mate finishing a delivery HQ could not be reached for)", hostname, hqPairLockWait)
	}
	return fmt.Sprintf("its checkout could not be taken (%v)", err)
}

// pairKey is the hostname a pair's checkout is held by: its record's, so the
// stage half's deploy and the dev half's push hold the same one.
func pairKey(stateDir, hostname string) string {
	if m, _ := workflow.FindServiceMeta(stateDir, hostname); m != nil {
		return m.Hostname
	}
	return hostname
}
