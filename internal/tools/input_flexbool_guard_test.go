package tools

import (
	"reflect"
	"strings"
	"testing"
)

// TestInputStructsUseFlexBoolForBooleans walks every top-level field of
// every *Input struct in the tools package and fails if any is a raw
// `bool`. LLM agents frequently send stringified primitives (`"true"`
// instead of `true`) and raw-bool fields reject those at the schema
// layer with a non-actionable error. The v7 post-mortem (commit 3475090)
// was the originating incident; FlexBool + explicit jsonschema are the
// fix pattern.
//
// Scope: direct fields only. Nested types from other packages
// (workflow.BootstrapTarget, workflow.RecipePlan, ops.DeployBatchTarget)
// are NOT walked — propagating FlexBool into those would require either
// moving FlexBool to a shared package or introducing a parallel
// MCP-boundary DTO layer, neither of which matches the established
// project convention (top-level *Input toggles use FlexBool; deep-domain
// types stay plain). If a nested bool turns out to be a real agent
// failure mode, that's a separate architectural decision.
//
// If this test fails, migrate the listed field: swap `bool` for
// `FlexBool`, route `.Bool()` at the handler call site, and add the
// field to the tool's explicit InputSchema via `flexBoolSchema(...)`.
func TestInputStructsUseFlexBoolForBooleans(t *testing.T) {
	t.Parallel()

	// Every *Input type registered with a tool handler. Additions must be
	// listed here so the guard keeps catching new fields.
	inputs := []any{
		WorkflowInput{},
		ManageInput{},
		MountInput{},
		VerifyInput{},
		KnowledgeInput{},
		GuidanceInput{},
		ScaleInput{},
		DeploySSHInput{},
		EventsInput{},
		DeployLocalInput{},
		DiscoverInput{},
		DeleteInput{},
		LogsInput{},
		ProcessInput{},
		DeployBatchInput{},
		SubdomainInput{},
		DevServerInput{},
		WorkspaceManifestInput{},
		RecordFactInput{},
		ImportInput{},
		PreprocessInput{},
		ExportInput{},
		EnvInput{},
	}

	plainBool := reflect.TypeFor[bool]()

	var violations []string
	for _, in := range inputs {
		rt := reflect.TypeOf(in)
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.Type == plainBool {
				violations = append(violations, rt.Name()+"."+f.Name)
			}
		}
	}

	if len(violations) > 0 {
		t.Fatalf("raw bool field(s) on tool-input struct(s) — migrate to FlexBool + flexBoolSchema:\n  %s",
			strings.Join(violations, "\n  "))
	}
}
