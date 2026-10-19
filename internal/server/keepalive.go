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

// keepAliveGap is the least time between a call's last progress notification
// and its response — the same race pollProcess documents: a progress
// notification and its response in one stdin chunk tear down Claude Code's MCP
// transport.
const keepAliveGap = time.Second

// keepAlive returns the middleware pair that owns a tools/call's progress
// stream when the client sent a progress token. receive sends a notification
// every interval while the call runs and holds the response until gap has
// passed since the stream's last notification. send numbers every progress
// notification on the call's token — the keep-alive's, a build poll's
// percentage, a tool's own — 1, 2, 3, so the values rise as MCP requires, and
// records when the last one went out.
func keepAlive(interval, gap time.Duration) (receive, send mcp.Middleware) {
	receive = func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			call, ok := req.(*mcp.CallToolRequest)
			if method != methodCallTool || !ok || call.Session == nil || call.Params == nil {
				return next(ctx, method, req)
			}
			token := call.Params.GetProgressToken()
			if token == nil {
				return next(ctx, method, req)
			}
			stream := &progressStream{token: token}
			ctx = context.WithValue(ctx, progressStreamKey{}, stream)
			beat := startHeartbeat(ctx, call.Session, token, interval)
			result, err := next(ctx, method, req)
			beat.stop()
			if last := stream.lastSent(); !last.IsZero() {
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
	send = func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			stream, _ := ctx.Value(progressStreamKey{}).(*progressStream)
			params, ok := req.GetParams().(*mcp.ProgressNotificationParams)
			session, isServer := req.GetSession().(*mcp.ServerSession)
			if method != methodProgress || stream == nil || !ok || params == nil || !isServer || params.ProgressToken != stream.token {
				return next(ctx, method, req)
			}
			// Held across the send, so the wire order is the numbering order.
			stream.mu.Lock()
			defer stream.mu.Unlock()
			stream.n++
			numbered := *params
			numbered.Progress, numbered.Total = stream.n, 0
			result, err := next(ctx, method, &mcp.ServerRequest[*mcp.ProgressNotificationParams]{Session: session, Params: &numbered})
			stream.last = time.Now()
			return result, err
		}
	}
	return receive, send
}

// methodProgress is the progress notification's method.
const methodProgress = "notifications/progress"

// progressStream is one tools/call's progress notifications: how many went
// out and when the last one did.
type progressStream struct {
	token any
	mu    sync.Mutex
	n     float64
	last  time.Time
}

type progressStreamKey struct{}

// lastSent is when the stream's last notification went out, zero when none
// did.
func (s *progressStream) lastSent() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

type heartbeat struct {
	done    chan struct{}
	stopped sync.WaitGroup
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
				_ = session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
					ProgressToken: token,
					Message:       fmt.Sprintf("still working (%s)", time.Since(start).Round(time.Second)),
				})
			}
		}
	})
	return h
}

// stop ends the heartbeat; no keep-alive follows its return.
func (h *heartbeat) stop() {
	close(h.done)
	h.stopped.Wait()
}
