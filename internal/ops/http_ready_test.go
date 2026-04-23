package ops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// sequencingHTTP returns responses from a pre-loaded queue. Statuses < 0
// represent a transport error; otherwise statuses are returned as HTTP
// responses with empty bodies. When the queue is exhausted, the last entry
// is returned repeatedly.
type sequencingHTTP struct {
	mu        sync.Mutex
	responses []int
	calls     atomic.Int32
}

func (s *sequencingHTTP) Do(*http.Request) (*http.Response, error) {
	s.calls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.responses) == 0 {
		return nil, errors.New("sequencingHTTP: no responses configured")
	}
	next := s.responses[0]
	if len(s.responses) > 1 {
		s.responses = s.responses[1:]
	}
	if next < 0 {
		return nil, fmt.Errorf("transport error")
	}
	return &http.Response{
		StatusCode: next,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

func TestWaitHTTPReady(t *testing.T) {
	// t.Parallel omitted — every subtest below mutates the package-level
	// defaultHTTPReadyConfig via OverrideHTTPReadyConfigForTest. The mutex
	// keeps the race detector green, but parallel subtests would still
	// clobber each other's interval/timeout values (e.g. a 20ms-timeout
	// subtest interleaving a 1s-timeout subtest would yield spurious
	// failures). Subtests therefore run sequentially.

	t.Run("immediate 200 returns nil on first attempt", func(t *testing.T) {
		restore := OverrideHTTPReadyConfigForTest(1*time.Millisecond, 1*time.Second)
		defer restore()

		doer := &sequencingHTTP{responses: []int{200}}
		err := WaitHTTPReady(context.Background(), doer, "http://test/")
		if err != nil {
			t.Fatalf("want nil, got %v", err)
		}
		if got := doer.calls.Load(); got != 1 {
			t.Errorf("want 1 HTTP call, got %d", got)
		}
	})

	t.Run("404 counts as ready (< 500 contract)", func(t *testing.T) {
		restore := OverrideHTTPReadyConfigForTest(1*time.Millisecond, 1*time.Second)
		defer restore()

		doer := &sequencingHTTP{responses: []int{404}}
		if err := WaitHTTPReady(context.Background(), doer, "http://test/"); err != nil {
			t.Fatalf("404 must count as ready: got %v", err)
		}
	})

	t.Run("5xx retried until 200", func(t *testing.T) {
		restore := OverrideHTTPReadyConfigForTest(1*time.Millisecond, 1*time.Second)
		defer restore()

		doer := &sequencingHTTP{responses: []int{502, 502, 200}}
		if err := WaitHTTPReady(context.Background(), doer, "http://test/"); err != nil {
			t.Fatalf("want nil after 3 attempts, got %v", err)
		}
		if got := doer.calls.Load(); got < 3 {
			t.Errorf("want ≥3 HTTP calls, got %d", got)
		}
	})

	t.Run("transport error retried until success", func(t *testing.T) {
		restore := OverrideHTTPReadyConfigForTest(1*time.Millisecond, 1*time.Second)
		defer restore()

		doer := &sequencingHTTP{responses: []int{-1, -1, 200}}
		if err := WaitHTTPReady(context.Background(), doer, "http://test/"); err != nil {
			t.Fatalf("want nil after transport retries, got %v", err)
		}
	})

	t.Run("timeout on 5xx loop wraps last status", func(t *testing.T) {
		restore := OverrideHTTPReadyConfigForTest(1*time.Millisecond, 20*time.Millisecond)
		defer restore()

		doer := &sequencingHTTP{responses: []int{503}}
		err := WaitHTTPReady(context.Background(), doer, "http://test/")
		if err == nil {
			t.Fatal("want timeout error, got nil")
		}
		msg := err.Error()
		if !strings.Contains(msg, "http not ready") {
			t.Errorf("want 'http not ready' in error, got: %s", msg)
		}
		if !strings.Contains(msg, "HTTP 503") {
			t.Errorf("want last status wrapped, got: %s", msg)
		}
	})

	t.Run("timeout on transport error wraps last cause", func(t *testing.T) {
		restore := OverrideHTTPReadyConfigForTest(1*time.Millisecond, 20*time.Millisecond)
		defer restore()

		doer := &sequencingHTTP{responses: []int{-1}}
		err := WaitHTTPReady(context.Background(), doer, "http://test/")
		if err == nil {
			t.Fatal("want timeout error, got nil")
		}
		if !strings.Contains(err.Error(), "transport error") {
			t.Errorf("want 'transport error' wrapped, got: %v", err)
		}
	})

	t.Run("pre-cancelled context returns ctx error", func(t *testing.T) {
		restore := OverrideHTTPReadyConfigForTest(1*time.Millisecond, 1*time.Second)
		defer restore()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		doer := &sequencingHTTP{responses: []int{200}}
		err := WaitHTTPReady(ctx, doer, "http://test/")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("want context.Canceled, got %v", err)
		}
	})

	t.Run("context cancelled mid-flight returns ctx error", func(t *testing.T) {
		restore := OverrideHTTPReadyConfigForTest(10*time.Millisecond, 1*time.Second)
		defer restore()

		ctx, cancel := context.WithCancel(context.Background())
		// Cancel after first response arrives.
		doer := &sequencingHTTP{responses: []int{502, 502, 502}}
		go func() {
			time.Sleep(5 * time.Millisecond)
			cancel()
		}()

		err := WaitHTTPReady(ctx, doer, "http://test/")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("want context.Canceled, got %v", err)
		}
	})

	t.Run("nil client returns error, zero HTTP calls", func(t *testing.T) {
		err := WaitHTTPReady(context.Background(), nil, "http://test/")
		if err == nil || !strings.Contains(err.Error(), "no HTTP client configured") {
			t.Errorf("want 'no HTTP client configured' error, got %v", err)
		}
	})

	t.Run("empty url returns error, zero HTTP calls", func(t *testing.T) {
		doer := &sequencingHTTP{responses: []int{200}}
		err := WaitHTTPReady(context.Background(), doer, "")
		if err == nil || !strings.Contains(err.Error(), "empty url") {
			t.Errorf("want 'empty url' error, got %v", err)
		}
		if got := doer.calls.Load(); got != 0 {
			t.Errorf("must not call Do on empty url, called %d times", got)
		}
	})
}
