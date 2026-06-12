package workflow

import (
	"os"
	"testing"
)

// TestResolveLifecycle_Precedence pins the focus rule (spec §5.3/§6.2): an
// infra-layer session FOREGROUNDS an open work session (infra-first), else an
// open/auto-closed work session, else idle — all resolved from DISK. This is
// the single precedence both the envelope (derivePhase) and the dispatcher
// status routing consume; the SPINE-1 bug was two functions reading OPPOSITE
// precedence from different sources.
func TestResolveLifecycle_Precedence(t *testing.T) {
	t.Run("idle: no work, no infra", func(t *testing.T) {
		if got := ResolveLifecycle(t.TempDir(), nil); got != FocusIdle {
			t.Errorf("got %v, want FocusIdle", got)
		}
	})
	t.Run("work: open ws, no infra", func(t *testing.T) {
		ws := NewWorkSession("proj", "local", "task", []string{"web"})
		if got := ResolveLifecycle(t.TempDir(), ws); got != FocusWork {
			t.Errorf("got %v, want FocusWork", got)
		}
	})
	t.Run("infra-first: open ws + bootstrap registered -> FocusBootstrap", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := InitSessionAtomic(dir, "proj", WorkflowBootstrap, "add redis"); err != nil {
			t.Fatalf("InitSessionAtomic: %v", err)
		}
		ws := NewWorkSession("proj", "local", "task", []string{"web"})
		// The SPINE-1 fix: the coexisting OPEN work session does NOT win — infra
		// foregrounds it.
		if got := ResolveLifecycle(dir, ws); got != FocusBootstrap {
			t.Errorf("got %v, want FocusBootstrap (infra foregrounds work)", got)
		}
	})
	t.Run("stale v2 recipe entry -> not an infra focus", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := InitSessionAtomic(dir, "proj", "recipe", "author"); err != nil {
			t.Fatalf("InitSessionAtomic: %v", err)
		}
		if got := ResolveLifecycle(dir, nil); got != FocusIdle {
			t.Errorf("got %v, want FocusIdle (retired v2 recipe sessions must not foreground)", got)
		}
	})
	t.Run("auto-closed ws -> FocusWork (renders develop-closed-auto, not idle)", func(t *testing.T) {
		ws := NewWorkSession("proj", "local", "task", []string{"web"})
		ws.ClosedAt = "2026-05-29T00:00:00Z"
		ws.CloseReason = CloseReasonAutoComplete
		if got := ResolveLifecycle(t.TempDir(), ws); got != FocusWork {
			t.Errorf("got %v, want FocusWork", got)
		}
	})
	t.Run("non-auto closed ws (iteration-cap) -> FocusIdle", func(t *testing.T) {
		ws := NewWorkSession("proj", "local", "task", []string{"web"})
		ws.ClosedAt = "2026-05-29T00:00:00Z"
		ws.CloseReason = CloseReasonIterationCap
		if got := ResolveLifecycle(t.TempDir(), ws); got != FocusIdle {
			t.Errorf("got %v, want FocusIdle", got)
		}
	})
	t.Run("recycled-PID infra ghost: bootstrap entry with foreign StartTime is NOT foregrounded", func(t *testing.T) {
		dir := t.TempDir()
		// A dead predecessor's bootstrap entry whose PID number was recycled to
		// THIS process. infraPhaseForPID must skip it (two-state liveness) so a
		// ghost does not foreground over real work — the P7-review Issue C gap.
		if err := RegisterSession(dir, SessionEntry{
			SessionID: "ghost",
			PID:       os.Getpid(),
			StartTime: "0.0-foreign-start",
			Workflow:  WorkflowBootstrap,
			ProjectID: "proj",
			Intent:    "stale add redis",
		}); err != nil {
			t.Fatalf("RegisterSession: %v", err)
		}
		if got := ResolveLifecycle(dir, nil); got != FocusIdle {
			t.Errorf("got %v, want FocusIdle (recycled-PID infra ghost must not foreground)", got)
		}
	})
}
