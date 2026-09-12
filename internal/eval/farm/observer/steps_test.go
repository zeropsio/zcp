package observer

import (
	"strings"
	"testing"
)

// jsonl joins its arguments with newlines and appends a trailing newline —
// a small transcript.jsonl fixture builder for these tests.
func jsonl(lines ...string) []byte {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	return []byte(b.String())
}

const initLine = `{"type":"system","subtype":"init","session_id":"s1"}`

// TestSteps_TaskPromptIsStepOne pins docs/spec-eval-farm.md §7.2 point 1:
// step 1 is always kind "user" carrying task-prompt.txt's text, synthesized
// — never read off the transcript itself (the real transcript has no
// leading "user" event with the task text, verified live against
// ~/.zerops-dev/farm/reports/final1/*).
func TestSteps_TaskPromptIsStepOne(t *testing.T) {
	transcript := jsonl(initLine)
	steps, err := BuildSteps("fix the api service", transcript, nil)
	if err != nil {
		t.Fatalf("BuildSteps: %v", err)
	}
	if len(steps) == 0 {
		t.Fatalf("BuildSteps returned no steps")
	}
	if steps[0].N != 1 {
		t.Errorf("steps[0].N = %d, want 1", steps[0].N)
	}
	if steps[0].Kind != StepUser {
		t.Errorf("steps[0].Kind = %q, want %q", steps[0].Kind, StepUser)
	}
	if steps[0].Text != "fix the api service" {
		t.Errorf("steps[0].Text = %q, want task prompt text", steps[0].Text)
	}
}

// TestSteps_ToolUseJoinedWithResultByID pins §7.2 point 3: a tool step's
// result is the tool_result block sharing its tool_use_id (is_error kept),
// and a tool call with no matching result reads
// "(no result recorded)".
func TestSteps_ToolUseJoinedWithResultByID(t *testing.T) {
	transcript := jsonl(
		initLine,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"zerops_discover","input":{"a":1}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"ok result"}]}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"zerops_deploy","input":{}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":[{"type":"text","text":"boom"}]}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t3","name":"zerops_logs","input":{}}]}}`,
	)
	steps, err := BuildSteps("task", transcript, nil)
	if err != nil {
		t.Fatalf("BuildSteps: %v", err)
	}

	var tools []Step
	for _, s := range steps {
		if s.Kind == StepTool {
			tools = append(tools, s)
		}
	}
	if len(tools) != 3 {
		t.Fatalf("got %d tool steps, want 3: %+v", len(tools), tools)
	}

	if tools[0].ToolName != "zerops_discover" || tools[0].ToolResultText != "ok result" || tools[0].ToolIsError {
		t.Errorf("tools[0] = %+v, want name=zerops_discover result=%q isError=false", tools[0], "ok result")
	}
	if !tools[0].ToolHasResult {
		t.Errorf("tools[0].ToolHasResult = false, want true")
	}

	if tools[1].ToolName != "zerops_deploy" || tools[1].ToolResultText != "boom" || !tools[1].ToolIsError {
		t.Errorf("tools[1] = %+v, want name=zerops_deploy result=%q isError=true", tools[1], "boom")
	}

	if tools[2].ToolHasResult {
		t.Errorf("tools[2].ToolHasResult = true, want false (no matching tool_result)")
	}
	if tools[2].ToolResultText != "(no result recorded)" {
		t.Errorf("tools[2].ToolResultText = %q, want %q", tools[2].ToolResultText, "(no result recorded)")
	}
}

// TestSteps_ResumedSegmentInsertsUserSimReply pins §7.2 point 2: every
// system/init event after the first starts a resumed segment, contributing
// one "user" step whose text is the k-th (0-based) resume reply, or the
// documented placeholder when that reply wasn't recorded.
func TestSteps_ResumedSegmentInsertsUserSimReply(t *testing.T) {
	cases := []struct {
		name    string
		replies []string
		want    string
	}{
		{"reply present", []string{"go ahead and fix it"}, "go ahead and fix it"},
		{"reply absent", nil, "(user-sim turn; text not recorded)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transcript := jsonl(
				initLine,
				`{"type":"assistant","message":{"content":[{"type":"text","text":"first segment done"}]}}`,
				initLine,
				`{"type":"assistant","message":{"content":[{"type":"text","text":"second segment"}]}}`,
			)
			steps, err := BuildSteps("task", transcript, tc.replies)
			if err != nil {
				t.Fatalf("BuildSteps: %v", err)
			}
			// steps: #1 user(task) #2 agent(first segment) #3 user(resume) #4 agent(second segment)
			if len(steps) != 4 {
				t.Fatalf("got %d steps, want 4: %+v", len(steps), steps)
			}
			if steps[2].Kind != StepUser {
				t.Fatalf("steps[2].Kind = %q, want %q", steps[2].Kind, StepUser)
			}
			if steps[2].Text != tc.want {
				t.Errorf("steps[2].Text = %q, want %q", steps[2].Text, tc.want)
			}
		})
	}
}

// TestSteps_BlockOrderAndKinds pins §7.2 point 3-4: one assistant event
// carrying thinking/text/tool_use blocks contributes three steps in block
// order, and a non-assistant, non-resumed-init event (e.g. rate_limit_event,
// or a plain tool_result-only user event) is never a step.
func TestSteps_BlockOrderAndKinds(t *testing.T) {
	transcript := jsonl(
		initLine,
		`{"type":"rate_limit_event","rate_limit_info":{}}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"let me check"},{"type":"text","text":"checking now"},{"type":"tool_use","id":"t1","name":"zerops_discover","input":{}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"result"}]}]}}`,
	)
	steps, err := BuildSteps("task", transcript, nil)
	if err != nil {
		t.Fatalf("BuildSteps: %v", err)
	}
	// #1 user(task) #2 thinking #3 agent #4 tool
	if len(steps) != 4 {
		t.Fatalf("got %d steps, want 4: %+v", len(steps), steps)
	}
	wantKinds := []StepKind{StepUser, StepThinking, StepAgent, StepTool}
	for i, want := range wantKinds {
		if steps[i].Kind != want {
			t.Errorf("steps[%d].Kind = %q, want %q", i, steps[i].Kind, want)
		}
		if steps[i].N != i+1 {
			t.Errorf("steps[%d].N = %d, want %d", i, steps[i].N, i+1)
		}
	}
	if steps[1].Text != "let me check" {
		t.Errorf("steps[1].Text = %q, want %q", steps[1].Text, "let me check")
	}
	if steps[2].Text != "checking now" {
		t.Errorf("steps[2].Text = %q, want %q", steps[2].Text, "checking now")
	}
}

// TestSteps_SameBytesSameNumbers pins §7.2's "a pure function of those
// files": building steps twice from the identical bytes yields identical
// numbering and content.
func TestSteps_SameBytesSameNumbers(t *testing.T) {
	transcript := jsonl(
		initLine,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"zerops_discover","input":{"x":1}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"r"}]}]}}`,
	)
	a, err := BuildSteps("task", transcript, nil)
	if err != nil {
		t.Fatalf("BuildSteps (a): %v", err)
	}
	b, err := BuildSteps("task", transcript, nil)
	if err != nil {
		t.Fatalf("BuildSteps (b): %v", err)
	}
	if len(a) != len(b) {
		t.Fatalf("len(a)=%d len(b)=%d, want equal", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("step %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}
