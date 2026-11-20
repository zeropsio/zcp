package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
)

// TestApplicationWords_SayWhatAChangeHoldsBesideAProductionOrOtherMates:
// open-change's answer says what one change holds wherever the application
// has a production, however it is run, and develop's start says it there and
// wherever other Mates work beside this one — never for a Mate alone with
// nothing live, a production of a kind zcp does not know, or an HQ older than
// the facts, which would send every Mate the clause regardless of its
// application (docs/spec-mate.md §10.10, MB-52, MB-53).
// witness: internal/tools/hq_application_has.go | case state.Mates > 1: → case state.Mates > 0:
func TestApplicationWords_SayWhatAChangeHoldsBesideAProductionOrOtherMates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		state         hq.MateState
		opened, start bool
	}{
		{name: "an HQ older than the facts", state: hq.MateState{}},
		{name: "this Mate alone, nothing live", state: hq.MateState{Mates: 1}},
		{name: "other Mates, nothing live", state: hq.MateState{Mates: 2}, start: true},
		{name: "a production HQ releases to", state: hq.MateState{Mates: 1, Production: hq.ProductionZerops}, opened: true, start: true},
		{name: "a production in Git", state: hq.MateState{Mates: 1, Production: hq.ProductionGit}, opened: true, start: true},
		{name: "a production run outside Mate", state: hq.MateState{Mates: 1, Production: hq.ProductionOutside}, opened: true, start: true},
		{name: "a production of a kind zcp does not know", state: hq.MateState{Mates: 1, Production: "elsewhere"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.Contains(applicationOpenedWords(tt.state), oneRunOfAsks); got != tt.opened {
				t.Errorf("open-change says what a change holds = %v, want %v", got, tt.opened)
			}
			if got := strings.Contains(applicationStartWords(tt.state), oneRunOfAsks); got != tt.start {
				t.Errorf("develop's start says what a change holds = %v, want %v", got, tt.start)
			}
		})
	}
}
