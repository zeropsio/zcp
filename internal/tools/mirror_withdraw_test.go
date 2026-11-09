// Tests for: tools/mirror.go action="withdraw" — the agent takes back an ask
// to connect a repository to GitHub it no longer needs, so Mate closes its
// card instead of the agent saying so in chat (docs/spec-mate.md §5.11).
package tools

import (
	"strings"
	"testing"
)

// A withdraw writes nothing and asks HQ nothing; it answers
// {"withdrawn":{"mirror":{"repo"}},"nextActions"}, naming the repository the
// withdrawn ask named.
// witness: internal/tools/mirror.go | Withdrawn: &mirrorWithdrawn{Mirror: w}, → Withdrawn: &mirrorWithdrawn{},
func TestMirrorWithdraw_AnswersWhatItWithdrew(t *testing.T) {
	srv, others := mirrorHQ(t, `[]`)

	result := callTool(t, srv, "zerops_mirror", map[string]any{"action": "withdraw", "repo": "app"})

	text := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("want a withdraw, got a refusal: %s", text)
	}
	want := `{"withdrawn":{"mirror":{"repo":"app"}},"nextActions":"`
	if !strings.HasPrefix(text, want) {
		t.Errorf("answer = %s, want it to start %s", text, want)
	}
	if got := others(); len(got) != 0 {
		t.Errorf("a withdraw sent HQ %v", got)
	}
}
