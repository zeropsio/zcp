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
