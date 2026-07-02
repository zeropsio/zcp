package ingest

import (
	"sync"
	"time"
)

// Limit tunables (spec §6 item 4).
const (
	ipRatePerMinute = 60.0
	ipBurst         = 600.0

	installDailyCap     = 10_000
	sessionToolCallCap  = 5_000
	newInstallHourlyCap = 1_000

	// staleAfter bounds how long an idle per-key entry is retained before
	// gc reclaims it — "in-memory with periodic GC" (spec §6 item 4).
	staleAfter = 2 * time.Hour
)

// ipBucket is a per-IP token bucket (60/min sustained, burst 600).
type ipBucket struct {
	tokens     float64
	lastRefill time.Time
	touchedAt  time.Time
}

// dailyCounter is a per-install rolling-UTC-day event counter.
type dailyCounter struct {
	day       string
	count     int
	touchedAt time.Time
}

// sessionCounter is a per-session tool_call counter (no reset — a session
// is process-lifetime scoped, spec §2).
type sessionCounter struct {
	count     int64
	touchedAt time.Time
}

// hourlyCardinality tracks the distinct install_ids one IP has introduced
// within the current UTC-hour bucket (spec §6 item 4 "new install-ids").
type hourlyCardinality struct {
	hour      string
	seen      map[string]struct{}
	touchedAt time.Time
}

// limiter is the ingest service's in-memory abuse/fairness guard (spec §6
// item 4): per-IP request rate, per-install daily volume, per-session
// tool_call volume, and per-IP new-install-id cardinality. Instance-owned,
// no package-level state — one per ingest process. All maps are
// periodically reclaimed via gc to bound memory under many distinct
// IPs/installs/sessions.
type limiter struct {
	mu sync.Mutex

	ipBuckets    map[string]*ipBucket
	installDaily map[string]*dailyCounter
	sessionCalls map[string]*sessionCounter
	ipNewInstall map[string]*hourlyCardinality
}

func newLimiter() *limiter {
	return &limiter{
		ipBuckets:    make(map[string]*ipBucket),
		installDaily: make(map[string]*dailyCounter),
		sessionCalls: make(map[string]*sessionCounter),
		ipNewInstall: make(map[string]*hourlyCardinality),
	}
}

// allowIP consumes one token from ip's bucket (60/min sustained, burst
// 600). When empty, returns false and how long until one token refills.
func (l *limiter) allowIP(ip string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.ipBuckets[ip]
	if !ok {
		b = &ipBucket{tokens: ipBurst, lastRefill: now}
		l.ipBuckets[ip] = b
	}
	if elapsed := now.Sub(b.lastRefill).Seconds(); elapsed > 0 {
		b.tokens = min(ipBurst, b.tokens+elapsed*(ipRatePerMinute/60.0))
		b.lastRefill = now
	}
	b.touchedAt = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	missing := 1 - b.tokens
	retryAfter := time.Duration(missing / (ipRatePerMinute / 60.0) * float64(time.Second))
	return false, retryAfter
}

// allowInstallDaily increments and checks install's rolling-UTC-day event
// counter (10k/day cap, spec §6 item 4).
func (l *limiter) allowInstallDaily(installID string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	day := now.UTC().Format("2006-01-02")
	c, ok := l.installDaily[installID]
	if !ok || c.day != day {
		c = &dailyCounter{day: day}
		l.installDaily[installID] = c
	}
	c.touchedAt = now
	if c.count >= installDailyCap {
		return false
	}
	c.count++
	return true
}

// allowSessionToolCall increments and checks session's tool_call counter
// (5k cap, spec §6 item 4 "then throttle-only" — every call past the cap
// stays rejected, it is not a one-shot cutoff).
func (l *limiter) allowSessionToolCall(sessionID string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	c, ok := l.sessionCalls[sessionID]
	if !ok {
		c = &sessionCounter{}
		l.sessionCalls[sessionID] = c
	}
	c.touchedAt = now
	if c.count >= sessionToolCallCap {
		return false
	}
	c.count++
	return true
}

// allowNewInstall tracks the distinct install_ids one IP has introduced
// within the current UTC-hour bucket. A repeated install_id from the same
// IP within the same hour is never double-counted. Exceeding
// newInstallHourlyCap distinct ids → false (ingest rejects the whole
// request with 429, spec §6 item 4).
func (l *limiter) allowNewInstall(ip, installID string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	hour := now.UTC().Format("2006-01-02T15")
	c, ok := l.ipNewInstall[ip]
	if !ok || c.hour != hour {
		c = &hourlyCardinality{hour: hour, seen: make(map[string]struct{})}
		l.ipNewInstall[ip] = c
	}
	c.touchedAt = now
	if _, already := c.seen[installID]; already {
		return true
	}
	if len(c.seen) >= newInstallHourlyCap {
		return false
	}
	c.seen[installID] = struct{}{}
	return true
}

// gc purges entries idle since before now-staleAfter, bounding memory
// under a large number of distinct IPs/installs/sessions over the
// process's lifetime (spec §6 item 4 "in-memory with periodic GC").
func (l *limiter) gc(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-staleAfter)
	for k, v := range l.ipBuckets {
		if v.touchedAt.Before(cutoff) {
			delete(l.ipBuckets, k)
		}
	}
	for k, v := range l.installDaily {
		if v.touchedAt.Before(cutoff) {
			delete(l.installDaily, k)
		}
	}
	for k, v := range l.sessionCalls {
		if v.touchedAt.Before(cutoff) {
			delete(l.sessionCalls, k)
		}
	}
	for k, v := range l.ipNewInstall {
		if v.touchedAt.Before(cutoff) {
			delete(l.ipNewInstall, k)
		}
	}
}
