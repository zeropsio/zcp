package eval

import (
	"context"
	"path/filepath"
	"testing"
	"time"
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
		// Wave-2 bug: the English done-marker "live" appearing MID-BODY in a
		// Czech message (a "live Postgres timestamp" description) false-fired
		// VerdictDone, even though the message ends with a Czech hand-back cue
		// ("Dej mi další prompt") and no "?". Must classify as WAITING — the
		// done-marker scan is scoped to the tail + a modal/hand-back cue (incl.
		// Czech) takes precedence over a mid-body done word.
		{"waiting_czech_tail_live", "waiting_czech_tail_live.jsonl", VerdictWaiting, "Dej mi další prompt"},
		{"done_via_verify", "done_via_verify.jsonl", VerdictDone, "Verify confirms"},
		{"waiting_question_mark", "waiting_question_mark.jsonl", VerdictWaiting, "do you want to adjust"},
		{"waiting_modal_phrase", "waiting_modal_phrase.jsonl", VerdictWaiting, "Should I go with"},
		{"error_max_turns", "error_max_turns.jsonl", VerdictMaxTurns, ""},
		{"error_is_error", "error_is_error.jsonl", VerdictError, ""},
		{"working_mid_roundtrip", "working_mid_roundtrip.jsonl", VerdictWorking, ""},
		// AskUserQuestion is a wait signal by MCP semantics — permission_denial
		// in headless mode does not change the agent's intent. User-sim must
		// engage. `_denied` covers the streaming-split case (prior text in one
		// event + AskUQ tool_use in the next); `_alone` covers AskUQ as the
		// only content in the burst (text falls back to AskUQ question prose).
		{"ask_user_question_denied", "ask_user_question_denied.jsonl", VerdictWaiting, "Mám tři možnosti"},
		{"ask_user_question_alone", "ask_user_question_alone.jsonl", VerdictWaiting, "Which database engine"},
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

// TestUserSimTurn_RecordsStartedAt pins that a recorded UserSimTurn carries
// a non-zero UTC StartedAt set at the turn's build site, before its
// sim.Reply call — the askWhen decision-row correlation (docs/spec-eval-farm.md
// §4.1 FM-31) needs this to order a user-sim turn against the MCP tool-call
// stream.
func TestUserSimTurn_RecordsStartedAt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")

	ts := &transcriptScripter{
		path:   transcript,
		t:      t,
		states: []string{loadFixture(t, "done_via_text.jsonl")},
	}
	ts.install(loadFixture(t, "waiting_question_mark.jsonl"))

	sim := &stubSimRunner{replies: []string{"go with MariaDB"}}
	sc := &Scenario{Prompt: "Set up Laravel app", ID: "test"}
	res := &BehavioralResult{}

	before := time.Now().UTC()
	if err := runUserSimLoop(context.Background(), sc, "session-startedat", transcript, sim, ts.resume, ClassifyTranscriptTail, res); err != nil {
		t.Fatalf("runUserSimLoop: %v", err)
	}
	after := time.Now().UTC()

	if len(res.UserSim.Turns) != 1 {
		t.Fatalf("turn count: got %d, want 1", len(res.UserSim.Turns))
	}
	got := res.UserSim.Turns[0].StartedAt
	if got.IsZero() {
		t.Fatal("StartedAt is zero, want a recorded instant")
	}
	if got.Before(before) || got.After(after) {
		t.Errorf("StartedAt = %s, want within [%s, %s]", got, before, after)
	}
	if got.Location() != time.UTC {
		t.Errorf("StartedAt location = %v, want UTC", got.Location())
	}
}
