// Tests for: zerops_knowledge uri="zerops://atoms/<id>" — the unified pull
// retrieval for pointer-rendered reference atoms (spec-knowledge-architecture
// §4). Replaces the deleted bespoke develop-atom workflow action.

package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/workflow"
)

// atomURI is the canonical pull URI for a reference atom — the form the
// pointer stub (synthesize.go::referenceStub) emits.
func atomURI(id string) string { return "zerops://atoms/" + id }

// TestKnowledgeTool_AtomURI_ResolvesReference verifies a known reference atom
// fetches its full body via the unified pull URI. This is the dereference
// behind the "pull on demand: uri=..." stubs — it MUST resolve (a dead
// pointer is the masking-fallback failure mode), so the test pins a known
// reference atom + a phrase from its body.
func TestKnowledgeTool_AtomURI_ResolvesReference(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, testKnowledgeStore(t), nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"uri": atomURI("develop-deploy-modes"),
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, "Two deploy classes") {
		t.Errorf("body must carry the deploy-modes content; got first 200B: %q",
			body[:min(len(body), 200)])
	}
}

// TestKnowledgeTool_AtomURI_AllReferenceAtomsResolve is the authoritative
// dead-pointer guarantee at the real handler boundary: EVERY atom Synthesize
// pointer-renders (reference: true) must resolve through the live
// zerops_knowledge uri= adapter against the embedded corpus. A reference atom
// whose URI the adapter can't fetch would render a stub the agent cannot
// dereference — the masking-fallback failure mode the single-owner design
// forbids.
func TestKnowledgeTool_AtomURI_AllReferenceAtomsResolve(t *testing.T) {
	t.Parallel()
	corpus, err := workflow.LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, testKnowledgeStore(t), nil, nil, nil, nil)

	var refCount int
	for _, atom := range corpus {
		if !atom.Reference {
			continue
		}
		refCount++
		result := callTool(t, srv, "zerops_knowledge", map[string]any{
			"uri": atomURI(atom.ID),
		})
		if result.IsError {
			t.Errorf("reference atom %q: uri fetch errored (dead pointer): %s", atom.ID, getTextContent(t, result))
			continue
		}
		if strings.TrimSpace(getTextContent(t, result)) == "" {
			t.Errorf("reference atom %q: fetch returned an empty body", atom.ID)
		}
	}
	if refCount == 0 {
		t.Fatal("no reference atoms in corpus — the pointer-render fetch path is untested")
	}
}

// TestKnowledgeTool_AtomURI_RejectsInline pins the placeholder-leak guard: an
// INLINE atom (reference:false) is delivered into the workflow response with
// {hostname}/{stage-hostname} substituted at synthesis. Its raw body must NOT
// be fetchable by URI — exposing it would leak unsubstituted placeholders to
// the agent. The adapter rejects every non-reference atom URI.
func TestKnowledgeTool_AtomURI_RejectsInline(t *testing.T) {
	t.Parallel()
	corpus, err := workflow.LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	// The first inline atom is enough — the guard rejects ALL non-reference
	// atoms; scanning the corpus avoids hard-coding an atom that may be renamed.
	var inlineID string
	for _, atom := range corpus {
		if !atom.Reference {
			inlineID = atom.ID
			break
		}
	}
	if inlineID == "" {
		t.Fatal("no inline atom in corpus — cannot exercise the reject path")
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, testKnowledgeStore(t), nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"uri": atomURI(inlineID),
	})
	if !result.IsError {
		t.Errorf("inline atom %q must be rejected via uri= (placeholder-leak guard), got body: %s",
			inlineID, getTextContent(t, result))
	}
}

// TestKnowledgeTool_AtomURI_UnknownErrors rejects an unknown atom id with an
// error rather than returning an empty body silently.
func TestKnowledgeTool_AtomURI_UnknownErrors(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, testKnowledgeStore(t), nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"uri": atomURI("develop-does-not-exist"),
	})
	if !result.IsError {
		t.Errorf("expected error for unknown atom id, got: %s", getTextContent(t, result))
	}
}
