package eval

import (
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
)

// TestCallShape_Parse_Table pins ParseCallShape's grammar: `<tool>` or
// `<tool>{k=v,k2=v2}`; anything else is a parse error.
func TestCallShape_Parse_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		expr    string
		want    CallShape
		wantErr bool
	}{
		{"bare_tool", "zerops_delete", CallShape{Tool: "zerops_delete", Args: map[string]string{}}, false},
		{"tool_with_one_arg", "zerops_import{override=true}", CallShape{Tool: "zerops_import", Args: map[string]string{"override": "true"}}, false},
		{"tool_with_two_args", "zerops_workflow{action=start,workflow=export}", CallShape{Tool: "zerops_workflow", Args: map[string]string{"action": "start", "workflow": "export"}}, false},
		{"empty_string", "", CallShape{}, true},
		{"unbalanced_brace", "zerops_import{override=true", CallShape{}, true},
		{"empty_braces_is_bare_tool_equivalent", "zerops_delete{}", CallShape{Tool: "zerops_delete", Args: map[string]string{}}, false},
		{"malformed_arg_no_equals", "zerops_import{override}", CallShape{}, true},
		{"malformed_leading_paren", "not a valid ( expr", CallShape{}, true},
		{"invalid_tool_chars", "zerops-delete", CallShape{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseCallShape(tt.expr)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseCallShape(%q): expected error, got %+v", tt.expr, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCallShape(%q): unexpected error: %v", tt.expr, err)
			}
			if got.Tool != tt.want.Tool || len(got.Args) != len(tt.want.Args) {
				t.Fatalf("ParseCallShape(%q) = %+v, want %+v", tt.expr, got, tt.want)
			}
			for k, v := range tt.want.Args {
				if got.Args[k] != v {
					t.Errorf("ParseCallShape(%q).Args[%q] = %q, want %q", tt.expr, k, got.Args[k], v)
				}
			}
		})
	}
}

// TestParseCallShape_Operators pins FM-60: the call-shape grammar gains
// `k=v` (existing, unchanged), `k≠v`, `k∈<prefix>`, `k~<regex>`, plus the
// existing error cases (a bad regex, and a symbol that isn't one of the
// four recognised operators).
func TestParseCallShape_Operators(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{"equals", "zerops_deploy{strategy=git-push}", false},
		{"not_equals", "zerops_deploy{strategy≠git-push}", false},
		{"prefix", "zerops_deploy{workingDir∈/var/www/appdev}", false},
		{"regex", "zerops_deploy{hostname~^app.*$}", false},
		{"bad_regex", "zerops_deploy{hostname~(unclosed}", true},
		{"unknown_operator", "zerops_deploy{strategy!git-push}", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseCallShape(tt.expr)
			if tt.wantErr && err == nil {
				t.Fatalf("ParseCallShape(%q): expected error, got none", tt.expr)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ParseCallShape(%q): unexpected error: %v", tt.expr, err)
			}
		})
	}
}

// TestCallShape_Match_AbsentKeyIsFalse pins the documented rule: a key
// absent from the call's arguments makes ANY constraint on that key false,
// regardless of operator — so `never: zerops_deploy{strategy≠git-push}`
// does not fire on a call that carries no `strategy` argument at all.
func TestCallShape_Match_AbsentKeyIsFalse(t *testing.T) {
	t.Parallel()
	shape, err := ParseCallShape("zerops_deploy{strategy≠git-push}")
	if err != nil {
		t.Fatalf("ParseCallShape: unexpected error: %v", err)
	}
	call := capture.MCPToolCall{Tool: "zerops_deploy", Arguments: map[string]any{"hostname": "api"}}
	if shape.Matches(call) {
		t.Error("shape.Matches(call) = true, want false — the call has no `strategy` argument at all")
	}
}

// TestVerification_NeverRow_FailsOnMatchingCall pins FM-30: one matching
// call anywhere in the stream fails the row; zero matching calls passes it;
// no captured stream blocks it.
func TestVerification_NeverRow_FailsOnMatchingCall(t *testing.T) {
	t.Parallel()

	t.Run("one matching call — failed", func(t *testing.T) {
		t.Parallel()
		calls := []capture.MCPToolCall{
			{Tool: "zerops_discover", Action: "status"},
			{Tool: "zerops_import", Arguments: map[string]any{"override": true}},
		}
		rows := EvaluateNeverRows([]string{"zerops_import{override=true}"}, calls, true, time.Now())
		if len(rows) != 1 || rows[0].Result != CheckFailed {
			t.Fatalf("expected single failed row, got %+v", rows)
		}
		if rows[0].ID != "decision/zerops_import{override=true}" {
			t.Errorf("row id = %q", rows[0].ID)
		}
	})

	t.Run("zero matching calls — passed", func(t *testing.T) {
		t.Parallel()
		calls := []capture.MCPToolCall{
			{Tool: "zerops_discover", Action: "status"},
			{Tool: "zerops_import", Arguments: map[string]any{"override": false}},
		}
		rows := EvaluateNeverRows([]string{"zerops_import{override=true}"}, calls, true, time.Now())
		if len(rows) != 1 || rows[0].Result != CheckPassed {
			t.Fatalf("expected single passed row, got %+v", rows)
		}
	})

	t.Run("no captured stream — blocked", func(t *testing.T) {
		t.Parallel()
		rows := EvaluateNeverRows([]string{"zerops_delete"}, nil, false, time.Now())
		if len(rows) != 1 || rows[0].Result != CheckBlocked {
			t.Fatalf("expected single blocked row, got %+v", rows)
		}
	})

	t.Run("bare tool name matches any call to that tool", func(t *testing.T) {
		t.Parallel()
		calls := []capture.MCPToolCall{{Tool: "zerops_delete", Action: "service"}}
		rows := EvaluateNeverRows([]string{"zerops_delete"}, calls, true, time.Now())
		if len(rows) != 1 || rows[0].Result != CheckFailed {
			t.Fatalf("expected single failed row, got %+v", rows)
		}
	})
}

// TestVerification_AskWhenRow_AdvisoryNeverAggregates pins FM-31: an
// askWhen row is never included in aggregateTaskResult — a caller must
// exclude it from the check set passed to aggregation, regardless of the
// row's own Result.
func TestVerification_AskWhenRow_AdvisoryNeverAggregates(t *testing.T) {
	t.Parallel()

	t.Run("error not seen — passed", func(t *testing.T) {
		t.Parallel()
		row := EvaluateAskWhenRow(AskWhenObservation{ErrorCode: "GIT_TOKEN_MISSING", ErrorSeen: false}, true, time.Now())
		if row.Result != CheckPassed {
			t.Errorf("row = %+v, want passed", row)
		}
	})

	t.Run("error seen, user-sim turn asked — passed", func(t *testing.T) {
		t.Parallel()
		row := EvaluateAskWhenRow(AskWhenObservation{ErrorCode: "GIT_TOKEN_MISSING", ErrorSeen: true, UserSimTurnAsked: true}, true, time.Now())
		if row.Result != CheckPassed {
			t.Errorf("row = %+v, want passed", row)
		}
	})

	t.Run("error seen, no user-sim turn — failed but excluded from aggregation", func(t *testing.T) {
		t.Parallel()
		row := EvaluateAskWhenRow(AskWhenObservation{ErrorCode: "GIT_TOKEN_MISSING", ErrorSeen: true, UserSimTurnAsked: false}, true, time.Now())
		if row.Result != CheckFailed {
			t.Fatalf("row = %+v, want failed (advisory rows still carry an honest verdict)", row)
		}

		// The contract this test pins: a scenario's aggregated result comes
		// only from its outcome + never rows. This askWhen row's failed
		// verdict must never be folded into that aggregation.
		outcomeRows := []RequiredCheck{
			{ID: "expected_service/appdev/exists", Result: CheckPassed},
			{ID: "no_failed_processes/p1", Result: CheckPassed},
		}
		if got := aggregateTaskResult(outcomeRows); got != CheckPassed {
			t.Fatalf("aggregateTaskResult(outcomeRows) = %s, want passed", got)
		}
		mixedIn := append(append([]RequiredCheck{}, outcomeRows...), row)
		if got := aggregateTaskResult(mixedIn); got != CheckFailed {
			t.Fatalf("aggregateTaskResult with the askWhen row mixed in = %s (demonstrates why a caller must NOT mix it in — aggregateTaskResult itself has no askWhen concept)", got)
		}
	})

	t.Run("no captured stream — blocked", func(t *testing.T) {
		t.Parallel()
		row := EvaluateAskWhenRow(AskWhenObservation{ErrorCode: "GIT_TOKEN_MISSING", ErrorSeen: true}, false, time.Now())
		if row.Result != CheckBlocked {
			t.Errorf("row = %+v, want blocked", row)
		}
	})
}

// askWhenTestMutatingTools is a fixed mutating-tool set for the
// BuildAskWhenObservations tests — zerops_import and zerops_deploy are
// mutating, zerops_discover is read-only.
var askWhenTestMutatingTools = map[string]bool{"zerops_import": true, "zerops_deploy": true}

// TestAskWhenCorrelation_ErrorThenSimTurnBeforeMutation_Asked pins the
// "asked" verdict: an error call carrying the declared code at t1, a
// user-sim turn starting at t2 (between t1 and the next mutating call), and
// a mutating zerops_import call at t3 → UserSimTurnAsked=true.
func TestAskWhenCorrelation_ErrorThenSimTurnBeforeMutation_Asked(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2026, 9, 10, 0, 0, 1, 0, time.UTC)
	t2 := time.Date(2026, 9, 10, 0, 0, 2, 0, time.UTC)
	t3 := time.Date(2026, 9, 10, 0, 0, 3, 0, time.UTC)
	calls := []capture.MCPToolCall{
		{Tool: "zerops_deploy", ResultIsError: true, ResultText: `{"code":"GIT_TOKEN_MISSING"}`, At: t1},
		{Tool: "zerops_import", At: t3},
	}
	turns := []UserSimTurn{{StartedAt: t2}}

	obs := buildAskWhenObservation("GIT_TOKEN_MISSING", calls, turns, askWhenTestMutatingTools)
	if !obs.ErrorSeen {
		t.Fatal("ErrorSeen = false, want true")
	}
	if !obs.UserSimTurnAsked {
		t.Error("UserSimTurnAsked = false, want true")
	}
}

// TestAskWhenCorrelation_ErrorThenMutationWithoutTurn_NotAsked pins the
// "not asked" verdict: the mutating call follows the error directly, with
// no user-sim turn between them.
func TestAskWhenCorrelation_ErrorThenMutationWithoutTurn_NotAsked(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2026, 9, 10, 0, 0, 1, 0, time.UTC)
	t3 := time.Date(2026, 9, 10, 0, 0, 3, 0, time.UTC)
	calls := []capture.MCPToolCall{
		{Tool: "zerops_deploy", ResultIsError: true, ResultText: `{"code":"GIT_TOKEN_MISSING"}`, At: t1},
		{Tool: "zerops_import", At: t3},
	}
	obs := buildAskWhenObservation("GIT_TOKEN_MISSING", calls, nil, askWhenTestMutatingTools)
	if !obs.ErrorSeen {
		t.Fatal("ErrorSeen = false, want true")
	}
	if obs.UserSimTurnAsked {
		t.Error("UserSimTurnAsked = true, want false (no turn before the next mutating call)")
	}
}

// TestAskWhenCorrelation_ReadOnlyCallsBetween_DoNotCountAsMutation pins that
// a read-only call (zerops_discover) between the error and a user-sim turn
// does not close the correlation window — only a call whose tool is in
// mutatingTools does.
func TestAskWhenCorrelation_ReadOnlyCallsBetween_DoNotCountAsMutation(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2026, 9, 10, 0, 0, 1, 0, time.UTC)
	t2 := time.Date(2026, 9, 10, 0, 0, 2, 0, time.UTC)
	t3 := time.Date(2026, 9, 10, 0, 0, 3, 0, time.UTC)
	t4 := time.Date(2026, 9, 10, 0, 0, 4, 0, time.UTC)
	calls := []capture.MCPToolCall{
		{Tool: "zerops_deploy", ResultIsError: true, ResultText: `{"code":"GIT_TOKEN_MISSING"}`, At: t1},
		{Tool: "zerops_discover", At: t2}, // read-only, must not count as the mutating boundary
		{Tool: "zerops_import", At: t4},   // the actual next mutating call
	}
	turns := []UserSimTurn{{StartedAt: t3}} // between the read-only call and the mutating call

	obs := buildAskWhenObservation("GIT_TOKEN_MISSING", calls, turns, askWhenTestMutatingTools)
	if !obs.UserSimTurnAsked {
		t.Error("UserSimTurnAsked = false, want true — the read-only zerops_discover call must not close the window early")
	}
}

// TestAskWhen_WindowAnchorsOnCodedErrorOnly pins E9: the trigger is the
// first call whose ResultText contains THIS entry's own "code":"<code>"
// fragment — an earlier, unrelated ResultIsError=true call must never
// anchor the window (docs/spec-eval-farm.md §4.1 FM-31). Before the fix, any
// ResultIsError=true call triggered regardless of which code it carried, so
// a user-sim turn that answered an unrelated earlier error was wrongly
// counted as having answered THIS entry's error.
func TestAskWhen_WindowAnchorsOnCodedErrorOnly(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2026, 9, 10, 0, 0, 1, 0, time.UTC)
	t1h := time.Date(2026, 9, 10, 0, 0, 1, 500000000, time.UTC) // between the unrelated error and the coded one
	t2 := time.Date(2026, 9, 10, 0, 0, 2, 0, time.UTC)
	t3 := time.Date(2026, 9, 10, 0, 0, 3, 0, time.UTC)
	calls := []capture.MCPToolCall{
		{Tool: "zerops_deploy", ResultIsError: true, ResultText: `{"code":"SOME_OTHER_ERROR"}`, At: t1},
		{Tool: "zerops_discover", ResultText: `{"code":"GIT_TOKEN_MISSING"}`, At: t2}, // the actual coded error, read-only tool
		{Tool: "zerops_import", At: t3}, // the next mutating call
	}
	turns := []UserSimTurn{{StartedAt: t1h}} // before the coded error, only after the unrelated one

	obs := buildAskWhenObservation("GIT_TOKEN_MISSING", calls, turns, askWhenTestMutatingTools)
	if !obs.ErrorSeen {
		t.Fatal("ErrorSeen = false, want true")
	}
	if obs.UserSimTurnAsked {
		t.Error("UserSimTurnAsked = true, want false — the turn happened before the coded error, wrongly anchored by the earlier unrelated error under the pre-fix trigger")
	}
}

// TestNeverRows_DefaultOverrideInjected_AndAllowLifts pins FM-58:
// EffectiveNeverList always folds in DefaultNeverOverride alongside the
// scenario's own never list (de-duplicated), and an allow entry naming the
// same shape lifts it out of the effective list.
func TestNeverRows_DefaultOverrideInjected_AndAllowLifts(t *testing.T) {
	t.Parallel()

	t.Run("default injected when file declares nothing", func(t *testing.T) {
		t.Parallel()
		sc := &Scenario{Verification: &VerificationConfig{Never: []string{"zerops_delete"}}}
		got := EffectiveNeverList(sc)
		if len(got) != 2 {
			t.Fatalf("EffectiveNeverList = %v, want 2 entries (default + file's own)", got)
		}
		foundDefault, foundFile := false, false
		for _, expr := range got {
			if expr == DefaultNeverOverride {
				foundDefault = true
			}
			if expr == "zerops_delete" {
				foundFile = true
			}
		}
		if !foundDefault || !foundFile {
			t.Errorf("EffectiveNeverList = %v, want both %q and %q", got, DefaultNeverOverride, "zerops_delete")
		}
	})

	t.Run("file repeating the default does not duplicate it", func(t *testing.T) {
		t.Parallel()
		sc := &Scenario{Verification: &VerificationConfig{Never: []string{DefaultNeverOverride}}}
		got := EffectiveNeverList(sc)
		if len(got) != 1 || got[0] != DefaultNeverOverride {
			t.Errorf("EffectiveNeverList = %v, want exactly one entry (no duplicate)", got)
		}
	})

	t.Run("allow lifts the default", func(t *testing.T) {
		t.Parallel()
		sc := &Scenario{Verification: &VerificationConfig{
			Never: []string{"zerops_delete"},
			Allow: []AllowEntry{{Call: DefaultNeverOverride, Reason: "READY_TO_DEPLOY has no other path"}},
		}}
		got := EffectiveNeverList(sc)
		if len(got) != 1 || got[0] != "zerops_delete" {
			t.Errorf("EffectiveNeverList = %v, want only the file's own never entry (default lifted)", got)
		}
	})
}
