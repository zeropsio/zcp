package observer

import (
	"strings"
	"testing"
)

func minimalDigestInput(steps []Step) DigestInput {
	return DigestInput{
		RunID:      "final1-recover-failed-buildfromgit-missing-dep",
		ScenarioID: "recover-failed-buildfromgit-missing-dep",
		Verdict:    "failed",
		Model:      "claude-sonnet-5",
		TaskPrompt: "fix the api service",
		Steps:      steps,
	}
}

// TestDigest_SectionsInFixedOrder pins §7.3: the digest carries all seven
// sections, in this fixed order, and the SCENARIO/SELF-REVIEW headers carry
// the spec's exact labels.
func TestDigest_SectionsInFixedOrder(t *testing.T) {
	steps := []Step{{N: 1, Kind: StepUser, Text: "fix the api service"}}
	in := minimalDigestInput(steps)
	text, _ := BuildDigest(in)

	sections := []string{"RUN", "TASK", "SCENARIO", "CHECKS", "STEPS", "FINAL STATE", "SELF-REVIEW"}
	last := -1
	for _, s := range sections {
		idx := strings.Index(text, s)
		if idx == -1 {
			t.Fatalf("section %q not found in digest:\n%s", s, text)
		}
		if idx <= last {
			t.Errorf("section %q at %d, want after previous section (%d)", s, idx, last)
		}
		last = idx
	}
	if !strings.Contains(text, "the agent never saw this file") {
		t.Errorf("SCENARIO header missing the spec label 'the agent never saw this file'")
	}
	if !strings.Contains(text, "written by the agent after the run, from its own memory") {
		t.Errorf("SELF-REVIEW header missing the spec label")
	}
}

// TestDigest_TruncationKeepsHeadAndTail pins §7.3's exact truncation
// numbers: a tool result over 6,000 chars keeps 4,000+2,000; an error
// result over 12,000 keeps 8,000+4,000; a thinking block or tool input over
// 2,000 keeps its first 2,000. Every cut is marked
// "[… <n> chars omitted …]" with n = the omitted char count — an
// independent hand-computed oracle, not the implementation's own count.
func TestDigest_TruncationKeepsHeadAndTail(t *testing.T) {
	cases := []struct {
		name   string
		step   Step
		marker string
	}{
		{
			name:   "tool result 10000",
			step:   Step{N: 2, Kind: StepTool, ToolName: "t", ToolInputJSON: "{}", ToolHasResult: true, ToolResultText: strings.Repeat("a", 10000)},
			marker: "[… 4000 chars omitted …]",
		},
		{
			name:   "error result 20000",
			step:   Step{N: 2, Kind: StepTool, ToolName: "t", ToolInputJSON: "{}", ToolHasResult: true, ToolIsError: true, ToolResultText: strings.Repeat("b", 20000)},
			marker: "[… 8000 chars omitted …]",
		},
		{
			name:   "thinking 3000",
			step:   Step{N: 2, Kind: StepThinking, Text: strings.Repeat("c", 3000)},
			marker: "[… 1000 chars omitted …]",
		},
		{
			name:   "input 3000",
			step:   Step{N: 2, Kind: StepTool, ToolName: "t", ToolInputJSON: strings.Repeat("d", 3000), ToolHasResult: true, ToolResultText: "ok"},
			marker: "[… 1000 chars omitted …]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			steps := []Step{{N: 1, Kind: StepUser, Text: "task"}, tc.step}
			in := minimalDigestInput(steps)
			text, _ := BuildDigest(in)
			if !strings.Contains(text, tc.marker) {
				t.Errorf("digest missing marker %q\n%s", tc.marker, text)
			}
		})
	}
}

// TestDigest_OverBudgetDropsMiddleSteps pins §7.3's overall 400,000-char
// digest budget: once the assembled digest would exceed it, whole steps are
// dropped from the middle of STEPS (never the head or the tail), marked
// "[… steps #<a>–#<b> omitted …]".
func TestDigest_OverBudgetDropsMiddleSteps(t *testing.T) {
	// 100 agent steps of 6000 chars each -> ~600,000 chars of step content
	// alone, well over the 400,000 budget; head/tail steps must survive.
	steps := []Step{{N: 1, Kind: StepUser, Text: "task"}}
	for i := 2; i <= 101; i++ {
		steps = append(steps, Step{N: i, Kind: StepAgent, Text: strings.Repeat("x", 6000)})
	}
	in := minimalDigestInput(steps)
	text, _ := BuildDigest(in)

	if len(text) > 400000 {
		t.Errorf("digest length = %d, want <= 400000", len(text))
	}
	if !strings.Contains(text, "#1 user") {
		t.Errorf("digest dropped step #1 (head); it must survive")
	}
	if !strings.Contains(text, "#101 agent") {
		t.Errorf("digest dropped step #101 (tail); it must survive")
	}
	if !strings.Contains(text, "omitted") {
		t.Errorf("digest over budget carries no omission marker:\n%s", firstN(text, 2000))
	}
}

// TestDigest_MissingOptionalFileSaysNotRecorded pins §7.2/§7.3: an absent
// optional file (self-review.md, platform-snapshot.json, scenario.md)
// renders as "(not recorded)" rather than an empty or missing section.
func TestDigest_MissingOptionalFileSaysNotRecorded(t *testing.T) {
	in := minimalDigestInput([]Step{{N: 1, Kind: StepUser, Text: "task"}})
	// ScenarioMD, FinalState, SelfReview all left zero-valued (absent).
	text, _ := BuildDigest(in)
	if strings.Count(text, "(not recorded)") != 3 {
		t.Errorf("want 3 occurrences of '(not recorded)' (scenario, final state, self-review), got %d:\n%s", strings.Count(text, "(not recorded)"), text)
	}
}
