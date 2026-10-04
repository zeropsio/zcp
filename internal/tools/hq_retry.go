package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
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

// hqNotAnsweringLine says which step HQ at address could not serve, after how
// many tries, and the last answer (hqNotServingWords, gitNotServingWords).
func hqNotAnsweringLine(address, step string, tries int, last string) string {
	return fmt.Sprintf("HQ at %s is not answering: %s failed after %d tries (the last: %s)", address, step, tries, last)
}

// hqNotServingWords is the last answer of an API call HQ could not serve, in
// words that never read as a refusal: a 5xx is "HQ answered 502 (not
// serving)" — the client calls a 5xx other than 503 a RefusedError — and no
// answer at all says what stopped it.
func hqNotServingWords(err error) string {
	var (
		refused     *hq.RefusedError
		unavailable *hq.UnavailableError
	)
	switch {
	case errors.As(err, &refused) && refused.Status >= http.StatusInternalServerError:
		return notServing(refused.Status)
	case errors.As(err, &unavailable) && unavailable.Err == nil:
		return notServing(http.StatusServiceUnavailable)
	case errors.As(err, &unavailable):
		return "no answer (" + unavailable.Err.Error() + ")"
	}
	return err.Error()
}

// gitStatus5xx is the 5xx git reports a remote answered.
var gitStatus5xx = regexp.MustCompile(`returned error: (5\d\d)`)

// gitNotServingWords is hqNotServingWords for a git command against HQ: a 5xx
// git reports is HQ not serving, and anything else is git's own words.
func gitNotServingWords(err error, output []byte) string {
	if m := gitStatus5xx.FindSubmatch(output); m != nil {
		status, _ := strconv.Atoi(string(m[1]))
		return notServing(status)
	}
	return gitPushErrorDetail(err, output)
}

func notServing(status int) string { return fmt.Sprintf("HQ answered %d (not serving)", status) }

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
