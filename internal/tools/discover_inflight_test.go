package tools

import (
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestClassifyAdoptionState_InFlightBootstrap pins Friction-1: a runtime that
// is live on the platform but not yet meta-stamped (the window between
// zerops_import and the provision-step partial-meta write) must classify as
// AdoptionBootstrapping — SILENT (no warning) — when an alive bootstrap session
// that has reached provision is creating it. NOT adoptable (the agent created
// it) and NOT resumable (the session is alive, not a dead one to reclaim, so
// the route=resume / "route=adopt would reject" guidance must NOT fire).
func TestClassifyAdoptionState_InFlightBootstrap(t *testing.T) {
	t.Parallel()
	svc := &ops.ServiceInfo{Hostname: "api", Type: "nodejs@22"}

	// No meta, no in-flight session → adoptable (genuine untracked service).
	if got, _ := classifyAdoptionState(svc, nil, nil); got != ops.AdoptionAdoptable {
		t.Errorf("no meta + no in-flight: got %s, want adoptable", got)
	}

	// No meta, but an alive provision-stage session is creating this hostname
	// → bootstrapping (silent), no session ID.
	got, sid := classifyAdoptionState(svc, nil, map[string]bool{"api": true})
	if got != ops.AdoptionBootstrapping || sid != "" {
		t.Errorf("in-flight bootstrap: got (%s, %q), want (bootstrapping, \"\")", got, sid)
	}

	// A different hostname in-flight must not affect this service.
	if got, _ := classifyAdoptionState(svc, nil, map[string]bool{"other": true}); got != ops.AdoptionAdoptable {
		t.Errorf("unrelated in-flight host: got %s, want adoptable", got)
	}
}

// TestClassifyAdoptionState_InFlightOverridesResumable pins the deeper
// Friction-1 case (Codex re-review): after provision completes, the partial
// meta carries a BootstrapSession. While the SAME session is still alive (at
// the close step), in-flight must override the meta.BootstrapSession→resumable
// branch — otherwise discover emits a route=resume warning that the engine
// rejects for an alive session (dead-end loop). A meta with BootstrapSession
// whose session is NOT alive (not in-flight) is correctly resumable.
func TestClassifyAdoptionState_InFlightOverridesResumable(t *testing.T) {
	t.Parallel()
	svc := &ops.ServiceInfo{Hostname: "api", Type: "nodejs@22"}
	partial := map[string]*workflow.ServiceMeta{
		"api": {Hostname: "api", BootstrapSession: "sess-A"}, // incomplete (no BootstrappedAt)
	}
	complete := map[string]*workflow.ServiceMeta{
		"api": {Hostname: "api", BootstrapSession: "sess-A", BootstrappedAt: "2026-06-03T00:00:00Z"},
	}

	// Partial meta + alive session still owns it → bootstrapping (NOT resumable).
	if got, _ := classifyAdoptionState(svc, partial, map[string]bool{"api": true}); got != ops.AdoptionBootstrapping {
		t.Errorf("partial meta + alive session: got %s, want bootstrapping", got)
	}
	// Partial meta + session NOT alive (dead) → resumable (route=resume is correct).
	got, sid := classifyAdoptionState(svc, partial, nil)
	if got != ops.AdoptionResumable || sid != "sess-A" {
		t.Errorf("partial meta + dead session: got (%s, %q), want (resumable, sess-A)", got, sid)
	}
	// Complete meta → adopted regardless of in-flight.
	if got, _ := classifyAdoptionState(svc, complete, map[string]bool{"api": true}); got != ops.AdoptionAdopted {
		t.Errorf("complete meta: got %s, want adopted", got)
	}
}
