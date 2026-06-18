package sync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// These fetchJSON tests are not parallel because they mutate the package-level
// httpRetryBaseDelay via withFastRetry. Running them sequentially is cheap —
// each test finishes in single-digit milliseconds.
func TestFetchJSON_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	var out struct {
		OK bool `json:"ok"`
	}
	err := fetchJSON(context.Background(), func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.OK {
		t.Error("expected OK=true")
	}
}

// TestFetchJSON_RetriesTransport verifies the retry layer hides transient
// transport failures. First attempt closes the connection mid-response;
// second attempt succeeds.
func TestFetchJSON_RetriesTransport(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			// Hijack the connection and close it abruptly to simulate a
			// transport error (what an HTTP/2 stream reset looks like to
			// the client).
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "no hijack", http.StatusInternalServerError)
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	// Shorten the retry delay for the test.
	restore := withFastRetry()
	t.Cleanup(restore)

	var out struct {
		OK bool `json:"ok"`
	}
	err := fetchJSON(context.Background(), func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	}, &out)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
	if !out.OK {
		t.Error("expected OK=true")
	}
}

// TestFetchJSON_Retries5xx verifies 5xx responses trigger retries and a
// subsequent 2xx succeeds.
func TestFetchJSON_Retries5xx(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	restore := withFastRetry()
	t.Cleanup(restore)

	var out struct {
		OK bool `json:"ok"`
	}
	err := fetchJSON(context.Background(), func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	}, &out)
	if err != nil {
		t.Fatalf("expected success after 5xx retries, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

// TestFetchJSON_DoesNotRetry4xx verifies client errors fail fast.
func TestFetchJSON_DoesNotRetry4xx(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	restore := withFastRetry()
	t.Cleanup(restore)

	var out any
	err := fetchJSON(context.Background(), func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	}, &out)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected *httpStatusError, got %T: %v", err, err)
	}
	if statusErr.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", statusErr.StatusCode)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("expected 1 attempt (no retry for 4xx), got %d", got)
	}
}

// TestFetchJSON_ExhaustsAttempts verifies the helper gives up after the
// configured attempt count and returns the last error.
func TestFetchJSON_ExhaustsAttempts(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	restore := withFastRetry()
	t.Cleanup(restore)

	var out any
	err := fetchJSON(context.Background(), func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	}, &out)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := atomic.LoadInt32(&attempts); int(got) != httpRetryAttempts {
		t.Errorf("expected %d attempts, got %d", httpRetryAttempts, got)
	}
}

// TestIsRetryable covers the classification logic, including the ctx-aware
// distinction between a per-request client timeout (retry) and caller
// cancellation (give up) — the release-flake fix.
func TestIsRetryable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ctxDone bool // parent ctx already cancelled/expired (caller gave up)
		err     error
		want    bool
	}{
		{"nil", false, nil, false},
		// Per-request http.Client.Timeout: parent ctx still live → retry.
		{"client_timeout_live_ctx", false, context.DeadlineExceeded, true},
		// Caller gave up (parent ctx done) → do not retry.
		{"caller_deadline_done_ctx", true, context.DeadlineExceeded, false},
		{"caller_canceled_done_ctx", true, context.Canceled, false},
		{"status_500", false, &httpStatusError{StatusCode: 500}, true},
		{"status_502", false, &httpStatusError{StatusCode: 502}, true},
		{"status_404", false, &httpStatusError{StatusCode: 404}, false},
		{"status_400", false, &httpStatusError{StatusCode: 400}, false},
		{"generic_transport_error", false, errors.New("connection reset"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if tt.ctxDone {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if got := isRetryable(ctx, tt.err); got != tt.want {
				t.Errorf("isRetryable(ctx, %v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// withFastRetry shortens the retry backoff so tests don't sit in time.After
// for 1.5 seconds each. Returns a restore func for cleanup.
func withFastRetry() func() {
	prev := httpRetryBaseDelay
	httpRetryBaseDelay = time.Millisecond
	return func() { httpRetryBaseDelay = prev }
}
