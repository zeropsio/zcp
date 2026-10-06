package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
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

// describeAway is what a describe whose tries HQ never served knows of its
// words.
type describeAway int

const (
	// describeServed: HQ served the describe or refused it — not away.
	describeServed describeAway = iota
	// describeNotReached: no try reached HQ, so the words did not land.
	describeNotReached
	// describeUnconfirmed: a try may have reached HQ — a connection dropped
	// after the words were sent, a balancer's 502 or 504 — and HQ never
	// said whether they landed.
	describeUnconfirmed
)

// describeClient is hqc as a describe calls HQ: each call sent once — no 503
// wait of the client's own stacks beneath describeRetry's tries — and
// bounded by deliveryBounds' connect and hqCallTimeout's answer.
func describeClient(hqc hq.Client) hq.Client {
	return hqc.Once().Bounded(deliveryBounds.connect, hqCallTimeout)
}

// putDescriptionRetried is putChangeDescription as describe-change calls it:
// tried as describeRetry paces it while HQ is away (describeTransient). Every
// try puts the same words, and a picture already attached is not attached
// again, so a try again changes nothing a landed one did. away says, when
// HQ was still away after the tries, whether any of them may have landed —
// HQ has no read of a change's words to ask.
func putDescriptionRetried(ctx context.Context, hqc hq.Client, stateDir string, m *workflow.ServiceMeta, number int, text, title string) (tries int, away describeAway, err error) {
	client := describeClient(hqc)
	unconfirmed := false
	tries = describeRetry.run(ctx, func() hqAnswer {
		err = putChangeDescription(ctx, client, stateDir, m, number, text, title)
		if !describeTransient(err) {
			return hqAnswered
		}
		unconfirmed = unconfirmed || !hqNotReached(err)
		return hqNotServing
	})
	switch {
	case !describeTransient(err):
		return tries, describeServed, err
	case unconfirmed:
		return tries, describeUnconfirmed, err
	}
	return tries, describeNotReached, err
}

// describeSelf is the Mate's own state as a describe reads it: once while
// the pair records its change, and with nothing recorded — the state is then
// the only word on which change is open — tried as describeRetry paces it
// while HQ is away.
func describeSelf(ctx context.Context, hqc hq.Client, m *workflow.ServiceMeta) (hq.MateState, error) {
	if m.HQ.Change != 0 {
		return hqc.Self(ctx)
	}
	var (
		state hq.MateState
		err   error
	)
	client := describeClient(hqc)
	describeRetry.run(ctx, func() hqAnswer {
		callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
		state, err = client.Self(callCtx)
		cancel()
		if describeTransient(err) {
			return hqNotServing
		}
		return hqAnswered
	})
	return state, err
}

// hqNotReached reports whether a try HQ did not serve surely never reached
// it: no connection made — refused, or none within the bound — or HQ's own
// 503, a standby that names why and serves nothing.
func hqNotReached(err error) bool {
	var (
		noAnswer    *hq.NoAnswerError
		unavailable *hq.UnavailableError
	)
	switch {
	case errors.As(err, &noAnswer):
		return !noAnswer.Connected
	case errors.Is(err, syscall.ECONNREFUSED):
		return true
	case errors.As(err, &unavailable):
		return unavailable.Err == nil && unavailable.Code != ""
	}
	return false
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
// every try: the words did not land and the change is still a draft — or,
// when a try may have reached HQ, that HQ never said whether they did — and
// they are kept for it.
func describeNotLanded(address string, number, tries int, away describeAway, err error) string {
	outcome := fmt.Sprintf("The description did not land: HQ at %s did not answer for about a minute (%d tries; the last: %s). "+
		"Change #%d is still a draft — the person is not asked to review it.", address, tries, hqNotServingWords(err), number)
	if away == describeUnconfirmed {
		outcome = fmt.Sprintf("The description may not have landed: HQ at %s did not answer for about a minute (%d tries; the last: %s), "+
			"and a try may have reached it without HQ saying so. Change #%d may still be a draft — the person is not asked to review it until it lands.",
			address, tries, hqNotServingWords(err), number)
	}
	return outcome + ` The words are kept for it: call describe-change again to put them on — the same words again change nothing — and if HQ still does not answer, tell the person HQ is not answering.`
}

// describeStateUnknown is describe-change's answer when no change is on
// record and the Mate's own state, the only word on which one is open,
// could not be read: nothing was written, and nothing is kept.
func describeStateUnknown(address, hostname string, err error) string {
	return fmt.Sprintf("Nothing was written: whether a change is open from %s is unknown — HQ at %s did not tell (the last answer: %s). "+
		`Call describe-change again once HQ answers; if it still does not, tell the person HQ is not answering.`,
		hostname, address, hqNotServingWords(err))
}

// holdPairToDescribe holds the pair's checkout for a describe as a delivery
// holds it (holdPairCheckout), so a describe from one chat and a delivery
// from another never interleave: a delivery reads whether the change's title
// is still zcp's and then retitles it, and puts the words the pair keeps, and
// a describe landing in between would be written over or lost. It answers the
// pair's record as the holder left it, or — the pair held past the wait —
// the refusal that says nothing was written.
func holdPairToDescribe(ctx context.Context, stateDir string, meta *workflow.ServiceMeta) (*workflow.ServiceMeta, func(), *mcp.CallToolResult) {
	release, err := holdPairCheckout(ctx, stateDir, meta.Hostname)
	if err != nil {
		return nil, nil, convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("Nothing was written onto %s's change: %s.", meta.Hostname, pairHeldReason(meta.Hostname, err)),
			fmt.Sprintf(`Call zerops_workflow action="describe-change" service=%q again once that delivery is done.`, meta.Hostname),
		), WithRecoveryStatus())
	}
	if fresh, _ := workflow.FindServiceMeta(stateDir, meta.Hostname); hqPairWired(fresh) {
		meta = fresh
	}
	return meta, release, nil
}
