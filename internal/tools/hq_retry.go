package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
)

// A delivery fails fast (spec-mate §10.10; the owner, 2026-10-04: HQ should
// always answer, and when it does not the problem is bigger — waiting it out
// only masks it). Each step of a delivery HQ serves — taking `main` in,
// opening the change, taking its branch in, pushing it — is tried at most
// three times, a short wait apart, and only while HQ could not serve it: not
// reached at all, or a 5xx. A refusal fails at once. Nothing tries again once
// the call has returned: the work stays committed in the pair's checkout, and
// the next delivery sends it.

// hqRetry paces the tries of one delivery step against HQ: one try more than
// it has waits.
type hqRetry struct {
	waits []time.Duration
	pause func(context.Context, time.Duration) error
}

// deliveryRetry is how every delivery step is tried: three tries, 1 s and
// 3 s apart. A var so tests do not wait it out.
var deliveryRetry = hqRetry{waits: []time.Duration{time.Second, 3 * time.Second}, pause: waitFor}

// run calls try until it answers that HQ served it — or refused it — or the
// tries are spent, or ctx ends during a wait. try answers whether HQ could
// not serve it. tries is how many ran.
func (r hqRetry) run(ctx context.Context, try func() (unavailable bool)) (tries int) {
	for {
		tries++
		if !try() || tries > len(r.waits) {
			return tries
		}
		if r.pause(ctx, r.waits[tries-1]) != nil {
			return tries
		}
	}
}

// waitFor waits d, or until ctx ends.
func waitFor(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// hqUnavailable reports whether an answer from HQ's API is HQ unable to serve
// the call — not reached, a standby's 503, or any other 5xx, the balancer's
// 502 for an HQ that is down among them — rather than HQ's own refusal.
func hqUnavailable(err error) bool {
	var refused *hq.RefusedError
	return hq.IsUnavailable(err) || errors.As(err, &refused) && refused.Status >= http.StatusInternalServerError
}

// hqUnreachableLine says which step HQ at address could not serve, after how
// many tries, and the last answer.
func hqUnreachableLine(address, step string, tries int, last string) string {
	return fmt.Sprintf("HQ at %s could not be reached to %s (%d tries; the last: %s)", address, step, tries, last)
}

// hqNotAnswering is what follows a delivery HQ could not serve: where the
// work is, the one way it goes on, and that the person hears of an HQ not
// answering rather than anyone waiting it out. again is the act that
// delivers it ("deploying appstage again").
func hqNotAnswering(hostname, again string) string {
	return fmt.Sprintf("Nothing tries it again in the background: the work stays committed in %s's checkout, and %s delivers it. HQ is meant to always answer — tell the person HQ is not answering, so it is looked into.", hostname, again)
}

// logDeliveryFailure puts one line on stderr for a delivery that did not
// reach HQ, so an HQ answering badly shows beside the agent's own output.
func logDeliveryFailure(hostname, line string) {
	fmt.Fprintf(os.Stderr, "zcp: hq: delivery of %s failed: %s\n", hostname, line)
}
