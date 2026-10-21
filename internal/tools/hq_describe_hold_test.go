// Tests for: tools/hq_describe_retry.go — a describe holds its pair's
// checkout, run against a fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/workflow"
)

// TestDescribeChange_WaitsWhileItsPairIsHeld: a describe never interleaves
// with a pass or a stand-up on its pair — a pass clearing the change while a
// describe writes onto it would leave the words on a change the pair no longer
// holds. A describe holds the pair, waits for a holder in flight, and writes
// nothing when the pair stays held.
func TestDescribeChange_WaitsWhileItsPairIsHeld(t *testing.T) {
	lab := openedLab(t)
	prev := hqPairLockWait
	hqPairLockWait = 50 * time.Millisecond
	t.Cleanup(func() { hqPairLockWait = prev })
	release, err := workflow.LockPair(t.Context(), lab.stateDir, "appdev", 0)
	if err != nil {
		t.Fatal(err)
	}
	text, _ := lab.describeTitled("", "A footer on every page", describedWords)
	for _, want := range []string{"Nothing was written", "held its checkout", "describe-change"} {
		if !strings.Contains(text, want) {
			t.Errorf("the answer misses %q:\n%s", want, text)
		}
	}
	if change := lab.hq.change(1); change.Body != "" || change.Title == "A footer on every page" {
		t.Errorf("a describe wrote onto change #1 while its pair was held: %+v", change)
	}
	release()
	if text, isError := lab.describeTitled("", "A footer on every page", describedWords); isError || !strings.Contains(text, `"described":true`) {
		t.Errorf("once the pair is let go, the describe lands:\n%s", text)
	}
}
