// Tests for: one process at a time runs git on a pair's checkout
// (workflow.LockPair) — the zcp of an agent describing or standing up, and a pass.
package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

// holdPair holds the pair's checkout the way another process would.
func (l *hqLab) holdPair() func() {
	l.t.Helper()
	release, err := workflow.LockPair(l.t.Context(), l.stateDir, "appdev", 0)
	if err != nil {
		l.t.Fatalf("hold the pair: %v", err)
	}
	return release
}

// TestAPassSkipsAPairHeldElsewhere: a pass meeting a pair another process
// holds leaves it — no git, nothing said, no attempt recorded, so its
// backoff does not grow — and takes it on the pass after.
func TestAPassSkipsAPairHeldElsewhere(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	release := lab.holdPair()
	ran := len(lab.ssh.commands)

	if lines := lab.wire(); len(lines) != 0 || len(lab.ssh.commands) != ran {
		t.Errorf("a pass over a held pair said %q and ran %d commands, want nothing", lines, len(lab.ssh.commands)-ran)
	}
	if state := readHQPairState(lab.stateDir, "appdev"); state.Attempts != 0 {
		t.Errorf("a skipped pair's backoff = %+v, want no attempt recorded", state)
	}
	release()

	if lines := lab.wire(); len(lines) != 1 || !strings.Contains(lines[0], "wired in HQ") {
		t.Fatalf("the pass after said %q, want the pair wired", lines)
	}
}
