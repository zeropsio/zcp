package hq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"time"
)

// RefusedError is HQ's own no: its HTTP status, the code it answered, and
// the reason beside it where HQ gives one (a permission's, or a change's own:
// change_not_open, repo_not_found, ...).
type RefusedError struct {
	Status int
	Code   string
	Reason string
}

func (r *RefusedError) Error() string {
	if r.Reason != "" && r.Reason != r.Code {
		return fmt.Sprintf("hq refused: %d %s (%s)", r.Status, r.Code, r.Reason)
	}
	return fmt.Sprintf("hq refused: %d %s", r.Status, r.Code)
}

// UnavailableError is HQ not serving now — never a refusal, always "try
// again": an HQ that does not answer, or one that answers 503 (a standby
// while a deploy runs two side by side, not_active; Zerops not answering it,
// zerops_unavailable) past the wait the client gives it. Code is HQ's, ""
// when it did not answer.
type UnavailableError struct {
	Code string
	Err  error
}

func (u *UnavailableError) Error() string {
	if u.Err != nil {
		return "hq unavailable: " + u.Err.Error()
	}
	return "hq unavailable: " + u.Code
}

func (u *UnavailableError) Unwrap() error { return u.Err }

// NoAnswerError is a call HQ did not answer within a bounded client's bounds
// (Client.Bounded): no connection within the connect bound — TLS included —
// or, connected, no answer within the answer bound. An UnavailableError
// carries it.
type NoAnswerError struct {
	Path      string
	Connected bool
	Within    time.Duration
}

func (e *NoAnswerError) Error() string { return "hq " + e.Path + ": " + e.Words() }

// Words is what the call met, without the path: "no connection within 5s".
func (e *NoAnswerError) Words() string {
	if !e.Connected {
		return fmt.Sprintf("no connection within %s", e.Within)
	}
	return fmt.Sprintf("no answer within %s", e.Within)
}

// IsUnavailable reports whether err is HQ not serving now: a delivery it
// stopped waits for the next pass rather than failing.
func IsUnavailable(err error) bool {
	var unavailable *UnavailableError
	return errors.As(err, &unavailable)
}

// refusedAs reports whether err is HQ's refusal with code.
func refusedAs(err error, code string) bool {
	var refused *RefusedError
	return errors.As(err, &refused) && refused.Code == code
}

// hqClient speaks HQ's Mate API at one address.
type hqClient struct {
	http    Doer
	address string
	// pause waits out a 503's Retry-After; nil waits on a timer.
	pause func(ctx context.Context, d time.Duration) error
	// once: a 503 is answered at once, never waited out (Client.Once).
	once bool
	// connect and answer bound each try (Client.Bounded); 0 leaves it to
	// the caller's context and the Doer.
	connect, answer time.Duration
}

// A 503 is waited out for at most unavailableBudget in all, each wait its
// Retry-After (unavailableWait when it names none) cut to unavailableWaitMax:
// a deploy overlaps two HQs for about 20 s, and HQ asks for 5 s.
const (
	unavailableBudget  = 20 * time.Second
	unavailableWait    = 5 * time.Second
	unavailableWaitMax = 10 * time.Second
)

func (c hqClient) challenge(ctx context.Context, projectID string) (string, error) {
	var out struct {
		Nonce string `json:"nonce"`
	}
	err := c.json(ctx, http.MethodPost, "/api/mate/challenge", "", map[string]string{"projectId": projectID}, &out)
	return out.Nonce, err
}

// credential presents the nonce, naming the id of the container's key and
// its own service where it has them; an HQ that does not know a field
// ignores it.
func (c hqClient) credential(ctx context.Context, projectID, nonce, keyTokenID, serviceID string) (string, error) {
	var out struct {
		Credential string `json:"credential"`
	}
	in := map[string]string{"projectId": projectID, "nonce": nonce}
	if keyTokenID != "" {
		in["keyTokenId"] = keyTokenID
	}
	if serviceID != "" {
		in["serviceId"] = serviceID
	}
	err := c.json(ctx, http.MethodPost, "/api/mate/credential", "", in, &out)
	return out.Credential, err
}

func (c hqClient) whoami(ctx context.Context, credential string) (string, error) {
	var out struct {
		ProjectID string `json:"projectId"`
	}
	err := c.json(ctx, http.MethodGet, "/api/mate/whoami", "Mate "+credential, nil, &out)
	return out.ProjectID, err
}

// keepKey names the id of the container's key under credential.
func (c hqClient) keepKey(ctx context.Context, credential, keyTokenID string) error {
	return c.json(ctx, http.MethodPut, "/api/mate/key", "Mate "+credential,
		map[string]string{"keyTokenId": keyTokenID}, nil)
}

func (c hqClient) json(ctx context.Context, method, path, authorization string, in, out any) error {
	var body []byte
	contentType := ""
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("hq %s: %w", path, err)
		}
		body, contentType = raw, "application/json"
	}
	return c.send(ctx, method, path, authorization, contentType, body, out)
}

// answerLimit bounds what zcp reads of an answer: a change's description is
// up to 20000 characters, each up to four bytes of JSON-escaped UTF-8.
const answerLimit = 1 << 20

// send is one request with body of contentType ("" for none), its answer
// decoded into out — sent again after each 503's Retry-After until
// unavailableBudget is spent, unless the client sends each call once.
func (c hqClient) send(ctx context.Context, method, path, authorization, contentType string, body []byte, out any) error {
	waited := time.Duration(0)
	for {
		wait, err := c.sendOnce(ctx, method, path, authorization, contentType, body, out)
		if wait == 0 || c.once || waited+wait > unavailableBudget {
			return err
		}
		waited += wait
		if err := c.wait(ctx, wait); err != nil {
			return &UnavailableError{Err: err}
		}
	}
}

// errNotConnected ends a try that has no connection within its bound.
var errNotConnected = errors.New("not connected")

// bounded is ctx bounded for one try as the client is (Client.Bounded): the
// connect bound runs until net/http reports the connection made, TLS
// included; the answer bound covers the whole try.
func (c hqClient) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	cancelAnswer := context.CancelFunc(func() {})
	if c.answer > 0 {
		ctx, cancelAnswer = context.WithTimeout(ctx, c.answer)
	}
	if c.connect == 0 {
		return ctx, cancelAnswer
	}
	ctx, cancel := context.WithCancelCause(ctx)
	timer := time.AfterFunc(c.connect, func() { cancel(errNotConnected) })
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { timer.Stop() }})
	return ctx, func() {
		timer.Stop()
		cancel(nil)
		cancelAnswer()
	}
}

// unanswered is what ended a try whose call failed with err: a bound of the
// client's (NoAnswerError), or err when the caller's own ctx ended it or
// nothing did.
func (c hqClient) unanswered(caller, try context.Context, path string, err error) error {
	switch {
	case caller.Err() != nil:
		return err
	case errors.Is(context.Cause(try), errNotConnected):
		return &NoAnswerError{Path: path, Within: c.connect}
	case errors.Is(try.Err(), context.DeadlineExceeded):
		return &NoAnswerError{Path: path, Connected: true, Within: c.answer}
	}
	return err
}

// sendOnce is one try; wait is how long to wait before the next, 0 when the
// answer is final.
func (c hqClient) sendOnce(caller context.Context, method, path, authorization, contentType string, body []byte, out any) (time.Duration, error) {
	ctx, release := c.bounded(caller)
	defer release()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.address, "/")+path, reader)
	if err != nil {
		return 0, fmt.Errorf("hq %s: %w", path, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, &UnavailableError{Err: c.unanswered(caller, ctx, path, fmt.Errorf("hq %s: %w", path, err))}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, answerLimit))
	if err != nil {
		return 0, &UnavailableError{Err: c.unanswered(caller, ctx, path, fmt.Errorf("hq %s: %w", path, err))}
	}
	// A 204 answers with nothing to decode.
	if resp.StatusCode == http.StatusNoContent {
		return 0, nil
	}
	if resp.StatusCode != http.StatusOK {
		var refusal struct {
			Code   string `json:"code"`
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(raw, &refusal)
		if resp.StatusCode == http.StatusServiceUnavailable {
			return retryAfter(resp.Header.Get("Retry-After")), &UnavailableError{Code: refusal.Code}
		}
		return 0, &RefusedError{Status: resp.StatusCode, Code: refusal.Code, Reason: refusal.Reason}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return 0, fmt.Errorf("hq %s: malformed answer: %w", path, err)
	}
	return 0, nil
}

// retryAfter is how long a 503 asks to be left alone, in whole seconds,
// within [1 s, unavailableWaitMax]; unavailableWait when it says nothing
// readable.
func retryAfter(header string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(header))
	if err != nil || seconds <= 0 {
		return unavailableWait
	}
	return min(time.Duration(seconds)*time.Second, unavailableWaitMax)
}

func (c hqClient) wait(ctx context.Context, d time.Duration) error {
	if c.pause != nil {
		return c.pause(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
