package workflow

import "testing"

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
	t.Run("recipe registered -> FocusRecipe", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := InitSessionAtomic(dir, "proj", WorkflowRecipe, "author"); err != nil {
			t.Fatalf("InitSessionAtomic: %v", err)
		}
		if got := ResolveLifecycle(dir, nil); got != FocusRecipe {
			t.Errorf("got %v, want FocusRecipe", got)
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
}
