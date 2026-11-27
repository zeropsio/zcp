package mate

import "testing"

// witness: internal/mate/copy_state.go | if state != "" { → if state != "" && state != CopyStarting {
func TestCopyState_CSTransitions_OnlyAllowedEventsChangeState(t *testing.T) {
	t.Parallel()
	events := []CopyEvent{
		{Kind: CopyRecorded}, {Kind: CopyStart}, {Kind: CopyListening}, {Kind: CopyStop},
		{Kind: CopyExit}, {Kind: CopyPark}, {Kind: CopyWake, Report: "running"},
		{Kind: CopyRelease}, {Kind: CopyLinkLost}, {Kind: CopyReport, Report: "running"},
	}
	// A dash is a refused event; other cells are literal resulting states.
	for _, row := range []struct {
		state CopyState
		want  []string
	}{
		{"parked", []string{"-", "-", "-", "-", "-", "parked", "running", "gone", "unknown", "-"}},
		{"starting", []string{"-", "starting", "running", "stopped", "failed", "parked", "-", "gone", "unknown", "running"}},
		{"running", []string{"-", "starting", "-", "stopped", "failed", "parked", "-", "gone", "unknown", "running"}},
		{"stopped", []string{"-", "starting", "-", "stopped", "-", "parked", "-", "gone", "unknown", "running"}},
		{"failed", []string{"-", "starting", "-", "stopped", "failed", "parked", "-", "gone", "unknown", "running"}},
		{"unknown", []string{"-", "-", "-", "-", "-", "parked", "-", "gone", "unknown", "running"}},
		{"gone", []string{"-", "-", "-", "-", "-", "-", "-", "-", "-", "-"}},
	} {
		for i, event := range events {
			got, accepted := NextCopyState(row.state, event)
			want, valid := CopyState(row.want[i]), row.want[i] != "-"
			if !valid {
				want = row.state
			}
			if got != want || accepted != valid {
				t.Errorf("%s + %s = %s, %v; want %s, %v", row.state, event.Kind, got, accepted, want, valid)
			}
		}
	}
	for _, row := range []struct {
		report string
		want   CopyState
	}{
		{"stopped", "stopped"}, {"starting", "starting"}, {"running", "running"}, {"crashed", "failed"}, {"failed", "failed"},
	} {
		got, ok := NextCopyState("unknown", CopyEvent{Kind: CopyReport, Report: row.report})
		if !ok || got != row.want {
			t.Errorf("unknown + report %s = %s, %v; want %s", row.report, got, ok, row.want)
		}
	}
	if got, ok := NextCopyState("", CopyEvent{Kind: CopyRecorded}); !ok || got != "stopped" {
		t.Errorf("recorded = %s, %v; want stopped", got, ok)
	}
	if got, ok := NextCopyState("unknown", CopyEvent{Kind: CopyReport, Report: "bogus"}); ok || got != "unknown" {
		t.Errorf("invalid report = %s, %v; want unchanged and refused", got, ok)
	}
}
