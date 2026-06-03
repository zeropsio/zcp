// Tests for: zerops_workflow action=develop-atom (P0c round 2 — the
// dereference behind pointer-rendered develop REFERENCE atoms).

package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
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

// TestWorkflowTool_DevelopAtom_AllReferenceAtomsResolve is the authoritative
// dead-pointer guarantee at the real handler boundary: EVERY atom that
// Synthesize pointer-renders (reference: true) must resolve through the live
// develop-atom handler against the embedded corpus. A reference atom whose id
// the handler can't fetch would render a stub the agent cannot dereference —
// the masking-fallback failure mode the single-owner design forbids.
func TestWorkflowTool_DevelopAtom_AllReferenceAtomsResolve(t *testing.T) {
	t.Parallel()
	corpus, err := workflow.LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "", nil, nil, nil, "", "", nil, nil, runtime.Info{})

	var refCount int
	for _, atom := range corpus {
		if !atom.Reference {
			continue
		}
		refCount++
		result := callTool(t, srv, "zerops_workflow", map[string]any{
			"action": "develop-atom",
			"atomId": atom.ID,
		})
		if result.IsError {
			t.Errorf("reference atom %q: develop-atom fetch errored (dead pointer): %s", atom.ID, getTextContent(t, result))
			continue
		}
		var payload struct {
			AtomID string `json:"atomId"`
			Body   string `json:"body"`
		}
		if err := json.Unmarshal([]byte(getTextContent(t, result)), &payload); err != nil {
			t.Errorf("reference atom %q: response not JSON: %v", atom.ID, err)
			continue
		}
		if payload.AtomID != atom.ID {
			t.Errorf("reference atom %q: fetch returned atomId %q", atom.ID, payload.AtomID)
		}
		if strings.TrimSpace(payload.Body) == "" {
			t.Errorf("reference atom %q: fetch returned an empty body", atom.ID)
		}
	}
	if refCount == 0 {
		t.Fatal("no reference atoms in corpus — pointer-render fetch path untested")
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
