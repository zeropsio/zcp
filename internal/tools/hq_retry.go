package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
)

// The tries of one call against HQ: how many, how far apart, and what the
// last answer of a call HQ could not serve says (a describe is the one caller,
// hq_describe_retry.go).

// hqAnswer is what one try of a call against HQ met.
type hqAnswer int

const (
	// hqAnswered: HQ served the try, or refused it — final either way.
	hqAnswered hqAnswer = iota
	// hqNotServing: HQ could not serve the try; tried again.
	hqNotServing
)

// hqRetry paces the tries of one call against HQ: one try more than it has
// waits.
type hqRetry struct {
	waits []time.Duration
	pause func(context.Context, time.Duration) error
}

// hqConnectBound bounds each try's connect to HQ, TLS included
// (hq.Client.Bounded).
const hqConnectBound = 5 * time.Second

// run calls try until HQ answers it — served or refused — or the tries are
// spent, or ctx ends during a wait. tries is how many ran.
func (r hqRetry) run(ctx context.Context, try func() hqAnswer) (tries int) {
	for {
		tries++
		if try() != hqNotServing || tries > len(r.waits) {
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

// hqNotServingWords is the last answer of an API call HQ could not serve, in
// words that never read as a refusal: a 5xx is "HQ answered 502 (not
// serving)" — the client calls a 5xx other than 503 a RefusedError — a bound
// that ended the try names itself, and no answer at all says what stopped it.
func hqNotServingWords(err error) string {
	var (
		refused     *hq.RefusedError
		noAnswer    *hq.NoAnswerError
		unavailable *hq.UnavailableError
	)
	switch {
	case errors.As(err, &noAnswer):
		return noAnswer.Words()
	case errors.As(err, &refused) && refused.Status >= http.StatusInternalServerError:
		return notServing(refused.Status)
	case errors.As(err, &unavailable) && unavailable.Err == nil:
		return notServing(http.StatusServiceUnavailable)
	case errors.As(err, &unavailable):
		return "no answer (" + unavailable.Err.Error() + ")"
	}
	return err.Error()
}

func notServing(status int) string { return fmt.Sprintf("HQ answered %d (not serving)", status) }
