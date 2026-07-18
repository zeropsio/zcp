package ingest

import (
	"testing"
	"time"
)

func TestLimiter_AllowIP_BurstThenSustained(t *testing.T) {
	t.Parallel()

	l := newLimiter()
	now := time.Now()

	// Burst 600 must all succeed immediately.
	for i := range 600 {
		if allowed, _ := l.allowIP("1.2.3.4", now); !allowed {
			t.Fatalf("request %d in burst unexpectedly rejected", i)
		}
	}
	// The 601st request in the same instant must be throttled.
	if allowed, retryAfter := l.allowIP("1.2.3.4", now); allowed {
		t.Error("request beyond burst unexpectedly allowed")
	} else if retryAfter <= 0 {
		t.Errorf("retryAfter = %v, want > 0", retryAfter)
	}
}

func TestLimiter_AllowIP_RefillsOverTime(t *testing.T) {
	t.Parallel()

	l := newLimiter()
	now := time.Now()
	for range 600 {
		l.allowIP("1.2.3.4", now)
	}
	// 60/min = 1/s: after 1s exactly one more token is available.
	later := now.Add(time.Second)
	if allowed, _ := l.allowIP("1.2.3.4", later); !allowed {
		t.Error("expected one refilled token after 1s")
	}
	if allowed, _ := l.allowIP("1.2.3.4", later); allowed {
		t.Error("expected only one refilled token after 1s")
	}
}

func TestLimiter_AllowIP_IndependentPerIP(t *testing.T) {
	t.Parallel()

	l := newLimiter()
	now := time.Now()
	for range 600 {
		l.allowIP("1.2.3.4", now)
	}
	if allowed, _ := l.allowIP("5.6.7.8", now); !allowed {
		t.Error("a different IP must have its own independent bucket")
	}
}

func TestLimiter_AllowInstallDaily_CapsAt10k(t *testing.T) {
	t.Parallel()

	l := newLimiter()
	now := time.Now()
	for i := range 10_000 {
		if !l.allowInstallDaily("install-a", now) {
			t.Fatalf("call %d unexpectedly rejected before reaching the daily cap", i)
		}
	}
	if l.allowInstallDaily("install-a", now) {
		t.Error("call beyond the 10k daily cap unexpectedly allowed")
	}
}

func TestLimiter_AllowInstallDaily_ResetsNextDay(t *testing.T) {
	t.Parallel()

	l := newLimiter()
	day1 := time.Date(2026, 7, 2, 23, 0, 0, 0, time.UTC)
	for range 10_000 {
		l.allowInstallDaily("install-a", day1)
	}
	if l.allowInstallDaily("install-a", day1) {
		t.Fatal("expected cap reached on day1")
	}
	day2 := day1.Add(2 * time.Hour) // crosses UTC midnight
	if !l.allowInstallDaily("install-a", day2) {
		t.Error("expected the per-day counter to reset on the next UTC day")
	}
}

func TestLimiter_AllowInstallDaily_IndependentPerInstall(t *testing.T) {
	t.Parallel()

	l := newLimiter()
	now := time.Now()
	for range 10_000 {
		l.allowInstallDaily("install-a", now)
	}
	if !l.allowInstallDaily("install-b", now) {
		t.Error("a different install_id must have its own independent daily counter")
	}
}

func TestLimiter_AllowSessionToolCall_CapsAt5k(t *testing.T) {
	t.Parallel()

	l := newLimiter()
	now := time.Now()
	for i := range 5000 {
		if !l.allowSessionToolCall("session-a", now) {
			t.Fatalf("call %d unexpectedly rejected before reaching the session cap", i)
		}
	}
	if l.allowSessionToolCall("session-a", now) {
		t.Error("tool_call beyond the 5k session cap unexpectedly allowed")
	}
	// A second call over the cap must also be rejected (throttle-only, not
	// a one-shot reject) — spec §6 "then throttle-only".
	if l.allowSessionToolCall("session-a", now) {
		t.Error("subsequent tool_call over the session cap must stay throttled")
	}
}

func TestLimiter_AllowNewInstall_CapsAt1kPerHourPerIP(t *testing.T) {
	t.Parallel()

	l := newLimiter()
	now := time.Now()
	for i := range 1000 {
		id := "install-" + time.Duration(i).String()
		if !l.allowNewInstall("9.9.9.9", id, now) {
			t.Fatalf("new install %d unexpectedly rejected before reaching the cardinality cap", i)
		}
	}
	if l.allowNewInstall("9.9.9.9", "install-overflow", now) {
		t.Error("the 1001st distinct new install_id from one IP in one hour must be rejected")
	}
}

func TestLimiter_AllowNewInstall_RepeatedIDNotDoubleCounted(t *testing.T) {
	t.Parallel()

	l := newLimiter()
	now := time.Now()
	for i := range 2000 {
		if !l.allowNewInstall("9.9.9.9", "install-same", now) {
			t.Fatalf("call %d: a repeated install_id must never itself trip the cardinality guard", i)
		}
	}
}

func TestLimiter_AllowNewInstall_ResetsNextHour(t *testing.T) {
	t.Parallel()

	l := newLimiter()
	hour1 := time.Date(2026, 7, 2, 10, 30, 0, 0, time.UTC)
	for i := range 1000 {
		id := "install-" + time.Duration(i).String()
		l.allowNewInstall("9.9.9.9", id, hour1)
	}
	if l.allowNewInstall("9.9.9.9", "install-overflow", hour1) {
		t.Fatal("expected cap reached within hour1")
	}
	hour2 := hour1.Add(time.Hour)
	if !l.allowNewInstall("9.9.9.9", "install-overflow", hour2) {
		t.Error("expected the per-hour cardinality counter to reset in the next hour bucket")
	}
}

func TestLimiter_AllowNewInstall_IndependentPerIP(t *testing.T) {
	t.Parallel()

	l := newLimiter()
	now := time.Now()
	for i := range 1000 {
		id := "install-" + time.Duration(i).String()
		l.allowNewInstall("9.9.9.9", id, now)
	}
	if !l.allowNewInstall("1.1.1.1", "install-fresh", now) {
		t.Error("a different IP must have its own independent cardinality counter")
	}
}

func TestLimiter_GC_PurgesStaleEntries(t *testing.T) {
	t.Parallel()

	l := newLimiter()
	now := time.Now()
	l.allowIP("1.2.3.4", now)
	l.allowInstallDaily("install-a", now)
	l.allowNewInstall("1.2.3.4", "install-a", now)

	l.gc(now.Add(48 * time.Hour))

	// After GC well past every window, a fresh probe must behave as if the
	// state never existed (full burst/cap available again) — proving the
	// stale maps were purged rather than merely stale-but-retained.
	for i := range 600 {
		if allowed, _ := l.allowIP("1.2.3.4", now.Add(48*time.Hour)); !allowed {
			t.Fatalf("post-GC request %d unexpectedly rejected", i)
		}
	}
}
