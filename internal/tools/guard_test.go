// Tests for: workflow guard — requireWorkflow blocks tools without active session.
package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

func TestRequireWorkflow_NilEngine_Passes(t *testing.T) {
	t.Parallel()
	result := requireWorkflow(nil)
	if result != nil {
		t.Errorf("nil engine should pass (backward compat), got: %v", result)
	}
}

func TestRequireWorkflow_NoSession_Blocks(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir())
	result := requireWorkflow(engine)
	if result == nil {
		t.Fatal("expected non-nil result when no session exists")
	}
	if !result.IsError {
		t.Error("expected IsError=true")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "WORKFLOW_REQUIRED") {
		t.Errorf("expected WORKFLOW_REQUIRED in error, got: %s", text)
	}
}

func TestRequireWorkflow_ActiveSession_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir)
	if _, err := engine.Start("proj-1", "deploy", "test"); err != nil {
		t.Fatalf("start session: %v", err)
	}
	result := requireWorkflow(engine)
	if result != nil {
		t.Errorf("active session should pass, got: %v", result)
	}
}
