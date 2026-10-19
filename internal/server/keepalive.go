package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// keepAliveInterval is the longest a tool call that asked for progress goes
// without a notification. OpenCode calls tools through the MCP TS SDK with its
// default 60 s request timeout and resetTimeoutOnProgress, so a call silent
// for a minute fails on the client as "MCP error -32001: Request timed out"
// while zcp works on (a deploy's zcli push and HQ delivery run outside the
// build poll's own progress).
const keepAliveInterval = 15 * time.Second

// keepAliveGap is the least time between the last keep-alive and the call's
// response — the same race pollProcess documents: a progress notification and
// its response in one stdin chunk tear down Claude Code's MCP transport.
const keepAliveGap = time.Second

// keepAlive returns middleware that sends a progress notification every
// interval while a tools/call runs, when the client sent a progress token.
// The response waits until gap has passed since the last one.
func keepAlive(interval, gap time.Duration) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			call, ok := req.(*mcp.CallToolRequest)
			if method != methodCallTool || !ok || call.Session == nil || call.Params == nil {
				return next(ctx, method, req)
			}
			token := call.Params.GetProgressToken()
			if token == nil {
				return next(ctx, method, req)
			}
			beat := startHeartbeat(ctx, call.Session, token, interval)
			result, err := next(ctx, method, req)
			if last := beat.stop(); !last.IsZero() {
				if wait := gap - time.Since(last); wait > 0 {
					select {
					case <-time.After(wait):
					case <-ctx.Done():
					}
				}
			}
			return result, err
		}
	}
}

type heartbeat struct {
	done    chan struct{}
	stopped sync.WaitGroup
	mu      sync.Mutex
	last    time.Time
}

func startHeartbeat(ctx context.Context, session *mcp.ServerSession, token any, interval time.Duration) *heartbeat {
	h := &heartbeat{done: make(chan struct{})}
	start := time.Now()
	h.stopped.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-h.done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				elapsed := time.Since(start).Round(time.Second)
				_ = session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
					ProgressToken: token,
					Message:       fmt.Sprintf("still working (%s)", elapsed),
					Progress:      time.Since(start).Seconds(),
				})
				h.mu.Lock()
				h.last = time.Now()
				h.mu.Unlock()
			}
		}
	})
	return h
}

// stop ends the heartbeat and returns when its last notification went out
// (zero when none did). No notification follows a return.
func (h *heartbeat) stop() time.Time {
	close(h.done)
	h.stopped.Wait()
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last
}
