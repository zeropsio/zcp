package update

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// IdleWaiter tracks active MCP requests via middleware.
// WaitForIdle blocks until no requests are in-flight.
type IdleWaiter struct {
	active atomic.Int32
	mu     sync.Mutex
	ch     chan struct{} // signaled when active drops to 0
}

// NewIdleWaiter creates an IdleWaiter ready for use.
func NewIdleWaiter() *IdleWaiter {
	return &IdleWaiter{}
}

// Middleware returns MCP middleware that tracks request lifecycle.
func (w *IdleWaiter) Middleware() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			w.active.Add(1)
			defer w.done()
			return next(ctx, method, req)
		}
	}
}

// done decrements the active counter and signals waiters when it reaches 0.
func (w *IdleWaiter) done() {
	if w.active.Add(-1) == 0 {
		w.mu.Lock()
		ch := w.ch
		w.mu.Unlock()
		if ch != nil {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
}

// WaitForIdle blocks until the active request count reaches 0.
// Returns immediately if already idle. Respects ctx cancellation.
func (w *IdleWaiter) WaitForIdle(ctx context.Context) error {
	if w.active.Load() == 0 {
		return nil
	}

	w.mu.Lock()
	if w.ch == nil {
		w.ch = make(chan struct{}, 1)
	}
	ch := w.ch
	w.mu.Unlock()

	// Re-check after setting up channel to avoid race.
	if w.active.Load() == 0 {
		return nil
	}

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
