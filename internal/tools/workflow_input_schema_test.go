package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestWorkflowInputSchema_FlexBoolPublished pins B4: the zerops_workflow
// InputSchema must publish force/skipPipelineSetup/confirmLaunch as
// oneOf[boolean,string] (not the inferred type:boolean), so an agent
// sending force="true" is not rejected at the schema layer before
// FlexBool's UnmarshalJSON runs. confirmLaunch joins this pin per the
// token-delegation spec §4.1 (plans/archive/token-delegation-implementation-spec-2026-07-10.md).
func TestWorkflowInputSchema_FlexBoolPublished(t *testing.T) {
	t.Parallel()
	s := workflowInputSchema()
	if s == nil {
		t.Fatal("workflowInputSchema returned nil — jsonschema.For[WorkflowInput] failed")
	}
	for _, key := range []string{"force", "skipPipelineSetup", "confirmLaunch"} {
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

// TestWorkflowInputSchema_PublicAccessListsAllowedValues pins that the plan's
// runtime.publicAccess is published with its allowed values, so the agent
// reads them instead of guessing.
func TestWorkflowInputSchema_PublicAccessListsAllowedValues(t *testing.T) {
	t.Parallel()
	s := workflowInputSchema()
	if s == nil {
		t.Fatal("workflowInputSchema returned nil")
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var doc any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	var found map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if props, ok := x["properties"].(map[string]any); ok {
				if pa, ok := props["publicAccess"].(map[string]any); ok {
					found = pa
				}
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(doc)
	if found == nil {
		t.Fatal("publicAccess property missing from schema")
	}
	enum, _ := found["enum"].([]any)
	if len(enum) != 3 || enum[0] != "auto" || enum[1] != "subdomain" || enum[2] != "none" {
		t.Errorf("publicAccess enum = %v, want [auto subdomain none]", enum)
	}
	if desc, _ := found["description"].(string); !strings.Contains(desc, "none") {
		t.Errorf("publicAccess description = %q, want it to explain none", desc)
	}
}

// TestWorkflowInputSchema_PoolMigrateNamed pins that the schema an agent reads
// at discovery names workflow="pool-migrate", the dev service it takes, and
// the pool hostname it may override.
func TestWorkflowInputSchema_PoolMigrateNamed(t *testing.T) {
	t.Parallel()
	s := workflowInputSchema()
	if s == nil {
		t.Fatal("workflowInputSchema returned nil")
	}
	for key, want := range map[string]string{
		"workflow": "pool-migrate",
		"service":  "pool-migrate",
		"pool":     "pool-migrate",
	} {
		prop, ok := s.Properties[key]
		if !ok || prop == nil {
			t.Fatalf("property %q missing from schema", key)
		}
		if !strings.Contains(prop.Description, want) {
			t.Errorf("property %q description %q does not name %q", key, prop.Description, want)
		}
	}
}
