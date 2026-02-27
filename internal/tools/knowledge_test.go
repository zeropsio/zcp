// Tests for: knowledge.go — zerops_knowledge MCP tool handler.

package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
)

func testKnowledgeStore(t *testing.T) *knowledge.Store {
	t.Helper()
	docs := map[string]*knowledge.Document{
		"zerops://themes/core": {
			URI:     "zerops://themes/core",
			Title:   "Zerops Core Reference",
			Content: "# Zerops Core Reference\n\nUniversal rules here.",
		},
		"zerops://themes/runtimes": {
			URI:     "zerops://themes/runtimes",
			Title:   "Runtime Deltas",
			Content: "## PHP\n\nPHP-specific rules.\n\n## Node.js\n\nNode.js-specific rules.",
		},
		"zerops://themes/services": {
			URI:     "zerops://themes/services",
			Title:   "Managed Service Reference",
			Content: "## PostgreSQL\n\nPort 5432.\n\n## Valkey\n\nPort 6379.",
		},
		"zerops://themes/wiring": {
			URI:     "zerops://themes/wiring",
			Title:   "Wiring Patterns",
			Content: "## Syntax Rules\n\nUse ${hostname_var}.\n\n## PostgreSQL\n\nDATABASE_URL.\n\n## Valkey\n\nREDIS_URL.",
		},
		"zerops://recipes/ghost": {
			URI:     "zerops://recipes/ghost",
			Title:   "Ghost Recipe",
			Content: "maxContainers: 1",
		},
	}
	store, err := knowledge.NewStore(docs)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	return store
}

func TestKnowledgeTool_Basic(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{"query": "postgresql"})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)

	var parsed []knowledge.SearchResult
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("failed to parse results: %v", err)
	}
	if len(parsed) == 0 {
		t.Error("expected at least one search result")
	}
}

func TestKnowledgeTool_WithLimit(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{"query": "zerops", "limit": 1})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed []knowledge.SearchResult
	text := getTextContent(t, result)
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("failed to parse results: %v", err)
	}
	if len(parsed) > 1 {
		t.Errorf("expected at most 1 result, got %d", len(parsed))
	}
}

func TestKnowledgeTool_EmptyQuery(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{"query": ""})

	if !result.IsError {
		t.Error("expected IsError for empty query")
	}
}

// --- New Mode Tests ---

func TestKnowledgeTool_BriefingMode(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"runtime":  "php-nginx@8.4",
		"services": []string{"postgresql@16"},
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	// Verify briefing contains stack-specific sections (no core — that's scope="infrastructure")
	if strings.Contains(text, "Zerops Core Reference") {
		t.Error("briefing should NOT contain core reference")
	}
	if !strings.Contains(text, "PHP") {
		t.Error("briefing missing PHP runtime delta")
	}
	if !strings.Contains(text, "PostgreSQL") {
		t.Error("briefing missing PostgreSQL card")
	}
}

func TestKnowledgeTool_RecipeMode(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{"recipe": "ghost"})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if !strings.Contains(text, "maxContainers") {
		t.Error("recipe missing expected content")
	}
}

func TestKnowledgeTool_ModeMixError(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"query":   "test",
		"runtime": "php@8",
	})

	if !result.IsError {
		t.Error("expected error for mixed modes")
	}
}

func TestKnowledgeTool_NoModeError(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{})

	if !result.IsError {
		t.Error("expected error for no mode")
	}
}

// --- Scope Mode Tests ---

func TestKnowledgeTool_ScopeInfrastructure_ReturnsCore(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"scope": "infrastructure",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if !strings.Contains(text, "Zerops Core Reference") {
		t.Error("scope=infrastructure should return core reference content")
	}
	if !strings.Contains(text, "Universal rules here") {
		t.Error("scope=infrastructure should return full core content")
	}
}

func TestKnowledgeTool_ScopeInvalid_Error(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"scope": "unknown",
	})

	if !result.IsError {
		t.Error("expected error for unknown scope")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "Unknown scope") {
		t.Errorf("error should mention unknown scope, got: %s", text)
	}
}

func TestKnowledgeTool_ScopePlusBriefing_Error(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"scope":   "infrastructure",
		"runtime": "nodejs@22",
	})

	if !result.IsError {
		t.Error("expected error for mixed scope + briefing modes")
	}
}
