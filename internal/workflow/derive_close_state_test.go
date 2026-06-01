package workflow

import "testing"

// TestDeriveCloseState pins the P5 derived-auto-complete model: auto-complete is
// COMPUTED from the gate (never stamped), the DECLARED scope is the close
// denominator (verdict Blocker-2 — not a deployed-so-far footprint), and a
// persisted iteration-cap close is returned verbatim.
func TestDeriveCloseState(t *testing.T) {
	t.Run("derived auto-complete: all declared green, NOT persisted on disk", func(t *testing.T) {
		dir := t.TempDir()
		ws := NewWorkSession("p", "container", "task", []string{"web"})
		ws.Deploys = map[string][]DeployAttempt{"web": {{AttemptedAt: "t", SucceededAt: "t"}}}
		ws.Verifies = map[string][]VerifyAttempt{"web": {{AttemptedAt: "t", PassedAt: "t", Passed: true}}}
		closed, _, reason := DeriveCloseState(dir, ws)
		if !closed || reason != CloseReasonAutoComplete {
			t.Errorf("DeriveCloseState = (closed=%v, reason=%q), want (true, %q)", closed, reason, CloseReasonAutoComplete)
		}
		if ws.ClosedAt != "" {
			t.Errorf("ClosedAt must stay unstamped — auto-complete is derived, not persisted: %q", ws.ClosedAt)
		}
	})

	t.Run("Blocker-2: declared scope is the denominator — one of two declared green stays active", func(t *testing.T) {
		dir := t.TempDir()
		ws := NewWorkSession("p", "container", "task", []string{"web", "api"})
		ws.Deploys = map[string][]DeployAttempt{"web": {{AttemptedAt: "t", SucceededAt: "t"}}}
		ws.Verifies = map[string][]VerifyAttempt{"web": {{AttemptedAt: "t", PassedAt: "t", Passed: true}}}
		// api (declared) has no deploy/verify — the task is NOT complete. A
		// deployed-so-far footprint would have fired here (the Blocker-2 trap).
		if closed, _, _ := DeriveCloseState(dir, ws); closed {
			t.Error("auto-complete fired while a DECLARED service (api) is undeployed — declared scope must be the close denominator")
		}
	})

	t.Run("iteration-cap close is persisted and returned verbatim", func(t *testing.T) {
		dir := t.TempDir()
		ws := NewWorkSession("p", "container", "task", []string{"web"})
		ws.ClosedAt = "2026-05-29T00:00:00Z"
		ws.CloseReason = CloseReasonIterationCap
		closed, closedAt, reason := DeriveCloseState(dir, ws)
		if !closed || reason != CloseReasonIterationCap || closedAt != "2026-05-29T00:00:00Z" {
			t.Errorf("DeriveCloseState = (%v, %q, %q), want persisted iteration-cap", closed, closedAt, reason)
		}
	})

	t.Run("genuinely active: nothing deployed -> not closed", func(t *testing.T) {
		ws := NewWorkSession("p", "container", "task", []string{"web"})
		if closed, _, _ := DeriveCloseState(t.TempDir(), ws); closed {
			t.Error("an untouched session must not be derived-closed")
		}
	})
}

// TestIsOpen pins the predicate that every lifecycle reader uses instead of a
// raw `ws.ClosedAt == ""` read — a DERIVED auto-complete session keeps ClosedAt
// unstamped on disk, so the raw read (the P7-review bug) would treat a done
// session as open and stuck-loop the agent on close+start-next.
func TestIsOpen(t *testing.T) {
	t.Run("nil -> not open", func(t *testing.T) {
		if IsOpen(t.TempDir(), nil) {
			t.Error("nil ws must not be open")
		}
	})
	t.Run("untouched session -> open", func(t *testing.T) {
		ws := NewWorkSession("p", "container", "task", []string{"web"})
		if !IsOpen(t.TempDir(), ws) {
			t.Error("a fresh session with no deploys must be open")
		}
	})
	t.Run("derived auto-complete -> not open (ClosedAt still unstamped)", func(t *testing.T) {
		dir := t.TempDir()
		ws := NewWorkSession("p", "container", "task", []string{"web"})
		ws.Deploys = map[string][]DeployAttempt{"web": {{AttemptedAt: "t", SucceededAt: "t"}}}
		ws.Verifies = map[string][]VerifyAttempt{"web": {{AttemptedAt: "t", PassedAt: "t", Passed: true}}}
		if ws.ClosedAt != "" {
			t.Fatal("precondition: ClosedAt must be unstamped")
		}
		if IsOpen(dir, ws) {
			t.Error("a derived auto-complete session must NOT be open (the stuck-loop bug)")
		}
	})
	t.Run("explicit/cap close -> not open", func(t *testing.T) {
		ws := NewWorkSession("p", "container", "task", []string{"web"})
		ws.ClosedAt = "2026-05-29T00:00:00Z"
		ws.CloseReason = CloseReasonIterationCap
		if IsOpen(t.TempDir(), ws) {
			t.Error("an explicitly closed session must not be open")
		}
	})
}
