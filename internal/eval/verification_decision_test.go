package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
)

// TestToolArgRows_NeverAlwaysMax pins FM-59/FM-60's toolArg grading: never
// (one match fails), always (zero matches fails), max (more than N matches
// fails) — each exercised pass and fail against a fixed transcript.
func TestToolArgRows_NeverAlwaysMax(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		entry      ToolArgEntry
		wantResult CheckResult
	}{
		{
			name:       "never — one matching call fails",
			entry:      ToolArgEntry{Never: "Bash{command~^ln -s}"},
			wantResult: CheckFailed,
		},
		{
			name:       "never — zero matching calls passes",
			entry:      ToolArgEntry{Never: "Bash{command~^rm -rf}"},
			wantResult: CheckPassed,
		},
		{
			name:       "always — zero matches fails",
			entry:      ToolArgEntry{Always: "zerops_deploy{workingDir∈/var/www/otherdir}"},
			wantResult: CheckFailed,
		},
		{
			name:       "always — at least one match passes",
			entry:      ToolArgEntry{Always: "zerops_deploy{workingDir∈/var/www/appdev2}"},
			wantResult: CheckPassed,
		},
		{
			name:       "max — more than N matches fails",
			entry:      ToolArgEntry{Max: 1, Call: "zerops_import"},
			wantResult: CheckFailed,
		},
		{
			name:       "max — at most N matches passes",
			entry:      ToolArgEntry{Max: 2, Call: "zerops_import"},
			wantResult: CheckPassed,
		},
	}

	// Fixed transcript: two Bash calls (one "ln -s...", one "cd ..."), two
	// zerops_deploy calls both under /var/www/appdev2, two zerops_import
	// calls.
	uses := []TranscriptToolUse{
		{Tool: "Bash", Input: map[string]any{"command": "ln -s /a /b"}},
		{Tool: "Bash", Input: map[string]any{"command": "cd /var/www"}},
		{Tool: "zerops_deploy", Input: map[string]any{"workingDir": "/var/www/appdev2"}},
		{Tool: "zerops_deploy", Input: map[string]any{"workingDir": "/var/www/appdev2/sub"}},
		{Tool: "zerops_import", Input: map[string]any{}},
		{Tool: "zerops_import", Input: map[string]any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rows := evaluateToolArgRowsForTest(t, []ToolArgEntry{tt.entry}, uses)
			if len(rows) != 1 {
				t.Fatalf("len(rows) = %d, want 1", len(rows))
			}
			if rows[0].Result != tt.wantResult {
				t.Errorf("rows[0].Result = %s, want %s (row: %+v)", rows[0].Result, tt.wantResult, rows[0])
			}
		})
	}
}

// TestToolArgRows_NoTranscript_Blocked pins FM-59: a run with no transcript
// freezes every toolArg row blocked, never passed.
func TestToolArgRows_NoTranscript_Blocked(t *testing.T) {
	t.Parallel()
	entries := []ToolArgEntry{
		{Never: "zerops_delete"},
		{Always: "zerops_deploy"},
		{Max: 1, Call: "zerops_import"},
	}
	rows := evaluateToolArgRows(entries, "testdata/does-not-exist-transcript.jsonl", nil, false, time.Now())
	if len(rows) != len(entries) {
		t.Fatalf("len(rows) = %d, want %d", len(rows), len(entries))
	}
	for i, row := range rows {
		if row.Result != CheckBlocked {
			t.Errorf("rows[%d].Result = %s, want blocked (row: %+v)", i, row.Result, row)
		}
	}
}

// TestNeverRow_And_ToolArgNever_GradeIdentically pins that a `never` entry
// (graded from the captured MCP stream via evaluateNeverRow) and an
// equivalent `toolArg{never}` entry (graded from the transcript via
// evaluateToolArgRows) produce the same Result for the same underlying
// call — the two families must never diverge on the same shape.
func TestNeverRow_And_ToolArgNever_GradeIdentically(t *testing.T) {
	t.Parallel()
	const expr = "zerops_import{override=true}"

	t.Run("matching call — both fail", func(t *testing.T) {
		t.Parallel()
		calls := []capture.MCPToolCall{{Tool: "zerops_import", Arguments: map[string]any{"override": true}}}
		neverRows := EvaluateNeverRows([]string{expr}, calls, true, time.Now())

		uses := []TranscriptToolUse{{Tool: "zerops_import", Input: map[string]any{"override": true}}}
		toolArgRows := evaluateToolArgRowsForTest(t, []ToolArgEntry{{Never: expr}}, uses)

		if neverRows[0].Result != toolArgRows[0].Result {
			t.Fatalf("never row Result = %s, toolArg row Result = %s, want identical", neverRows[0].Result, toolArgRows[0].Result)
		}
		if neverRows[0].Result != CheckFailed {
			t.Fatalf("both rows Result = %s, want failed", neverRows[0].Result)
		}
	})

	t.Run("no matching call — both pass", func(t *testing.T) {
		t.Parallel()
		calls := []capture.MCPToolCall{{Tool: "zerops_import", Arguments: map[string]any{"override": false}}}
		neverRows := EvaluateNeverRows([]string{expr}, calls, true, time.Now())

		uses := []TranscriptToolUse{{Tool: "zerops_import", Input: map[string]any{"override": false}}}
		toolArgRows := evaluateToolArgRowsForTest(t, []ToolArgEntry{{Never: expr}}, uses)

		if neverRows[0].Result != toolArgRows[0].Result {
			t.Fatalf("never row Result = %s, toolArg row Result = %s, want identical", neverRows[0].Result, toolArgRows[0].Result)
		}
		if neverRows[0].Result != CheckPassed {
			t.Fatalf("both rows Result = %s, want passed", neverRows[0].Result)
		}
	})
}

// TestToolResultRows_ContainsAndField pins FM-59's toolResult grading over
// captured MCP results: `contains` passes iff at least one call to the
// named tool has a ResultText containing the substring, and blocks (never
// passes) when no MCP stream was captured at all.
//
// NOTE: scenario.go's ToolResultEntry (S2, out of this slice's write-set)
// carries only Tool/Contains — no Field. The `field` half of FM-59's
// "contains|field" shape (JSON field presence, top-level or inside a
// top-level envelope) cannot be exercised until ToolResultEntry gains that
// field; this test therefore covers `contains` only. See the slice report
// for the handoff.
func TestToolResultRows_ContainsAndField(t *testing.T) {
	t.Parallel()

	calls := []capture.MCPToolCall{
		{Tool: "zerops_discover", ResultText: `{"status":"ok"}`},
		{Tool: "zerops_env", ResultText: `{"restartedServices":["api"]}`},
	}

	t.Run("contains — matching call passes", func(t *testing.T) {
		t.Parallel()
		rows := evaluateToolResultRows([]ToolResultEntry{{Tool: "zerops_env", Contains: "restartedServices"}}, "", calls, true, time.Now())
		if len(rows) != 1 || rows[0].Result != CheckPassed {
			t.Fatalf("rows = %+v, want single passed row", rows)
		}
	})

	t.Run("contains — no matching call fails", func(t *testing.T) {
		t.Parallel()
		rows := evaluateToolResultRows([]ToolResultEntry{{Tool: "zerops_env", Contains: "nope-not-there"}}, "", calls, true, time.Now())
		if len(rows) != 1 || rows[0].Result != CheckFailed {
			t.Fatalf("rows = %+v, want single failed row", rows)
		}
	})

	t.Run("no captured stream — blocked", func(t *testing.T) {
		t.Parallel()
		rows := evaluateToolResultRows([]ToolResultEntry{{Tool: "zerops_env", Contains: "restartedServices"}}, "", nil, false, time.Now())
		if len(rows) != 1 || rows[0].Result != CheckBlocked {
			t.Fatalf("rows = %+v, want single blocked row", rows)
		}
	})
}

// TestMustOfferRows_RegexOverResultText pins FM-59's mustOffer grading:
// passed iff the regex matches at least one captured call's ResultText;
// blocked (never passed) with no captured stream.
func TestMustOfferRows_RegexOverResultText(t *testing.T) {
	t.Parallel()

	calls := []capture.MCPToolCall{
		{Tool: "zerops_workflow", ResultText: "next up: recipe:nestjs-minimal or build your own"},
	}

	t.Run("matching result passes", func(t *testing.T) {
		t.Parallel()
		rows := evaluateMustOfferRows([]string{"recipe:nestjs-minimal"}, "", calls, true, time.Now())
		if len(rows) != 1 || rows[0].Result != CheckPassed {
			t.Fatalf("rows = %+v, want single passed row", rows)
		}
	})

	t.Run("no matching result fails", func(t *testing.T) {
		t.Parallel()
		rows := evaluateMustOfferRows([]string{"recipe:nodejs-minimal"}, "", calls, true, time.Now())
		if len(rows) != 1 || rows[0].Result != CheckFailed {
			t.Fatalf("rows = %+v, want single failed row", rows)
		}
	})

	t.Run("no captured stream — blocked", func(t *testing.T) {
		t.Parallel()
		rows := evaluateMustOfferRows([]string{"recipe:nestjs-minimal"}, "", nil, false, time.Now())
		if len(rows) != 1 || rows[0].Result != CheckBlocked {
			t.Fatalf("rows = %+v, want single blocked row", rows)
		}
	})
}

// evaluateToolArgRowsForTest writes uses to a scratch transcript.jsonl-shaped
// fixture is unnecessary — evaluateToolArgRows takes a transcript PATH, but
// the tests above need to grade against an in-memory []TranscriptToolUse.
// This helper writes uses out as a minimal transcript.jsonl (one assistant
// tool_use event per use) to a temp file and calls the real
// evaluateToolArgRows against it, so the test exercises the same code path
// production does (file → ReadTranscriptToolUses → grading), not a bypass.
func evaluateToolArgRowsForTest(t *testing.T, entries []ToolArgEntry, uses []TranscriptToolUse) []RequiredCheck {
	t.Helper()
	path := writeTranscriptFixture(t, uses)
	return evaluateToolArgRows(entries, path, nil, false, time.Now())
}

// writeTranscriptFixture renders uses as a transcript.jsonl (one assistant
// tool_use event per use) into a temp file and returns its path, so tests
// can grade toolArg entries through the real file → ReadTranscriptToolUses
// path rather than fabricating a []TranscriptToolUse the production code
// never sees.
func writeTranscriptFixture(t *testing.T, uses []TranscriptToolUse) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create transcript fixture: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, u := range uses {
		event := map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []map[string]any{
					{"type": "tool_use", "name": u.Tool, "input": u.Input},
				},
			},
		}
		if err := enc.Encode(event); err != nil {
			t.Fatalf("encode transcript fixture event: %v", err)
		}
	}
	return path
}
