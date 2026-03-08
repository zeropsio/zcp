// Tests for: knowledge dedup — skip universals when scope loaded, skip briefing when already loaded.
package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestKnowledgeTool_ScopeDedup_SkipsUniversals(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)

	engine := testBootstrapEngine(t)
	RegisterKnowledge(srv, store, nil, nil, nil, engine)

	// First call: should include universals.
	result1 := callTool(t, srv, "zerops_knowledge", map[string]any{
		"scope": "infrastructure",
	})
	if result1.IsError {
		t.Fatalf("first call error: %s", getTextContent(t, result1))
	}
	text1 := getTextContent(t, result1)
	if !strings.Contains(text1, "Platform Universals") {
		t.Error("first scope call should include universals")
	}

	// Second call: ScopeLoaded is now true, should skip universals.
	result2 := callTool(t, srv, "zerops_knowledge", map[string]any{
		"scope": "infrastructure",
	})
	if result2.IsError {
		t.Fatalf("second call error: %s", getTextContent(t, result2))
	}
	text2 := getTextContent(t, result2)
	if strings.Contains(text2, "Platform Universals") {
		t.Error("second scope call should NOT include universals (already loaded)")
	}
	if !strings.Contains(text2, "Zerops Core Reference") {
		t.Error("second scope call should still include core reference")
	}
}

func TestKnowledgeTool_ScopeDedup_NoEngine_IncludesUniversals(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)

	// No engine — universals always included.
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"scope": "infrastructure",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "Platform Universals") {
		t.Error("scope without engine should include universals")
	}
}

func TestKnowledgeTool_BriefingDedup_ReturnStub(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)

	engine := testBootstrapEngine(t)
	RegisterKnowledge(srv, store, nil, nil, nil, engine)

	// First call: full briefing.
	result1 := callTool(t, srv, "zerops_knowledge", map[string]any{
		"runtime":  "php-nginx@8.4",
		"services": []string{"postgresql@16"},
	})
	if result1.IsError {
		t.Fatalf("first briefing error: %s", getTextContent(t, result1))
	}
	text1 := getTextContent(t, result1)
	if !strings.Contains(text1, "PHP") {
		t.Error("first briefing should include PHP content")
	}

	// Second call with same params: should return stub.
	result2 := callTool(t, srv, "zerops_knowledge", map[string]any{
		"runtime":  "php-nginx@8.4",
		"services": []string{"postgresql@16"},
	})
	if result2.IsError {
		t.Fatalf("second briefing error: %s", getTextContent(t, result2))
	}
	text2 := getTextContent(t, result2)
	if !strings.Contains(text2, "already loaded") {
		t.Error("second briefing with same key should return 'already loaded' stub")
	}
}

func TestKnowledgeTool_BriefingDedup_DifferentKey_FullBriefing(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)

	engine := testBootstrapEngine(t)
	RegisterKnowledge(srv, store, nil, nil, nil, engine)

	// First call with PHP.
	result1 := callTool(t, srv, "zerops_knowledge", map[string]any{
		"runtime": "php-nginx@8.4",
	})
	if result1.IsError {
		t.Fatalf("first call error: %s", getTextContent(t, result1))
	}

	// Second call with Node.js — different key, should return full briefing.
	result2 := callTool(t, srv, "zerops_knowledge", map[string]any{
		"runtime": "nodejs@22",
	})
	if result2.IsError {
		t.Fatalf("second call error: %s", getTextContent(t, result2))
	}
	text2 := getTextContent(t, result2)
	if strings.Contains(text2, "already loaded") {
		t.Error("different briefing key should NOT return stub")
	}
}

func TestKnowledgeTool_BriefingDedup_NoEngine_NoStub(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)

	// No engine — no dedup.
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	callTool(t, srv, "zerops_knowledge", map[string]any{
		"runtime": "php-nginx@8.4",
	})

	// Second call — no engine, so should return full briefing, not stub.
	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"runtime": "php-nginx@8.4",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if strings.Contains(text, "already loaded") {
		t.Error("without engine, briefing should never return stub")
	}
}
