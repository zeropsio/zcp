package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A describe is the Mate's own call, and the change stays a draft until it
// lands, so — unlike a delivery, which fails fast (hq_retry.go) — it waits out
// an HQ that is only away for a moment: HQ's Core rolls for about 20 s, and a
// describe that met the roll once left the change a draft. Its calls to HQ
// are tried again, a growing wait apart for about a minute in all, while HQ
// could not be reached — refused, reset, no connection within the bound — or
// answered 502, 503 or 504. HQ's own refusal, any 4xx or a 500, is final, and
// so is a try HQ connected for and then left unanswered past its bound.

// describeRetry paces a describe's tries: eight, the waits between them
// adding up to a minute. A var so tests do not wait it out.
var describeRetry = hqRetry{
	waits: []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second, 15 * time.Second, 15 * time.Second},
	pause: waitFor,
}

// putDescriptionRetried is putChangeDescription as describe-change calls it:
// tried as describeRetry paces it while HQ is away (describeTransient), each
// call sent once — no 503 wait of the client's own stacks beneath the tries —
// and bounded by deliveryBounds' connect and hqCallTimeout's answer. Every
// try puts the same words, and a picture already attached is not attached
// again, so a try again changes nothing a landed one did. away reports whether
// the error that ended it is HQ still away after its tries.
func putDescriptionRetried(ctx context.Context, hqc hq.Client, stateDir string, m *workflow.ServiceMeta, number int, text, title string) (tries int, away bool, err error) {
	client := hqc.Once().Bounded(deliveryBounds.connect, hqCallTimeout)
	tries = describeRetry.run(ctx, func() hqAnswer {
		err = putChangeDescription(ctx, client, stateDir, m, number, text, title)
		if describeTransient(err) {
			return hqNotServing
		}
		return hqAnswered
	})
	return tries, describeTransient(err), err
}

// describeTransient reports whether err is HQ away for a moment, as a rolling
// deploy leaves it: not reached at once, or a 502, 503 or 504 — never a
// refusal of its own, nor a try it connected for and left unanswered.
func describeTransient(err error) bool {
	var (
		noAnswer *hq.NoAnswerError
		refused  *hq.RefusedError
	)
	switch {
	case err == nil:
		return false
	case errors.As(err, &noAnswer):
		return !noAnswer.Connected
	case errors.As(err, &refused):
		return refused.Status == http.StatusBadGateway || refused.Status == http.StatusServiceUnavailable || refused.Status == http.StatusGatewayTimeout
	}
	return hq.IsUnavailable(err)
}

// describeNotLanded is describe-change's answer when HQ stayed away through
// every try: the words did not land, the change is still a draft, and they
// are kept for it.
func describeNotLanded(address string, number, tries int, err error) string {
	return fmt.Sprintf("The description did not land: HQ at %s did not answer for about a minute (%d tries; the last: %s). "+
		"Change #%d is still a draft — the person is not asked to review it. The words are kept for it: "+
		`call describe-change again to put them on, and if HQ still does not answer, tell the person HQ is not answering.`,
		address, tries, hqNotServingWords(err), number)
}
