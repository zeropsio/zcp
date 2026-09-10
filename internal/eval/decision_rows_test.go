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
