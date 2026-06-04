package workflow

import "testing"

// TestBootstrapReachedProvision pins the Friction-1 gate (Codex catch): an
// in-flight suppression must only apply once a session has REACHED the
// provision step (where zerops_import runs). A session still at discover has
// touched nothing on the platform, so a pre-existing same-named live service
// must stay genuinely adoptable, not be silently suppressed.
func TestBootstrapReachedProvision(t *testing.T) {
	t.Parallel()
	steps := []BootstrapStep{{Name: StepDiscover}, {Name: StepProvision}, {Name: StepClose}}
	cases := []struct {
		name string
		cur  int
		want bool
	}{
		{"at discover — not reached", 0, false},
		{"at provision — reached", 1, true},
		{"past provision (close) — reached", 2, true},
	}
	for _, tc := range cases {
		if got := bootstrapReachedProvision(&BootstrapState{CurrentStep: tc.cur, Steps: steps}); got != tc.want {
			t.Errorf("%s: bootstrapReachedProvision = %v, want %v", tc.name, got, tc.want)
		}
	}
	// Defensive: a step list with no provision step never counts as reached.
	if bootstrapReachedProvision(&BootstrapState{CurrentStep: 5, Steps: []BootstrapStep{{Name: StepDiscover}}}) {
		t.Error("no provision step in the plan should never report reached")
	}
}
