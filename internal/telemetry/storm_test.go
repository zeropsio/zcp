package telemetry

import (
	"testing"
	"time"
)

func TestStormGuard_AllowsWithinBurstThenDenies(t *testing.T) {
	now := time.Now()
	g := newStormGuard(now)

	allowed := 0
	for range int(stormBurst) + 50 {
		if g.allow(now) {
			allowed++
		}
	}
	if allowed != int(stormBurst) {
		t.Fatalf("allowed = %d, want exactly burst=%d with no elapsed time between calls", allowed, int(stormBurst))
	}
}

func TestStormGuard_RefillsOverTime(t *testing.T) {
	now := time.Now()
	g := newStormGuard(now)

	// Drain the whole burst.
	for range int(stormBurst) {
		if !g.allow(now) {
			t.Fatal("burst should not be exhausted yet")
		}
	}
	if g.allow(now) {
		t.Fatal("bucket should be empty immediately after burst drain")
	}

	// 1 second later, sustained rate (2/s) should have refilled ~2 tokens.
	later := now.Add(1 * time.Second)
	got := 0
	for range 5 {
		if g.allow(later) {
			got++
		}
	}
	if got != 2 {
		t.Fatalf("allowed after 1s refill = %d, want 2 (sustained rate)", got)
	}
}

func TestStormGuard_RecordDrop_FirstDropFiresImmediately(t *testing.T) {
	now := time.Now()
	g := newStormGuard(now)

	delta, due := g.recordDrop(now)
	if !due {
		t.Fatal("first drop after a quiet period must fire a throttle event immediately")
	}
	if delta != 1 {
		t.Fatalf("delta = %d, want 1", delta)
	}
}

func TestStormGuard_RecordDrop_SuppressedWithinWindow(t *testing.T) {
	now := time.Now()
	g := newStormGuard(now)

	if _, due := g.recordDrop(now); !due {
		t.Fatal("first drop should fire")
	}

	// Second drop 10s later, still within the 1-minute window: suppressed.
	_, due := g.recordDrop(now.Add(10 * time.Second))
	if due {
		t.Fatal("second drop within the 1-minute window must be suppressed")
	}
}

func TestStormGuard_RecordDrop_FiresAgainAfterWindowElapses(t *testing.T) {
	now := time.Now()
	g := newStormGuard(now)

	g.recordDrop(now)
	g.recordDrop(now.Add(10 * time.Second))
	g.recordDrop(now.Add(20 * time.Second))

	delta, due := g.recordDrop(now.Add(stormThrottleWindow + time.Second))
	if !due {
		t.Fatal("drop after the window elapsed must fire")
	}
	// The drop at now already fired+reset the counter; +10s, +20s, and this
	// one accumulate to 3 since the last throttle emission.
	if delta != 3 {
		t.Fatalf("delta = %d, want 3 (accumulated since last throttle emission)", delta)
	}
}

func TestStormGuard_TotalDropped_AccumulatesAcrossWindows(t *testing.T) {
	now := time.Now()
	g := newStormGuard(now)

	g.recordDrop(now)
	g.recordDrop(now.Add(10 * time.Second))
	g.recordDrop(now.Add(stormThrottleWindow + time.Second))

	if got := g.totalDropped(); got != 3 {
		t.Fatalf("totalDropped() = %d, want 3", got)
	}
}

func TestStormGuard_ExemptEventTypesNeverGoThroughGuard(t *testing.T) {
	for _, et := range []string{"tool_call", "cli_command"} {
		if !isStormGoverned(et) {
			t.Errorf("isStormGoverned(%q) = false, want true", et)
		}
	}
	for _, et := range []string{"session_start", "session_end", "client_throttle"} {
		if isStormGoverned(et) {
			t.Errorf("isStormGoverned(%q) = true, want false (exempt per spec §5.2)", et)
		}
	}
}
