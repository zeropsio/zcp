package observer

import (
	"strings"
	"testing"
)

// TestPromptMD_NamesFormat2FieldsAndFindingCap pins §7.5's model-authored
// contract: prompt.md must keep asking for exactly the format-2 fields and
// the three-finding cap. A future edit to prompt.md that drops one of these
// breaks this test rather than silently drifting from the schema
// observation.go actually parses.
func TestPromptMD_NamesFormat2FieldsAndFindingCap(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"headline field", `"headline"`},
		{"story field", `"story"`},
		{"story.task", `"task"`},
		{"story.expected", `"expected"`},
		{"story.did", `"did"`},
		{"story.stuck", `"stuck"`},
		{"story.ending", `"ending"`},
		{"goal field", `"goal"`},
		{"checks.judged field", `"judged"`},
		{"findings field", `"findings"`},
		{"finding.surface", `"surface"`},
		{"finding.anchor", `"anchor"`},
		{"finding.span", `"span"`},
		{"finding.causedVerdict", `"causedVerdict"`},
		{"selfReview field", `"selfReview"`},
		{"three-finding cap", "three"},
		{"clean run OK headline convention", "OK —"},
		{"tool name without mcp__ prefix", "mcp__"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(PromptText, tc.want) {
				t.Errorf("prompt.md doesn't mention %q, want it to (the model-authored schema, §7.5)", tc.want)
			}
		})
	}
}
