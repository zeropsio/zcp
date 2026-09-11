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
		// item 2 (FIX2.md FIX2-DATA): the anchor must read the same on a
		// different run — don't anchor on this run's own transient path.
		{"anchor reads the same on a different run", "read the same on a different run"},
		// item 5: a session/turn limit is never a finding — it's captured
		// in story.ending, which alone makes the outcome inconclusive.
		{"session/turn limit is never a finding", "Never file a finding for the agent hitting its own session or turn limit"},
		// item 4: OK is for a finished, finding-free run only.
		{"OK headline requires no findings and a finished run", "only when you report no findings and the run finished"},
		// FIX2 round 2: never claim a ZCP field/option is missing (the
		// observer twice wrongly claimed stageType was missing though it
		// exists) — describe the observable gap instead.
		{"never claim a ZCP field is missing from ZCP's design", "Never say a ZCP field or option is missing from ZCP's design"},
		// FIX2 round 2: never put the owner enum in prose.
		{"never put the owner enum in prose", "Never put owner's enum value in prose"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(PromptText, tc.want) {
				t.Errorf("prompt.md doesn't mention %q, want it to (the model-authored schema, §7.5)", tc.want)
			}
		})
	}
}
