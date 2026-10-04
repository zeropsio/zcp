// Tests for: tools/hq_pending.go — a delivery HQ could not be reached for is
// finished without anyone asking, once HQ answers (SPEC §3.2a, §6.2.3).
package tools

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestKeepFinishingDeliveries_BacksOffWhileOneIsOwed: while a delivery is
// owed the rounds come on a growing wait, capped; once nothing is owed they
// come on the idle wait again, and the next delivery HQ cannot be reached for
// starts the backoff afresh. What a round says is logged once, not on every
// round that says it again.
func TestKeepFinishingDeliveries_BacksOffWhileOneIsOwed(t *testing.T) {
	owed := []int{0, 1, 1, 1, 1, 0, 1}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var waits []time.Duration
	var logged []string
	round := 0
	keepFinishingDeliveries(ctx, func(context.Context) (int, []string) {
		n := owed[round]
		round++
		if n > 0 {
			return n, []string{"appdev: HQ could not be reached"}
		}
		return 0, nil
	}, DeliveryKeepOptions{
		Idle: time.Minute, Retry: time.Second, RetryMax: 4 * time.Second,
		Log: func(line string) { logged = append(logged, line) },
	}, func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		if len(waits) == len(owed) {
			cancel()
			return context.Canceled
		}
		return nil
	})

	want := []time.Duration{time.Minute, time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second, time.Minute, time.Second}
	if !reflect.DeepEqual(waits, want) {
		t.Errorf("waits = %v, want %v", waits, want)
	}
	if len(logged) != 2 || !strings.Contains(logged[0], "HQ could not be reached") {
		t.Errorf("logged = %q, want the line said once per stretch it holds", logged)
	}
}

// TestFinishPendingDeliveries_OnceHQAnswers: HQ away, a stage deploy's
// delivery is owed; rounds while HQ still does not answer finish nothing and
// keep it owed; once HQ answers — first with a standby's 503, then for real
// — the round opens the change and pushes its branch, with nobody asked.
func TestFinishPendingDeliveries_OnceHQAnswers(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"footer.js": "the footer\n"})
	lab.hq.setDown(true)
	if d := lab.deliver(); d == nil || d.Change != nil {
		t.Fatalf("want a pending delivery, got %+v", d)
	}
	round := func() (int, []string) {
		return FinishPendingDeliveries(t.Context(), lab.mock, lab.hq.srv.Client(), lab.ssh, lab.rt, lab.stateDir)
	}

	for range 2 {
		if owed, lines := round(); owed != 1 || len(lines) != 0 {
			t.Fatalf("a round while HQ is away = %d owed, %q; want it still owed, nothing said", owed, lines)
		}
	}

	lab.hq.setDown(false)
	lab.hq.standby(1, nil)
	owed, lines := round()
	if owed != 0 || len(lines) != 1 || !strings.Contains(lines[0], "is done") {
		t.Fatalf("a round once HQ answers = %d owed, %q; want the delivery done", owed, lines)
	}
	if lab.hq.unavailable != 0 {
		t.Errorf("the round never met the standby")
	}
	if head := lab.remoteHead("mate/p-mate/1"); head != lab.git("rev-parse", "HEAD") {
		t.Errorf("change #1's branch is at %q, want the checkout's HEAD", head)
	}
	if record := lab.meta().HQ; record.Pending != nil || record.Change != 1 {
		t.Errorf("the pair's record = %+v, want change #1 and nothing owed", record)
	}
	if owed, lines := round(); owed != 0 || len(lines) != 0 {
		t.Errorf("a round with nothing owed = %d, %q; want it quiet", owed, lines)
	}
}
