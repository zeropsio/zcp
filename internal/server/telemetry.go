package server

import (
	"encoding/json"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

// toolZeropsWorkflow is the only tool whose Arguments also carry step/route
// (spec §5.3) — every other tool gets tool+action only.
const toolZeropsWorkflow = "zerops_workflow"

// actionPeek is the single-key tiny-struct peek for the "action" argument,
// shared by every tool (spec §5.3 "nothing else is ever decoded from
// arguments").
type actionPeek struct {
	Action string `json:"action"`
}

// workflowPeek adds the two extra keys zerops_workflow carries. A single
// combined peek struct is fine per the plan — still only three allowlisted
// keys are ever read, and only for this one tool.
type workflowPeek struct {
	Step  string `json:"step"`
	Route string `json:"route"`
}

// errorCodePeek is the single-key peek of the ErrorWire JSON body
// (internal/tools.ErrorWire) carried in CallToolResult.Content[0].Text on
// error — server does not import internal/tools for this; the "code" /
// "subcode" json tags are the wire contract, pinned by
// internal/tools.TestErrorWire_*/TestConvertError_Subcode.
type errorCodePeek struct {
	Code    string `json:"code"`
	Subcode string `json:"subcode"`
}

// workflowContext remembers the LAST zerops_workflow route/step this
// process observed (spec §5.3 workflow-context stamping, telemetry-
// production-readiness plan S4) so every subsequent tool_call — regardless
// of tool — can be attributed to the workflow phase in flight when it
// fails (R4): before this, only zerops_workflow calls themselves ever
// carried workflow_route/workflow_step, so a deploy/import failure mid-
// workflow was undiagnosable by phase. Mutex-guarded: the MCP SDK dispatches
// concurrent tool_use blocks onto separate goroutines (spec §5.3
// architecture note), so Server.observe() can call remember/snapshot from
// more than one goroutine at once.
type workflowContext struct {
	mu    sync.Mutex
	route string
	step  string
}

// remember updates route/step from a zerops_workflow call's own
// (already shape-checked) peeked values. An empty field is skipped, not
// blanked — a call that only supplies step (e.g. action=status) must not
// erase a previously-remembered route, and vice versa.
func (w *workflowContext) remember(route, step string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if route != "" {
		w.route = route
	}
	if step != "" {
		w.step = step
	}
}

// snapshot returns the currently-remembered route/step for stamping onto a
// non-zerops_workflow tool_call event. Zero values (never seen a
// zerops_workflow call yet this process) stamp as empty, matching the
// pre-S4 behavior for that case.
func (w *workflowContext) snapshot() (route, step string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.route, w.step
}

// emitToolCallTelemetry builds and emits the tool_call wire.Event for one
// completed "tools/call" dispatch (spec §5.3). Called AFTER next() returns
// so telemetry never sits in the handler's response path (B3). The entire
// body is recovered so an extraction/emit panic can never affect the
// already-computed tool result (spec B3, T1).
func (s *Server) emitToolCallTelemetry(req mcp.Request, result mcp.Result, durationMs int64) {
	if s.telemetry == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			s.logger.Warn("telemetry: tool-call extraction panic recovered", "panic", r)
		}
	}()

	e := wire.Event{
		EventType:  wire.EventToolCall,
		DurationMs: durationMs,
	}

	if req != nil {
		if raw, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok && raw != nil {
			e.Tool = shapeOrUnknown(raw.Name)

			var ap actionPeek
			if json.Unmarshal(raw.Arguments, &ap) == nil {
				e.Action = shapeOrUnknown(ap.Action)
			}

			if raw.Name == toolZeropsWorkflow {
				var wp workflowPeek
				if json.Unmarshal(raw.Arguments, &wp) == nil {
					e.WorkflowStep = shapeOrUnknown(wp.Step)
					e.WorkflowRoute = shapeOrUnknown(wp.Route)
				}
				// This call is the source of truth for the process's current
				// workflow phase — remember it for every subsequent
				// non-zerops_workflow tool_call (spec §5.3 workflow-context
				// stamping).
				s.wfCtx.remember(e.WorkflowRoute, e.WorkflowStep)
			} else {
				// Stamp the last-seen workflow phase onto every OTHER tool
				// call so a deploy/import failure mid-workflow is
				// attributable to its phase (R4, S4) — pre-S4 these two
				// fields were always empty outside zerops_workflow itself.
				e.WorkflowRoute, e.WorkflowStep = s.wfCtx.snapshot()
			}
		}
	}

	ctr, ok := result.(*mcp.CallToolResult)
	switch {
	case ok && ctr != nil:
		e.Success = !ctr.IsError
	default:
		// No structured CallToolResult — either a nil result or a
		// protocol-level dispatch error. Both count as a failed call;
		// there is no ErrorWire payload to peek from.
		e.Success = false
	}
	if !e.Success {
		e.ErrorCode, e.ErrorSubcode = extractErrorCode(ctr)
	}

	s.telemetry.Emit(e)
}

// extractErrorCode decodes the "code"/"subcode" allowlisted keys from the
// ErrorWire JSON in Content[0].Text (spec §5.3/§4.2). code is REQUIRED:
// absent/undecodable/shape-invalid all collapse to the UNKNOWN sentinel —
// never a partial or malformed value. subcode is genuinely OPTIONAL (most
// errors don't set one) — absent or shape-invalid leaves it "" rather than
// substituting a sentinel, so a shape-invalid subcode can never itself
// cause wire.ValidateLite to reject the whole event.
func extractErrorCode(ctr *mcp.CallToolResult) (code, subcode string) {
	if ctr == nil || len(ctr.Content) == 0 {
		return wire.UnknownErrorCode, ""
	}
	text, ok := ctr.Content[0].(*mcp.TextContent)
	if !ok || text == nil {
		return wire.UnknownErrorCode, ""
	}
	var peek errorCodePeek
	if err := json.Unmarshal([]byte(text.Text), &peek); err != nil || peek.Code == "" {
		return wire.UnknownErrorCode, ""
	}
	if !wire.ValidErrorCodeShape(peek.Code) {
		return wire.UnknownErrorCode, ""
	}
	code = peek.Code
	if peek.Subcode != "" && wire.ValidSubcodeShape(peek.Subcode) {
		subcode = peek.Subcode
	}
	return code, subcode
}

// shapeOrUnknown returns v unchanged when empty (field genuinely absent —
// callers leave "" alone, e.g. optional workflow step/route on non-workflow
// tools) or shape-valid; otherwise the UnknownIdentifier sentinel (spec §5.3
// "Values failing shape checks → unknown"). This is the anti-leak guard for
// T2: a caller passing a secret/path-shaped string through the one
// allowlisted "action"/"step"/"route" key never reaches the wire verbatim.
func shapeOrUnknown(v string) string {
	if v == "" || wire.ValidIdentifierShape(v) {
		return v
	}
	return wire.UnknownIdentifier
}
