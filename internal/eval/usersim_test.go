package eval

import (
	"path/filepath"
	"testing"
)

// TestClassifyTranscriptTail covers the seven-rule decision table from
// docs/spec / plans/flow-eval-usersim-2026-05-04.md §"Detection rules".
// Each fixture is a hand-authored stream-json transcript scoped to the minimum
// events needed to exercise a specific rule path. Order of fixtures mirrors
// rule order in the table; updating the table requires updating these.
func TestClassifyTranscriptTail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fixture  string
		wantKind VerdictKind
		// wantTextSubstr asserts a substring of LastAssistantText so the
		// classifier returns enough context for the user-sim prompt builder.
		// Empty string disables the check.
		wantTextSubstr string
	}{
		{"done_via_text", "done_via_text.jsonl", VerdictDone, "live"},
		{"done_via_verify", "done_via_verify.jsonl", VerdictDone, "Verify confirms"},
		{"waiting_question_mark", "waiting_question_mark.jsonl", VerdictWaiting, "do you want to adjust"},
		{"waiting_modal_phrase", "waiting_modal_phrase.jsonl", VerdictWaiting, "Should I go with"},
		{"error_max_turns", "error_max_turns.jsonl", VerdictMaxTurns, ""},
		{"error_is_error", "error_is_error.jsonl", VerdictError, ""},
		{"working_mid_roundtrip", "working_mid_roundtrip.jsonl", VerdictWorking, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join("testdata", "usersim", tt.fixture)
			got, err := ClassifyTranscriptTail(path)
			if err != nil {
				t.Fatalf("ClassifyTranscriptTail: %v", err)
			}
			if got.Kind != tt.wantKind {
				t.Errorf("Kind: got %v (reason=%q), want %v", got.Kind, got.Reason, tt.wantKind)
			}
			if tt.wantTextSubstr != "" && !containsSubstr(got.LastAssistantText, tt.wantTextSubstr) {
				t.Errorf("LastAssistantText: missing %q\ngot: %q", tt.wantTextSubstr, got.LastAssistantText)
			}
		})
	}
}

// TestClassifyTranscriptTail_FileMissing asserts a clear error for a path
// that does not exist — runner callers must distinguish a malformed/absent
// transcript from a verdict.
func TestClassifyTranscriptTail_FileMissing(t *testing.T) {
	t.Parallel()

	_, err := ClassifyTranscriptTail(filepath.Join("testdata", "usersim", "does-not-exist.jsonl"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func containsSubstr(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
