// Tests for: knowledge.go — zerops_knowledge MCP tool handler.

package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/workflow"
)

func testKnowledgeStore(t *testing.T) *knowledge.Store {
	t.Helper()
	docs := map[string]*knowledge.Document{
		"zerops://themes/core": {
			URI:     "zerops://themes/core",
			Title:   "Zerops Core Reference",
			Content: "# Zerops Core Reference\n\nUniversal rules here.",
		},
		"zerops://themes/universals": {
			URI:     "zerops://themes/universals",
			Title:   "Zerops Platform Universals",
			Content: "# Zerops Platform Universals\n\nBind 0.0.0.0. deployFiles mandatory.",
		},
		"zerops://runtimes/php": {
			URI:     "zerops://runtimes/php",
			Title:   "PHP on Zerops",
			Content: "# PHP on Zerops\n\n## Keywords\nphp, php-nginx, zerops.yml\n\n## TL;DR\nPHP-specific rules.\n\n### Details\nPHP-specific rules.",
		},
		"zerops://runtimes/nodejs": {
			URI:     "zerops://runtimes/nodejs",
			Title:   "Node.js on Zerops",
			Content: "# Node.js on Zerops\n\n## Keywords\nnodejs, node, npm\n\n## TL;DR\nNode.js-specific rules.\n\n### Details\nNode.js-specific rules.",
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
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

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
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

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
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

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
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

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
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

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
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

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
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

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
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

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

func TestKnowledgeTool_ScopeInfrastructure_PrependsUniversals(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"scope": "infrastructure",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if !strings.Contains(text, "Platform Universals") {
		t.Error("scope=infrastructure should prepend universals")
	}
	// Universals should appear before core content
	uIdx := strings.Index(text, "Platform Universals")
	cIdx := strings.Index(text, "Zerops Core Reference")
	if uIdx >= cIdx {
		t.Error("universals should appear before core reference")
	}
}

func TestKnowledgeTool_RecipeMode_PrependsUniversals(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{"recipe": "ghost"})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if !strings.Contains(text, "Platform Universals") {
		t.Error("recipe should be prepended with universals")
	}
	if !strings.Contains(text, "maxContainers") {
		t.Error("recipe should still contain original content")
	}
}

func TestKnowledgeTool_ScopeInvalid_Error(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

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
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"scope":   "infrastructure",
		"runtime": "nodejs@22",
	})

	if !result.IsError {
		t.Error("expected error for mixed scope + briefing modes")
	}
}

// --- Item 23: Knowledge tool persistence tests ---

func TestKnowledgeTool_PersistsScope(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)

	engine := testBootstrapEngine(t)
	RegisterKnowledge(srv, store, nil, nil, nil, engine)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"scope": "infrastructure",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	// Verify scope was persisted to session state.
	state, err := engine.GetState()
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state.Bootstrap == nil || state.Bootstrap.Context == nil {
		t.Fatal("bootstrap context should exist after scope call")
	}
	if !state.Bootstrap.Context.ScopeLoaded {
		t.Error("ScopeLoaded should be true after scope call")
	}
}

func TestKnowledgeTool_PersistsBriefing(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)

	engine := testBootstrapEngine(t)
	RegisterKnowledge(srv, store, nil, nil, nil, engine)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"runtime":  "php-nginx@8.4",
		"services": []string{"postgresql@16"},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	// Verify briefing was persisted to session state.
	state, err := engine.GetState()
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state.Bootstrap == nil || state.Bootstrap.Context == nil {
		t.Fatal("bootstrap context should exist after briefing call")
	}
	if state.Bootstrap.Context.BriefingFor != "php-nginx@8.4+postgresql@16" {
		t.Errorf("BriefingFor: want %q, got %q", "php-nginx@8.4+postgresql@16", state.Bootstrap.Context.BriefingFor)
	}
}

func TestKnowledgeTool_BriefingRuntime_EmptyServices(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "empty_services_array",
			args: map[string]any{
				"runtime":  "nodejs@22",
				"services": []string{},
			},
		},
		{
			name: "omitted_services",
			args: map[string]any{
				"runtime": "nodejs@22",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := callTool(t, srv, "zerops_knowledge", tt.args)
			if result.IsError {
				t.Errorf("should not error with runtime set: %s", getTextContent(t, result))
			}
			text := getTextContent(t, result)
			if !strings.Contains(text, "Node.js") {
				t.Error("briefing should contain Node.js runtime content")
			}
		})
	}
}

// testBootstrapEngine creates a workflow engine with an active bootstrap session.
func testBootstrapEngine(t *testing.T) *workflow.Engine {
	t.Helper()
	engine := workflow.NewEngine(t.TempDir())
	if _, err := engine.BootstrapStart("proj-1", "test intent"); err != nil {
		t.Fatalf("bootstrap start: %v", err)
	}
	return engine
}
