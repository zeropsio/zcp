// Tests for: tools/hq_pair.go — what a Mate keeping its code in HQ plans,
// launches and hands over.
package tools

import (
	"os"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq/hqtest"
	"github.com/zeropsio/zcp/internal/runtime"
)

// linkAs leaves this test's container without a key for HQ ("missing") or
// holding one ("kept"). Callers redirect HOME first, and the test is not
// parallel: the identity is the process environment.
func linkAs(t *testing.T, state string) {
	t.Helper()
	switch state {
	case "missing":
		hqtest.Link(t, "", "", "")
	case "kept":
		hqtest.Link(t, "https://hq.example", "p1", "c")
	default:
		t.Fatalf("unknown link state %q", state)
	}
}

// TestLaunchProduction_AMateKeepingItsCodeInHQRefusesAndSaysWhatIsTrue: an
// application's production is a project the person adds from the projects
// page, so a Mate keeping its code in HQ refuses launch-production outright,
// and its next step says what is TRUE of its own pairs — a change still open
// by number, Put it live once the work is on main, or to push and merge first
// — read fresh, so a change merged since the last pass is never named as
// open.
func TestLaunchProduction_AMateKeepingItsCodeInHQRefusesAndSaysWhatIsTrue(t *testing.T) {
	t.Parallel()
	if refusal := hqLaunchProductionRefusal(nil, runtime.Info{}, t.TempDir(), false); refusal != nil {
		t.Fatalf("a container that is not a Mate keeps zcp's own launch-production, got %q", resultText(t, refusal))
	}
	tests := []struct {
		name string
		// settle is what happens to change #1 before the launch is asked for;
		// nil opens no change at all.
		settle   func(lab *hqLab)
		want     []string
		wantNone []string
	}{
		{name: "no change at all", want: []string{"wired_mate_production_is_the_groups", "branch of your own"}},
		{name: "a change still open is named", settle: func(*hqLab) {}, want: []string{"#1", "appdev", "merge"}},
		{name: "HQ's open change is named after its record is lost", settle: func(lab *hqLab) { lab.forget(1) },
			want: []string{"#1", "appdev", "merge"}},
		{name: "a change merged since the last pass is not named as open", settle: func(lab *hqLab) { lab.hq.merge() },
			want: []string{"Put it live"}, wantNone: []string{"#1", "merge ", "branch of your own"}},
		{name: "a change closed without merging means push and merge first", settle: func(lab *hqLab) { lab.hq.close(1) },
			want: []string{"branch of your own"}, wantNone: []string{"#1", "Tell the person: production is added and released"}},
		{name: "a change that landed with another is on main, as a merged one", settle: func(lab *hqLab) { lab.hq.landWith(1, 2) },
			want: []string{"Put it live"}, wantNone: []string{"#1", "merge ", "branch of your own"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lab := newHQLab(t)
			lab.wire()
			if tt.settle != nil {
				lab.write(map[string]string{"index.js": "the app\n"})
				lab.openChange()
				tt.settle(lab)
				lab.snapshotMate(t.Context())
			}
			text := resultText(t, hqLaunchProductionRefusal(lab.hq.srv.Client(), lab.rt, lab.stateDir, true))
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Errorf("the refusal misses %q: %s", want, text)
				}
			}
			for _, never := range tt.wantNone {
				if strings.Contains(text, never) {
					t.Errorf("the refusal must not say %q: %s", never, text)
				}
			}
		})
	}
}

// HQ owns the merged history: a merged change the pair holds no record of
// still reads as work on main.
func TestProductionKnowsMergedWorkThePairHoldsNoRecordOf(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "the app\n"})
	lab.openChange()
	lab.hq.merge()
	lab.snapshotMate(t.Context())
	if kept := lab.ledger(); len(kept) != 0 {
		t.Fatalf("the ledger keeps %+v; want no record", kept)
	}
	text := hqLaunchProductionNextStep(lab.hq.srv.Client(), lab.rt, lab.stateDir)
	if !strings.Contains(text, "Put it live") || strings.Contains(text, "branch of your own") || strings.Contains(text, "merge ") {
		t.Fatalf("the guidance forgot the work HQ has merged: %s", text)
	}
	if !strings.Contains(text, "Put it live on the project's card") || strings.Contains(text, "both offered in this conversation") {
		t.Fatalf("production setup belongs to the project, not the change review: %s", text)
	}
}

// An unavailable authority is unknown, never evidence that no work landed:
// the Mate's server holds no state from HQ.
func TestProductionGuidance_HQUnavailableIsUnknown(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.wire()
	if err := os.Remove(lab.rt.HQ.StatePath); err != nil {
		t.Fatal(err)
	}
	text := hqLaunchProductionNextStep(lab.hq.srv.Client(), lab.rt, lab.stateDir)
	if !strings.Contains(text, "could not read") || !strings.Contains(text, "Ask again") || strings.Contains(text, "none of this Mate's work") {
		t.Fatalf("HQ's failure must be visible, with a manual next step: %s", text)
	}
}
