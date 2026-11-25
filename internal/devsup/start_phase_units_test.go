// Tests for: internal/devsup/state.go — where a starting server is
// (StartPhase).
package devsup

import "testing"

// ST-PHASE — A starting server's phase is the supervisor's alone: a start under way is launching even while the previous run's port had answered, and an awaited port is stalled only once this start's listen wait ran out, so no reader compares its own clock with the start or reads a restart of a healthy server as a stalled start.
// witness: internal/devsup/state.go | case underWay: → case underWay && !answered:
func TestStartPhase_STPhase_TheSupervisorsAlone(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		underWay, answered, waitEnded bool
		want                          StartPhase
	}{
		{underWay: true, want: StartLaunching},
		{underWay: true, answered: true, want: StartLaunching},
		{underWay: true, answered: true, waitEnded: true, want: StartLaunching},
		{want: StartAwaitingPort},
		{answered: true, waitEnded: true, want: StartStalled},
		{answered: true, want: StartListened},
	} {
		if got := startPhaseOf(tt.underWay, tt.answered, tt.waitEnded); got != tt.want {
			t.Errorf("startPhaseOf(underWay %v, answered %v, waitEnded %v) = %q, want %q", tt.underWay, tt.answered, tt.waitEnded, got, tt.want)
		}
	}
}
