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
	"github.com/zeropsio/zcp/internal/ops"
)

// A delivery fails fast (spec-mate §10.10; the owner, 2026-10-04: HQ should
// always answer, and when it does not the problem is bigger — waiting it out
// only masks it). Each step of a delivery HQ serves — taking `main` in,
// opening the change, taking its branch in, pushing it — is tried at most
// three times, a short wait apart, and only while HQ could not serve it: not
// reached at once — no connection within the bound, git's preflight among
// them — a transfer HQ stopped feeding, or a 5xx. A refusal fails at once,
// and so does a try HQ connected for and then left unanswered past its bound
// (deliveryBounds, ops.HQGitBound). Nothing tries again once the call has
// returned: the work stays committed in the pair's checkout, and the next
// delivery sends it.

// hqAnswer is what one try of a delivery step met.
type hqAnswer int

const (
	// hqAnswered: HQ served the try, or refused it — final either way.
	hqAnswered hqAnswer = iota
	// hqNotServing: HQ was not reached at once, or answered a 5xx; tried
	// again.
	hqNotServing
	// hqSilent: HQ left the try unanswered past its bound; final, since
	// another try would only spend the bound again.
	hqSilent
)

// hqRetry paces the tries of one delivery step against HQ: one try more than
// it has waits.
type hqRetry struct {
	waits []time.Duration
	pause func(context.Context, time.Duration) error
}

// deliveryRetry is how every delivery step is tried: three tries, 1 s and
// 3 s apart. A var so tests do not wait it out.
var deliveryRetry = hqRetry{waits: []time.Duration{time.Second, 3 * time.Second}, pause: waitFor}

// deliveryBounds bound each try of a delivery's API call to HQ: connected
// within connect, TLS included, and answered within answer
// (hq.Client.Bounded). A try of git against HQ is preceded by a preflight
// bounded by connect both ways (hq.Client.Serving): git has no connect bound
// of its own, and its transfer is bounded by its stall (ops.HQGitBound). A
// var so tests narrow it.
var deliveryBounds = struct{ connect, answer time.Duration }{5 * time.Second, 10 * time.Second}

// deliveryClient is hqc as a delivery calls HQ: each call sent once, so no
// 503 wait stacks beneath the tries, and bounded by deliveryBounds.
func deliveryClient(hqc hq.Client) hq.Client {
	return hqc.Once().Bounded(deliveryBounds.connect, deliveryBounds.answer)
}

// run calls try until HQ answers it — served or refused — or leaves it
// unanswered past its bound, or the tries are spent, or ctx ends during a
// wait. tries is how many ran.
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

// hqUnavailable reports whether an answer from HQ's API is HQ unable to serve
// the call — not reached, a standby's 503, or any other 5xx, the balancer's
// 502 for an HQ that is down among them — rather than HQ's own refusal.
func hqUnavailable(err error) bool {
	var refused *hq.RefusedError
	return hq.IsUnavailable(err) || errors.As(err, &refused) && refused.Status >= http.StatusInternalServerError
}

// hqCallAnswer is what a try of an API call to HQ met, err its outcome: no
// connection within the bound is HQ not reached, tried again; connected and
// no answer within it is HQ silent.
func hqCallAnswer(err error) hqAnswer {
	var noAnswer *hq.NoAnswerError
	switch {
	case errors.As(err, &noAnswer) && noAnswer.Connected:
		return hqSilent
	case hqUnavailable(err):
		return hqNotServing
	}
	return hqAnswered
}

// hqGitAnswer is what a try of a git command against HQ met, err and output
// its outcome: its preflight finding HQ not serving, a stall or a 5xx is HQ
// not serving, tried again; git's safety cap ending it is HQ silent.
func hqGitAnswer(err error, output []byte) hqAnswer {
	if err == nil {
		return hqAnswered
	}
	if _, silent := ops.HQGitNoAnswer(string(output)); silent {
		return hqSilent
	}
	if gitHQUnavailable(err, output) {
		return hqNotServing
	}
	return hqAnswered
}

// hqPreflightError is a try of a git command against HQ that never ran git:
// its preflight (hq.Client.Serving) found HQ not serving.
type hqPreflightError struct{ err error }

func (e *hqPreflightError) Error() string {
	return "HQ did not answer before git ran: " + e.err.Error()
}
func (e *hqPreflightError) Unwrap() error { return e.err }

// gitHQUnavailable reports whether a try of a git command against HQ met HQ
// unable to serve it: its preflight, or git's own output, says so.
func gitHQUnavailable(err error, output []byte) bool {
	var preflight *hqPreflightError
	return errors.As(err, &preflight) || ops.GitRemoteUnavailable(string(output))
}

// hqNotAnsweringLine says which step HQ at address could not serve, after how
// many tries, and the last answer (hqNotServingWords, gitNotServingWords).
func hqNotAnsweringLine(address, step string, tries int, last string) string {
	if tries == 1 {
		return fmt.Sprintf("HQ at %s is not answering: %s failed after 1 try (the last: %s)", address, step, last)
	}
	return fmt.Sprintf("HQ at %s is not answering: %s failed after %d tries (the last: %s)", address, step, tries, last)
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

// gitStatus5xx is the 5xx git reports a remote answered.
var gitStatus5xx = regexp.MustCompile(`returned error: (5\d\d)`)

// gitNotServingWords is hqNotServingWords for a git command against HQ: the
// bound that ended it names itself, a 5xx git reports is HQ not serving, and
// anything else is git's own words.
func gitNotServingWords(err error, output []byte) string {
	var preflight *hqPreflightError
	if errors.As(err, &preflight) {
		return hqNotServingWords(preflight.err)
	}
	if words, silent := ops.HQGitNoAnswer(string(output)); silent {
		return words
	}
	if ops.HQGitStalled(string(output)) {
		return ops.HQGitStallWords()
	}
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
