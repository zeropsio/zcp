package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Resilience settings for all sync HTTP calls against the Zerops API.
// Transient HTTP/2 stream resets AND slow recipe-list reads have been observed
// in CI (the recipe payload is large; a slow read tripped the old 30s per-request
// timeout and — because the resulting DeadlineExceeded was mis-classified as a
// caller cancellation — was NOT retried, sinking releases). Three attempts with
// exponential backoff + a generous per-request timeout cover both without masking
// real outages. These are vars (not consts) so tests can shorten the delays
// without waiting real seconds for retry backoff.
var (
	httpRetryAttempts  = 3
	httpRetryBaseDelay = 500 * time.Millisecond
	httpRequestTimeout = 90 * time.Second
)

// syncHTTPClient is a package-level client with a per-request timeout. Go's
// http.DefaultClient has no timeout — a stalled connection hangs indefinitely.
var syncHTTPClient = &http.Client{Timeout: httpRequestTimeout}

// httpStatusError carries a non-2xx status so the retry layer can decide
// whether to retry (5xx yes, 4xx no) and callers can inspect the body.
type httpStatusError struct {
	StatusCode int
	Body       []byte
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, string(e.Body))
}

// RequestFactory builds a fresh *http.Request bound to ctx. Each retry calls
// it again because net/http consumes the request body on the first attempt.
// Implementations should use http.NewRequestWithContext(ctx, ...) directly.
type RequestFactory func(ctx context.Context) (*http.Request, error)

// fetchJSON issues an HTTP request with retry + exponential backoff and
// decodes the response body into out.
//
// Retries on: transport errors, 5xx responses, per-request client timeouts.
// Does not retry: 4xx responses, caller (parent ctx) cancellation.
func fetchJSON(ctx context.Context, newReq RequestFactory, out any) error {
	body, err := doWithRetry(ctx, newReq)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	return nil
}

// doWithRetry runs newReq through the retry policy and returns the response
// body on success. Callers that need the raw body (not JSON) use this directly.
func doWithRetry(ctx context.Context, newReq RequestFactory) ([]byte, error) {
	var lastErr error
	delay := httpRetryBaseDelay

	for attempt := 1; attempt <= httpRetryAttempts; attempt++ {
		body, err := doOnce(ctx, newReq)
		if err == nil {
			return body, nil
		}
		lastErr = err

		if !isRetryable(ctx, err) || attempt == httpRetryAttempts {
			break
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
	}

	return nil, lastErr
}

// doOnce executes a single attempt. On 4xx/5xx it returns an *httpStatusError
// the retry layer can inspect. On transport errors it returns the raw error.
func doOnce(ctx context.Context, newReq RequestFactory) ([]byte, error) {
	req, err := newReq(ctx)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := syncHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, &httpStatusError{StatusCode: resp.StatusCode, Body: body}
	}
	return body, nil
}

// isRetryable decides whether the retry layer should try again. It needs the
// parent ctx to tell a CALLER cancellation (give up) apart from a per-request
// http.Client.Timeout (a transient slow read): both surface as
// context.DeadlineExceeded via errors.Is, but only the caller-cancellation case
// leaves the PARENT ctx done. The old code retried neither — so a single slow
// recipe read failed the whole sync (the CI release-flake root cause).
//   - Parent ctx done (caller cancelled / its deadline passed): no (caller gave up).
//   - Per-request client timeout (parent ctx still live): yes (transient slow read).
//   - 4xx: no (client error — retrying won't help).
//   - 5xx: yes (server-side transient).
//   - transport errors: yes (network instability is why this helper exists).
func isRetryable(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx.Err() != nil {
		return false // the caller's context is done — don't retry past a give-up
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// Parent ctx is still live (checked above), so this is the per-request
		// http.Client.Timeout firing on a slow read — retry it.
		return true
	}
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode >= 500
	}
	return true
}
