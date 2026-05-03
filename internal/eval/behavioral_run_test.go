package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadRetrospectivePrompt_BriefingFutureAgent_Embedded asserts the default
// retrospective prompt is embedded and loadable. Drift in embed path (e.g.
// rename or move) surfaces here, not as a runtime "prompt not found" during a
// live run.
func TestLoadRetrospectivePrompt_BriefingFutureAgent_Embedded(t *testing.T) {
	t.Parallel()
	body, err := LoadRetrospectivePrompt("briefing-future-agent")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.Contains(body, "briefing a future agent") {
		t.Errorf("loaded prompt missing expected phrase 'briefing a future agent'; got first 200 chars:\n%s", firstN(body, 200))
	}
}

// TestLoadRetrospectivePrompt_UnknownStyle_ListsAvailable asserts the error
// message for an unknown style includes the available styles, so an operator
// who typoed the scenario.retrospective.promptStyle gets actionable feedback.
func TestLoadRetrospectivePrompt_UnknownStyle_ListsAvailable(t *testing.T) {
	t.Parallel()
	_, err := LoadRetrospectivePrompt("does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown style")
	}
	if !strings.Contains(err.Error(), "available:") {
		t.Errorf("error message missing 'available:' hint: %v", err)
	}
	if !strings.Contains(err.Error(), "briefing-future-agent") {
		t.Errorf("error message should list briefing-future-agent: %v", err)
	}
}

// TestLoadRetrospectivePrompt_PathTraversal_Rejected guards against a scenario
// whose retrospective.promptStyle field contains slashes or dots. Without this
// the embed.FS lookup could be coaxed into walking out of the prompts dir.
func TestLoadRetrospectivePrompt_PathTraversal_Rejected(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"../scenario", "foo/bar", "foo.bar", ""} {
		if _, err := LoadRetrospectivePrompt(bad); err == nil {
			t.Errorf("expected error for style %q", bad)
		}
	}
}

// TestExtractSessionID_FirstSystemEvent_Returned confirms the parser pulls the
// session_id from the first system event in a stream-json transcript.
func TestExtractSessionID_FirstSystemEvent_Returned(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"system","subtype":"init","session_id":"ses_abc123","cwd":"/var/www"}
{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}
{"type":"system","subtype":"init","session_id":"ses_should_not_match"}
`
	path := writeTmp(t, "transcript.jsonl", jsonl)
	got, err := extractSessionID(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got != "ses_abc123" {
		t.Errorf("session_id = %q, want ses_abc123", got)
	}
}

// TestExtractSessionID_MissingSystemEvent_Errors_WithPreview pins the error
// path: no system event with session_id → error mentioning the file and a
// preview of leading events for debugging.
func TestExtractSessionID_MissingSystemEvent_Errors_WithPreview(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"assistant","message":{"content":[{"type":"text","text":"alone"}]}}
{"type":"result","is_error":false}
`
	path := writeTmp(t, "transcript.jsonl", jsonl)
	_, err := extractSessionID(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no system event") {
		t.Errorf("error message: %v", err)
	}
}

// TestExtractSelfReview_AssistantTextEvents_Joined asserts assistant text
// events from the retrospective JSONL are concatenated into the self-review.
// Non-assistant events and non-text content blocks are skipped.
func TestExtractSelfReview_AssistantTextEvents_Joined(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"system","subtype":"init","session_id":"ses_x"}
{"type":"assistant","message":{"content":[{"type":"text","text":"First paragraph."}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"unrelated"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Second paragraph."},{"type":"tool_use","name":"x"}]}}
{"type":"result","is_error":false}
`
	path := writeTmp(t, "retro.jsonl", jsonl)
	got, err := extractSelfReview(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	want := "First paragraph.\n\nSecond paragraph."
	if got != want {
		t.Errorf("self-review =\n%q\nwant\n%q", got, want)
	}
}

// TestExtractSelfReview_NoAssistantText_Errors pins the failure mode where the
// retrospective contains no extractable text (e.g. agent emitted only tool
// calls). RunBehavioralScenario uses this to write the .Error field.
func TestExtractSelfReview_NoAssistantText_Errors(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"system","subtype":"init","session_id":"ses_x"}
{"type":"result","is_error":false}
`
	path := writeTmp(t, "retro.jsonl", jsonl)
	if _, err := extractSelfReview(path); err == nil {
		t.Fatal("expected error for empty retrospective")
	}
}

// TestDetectCompaction covers all three signal forms — keeps the heuristic
// honest if Claude Code stream-json adds a new compaction signal we'd miss.
func TestDetectCompaction(t *testing.T) {
	t.Parallel()
	for name, jsonl := range map[string]string{
		"compact_boundary subtype": `{"type":"system","subtype":"compact_boundary"}`,
		"compacted bool":           `{"type":"system","subtype":"init","session_id":"x","compacted":true}`,
		"prose marker":             `{"type":"assistant","message":{"content":[{"type":"text","text":"Previous Conversation Compacted"}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := writeTmp(t, "retro.jsonl", jsonl)
			if !detectCompaction(path) {
				t.Errorf("expected compaction detected in %s", name)
			}
		})
	}

	t.Run("clean transcript", func(t *testing.T) {
		t.Parallel()
		path := writeTmp(t, "retro.jsonl", `{"type":"assistant","message":{"content":[{"type":"text","text":"normal post-hoc reply"}]}}`)
		if detectCompaction(path) {
			t.Error("false positive on clean transcript")
		}
	})
}

// TestParseScenario_Behavioral_FieldsPopulated round-trips the committed
// behavioral scenario through ParseScenario and asserts the new
// Tags/Area/Retrospective/NotableFriction fields populate.
func TestParseScenario_Behavioral_FieldsPopulated(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(filepath.Dir(wd)) // .../zcp
	path := filepath.Join(repoRoot, "eval", "behavioral", "scenarios", "greenfield-node-postgres-dev-stage.md")
	sc, err := ParseScenario(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !sc.IsBehavioral() {
		t.Fatal("scenario should be behavioral (retrospective set)")
	}
	if sc.Retrospective.PromptStyle != "briefing-future-agent" {
		t.Errorf("promptStyle = %q", sc.Retrospective.PromptStyle)
	}
	if len(sc.Tags) == 0 {
		t.Error("tags empty")
	}
	if sc.Area == "" {
		t.Error("area empty")
	}
	if len(sc.NotableFriction) != 2 {
		t.Errorf("notableFriction count = %d, want 2", len(sc.NotableFriction))
	}
	if sc.Seed != ModeEmpty {
		t.Errorf("seed = %q, want empty", sc.Seed)
	}
}

func writeTmp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	return p
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
