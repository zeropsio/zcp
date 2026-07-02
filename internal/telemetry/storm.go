package telemetry

import (
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

// Storm guard tunables (spec §5.2): 2 events/s sustained, burst 200, at most
// one client_throttle synthesis per minute.
const (
	stormRatePerSecond  = 2.0
	stormBurst          = 200.0
	stormThrottleWindow = time.Minute
)

// isStormGoverned reports whether eventType is subject to the storm guard.
// session_start/session_end/client_throttle are exempt (spec §5.2) — callers
// never route those through allow/recordDrop.
func isStormGoverned(eventType string) bool {
	return eventType == wire.EventToolCall || eventType == wire.EventCLICommand
}

// stormGuard is a per-process token bucket applied to tool_call/cli_command
// events (spec §5.2). Instance-owned state — one per *Client, never a
// package-level var — so no global mutable state is introduced.
type stormGuard struct {
	mu sync.Mutex

	tokens     float64
	lastRefill time.Time

	droppedSinceThrottle int64
	lastThrottleAt       time.Time

	totalDroppedCount int64 // cumulative, feeds Client.DroppedCount()
}

func newStormGuard(now time.Time) *stormGuard {
	return &stormGuard{tokens: stormBurst, lastRefill: now}
}

// allow refills the bucket for elapsed wall-clock time, then consumes one
// token if available. Returns false when the caller must drop the event.
func (g *stormGuard) allow(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if elapsed := now.Sub(g.lastRefill).Seconds(); elapsed > 0 {
		g.tokens = min(stormBurst, g.tokens+elapsed*stormRatePerSecond)
		g.lastRefill = now
	}
	if g.tokens >= 1 {
		g.tokens--
		return true
	}
	return false
}

// recordDrop counts a storm-guard drop and reports whether a client_throttle
// event is due (spec §5.2: at most one per minute, carrying the dropped
// delta since the last throttle emission). The first drop after a quiet
// period fires immediately; subsequent drops accumulate silently until the
// 1-minute window elapses.
func (g *stormGuard) recordDrop(now time.Time) (delta int64, due bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.droppedSinceThrottle++
	g.totalDroppedCount++
	if g.lastThrottleAt.IsZero() || now.Sub(g.lastThrottleAt) >= stormThrottleWindow {
		delta = g.droppedSinceThrottle
		g.droppedSinceThrottle = 0
		g.lastThrottleAt = now
		return delta, true
	}
	return 0, false
}

// totalDropped returns the cumulative storm-guard drop count for the
// process so far.
func (g *stormGuard) totalDropped() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.totalDroppedCount
}
