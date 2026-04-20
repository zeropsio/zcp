// Tests for: handleWorkSessionClose — close-without-deploy guard + force escape.
//
// Work-session close has two policy modes:
//   - Default (force=false): refuses when no successful deploy is recorded.
//     Catches the "agent edited code then forgot to deploy" regression —
//     the status block in that shape tells the agent the task is done but
//     no artifact actually reached the platform.
//   - Force (force=true): bypasses the guard. Intended for investigations,
//     env-only changes, and abandoned sessions that legitimately have no
//     deploy artifact.
//
// Auto-close is orthogonal: once ws.ClosedAt is set with
// CloseReasonAutoComplete the full-green heuristic already ran, so the
// guard must NOT block a subsequent explicit close (idempotency contract).
package tools

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// closeTestEngine builds an engine rooted at a fresh temp dir. Returns the
// engine and the dir so callers can seed work sessions directly.
func closeTestEngine(t *testing.T) (*workflow.Engine, string) {
	t.Helper()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	t.Cleanup(func() {
		// Always clean the PID's work session — tests can leave one on disk
		// if they seeded but the test helper didn't delete via close.
		_ = workflow.DeleteWorkSession(dir, os.Getpid())
		_ = workflow.UnregisterSession(dir, workflow.WorkSessionID(os.Getpid()))
	})
	return engine, dir
}

// seedOpenWorkSession writes a work session with the given deploy history.
// `deploySucceeded` controls whether the single deploy attempt carries a
// non-empty SucceededAt — the signal HasSuccessfulDeploy keys off.
func seedOpenWorkSession(t *testing.T, dir string, deploySucceeded bool) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	ws := workflow.NewWorkSession("proj1", string(workflow.EnvContainer), "test intent", []string{"appdev"})
	if deploySucceeded {
		ws.Deploys = map[string][]workflow.DeployAttempt{
			"appdev": {{AttemptedAt: now, SucceededAt: now, Setup: "dev", Strategy: workflow.StrategyPushDev}},
		}
	}
	if err := workflow.SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}
}

// seedAutoClosedSession writes a work session that already auto-closed
// (ClosedAt + CloseReasonAutoComplete). The guard must NOT fire — an
// auto-closed session already cleared the full-green heuristic.
func seedAutoClosedSession(t *testing.T, dir string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	ws := workflow.NewWorkSession("proj1", string(workflow.EnvContainer), "done", []string{"appdev"})
	ws.Deploys = map[string][]workflow.DeployAttempt{
		"appdev": {{AttemptedAt: now, SucceededAt: now}},
	}
	ws.Verifies = map[string][]workflow.VerifyAttempt{
		"appdev": {{AttemptedAt: now, PassedAt: now, Passed: true}},
	}
	ws.ClosedAt = now
	ws.CloseReason = workflow.CloseReasonAutoComplete
	if err := workflow.SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}
}

// closeInput shortens the WorkflowInput constructor — only the two fields
// the close handler reads are relevant.
func closeInput(force bool) WorkflowInput {
	return WorkflowInput{
		Workflow: "develop",
		Action:   "close",
		Force:    FlexBool(force),
	}
}

func TestHandleWorkSessionClose_SuccessfulDeploy_Closes(t *testing.T) {
	t.Parallel()
	engine, dir := closeTestEngine(t)
	seedOpenWorkSession(t, dir, true /*deploySucceeded*/)

	result, _, err := handleWorkSessionClose(engine, closeInput(false))
	if err != nil {
		t.Fatalf("handleWorkSessionClose: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", extractText(result))
	}
	if ws, _ := workflow.CurrentWorkSession(dir); ws != nil {
		t.Error("work session should be deleted after successful close")
	}
}

func TestHandleWorkSessionClose_NoDeploy_Blocks(t *testing.T) {
	t.Parallel()
	engine, dir := closeTestEngine(t)
	seedOpenWorkSession(t, dir, false /*deploySucceeded*/)

	result, _, err := handleWorkSessionClose(engine, closeInput(false))
	if err != nil {
		t.Fatalf("handleWorkSessionClose: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected ErrCloseBlocked when no deploy recorded, got:\n%s", extractText(result))
	}
	text := extractText(result)
	for _, needle := range []string{"CLOSE_BLOCKED", "no successful deploy", "force=true"} {
		if !strings.Contains(text, needle) {
			t.Errorf("response missing %q. Got:\n%s", needle, text)
		}
	}
	// Guard must not delete the file.
	if ws, _ := workflow.CurrentWorkSession(dir); ws == nil {
		t.Error("work session was deleted despite guard; guard must keep state for retry")
	}
}

func TestHandleWorkSessionClose_NoDeploy_ForceTrue_Closes(t *testing.T) {
	t.Parallel()
	engine, dir := closeTestEngine(t)
	seedOpenWorkSession(t, dir, false /*deploySucceeded*/)

	result, _, err := handleWorkSessionClose(engine, closeInput(true))
	if err != nil {
		t.Fatalf("handleWorkSessionClose: %v", err)
	}
	if result.IsError {
		t.Fatalf("force=true must bypass guard, got:\n%s", extractText(result))
	}
	if ws, _ := workflow.CurrentWorkSession(dir); ws != nil {
		t.Error("work session should be deleted after force=true close")
	}
}

// Auto-closed sessions have already cleared the full-green heuristic, so a
// follow-up explicit close (without force) must proceed — the guard only
// applies to OPEN sessions (ClosedAt empty).
func TestHandleWorkSessionClose_AutoClosedSession_Closes(t *testing.T) {
	t.Parallel()
	engine, dir := closeTestEngine(t)
	seedAutoClosedSession(t, dir)

	result, _, err := handleWorkSessionClose(engine, closeInput(false))
	if err != nil {
		t.Fatalf("handleWorkSessionClose: %v", err)
	}
	if result.IsError {
		t.Fatalf("auto-closed session close must succeed without force, got:\n%s", extractText(result))
	}
	if ws, _ := workflow.CurrentWorkSession(dir); ws != nil {
		t.Error("auto-closed session should be removed after explicit close")
	}
}

// No session on disk: the call is idempotent and succeeds. An LLM calling
// close twice after context compaction should not get an error — the
// second call is a legal no-op.
func TestHandleWorkSessionClose_NoSession_NoOp(t *testing.T) {
	t.Parallel()
	engine, _ := closeTestEngine(t)
	result, _, err := handleWorkSessionClose(engine, closeInput(false))
	if err != nil {
		t.Fatalf("handleWorkSessionClose: %v", err)
	}
	if result.IsError {
		t.Fatalf("close without session must not error, got:\n%s", extractText(result))
	}
}

// handleWorkSessionClose must reject a non-develop workflow value.
func TestHandleWorkSessionClose_WrongWorkflow_Rejected(t *testing.T) {
	t.Parallel()
	engine, _ := closeTestEngine(t)
	result, _, err := handleWorkSessionClose(engine, WorkflowInput{
		Workflow: "bootstrap",
		Action:   "close",
	})
	if err != nil {
		t.Fatalf("handleWorkSessionClose: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for workflow=bootstrap close")
	}
}

// End-to-end through RegisterWorkflow: confirms the guard fires at the MCP
// tool boundary (Force field surfaces through the JSON schema).
func TestWorkflowTool_CloseGuardFromJSON(t *testing.T) {
	t.Parallel()
	engine, dir := closeTestEngine(t)
	seedOpenWorkSession(t, dir, false /*deploySucceeded*/)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, dir, "", nil, runtime.Info{})

	// Without force → blocked.
	blocked := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "close",
		"workflow": "develop",
	})
	if !blocked.IsError {
		t.Fatalf("close without deploy must be blocked, got:\n%s", getTextContent(t, blocked))
	}

	// With force → succeeds.
	forced := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "close",
		"workflow": "develop",
		"force":    true,
	})
	if forced.IsError {
		t.Fatalf("force=true must close, got:\n%s", getTextContent(t, forced))
	}
	if ws, _ := workflow.CurrentWorkSession(dir); ws != nil {
		t.Error("work session not deleted after force=true close")
	}
}

// Guard against context mutation: close shouldn't care about input fields
// unrelated to close (intent, plan, etc.).
func TestHandleWorkSessionClose_IgnoresUnrelatedInputFields(t *testing.T) {
	t.Parallel()
	engine, dir := closeTestEngine(t)
	seedOpenWorkSession(t, dir, true)

	in := WorkflowInput{
		Workflow:    "develop",
		Action:      "close",
		Intent:      "whatever",
		Attestation: "whatever",
		Step:        "discover",
	}
	result, _, err := handleWorkSessionClose(engine, in)
	if err != nil {
		t.Fatalf("handleWorkSessionClose: %v", err)
	}
	if result.IsError {
		t.Fatalf("close should ignore unrelated fields, got:\n%s", extractText(result))
	}
	_ = context.Background()
}
