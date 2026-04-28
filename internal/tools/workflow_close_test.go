// Tests for: handleWorkSessionClose — close always succeeds.
//
// Close is session cleanup, not commitment: any code edits live on the
// SSHFS mount, any deploys live on the platform. Close removes the per-PID
// session file; nothing of substance is lost. Auto-close (scope-all-green)
// is the "task done, verified" signal; manual close is "I'm done here, for
// whatever reason". Both delete the file.
//
// The previous close guard blocked close-without-deploy to catch "agent
// edited code then forgot to deploy" regressions. In practice it created
// friction on legitimate pivots (the agent worked around it via direct
// tools), and with the scope-explicit invariant restored (auto-close fires
// reliably on task completion) the guard's raison d'être evaporated.
package tools

import (
	"os"
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
// non-empty SucceededAt — informational since the guard is gone but still
// useful to confirm close doesn't care about deploy state either way.
func seedOpenWorkSession(t *testing.T, dir string, deploySucceeded bool) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	ws := workflow.NewWorkSession("proj1", string(workflow.EnvContainer), "test intent", []string{"appdev"})
	if deploySucceeded {
		ws.Deploys = map[string][]workflow.DeployAttempt{
			"appdev": {{AttemptedAt: now, SucceededAt: now, Setup: "dev", Strategy: "push-dev"}},
		}
	}
	if err := workflow.SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}
}

// seedAutoClosedSession writes a work session that already auto-closed
// (ClosedAt + CloseReasonAutoComplete). A follow-up manual close must still
// remove the file — close is idempotent.
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

// closeInput shortens the WorkflowInput constructor — only the workflow
// value is relevant now that the close guard is gone.
func closeInput() WorkflowInput {
	return WorkflowInput{
		Workflow: "develop",
		Action:   "close",
	}
}

func TestHandleWorkSessionClose_SuccessfulDeploy_Closes(t *testing.T) {
	t.Parallel()
	engine, dir := closeTestEngine(t)
	seedOpenWorkSession(t, dir, true /*deploySucceeded*/)

	result, _, err := handleWorkSessionClose(engine, closeInput())
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

// Close without any deploy history must succeed — the old guard blocked
// this with CLOSE_BLOCKED; the new contract treats close as session
// cleanup with no deploy precondition.
func TestHandleWorkSessionClose_NoDeploy_Closes(t *testing.T) {
	t.Parallel()
	engine, dir := closeTestEngine(t)
	seedOpenWorkSession(t, dir, false /*deploySucceeded*/)

	result, _, err := handleWorkSessionClose(engine, closeInput())
	if err != nil {
		t.Fatalf("handleWorkSessionClose: %v", err)
	}
	if result.IsError {
		t.Fatalf("close must succeed without deploy history, got:\n%s", extractText(result))
	}
	if ws, _ := workflow.CurrentWorkSession(dir); ws != nil {
		t.Error("work session should be deleted after close")
	}
}

// Auto-closed sessions (ClosedAt set) still get removed by an explicit
// follow-up close. The handler is idempotent.
func TestHandleWorkSessionClose_AutoClosedSession_Closes(t *testing.T) {
	t.Parallel()
	engine, dir := closeTestEngine(t)
	seedAutoClosedSession(t, dir)

	result, _, err := handleWorkSessionClose(engine, closeInput())
	if err != nil {
		t.Fatalf("handleWorkSessionClose: %v", err)
	}
	if result.IsError {
		t.Fatalf("auto-closed session close must succeed, got:\n%s", extractText(result))
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
	result, _, err := handleWorkSessionClose(engine, closeInput())
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

// End-to-end through RegisterWorkflow: confirms close surfaces at the MCP
// tool boundary and always succeeds.
func TestWorkflowTool_CloseAlwaysSucceeds(t *testing.T) {
	t.Parallel()
	engine, dir := closeTestEngine(t)
	seedOpenWorkSession(t, dir, false /*deploySucceeded*/)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, dir, "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "close",
		"workflow": "develop",
	})
	if result.IsError {
		t.Fatalf("close must succeed, got:\n%s", getTextContent(t, result))
	}
	if ws, _ := workflow.CurrentWorkSession(dir); ws != nil {
		t.Error("work session not deleted after close")
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
}
