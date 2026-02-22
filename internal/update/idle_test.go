// Tests for: internal/update/idle.go — IdleWaiter MCP middleware for tracking in-flight requests.

package update

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestIdleWaiter_WaitForIdle_AlreadyIdle(t *testing.T) {
	t.Parallel()

	w := NewIdleWaiter()
	ctx := context.Background()

	// Should return immediately when no requests are in-flight.
	done := make(chan error, 1)
	go func() { done <- w.WaitForIdle(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WaitForIdle returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForIdle should return immediately when idle")
	}
}

func TestIdleWaiter_WaitForIdle_BlocksUntilDone(t *testing.T) {
	t.Parallel()

	w := NewIdleWaiter()
	ctx := context.Background()

	// Simulate an in-flight request.
	w.active.Add(1)

	waited := make(chan error, 1)
	go func() { waited <- w.WaitForIdle(ctx) }()

	// Should not return yet.
	select {
	case <-waited:
		t.Fatal("WaitForIdle should block while requests are in-flight")
	case <-time.After(50 * time.Millisecond):
		// Expected — still blocked.
	}

	// Finish the request.
	w.done()

	select {
	case err := <-waited:
		if err != nil {
			t.Fatalf("WaitForIdle returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForIdle should return after request completes")
	}
}

func TestIdleWaiter_WaitForIdle_ContextCancelled(t *testing.T) {
	t.Parallel()

	w := NewIdleWaiter()
	ctx, cancel := context.WithCancel(context.Background())

	// Simulate an in-flight request that never completes.
	w.active.Add(1)

	waited := make(chan error, 1)
	go func() { waited <- w.WaitForIdle(ctx) }()

	cancel()

	select {
	case err := <-waited:
		if err != context.Canceled {
			t.Fatalf("WaitForIdle error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForIdle should return on context cancellation")
	}
}

func TestIdleWaiter_Middleware_TracksRequests(t *testing.T) {
	t.Parallel()

	w := NewIdleWaiter()
	mw := w.Middleware()

	// Wrap a handler that blocks until we signal it.
	handlerDone := make(chan struct{})
	var zeroResult mcp.CallToolResult
	handler := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		<-handlerDone
		return &zeroResult, nil
	})

	// Start a request in a goroutine.
	go func() {
		_, _ = handler(context.Background(), "test/method", nil)
	}()

	// Give goroutine time to enter handler.
	time.Sleep(20 * time.Millisecond)

	if got := w.active.Load(); got != 1 {
		t.Fatalf("active = %d, want 1", got)
	}

	// WaitForIdle should block.
	ctx := context.Background()
	waited := make(chan error, 1)
	go func() { waited <- w.WaitForIdle(ctx) }()

	select {
	case <-waited:
		t.Fatal("WaitForIdle should block while handler is running")
	case <-time.After(50 * time.Millisecond):
	}

	// Complete the handler.
	close(handlerDone)

	select {
	case err := <-waited:
		if err != nil {
			t.Fatalf("WaitForIdle returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForIdle should return after handler completes")
	}

	if got := w.active.Load(); got != 0 {
		t.Fatalf("active = %d, want 0 after handler completes", got)
	}
}

func TestIdleWaiter_Middleware_ConcurrentRequests(t *testing.T) {
	t.Parallel()

	w := NewIdleWaiter()
	mw := w.Middleware()

	const n = 5
	barriers := make([]chan struct{}, n)
	for i := range barriers {
		barriers[i] = make(chan struct{})
	}

	var zeroResult mcp.CallToolResult
	handler := func(i int) mcp.MethodHandler {
		return mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
			<-barriers[i]
			return &zeroResult, nil
		})
	}

	// Start n concurrent requests.
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = handler(i)(context.Background(), "test", nil)
		}()
	}

	// Give goroutines time to enter handlers.
	time.Sleep(50 * time.Millisecond)
	if got := w.active.Load(); got != int32(n) {
		t.Fatalf("active = %d, want %d", got, n)
	}

	// Release all but one.
	for i := range n - 1 {
		close(barriers[i])
	}
	time.Sleep(20 * time.Millisecond)

	if got := w.active.Load(); got != 1 {
		t.Fatalf("active = %d, want 1 after releasing %d handlers", got, n-1)
	}

	// WaitForIdle should still block.
	ctx := context.Background()
	waited := make(chan error, 1)
	go func() { waited <- w.WaitForIdle(ctx) }()

	select {
	case <-waited:
		t.Fatal("WaitForIdle should block while last handler is running")
	case <-time.After(50 * time.Millisecond):
	}

	// Release the last one.
	close(barriers[n-1])

	select {
	case err := <-waited:
		if err != nil {
			t.Fatalf("WaitForIdle returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForIdle should return after all handlers complete")
	}

	wg.Wait()
}
