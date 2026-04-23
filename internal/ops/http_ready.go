package ops

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type httpReadyConfig struct {
	interval       time.Duration
	timeout        time.Duration
	requestTimeout time.Duration
	successBelow   int
}

var defaultHTTPReadyConfig = httpReadyConfig{
	interval:       500 * time.Millisecond,
	timeout:        10 * time.Second,
	requestTimeout: 5 * time.Second,
	successBelow:   500,
}

// WaitHTTPReady probes url with GET until a response with status < 500
// arrives, or the timeout elapses. Matches checkHTTPRoot's "<500 = ready"
// contract so callers can compose both helpers.
//
// On timeout, returns an error wrapping the last-seen cause (5xx status
// or transport error). This is strictly more informative than
// WaitSSHReady's single-error return — no reason to lose the last
// observation when it's the whole diagnostic signal.
//
// Tuned for L7 propagation windows measured empirically at 440ms-1.3s
// (see plans/archive/subdomain-robustness.md §1.3). The 10-second
// default covers the 99th percentile with safety margin; callers
// needing longer waits should call waitHTTPReady (unexported) with an
// explicit config.
func WaitHTTPReady(ctx context.Context, httpClient HTTPDoer, url string) error {
	return waitHTTPReady(ctx, httpClient, url, defaultHTTPReadyConfig)
}

func waitHTTPReady(ctx context.Context, httpClient HTTPDoer, url string, cfg httpReadyConfig) error {
	if httpClient == nil {
		return fmt.Errorf("no HTTP client configured")
	}
	if url == "" {
		return fmt.Errorf("empty url")
	}

	deadline := time.After(cfg.timeout)
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			if lastErr != nil {
				return fmt.Errorf("http not ready on %s after %s: %w", url, cfg.timeout, lastErr)
			}
			return fmt.Errorf("http not ready on %s after %s", url, cfg.timeout)
		default:
		}

		reqCtx, cancel := context.WithTimeout(ctx, cfg.requestTimeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
		if err != nil {
			cancel()
			return err
		}
		resp, err := httpClient.Do(req)
		if err == nil {
			code := resp.StatusCode
			resp.Body.Close()
			if code < cfg.successBelow {
				cancel()
				return nil
			}
			lastErr = fmt.Errorf("HTTP %d", code)
		} else {
			lastErr = err
		}
		cancel()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("http not ready on %s after %s: %w", url, cfg.timeout, lastErr)
		case <-time.After(cfg.interval):
		}
	}
}

// OverrideHTTPReadyConfigForTest overrides the HTTP readiness polling config.
// Returns a restore function. Only for use in tests; enables ms-scale cadence.
func OverrideHTTPReadyConfigForTest(interval, timeout time.Duration) func() {
	old := defaultHTTPReadyConfig
	defaultHTTPReadyConfig = httpReadyConfig{
		interval:       interval,
		timeout:        timeout,
		requestTimeout: old.requestTimeout,
		successBelow:   old.successBelow,
	}
	return func() { defaultHTTPReadyConfig = old }
}
