package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestWorkflowInputSchema_FlexBoolPublished pins B4: the zerops_workflow
// InputSchema must publish force/skipPipelineSetup as oneOf[boolean,string]
// (not the inferred type:boolean), so an agent sending force="true" is not
// rejected at the schema layer before FlexBool's UnmarshalJSON runs.
func TestWorkflowInputSchema_FlexBoolPublished(t *testing.T) {
	t.Parallel()
	s := workflowInputSchema()
	if s == nil {
		t.Fatal("workflowInputSchema returned nil — jsonschema.For[WorkflowInput] failed")
	}
	for _, key := range []string{"force", "skipPipelineSetup"} {
		prop, ok := s.Properties[key]
		if !ok || prop == nil {
			t.Fatalf("property %q missing from schema", key)
		}
		b, err := json.Marshal(prop)
		if err != nil {
			t.Fatalf("marshal %s: %v", key, err)
		}
		js := string(b)
		if !strings.Contains(js, "boolean") || !strings.Contains(js, "string") || !strings.Contains(js, "oneOf") {
			t.Errorf("property %q = %s, want oneOf[boolean,string]", key, js)
		}
	}
}

// TestWorkflowInputSchema_CloseModePresentsAutoManualOnly pins F1 at the
// HIGHEST-blast-radius TELL surface: the tool schema (read at discovery time,
// before any atom). The close-mode descriptions must present auto/manual and
// must NOT enumerate git-push as a selectable CloseDeployMode value — it is
// retired (folds to auto; delivery is a separate dimension via git-push-setup).
// validCloseModes still ACCEPTS git-push on the wire for compat — only the
// presentation here is curated (validation set ≠ presentation set).
func TestWorkflowInputSchema_CloseModePresentsAutoManualOnly(t *testing.T) {
	t.Parallel()
	s := workflowInputSchema()
	if s == nil {
		t.Fatal("workflowInputSchema returned nil")
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	js := string(b)
	for _, forbidden := range []string{"auto/git-push/manual", "CloseDeployMode auto/git-push"} {
		if strings.Contains(js, forbidden) {
			t.Errorf("schema presents git-push as a close-mode value (%q) — retired (folds to auto); present auto/manual", forbidden)
		}
	}
	if !strings.Contains(js, "auto/manual") {
		t.Error("schema should present close-mode as auto/manual")
	}
}
