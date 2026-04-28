// Tests for: zerops_workflow action=dispatch-brief-atom (Cx-BRIEF-OVERFLOW).

package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/runtime"
)

// TestWorkflowTool_DispatchBriefAtom_ReturnsBody verifies that
// action=dispatch-brief-atom with a valid atomId returns JSON carrying
// the atom's body. This is the retrieval side of the Cx-BRIEF-OVERFLOW
// envelope pattern — the main agent calls this once per atom listed in
// the substep-guide envelope, concatenates the bodies in order, and
// dispatches the full brief to the sub-agent.
func TestWorkflowTool_DispatchBriefAtom_ReturnsBody(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "", nil, nil, nil, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "dispatch-brief-atom",
		"atomId": "briefs.writer.manifest-contract",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)

	var payload struct {
		AtomID string `json:"atomId"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("response is not JSON: %v — raw: %s", err, text)
	}
	if payload.AtomID != "briefs.writer.manifest-contract" {
		t.Errorf("atomId: got %q, want %q", payload.AtomID, "briefs.writer.manifest-contract")
	}
	if payload.Body == "" {
		t.Error("body is empty — atom loader returned no content")
	}
	if !strings.Contains(payload.Body, "fact_title") {
		t.Errorf("manifest-contract body must name the fact_title JSON key somewhere; got first 200B: %q", payload.Body[:min(len(payload.Body), 200)])
	}
}

// TestWorkflowTool_DispatchBriefAtom_MissingID rejects calls without
// an atomId with INVALID_PARAMETER.
func TestWorkflowTool_DispatchBriefAtom_MissingID(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "", nil, nil, nil, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "dispatch-brief-atom",
	})

	if !result.IsError {
		t.Error("expected IsError when atomId is omitted")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "INVALID_PARAMETER") {
		t.Errorf("expected INVALID_PARAMETER in error; got: %s", text)
	}
	if !strings.Contains(text, "atomId is required") {
		t.Errorf("expected 'atomId is required' in error; got: %s", text)
	}
}

// TestWorkflowTool_DispatchBriefAtom_UnknownID rejects unknown atom
// IDs with INVALID_PARAMETER naming the offending ID.
func TestWorkflowTool_DispatchBriefAtom_UnknownID(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "", nil, nil, nil, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "dispatch-brief-atom",
		"atomId": "briefs.writer.does-not-exist",
	})

	if !result.IsError {
		t.Error("expected IsError for unknown atomId")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "briefs.writer.does-not-exist") {
		t.Errorf("expected error to name the unknown atomId; got: %s", text)
	}
}
