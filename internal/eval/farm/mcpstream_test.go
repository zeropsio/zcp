package farm

import "testing"

// TestMCPStream_ToolCallsWithActionAndPhase_Parsed reads
// testdata/mcpstream/basic.jsonl — a hand-written, redacted fixture in the
// capture.Record JSONL shape (internal/capture/mcp.go writers; cross-checked
// against the golden run5 capture) — and pins that ReadMCPStream recovers,
// in call order: the tool name, the "action" argument when the tool's
// arguments carry one, and the envelope phase from whichever carrier the
// result uses (docs/spec-mate.md §1): a trailing fenced ```json
// zcp-envelope``` block for a prose result, a top-level "envelope" key for a
// JSON-document result, and no phase at all for a plain result.
func TestMCPStream_ToolCallsWithActionAndPhase_Parsed(t *testing.T) {
	t.Parallel()

	calls, err := ReadMCPStream("testdata/mcpstream/basic.jsonl")
	if err != nil {
		t.Fatalf("ReadMCPStream: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3: %+v", len(calls), calls)
	}

	status := calls[0]
	if status.Tool != "zerops_workflow" {
		t.Errorf("calls[0].Tool = %q, want zerops_workflow", status.Tool)
	}
	if status.Action != "status" {
		t.Errorf("calls[0].Action = %q, want status", status.Action)
	}
	if status.EnvelopePhase != "develop-active" {
		t.Errorf("calls[0].EnvelopePhase = %q, want develop-active (fenced carrier)", status.EnvelopePhase)
	}

	deploy := calls[1]
	if deploy.Tool != "zerops_deploy" {
		t.Errorf("calls[1].Tool = %q, want zerops_deploy", deploy.Tool)
	}
	if deploy.Action != "" {
		t.Errorf("calls[1].Action = %q, want empty (no action argument)", deploy.Action)
	}
	if service, _ := deploy.Arguments["service"].(string); service != "appdev" {
		t.Errorf("calls[1].Arguments[service] = %q, want appdev", service)
	}
	if deploy.EnvelopePhase != "deploy-active" {
		t.Errorf("calls[1].EnvelopePhase = %q, want deploy-active (envelope-key carrier)", deploy.EnvelopePhase)
	}

	discover := calls[2]
	if discover.Tool != "zerops_discover" {
		t.Errorf("calls[2].Tool = %q, want zerops_discover", discover.Tool)
	}
	if discover.EnvelopePhase != "" {
		t.Errorf("calls[2].EnvelopePhase = %q, want empty (plain result, no carrier)", discover.EnvelopePhase)
	}
	if discover.ResultIsError {
		t.Error("calls[2].ResultIsError = true, want false")
	}
}
