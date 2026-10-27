// Tests for: tools/hq_change_description.go — a describe holds its change,
// run against a fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/workflow"
)

// TestDescribeChange_WaitsWhileItsChangeIsHeld: a describe never interleaves
// with a pass, or another describe, on its change — a pass forgetting the
// change while a describe writes onto it would leave the words on a change
// zcp no longer keeps. A describe holds the change, waits for a holder in
// flight, and writes nothing when the change stays held; it holds no pair:
// the pair's checkout is not the change's.
func TestDescribeChange_WaitsWhileItsChangeIsHeld(t *testing.T) {
	lab := openedLab(t)
	prev := hqChangeHoldWait
	hqChangeHoldWait = 50 * time.Millisecond
	t.Cleanup(func() { hqChangeHoldWait = prev })
	pair, err := workflow.LockPair(t.Context(), lab.stateDir, "appdev", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer pair()
	release, err := workflow.HoldHQChange(t.Context(), lab.stateDir, labApp, "appdev", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	text, _ := lab.describeTitled("", "A footer on every page", describedWords)
	for _, want := range []string{"Nothing was written", "#1", "held", "describe-change"} {
		if !strings.Contains(text, want) {
			t.Errorf("the answer misses %q:\n%s", want, text)
		}
	}
	if change := lab.hq.change(1); change.Body != "" || change.Title == "A footer on every page" {
		t.Errorf("a describe wrote onto change #1 while it was held: %+v", change)
	}
	release()
	if text, isError := lab.describeTitled("", "A footer on every page", describedWords); isError || !strings.Contains(text, `"described":true`) {
		t.Errorf("once the change is let go, the describe lands — its pair held or not:\n%s", text)
	}
}
