// Tests for: workflow guards — requireWorkflowContext and requireAdoption.
package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

func TestRequireWorkflowContext_NilEngine_NoMarker_Blocks(t *testing.T) {
	t.Parallel()
	result := requireWorkflowContext(nil, t.TempDir())
	if result == nil {
		t.Fatal("expected non-nil result when no workflow context")
	}
	if !result.IsError {
		t.Error("expected IsError=true")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "WORKFLOW_REQUIRED") {
		t.Errorf("expected WORKFLOW_REQUIRED, got: %s", text)
	}
}

func TestRequireWorkflowContext_ActiveSession_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	if _, err := engine.Start("proj-1", "bootstrap", "test"); err != nil {
		t.Fatalf("start session: %v", err)
	}
	result := requireWorkflowContext(engine, dir)
	if result != nil {
		t.Errorf("active session should pass, got error")
	}
}

func TestRequireWorkflowContext_DevelopMarker_Passes(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteDevelopMarker(stateDir, "proj-1", "test"); err != nil {
		t.Fatalf("write develop marker: %v", err)
	}
	result := requireWorkflowContext(nil, stateDir)
	if result != nil {
		t.Errorf("develop marker should pass, got error")
	}
}

func TestRequireWorkflowContext_EmptyStateDir_Blocks(t *testing.T) {
	t.Parallel()
	result := requireWorkflowContext(nil, "")
	if result == nil {
		t.Fatal("expected non-nil result for empty stateDir with nil engine")
	}
}

func TestRequireAdoption_KnownService_Passes(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{Hostname: "app", BootstrappedAt: "2026-01-01"}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	result := requireAdoption(stateDir, "app")
	if result != nil {
		t.Errorf("known service should pass, got error")
	}
}

func TestRequireAdoption_UnknownService_Blocks(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{Hostname: "other", BootstrappedAt: "2026-01-01"}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	result := requireAdoption(stateDir, "app")
	if result == nil {
		t.Fatal("unknown service should be blocked")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "not adopted") {
		t.Errorf("expected 'not adopted', got: %s", text)
	}
}
