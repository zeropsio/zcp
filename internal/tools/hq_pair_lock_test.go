// Tests for: one process at a time runs git on a pair's checkout
// (workflow.LockPair) — the zcp of an agent delivering or pushing, a pass,
// and `zcp service mate` finishing an owed delivery (hq_pending.go).
package tools

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/workflow"
)

// owedDelivery leaves the lab's pair with a delivery owed: HQ was away for
// it, and answers again.
func (l *hqLab) owedDelivery() {
	l.t.Helper()
	l.wire()
	l.write(map[string]string{"footer.js": "the footer\n"})
	l.hq.setDown(true)
	if d := l.deliver(); d == nil || d.Change != nil {
		l.t.Fatalf("want a pending delivery, got %+v", d)
	}
	l.hq.setDown(false)
}

// finishRound is one round of `zcp service mate` finishing owed deliveries.
func (l *hqLab) finishRound() (int, []string) {
	return FinishPendingDeliveries(l.t.Context(), l.mock, l.hq.srv.Client(), l.ssh, l.rt, l.stateDir)
}

// holdPair holds the pair's checkout the way another process would.
func (l *hqLab) holdPair() func() {
	l.t.Helper()
	release, err := workflow.LockPair(l.t.Context(), l.stateDir, "appdev", 0)
	if err != nil {
		l.t.Fatalf("hold the pair: %v", err)
	}
	return release
}

// TestTwoFinishesOfOnePairRunGitOnce: while one finish of an owed delivery
// runs git on the pair's checkout, another skips the pair — no git of its
// own, nothing said, the delivery still owed — and the first finishes it.
func TestTwoFinishesOfOnePairRunGitOnce(t *testing.T) {
	lab := newHQLab(t)
	lab.owedDelivery()
	inGit, goOn := make(chan struct{}), make(chan struct{})
	var paused atomic.Bool
	lab.ssh.pause = func(string) {
		if paused.CompareAndSwap(false, true) {
			close(inGit)
			<-goOn
		}
	}
	first := make(chan []string, 1)
	go func() {
		_, lines := lab.finishRound()
		first <- lines
	}()
	<-inGit
	ran := len(lab.ssh.commands)

	owed, lines := lab.finishRound()
	if owed != 1 || len(lines) != 0 || len(lab.ssh.commands) != ran {
		t.Errorf("the second finish = %d owed, %q, %d commands; want it to skip the pair: still owed, nothing said, no git",
			owed, lines, len(lab.ssh.commands)-ran)
	}
	close(goOn)
	if lines := <-first; len(lines) != 1 || !strings.Contains(lines[0], "is done") {
		t.Fatalf("the first finish said %q, want the delivery done", lines)
	}
	if lab.hq.change(1) == nil || lab.hq.change(2) != nil {
		t.Errorf("want change #1 opened once")
	}
}

// TestAPassSkipsAPairHeldElsewhere: a pass meeting a pair another process
// holds leaves it — no git, nothing said, no attempt recorded, so its
// backoff does not grow — and takes it on the pass after.
func TestAPassSkipsAPairHeldElsewhere(t *testing.T) {
	lab := newHQLab(t)
	lab.owedDelivery()
	elapseHQBackoff(t, lab.stateDir)
	release := lab.holdPair()
	ran := len(lab.ssh.commands)

	if lines := lab.wire(); len(lines) != 0 || len(lab.ssh.commands) != ran {
		t.Errorf("a pass over a held pair said %q and ran %d commands, want nothing", lines, len(lab.ssh.commands)-ran)
	}
	if state := readHQPairState(lab.stateDir, "appdev"); state.Attempts != 0 {
		t.Errorf("a skipped pair's backoff = %+v, want no attempt recorded", state)
	}
	release()

	if lines := lab.wire(); len(lines) != 1 || !strings.Contains(lines[0], "is done") {
		t.Fatalf("the pass after said %q, want the delivery done", lines)
	}
}

// TestADeliveryWaitsForItsPairsCheckout: an agent's delivery meeting its pair
// held waits for it, and delivers once it is let go within the wait; held
// past the wait, it runs no git and says why its code has not reached HQ.
func TestADeliveryWaitsForItsPairsCheckout(t *testing.T) {
	tests := []struct {
		name      string
		releaseIn time.Duration // 0: held past the wait
		wantLine  string
	}{
		{name: "let go within the wait", releaseIn: 100 * time.Millisecond, wantLine: "change #1"},
		{name: "held past the wait", wantLine: "another delivery of appdev held its checkout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			lab.write(map[string]string{"footer.js": "the footer\n"})
			prev := hqPairLockWait
			hqPairLockWait = 2 * time.Second
			if tt.releaseIn == 0 {
				hqPairLockWait = 50 * time.Millisecond
			}
			t.Cleanup(func() { hqPairLockWait = prev })
			release := lab.holdPair()
			if tt.releaseIn > 0 {
				time.AfterFunc(tt.releaseIn, release)
			} else {
				defer release()
			}
			ran := len(lab.ssh.commands)

			d := lab.deliver()
			if d == nil || !strings.Contains(d.Line, tt.wantLine) {
				t.Fatalf("the delivery = %+v, want its line to say %q", d, tt.wantLine)
			}
			if tt.releaseIn == 0 {
				if d.Change != nil || len(lab.ssh.commands) != ran || lab.hq.change(1) != nil {
					t.Errorf("a delivery that never got its checkout ran %d commands, change %+v", len(lab.ssh.commands)-ran, d.Change)
				}
				return
			}
			if d.Change == nil || lab.remoteHead("mate/p-mate/1") != lab.git("rev-parse", "HEAD") {
				t.Errorf("the delivery after the wait = %+v, want change #1 at the checkout's HEAD", d.Change)
			}
		})
	}
}
