// Tests for: zerops_workflow action=develop-atom (P0c round 2 — the
// dereference behind pointer-rendered develop REFERENCE atoms).

package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/runtime"
)

// TestWorkflowTool_DevelopAtom_ReturnsBody verifies that action=develop-atom
// with a real develop-corpus atomId returns its full body. This is the
// dereference behind the "fetch: action=develop-atom atomId=..." pointers —
// it MUST resolve (a dead pointer is the masking-fallback failure mode), so
// the test pins a known reference atom + a phrase from its body.
func TestWorkflowTool_DevelopAtom_ReturnsBody(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "", nil, nil, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "develop-atom",
		"atomId": "develop-deploy-modes",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}
	var payload struct {
		AtomID string `json:"atomId"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &payload); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if payload.AtomID != "develop-deploy-modes" {
		t.Errorf("atomId: got %q, want develop-deploy-modes", payload.AtomID)
	}
	if !strings.Contains(payload.Body, "Two deploy classes") {
		t.Errorf("body must carry the deploy-modes content; got first 200B: %q",
			payload.Body[:min(len(payload.Body), 200)])
	}
}

// TestWorkflowTool_DevelopAtom_UnknownErrors rejects an unknown atomId with
// INVALID_PARAMETER rather than returning an empty body silently.
func TestWorkflowTool_DevelopAtom_UnknownErrors(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "", nil, nil, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "develop-atom",
		"atomId": "develop-does-not-exist",
	})
	if !result.IsError {
		t.Errorf("expected error for unknown atomId, got: %s", getTextContent(t, result))
	}
}
