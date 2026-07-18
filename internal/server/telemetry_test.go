// Tests for: telemetry.go — the observe() middleware's tool_call telemetry
// extraction (spec §5.3): tool name from CallToolParamsRaw.Name, action
// (+ step/route for zerops_workflow) via single-key peeks of Arguments,
// outcome from CallToolResult.IsError, error_code from the ErrorWire JSON.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

// recordingEmitter is an in-memory telemetry.Emitter for assertions.
type recordingEmitter struct {
	events []wire.Event
}

func (r *recordingEmitter) Emit(e wire.Event) { r.events = append(r.events, e) }

// panicEmitter panics on every Emit call, used to pin that a panicking
// Emitter never affects the tool result (spec B3).
type panicEmitter struct{}

func (panicEmitter) Emit(wire.Event) { panic("boom") }

func newTestServer(t *testing.T, emitter *recordingEmitter) *Server {
	t.Helper()
	s := &Server{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if emitter != nil {
		s.telemetry = emitter
	}
	return s
}

// callToolReq builds a Request whose GetParams() returns a
// *mcp.CallToolParamsRaw with the given tool name and JSON-encoded
// arguments — the exact shape observe() sees for method="tools/call".
func callToolReq(t *testing.T, toolName string, args any) mcp.Request {
	t.Helper()
	var raw json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("marshal args: %v", err)
		}
		raw = b
	}
	return &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
		Params: &mcp.CallToolParamsRaw{Name: toolName, Arguments: raw},
	}
}

func successResult() *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}
}

func errorResult(t *testing.T, code string) *mcp.CallToolResult {
	t.Helper()
	body, err := json.Marshal(struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}{Code: code, Error: "boom"})
	if err != nil {
		t.Fatalf("marshal error body: %v", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		IsError: true,
	}
}

func runObserve(s *Server, req mcp.Request, result mcp.Result, handlerErr error) (mcp.Result, error) {
	mw := s.observe()
	handler := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return result, handlerErr
	})
	return handler(context.Background(), methodCallTool, req)
}

func TestObserve_Telemetry_SuccessEmitsCorrectEvent(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	req := callToolReq(t, "zerops_deploy", map[string]string{"action": "deploy"})
	if _, err := runObserve(s, req, successResult(), nil); err != nil {
		t.Fatalf("observe: %v", err)
	}

	if len(rec.events) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(rec.events))
	}
	e := rec.events[0]
	if e.EventType != wire.EventToolCall {
		t.Errorf("EventType = %q, want %q", e.EventType, wire.EventToolCall)
	}
	if e.Tool != "zerops_deploy" {
		t.Errorf("Tool = %q, want zerops_deploy", e.Tool)
	}
	if e.Action != "deploy" {
		t.Errorf("Action = %q, want deploy", e.Action)
	}
	if !e.Success {
		t.Error("Success = false, want true")
	}
	if e.ErrorCode != "" {
		t.Errorf("ErrorCode = %q, want empty on success", e.ErrorCode)
	}
}

func TestObserve_Telemetry_WorkflowToolPeeksStepAndRoute(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	req := callToolReq(t, "zerops_workflow", map[string]string{
		"action": "complete",
		"step":   "discover",
		"route":  "adopt",
	})
	if _, err := runObserve(s, req, successResult(), nil); err != nil {
		t.Fatalf("observe: %v", err)
	}

	if len(rec.events) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(rec.events))
	}
	e := rec.events[0]
	if e.WorkflowStep != "discover" {
		t.Errorf("WorkflowStep = %q, want discover", e.WorkflowStep)
	}
	if e.WorkflowRoute != "adopt" {
		t.Errorf("WorkflowRoute = %q, want adopt", e.WorkflowRoute)
	}
}

func TestObserve_Telemetry_NonWorkflowToolNeverCarriesStepOrRoute(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	// A non-zerops_workflow tool that happens to also have step/route-shaped
	// arguments must never have them peeked — only zerops_workflow does.
	req := callToolReq(t, "zerops_deploy", map[string]string{
		"action": "deploy",
		"step":   "discover",
		"route":  "adopt",
	})
	if _, err := runObserve(s, req, successResult(), nil); err != nil {
		t.Fatalf("observe: %v", err)
	}

	e := rec.events[0]
	if e.WorkflowStep != "" || e.WorkflowRoute != "" {
		t.Errorf("non-workflow tool leaked step/route: step=%q route=%q", e.WorkflowStep, e.WorkflowRoute)
	}
}

func TestObserve_Telemetry_ErrorCarriesCodeFromErrorWire(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	req := callToolReq(t, "zerops_import", map[string]string{"action": "start"})
	if _, err := runObserve(s, req, errorResult(t, "DIAGNOSIS_REQUIRED"), nil); err != nil {
		t.Fatalf("observe: %v", err)
	}

	e := rec.events[0]
	if e.Success {
		t.Error("Success = true, want false for IsError result")
	}
	if e.ErrorCode != "DIAGNOSIS_REQUIRED" {
		t.Errorf("ErrorCode = %q, want DIAGNOSIS_REQUIRED", e.ErrorCode)
	}
}

// errorResultWithSubcode builds an ErrorWire-shaped result carrying both
// code and subcode (spec §4.2 error_subcode).
func errorResultWithSubcode(t *testing.T, code, subcode string) *mcp.CallToolResult {
	t.Helper()
	body, err := json.Marshal(struct {
		Code    string `json:"code"`
		Subcode string `json:"subcode,omitempty"`
		Error   string `json:"error"`
	}{Code: code, Subcode: subcode, Error: "boom"})
	if err != nil {
		t.Fatalf("marshal error body: %v", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		IsError: true,
	}
}

// TestObserve_Telemetry_ErrorCarriesSubcodeFromErrorWire pins spec §4.2/§5.3:
// the middleware peeks "subcode" from the ErrorWire body exactly like "code".
func TestObserve_Telemetry_ErrorCarriesSubcodeFromErrorWire(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	req := callToolReq(t, "zerops_workflow", map[string]string{"action": "complete"})
	if _, err := runObserve(s, req, errorResultWithSubcode(t, "INVALID_PARAMETER", "AMBIGUOUS_SCOPE"), nil); err != nil {
		t.Fatalf("observe: %v", err)
	}

	e := rec.events[0]
	if e.ErrorCode != "INVALID_PARAMETER" {
		t.Errorf("ErrorCode = %q, want INVALID_PARAMETER", e.ErrorCode)
	}
	if e.ErrorSubcode != "AMBIGUOUS_SCOPE" {
		t.Errorf("ErrorSubcode = %q, want AMBIGUOUS_SCOPE", e.ErrorSubcode)
	}
}

// TestObserve_Telemetry_ErrorNoSubcode_LeavesFieldEmpty pins that an
// ErrorWire with no "subcode" key (the common case — most call sites haven't
// been split) leaves ErrorSubcode "" rather than substituting a sentinel
// (subcode is genuinely optional, unlike error_code).
func TestObserve_Telemetry_ErrorNoSubcode_LeavesFieldEmpty(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	req := callToolReq(t, "zerops_import", map[string]string{"action": "start"})
	if _, err := runObserve(s, req, errorResult(t, "DIAGNOSIS_REQUIRED"), nil); err != nil {
		t.Fatalf("observe: %v", err)
	}

	if e := rec.events[0]; e.ErrorSubcode != "" {
		t.Errorf("ErrorSubcode = %q, want empty when the ErrorWire carries no subcode", e.ErrorSubcode)
	}
}

// TestObserve_Telemetry_ErrorSubcode_ShapeInvalidLeavesFieldEmpty pins that a
// shape-invalid subcode (e.g. leaked free text) never reaches the wire
// verbatim and never itself causes wire.ValidateLite to reject the whole
// event — it is dropped to "" like every other shape-checked extraction.
func TestObserve_Telemetry_ErrorSubcode_ShapeInvalidLeavesFieldEmpty(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	req := callToolReq(t, "zerops_deploy", map[string]string{"action": "deploy"})
	if _, err := runObserve(s, req, errorResultWithSubcode(t, "INVALID_PARAMETER", "not a valid subcode!"), nil); err != nil {
		t.Fatalf("observe: %v", err)
	}

	if e := rec.events[0]; e.ErrorSubcode != "" {
		t.Errorf("ErrorSubcode = %q, want empty for a shape-invalid value", e.ErrorSubcode)
	}
}

// TestObserve_Telemetry_WorkflowContextStampedOntoSubsequentToolCall pins the
// S4 workflow-context stamping contract: a zerops_workflow call's route/step
// is remembered and stamped onto every LATER non-zerops_workflow tool_call
// this process, so a deploy/import failure mid-workflow is attributable to
// its phase (R4).
func TestObserve_Telemetry_WorkflowContextStampedOntoSubsequentToolCall(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	wfReq := callToolReq(t, "zerops_workflow", map[string]string{
		"action": "complete",
		"step":   "discover",
		"route":  "adopt",
	})
	if _, err := runObserve(s, wfReq, successResult(), nil); err != nil {
		t.Fatalf("observe (workflow call): %v", err)
	}

	deployReq := callToolReq(t, "zerops_deploy", map[string]string{"action": "deploy"})
	if _, err := runObserve(s, deployReq, errorResult(t, "DEPLOY_FAILED"), nil); err != nil {
		t.Fatalf("observe (deploy call): %v", err)
	}

	if len(rec.events) != 2 {
		t.Fatalf("expected 2 emitted events, got %d", len(rec.events))
	}
	deployEvent := rec.events[1]
	if deployEvent.WorkflowRoute != "adopt" {
		t.Errorf("WorkflowRoute = %q, want adopt (stamped from the earlier zerops_workflow call)", deployEvent.WorkflowRoute)
	}
	if deployEvent.WorkflowStep != "discover" {
		t.Errorf("WorkflowStep = %q, want discover (stamped from the earlier zerops_workflow call)", deployEvent.WorkflowStep)
	}
}

// TestObserve_Telemetry_WorkflowContextPersistsWhenLaterCallOmitsRoute pins
// that a later zerops_workflow call which only supplies step (no route —
// e.g. action=status) does not erase a previously-remembered route: the two
// fields update independently, last-non-empty-wins per field.
func TestObserve_Telemetry_WorkflowContextPersistsWhenLaterCallOmitsRoute(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	first := callToolReq(t, "zerops_workflow", map[string]string{
		"action": "complete",
		"step":   "discover",
		"route":  "recipe",
	})
	if _, err := runObserve(s, first, successResult(), nil); err != nil {
		t.Fatalf("observe (first workflow call): %v", err)
	}

	second := callToolReq(t, "zerops_workflow", map[string]string{
		"action": "complete",
		"step":   "provision",
		// no route on this call.
	})
	if _, err := runObserve(s, second, successResult(), nil); err != nil {
		t.Fatalf("observe (second workflow call): %v", err)
	}

	other := callToolReq(t, "zerops_deploy", map[string]string{"action": "deploy"})
	if _, err := runObserve(s, other, successResult(), nil); err != nil {
		t.Fatalf("observe (deploy call): %v", err)
	}

	deployEvent := rec.events[2]
	if deployEvent.WorkflowRoute != "recipe" {
		t.Errorf("WorkflowRoute = %q, want recipe (must survive a later call that omitted route)", deployEvent.WorkflowRoute)
	}
	if deployEvent.WorkflowStep != "provision" {
		t.Errorf("WorkflowStep = %q, want provision (updated by the second call)", deployEvent.WorkflowStep)
	}
}

// TestObserve_Telemetry_WorkflowContextEmptyBeforeAnyWorkflowCall pins the
// pre-S4 behavior for a process that has never seen a zerops_workflow call:
// workflow_route/workflow_step stay empty, not some stale/zero sentinel.
func TestObserve_Telemetry_WorkflowContextEmptyBeforeAnyWorkflowCall(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	req := callToolReq(t, "zerops_deploy", map[string]string{"action": "deploy"})
	if _, err := runObserve(s, req, successResult(), nil); err != nil {
		t.Fatalf("observe: %v", err)
	}

	e := rec.events[0]
	if e.WorkflowRoute != "" || e.WorkflowStep != "" {
		t.Errorf("expected empty workflow route/step before any zerops_workflow call, got route=%q step=%q", e.WorkflowRoute, e.WorkflowStep)
	}
}

func TestObserve_Telemetry_ErrorAbsentCodeBecomesUNKNOWN(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	req := callToolReq(t, "zerops_import", map[string]string{"action": "start"})
	// Error result whose content is not ErrorWire-shaped JSON at all.
	result := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "plain text, not JSON"}},
		IsError: true,
	}
	if _, err := runObserve(s, req, result, nil); err != nil {
		t.Fatalf("observe: %v", err)
	}

	e := rec.events[0]
	if e.ErrorCode != wire.UnknownErrorCode {
		t.Errorf("ErrorCode = %q, want %q", e.ErrorCode, wire.UnknownErrorCode)
	}
}

func TestObserve_Telemetry_SecretAndPathArgsNeverLeak(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	secret := "sk-live-ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	path := "/Users/karel/.ssh/id_rsa"
	req := callToolReq(t, "zerops_deploy", map[string]string{
		"action":      "deploy",
		"gitToken":    secret,
		"sourcePath":  path,
		"description": "contains " + secret + " and " + path,
	})
	if _, err := runObserve(s, req, successResult(), nil); err != nil {
		t.Fatalf("observe: %v", err)
	}

	e := rec.events[0]
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	blob := string(b)
	if strings.Contains(blob, secret) {
		t.Errorf("emitted event leaked secret: %s", blob)
	}
	if strings.Contains(blob, path) {
		t.Errorf("emitted event leaked path: %s", blob)
	}
	// action is the only allowlisted key — its value must be exactly what
	// was sent, nothing else decoded from Arguments.
	if e.Action != "deploy" {
		t.Errorf("Action = %q, want deploy", e.Action)
	}
}

// TestObserve_Telemetry_ShapeInvalidActionBecomesUnknown pins that a
// malformed "action" value (secret/path-shaped, not a valid identifier) is
// substituted with the unknown sentinel rather than passed through verbatim
// or dropping the whole event.
func TestObserve_Telemetry_ShapeInvalidActionBecomesUnknown(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	req := callToolReq(t, "zerops_deploy", map[string]string{"action": "/etc/passwd"})
	if _, err := runObserve(s, req, successResult(), nil); err != nil {
		t.Fatalf("observe: %v", err)
	}

	e := rec.events[0]
	if e.Action != wire.UnknownIdentifier {
		t.Errorf("Action = %q, want %q", e.Action, wire.UnknownIdentifier)
	}
}

func TestObserve_Telemetry_NilEmitterIsNoOp(t *testing.T) {
	t.Parallel()

	s := newTestServer(t, nil)
	req := callToolReq(t, "zerops_deploy", map[string]string{"action": "deploy"})
	result, err := runObserve(s, req, successResult(), nil)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestObserve_Telemetry_PanicInEmitterDoesNotAffectResult(t *testing.T) {
	t.Parallel()

	s := newTestServer(t, nil)
	s.telemetry = panicEmitter{}

	req := callToolReq(t, "zerops_deploy", map[string]string{"action": "deploy"})
	want := successResult()
	got, err := runObserve(s, req, want, nil)
	if err != nil {
		t.Fatalf("observe returned error despite emitter panic: %v", err)
	}
	if got != want {
		t.Errorf("result mutated by panicking emitter: got %+v, want %+v", got, want)
	}
}

func TestObserve_Telemetry_NonToolCallResultTreatedAsFailure(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	req := callToolReq(t, "zerops_deploy", map[string]string{"action": "deploy"})
	handlerErr := errors.New("transport error")
	if _, err := runObserve(s, req, nil, handlerErr); !errors.Is(err, handlerErr) {
		t.Fatalf("observe should pass through handler error, got %v", err)
	}

	if len(rec.events) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(rec.events))
	}
	if rec.events[0].Success {
		t.Error("Success = true, want false when handler returned a transport-level error")
	}
	if rec.events[0].ErrorCode != wire.UnknownErrorCode {
		t.Errorf("ErrorCode = %q, want %q", rec.events[0].ErrorCode, wire.UnknownErrorCode)
	}
}

func TestObserve_Telemetry_NilReqDoesNotPanic(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	// method != tools/call must still short-circuit before touching req.
	mw := s.observe()
	handler := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})
	if _, err := handler(context.Background(), "tools/list", nil); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if len(rec.events) != 0 {
		t.Errorf("expected no telemetry for non-tool-call method, got %d events", len(rec.events))
	}

	// tools/call with a literal nil req must not panic and must still emit
	// (with an empty/unknown tool identity) rather than crash the handler.
	if _, err := handler(context.Background(), methodCallTool, nil); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if len(rec.events) != 1 {
		t.Fatalf("expected 1 event for nil-req tool call, got %d", len(rec.events))
	}
}

func TestObserve_Telemetry_DurationStamped(t *testing.T) {
	t.Parallel()

	rec := &recordingEmitter{}
	s := newTestServer(t, rec)

	mw := s.observe()
	handler := mw(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		time.Sleep(5 * time.Millisecond)
		return successResult(), nil
	})
	req := callToolReq(t, "zerops_deploy", map[string]string{"action": "deploy"})
	if _, err := handler(context.Background(), methodCallTool, req); err != nil {
		t.Fatalf("observe: %v", err)
	}

	if rec.events[0].DurationMs <= 0 {
		t.Errorf("DurationMs = %d, want > 0", rec.events[0].DurationMs)
	}
}
