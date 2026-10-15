package eval

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
)

// launchLine renders one stream-json transcript line shaped like the farm's
// captured transcripts (assistant tool_use, user tool_result).
func launchLine(t *testing.T, v map[string]any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func toolUseLine(t *testing.T, id, name string, input map[string]any) string {
	t.Helper()
	return launchLine(t, map[string]any{
		"type":             "assistant",
		"message":          map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}}},
		"wire_tool_inputs": map[string]any{id: input},
	})
}

func toolResultLine(t *testing.T, id, text string) string {
	t.Helper()
	return launchLine(t, map[string]any{
		"type":            "user",
		"message":         map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": text}}},
		"tool_use_result": map[string]any{"stdout": text},
	})
}

func textLine(t *testing.T, text string) string {
	t.Helper()
	return launchLine(t, map[string]any{
		"type":    "assistant",
		"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}},
	})
}

// TestTokenNotInTranscript_ManualPath_AcceptsOnlyReadAndLaunchKey pins
// docs/spec-workflows.md P-LP-14 on the manual path: the persona's key is a
// value the user pastes in, so it may appear in the agent's one read of
// $ZCP_E2E_LAUNCH_KEY and in the zerops_workflow launchKey argument — and
// nowhere else.
func TestTokenNotInTranscript_ManualPath_AcceptsOnlyReadAndLaunchKey(t *testing.T) {
	t.Parallel()
	const token = "Zk3-launch_secretValue42"
	sha := sha256Hex(token)
	readCmd := map[string]any{"command": `echo "$ZCP_E2E_LAUNCH_KEY"`}
	workflow := func(extra map[string]any) map[string]any {
		in := map[string]any{"action": "start", "workflow": "launch-production", "launchKey": token}
		maps.Copy(in, extra)
		return in
	}
	join := func(lines ...string) string { return strings.Join(lines, "\n") }

	cases := []struct {
		name string
		text func() string
		want CheckResult
	}{
		{"one read and the launchKey argument pass", func() string {
			return join(
				toolUseLine(t, "r1", "Bash", readCmd), toolResultLine(t, "r1", token+"\n"),
				toolUseLine(t, "w1", "mcp__zerops__zerops_workflow", workflow(nil)),
				toolResultLine(t, "w1", `{"status":"launched"}`))
		}, CheckPassed},
		{"a second read fails", func() string {
			return join(
				toolUseLine(t, "r1", "Bash", readCmd), toolResultLine(t, "r1", token+"\n"),
				toolUseLine(t, "r2", "Bash", readCmd), toolResultLine(t, "r2", token+"\n"))
		}, CheckFailed},
		{"the token in another tool argument fails", func() string {
			return join(
				toolUseLine(t, "r1", "Bash", readCmd), toolResultLine(t, "r1", token+"\n"),
				toolUseLine(t, "b1", "Bash", map[string]any{"command": "echo " + token + " > /tmp/k"}))
		}, CheckFailed},
		{"the token in a non-launchKey workflow argument fails", func() string {
			return join(
				toolUseLine(t, "w1", "mcp__zerops__zerops_workflow", workflow(map[string]any{"intent": "use " + token})))
		}, CheckFailed},
		{"the token in a tool result outside the read fails", func() string {
			return join(
				toolUseLine(t, "r1", "Bash", readCmd), toolResultLine(t, "r1", token+"\n"),
				toolUseLine(t, "c1", "Bash", map[string]any{"command": "cat /tmp/k"}), toolResultLine(t, "c1", token))
		}, CheckFailed},
		{"the token in the agent's text fails", func() string {
			return join(
				toolUseLine(t, "r1", "Bash", readCmd), toolResultLine(t, "r1", token+"\n"),
				textLine(t, "I used the key "+token+" to launch."))
		}, CheckFailed},
		{"a length-only check of the variable is not a read", func() string {
			return join(
				toolUseLine(t, "l1", "Bash", map[string]any{"command": `echo "${#ZCP_E2E_LAUNCH_KEY}"`}), toolResultLine(t, "l1", "40"),
				toolUseLine(t, "r1", "Bash", readCmd), toolResultLine(t, "r1", token+"\n"))
		}, CheckPassed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := evaluateTokenNotInTranscriptRow(tc.text(), nil, sha)
			if row.Result != tc.want {
				t.Fatalf("result = %s, want %s (%+v)", row.Result, tc.want, row)
			}
		})
	}
}
